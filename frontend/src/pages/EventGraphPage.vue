<script setup lang="ts">
/**
 * Event Graph (the EventGraph boards): what a root fires and what fires it, laid out left to right by step,
 * with the selected definition opened beside the graph or below it. Full screen keeps the graph and the
 * drawer, the toolbar floating over the graph.
 */
import { computed, useTemplateRef, watch } from "vue";
import { useRoute } from "vue-router";
import { useElementSize, useFullscreen, useLocalStorage } from "@vueuse/core";
import EventDetail from "../components/eventGraph/EventDetail.vue";
import EventGraphFilters from "../components/eventGraph/EventGraphFilters.vue";
import EventGraphToolbar from "../components/eventGraph/EventGraphToolbar.vue";
import GraphCanvas from "../components/eventGraph/GraphCanvas.vue";
import { useEventGraph } from "../composables/useEventGraph";
import { useLiveEnabled } from "../composables/useLiveEnabled";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";

const { id: workspaceId } = defineProps<{ id: string }>();

const route = useRoute();
const store = useWorkspaceStore();
const settings = useSettingsStore();
const live = useLiveEnabled(() => workspaceId);
const { params, hidden, selected, searchTerm, graph, error, detail, roots, searching, shown, laid, filtersOn, select, setRoot, resetFilters } =
  useEventGraph(() => workspaceId, live);
const canvas = useTemplateRef("canvas");
const page = useTemplateRef<HTMLElement>("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);
const { width: pageWidth } = useElementSize(page);

// where the detail opens, kept in the app's settings, and how big: beside the graph, up to half the page
const layout = computed({
  get: () => (settings.values["eventGraph.layout"] === "panel" ? "panel" : "drawer"),
  set: (value: "drawer" | "panel") => void settings.set("eventGraph.layout", value),
});
const drawerWidth = useLocalStorage("pmt.graph.drawerWidth", 420);
const panelHeight = useLocalStorage("pmt.graph.panelHeight", 320);
// full screen keeps the detail beside the graph
const below = computed(() => layout.value === "panel" && !isFullscreen.value);

const rootKind = computed(() => graph.value?.nodes?.find((node) => node.root)?.kind ?? "");
// each kind drawn and each origin, with how many nodes of it
const kinds = computed(() => {
  const counts = new Map<string, number>();
  for (const node of graph.value?.nodes ?? []) if (!node.stub) counts.set(node.kind, (counts.get(node.kind) ?? 0) + 1);
  return [...counts].map(([kind, count]) => ({ kind, count }));
});
const origins = computed(() =>
  [store.originVanilla, ...store.workspaceMods.map((mod) => mod.id)].map((id) => ({
    id,
    label: store.originName(id),
    color: store.originColor(id),
    count: (graph.value?.nodes ?? []).filter((node) => (node.origin ?? "") === id && !node.stub).length,
  })),
);
const folded = computed(() => shown.value.nodes.filter((node) => node.stub).length);

// the IDE's "Open in Event Graph" routes here with ?root=; another workspace starts empty
watch(
  () => route.query.root,
  (root) => typeof root === "string" && root && setRoot(root),
  { immediate: true },
);
watch(() => workspaceId, () => setRoot(""));
</script>

<template>
  <div ref="page" class="relative flex h-full min-h-0 flex-col overflow-hidden bg-default">
    <!-- one toolbar: above the graph, or floating over it in full screen -->
    <EventGraphToolbar
      v-model:search-term="searchTerm"
      v-model:follow="params.follow"
      v-model:layout="layout"
      :full="isFullscreen"
      :class="{ 'absolute top-3 left-4 z-10': isFullscreen }"
      :root="params.root"
      :roots="roots"
      :searching="searching"
      :root-kind="rootKind"
      :nodes="shown.nodes.length"
      @update:root="setRoot"
      @fit="canvas?.fit()"
      @collapse="params.expand = []"
      @fullscreen="toggleFullscreen()"
    >
      <template #filters>
        <EventGraphFilters
          v-model:hidden="hidden"
          v-model:scripted="params.scripted"
          v-model:shown-origins="params.origins"
          v-model:depth="params.depth"
          v-model:max-nodes="params.maxNodes"
          :kinds="kinds"
          :origins="origins"
          :count="filtersOn"
          @reset="resetFilters"
        />
      </template>
    </EventGraphToolbar>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
    <div class="flex min-h-0 flex-1" :class="below ? 'flex-col' : 'flex-row'">
      <div class="relative min-h-0 min-w-0 flex-1">
        <GraphCanvas
          ref="canvas"
          :nodes="shown.nodes"
          :edges="shown.edges"
          :positions="laid.positions"
          :heads="laid.heads"
          :root-id="params.root"
          :selected-id="selected"
          :depth="params.depth"
          :full="isFullscreen"
          @select="select"
          @reroot="setRoot"
          @clear="selected = ''"
        >
          <p v-if="graph?.reason" class="pointer-events-none absolute inset-0 grid place-items-center text-13 text-dimmed">
            {{ graph.reason }}
          </p>
        </GraphCanvas>
      </div>
      <template v-if="selected && detail">
        <ResizeGrip v-if="below" v-model="panelHeight" axis="y" reverse :min="180" :max="640" :default-size="320" />
        <ResizeGrip v-else v-model="drawerWidth" reverse :min="320" :max="Math.max(420, pageWidth / 2)" :default-size="420" />
        <EventDetail
          :workspace-id="workspaceId"
          :detail="detail"
          :is-root="detail.name === params.root"
          class="shrink-0 border-default"
          :class="below ? 'border-t' : 'border-s'"
          :style="below ? { height: `${panelHeight}px` } : { width: `${drawerWidth}px` }"
          @close="selected = ''"
          @root="setRoot"
        />
      </template>
    </div>
    <StatusFacts>
      <span>
        {{ shown.nodes.length }} {{ shown.nodes.length === 1 ? "node" : "nodes" }}{{ graph?.truncated ? `, stopped at the node limit at step ${graph.reached}` : "" }},
        {{ folded }} {{ folded === 1 ? "group" : "groups" }} folded
      </span>
      <span>Laid out left to right by step</span>
    </StatusFacts>
  </div>
</template>
