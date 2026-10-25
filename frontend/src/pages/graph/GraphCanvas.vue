<!--
  The graph canvas. It draws the laid-out graph with a minimap, reports a node's click and double click, and exports
  the picture as SVG or PNG.
-->
<script setup lang="ts">
import { computed, useTemplateRef } from "vue";
import {
  BaseEdge,
  EdgeLabelRenderer,
  VueFlow,
  getBezierPath,
  getRectOfNodes,
  useVueFlow,
  type EdgeProps,
  type FlowEvents,
  type GraphNode as FlowNode,
} from "@vue-flow/core";
import { useResizeObserver } from "@vueuse/core";
import { MiniMap } from "@vue-flow/minimap";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/minimap/dist/style.css";
import { toPng, toSvg } from "html-to-image";
import type { Graph, Node } from "@services/graph/models";
import GraphNode from "./GraphNode.vue";
import { useEventGraph } from "./layout";

const { graph, selected, full = false } = defineProps<{ graph: Graph; selected: string; full?: boolean }>();
const emit = defineEmits<{ select: [id: string]; draw: [id: string] }>();

type Point = { x: number; y: number };

/** The room left around the graph in an exported picture. */
const MARGIN = 24;
const LEGEND = [
  { label: "Event", sample: "h-3 w-5.5 rounded-xs border border-accented bg-elevated" },
  { label: "On action", sample: "h-2.5 w-5 rounded-full border border-accented bg-muted" },
  { label: "Effect or trigger", sample: "h-2.5 w-5 rounded-full border border-accented" },
  { label: "Root", sample: "h-2.5 w-5 rounded-full border border-primary" },
  { label: "Selected path", sample: "h-0.5 w-3.5 bg-secondary" },
];

const flow = useEventGraph(() => graph);
const { fitView, getNodes, viewport, zoomIn, zoomOut } = useVueFlow("event-graph");
const canvas = useTemplateRef("canvas");
const zoomedOut = computed(() => viewport.value.zoom < 0.7);
const zoom = computed(() => Math.round(viewport.value.zoom * 100));

const lit = computed(() => {
  if (!selected) return null;
  const parents = new Map((graph.nodes ?? []).map((node) => [node.id, node.parent]));
  const route = new Set([selected]);
  for (let id = parents.get(selected); id; id = parents.get(id)) route.add(id);
  const near = new Set(route);
  for (const edge of graph.edges ?? []) {
    if (edge.from === selected) near.add(edge.to);
    if (edge.to === selected) near.add(edge.from);
  }
  return { route, near };
});
const nodes = computed(() =>
  flow.nodes.map((node) =>
    node.type === "graph"
      ? {
          ...node,
          class: [node.class, node.id === selected && "selected", lit.value && !lit.value.near.has(node.id) && "dim"],
        }
      : node,
  ),
);
const edges = computed(() =>
  flow.edges.map((edge) => {
    const touches = [edge.source, edge.target].includes(selected);
    const onRoute = lit.value?.route.has(edge.source) && lit.value.route.has(edge.target);
    return {
      ...edge,
      label: touches ? edge.label : "",
      class: onRoute || touches ? "lit" : [lit.value && "dim", edge.data.back && "back"],
    };
  }),
);

let lastRoot = "";
// the view is fitted again as the canvas changes size, until the user moves it
let following = true;
useResizeObserver(canvas, () => following && void fit());

/** Fits the graph once for each root. The same root with other filters keeps the view. */
function fitOnce(): void {
  if (lastRoot === graph.root) return;
  lastRoot = graph.root;
  following = true;
  void fit();
}

/** Stops following the canvas's size once the user pans or zooms. */
function stopFollowing({ event }: FlowEvents["moveStart"]): void {
  if ("sourceEvent" in event && event.sourceEvent) following = false;
}

/** Fits the whole graph in the canvas. */
function fit(): Promise<boolean> {
  return fitView({ padding: 0.15, maxZoom: 1 });
}

/** Builds the path of a fire and the place of its label. A fire back follows its route, else it is a curve. */
function buildPath(edge: EdgeProps): { path: string; x: number; y: number } {
  const points: Point[] = edge.data.points;
  if (edge.source === edge.target) {
    const { sourceX: x, sourceY: y } = edge;
    const top = y - edge.sourceNode.dimensions.height / 2;
    return { path: `M${x},${y} C${x + 40},${y} ${x + 24},${top - 32} ${x - 16},${top}`, x: x + 18, y: top - 14 };
  }
  if (!points.length) {
    const [path, x, y] = getBezierPath(edge);
    return { path, x, y };
  }
  const path = points.slice(1).reduce((route, point, position) => {
    const previous = points[position];
    const half = (point.x - previous.x) / 2;
    return `${route} C${previous.x + half},${previous.y} ${point.x - half},${point.y} ${point.x},${point.y}`;
  }, `M${points[0].x},${points[0].y}`);
  const middle = points[Math.floor(points.length / 2)];
  return { path, x: middle.x, y: middle.y };
}

