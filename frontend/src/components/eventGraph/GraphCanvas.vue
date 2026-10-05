<script setup lang="ts">
/**
 * The graph on its canvas (pmt.css .gcanvas, .ge, .colh): Vue Flow over the laid-out nodes, a head over each
 * column by its step from the root, the legend, zoom and the minimap. With a node selected, what is off a
 * route from the root to it, but for its neighbors, fades, and the route's fires are drawn in the link
 * color, the selected node's own with what they pass through. Zoomed out, the nodes show their ids only, which full screen says.
 */
import { computed, watch } from "vue";
import { VueFlow, useVueFlow, type Edge as FlowEdge, type Node as FlowNode, type NodeMouseEvent } from "@vue-flow/core";
import { MiniMap } from "@vue-flow/minimap";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/minimap/dist/style.css";
import type { Edge, Node } from "@services/models";
import { reach } from "../../composables/useEventGraph";
import GraphNode from "./GraphNode.vue";

const { nodes, edges, positions, heads, rootId, selectedId, depth, full = false } = defineProps<{
  nodes: Node[];
  edges: Edge[];
  positions: Map<string, { x: number; y: number }>;
  heads: { x: number; y: number; step: number }[];
  rootId: string;
  selectedId: string;
  depth: number;
  full?: boolean;
}>();
const emit = defineEmits<{ select: [node: Node]; reroot: [id: string]; clear: [] }>();
const { viewport, setViewport, fitView, zoomIn, zoomOut } = useVueFlow("event-graph");

/** Zoomed out this far, nodes show their ids only. */
const lod = computed(() => viewport.value.zoom < 0.7);
const zoom = computed(() => Math.round(viewport.value.zoom * 100));

// what stays lit: every route from the root to the selected node, and that node's neighbors
const lit = computed(() => {
  if (!selectedId) return null;
  const back = reach(edges, selectedId, false);
  const route = new Set([...reach(edges, rootId, true).keys()].filter((id) => back.has(id)));
  const near = new Set([...route, selectedId]);
  for (const edge of edges) {
    if (edge.from === selectedId) near.add(edge.to);
    if (edge.to === selectedId) near.add(edge.from);
  }
  return { route, near };
});
const onRoute = (edge: Edge) => !!lit.value && lit.value.route.has(edge.from) && lit.value.route.has(edge.to);
const touches = (edge: Edge) => !!lit.value && (edge.from === selectedId || edge.to === selectedId);

/** A column's head: the root's, its steps out, the last at the depth limit; the callers' steps before it. */
function headOf(step: number): string {
  if (step === 0) return "Root";
  if (step === -1) return "Fired by";
  if (step < 0) return `${-step} steps before`;
  return `${step} ${step === 1 ? "step" : "steps"}${step === depth ? ", the depth limit" : ""}`;
}

const flowNodes = computed((): FlowNode[] => [
  ...heads.map((head) => ({
    id: `head:${head.x}`,
    type: "head",
    position: { x: head.x, y: head.y },
    data: { label: headOf(head.step) },
    selectable: false,
  })),
  ...nodes.map((node) => ({
    id: node.id,
    type: "graph",
    position: positions.get(node.id) ?? { x: 0, y: 0 },
    data: node,
  })),
]);

// a fire on the selected route, or into or out of the selected node, is drawn in the link color; one into
// or out of the selected node is labelled by what it passes through (a dense graph's route is most of it)
const flowEdges = computed((): FlowEdge[] =>
  edges.map((edge, i) => ({
    id: `${edge.from}->${edge.to}:${i}`,
    source: edge.from,
    target: edge.to,
    label: touches(edge) ? edge.via : "",
    labelBgPadding: [4, 0] as [number, number],
    labelBgBorderRadius: 3,
    class: onRoute(edge) || touches(edge) ? "lit" : lit.value ? "dim" : "",
  })),
);

// an opened stub's neighbor stays where it was on the screen while the graph is laid out again
let anchor: { id: string; x: number; y: number } | undefined;
function onClick({ node: { type, data } }: NodeMouseEvent): void {
  if (type !== "graph") return;
  const node: Node = data;
  if (node.stub) {
    const edge = edges.find((candidate) => candidate.to === node.id || candidate.from === node.id);
    const id = edge && (edge.to === node.id ? edge.from : edge.to);
    const at = id ? positions.get(id) : undefined;
    anchor = id && at ? { id, ...at } : undefined;
  }
  emit("select", node);
}
watch(
  () => positions,
  (now) => {
    const at = anchor && now.get(anchor.id);
    if (anchor && at) {
      const { x, y, zoom } = viewport.value;
      void setViewport({ x: x - (at.x - anchor.x) * zoom, y: y - (at.y - anchor.y) * zoom, zoom });
    }
    anchor = undefined;
  },
);

