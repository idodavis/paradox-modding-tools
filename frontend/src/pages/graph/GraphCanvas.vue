<!--
  The canvas the event graph draws on, with a minimap, zoom buttons and a
  legend.
-->
<script setup lang="ts">
import { computed, useTemplateRef } from "vue";
import {
  BaseEdge,
  EdgeLabelRenderer,
  VueFlow,
  getRectOfNodes,
  getTransformForBounds,
  useVueFlow,
  type Edge as FlowEdge,
  type EdgeProps,
  type FlowEvents,
  type GraphNode as FlowNode,
  type Node as FlowInput,
} from "@vue-flow/core";
import { createReusableTemplate, useResizeObserver } from "@vueuse/core";
import { MiniMap } from "@vue-flow/minimap";
import { clamp } from "es-toolkit";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/minimap/dist/style.css";
import GraphNode from "./GraphNode.vue";
import { findMiddle, roundPath, type Point } from "./layered";
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
const emit = defineEmits<{ select: [id: string]; ask: [id: string] }>();

/** SELF_LOOP is the curve of an edge to its own node, and its label's place. */
const SELF_LOOP = { out: 40, near: 24, rise: 32, back: 16, labelX: 18, labelY: 14 };
/** FIT frames a whole drawing, never past full size. */
const FIT = { padding: 0.05, maxZoom: 1 };
/**
 * READABLE_ZOOM is the smallest zoom a view opens fitted at. A drawing that
 * needs less opens at full size on its root, to pan from.
 */
const READABLE_ZOOM = 0.8;
const ZOOM_LIMITS = { min: 0.3, max: 2 };
/** FADED_BELOW is the zoom percent under which full screen explains fading. */
const FADED_BELOW = 70;
const MINIMAP = { width: 150, height: 96 };
/** LEGEND is a row for each mark of the canvas, with a sample in classes. */
const LEGEND = [
  { label: "Event", sample: "h-3 w-5.5 rounded-xs border border-inverted/40 bg-(--bg-3)" },
  {
    label: "Hidden event",
    sample:
      "h-3 w-5.5 rounded-xs border border-inverted/40 bg-[repeating-linear-gradient(135deg,var(--bg-3)_0_3px,var(--bg-0)_3px_6px)]",
  },
  { label: "Message", sample: "h-3 w-5.5 rounded-[3px] border border-inverted/40 bg-(--bg-2)" },
  { label: "On action", sample: "h-2.5 w-5 rounded-full border border-inverted/40 bg-(--bg-2)" },
  { label: "Root", sample: "h-2.5 w-5 rounded-full border border-secondary" },
  { label: "Selected", sample: "h-2.5 w-5 rounded-full border border-primary" },
  { label: "Selected path", sample: "h-0.5 w-3.5 bg-(--link)" },
  { label: "Delayed", sample: "w-5 border-t-[1.5px] border-dashed border-(--fg-3)" },
  { label: "Through a macro", sample: "w-5 border-t-2 border-dotted border-(--fg-3)" },
];

const [DefineWire, Wire] = createReusableTemplate<{ edge: EdgeProps; line: ReturnType<typeof buildPath> }>();
const { dimensions, findNode, fitView, getNodes, setCenter, setViewport, viewport, zoomIn, zoomOut } =
  useVueFlow("graph");
const canvas = useTemplateRef("canvas");
const zoom = computed(() => Math.round(viewport.value.zoom * 100));

let lastView = "";
let following = true;
let anchor = "";
useResizeObserver(canvas, () => following && void frame());

/**
 * frameOnce frames a new view. A drawing that changed under the same view,
 * as after a fold opens, is centered on the anchor once.
 */
function frameOnce(): void {
  if (lastView !== view) {
    lastView = view;
    following = true;
    void frame();
    return;
  }

  const node = findNode(anchor);
  anchor = "";
  if (!node) return;

  const { x, y } = node.computedPosition;
  following = false;
  void setCenter(x + node.dimensions.width / 2, y + node.dimensions.height / 2, { zoom: viewport.value.zoom });
}

/** setAnchor names the node to center once the drawing next changes. */
function setAnchor(id: string): void {
  anchor = id;
}

/**
 * frame shows the whole drawing where its text stays readable. A larger one
 * opens at full size on its root.
 */
function frame(): Promise<boolean> {
  const { width, height } = dimensions.value;
  const drawn = getRectOfNodes(getNodes.value);
  const { zoom } = getTransformForBounds(drawn, width, height, ZOOM_LIMITS.min, FIT.maxZoom, FIT.padding);
  const focus = findNode(root) ?? getNodes.value[0];
  if (!focus || zoom >= READABLE_ZOOM) return fitView(FIT);

  const { x, y } = focus.computedPosition;
  return setViewport({
    x: centerOffset(width, x + focus.dimensions.width / 2, drawn.x, drawn.width),
    y: drawn.height > height ? centerOffset(height, y + focus.dimensions.height / 2, drawn.y, drawn.height) : -drawn.y,
    zoom: FIT.maxZoom,
  });
}

