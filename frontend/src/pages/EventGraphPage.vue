<script setup lang="ts">
/**
 * Event graph page: GetEventGraph + GetEventDetail. Layout is dagre in GraphCanvas.
 */
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useQuery } from "@pinia/colada";
import { refDebounced, useLocalStorage } from "@vueuse/core";
import { GetEventDetail, GetEventGraph } from "@services/viewsservice";
import { WorkspaceSymbols } from "@services/ideservice";
import type { EventGraphParams } from "@services/internal/views/models";
import WorkspaceToolBar from "../components/WorkspaceToolBar.vue";
import LanguageHealthStrip from "../components/LanguageHealthStrip.vue";
import GraphCanvas from "../components/graph/GraphCanvas.vue";
import EventDetailPanel from "../components/graph/EventDetailPanel.vue";
import OriginSelectMenu from "../components/OriginSelectMenu.vue";
import { PILL_UI } from "../components/OriginBadge.vue";
import { useLiveEnabled } from "../composables/useLiveEnabled";
import type { GraphLayoutMode } from "../composables/useGraphLayout";
import { originHex } from "../ide/rootDecorations";
import { useWorkspaceStore } from "../stores/workspace";

defineOptions({ name: "EventGraphPage" });

const route = useRoute();
const ws = useWorkspaceStore();
const vanilla = computed(() => ws.originVanilla);

const liveMods = computed(() => ws.workspaceMods.filter((m) => !m.isBroken && m.path));
const originItems = computed(() => [
  {
    id: vanilla.value,
    label: ws.gameName(ws.activeWorkspace?.gameId ?? ws.currentGameId),
    color: originHex({
      kind: "game",
      path: "",
      color: ws.activeWorkspace?.gameColor,
    }),
    gameId: ws.activeWorkspace?.gameId ?? ws.currentGameId,
  },
  ...liveMods.value.map((m, i) => ({
    id: m.id,
    label: m.name,
    color: originHex({
      kind: "mod",
      path: m.path,
      color: m.color,
      wrapIndex: i,
    }),
    thumbnail: ws.thumbUrls[m.id],
  })),
]);
const origins = ref<string[]>([]);
const originsNarrowed = computed(() => origins.value.length > 0 && origins.value.length < originItems.value.length);

const workspaceId = computed(() => String(route.params.id ?? ""));
const live = useLiveEnabled(workspaceId);
const selectedId = ref("");
const root = ref<string | undefined>();
const namespace = ref<string | undefined>();
const expand = ref<string[]>([]);
const layout = useLocalStorage<GraphLayoutMode>("pmt.graph.layout", "lr");
const graphCanvas = ref<{ recenter: () => Promise<void> } | null>(null);

const layoutItems = [
  { label: "After", value: "lr", icon: "i-lucide-arrow-right" },
  { label: "Tree", value: "tree", icon: "i-lucide-waypoints" },
];

const graphParams = computed((): EventGraphParams => {
  const p: EventGraphParams = {};
  if (root.value) p.root = root.value;
  if (namespace.value) p.namespace = namespace.value;
  if (originsNarrowed.value) p.origins = origins.value;
  if (expand.value.length) p.expand = expand.value;
  return p;
});

const { data: graph, error: graphError } = useQuery({
  key: () => ["session", "graph", workspaceId.value, graphParams.value],
  query: () => GetEventGraph(workspaceId.value, graphParams.value),
  enabled: () => live.value,
  placeholderData: (prev) => prev,
});

const { data: detailData } = useQuery({
  key: () => ["session", "event-detail", workspaceId.value, selectedId.value],
  query: () => GetEventDetail(workspaceId.value, selectedId.value),
  enabled: () => live.value && !!selectedId.value,
});

const rootPickerOpen = ref(false);
const symbolTerm = ref("");
const symbolQuery = refDebounced(symbolTerm, 200);

const { data: symbolData, isLoading: symbolsLoading } = useQuery({
  key: () => ["session", "symbols", workspaceId.value, symbolQuery.value],
  query: () => WorkspaceSymbols(workspaceId.value, symbolQuery.value),
  enabled: () => live.value && symbolQuery.value.length > 0,
  placeholderData: (prev) => prev,
});

/** WorkspaceSymbols already ranked and capped the hits, so the palette must not refilter. */
const rootGroups = computed(() => [
  {
    id: "symbols",
    ignoreFilter: true,
    items: (symbolData.value ?? []).map((sym) => ({
      label: sym.name,
      suffix: sym.containerName,
      onSelect: () => {
        setRoot(sym.name);
        rootPickerOpen.value = false;
      },
    })),
  },
]);

const detail = computed(() => (selectedId.value ? detailData.value : null));
const error = computed(() => graphError.value?.message ?? "");

/** Click: select; more-stubs append their parent to Expand. */
function onSelect(id: string): void {
  if (id.startsWith("more-in:") || id.startsWith("more:")) {
    const parent = id.startsWith("more-in:") ? id.slice(8) : id.slice(5);
    if (parent && !expand.value.includes(parent)) {
      expand.value = [...expand.value, parent];
    }
    return;
  }
  selectedId.value = id;
}

/** Double-click or event chip: re-root; align namespace so load keeps this id. */
function onReroot(id: string): void {
  if (id.startsWith("more:") || id.startsWith("more-in:")) {
    onSelect(id);
    return;
  }
  const dot = id.indexOf(".");
  const ns = dot > 0 ? id.slice(0, dot) : "";
  if (ns && namespace.value && namespace.value !== ns) {
    namespace.value = ns;
  }
  root.value = id;
  selectedId.value = id;
}

