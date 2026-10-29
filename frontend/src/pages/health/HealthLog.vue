<!--
  The game's error log, with the lines about your mods set apart from the game's own. It lists them by mod, each mod's
  section folding, or by message, with the open line's detail beside them.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { sortBy } from "es-toolkit";
import type { Finding, GameLog } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import {
  countLines,
  describeCount,
  describeDetail,
  describeLogLine,
  describeTime,
  type FindingRow,
  type Level,
  LEVELS,
  listFindingRows,
  type Part,
  plural,
  splitFinding,
  splitLogLine,
  splitMessage,
  toSiteKey,
} from "../../lib/format";
import { codesQuery, loadQuery, placeQuery, snippetsQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import FactText from "../../ui/FactText.vue";
import ModTitle from "../../ui/ModTitle.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ProblemRow from "../../ui/ProblemRow.vue";
import HealthFrame from "./HealthFrame.vue";
import { PANES, ROWS } from "./HealthList.vue";
import HealthLink from "./HealthLink.vue";
import HealthSection from "./HealthSection.vue";
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

/** A HeadRow is the row above one mod's lines, which may be folded, or above the game's own when it has no mod. */
type HeadRow = { mod: string; count: number; folded: boolean };

const GROUPINGS = [
  { label: "By mod", value: "mod" },
  { label: "By message", value: "message" },
];
/** WHY holds the sentence that says why a row is a mod's, by what ties it to the mod: a file or a name. */
const WHY: Readonly<Record<string, string>> = {
  file: "The line points at {file}, and the game loads that file from {mod}.",
  name: "The line names {name}, and only {mod} defines it.",
};

const workspace = useWorkspaceStore();
const editor = useEditorStore();
const { data: codes } = useQuery(codesQuery);
// the tabs give their values as text or numbers
const scope = shallowRef<string | number>("mods");
const grouping = shallowRef<string | number>("mod");
/** The mods whose sections are folded. */
const folded = shallowRef<string[]>([]);

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
      const shut = folded.value.includes(mod.id);
      const head = { mod: mod.id, count: countLines(own.map((row) => row.finding)), folded: shut };
      return own.length ? [head, ...(shut ? [] : own)] : [];
    });
    return grouped.length && game ? [...grouped, { mod: "", count: game, folded: true }] : grouped;
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
/** The mod's problem at the open row's place, if any, with the sentence that says the mod's own check finds it. */
const fault = computed(() => {
  const shared = open.value && faults.value.get(toSiteKey(open.value));
  if (!shared) return undefined;
  const mod: Part = { text: workspace.originName(shared.mod), style: "mod" };
  const parts: Part[] = [
    mod,
    { text: "'s own check finds the same problem: ", style: "" },
    ...splitFinding(shared.finding, codes.value),
    { text: " Open it in ", style: "" },
    mod,
    { text: "'s problems.", style: "" },
  ];
  return { ...shared, parts };
});

const { mutate: openFile } = useMutation({
  mutation: async (path: string) => {
    const place = await loadQuery(placeQuery(path));
    if (place) editor.show(place);
  },
});

/** describeWhy returns the sentence that says why a row is a mod's, as parts: the file it points at, or its name. */
function describeWhy(finding: Finding): Part[] {
  const named = finding.facts?.named ?? "";
  return splitMessage(WHY[finding.facts?.why ?? ""] ?? "", {
    file: named,
    name: named,
    mod: workspace.originName(finding.origin),
  });
}

/** fold folds a mod's section, or unfolds it if it is folded. */
function fold(mod: string): void {
  folded.value = folded.value.includes(mod) ? folded.value.filter((one) => one !== mod) : [...folded.value, mod];
}

/** describeFaultColor returns the level color of the mod's problem at a row's place, or undefined for none. */
function describeFaultColor(finding: Finding): Level["color"] | undefined {
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
          <button
            v-if="'mod' in item && item.mod"
            type="button"
            class="flex h-8 w-full items-center gap-2 border-b border-default px-3 text-12 font-medium text-toned hover:bg-accented"
            :aria-expanded="!item.folded"
            @click="fold(item.mod)"
          >
            <ModTitle :origin="item.mod" />
            <span class="text-11 font-normal text-dimmed tabular-nums">{{ plural(item.count, "line") }}</span>
            <span class="grow" />
            <UIcon
              :name="item.folded ? 'i-lucide-chevron-right' : 'i-lucide-chevron-down'"
              class="size-3.5 text-dimmed"
            />
          </button>
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
            <template #message><FactText :parts="splitLogLine(item.finding)" /></template>
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
        <HealthFrame v-if="open" icon="i-lucide-terminal" label="Game log">
          <template #title><FactText :parts="splitLogLine(open)" /></template>
          <template #line>
            <span v-if="open.count > 1" class="whitespace-nowrap text-toned">
              ×{{ describeCount(open.count) }} this run
            </span>
            <span v-if="open.facts?.time" class="whitespace-nowrap">· last at {{ open.facts.time }}</span>
            <template v-if="open.origin">· <ModTitle :origin="open.origin" /></template>
          </template>
          <template #buttons>
            <UButton
              v-if="sites[0]"
              label="Open in the IDE"
              icon="i-lucide-code"
              @click="sites[0] && openSite(sites[0])"
            />
          </template>
          <HealthSection title="What the game logged">
            <pre
              class="overflow-auto rounded-md bg-(--bg-0) px-3 py-2.5 font-mono text-11.5 leading-4.5 whitespace-pre-wrap text-toned"
            >
[{{ open.facts?.time }}][{{ open.facts?.source }}]: {{ open.facts?.message }}</pre>
          </HealthSection>
          <HealthSection v-if="open.origin" :title="`Why it's listed under ${workspace.originName(open.origin)}`">
            <p class="max-w-[74ch] text-13 leading-5.5 text-toned"><FactText :parts="describeWhy(open)" /></p>
          </HealthSection>
          <HealthSection v-if="sites[0]" title="Where it points" @open="sites[0] && openSite(sites[0])">
            <HealthSite v-for="(site, index) in sites" :key="index" :site="site" :snippet="snippets?.[index]" />
          </HealthSection>
          <HealthSection v-if="fault" title="Health found it too">
            <HealthLink
              :level="fault.finding.level"
              :parts="fault.parts"
              @open="fault && emit('fix', fault.mod, fault.row)"
            />
          </HealthSection>
        </HealthFrame>
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </USplitter>
  </div>
</template>
