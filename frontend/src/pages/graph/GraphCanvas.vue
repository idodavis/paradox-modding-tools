<!-- The canvas the graph draws on, with a minimap, zoom buttons and a legend. It exports the picture as SVG or PNG. -->
<script setup lang="ts">
import { computed, useTemplateRef } from "vue";
import {
  BaseEdge,
  EdgeLabelRenderer,
  VueFlow,
  getRectOfNodes,
  useVueFlow,
  type Edge as FlowEdge,
  type EdgeProps,
  type FlowEvents,
  type GraphNode as FlowNode,
  type Node as FlowInput,
} from "@vue-flow/core";
import { useResizeObserver } from "@vueuse/core";
import { MiniMap } from "@vue-flow/minimap";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/minimap/dist/style.css";
import { toPng, toSvg } from "html-to-image";
import GraphNode from "./GraphNode.vue";
import type { Drawn } from "./layout";

const {
  nodes,
  edges,
  view,
  root,
  full = false,
} = defineProps<{
  nodes: FlowInput[];
  edges: FlowEdge[];
  /** What the canvas shows. The view is fitted again when it changes. */
  view: string;
  root: string;
  full?: boolean;
}>();
const emit = defineEmits<{ select: [id: string]; draw: [id: string] }>();

type Point = { x: number; y: number };

/** MARGIN is the space around the graph in an exported picture. */
const MARGIN = 24;
/** LEAD is how far an edge runs level from a handle before it curves, in pixels. */
const LEAD = 24;
/** LEGEND is the legend's entries. */
const LEGEND = [
  { label: "Event", sample: "h-3 w-5.5 rounded-xs border border-(--line-2) bg-(--bg-3)" },
  {
    label: "Hidden event",
    sample: "h-3 w-5.5 rounded-xs bg-[repeating-linear-gradient(135deg,var(--bg-2)_0_3px,var(--bg-1)_3px_6px)]",
  },
  { label: "Message", sample: "h-3 w-5.5 rounded-[3px] bg-(--bg-2)" },
  { label: "On action", sample: "h-2.5 w-5 rounded-full border border-(--line-2) bg-(--bg-2)" },
  { label: "Root", sample: "h-2.5 w-5 rounded-full border border-primary" },
  { label: "Selected path", sample: "h-0.5 w-3.5 bg-(--link)" },
  { label: "Delayed", sample: "w-5 border-t-[1.5px] border-dashed border-(--fg-3)" },
  { label: "Through a macro", sample: "w-5 border-t-2 border-dotted border-(--fg-3)" },
];

const { fitView, getNodes, viewport, zoomIn, zoomOut } = useVueFlow("event-graph");
const canvas = useTemplateRef("canvas");
const zoom = computed(() => Math.round(viewport.value.zoom * 100));

let lastView = "";
let following = true;
useResizeObserver(canvas, () => following && void fit());

/** fitOnce fits the canvas the first time it sees a view. */
function fitOnce(): void {
  if (lastView === view) return;
  lastView = view;
  following = true;
  void fit();
}

/** stopFollowing stops fitting the view to the canvas's size once the user moves it. */
function stopFollowing({ event }: FlowEvents["moveStart"]): void {
  if ("sourceEvent" in event && event.sourceEvent) following = false;
}

/** fit fits the view to everything drawn, never smaller than half size so the text stays readable. */
function fit(): Promise<boolean> {
  return fitView({ padding: 0.15, minZoom: 0.5, maxZoom: 1 });
}

/**
 * buildPath builds a firing's path and label place. The path leaves the firer's handle and reaches the target's level.
 * Between neighbouring ranks it is one S curve; a longer edge is one smooth curve through the points the layout routes
 * it by. The label sits at its middle.
 */
