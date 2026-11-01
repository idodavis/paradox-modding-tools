<!--
  The findings of one view in rows grouped by level and then by check, with the open one's detail beside them. Each
  row names the thing it is about, the worst first, and chips and filters above them narrow the rows.
-->
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
import {
  countFindings,
  countFindingsBy,
  countLevels,
  splitBrief,
  describeCheck,
  describeCount,
  describeDetail,
  describeFinding,
  splitSubject,
  findOwner,
  findTopic,
  joinParts,
  LEVELS,
  matchQuery,
  type Part,
  PROBLEM_LEVELS,
  TOPICS,
  toSiteKey,
} from "../../lib/format";
import { codesQuery } from "../../queries";
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

/** A LevelHead is the head row of one level. */
type LevelHead = { level: string; count: number };
/** A CheckHead is the head row of one check within a level, with the mod all its rows share, if any. */
type CheckHead = { key: string; check: string; count: number; open: boolean; shared: Part[] };

const LEVEL_ORDER = Object.keys(LEVELS);
const SORTS = [
  { label: "Sort: worst first", value: "level" },
  { label: "Sort: name", value: "name" },
  { label: "Sort: file", value: "file" },
];

const workspace = useWorkspaceStore();
const { data: codes } = useQuery(codesQuery);
/** The levels whose rows show. Info starts folded. */
const shown = shallowRef([...PROBLEM_LEVELS]);
/** Whether each check's rows show, set when its head is pressed. */
const pressed = shallowRef<Record<string, boolean>>({});
const checks = shallowRef<string[]>([]);
const sort = shallowRef("level");
// a check picked among other findings, such as another topic's, is dropped once no finding has it
watch(
  () => findings,
  (next) => (checks.value = checks.value.filter((check) => next.some((finding) => finding.code === check))),
);

/** The level chips' items, each level with how many findings it has, the ones folded into a row included. */
const levelItems = computed(() =>
  LEVEL_ORDER.map((level) => ({
    label: `${LEVELS[level]?.label} ${describeCount(counts.value[level] ?? 0)}`,
    value: level,
  })),
);
/** The check filter's items, the most frequent check first. */
const checkItems = computed(() => {
  return sortBy(Object.entries(countFindingsBy(findings, (finding) => finding.code)), [([, count]) => -count]).map(
    ([check, count]) => ({
      label: `${describeCheck(check, codes.value)} ${describeCount(count)}`,
      value: check,
    }),
  );
});
const matches = computed(() =>
  findings.filter(
    (finding) =>
      isPicked(finding.code) &&
      matchQuery(
        filter.value,
        describeFinding(finding, codes.value),
        finding.name,
        finding.code,
        describeDetail(finding),
        joinParts(splitBrief(finding)),
        ...(finding.repeats ?? []).map((site) => `${site.place.rel}:${site.line}`),
      ),
  ),
);
const counts = computed(() => countLevels(matches.value));
const rows = computed((): (LevelHead | CheckHead | Finding)[] =>
  LEVEL_ORDER.flatMap((level) => {
    const members = matches.value.filter((finding) => finding.level === level);
    if (!members.length) return [];
    const head = { level, count: counts.value[level] ?? 0 };
    if (!shown.value.includes(level)) return [head];
    const groups = sortBy(Object.entries(groupBy(members, (finding) => finding.code)), [
      ([, group]) => -countFindings(group),
    ]);
    return [head, ...groups.flatMap(([check, group]) => listCheck(level, check, group, groups.length))];
  }),
);

/** isPicked reports whether a check passes the check filter. */
function isPicked(check: string): boolean {
  return !checks.value.length || checks.value.includes(check);
}

/** listCheck lists the head of one check's rows in a level, then the rows if they show. */
function listCheck(level: string, check: string, members: Finding[], siblings: number): (CheckHead | Finding)[] {
  const key = `${level}:${check}`;
  const shownByDefault =
    siblings === 1 || !!filter.value.trim() || checks.value.length > 0 || members.some((member) => member === opened);
  const open = pressed.value[key] ?? shownByDefault;
  const head = { key, check, count: countFindings(members), open, shared: describeShared(members) };
  if (!open) return [head];
  // the worst first is the order Go sent them in
  if (sort.value === "level") return [head, ...members];
  const order = sort.value === "name" ? (finding: Finding) => finding.name : toSiteKey;
  return [head, ...sortBy(members, [order])];
}

/**
 * describeShared returns the mod every finding of a check shares, as parts: the one mod that alone defines the names
 * its mod lacks, or the one mod a check of the workspace's compatibility is about. It is empty if they share none.
 */
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

/** describePlace returns where a mod's own problem is, as its file's name and line. It is "" for any other finding. */
function describePlace(finding: Finding): string {
  const site = finding.sites?.[0];
  return finding.question === "health" && site ? `${site.place.name}:${site.line}` : "";
}

/** toggle folds a level's rows, or shows them if they are folded. */
function toggle(level: string): void {
  shown.value = shown.value.includes(level) ? shown.value.filter((one) => one !== level) : [...shown.value, level];
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
              <span class="size-1.5 rounded-full" :style="{ background: `var(--ui-${LEVELS[item.value]?.color})` }" />
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
      <!-- the sort and the filter wrap to the next line together -->
      <span class="ms-auto flex items-center gap-1.5">
        <USelect v-model="sort" :items="SORTS" size="xs" class="w-28" />
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
            :message="describeFinding(item, codes)"
            :selected="item === opened"
            class="ps-8"
            @open="emit('open', item)"
          >
            <template v-if="item.name" #message>
              <span class="font-mono text-12"><FactText :parts="splitSubject(item)" /></span>
            </template>
            <template #detail>
              <span class="flex min-w-0 grow items-center gap-3 text-12 text-dimmed">
                <span class="min-w-0 truncate"><FactText :parts="splitBrief(item)" /></span>
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
            <span class="size-1.5 rounded-full" :style="{ background: `var(--ui-${LEVELS[item.level]?.color})` }" />
            {{ LEVELS[item.level]?.label }}
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
            <span class="truncate font-medium text-highlighted">{{ describeCheck(item.check, codes) }}</span>
            <span class="shrink-0 text-11 text-dimmed">{{ TOPICS[findTopic(item.check, codes)] }}</span>
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
