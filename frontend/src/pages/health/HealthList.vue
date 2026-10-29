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
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { countBy, groupBy, sortBy, sumBy, uniq } from "es-toolkit";
import type { Finding } from "@services/health/models";
import {
  countLevels,
  describeBrief,
  describeCheck,
  describeCount,
  describeDetail,
  describeFinding,
  describeTitle,
  type FindingRow,
  findOwner,
  joinParts,
  LEVELS,
  listFindingRows,
  type Part,
  PROBLEM_LEVELS,
  TOPICS,
  toRepeatKey,
  toSiteKey,
  toWeight,
} from "../../lib/format";
import { codesQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import FactText from "../../ui/FactText.vue";
import ProblemRow from "../../ui/ProblemRow.vue";

const {
  findings,
  empty,
  opened = -1,
  topics = false,
  infoCheck = "",
} = defineProps<{
  findings: Finding[];
  /** What the list says when there are no findings. */
  empty: string;
  /** The open finding's index, or -1 for none. */
  opened?: number;
  /** Whether the chips pick a topic instead of folding levels. */
  topics?: boolean;
  /** The check of the info slot's row under Info, or "" for no such row. */
  infoCheck?: string;
}>();
const emit = defineEmits<{ open: [index: number] }>();
/** The text a row must contain to be listed. */
const filter = defineModel<string>("filter", { default: "" });

/** An Extra is the row that holds the info slot. */
type Extra = { extra: true };
/** A Repeats is one row for a finding and its repeats. */
type Repeats = { rows: FindingRow[] };
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
const topic = shallowRef("");
const checks = shallowRef<string[]>([]);
const sort = shallowRef("level");

const topicCounts = computed(() => countBy(findings, (finding) => findTopic(finding.code)));
/** The check filter's items, the most frequent check first. */
const checkItems = computed(() => {
  const counts = { ...countBy(findings, (finding) => finding.code), ...(infoCheck ? { [infoCheck]: 1 } : {}) };
  return sortBy(Object.entries(counts), [([, count]) => -count]).map(([check, count]) => ({
    label: `${describeCheck(check, codes.value)} ${describeCount(count)}`,
    value: check,
  }));
});
const findingRows = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  return listFindingRows(findings).filter(
    ({ finding }) =>
      isPicked(finding.code) &&
      (!needle ||
        `${describeFinding(finding, codes.value)} ${finding.name} ${finding.code} ${describeDetail(finding)} ${joinParts(describeBrief(finding))}`
          .toLowerCase()
          .includes(needle)),
  );
});
const extras = computed((): Extra[] =>
  infoCheck && isPicked(infoCheck) && !filter.value.trim() ? [{ extra: true }] : [],
);
const counts = computed((): Record<string, number> => {
  const levels = countLevels(findingRows.value.map((row) => row.finding));
  return { ...levels, info: levels.info + extras.value.length };
});
const rows = computed((): (LevelHead | CheckHead | Repeats | Extra)[] =>
  LEVEL_ORDER.flatMap((level) => {
    const members = [
      ...findingRows.value.filter((row) => row.finding.level === level),
      ...(level === "info" ? extras.value : []),
    ];
    if (!members.length) return [];
    const head = { level, count: members.length };
    if (!shown.value.includes(level)) return [head];
    const groups = sortBy(Object.entries(groupBy(members, findCheck)), [([, group]) => -group.length]);
    return [head, ...groups.flatMap(([check, group]) => listCheck(level, check, group, groups.length))];
  }),
);

/** findTopic returns the topic of a check, or "" if the app does not list it. */
function findTopic(check: string): string {
  return codes.value?.find((code) => code.id === check)?.topic ?? "";
}

/** findCheck returns the check of a row. The info slot's row has the info check. */
function findCheck(member: FindingRow | Extra): string {
  return "extra" in member ? infoCheck : member.finding.code;
}

/** isPicked reports whether a check passes the topic chips and the check filter. */
function isPicked(check: string): boolean {
  return (!topic.value || findTopic(check) === topic.value) && (!checks.value.length || checks.value.includes(check));
}

/** listCheck lists the head of one check's rows in a level, then the rows if they show. */
function listCheck(
  level: string,
  check: string,
  members: (FindingRow | Extra)[],
  siblings: number,
): (CheckHead | Repeats | Extra)[] {
  const key = `${level}:${check}`;
  const shownByDefault =
    siblings === 1 ||
    !!filter.value.trim() ||
    checks.value.length > 0 ||
    members.some((member) => "index" in member && member.index === opened);
  const open = pressed.value[key] ?? shownByDefault;
  const findingMembers = members.filter((member): member is FindingRow => "index" in member);
  const head = { key, check, count: members.length, open, shared: describeShared(findingMembers) };
  if (!open) return [head];
  const repeats = Object.values(groupBy(findingMembers, (row) => toRepeatKey(row.finding, codes.value))).map(
    (group): Repeats => ({ rows: group }),
  );
  const extra = members.filter((member): member is Extra => "extra" in member);
  // the worst first: the most used names, then the most repeated
  if (sort.value === "level")
    return [head, ...sortBy(repeats, [({ rows: group }) => -sumBy(group, (row) => toWeight(row.finding))]), ...extra];
  const sorted = sortBy(repeats, [
    ({ rows: [first] }) => (first ? (sort.value === "name" ? first.finding.name : toSiteKey(first.finding)) : ""),
  ]);
  return [head, ...sorted, ...extra];
}

