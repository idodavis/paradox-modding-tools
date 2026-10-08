<script setup lang="ts">
/**
 * Event Graph (the EventGraph boards): what a root fires and what fires it, laid out left to right by step,
 * with the selected definition opened beside the graph or below it. Full screen keeps the graph and the
 * drawer, the toolbar floating over the graph.
 */
import { computed, useTemplateRef, watch } from "vue";
import { useRoute } from "vue-router";
import { useFullscreen, useLocalStorage } from "@vueuse/core";
import { useEventGraph } from "../composables/useEventGraph";
import { useLiveEnabled } from "../composables/useLiveEnabled";
import { useWorkspaceStore } from "../stores/workspace";
import { plural } from "../lib/format";

const { id: workspaceId } = defineProps<{ id: string }>();

const route = useRoute();
const store = useWorkspaceStore();
const live = useLiveEnabled(() => workspaceId);
const { params, hidden, selected, searchTerm, graph, error, detail, roots, searching, shown, laid, filtersOn, select, setRoot, resetFilters } =
  useEventGraph(() => workspaceId, live);
const canvas = useTemplateRef("canvas");
const page = useTemplateRef<HTMLElement>("page");
const { isFullscreen, toggle: toggleFullscreen } = useFullscreen(page);

// where the detail opens: beside the graph (the graph keeping at least half the page) or below it; full screen keeps
// it beside. The splitter keeps each one's size.
const layout = useLocalStorage<"drawer" | "panel">("pmt.graph.layout", "drawer");
const below = computed(() => layout.value === "panel" && !isFullscreen.value);
const panes = computed(() => [
  { id: "graph", slot: "graph", order: 1, minSize: below.value ? undefined : 50 },
  ...(selected.value && detail.value
    ? [{ id: "detail", slot: "detail", order: 2, sizeUnit: "px" as const, ...(below.value ? { defaultSize: 320, minSize: 180, maxSize: 640 } : { defaultSize: 420, minSize: 320 }) }]
    : []),
]);

const rootKind = computed(() => graph.value?.nodes?.find((node) => node.root)?.kind ?? "");
// each kind and each origin, with how many of the graph's reach are of it (Go counts with no fan-out or node cap)
const kinds = computed(() => Object.entries(graph.value?.kinds ?? {}).map(([kind, count]) => ({ kind, count: count ?? 0 })));
const origins = computed(() =>
  [store.originVanilla, ...store.workspaceMods.map((mod) => mod.id)].map((id) => ({
    id,
    label: store.originName(id),
    color: store.originColor(id),
    count: graph.value?.origins?.[id] ?? 0,
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
      :reach="graph?.reach ?? 0"
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
    <USplitter
      :key="below ? 'below' : 'beside'"
      :items="panes"
      :orientation="below ? 'vertical' : 'horizontal'"
      :auto-save-id="below ? 'pmt.graph.panel' : 'pmt.graph.drawer'"
      class="min-h-0 flex-1"
    >
      <template #graph>
      <div class="relative min-h-0 min-w-0 flex-1">
        <GraphCanvas
          ref="canvas"
          :nodes="shown.nodes"
          :edges="shown.edges"
          :positions="laid.positions"
          :routes="laid.routes"
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
      </template>
      <template #detail>
        <!-- a name nothing defines has its reason, not a definition, to show -->
        <p v-if="detail?.reason" class="grid min-h-0 min-w-0 flex-1 place-items-center border-default p-6 text-13 text-dimmed" :class="below ? 'border-t' : 'border-s'">
          {{ detail.reason }}
        </p>
        <EventDetail
          v-else-if="detail"
          :detail="detail"
          :is-root="detail.name === params.root"
          class="min-h-0 min-w-0 flex-1 border-default"
          :class="below ? 'border-t' : 'border-s'"
          @close="selected = ''"
          @root="setRoot"
        />
      </template>
    </USplitter>
    <StatusFacts>
      <span>
        {{ plural(shown.nodes.length, "node") }}{{ graph?.truncated ? `, stopped at the node limit at step ${graph.reached}` : "" }},
        {{ plural(folded, "group") }} folded
      </span>
      <span>Laid out left to right by step</span>
    </StatusFacts>
  </div>
</template>
