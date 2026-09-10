<script setup lang="ts">
/**
 * Workspace Health: GetHealth KPIs, drag-preview load order, one table.
 * Filters and sort live in UTable column headers.
 */
import { computed, ref, shallowRef, useTemplateRef, watch } from "vue";
import { useRoute } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useSortable } from "@vueuse/integrations/useSortable";
import {
  getFacetedUniqueValues,
  type ColumnFiltersState,
  type RowSelectionState,
  type SortingState,
} from "@tanstack/table-core";
import type { TableColumn } from "@nuxt/ui";
import { GetHealth, LookupLoc } from "@services/viewsservice";
import { ReorderWorkspaceMods } from "@services/workspaceservice";
import type { HealthRow, OverrideSite } from "@services/internal/views/models";
import WorkspaceToolBar from "../components/WorkspaceToolBar.vue";
import LanguageHealthStrip from "../components/LanguageHealthStrip.vue";
import DetailPane from "../components/DetailPane.vue";
import OriginBadge, { PILL_UI } from "../components/OriginBadge.vue";
import OriginSelectMenu from "../components/OriginSelectMenu.vue";
import ScriptSnippet from "../components/ScriptSnippet.vue";
import { useLiveEnabled } from "../composables/useLiveEnabled";
import { useWorkspaceStore } from "../stores/workspace";
import { originHex, originHexByOriginId } from "../ide/rootDecorations";

defineOptions({ name: "HealthPage" });

/** KPI / table scope. Matches Go HealthRow.type. */
type HealthFilter =
  | "conflict"
  | "override"
  | "depends"
  | "dangling"
  | "missing"
  | "orphaned"
  | "untranslated";

type BadgeColor =
  | "error"
  | "primary"
  | "secondary"
  | "tertiary"
  | "success"
  | "info"
  | "warning"
  | "neutral";

/** One color per finding type (KPI chips, table tags, detail badge). */
const TYPE_COLOR: Record<HealthFilter, BadgeColor> = {
  conflict: "warning",
  override: "secondary",
  depends: "info",
  dangling: "error",
  missing: "primary",
  orphaned: "success",
  untranslated: "tertiary",
};

const LOC_TYPES = new Set<HealthFilter>(["missing", "orphaned", "untranslated"]);

const KPI_PILL = {
  base: "rounded-sm px-1.5 py-0.5 text-xs leading-none font-normal",
} as const;

const KPI_CARD = { body: "p-1.5 sm:p-1.5" } as const;

/** Nested dashboard: cancel fixed inset-0 / min-h-svh so KPIs stay visible. */
const DASH_GROUP_UI = {
  base: "relative inset-auto flex min-h-0 min-w-0 flex-1 overflow-hidden",
} as const;

const DASH_PANEL_UI = {
  root: "min-h-0",
  body: "gap-0 overflow-hidden p-0 sm:p-0",
} as const;

const TABLE_UI = {
  root: "relative h-full min-h-0 overflow-auto",
  base: "min-w-full overflow-visible",
  th: "px-2 py-1.5 text-xs",
  td: "px-2 py-1.5 text-sm",
} as const;

const route = useRoute();
const ws = useWorkspaceStore();
const workspaceId = computed(() => String(route.params.id ?? ""));
const live = useLiveEnabled(workspaceId);
const originFallback = computed(() =>
  ws.gameName(ws.activeWorkspace?.gameId ?? ws.currentGameId),
);
const vanilla = computed(() => ws.originVanilla);
const selectedFilter = shallowRef<HealthFilter | null>(null);
const locLang = shallowRef("");

/**
 * The persisted order, as Go stored it. cloneWorkspace runs sortWorkspaceMods
 * before ListWorkspaces returns, so this list is already in load order.
 *
 * It must not be re-sorted here, and this is not cosmetic: the array is both
 * what GetHealth receives and what orderDirty compares against, and load order
 * is the FIOS/LIOS input. Re-sorting with localeCompare against Go's
 * strings.Compare — byte order, which disagrees on case — made Save look dirty
 * on load and handed Go a different winner than the one it had persisted.
 */
const persistedOrder = computed(() => ws.workspaceMods.map((m) => m.id));

/** Local chip order passed to GetHealth. Not persisted until Save. */
const previewOrder = ref<string[]>([]);