/** Returns the minimap's color for a node: the accent for the root, a quiet line for the rest. */
function resolveColor(node: FlowNode): string {
  return node.id === graph.root ? "var(--accent)" : "var(--line-2)";
}

/** Saves the whole graph as a picture, drawn by the given function, in a file with the given extension. */
async function save(draw: typeof toSvg, extension: string): Promise<void> {
  const viewport = canvas.value?.querySelector<HTMLElement>(".vue-flow__viewport");
  if (!canvas.value || !viewport) return;
  const bounds = getRectOfNodes(getNodes.value);
  const width = bounds.width + 2 * MARGIN;
  const height = bounds.height + 2 * MARGIN;
  const link = document.createElement("a");
  link.href = await draw(viewport, {
    width,
    height,
    backgroundColor: getComputedStyle(canvas.value).backgroundColor,
    style: {
      width: `${width}px`,
      height: `${height}px`,
      transform: `translate(${MARGIN - bounds.x}px, ${MARGIN - bounds.y}px) scale(1)`,
    },
  });
  link.download = `event-graph.${extension}`;
  link.click();
}

defineExpose({ fit, exportSvg: () => save(toSvg, "svg"), exportPng: () => save(toPng, "png") });
</script>

<template>
  <div ref="canvas" class="graph relative h-full w-full bg-(--bg-0)" :class="{ 'zoomed-out': zoomedOut }">
    <VueFlow
      id="event-graph"
      :nodes="nodes"
      :edges="edges"
      :nodes-draggable="false"
      :nodes-connectable="false"
      :elements-selectable="false"
      :min-zoom="0.3"
      :max-zoom="2"
      class="h-full w-full"
      @node-click="({ node }) => node.type === 'graph' && emit('select', node.id)"
      @node-double-click="({ node }) => node.type === 'graph' && !node.data.count && emit('draw', node.id)"
      @pane-click="emit('select', '')"
      @nodes-initialized="fitOnce"
      @move-start="stopFollowing"
    >
      <template #edge-fire="edge">
        <BaseEdge :id="edge.id" :path="buildPath(edge).path" />
        <EdgeLabelRenderer v-if="edge.label">
          <span
            class="fire-label"
            :style="{ transform: `translate(-50%, -50%) translate(${buildPath(edge).x}px, ${buildPath(edge).y}px)` }"
            >{{ edge.label }}</span
          >
        </EdgeLabelRenderer>
      </template>
      <template #node-graph="{ data }: { data: Node }">
        <GraphNode :node="data" />
      </template>
      <template #node-head="{ data }: { data: { label: string } }">
        <span class="pointer-events-none text-11 font-medium whitespace-nowrap text-dimmed">{{ data.label }}</span>
      </template>
      <MiniMap
        :width="150"
        :height="96"
        :node-color="resolveColor"
        node-stroke-color="transparent"
        mask-color="color-mix(in srgb, var(--bg-0) 55%, transparent)"
        :node-border-radius="2"
        pannable
        zoomable
      />
    </VueFlow>
    <div
      v-if="full && zoomedOut"
      class="absolute start-4 bottom-3.5 flex items-center gap-2 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 text-dimmed"
    >
      <UIcon name="i-lucide-info" class="size-3.5" />
      Zoomed out to {{ zoom }}%: cards show names only. Nodes off the selected path are faded.
    </div>
    <div
      v-else
      class="absolute start-3 bottom-3 flex items-center gap-3.5 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 whitespace-nowrap text-dimmed"
    >
      <span v-for="item in LEGEND" :key="item.label" class="flex items-center gap-1.5">
        <span :class="item.sample" />{{ item.label }}
      </span>
    </div>
    <div class="absolute end-43 bottom-3 flex items-end gap-1.5">
      <UBadge v-if="full" :label="`${zoom}%`" />
      <div class="flex flex-col gap-1">
        <UButton icon="i-lucide-plus" size="sm" square aria-label="Zoom in" @click="zoomIn()" />
        <UButton icon="i-lucide-minus" size="sm" square aria-label="Zoom out" @click="zoomOut()" />
      </div>
    </div>
  </div>
</template>

<style scoped>
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
.graph :deep(.vue-flow__edge.back) {
  opacity: 0.2;
}
.fire-label {
  position: absolute;
  z-index: 1001;
  padding: 0 4px;
  border-radius: 3px;
  background: var(--bg-0);
  color: var(--link);
  font-family: var(--font-mono);
  font-size: 10.5px;
  white-space: nowrap;
  pointer-events: none;
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