/**
 * describeShared returns the mod every row of a check shares, as parts: the one mod that alone defines the names its
 * mod lacks, or the one mod a check of the workspace's compatibility is about. It is empty if the rows share none.
 */
function describeShared(rows: FindingRow[]): Part[] {
  const others = uniq(rows.map((row) => row.finding.facts?.other ?? ""));
  if (others.length === 1 && others[0])
    return [
      { text: "all only in ", style: "" },
      { text: others[0], style: "mod" },
    ];
  const owners = uniq(rows.map((row) => (row.finding.question === "compatibility" ? findOwner(row.finding) : "")));
  return owners.length === 1 && owners[0] ? [{ text: workspace.originName(owners[0]), style: "mod" }] : [];
}

/** describePlace returns where a mod's own problem is, as its file's name and line. It is "" for any other finding. */
function describePlace(finding: Finding): string {
  const site = finding.sites?.[0];
  return finding.question === "health" && site ? `${site.place.name}:${site.range.startLineNumber}` : "";
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
      <template v-if="topics">
        <UButton
          label="All topics"
          :color="topic ? 'neutral' : 'primary'"
          :variant="topic ? 'outline' : 'soft'"
          size="xs"
          @click="topic = ''"
        />
        <UButton
          v-for="(label, id) in TOPICS"
          v-show="topicCounts[id]"
          :key="id"
          :label="`${label} ${describeCount(topicCounts[id] ?? 0)}`"
          :color="topic === id ? 'primary' : 'neutral'"
          :variant="topic === id ? 'soft' : 'outline'"
          size="xs"
          @click="topic = id"
        />
      </template>
      <template v-else>
        <UButton
          v-for="level in LEVEL_ORDER"
          :key="level"
          :label="`${LEVELS[level]?.label} ${describeCount(counts[level] ?? 0)}`"
          :color="shown.includes(level) ? 'primary' : 'neutral'"
          :variant="shown.includes(level) ? 'soft' : 'outline'"
          size="xs"
          @click="toggle(level)"
        >
          <template #leading>
            <span class="size-1.5 rounded-full" :style="{ background: `var(--ui-${LEVELS[level]?.color})` }" />
          </template>
        </UButton>
      </template>
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
          <button
            v-if="'level' in item"
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
            v-else-if="'key' in item"
            type="button"
            class="flex h-7 w-full items-center gap-2 border-b border-default ps-5 pe-3 text-12 text-toned hover:bg-accented"
            @click="toggleCheck(item)"
          >
            <UIcon
              :name="item.open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              class="size-3.5 shrink-0 text-dimmed"
            />
            <span class="truncate font-medium text-highlighted">{{ describeCheck(item.check, codes) }}</span>
            <span class="shrink-0 text-11 text-dimmed">{{ TOPICS[findTopic(item.check)] }}</span>
            <span class="grow" />
            <span v-if="item.shared.length" class="shrink-0 text-11 whitespace-nowrap text-dimmed">
              <FactText :parts="item.shared" />
            </span>
            <span class="shrink-0 text-11 text-dimmed tabular-nums">{{ describeCount(item.count) }}</span>
          </button>
          <slot v-else-if="'extra' in item" name="info" />
          <ProblemRow
            v-else-if="item.rows[0]"
            :level="item.rows[0].finding.level"
            :message="describeFinding(item.rows[0].finding, codes)"
            :selected="item.rows.some((row) => row.index === opened)"
            class="ps-8"
            @open="emit('open', item.rows[0].index)"
          >
            <template v-if="item.rows[0].finding.name" #message>
              <span class="font-mono text-12"><FactText :parts="describeTitle(item.rows[0].finding)" /></span>
            </template>
            <template #detail>
              <span class="flex min-w-0 grow items-center gap-3 text-12 text-dimmed">
                <span class="min-w-0 truncate"><FactText :parts="describeBrief(item.rows[0].finding)" /></span>
                <span class="grow" />
                <span class="shrink-0 font-mono text-11">{{ describePlace(item.rows[0].finding) }}</span>
              </span>
            </template>
            <template #trailing>
              <UBadge
                v-if="item.rows.length > 1"
                :label="`×${describeCount(item.rows.length)}`"
                size="sm"
                class="shrink-0"
              />
              <slot name="trailing" :finding="item.rows[0].finding" :index="item.rows[0].index" />
            </template>
          </ProblemRow>
        </UScrollArea>
        <p v-else class="w-full border-e border-default bg-muted px-3 py-3 text-12 text-dimmed">
          {{ findings.length ? "No row matches the filters." : empty }}
        </p>
      </template>
      <template #detail><slot name="detail" /></template>
    </USplitter>
  </div>
</template>