watch(
  persistedOrder,
  (ids) => {
    const cur = previewOrder.value;
    const sameSet = cur.length === ids.length && cur.every((id) => ids.includes(id));
    if (!sameSet) previewOrder.value = [...ids];
  },
  { immediate: true },
);

const orderDirty = computed(() => {
  const a = previewOrder.value;
  const b = persistedOrder.value;
  return a.length !== b.length || a.some((id, i) => id !== b[i]);
});

const chipsEl = useTemplateRef<HTMLElement>("chips");
useSortable(chipsEl, previewOrder, {
  handle: ".mod-handle",
  animation: 150,
  watchElement: true,
  onEnd: () => {
    previewOrder.value = previewOrder.value.slice();
  },
});

const originItems = computed(() =>
  persistedOrder.value.flatMap((id, i) => {
    const m = ws.workspaceMods.find((mod) => mod.id === id);
    if (!m || m.isBroken || !m.path) return [];
    return [
      {
        id: m.id,
        label: m.name,
        color: originHex({ kind: "mod", path: m.path, color: m.color, wrapIndex: i }),
        thumbnail: ws.thumbUrls[m.id],
      },
    ];
  }),
);
const originIds = ref<string[]>([]);

const loadOrderCopy = computed(() => {
  const extra = ws.firstWins(ws.activeWorkspace?.gameId ?? ws.currentGameId);
  return extra
    ? `Last listed wins (LIOS). ${extra} Drag to preview. Save writes workspace order.`
    : "Last listed wins (LIOS). Drag to preview. Save writes workspace order.";
});

const {
  data: report,
  error: loadError,
  isPending,
} = useQuery({
  key: () => ["session", "health", workspaceId.value, previewOrder.value],
  query: () => GetHealth(workspaceId.value, previewOrder.value),
  enabled: () => live.value,
});

watch(
  () => [report.value?.languages, ws.activeWorkspace?.defaultLocLang] as const,
  ([langs, def]) => {
    const list = langs ?? [];
    if (!list.length) return;
    if (locLang.value && list.some((l) => l.language === locLang.value)) return;
    const seed = def || "english";
    locLang.value = list.some((l) => l.language === seed)
      ? seed
      : list[0]!.language;
  },
  { immediate: true },
);

const locStats = computed(
  () =>
    (report.value?.languages ?? []).find((l) => l.language === locLang.value) ??
    null,
);

const locLangItems = computed(() =>
  (report.value?.languages ?? []).map((l) => ({
    label: `${l.language} (${l.defined})`,
    value: l.language,
  })),
);

const locActive = computed(
  () => selectedFilter.value != null && LOC_TYPES.has(selectedFilter.value),
);

const scopedAll = computed(() =>
  (report.value?.rows ?? []).filter(
    (r) => !isLocType(r.type) || r.language === locLang.value,
  ),
);

const rows = computed(() => {
  const all = scopedAll.value;
  const f = selectedFilter.value;
  if (!f) return all;
  return all.filter((r) => r.type === f);
});

const error = computed(() => loadError.value?.message ?? "");

const compatKpis = computed(() => {
  const r = report.value;
  return [
    {
      type: null as HealthFilter | null,
      label: "All",
      count: scopedAll.value.length,
      color: "neutral" as const,
      variant: "outline" as const,
    },
    {
      type: "conflict" as const,
      label: "Conflicts",
      count: r?.conflicts ?? 0,
      color: TYPE_COLOR.conflict,
    },
    {
      type: "override" as const,
      label: "Overrides",
      count: r?.overrides ?? 0,
      color: TYPE_COLOR.override,
    },
    {
      type: "depends" as const,
      label: "Depends",
      count: r?.depends ?? 0,
      color: TYPE_COLOR.depends,
    },
    {
      type: "dangling" as const,
      label: "Dangling",
      count: r?.dangling ?? 0,
      color: TYPE_COLOR.dangling,
    },
  ];
});

const locKpis = computed(() => {
  const s = locStats.value;
  return [
    {
      type: "missing" as const,
      label: "Missing",
      count: s?.missing ?? 0,
      color: TYPE_COLOR.missing,
    },
    {
      type: "orphaned" as const,
      label: "Orphans",
      count: s?.orphaned ?? 0,
      color: TYPE_COLOR.orphaned,
    },
    {
      type: "untranslated" as const,
      label: "Untranslated",
      count: s?.untranslated ?? 0,
      color: TYPE_COLOR.untranslated,
    },
  ];
});