/**
 * centerOffset returns the offset along one axis that puts a point at the
 * middle of the canvas, held back so no empty room shows past the drawing.
 */
function centerOffset(canvasSize: number, point: number, start: number, size: number): number {
  const startAtEdge = -start;
  const endAtEdge = canvasSize - start - size;
  return clamp(canvasSize / 2 - point, Math.min(startAtEdge, endAtEdge), Math.max(startAtEdge, endAtEdge));
}

/**
 * stopFollowing stops fitting the view to the canvas's size once the user
 * moves it.
 */
function stopFollowing({ event }: FlowEvents["moveStart"]): void {
  if ("sourceEvent" in event && event.sourceEvent) following = false;
}

function fit(): Promise<boolean> {
  return fitView(FIT);
}

/**
 * buildPath builds an edge's path and label place from the wire the layout
 * routed. An edge from a node to itself loops over the node.
 */
function buildPath(edge: EdgeProps): { path: string; x: number; y: number } {
  const points: Point[] = edge.data.points;
  if (points.length) {
    const [x, y] = findMiddle(points);

    return { path: roundPath(points), x, y };
  }

  const { sourceX, sourceY } = edge;
  const top = sourceY - edge.sourceNode.dimensions.height / 2;
  const { out, near, rise, back, labelX, labelY } = SELF_LOOP;
  const path = `M${sourceX},${sourceY} C${sourceX + out},${sourceY} ${sourceX + near},${top - rise} ${sourceX - back},${top}`;

  return { path, x: sourceX + labelX, y: top - labelY };
}

function resolveColor(node: FlowNode): string {
  return node.id === root ? "var(--link)" : "var(--line-2)";
}

/**
 * click selects the node that was clicked. A loop's marker selects the node it
 * names.
 */
function click(node: FlowNode): void {
  emit("select", node.data.loop?.marker === "back" ? node.data.node.id : node.id);
}

/**
 * ask asks about the node that was double-clicked. A fold and a loop's marker
 * are no subject.
 */
function ask(node: FlowNode): void {
  if (node.data.loop || node.data.node.count) return;
  emit("ask", node.data.node.id);
}

defineExpose({ fit, setAnchor });
</script>

<template>
  <div ref="canvas" class="graph relative h-full w-full bg-(--bg-0)">
    <DefineWire v-slot="{ edge, line }">
      <BaseEdge :id="edge.id" :path="line.path" />
      <path v-if="edge.data.conditions" :d="line.path" class="hover-area">
        <title>{{ edge.data.conditions }}</title>
      </path>
      <EdgeLabelRenderer v-if="edge.label">
        <span class="wire-label" :style="{ transform: `translate(-50%, -50%) translate(${line.x}px, ${line.y}px)` }">{{
          edge.label
        }}</span>
      </EdgeLabelRenderer>
    </DefineWire>
    <VueFlow
      id="graph"
      :nodes="nodes"
      :edges="edges"
      :nodes-draggable="false"
      :nodes-connectable="false"
      :elements-selectable="false"
      :min-zoom="ZOOM_LIMITS.min"
      :max-zoom="ZOOM_LIMITS.max"
      class="h-full w-full"
      @node-click="({ node }) => click(node)"
      @node-double-click="({ node }) => ask(node)"
      @pane-click="emit('select', '')"
      @nodes-initialized="frameOnce"
      @move-start="stopFollowing"
    >
      <template #edge-wire="edge"><Wire :edge="edge" :line="buildPath(edge)" /></template>
      <template #node-graph="{ data }: { data: Drawn }">
        <GraphNode :drawn="data" />
      </template>
      <MiniMap
        :width="MINIMAP.width"
        :height="MINIMAP.height"
        :node-color="resolveColor"
        node-stroke-color="transparent"
        mask-color="color-mix(in srgb, var(--bg-0) 55%, transparent)"
        :node-border-radius="2"
        pannable
        zoomable
      />
    </VueFlow>
    <div
      v-if="full && zoom < FADED_BELOW"
      class="absolute start-4 bottom-3.5 flex items-center gap-2 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 text-dimmed"
    >
      <UIcon name="i-lucide-info" class="size-3.5" />
      Zoomed out to {{ zoom }}%. Wires off the selected path are faded.
    </div>
    <div
      v-else
      class="absolute start-3 bottom-3 flex items-center gap-3.5 rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 whitespace-nowrap text-dimmed"
    >
      <span v-for="row in LEGEND" :key="row.label" class="flex items-center gap-1.5">
        <span :class="row.sample" />{{ row.label }}
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
.hover-area {
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
.wire-label {
  position: absolute;
  z-index: 1001;
  padding: 0 4px;
  border-radius: 3px;
  background: var(--bg-0);
  color: var(--link);
  font-family: var(--font-mono);
  font-size: 10.5px;
  white-space: nowrap;
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
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