// a new root is fitted to the canvas once its nodes are measured; an opened stub keeps the view
let fitted = "";
function fitRoot(): void {
  if (fitted === rootId) return;
  fitted = rootId;
  void fitView({ padding: 0.15, minZoom: 0.7, maxZoom: 1 });
}

/** A double-click draws the graph from a node; a stub opens on the first click. */
function onDoubleClick({ node: { type, id, data } }: NodeMouseEvent): void {
  if (type === "graph" && !data.stub) emit("reroot", id);
}

function minimapColor(node: FlowNode): string {
  return node.id === rootId ? "var(--accent)" : "var(--line-2)";
}

defineExpose({ fit: () => fitView({ padding: 0.15, maxZoom: 1 }) });
</script>

<template>
  <div class="graph relative h-full w-full bg-(--bg-0)">
    <VueFlow
      id="event-graph"
      :nodes="flowNodes"
      :edges="flowEdges"
      :nodes-draggable="false"
      :nodes-connectable="false"
      :elements-selectable="false"
      :min-zoom="0.3"
      :max-zoom="2"
      class="h-full w-full"
      @node-click="onClick"
      @node-double-click="onDoubleClick"
      @pane-click="emit('clear')"
      @nodes-initialized="fitRoot"
    >
      <template #node-head="{ data }">
        <span class="pointer-events-none text-11 font-medium whitespace-nowrap text-dimmed">{{ data.label }}</span>
      </template>
      <template #node-graph="{ data }">
        <GraphNode :node="data" :selected="data.id === selectedId" :dim="!!lit && !lit.near.has(data.id)" :lod="lod" />
      </template>
      <MiniMap
        :width="150"
        :height="96"
        :node-color="minimapColor"
        node-stroke-color="transparent"
        mask-color="color-mix(in srgb, var(--bg-0) 55%, transparent)"
        :node-border-radius="2"
        pannable
        zoomable
      />
    </VueFlow>
    <div
      v-if="full && lod"
      class="absolute start-4 bottom-3.5 flex items-center gap-2 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 text-dimmed"
    >
      <UIcon name="i-lucide-info" class="size-3.5" />
      Zoomed out to {{ zoom }}%: cards show ids only. Nodes off the selected path are faded.
    </div>
    <div
      v-else
      class="absolute start-3 bottom-3 flex items-center gap-3.5 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 whitespace-nowrap text-dimmed"
    >
      <span class="flex items-center gap-1.5">
        <span class="relative h-3 w-5.5 overflow-hidden rounded-xs border border-accented bg-elevated">
          <i class="absolute inset-y-0 start-0 w-0.5 bg-(--o1)" />
        </span>
        Event
      </span>
      <span class="flex items-center gap-1.5"><span class="h-2.5 w-5 rounded-full border border-accented bg-muted" />On action</span>
      <span class="flex items-center gap-1.5"><span class="h-2.5 w-5 rounded-full border border-accented" />Effect or trigger</span>
      <span class="flex items-center gap-1.5"><OriginDot color="var(--o1)" small />Origin</span>
      <span class="flex items-center gap-1.5"><span class="h-0.5 w-3.5 bg-secondary" />Selected path</span>
    </div>
    <div class="absolute end-43 bottom-3 flex items-end gap-1.5">
      <UBadge v-if="full" :label="`${zoom}%`" />
      <div class="flex flex-col gap-1">
        <UButton icon="i-lucide-plus" size="sm" square aria-label="Zoom in" @click="zoomIn()" />
        <UButton icon="i-lucide-minus" size="sm" square aria-label="Zoom out" @click="zoomOut()" />
      </div>
    </div>
    <slot />
  </div>
</template>

<style scoped>
/* the board's canvas: bg-0 under a dot grid on line-1, every 20px */
.graph {
  background-image: radial-gradient(var(--line-1) 1px, transparent 1px);
  background-size: 20px 20px;
}
.graph :deep(.vue-flow__node) {
  cursor: pointer;
}
.graph :deep(.vue-flow__handle) {
  opacity: 0;
}
.graph :deep(.vue-flow__edge-path) {
  stroke: var(--line-2);
  stroke-width: 1.5;
}
.graph :deep(.vue-flow__edge.lit .vue-flow__edge-path) {
  stroke: var(--link);
  stroke-width: 2;
}
.graph :deep(.vue-flow__edge.dim) {
  opacity: 0.35;
}
.graph :deep(.vue-flow__edge-text) {
  fill: var(--link);
  font-family: var(--font-mono);
  font-size: 10.5px;
}
.graph :deep(.vue-flow__edge-textbg) {
  fill: var(--bg-0);
}
.graph :deep(.vue-flow__minimap) {
  inset: auto 12px 12px auto;
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--line-1);
  border-radius: 8px;
  background: var(--bg-2);
}
</style>