const compatBlurb = computed(() => {
  switch (selectedFilter.value) {
    case "conflict":
      return "Keys defined in more than one workspace mod. The winner is who actually loads.";
    case "override":
      return "A workspace mod shadows a game-file definition.";
    case "depends":
      return "A workspace mod references a definition that only exists in another mod.";
    case "dangling":
      return "Missing definitions in script that do not resolve to a harvested object.";
    case "missing":
    case "orphaned":
    case "untranslated":
      return "FIOS/LIOS contests, game-file overrides, cross-mod depends, and dangling refs.";
    case null:
      return "Every compatibility and localization finding. Pick a card to filter.";
    default: {
      const _exhaustive: never = selectedFilter.value;
      return _exhaustive;
    }
  }
});

const locBlurb = computed(() => {
  switch (selectedFilter.value) {
    case "missing":
      return "Script cites a loc key that this language does not define (inherit still counts).";
    case "orphaned":
      return "Loc keys in this language that script never cites.";
    case "untranslated":
      return "This language copies english or leaves the value blank.";
    default:
      return "Missing, orphans, and untranslated keys for the selected language.";
  }
});

const emptyCopy = computed(() => {
  switch (selectedFilter.value) {
    case "conflict":
      return "No overlapping definitions across workspace mods.";
    case "override":
      return "No game-file overrides.";
    case "depends":
      return "No cross-mod dependencies.";
    case "dangling":
      return "No dangling references.";
    case "missing":
      return "No missing loc keys for this language.";
    case "orphaned":
      return "No orphan loc keys for this language.";
    case "untranslated":
      return "No untranslated loc keys for this language.";
    case null:
      return "No health findings for this filter.";
    default: {
      const _exhaustive: never = selectedFilter.value;
      return _exhaustive;
    }
  }
});

const showContestCols = computed(
  () =>
    selectedFilter.value === "conflict" || selectedFilter.value === "override",
);

const columnFilters = shallowRef<ColumnFiltersState>([]);
const sorting = shallowRef<SortingState>([]);
const selected = shallowRef<HealthRow | null>(null);
const activeSite = shallowRef<OverrideSite | null>(null);
const rowSelection = shallowRef<RowSelectionState>({});
const table = useTemplateRef<{
  tableApi?: {
    getColumn: (id: string) =>
      | {
          getFilterValue: () => unknown;
          setFilterValue: (v: unknown) => void;
          getFacetedUniqueValues: () => Map<unknown, number>;
        }
      | undefined;
  };
}>("table");

/** True when type is a loc issue kind. */
function isLocType(type: string): boolean {
  return type === "missing" || type === "orphaned" || type === "untranslated";
}

/** Mod origin ids on a contest row (no vanilla). */
function modOriginIds(row: HealthRow): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const s of row.sites ?? []) {
    const origin = s.origin || "";
    if (!origin || origin === vanilla.value) continue;
    if (seen.has(origin)) continue;
    seen.add(origin);
    out.push(origin);
  }
  return out;
}

/** Origin ids used by the shared origin column filter. */
function originIdsOf(row: HealthRow): string[] {
  if (isLocType(row.type)) {
    return row.origin ? [row.origin] : row.from ? [row.from] : [];
  }
  switch (row.type) {
    case "depends":
    case "dangling":
      return row.from ? [row.from] : [];
    default:
      return modOriginIds(row);
  }
}

const originColumn: TableColumn<HealthRow> = {
  id: "origin",
  header: "Origin",
  accessorFn: (row) => originIdsOf(row),
  getUniqueValues: (row) => originIdsOf(row),
  filterFn: (row, id, value) => {
    if (!Array.isArray(value) || !value.length) return true;
    return (row.getValue(id) as string[]).some((o) => value.includes(o));
  },
};