function buildPath(edge: EdgeProps): { path: string; x: number; y: number } {
  const { sourceX, sourceY, targetX, targetY } = edge;
  if (edge.source === edge.target) {
    const top = sourceY - edge.sourceNode.dimensions.height / 2;
    const path = `M${sourceX},${sourceY} C${sourceX + 40},${sourceY} ${sourceX + 24},${top - 32} ${sourceX - 16},${top}`;
    return { path, x: sourceX + 18, y: top - 14 };
  }
  const inner: Point[] = edge.data.points;
  if (!inner.length) {
    const half = (targetX - sourceX) / 2;
    const path = `M${sourceX},${sourceY} C${sourceX + half},${sourceY} ${targetX - half},${targetY} ${targetX},${targetY}`;
    return { path, x: (sourceX + targetX) / 2, y: (sourceY + targetY) / 2 };
  }
  // the short level runs at both ends make the curve leave and reach the handles straight
  const points = [
    { x: sourceX, y: sourceY },
    { x: sourceX + LEAD, y: sourceY },
    ...inner,
    { x: targetX - LEAD, y: targetY },
    { x: targetX, y: targetY },
  ];
  const middle = inner[Math.floor(inner.length / 2)];
  return { path: traceBasis(points), x: middle.x, y: middle.y };
}

/** traceBasis traces a uniform B-spline through points as an SVG path, starting and ending on the end points. */
function traceBasis(points: Point[]): string {
  const [first, second] = points;
  let path = `M${first.x},${first.y} L${(5 * first.x + second.x) / 6},${(5 * first.y + second.y) / 6}`;
  for (let position = 2; position < points.length + 1; position++) {
    const [before, current] = [points[position - 2], points[position - 1]];
    const next = points[Math.min(position, points.length - 1)];
    path +=
      ` C${(2 * before.x + current.x) / 3},${(2 * before.y + current.y) / 3}` +
      ` ${(before.x + 2 * current.x) / 3},${(before.y + 2 * current.y) / 3}` +
      ` ${(before.x + 4 * current.x + next.x) / 6},${(before.y + 4 * current.y + next.y) / 6}`;
  }
  const last = points[points.length - 1];
  return `${path} L${last.x},${last.y}`;
}

/** resolveColor returns the minimap color of a node. */
function resolveColor(node: FlowNode): string {
  return node.id === root ? "var(--accent)" : "var(--line-2)";
}

/** click selects the node that was clicked. A loop's marker selects the node it names. */
function click(node: FlowNode): void {
  emit("select", node.data.loop?.kind === "back" ? node.data.node.id : node.id);
}

/** save saves the whole canvas as a picture made by the given function. */
async function save(draw: typeof toSvg, extension: string): Promise<void> {
  const flow = canvas.value?.querySelector<HTMLElement>(".vue-flow__viewport");
  if (!canvas.value || !flow) return;
  const bounds = getRectOfNodes(getNodes.value);
  const width = bounds.width + 2 * MARGIN;
  const height = bounds.height + 2 * MARGIN;
  const link = document.createElement("a");
  link.href = await draw(flow, {
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
  <div ref="canvas" class="graph relative h-full w-full bg-(--bg-0)">
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
      @node-click="({ node }) => click(node)"
      @node-double-click="({ node }) => !node.data.loop && !node.data.node.count && emit('draw', node.id)"
      @pane-click="emit('select', '')"
      @nodes-initialized="fitOnce"
      @move-start="stopFollowing"
    >
      <template #edge-fire="edge">
        <BaseEdge :id="edge.id" :path="buildPath(edge).path" />
        <!-- a wider invisible stroke over the edge, whose title shows the firing's conditions on hover -->
        <path v-if="edge.data.conditions" :d="buildPath(edge).path" class="hit">
          <title>{{ edge.data.conditions }}</title>
        </path>
        <EdgeLabelRenderer v-if="edge.label">
          <span
            class="fire-label"
            :style="{ transform: `translate(-50%, -50%) translate(${buildPath(edge).x}px, ${buildPath(edge).y}px)` }"
            >{{ edge.label }}</span
          >
        </EdgeLabelRenderer>
      </template>
      <template #node-graph="{ data }: { data: Drawn }">
        <GraphNode :drawn="data" />
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
      v-if="full && zoom < 70"
      class="absolute start-4 bottom-3.5 flex items-center gap-2 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 text-dimmed"
    >
      <UIcon name="i-lucide-info" class="size-3.5" />
      Zoomed out to {{ zoom }}%. Nodes off the selected path are faded.
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
.hit {
  fill: none;
  stroke: transparent;
  stroke-width: 12;
}
.graph :deep(.vue-flow__edge.delayed .vue-flow__edge-path) {
  stroke-dasharray: 6 4;
}
.graph :deep(.vue-flow__edge.via .vue-flow__edge-path) {
  stroke-dasharray: 1.5 3.5;
  stroke-linecap: round;
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
