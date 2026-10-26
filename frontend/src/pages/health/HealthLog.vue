<!--
  The game's error log, with the lines about your mods set apart from the game's own. It lists them by mod or by
  message, with the open line's detail beside them.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { sortBy } from "es-toolkit";
import type { Finding, GameLog } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import {
  countLines,
  describeCheck,
  describeCount,
  describeDetail,
  describeFinding,
  describeLogLine,
  describeTime,
  type FindingRow,
  LEVELS,
  listFindingRows,
  plural,
  toSiteKey,
} from "../../lib/format";
import { codesQuery, loadQuery, placeQuery, snippetsQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ProblemRow from "../../ui/ProblemRow.vue";
import { PANES, ROWS } from "./HealthList.vue";
import HealthSite, { openSite } from "./HealthSite.vue";

const {
  log = undefined,
  error = null,
  mods,
  health,
  yours,
  game,
  opened = -1,
} = defineProps<{
  log?: GameLog;
  /** Why the log could not be read, or null. */
  error?: Error | null;
  mods: Mod[];
  /** Each mod's problems, by mod id. */
  health: Record<string, Finding[]>;
  /** How many lines of the log are the mods'. */
  yours: number;
  /** How many lines of the log are the game's own. */
  game: number;
  /** The open row's index, or -1 for none. */
  opened?: number;
}>();
const emit = defineEmits<{ open: [index: number]; fix: [mod: string, row: number] }>();
const filter = defineModel<string>("filter", { default: "" });

/** A HeadRow is the row above one mod's lines, or above the game's own when it has no mod. */
type HeadRow = { mod: string; count: number };

const GROUPINGS = [
  { label: "By mod", value: "mod" },
  { label: "By message", value: "message" },
];

const workspace = useWorkspaceStore();
const editor = useEditorStore();
const { data: codes } = useQuery(codesQuery);
// the tabs give their values as text or numbers
const scope = shallowRef<string | number>("mods");
const grouping = shallowRef<string | number>("mod");

const logRows = computed(() => listFindingRows(log?.findings ?? []));
const gameRows = computed(() => logRows.value.filter((row) => !row.finding.origin));
/** Whether any line repeats. Every row then gets a column for its count. */
const repeated = computed(() => logRows.value.some((row) => row.finding.count > 1));
const modRows = computed(() => logRows.value.filter((row) => row.finding.origin));
const scopes = computed(() => [
  { label: `From your mods ${describeCount(yours)}`, value: "mods" },
  { label: `The game's own ${describeCount(game)}`, value: "game" },
]);
const rows = computed((): (HeadRow | FindingRow)[] => {
  const needle = filter.value.trim().toLowerCase();
  const matches = (scope.value === "mods" ? modRows.value : gameRows.value).filter(
    (row) => !needle || `${describeLogLine(row.finding)} ${describeDetail(row.finding)}`.toLowerCase().includes(needle),
  );
  if (scope.value === "mods" && grouping.value === "mod") {
    const grouped = mods.flatMap((mod) => {
      const own = matches.filter((row) => row.finding.origin === mod.id);
      return own.length ? [{ mod: mod.id, count: countLines(own.map((row) => row.finding)) }, ...own] : [];
    });
    return grouped.length && game ? [...grouped, { mod: "", count: game }] : grouped;
  }
  return sortBy(matches, [(row) => -row.finding.count]);
});
const empty = computed(() => {
  if (error) return "The game's log could not be read.";
  if (!log) return "Reading the game's log.";
  if (!log.findings?.length) return "The game has not logged anything yet.";
  if (filter.value.trim()) return "No line matches the filter.";
  return scope.value === "mods" ? "No line of the game's last run names your mods." : "Every line names your mods.";
});
const lastRun = computed(() => (log?.writtenAt ? describeTime(log.writtenAt) : ""));
/** Which of the workspace's mods the run that wrote the log did not read from their own folders, or "". */
const runNote = computed(() => {
  const unloaded = (log?.unloaded ?? []).map((mod) => workspace.originName(mod));
  const copies = Object.entries(log?.copies ?? {}).map(([mod, folder]) => `${workspace.originName(mod)} (${folder})`);
  const parts = [
    unloaded.length ? `It did not load ${unloaded.join(", ")}.` : "",
    copies.length ? `It loaded another copy of ${copies.join(", ")}.` : "",
  ].filter(Boolean);
  return parts.length
    ? `This log is from a run without all of this workspace's mods. ${parts.join(" ")} Their lines count as the game's.`
    : "";
});
const open = computed(() => log?.findings?.[opened]);
const sites = computed(() => open.value?.sites ?? []);
const { data: snippets } = useQuery(() => ({
  ...snippetsQuery(sites.value),
  enabled: workspace.isOpen() && sites.value.length > 0,
}));
/** The mods' first problem at each place, with its mod and its row in that mod's list. */
const faults = computed(() => {
  const byPlace = new Map<string, { mod: string; row: number; finding: Finding }>();
  for (const [mod, problems] of Object.entries(health)) {
    problems.forEach((finding, row) => {
      const place = toSiteKey(finding);
      if (place && !byPlace.has(place)) byPlace.set(place, { mod, row, finding });
    });
  }
  return byPlace;
});
/** The mod's problem at the open row's place, if any. */
const fault = computed(() => {
  const shared = open.value && faults.value.get(toSiteKey(open.value));
  return shared && { ...shared, label: describeCheck(shared.finding.code, codes.value) };
});

const { mutate: openFile } = useMutation({
  mutation: async (path: string) => {
    const place = await loadQuery(placeQuery(path));
    if (place) editor.show(place);
  },
});

/** describeWhy says in a sentence why a row is a mod's. */
function describeWhy(finding: Finding): string {
  const mod = workspace.originName(finding.origin);
  const named = finding.facts?.named;
  return finding.facts?.why === "file"
    ? `Yours because the line names ${named}, a file the game loads from ${mod}.`
    : `Names ${named}, which only ${mod} defines.`;
}

/** describeFaultColor returns the level color of the mod's problem at a row's place, or undefined for none. */
function describeFaultColor(finding: Finding): "error" | "warning" | "info" | undefined {
  const shared = faults.value.get(toSiteKey(finding));
  return shared && LEVELS[shared.finding.level]?.color;
}

/** showGame shows the game's own rows. */
function showGame(): void {
  scope.value = "game";
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col">
    <div class="flex shrink-0 items-center gap-2.5 px-4 pt-3.5 pb-2.5">
      <span class="text-16 text-highlighted">Game log</span>
      <span v-if="workspace.isOpen()" class="flex items-center gap-1.5 text-11 text-dimmed">
        <span class="size-1.5 rounded-full" style="background: var(--ui-success)" />
        Live<template v-if="lastRun"> · last run {{ lastRun }}</template>
      </span>
      <span class="grow" />
      <UButton
        v-if="log && lastRun"
        label="Open error.log"
        icon="i-lucide-code"
        variant="ghost"
        color="neutral"
        @click="openFile(log.path)"
      />
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="mx-4 mb-2.5 shrink-0" />
    <UAlert v-if="runNote" color="warning" variant="subtle" :description="runNote" class="mx-4 mb-2.5 shrink-0" />
    <div class="flex shrink-0 flex-wrap items-center gap-2 px-4 pb-2.5">
      <UTabs v-model="scope" :items="scopes" :content="false" size="xs" variant="pill" color="neutral" />
      <span class="grow" />
      <UTabs
        v-if="scope === 'mods'"
        v-model="grouping"
        :items="GROUPINGS"
        :content="false"
        size="xs"
        variant="pill"
        color="neutral"
      />
      <UInput v-model="filter" icon="i-lucide-filter" size="xs" placeholder="Filter" class="w-40" />
    </div>
    <USplitter
      :items="PANES"
      auto-save-id="pmt.health.log"
      class="min-h-0 grow overflow-hidden border-t border-default"
    >
      <template #list>
        <UScrollArea
          v-if="rows.length"
          v-slot="{ item }"
          :items="rows"
          :virtualize="ROWS"
          class="h-full w-full border-e border-default bg-muted"
        >
          <div
            v-if="'mod' in item && item.mod"
            class="flex h-8 w-full items-center gap-2 border-b border-default px-3 text-12 font-medium text-toned"
          >
            <OriginChip :origin="item.mod" />
            <span class="text-11 font-normal text-dimmed tabular-nums">{{ plural(item.count, "line") }}</span>
          </div>
          <button
            v-else-if="'mod' in item"
            type="button"
            class="flex h-8 w-full items-center gap-2 border-b border-default px-3 text-12 font-medium text-toned hover:bg-accented"
            @click="showGame"
          >
            The game's own
            <span class="text-11 font-normal text-dimmed tabular-nums">{{ plural(item.count, "line") }}, hidden</span>
          </button>
          <ProblemRow
            v-else
            :message="describeLogLine(item.finding)"
            :detail="describeDetail(item.finding)"
            :mono="!!item.finding.sites?.length"
            :selected="item.index === opened"
            @open="emit('open', item.index)"
          >
            <template v-if="repeated" #leading>
              <span class="flex w-9 shrink-0 justify-center">
                <UBadge v-if="item.finding.count > 1" :label="`×${describeCount(item.finding.count)}`" size="sm" />
              </span>
            </template>
            <template #trailing>
              <OriginChip v-if="grouping === 'message' && item.finding.origin" :origin="item.finding.origin" dot />
              <UBadge
                v-if="describeFaultColor(item.finding)"
                label="in Health"
                :color="describeFaultColor(item.finding)"
                variant="subtle"
                size="sm"
                class="shrink-0"
              />
            </template>
          </ProblemRow>
        </UScrollArea>
        <p v-else class="w-full border-e border-default bg-muted p-4 text-12 text-dimmed">{{ empty }}</p>
      </template>
      <template #detail>
        <section v-if="open" class="flex h-full min-w-0 grow flex-col gap-3 overflow-auto bg-default p-4">
          <div class="flex items-center gap-2">
            <span class="min-w-0 text-13 font-semibold break-words text-highlighted">{{ describeLogLine(open) }}</span>
            <UBadge
              v-if="open.count > 1"
              :label="`×${describeCount(open.count)} this run`"
              size="sm"
              class="shrink-0"
            />
            <span class="grow" />
            <UButton v-if="sites.length" label="Open in the IDE" icon="i-lucide-code" @click="openSite(sites[0])" />
          </div>
          <div
            v-if="open.origin"
            class="flex items-start gap-2.5 rounded-lg border border-default bg-muted px-3 py-2.5 text-12 text-muted"
          >
            <UIcon name="i-lucide-pin" class="mt-0.5 size-3.5 shrink-0" />
            {{ describeWhy(open) }}
          </div>
          <pre
            class="overflow-auto rounded-lg border border-default bg-(--bg-0) p-3 font-mono text-11 whitespace-pre-wrap text-toned"
          >
[{{ open.facts?.time }}][{{ open.facts?.source }}]: {{ open.facts?.message }}</pre>
          <HealthSite v-for="(site, index) in sites" :key="index" :site="site" :snippet="snippets?.[index]" />
          <button
            v-if="fault"
            type="button"
            class="flex items-center gap-2.5 rounded-lg border border-default bg-muted px-3 py-2.5 text-start hover:bg-accented"
            @click="emit('fix', fault.mod, fault.row)"
          >
            <UIcon
              :name="LEVELS[fault.finding.level]?.icon ?? 'i-lucide-info'"
              class="size-4 shrink-0"
              :style="{ color: `var(--ui-${LEVELS[fault.finding.level]?.color})` }"
            />
            <span class="flex min-w-0 grow flex-col">
              <span class="text-12 text-highlighted">Health found it too: {{ fault.label }}</span>
              <span class="truncate text-11 text-dimmed">{{ describeFinding(fault.finding, codes) }}</span>
            </span>
            <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
          </button>
        </section>
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </USplitter>
  </div>
</template>
