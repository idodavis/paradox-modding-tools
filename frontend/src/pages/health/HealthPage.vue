<!-- The Health page: a rail switches between the summary and the views of compatibility, each mod and the log. -->
<script setup lang="ts">
import { computed, onActivated, onDeactivated, shallowRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { isEqual, keyBy } from "es-toolkit";
import { useRouter } from "vue-router";
import type { SplitterItem } from "@nuxt/ui";
import { ReorderMods } from "@services/workspace/service";
import type { Finding, Tally } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import { countFindings, describeCount, joinParts } from "../../lib/format";
import { useShown } from "../../app/pages";
import { errorLogQuery, keys, overviewQuery, reportQuery, stateQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/toolView";
import { useWorkspaceStore } from "../../stores/workspace";
import LeftOutMark from "../../ui/LeftOutMark.vue";
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
  levels?: Tally["counts"];
  count?: number;
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
const mods = computed(() => workspace.listMods());
const ids = computed(() => mods.value.map((workspaceMod) => workspaceMod.id));

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
// The mods' files may have changed while the page was away.
onActivated(() => {
  if (report.value) void cache.invalidateQueries({ key: keys.health });
});
// A row another page opens counts in the workspace's own order.
onDeactivated(() => (preview.value = []));

const compatibility = computed(() => report.value?.compatibility.findings ?? []);
/** The report's group of each mod, by mod id. */
const groups = computed(() => keyBy(report.value?.mods ?? [], (group) => group.mod));
const logRows = computed(() => gameLog.value?.findings ?? []);
const yours = computed(() =>
  countFindings(logRows.value.filter((finding) => finding.origin && !workspace.isExternal(finding.origin))),
);
const external = computed(() =>
  countFindings(logRows.value.filter((finding) => finding.origin && workspace.isExternal(finding.origin))),
);
const game = computed(() => countFindings(logRows.value.filter((finding) => !finding.origin)));

/** The name or check asked for. It opens the first view with a row for it. */
const wanted = computed(() => name || code);
const landing = computed(() => {
  if (!wanted.value) return { view: "summary", mod: "" };
  if (compatibility.value.some(matchFinding)) return { view: "compatibility", mod: "" };
  const owner = mods.value.find((candidate) => groups.value[candidate.id]?.findings?.some(matchFinding));
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
const currentId = computed(() => mod || landing.value.mod);
const current = computed(() => mods.value.find((candidate) => candidate.id === currentId.value) ?? mods.value[0]);
const filter = shallowRef(name);
watch(
  () => name,
  (next) => (filter.value = next),
);
/** The checks picked in the checks menu. A check asked for is picked there, not typed into the filter. */
const checks = shallowRef<string[]>(code ? [code] : []);
watch(
  () => code,
  (next) => (checks.value = next ? [next] : []),
);
/** The findings of the view shown, where the route's row is the index of the open one. */
const shownFindings = computed(() => listFindings(shown.value, current.value?.id));
const openFinding = computed((): Finding | undefined => (row ? shownFindings.value[Number(row)] : undefined));
useChip(() => {
  const finding = openFinding.value;
  const site = finding?.sites?.[0];
  const where = site ? ` at ${site.place.path} line ${site.line}` : "";
  return finding ? makeChip(finding.code, `Health's ${finding.code} finding about ${finding.name}${where}`) : null;
});

const { data: overview } = useQuery(() => ({
  ...overviewQuery,
  enabled: workspace.isOpen() && shown.value === "mod",
}));
const currentOverview = computed(() => overview.value?.find((counts) => counts.mod === current.value?.id));

const { mutate: reorder, isLoading: saving } = useMutation({
  mutation: (next: string[]) => ReorderMods(id, next),
  onSuccess: () => {
    preview.value = [];
  },
});

watch([() => id, () => ids.value.join()], () => {
  preview.value = [];
});
// A row's index points at another finding in another order's report.
watch(order, (next, previous) => {
  if (visible.value && row && !isEqual(next, previous))
    void router.replace({ query: { ...router.currentRoute.value.query, row: undefined } });
});

const rail = computed((): { title: string; rows: RailRow[] }[] =>
  [
    { title: "", rows: [{ view: "summary", label: "Summary", icon: "i-lucide-gauge" }] },
    {
      title: "Workspace",
      rows: [
        {
          view: "compatibility",
          label: "Compatibility",
          icon: "i-lucide-git-merge",
          levels: report.value?.compatibility.counts,
        },
      ],
    },
    { title: "Your mods", rows: mods.value.filter((workspaceMod) => !workspaceMod.external).map(toModRow) },
    {
      title: "External mods · not yours to fix",
      rows: mods.value.filter((workspaceMod) => workspaceMod.external).map(toModRow),
    },
    {
      title: "Game log",
      rows: [{ view: "log", label: "error.log", icon: "i-lucide-terminal", count: yours.value }],
    },
  ].filter((group) => group.rows.length),
);

/** toModRow returns the rail's row of a mod. */
function toModRow(workspaceMod: Mod): RailRow {
  return {
    view: "mod",
    mod: workspaceMod,
    label: workspaceMod.descriptor.name,
    levels: groups.value[workspaceMod.id]?.counts,
  };
}

/** matchFinding reports whether a finding has the name or check asked for. */
function matchFinding(finding: Finding): boolean {
  return name ? finding.name === name : finding.code === code;
}

/** matchLogRow reports whether a log row mentions the name or has the check asked for. */
function matchLogRow(finding: Finding): boolean {
  return name ? joinParts(finding.message).includes(name) : finding.code === code;
}

/** isShown reports whether a row of the rail is the view shown. */
function isShown(railRow: RailRow): boolean {
  return railRow.view === shown.value && (!railRow.mod || railRow.mod.id === current.value?.id);
}

/** listFindings returns the findings the given view lists, in row order. */
function listFindings(view: string, modId?: string): Finding[] {
  if (view === "compatibility") return compatibility.value;
  if (view === "log") return logRows.value;
  return view === "mod" ? (groups.value[modId ?? ""]?.findings ?? []) : [];
}

/** show shows a view, with a mod and a finding open if given. It clears the filter and the checks picked. */
function show(next: string, modId?: string, finding?: Finding): void {
  filter.value = "";
  checks.value = [];
  void router.replace({
    query: { view: next, mod: modId, row: finding && String(listFindings(next, modId).indexOf(finding)) },
  });
}

/** showCheck shows compatibility with its rows filtered to one check. */
function showCheck(check: string): void {
  void router.replace({ query: { view: "compatibility", code: check } });
}

/** keepRailWidth keeps the rail's width from the panes' sizes. */
function keepRailWidth(sizes: number[]): void {
  if (sizes.length === 2 && sizes[0]) railWidth.value = Math.round(sizes[0]);
}

/** openRow opens a finding of the view shown. */
function openRow(finding: Finding): void {
  void router.replace({
    query: {
      view: shown.value,
      mod: shown.value === "mod" ? current.value?.id : undefined,
      row: String(shownFindings.value.indexOf(finding)),
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
              <UTooltip v-if="railRow.mod?.external" text="External mod">
                <UIcon name="i-lucide-package" class="size-3.5 shrink-0 text-dimmed" aria-label="External mod" />
              </UTooltip>
              <LeftOutMark v-if="railRow.mod?.leftOut" />
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
            :groups="report?.mods ?? []"
            :yours="yours"
            :external="external"
            :game="game"
            :opened="openFinding"
            @open="openRow"
            @fix="(modId, finding) => show('mod', modId, finding)"
          />
          <p v-else-if="!report" class="p-6 text-12 text-dimmed">Checking the mods.</p>
          <HealthSummary
            v-else-if="shown === 'summary'"
            :mods="mods"
            :report="report"
            :log="gameLog"
            :yours="yours"
            :external="external"
            :game="game"
            @open="show"
            @check="showCheck"
          />
          <HealthCompatibility
            v-else-if="shown === 'compatibility'"
            v-model:order="order"
            v-model:filter="filter"
            v-model:checks="checks"
            :findings="compatibility"
            :changes="report.changes ?? []"
            :mods="mods"
            :opened="openFinding"
            :dirty="dirty"
            :saving="saving"
            @open="openRow"
            @save="reorder(order)"
          />
          <HealthMod
            v-else-if="current"
            :key="current.id"
            v-model:filter="filter"
            v-model:checks="checks"
            :mod="current"
            :findings="groups[current.id]?.findings ?? []"
            :languages="groups[current.id]?.languages ?? []"
            :overview="currentOverview"
            :log="logRows"
            :compatibility="compatibility"
            :opened="openFinding"
            @open="openRow"
            @log="(finding) => show('log', undefined, finding)"
            @compatibility="(finding) => show('compatibility', undefined, finding)"
          />
        </section>
      </template>
    </USplitter>
  </div>
</template>