const columns = computed<TableColumn<HealthRow>[]>(() => {
  const keyCol: TableColumn<HealthRow> = {
    accessorKey: "name",
    header: "Key",
    filterFn: "includesString",
  };
  const kindCol: TableColumn<HealthRow> = {
    accessorKey: "kind",
    header: "Kind",
    filterFn: "equals",
  };
  const refsCol: TableColumn<HealthRow> = { accessorKey: "refs", header: "Refs" };
  const typeCol: TableColumn<HealthRow> = {
    accessorKey: "type",
    header: "Type",
    filterFn: "equals",
  };
  switch (selectedFilter.value) {
    case "conflict":
    case "override":
      return [
        kindCol,
        { accessorKey: "name", header: "Name", filterFn: "includesString" },
        { accessorKey: "rule", header: "Rule", filterFn: "equals" },
        originColumn,
        { id: "winner", header: "Winner" },
        refsCol,
      ];
    case "depends":
      return [
        { accessorKey: "fromName", header: "From" },
        keyCol,
        kindCol,
        { accessorKey: "toName", header: "To" },
        refsCol,
      ];
    case "dangling":
      return [keyCol, kindCol, originColumn, refsCol];
    case "missing":
    case "orphaned":
    case "untranslated":
      return [
        typeCol,
        keyCol,
        originColumn,
        refsCol,
        { accessorKey: "language", header: "Language", filterFn: "equals" },
      ];
    case null:
      return [typeCol, keyCol, kindCol, originColumn, refsCol];
    default: {
      const _exhaustive: never = selectedFilter.value;
      return _exhaustive;
    }
  }
});

function facetItems(id: string): { label: string; value: string }[] {
  const col = table.value?.tableApi?.getColumn(id);
  return [...(col?.getFacetedUniqueValues() ?? new Map())].map(([v, n]) => ({
    label: `${String(v)} (${n})`,
    value: String(v),
  }));
}
function setCol(id: string, value: unknown): void {
  const rest = columnFilters.value.filter((f) => f.id !== id);
  columnFilters.value =
    value == null || value === "" ? rest : [...rest, { id, value }];
}
function colString(id: string): string | undefined {
  const hit = columnFilters.value.find((f) => f.id === id);
  return hit?.value == null ? undefined : String(hit.value);
}
watch(originIds, (ids) => {
  const narrowed = ids.length > 0 && ids.length < originItems.value.length;
  setCol("origin", narrowed ? ids : undefined);
});

watch(selectedFilter, () => {
  selected.value = null;
  activeSite.value = null;
  rowSelection.value = {};
  if (!showContestCols.value) {
    columnFilters.value = columnFilters.value.filter((f) => f.id !== "rule");
  }
});

watch(locLang, () => {
  if (locActive.value || selectedFilter.value == null) {
    selected.value = null;
    activeSite.value = null;
    rowSelection.value = {};
  }
});

/** Stable row id for selection across preview-order refetches. */
function rowKey(row: HealthRow): string {
  return `${row.type}:${row.kind ?? ""}:${row.name}:${row.from ?? ""}:${row.origin ?? ""}:${row.language ?? ""}`;
}

/** Toggle a KPI filter; click again for overview. */
function selectFilter(t: HealthFilter | null): void {
  selectedFilter.value = selectedFilter.value === t ? null : t;
}

const { mutateAsync: saveOrder, isLoading: savingOrder } = useMutation({
  mutation: async () => {
    await ReorderWorkspaceMods(workspaceId.value, previewOrder.value);
    await ws.refresh();
  },
});

const locKey = computed(() => selected.value?.name ?? "");
const { data: loc } = useQuery({
  key: () => ["session", "loc", workspaceId.value, locKey.value],
  query: () => LookupLoc(workspaceId.value, locKey.value),
  enabled: () => live.value && !!locKey.value && !locActive.value,
});

/** Winning or first site for the detail header. */
const headerSite = computed((): OverrideSite | undefined => {
  if (activeSite.value) return activeSite.value;
  const row = selected.value;
  if (!row) return undefined;
  if (row.type === "conflict" || row.type === "override") {
    return (row.sites ?? []).find((s) => s.origin === row.winner) ?? row.sites?.[0];
  }
  return row.sites?.[0];
});

const snippetLang = computed(() =>
  selected.value && isLocType(selected.value.type) ? "paradox-loc" : "paradox",
);

/** KPI color for a row type tag. */
function typeColor(type: string): BadgeColor {
  if (Object.hasOwn(TYPE_COLOR, type)) {
    return TYPE_COLOR[type as HealthFilter];
  }
  return "neutral";
}

