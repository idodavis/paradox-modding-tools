<!-- The findings of one view in rows grouped by level and check, with filters above and the open one's detail. -->
<script lang="ts">
/** PANES are the splitter's two panes, the list and the detail. */
export const PANES = [{ slot: "list", defaultSize: 56, minSize: 20, maxSize: 80 }, { slot: "detail" }];

/** ROWS draws only the rows in view. Rows are measured after each frame, so a wrapped row causes no resize loop. */
export const ROWS = { estimateSize: 64, useAnimationFrameWithResizeObserver: true };
</script>

<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { groupBy, sortBy, uniq } from "es-toolkit";
import type { Finding } from "@services/health/models";
import type { Part } from "@services/lsp/models";
import {
  countFindings,
  countFindingsBy,
  describeCount,
  splitSubject,
  findOwner,
  joinParts,
  matchQuery,
  toSiteKey,
} from "../../lib/format";
import { levelsQuery, topicsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import FactText from "../../ui/FactText.vue";
import ProblemRow from "../../ui/ProblemRow.vue";

const {
  findings,
  empty,
  opened = undefined,
} = defineProps<{
  /** The findings of one group, folded and sorted by Go. */
  findings: Finding[];
  /** What the list says when there are no findings. */
  empty: string;
  /** The open finding, or undefined for none. */
  opened?: Finding;
}>();
const emit = defineEmits<{ open: [finding: Finding] }>();
/** The text a row must contain to be listed. */
const filter = defineModel<string>("filter", { default: "" });
/** The checks whose findings are listed. With none picked, every check's are. */
const checks = defineModel<string[]>("checks", { default: () => [] });

/** A LevelHead is the head row of one level, with the level's words and color. */
type LevelHead = { level: string; label: string; color: string; count: number };
/** A CheckHead is the head row of one check within a level. */
type CheckHead = { key: string; label: string; topic: string; count: number; open: boolean; shared: Part[] };

const SORTS = [
  { label: "Sort: worst first", value: "level" },
  { label: "Sort: name", value: "name" },
  { label: "Sort: file", value: "file" },
];

const workspace = useWorkspaceStore();
const { data: levels } = useQuery(levelsQuery);
const { data: topics } = useQuery(topicsQuery);
/** The levels the user chose to show, or undefined until they choose. */
const selection = shallowRef<string[]>();
/** The levels whose rows show: the user's choice, else the problems, so info starts folded. */
const shown = computed({
  get: () => selection.value ?? (levels.value ?? []).filter((level) => level.problem).map((level) => level.id),
  set: (next: string[]) => (selection.value = next),
});
/** Whether each check's rows show, set when its head is pressed. */
const pressed = shallowRef<Record<string, boolean>>({});
const sort = shallowRef("level");
// A check picked among other findings is dropped once no finding has it.
watch(
  () => findings,
  (next) => (checks.value = checks.value.filter((check) => next.some((finding) => finding.code === check))),
);

/** The level chips' items, each with its findings count. */
const levelItems = computed(() =>
  (levels.value ?? []).map((level) => ({
    label: `${level.label} ${describeCount(counts.value[level.id] ?? 0)}`,
    value: level.id,
    color: level.color,
  })),
);
/** The check filter's items, the most frequent check first. */
const checkItems = computed(() =>
  sortBy(Object.entries(groupBy(findings, (finding) => finding.code)), [([, group]) => -countFindings(group)]).map(
    ([check, group]) => ({ label: `${group[0]?.label} ${describeCount(countFindings(group))}`, value: check }),
  ),
);
const matches = computed(() =>
  findings.filter(
    (finding) =>
      (!checks.value.length || checks.value.includes(finding.code)) &&
      matchQuery(
        filter.value,
        joinParts(finding.message),
        finding.name,
        finding.code,
        finding.detail,
        joinParts(finding.brief),
        ...(finding.repeats ?? []).map((site) => `${site.place.rel}:${site.line}`),
      ),
  ),
);
const counts = computed(() => countFindingsBy(matches.value, (finding) => finding.level));
const rows = computed((): (LevelHead | CheckHead | Finding)[] =>
  (levels.value ?? []).flatMap(({ id: level, label, color }) => {
    const members = matches.value.filter((finding) => finding.level === level);
    if (!members.length) return [];
    const head = { level, label, color, count: counts.value[level] ?? 0 };
    if (!shown.value.includes(level)) return [head];
    const groups = sortBy(Object.entries(groupBy(members, (finding) => finding.code)), [
      ([, group]) => -countFindings(group),
    ]);
    return [head, ...groups.flatMap(([check, group]) => listCheck(level, check, group, groups.length))];
  }),
);

/** listCheck lists the head of one check's rows in a level, then the rows if they show. */
function listCheck(level: string, check: string, members: Finding[], siblings: number): (CheckHead | Finding)[] {
  const key = `${level}:${check}`;
  const shownByDefault =
    siblings === 1 || !!filter.value.trim() || checks.value.length > 0 || members.some((member) => member === opened);
  const open = pressed.value[key] ?? shownByDefault;
  const topic = topics.value?.find((candidate) => candidate.id === members[0]?.topic)?.label ?? "";
  const label = members[0]?.label ?? "";
  const head = { key, label, topic, count: countFindings(members), open, shared: describeShared(members) };
  if (!open) return [head];
  if (sort.value === "level") return [head, ...members];
  const order = sort.value === "name" ? (finding: Finding) => finding.name : toSiteKey;
  return [head, ...sortBy(members, [order])];
}

/** describeShared returns the one mod all the findings of a check share, as parts, or none if there is no such mod. */
function describeShared(members: Finding[]): Part[] {
  const others = uniq(members.map((finding) => finding.facts?.other ?? ""));
  if (others.length === 1 && others[0])
    return [
      { text: "all only in ", style: "" },
      { text: others[0], style: "mod" },
    ];
  const owners = uniq(members.map((finding) => (finding.question === "compatibility" ? findOwner(finding) : "")));
  return owners.length === 1 && owners[0] ? [{ text: workspace.originName(owners[0]), style: "mod" }] : [];
}

/**
 * describePlace returns where a mod's own problem is, as its file's name and line, or "" for any other. A problem
 * with a copy in the game names the mod's copy.
 */
function describePlace(finding: Finding): string {
  const site = finding.sites?.find((other) => other.place.origin === findOwner(finding)) ?? finding.sites?.[0];
  return finding.question === "health" && site ? `${site.place.name}:${site.line}` : "";
}

/** toggle folds a level's rows, or shows them if they are folded. */
function toggle(level: string): void {
  shown.value = shown.value.includes(level)
    ? shown.value.filter((shownLevel) => shownLevel !== level)
    : [...shown.value, level];
}

/** toggleCheck folds a check's rows, or shows them if they are folded. */
function toggleCheck(head: CheckHead): void {
  pressed.value = { ...pressed.value, [head.key]: !head.open };
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col">
    <div class="flex shrink-0 flex-wrap items-center gap-1.5 border-y border-default px-4 py-2">
      <slot name="tabs">
        <UCheckboxGroup
          v-model="shown"
          :items="levelItems"
          orientation="horizontal"
          variant="card"
          indicator="hidden"
          size="xs"
          :ui="{ fieldset: 'gap-1.5', item: 'px-2 py-0.5' }"
        >
          <template #label="{ item }">
            <span class="flex items-center gap-1.5">
              <span class="size-1.5 rounded-full" :style="{ background: `var(--ui-${item.color})` }" />
              {{ item.label }}
            </span>
          </template>
        </UCheckboxGroup>
      </slot>
      <USelectMenu
        v-model="checks"
        :items="checkItems"
        value-key="value"
        multiple
        placeholder="All checks"
        :search-input="false"
        size="xs"
        class="w-36"
        :ui="{ content: 'min-w-fit' }"
      />
      <span class="ms-auto flex items-center gap-1.5">
        <USelect v-model="sort" :items="SORTS" size="xs" class="w-36" />
        <UInput v-model="filter" icon="i-lucide-filter" size="xs" placeholder="Filter" class="w-36" />
      </span>
    </div>
    <USplitter :items="PANES" auto-save-id="pmt.health.list" class="min-h-0 grow overflow-hidden">
      <template #list>
        <UScrollArea
          v-if="rows.length"
          v-slot="{ item }"
          :items="rows"
          :virtualize="ROWS"
          class="h-full w-full border-e border-default bg-muted"
        >
          <ProblemRow
            v-if="'code' in item"
            :level="item.level"
            :message="joinParts(item.message)"
            :selected="item === opened"
            class="ps-8"
            @open="emit('open', item)"
          >
            <template v-if="item.name" #message>
              <span class="font-mono text-12"><FactText :parts="splitSubject(item)" /></span>
            </template>
            <template #detail>
              <span class="flex min-w-0 grow items-center gap-3 text-12 text-dimmed">
                <span class="min-w-0 truncate"><FactText :parts="item.brief" /></span>
                <span class="grow" />
                <span class="shrink-0 font-mono text-11">{{ describePlace(item) }}</span>
              </span>
            </template>
            <template #trailing>
              <UBadge v-if="item.count > 1" :label="`×${describeCount(item.count)}`" size="sm" class="shrink-0" />
              <slot name="trailing" :finding="item" />
            </template>
          </ProblemRow>
          <button
            v-else-if="'level' in item"
            type="button"
            class="flex h-8 w-full items-center gap-2 border-b border-default px-3 text-12 font-medium text-toned hover:bg-accented"
            @click="toggle(item.level)"
          >
            <span class="size-1.5 rounded-full" :style="{ background: `var(--ui-${item.color})` }" />
            {{ item.label }}
            <span class="text-11 font-normal text-dimmed tabular-nums">{{ describeCount(item.count) }}</span>
            <span class="grow" />
            <UIcon
              :name="shown.includes(item.level) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              class="size-3.5 text-dimmed"
            />
          </button>
          <button
            v-else
            type="button"
            class="flex h-7 w-full items-center gap-2 border-b border-default ps-5 pe-3 text-12 text-toned hover:bg-accented"
            @click="toggleCheck(item)"
          >
            <UIcon
              :name="item.open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              class="size-3.5 shrink-0 text-dimmed"
            />
            <span class="truncate font-medium text-highlighted">{{ item.label }}</span>
            <span class="shrink-0 text-11 text-dimmed">{{ item.topic }}</span>
            <span class="grow" />
            <span v-if="item.shared.length" class="shrink-0 text-11 whitespace-nowrap text-dimmed">
              <FactText :parts="item.shared" />
            </span>
            <span class="shrink-0 text-11 text-dimmed tabular-nums">{{ describeCount(item.count) }}</span>
          </button>
        </UScrollArea>
        <p v-else class="w-full border-e border-default bg-muted px-3 py-3 text-12 text-dimmed">
          {{ findings.length ? "No row matches the filters." : empty }}
        </p>
      </template>
      <template #detail><slot name="detail" /></template>
    </USplitter>
  </div>
</template>