function onRecenter(): void {
  void graphCanvas.value?.recenter();
}
function onShowMore(): void {
  const id = selectedId.value;
  if (!id || id.startsWith("more:")) return;
  if (!expand.value.includes(id)) expand.value = [...expand.value, id];
}
function setLayout(v: string): void {
  if (v === "lr" || v === "tree") layout.value = v;
}
function setRoot(v: string | null | undefined): void {
  if (v) root.value = v;
}
function clearRoot(): void {
  root.value = undefined;
  namespace.value = undefined;
  selectedId.value = "";
}

watch(namespace, (ns) => {
  if (ns && root.value && !root.value.startsWith(`${ns}.`)) {
    root.value = undefined;
  }
  expand.value = [];
});
watch(root, () => {
  expand.value = [];
});
watch(workspaceId, () => {
  root.value = undefined;
  namespace.value = undefined;
  selectedId.value = "";
  expand.value = [];
  applyGraphTargetFromQuery();
});
watch(
  () => [queryStr(route.query.root), queryStr(route.query.namespace)] as const,
  () => applyGraphTargetFromQuery(),
  { immediate: true },
);

/** String query param, or empty when missing / repeated. */
function queryStr(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** IDE hover deep-link: always set namespace from the id prefix. */
function applyGraphTargetFromQuery(): void {
  const rootId = queryStr(route.query.root);
  if (!rootId) return;
  const qns = queryStr(route.query.namespace);
  const dot = rootId.indexOf(".");
  const ns = qns || (dot > 0 ? rootId.slice(0, dot) : "");
  namespace.value = ns || undefined;
  root.value = rootId;
  selectedId.value = rootId;
  expand.value = [];
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <WorkspaceToolBar :workspace-id="workspaceId">
      <template #trailing>
        <USelect
          :model-value="layout"
          :items="layoutItems"
          value-key="value"
          size="md"
          class="w-28"
          @update:model-value="setLayout"
        />
        <LanguageHealthStrip v-if="workspaceId" :workspace-id="workspaceId" />
      </template>
    </WorkspaceToolBar>

    <UDashboardToolbar class="px-2 sm:px-2">
      <template #left>
        <UModal
          v-model:open="rootPickerOpen"
          title="Root event"
          description="Search the workspace for the event, on_action or decision to draw from."
          :ui="{ content: 'sm:max-w-xl' }"
        >
          <UButton
            :label="root ?? 'Root event'"
            icon="i-lucide-search"
            size="md"
            color="neutral"
            variant="outline"
            class="min-w-64 flex-1 justify-start"
          />
          <template #body>
            <UCommandPalette
              v-model:search-term="symbolTerm"
              :groups="rootGroups"
              :loading="symbolsLoading"
              placeholder="Search events, on_actions, decisions…"
              class="h-80"
            />
          </template>
        </UModal>
        <OriginSelectMenu v-model="origins" :items="originItems" />
        <UButton
          label="Clear"
          icon="i-lucide-x"
          size="md"
          color="neutral"
          variant="outline"
          :disabled="!root && !namespace"
          @click="clearRoot"
        />
        <UButton
          label="Recenter"
          icon="i-lucide-locate-fixed"
          size="md"
          color="neutral"
          variant="soft"
          :disabled="!root"
          @click="onRecenter"
        />
        <UButton
          v-if="graph?.truncated"
          label="Show more"
          size="md"
          color="neutral"
          variant="ghost"
          :disabled="!selectedId || selectedId.startsWith('more:') || selectedId.startsWith('more-in:')"
          @click="onShowMore"
        />
        <span v-if="root && graph" class="text-xs text-muted"> {{ graph.nodes?.length ?? 0 }} nodes </span>
        <UBadge v-if="graph?.truncated" color="warning" variant="subtle" size="xs" :ui="PILL_UI"> truncated </UBadge>
        <span v-if="graph?.truncated" class="text-xs text-muted">
          Double-click a node at the edge to walk further.
        </span>
        <span v-if="!root && graph?.emptyReason" class="text-xs text-muted">
          {{ graph.emptyReason }}
        </span>
      </template>
    </UDashboardToolbar>

    <UAlert v-if="error" color="error" variant="subtle" :description="error" class="m-2" />

    <UDashboardGroup
      storage="local"
      storage-key="pmt-event-graph"
      class="relative! inset-auto! min-h-0 min-w-0 flex-1 overflow-hidden"
    >
      <UDashboardPanel
        id="event-graph-main"
        resizable
        :default-size="75"
        :min-size="50"
        :max-size="85"
        class="min-h-0!"
      >
        <GraphCanvas
          ref="graphCanvas"
          class="h-full min-h-0 w-full"
          :nodes="graph?.nodes ?? []"
          :edges="graph?.edges ?? []"
          :layout="layout"
          :selected-id="selectedId"
          :root-id="root"
          @select="onSelect"
          @reroot="onReroot"
          @clear-selection="selectedId = ''"
        />
      </UDashboardPanel>
      <UDashboardPanel id="event-graph-detail" class="min-h-0!">
        <EventDetailPanel :workspace-id="workspaceId" :detail="detail ?? null" @reroot="onReroot" />
      </UDashboardPanel>
    </UDashboardGroup>
  </div>
</template>
