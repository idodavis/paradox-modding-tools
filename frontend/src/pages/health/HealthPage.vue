<!--
  The Health page. A rail switches between the summary and the views of workspace compatibility, of each mod and of
  the game's log.
-->
<script setup lang="ts">
import { computed, onActivated, onDeactivated, shallowRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { groupBy, isEqual } from "es-toolkit";
import { useRouter } from "vue-router";
import type { SplitterItem } from "@nuxt/ui";
import { ReorderMods } from "@services/workspace/service";
import type { Finding } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import { countLevels, countLines, describeCount, describeLogLine, findOwner, listProblems } from "../../lib/format";
import { useShown } from "../../app/pages";
import { errorLogQuery, keys, overviewQuery, reportQuery, stateQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/agent";
import { useWorkspaceStore } from "../../stores/workspace";
import LevelCounts from "../../ui/LevelCounts.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import HealthCompatibility from "./HealthCompatibility.vue";
import HealthLog from "./HealthLog.vue";
import HealthMod from "./HealthMod.vue";
import HealthSummary from "./HealthSummary.vue";

const {
  id,
  view = "",
  mod = "",
  row = "",
  name = "",
  code = "",
} = defineProps<{
  id: string;
  /** The view shown, or "" for the first one that matches the name or check. */
  view?: string;
  /** The mod the mod view shows. */
  mod?: string;
  /** The open row's index. */
  row?: string;
  /** A name to filter the rows to. */
  name?: string;
  /** A check to filter the rows to. */
  code?: string;
}>();

/** A RailRow is one row of the rail, a view or one mod's view. */
type RailRow = {
  view: string;
  label: string;
  icon?: string;
  mod?: Mod;
  levels?: Record<string, number>;
  count?: number;
  /** Whether the row's mod is left out of the compatibility checks. */
  leftOut?: boolean;
};

/** The rail's pane, wide enough for a long mod name and its counts. */
const RAIL = {
  id: "rail",
  slot: "rail",
  order: 1,
  sizeUnit: "px" as const,
  defaultSize: 410,
  minSize: 200,
  maxSize: 520,
};
/** The view's pane. The summary has no other. */
const VIEW = { id: "view", slot: "view", order: 2 };

const router = useRouter();
const workspace = useWorkspaceStore();
const cache = useQueryCache();

const { data: state } = useQuery(stateQuery);
const mods = computed(() => state.value?.mods ?? []);
const ids = computed(() => mods.value.map((one) => one.id));

/** The load order dragged to preview, or [] for the workspace's own. */
const preview = shallowRef<string[]>([]);
const order = computed({
  get: () => (preview.value.length ? preview.value : ids.value),
  set: (next: string[]) => (preview.value = next),
});
const dirty = computed(() => !isEqual(order.value, ids.value));

const { data: report, error } = useQuery(() => ({
  ...reportQuery(order.value),
  enabled: workspace.isOpen() && !!state.value,
}));
const { data: gameLog, error: logError } = useQuery(() => ({ ...errorLogQuery, enabled: workspace.isOpen() }));
const visible = useShown();
// the mods' files may have changed while the page was away
onActivated(() => {
  if (report.value) void cache.invalidateQueries({ key: keys.health });
});
// a row another page opens counts in the workspace's own order
onDeactivated(() => (preview.value = []));
/** When the report was made. */
const checkedAt = computed(() => {
  void report.value;
  return new Date(cache.get(reportQuery(order.value).key)?.when ?? Date.now());
});

const findings = computed(() => report.value?.findings ?? []);
const compatibility = computed(() => findings.value.filter((finding) => finding.question === "compatibility"));
/** Each mod's coverage findings. Its list of problems leaves them out. */
const languages = computed(() =>
  groupBy(
    findings.value.filter((finding) => finding.question === "health" && finding.code === "coverage"),
    findOwner,
  ),
);
const problems = computed(() => listProblems(findings.value));
const logRows = computed(() => gameLog.value?.findings ?? []);
const yours = computed(() => countLines(logRows.value.filter((finding) => finding.origin)));
const game = computed(() => countLines(logRows.value.filter((finding) => !finding.origin)));
const compatibilityLevels = computed(() => countLevels(compatibility.value));
const healthLevels = computed(() => countLevels(Object.values(problems.value).flat()));
/** The level counts of each mod, zero for a mod with no problem. */
const modLevels = computed(() =>
  Object.fromEntries(mods.value.map((one) => [one.id, countLevels(problems.value[one.id] ?? [])])),
);

/** The name or check asked for. It opens the first view with a row for it. */
const wanted = computed(() => name || code);
const landing = computed(() => {
  if (!wanted.value) return { view: "summary", mod: "" };
  if (compatibility.value.some(matchFinding)) return { view: "compatibility", mod: "" };
  const owner = mods.value.find((one) => problems.value[one.id]?.some(matchFinding));
  if (owner) return { view: "mod", mod: owner.id };
  return { view: logRows.value.some(matchLogRow) ? "log" : "summary", mod: "" };
});
const shown = computed(() => view || landing.value.view);
/** The rail's width in pixels, kept between visits. */
const railWidth = useLocalStorage("pmt.health.rail", RAIL.defaultSize);
/** The splitter's panes. The rail takes its kept width when it appears, not while it is dragged. */
const panes = shallowRef<SplitterItem[]>([VIEW]);
watch(
  () => shown.value === "summary",
  (summary) => (panes.value = summary ? [VIEW] : [{ ...RAIL, defaultSize: railWidth.value }, VIEW]),
  { immediate: true },
);
const current = computed(() => mods.value.find((one) => one.id === (mod || landing.value.mod)) ?? mods.value[0]);
const filter = shallowRef(wanted.value);
watch(wanted, (next) => (filter.value = next));
const opened = computed(() => (row ? Number(row) : -1));
// the open row's finding goes with an agent's prompt
const openFinding = computed((): Finding | undefined => {
  if (opened.value < 0) return undefined;
  if (shown.value === "compatibility") return compatibility.value[opened.value];
  if (shown.value === "log") return logRows.value[opened.value];
  return shown.value === "mod" && current.value ? problems.value[current.value.id]?.[opened.value] : undefined;
});
useChip(() => {
  const finding = openFinding.value;
  const site = finding?.sites?.[0];
  const where = site ? ` at ${site.place.path} line ${site.range.startLineNumber}` : "";
  return finding ? makeChip(finding.code, `Health's ${finding.code} finding about ${finding.name}${where}`) : null;
});

const { data: overview } = useQuery(() => ({
  ...overviewQuery,
  enabled: workspace.isOpen() && shown.value === "mod",
}));
const currentOverview = computed(() => overview.value?.find((one) => one.mod === current.value?.id));

const { mutate: reorder, isLoading: saving } = useMutation({
  mutation: (next: string[]) => ReorderMods(id, next),
  onSuccess: () => {
    preview.value = [];
  },
});

watch([() => id, () => ids.value.join()], () => {
  preview.value = [];
});
// a row's index points at another finding in another order's report
watch(order, (next, previous) => {
  if (visible.value && row && !isEqual(next, previous))
    void router.replace({ query: { ...router.currentRoute.value.query, row: undefined } });
});

const rail = computed((): { title: string; rows: RailRow[] }[] => [
  { title: "", rows: [{ view: "summary", label: "Summary", icon: "i-lucide-gauge" }] },
  {
    title: "Workspace",
    rows: [
      { view: "compatibility", label: "Compatibility", icon: "i-lucide-git-merge", levels: compatibilityLevels.value },
    ],
  },
  {
    title: "Mods",
    rows: mods.value.map((one) => ({
      view: "mod",
      mod: one,
      label: one.descriptor.name,
      levels: modLevels.value[one.id],
      leftOut: workspace.isLeftOut(one.id),
    })),
  },
  {
    title: "Game log",
    rows: [{ view: "log", label: "error.log", icon: "i-lucide-terminal", count: yours.value }],
  },
]);

/** matchFinding reports whether a finding has the name or check asked for. */
function matchFinding(finding: Finding): boolean {
  return name ? finding.name === name : finding.code === code;
}

/** matchLogRow reports whether a log row mentions the name or has the check asked for. */
function matchLogRow(finding: Finding): boolean {
  return name ? describeLogLine(finding).includes(name) : finding.code === code;
}

/** isShown reports whether a row of the rail is the view shown. */
function isShown(railRow: RailRow): boolean {
  return railRow.view === shown.value && (!railRow.mod || railRow.mod.id === current.value?.id);
}

/** show shows a view, with a mod and a row open in it if given. It clears the filter. */
function show(next: string, modId?: string, index?: number): void {
  filter.value = "";
  void router.replace({
    query: { view: next, mod: modId, row: index === undefined ? undefined : String(index) },
  });
}

/** showCheck shows workspace compatibility with its rows filtered to one check. */
function showCheck(check: string): void {
  void router.replace({ query: { view: "compatibility", code: check } });
}

/** keepRailWidth keeps the rail's width from the panes' sizes. The summary has no rail. */
function keepRailWidth(sizes: number[]): void {
  if (sizes.length === 2 && sizes[0]) railWidth.value = Math.round(sizes[0]);
}

/** openRow opens a row of the view shown. */
function openRow(index: number): void {
  void router.replace({
    query: {
      view: shown.value,
      mod: shown.value === "mod" ? current.value?.id : undefined,
      row: String(index),
      name: name || undefined,
      code: code || undefined,
    },
  });
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <UAlert
      v-if="error"
      color="error"
      variant="subtle"
      title="Health could not check the mods. Reopen the workspace from the Library to try again."
      :description="error.message"
      class="m-4 shrink-0"
    />
    <UAlert
      v-if="logError && shown !== 'log'"
      color="error"
      variant="subtle"
      title="The game's log could not be read. Run the game once to write it."
      :description="logError.message"
      class="mx-4 mb-4 shrink-0"
    />
    <USplitter :items="panes" class="min-h-0 grow" @layout="keepRailWidth">
      <template #rail>
        <nav class="flex min-w-0 grow flex-col gap-0.5 overflow-y-auto border-e border-default bg-muted p-2">
          <template v-for="group in rail" :key="group.title">
            <span v-if="group.title" class="px-2 pt-3 pb-1 text-11 font-semibold tracking-wide text-dimmed uppercase">
              {{ group.title }}
            </span>
            <button
              v-for="railRow in group.rows"
              :key="railRow.mod?.id ?? railRow.view"
              type="button"
              class="flex items-center gap-2.5 rounded-md px-2 py-1.5 text-start text-12 hover:bg-accented"
              :class="isShown(railRow) ? 'bg-elevated text-highlighted' : 'text-toned'"
              @click="show(railRow.view, railRow.mod?.id)"
            >
              <ModAvatar v-if="railRow.mod" :mod="railRow.mod" size="2xs" />
              <span
                v-else-if="railRow.icon"
                class="flex size-6.5 shrink-0 items-center justify-center rounded-md border border-default bg-elevated"
              >
                <UIcon :name="railRow.icon" class="size-3.5 text-toned" />
              </span>
              <span class="min-w-0 grow truncate font-medium">{{ railRow.label }}</span>
              <UIcon
                v-if="railRow.leftOut"
                name="i-lucide-eye-off"
                class="size-3.5 shrink-0 text-dimmed"
                aria-label="Left out of workspace compatibility"
              />
              <LevelCounts v-if="railRow.levels" :levels="railRow.levels" />
              <span v-else-if="railRow.count !== undefined" class="shrink-0 text-11 text-toned tabular-nums">
                {{ describeCount(railRow.count) }}
              </span>
            </button>
          </template>
        </nav>
      </template>
      <template #view>
        <section class="min-w-0 grow">
          <HealthLog
            v-if="shown === 'log'"
            v-model:filter="filter"
            :log="gameLog"
            :error="logError"
            :mods="mods"
            :health="problems"
            :yours="yours"
            :game="game"
            :opened="opened"
            @open="openRow"
            @fix="(modId, index) => show('mod', modId, index)"
          />
          <p v-else-if="!report" class="p-6 text-12 text-dimmed">Checking the mods.</p>
          <HealthSummary
            v-else-if="shown === 'summary'"
            :mods="mods"
            :compatibility="compatibility"
            :health="problems"
            :log="gameLog"
            :checked-at="checkedAt"
            :compatibility-levels="compatibilityLevels"
            :health-levels="healthLevels"
            :mod-levels="modLevels"
            :yours="yours"
            :game="game"
            @open="show"
            @check="showCheck"
          />
          <HealthCompatibility
            v-else-if="shown === 'compatibility'"
            v-model:order="order"
            v-model:filter="filter"
            :findings="compatibility"
            :changes="report.changes ?? []"
            :mods="mods"
            :opened="opened"
            :dirty="dirty"
            :saving="saving"
            @open="openRow"
            @save="reorder(order)"
          />
          <HealthMod
            v-else-if="current"
            :key="current.id"
            v-model:filter="filter"
            :mod="current"
            :findings="problems[current.id] ?? []"
            :languages="languages[current.id] ?? []"
            :overview="currentOverview"
            :log="logRows"
            :compatibility="compatibility"
            :opened="opened"
            @open="openRow"
            @log="(index) => show('log', undefined, index)"
            @compatibility="(index) => show('compatibility', undefined, index)"
          />
        </section>
      </template>
    </USplitter>
  </div>
</template>