/** Select one row for the detail pane. */
function onRowSelect(_e: Event, row: { original: HealthRow; id: string }): void {
  selected.value = row.original;
  activeSite.value = row.original.sites?.[0] ?? null;
  rowSelection.value = { [row.id]: true };
}

function chipLabel(id: string): string {
  return ws.workspaceMods.find((m) => m.id === id)?.name ?? id;
}

/** Thumb URL for an origin id. */
function originThumb(id: string): string | undefined {
  return ws.thumbUrls[id];
}

/** Whether this KPI card is the active filter (All when none). */
function kpiOn(type: HealthFilter | null): boolean {
  return selectedFilter.value === type;
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <WorkspaceToolBar :workspace-id="workspaceId">
      <template #trailing>
        <LanguageHealthStrip v-if="workspaceId" :workspace-id="workspaceId" />
      </template>
    </WorkspaceToolBar>
    <UAlert v-if="error" color="error" variant="subtle" :description="error" class="m-2" />

    <div class="grid shrink-0 grid-cols-2 items-stretch px-2 pt-2">
      <section class="flex min-w-0 flex-col border-e border-default pe-3">
        <div class="flex min-h-9 shrink-0 items-center">
          <h2 class="text-lg font-semibold">Compatibility</h2>
        </div>
        <p class="mb-1.5 line-clamp-2 min-h-10 text-sm leading-snug text-muted">
          {{ compatBlurb }}
        </p>
        <div class="grid grid-cols-5 gap-1.5">
          <div
            v-for="kpi in compatKpis"
            :key="kpi.label"
            role="button"
            tabindex="0"
            class="min-w-0 cursor-pointer rounded-lg"
            :class="kpiOn(kpi.type) ? 'ring-2 ring-primary' : ''"
            @click="selectFilter(kpi.type)"
            @keydown.enter.prevent="selectFilter(kpi.type)"
          >
            <UCard variant="subtle" :ui="KPI_CARD">
              <div class="flex min-w-0 items-center justify-between gap-1 whitespace-nowrap">
                <UBadge
                  :label="kpi.label"
                  :color="kpi.color"
                  :variant="kpi.variant ?? 'subtle'"
                  size="sm"
                  :ui="KPI_PILL"
                  class="min-w-0 truncate"
                />
                <span class="shrink-0 text-lg font-semibold tabular-nums">{{ kpi.count }}</span>
              </div>
            </UCard>
          </div>
        </div>
      </section>
      <section class="flex min-w-0 flex-col ps-3">
        <div class="flex min-h-9 shrink-0 items-center justify-between gap-2">
          <h2 class="text-lg font-semibold">Localization</h2>
          <USelect
            v-model="locLang"
            :items="locLangItems"
            value-key="value"
            size="xs"
            class="w-40 shrink-0"
          />
        </div>
        <p class="mb-1.5 line-clamp-2 min-h-10 text-sm leading-snug text-muted">
          {{ locBlurb }}
        </p>
        <div class="grid grid-cols-3 gap-1.5">
          <div
            v-for="kpi in locKpis"
            :key="kpi.type"
            role="button"
            tabindex="0"
            class="min-w-0 cursor-pointer rounded-lg"
            :class="kpiOn(kpi.type) ? 'ring-2 ring-primary' : ''"
            @click="selectFilter(kpi.type)"
            @keydown.enter.prevent="selectFilter(kpi.type)"
          >
            <UCard variant="subtle" :ui="KPI_CARD">
              <div class="flex min-w-0 items-center justify-between gap-1 whitespace-nowrap">
                <UBadge
                  :label="kpi.label"
                  :color="kpi.color"
                  variant="subtle"
                  size="sm"
                  :ui="KPI_PILL"
                  class="min-w-0 truncate"
                />
                <span class="shrink-0 text-lg font-semibold tabular-nums">{{ kpi.count }}</span>
              </div>
            </UCard>
          </div>
        </div>
      </section>
    </div>

    <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-default px-2 py-2">
      <div class="min-w-0 flex-1">
        <p class="text-xs font-semibold">Load order (drag to preview)</p>
        <p class="text-[11px] leading-snug text-muted">{{ loadOrderCopy }}</p>
        <ul ref="chips" class="mt-1 flex flex-wrap items-center gap-1">
          <li
            v-for="(id, i) in previewOrder"
            :key="id"
            class="flex items-center gap-1 rounded-md border border-default bg-elevated px-1 py-0.5"
          >
            <UButton
              icon="i-lucide-grip-vertical"
              color="neutral"
              variant="ghost"
              size="xs"
              class="mod-handle cursor-grab"
            />
            <span class="w-4 shrink-0 tabular-nums text-xs text-muted">{{ i + 1 }}</span>
            <img
              v-if="ws.thumbUrls[id]"
              :src="ws.thumbUrls[id]"
              alt=""
              class="size-4 shrink-0 rounded-sm object-cover"
            />
            <OriginBadge :label="chipLabel(id)" :hex="originHexByOriginId(id)" class="min-w-0" />
          </li>
        </ul>
      </div>
      <UButton
        label="Save as workspace order"
        size="sm"
        :disabled="!orderDirty"
        :loading="savingOrder"
        @click="saveOrder()"
      />
    </div>

    <UDashboardGroup
      storage="local"
      storage-key="pmt-health"
      :ui="DASH_GROUP_UI"
      class="min-h-0 min-w-0 flex-1 pb-2"
    >
      <UDashboardPanel
        id="health-main"
        resizable
        :default-size="70"
        :min-size="40"
        :max-size="85"
        :ui="DASH_PANEL_UI"
      >
        <UTable
          ref="table"
          class="h-full min-h-0"
          :ui="TABLE_UI"
          :column-filters="columnFilters"
          :sorting="sorting"
          :row-selection="rowSelection"
          :row-selection-options="{ enableMultiRowSelection: false }"
          :data="rows"
          :columns="columns"
          :loading="isPending"
          :faceted-options="{ getFacetedUniqueValues: getFacetedUniqueValues() }"
          sticky="header"
          :empty="emptyCopy"
          :get-row-id="rowKey"
          @update:column-filters="
            (v?: ColumnFiltersState) => {
              if (v) columnFilters = v;
            }
          "
          @update:sorting="
            (v?: SortingState) => {
              if (v) sorting = v;
            }
          "
          @select="onRowSelect"
        >
          <template #type-header>
            <div class="flex min-w-28 flex-col gap-1 py-0.5">
              <span class="text-xs">Type</span>
              <USelect
                :model-value="colString('type')"
                :items="facetItems('type')"
                placeholder="All"
                size="xs"
                @update:model-value="setCol('type', $event)"
              />
            </div>
          </template>
          <template #name-header>
            <div class="flex min-w-36 flex-col gap-1 py-0.5">
              <span class="text-xs">Key</span>
              <UInput
                :model-value="colString('name') ?? ''"
                placeholder="Contains"
                size="xs"
                @update:model-value="setCol('name', $event || undefined)"
              />
            </div>
          </template>
          <template #kind-header>
            <div class="flex min-w-28 flex-col gap-1 py-0.5">
              <span class="text-xs">Kind</span>
              <USelect
                :model-value="colString('kind')"
                :items="facetItems('kind')"
                placeholder="All"
                size="xs"
                @update:model-value="setCol('kind', $event)"
              />
            </div>
          </template>
          <template #rule-header>
            <div class="flex min-w-24 flex-col gap-1 py-0.5">
              <span class="text-xs">Rule</span>
              <USelect
                :model-value="colString('rule')"
                :items="facetItems('rule')"
                placeholder="All"
                size="xs"
                @update:model-value="setCol('rule', $event)"
              />
            </div>
          </template>
          <template #origin-header>
            <div class="flex min-w-40 flex-col gap-1 py-0.5">
              <span class="text-xs">Origin</span>
              <OriginSelectMenu v-model="originIds" :items="originItems" />
            </div>
          </template>
          <template #language-header>
            <div class="flex min-w-28 flex-col gap-1 py-0.5">
              <span class="text-xs">Language</span>
              <USelect
                :model-value="colString('language')"
                :items="facetItems('language')"
                placeholder="All"
                size="xs"
                @update:model-value="setCol('language', $event)"
              />
            </div>
          </template>
          <template #type-cell="{ row }">
            <UBadge
              :label="row.original.type"
              :color="typeColor(row.original.type)"
              variant="subtle"
              size="xs"
              :ui="PILL_UI"
            />
          </template>
          <template #origin-cell="{ row }">
            <div class="flex flex-wrap gap-1">
              <OriginBadge
                v-for="oid in row.getValue('origin') as string[]"
                :key="oid"
                :label="
                  row.original.sites?.find((s) => s.origin === oid)?.originName ||
                  row.original.originName ||
                  row.original.fromName ||
                  oid
                "
                :hex="originHexByOriginId(oid)"
                :thumb="originThumb(oid)"
              />
            </div>
          </template>
          <template #rule-cell="{ row }">
            <UBadge
              :label="row.original.rule"
              :color="row.original.rule === 'FIOS' ? 'warning' : 'info'"
              variant="subtle"
              size="xs"
              :ui="PILL_UI"
            />
          </template>
          <template #winner-cell="{ row }">
            <OriginBadge
              :label="row.original.winnerName || originFallback"
              :hex="originHexByOriginId(row.original.winner ?? '')"
              :thumb="originThumb(row.original.winner ?? '')"
            />
          </template>
          <template #fromName-cell="{ row }">
            <OriginBadge
              :label="row.original.fromName || originFallback"
              :hex="originHexByOriginId(row.original.from ?? '')"
              :thumb="originThumb(row.original.from ?? '')"
            />
          </template>
          <template #toName-cell="{ row }">
            <OriginBadge
              :label="row.original.toName || originFallback"
              :hex="originHexByOriginId(row.original.to ?? '')"
              :thumb="originThumb(row.original.to ?? '')"
            />
          </template>
        </UTable>
      </UDashboardPanel>
      <UDashboardPanel id="health-detail" :ui="DASH_PANEL_UI">
        <DetailPane
          :workspace-id="workspaceId"
          :title="selected?.name"
          :file="headerSite?.path"
          :rel="headerSite?.rel"
          :line="headerSite?.line"
        >
          <template v-if="selected" #origin>
            <OriginBadge
              v-if="selected.type === 'conflict' || selected.type === 'override'"
              :label="selected.winnerName || originFallback"
              :hex="originHexByOriginId(selected.winner ?? '')"
              :thumb="originThumb(selected.winner ?? '')"
            />
            <template v-else-if="selected.type === 'depends'">
              <OriginBadge
                :label="selected.fromName || originFallback"
                :hex="originHexByOriginId(selected.from ?? '')"
                :thumb="originThumb(selected.from ?? '')"
              />
              <span class="text-xs text-muted">→</span>
              <OriginBadge
                :label="selected.toName || originFallback"
                :hex="originHexByOriginId(selected.to ?? '')"
                :thumb="originThumb(selected.to ?? '')"
              />
            </template>
            <OriginBadge
              v-else
              :label="selected.originName || selected.fromName || originFallback"
              :hex="originHexByOriginId(selected.origin || selected.from || '')"
              :thumb="originThumb(selected.origin || selected.from || '')"
            />
          </template>
          <template v-if="selected" #badges>
            <UBadge
              :label="selected.type"
              :color="typeColor(selected.type)"
              variant="subtle"
              size="md"
              :ui="KPI_PILL"
            />
            <UBadge
              v-if="selected.rule"
              :label="selected.rule"
              :color="selected.rule === 'FIOS' ? 'warning' : 'info'"
              variant="subtle"
              size="xs"
              :ui="PILL_UI"
            />
            <UBadge
              v-if="selected.kind"
              :label="selected.kind"
              color="neutral"
              variant="subtle"
              size="xs"
              :ui="PILL_UI"
            />
          </template>
          <div class="flex flex-col gap-2">
            <p v-if="loc?.text" class="text-sm italic text-muted">{{ loc.text }}</p>
            <ScriptSnippet
              v-if="headerSite?.snippet"
              :text="headerSite.snippet"
              :language="snippetLang"
              :from-line="headerSite.snippetFrom"
              :hit-line="headerSite.line"
              gutter
            />
            <UButton
              v-for="(site, i) in selected?.sites ?? []"
              :key="i"
              :label="`${site.originName || originFallback} · ${site.rel || site.path}:${site.line + 1}`"
              size="xs"
              :color="site === headerSite ? 'primary' : 'neutral'"
              variant="subtle"
              class="justify-start"
              @click="activeSite = site"
            />
          </div>
        </DetailPane>
      </UDashboardPanel>
    </UDashboardGroup>
  </div>
</template>
