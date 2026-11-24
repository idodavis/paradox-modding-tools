<!-- The one canvas: a subject's nodes and wires drawn by Vue Flow, with its minimap, legend, zoom and selection. -->
<script setup lang="ts">
import { computed, markRaw, nextTick, shallowRef, watch } from "vue";
import {
  BaseEdge,
  EdgeLabelRenderer,
  MarkerType,
  useVueFlow,
  VueFlow,
  type Edge as Wire,
  type EdgeProps,
  type Node as Box,
} from "@vue-flow/core";
import { MiniMap } from "@vue-flow/minimap";
import { createReusableTemplate } from "@vueuse/core";
import "@vue-flow/core/dist/style.css";
import "@vue-flow/minimap/dist/style.css";

import { Relation } from "@internal/index/models";
import type { Condition, Edge, Graph, Node } from "@services/atlas/models";
import AtlasNode, { GOODS, RELATIONS, shapeOf, type Kind, type Shape } from "./AtlasNode.vue";
import { BLOCK, drawRoute, placeLayers, placeTree, type Point, type Region, type Tie } from "./layout";

const {
  graph,
  kind,
  selected = "",
  opened = [],
  view = "",
  direction = "",
  full = false,
} = defineProps<{
  graph: Graph;
  kind: Kind;
  /** The selected node's id, "" for none. */
  selected?: string;
  /** The ids of the folds and groups the filter opens. */
  opened?: readonly string[];
  /** "chain" draws a technology's chain alone. */
  view?: string;
  /** "down" reads a tree top to bottom. */
  direction?: string;
  /** Whether the canvas fills the screen. */
  full?: boolean;
}>();
const emit = defineEmits<{
  /** A node was selected, or the selection cleared with "". */
  select: [id: string];
  /** A node was asked for as the subject. */
  ask: [id: string];
  /** A fold, a group or a node the depth stopped at was clicked, to open or close. */
  open: [id: string];
  /** A production method was clicked, to run in its group. */
  pick: [id: string];
}>();

/** A Drawn is what a flow node hands AtlasNode. */
type Drawn = { node: Node; shape: Shape; amount: string; audience: string; mark: string; isOpen: boolean };
/** A Heading is a line of words over a column of a good's or a building's canvas. */
type Heading = { title: string; summary: string };

/** The smallest zoom a canvas opens fitted at. One that needs less opens at full size on its subject. */
const READABLE_ZOOM = 0.8;
const ZOOM_LIMITS = { min: 0.3, max: 2 };
/** The share of the pane left free round a fitted canvas. */
const FIT_PADDING = 0.05;
/** The zoom percent under which full screen says the wires are faded. */
const FADED_BELOW = 70;
const MINIMAP = { width: 150, height: 96 };
const ARROW = { type: MarkerType.ArrowClosed, width: 9, height: 9 };
/** The room between the layers of each canvas: the event graph's 90, and more where a wire says an amount. */
const LAYER_GAPS: Record<Kind, number> = { event: 90, good: 130, building: 130, tree: 0 };
/** How far over the top node of its column a heading stands. */
const HEADING_RISE = 28;
/** The starts of the ids of the lines of words the canvas adds: a column's heading and a run of pills' line. */
const COLUMN = "column:";
const LINE = "line:";
const SUBJECT = { label: "Subject", sample: "h-2.5 w-5 rounded-full border border-secondary" };
const SELECTED = { label: "Selected", sample: "h-2.5 w-5 rounded-full border border-primary" };
const LINKS = [
  SUBJECT,
  SELECTED,
  { label: "Produces", sample: "h-0.5 w-3.5 bg-(--blue)" },
  { label: "Boosts or gives", sample: "h-0.5 w-3.5 bg-(--purple)" },
  { label: "Takes", sample: "h-0.5 w-3.5 bg-(--line-2)" },
  { label: "Under a condition", sample: "h-3 w-5.5 rounded-[4px] border border-dashed border-(--line-2)" },
];
/** LEGENDS holds a row for each mark of a canvas, with a sample in classes. */
const LEGENDS: Record<Kind, { label: string; sample: string }[]> = {
  event: [
    { label: "Event", sample: "h-3 w-5.5 rounded-xs border border-inverted/40 bg-(--bg-3)" },
    {
      label: "Hidden event",
      sample:
        "h-3 w-5.5 rounded-xs border border-inverted/40 bg-[repeating-linear-gradient(135deg,var(--bg-3)_0_3px,var(--bg-0)_3px_6px)]",
    },
    { label: "Message", sample: "h-3 w-5.5 rounded-[3px] border border-inverted/40 bg-(--bg-2)" },
    { label: "On action", sample: "h-2.5 w-5 rounded-full border border-inverted/40 bg-(--bg-2)" },
    { ...SUBJECT, label: "Root" },
    SELECTED,
    { label: "Selected path", sample: "h-0.5 w-3.5 bg-(--link)" },
    { label: "Delayed", sample: "w-5 border-t-[1.5px] border-dashed border-(--fg-3)" },
    { label: "Through a macro", sample: "w-5 border-t-2 border-dotted border-(--fg-3)" },
  ],
  good: LINKS,
  building: [
    ...LINKS,
    { label: "Runs", sample: "h-3 w-5.5 rounded-xs bg-(--bg-4)" },
    { label: "Removes jobs", sample: "h-0.5 w-3.5 bg-(--red)" },
    { label: "Unlocked after the era you chose", sample: "h-3 w-5.5 rounded-xs bg-(--bg-3) opacity-45" },
  ],
  tree: [
    SELECTED,
    { label: "Its chain", sample: "h-2.5 w-5 rounded-full border border-secondary" },
    {
      label: "For an audience",
      sample: "h-3 w-5.5 rounded-xs border border-(--line-2) border-s-3 border-s-(--purple)",
    },
  ],
};

const [DefineWire, DrawnWire] = createReusableTemplate<{ edge: EdgeProps; line: ReturnType<typeof drawRoute> }>();

const flow = useVueFlow("atlas");
const isTree = computed(() => kind === "tree");
const isDown = computed(() => isTree.value && direction === "down");

/** The subject's chain in a tree: itself, what it requires at any distance and what requires it. */
const chain = computed(() => {
  const ids = new Set([graph.root]);
  for (const [near, far] of [["to", "from"] as const, ["from", "to"] as const]) {
    const frontier = [graph.root];
    while (frontier.length) {
      const current = frontier.pop();
      for (const edge of graph.edges ?? []) {
        if (edge[near] !== current || ids.has(edge[far])) continue;
        ids.add(edge[far]);
        frontier.push(edge[far]);
      }
    }
  }
  return ids;
});
/** The nodes drawn: every node of the answer, or a tree's chain alone. A block is a region, not a node. */
const nodes = computed(() =>
  (graph.nodes ?? []).filter(
    (node) => !node.id.startsWith(BLOCK) && (!isTree.value || view !== "chain" || chain.value.has(node.id)),
  ),
);
const byId = computed(() => new Map(nodes.value.map((node) => [node.id, node])));
const edges = computed(() =>
  (graph.edges ?? []).filter((edge) => byId.value.has(edge.from) && byId.value.has(edge.to)),
);
const audiences = computed(() => new Map((graph.audiences ?? []).map((audience) => [audience.id, audience.title])));
/** How much each node's wires carry, where they all say the same: a pill several lines take differently says none. */
const amounts = computed(() => {
  const said = new Map<string, string>();
  for (const edge of edges.value) {
    for (const end of [edge.from, edge.to]) {
      said.set(end, (said.get(end) ?? edge.amount) === edge.amount ? (edge.amount ?? "") : "");
    }
  }
  return said;
});
/** The selected node's path to the subject, by each node's parent. */
const path = computed(() => {
  const ids = new Set<string>();
  for (let id = selected; id && !ids.has(id); id = byId.value.get(id)?.parent ?? "") ids.add(id);
  return ids;
});
/** The lines of what meeting a need gives, each over the first of the pills that share it. */
const lines = computed(() => {
  const runs = new Map<string, string>();
  let last = "";
  for (const node of kind === "building" ? nodes.value : []) {
    if (node.distance >= 0 || !node.summary || !GOODS.includes(node.type)) continue;
    if (node.summary !== last) runs.set(node.id, node.summary);
    last = node.summary;
  }
  return runs;
});
/**
 * The ties the layered layout places: the drawn wires first, then one between a fold and its parent, which no wire
 * joins, one from each line of words to the subject, which keeps the line in its pills' column, and one from each
 * node two steps out to a node one step out, which keeps a building's technologies in a column of their own.
 */
const ties = computed<Tie[]>(() => {
  const drawn = edges.value.map(({ from, to }) => ({ from, to }));
  const joined = new Set(drawn.flatMap(({ from, to }) => [from, to]));
  const toward = (node: Node, other: string) =>
    node.distance > 0 ? { from: other, to: node.id } : { from: node.id, to: other };
  const folds = nodes.value
    .filter((node) => node.parent && !joined.has(node.id))
    .map((node) => toward(node, node.parent));
  const outer = nodes.value
    .filter((node) => node.parent === graph.root && Math.abs(node.distance) > 1)
    .flatMap((node) => {
      const near = nodes.value.find((other) => other.distance === Math.sign(node.distance));
      return near ? toward(node, near.id) : [];
    });
  return [...drawn, ...folds, ...outer, ...[...lines.value.keys()].map((id) => ({ from: LINE + id, to: graph.root }))];
});

const layout = shallowRef<{
  root: string;
  points: Map<string, Point>;
  routes: (Point[] | undefined)[];
  regions: Region[];
}>({ root: "", points: new Map(), routes: [], regions: [] });
/** The headings over the columns of a good's or a building's canvas, each over the top of its distance. */
const columns = computed(() =>
  (graph.columns ?? []).flatMap((column) => {
    const points = nodes.value
      .filter((node) => node.distance === column.distance)
      .flatMap((node) =>
        [layout.value.points.get(node.id) ?? [], layout.value.points.get(LINE + node.id) ?? []].flat(),
      );
    if (!points.length) return [];
    return {
      id: COLUMN + column.distance,
      data: { title: column.title, summary: column.summary ?? "" },
      position: {
        x: Math.min(...points.map((point) => point.x)),
        y: Math.min(...points.map((point) => point.y)) - HEADING_RISE,
      },
    };
  }),
);

/** The flow's nodes. They do not change with the selection, which each node is told apart from its data. */
const boxes = computed<Box[]>(() => {
  const place = (id: string) => layout.value.points.get(id) ?? { x: 0, y: 0 };
  const drawn: Box[] = layout.value.regions.map((region) => ({
    id: region.id,
    type: "region",
    position: region,
    data: region,
    zIndex: -1,
    selectable: false,
  }));
  for (const node of nodes.value) {
    const line = lines.value.get(node.id);
    if (line) drawn.push({ id: LINE + node.id, type: "line", position: place(LINE + node.id), data: line });
    const data: Drawn = {
      node,
      shape: shapeOf(node, kind, graph.root),
      amount: amounts.value.get(node.id) ?? "",
      audience: audiences.value.get(node.audience ?? "") ?? "",
      mark: describeMark(node),
      isOpen: opened.includes(node.id),
    };
    drawn.push({
      id: node.id,
      type: "atlas",
      position: place(node.id),
      data: markRaw(data),
      class: layout.value.points.has(node.id) ? "" : "unplaced",
    });
  }
  return [...drawn, ...columns.value.map((heading) => ({ ...heading, type: "words", selectable: false }))];
});

const wires = computed<Wire[]>(() => {
  const seen = new Set<string>();
  return edges.value.map((edge, position) => {
    const isSecond = isTree.value && seen.has(edge.to);
    seen.add(edge.to);
    const lit = isTree.value
      ? chain.value.has(edge.from) && chain.value.has(edge.to)
      : path.value.has(edge.from) && path.value.has(edge.to);
    const touches = !isTree.value && !!selected && (edge.from === selected || edge.to === selected);
    return {
      id: `${edge.from}>${edge.to}#${position}`,
      source: edge.from,
      target: edge.to,
      type: "wire",
      markerEnd: ARROW,
      label: touches ? describeWire(edge) : undefined,
      data: { conditions: describeSteps(edge), route: layout.value.routes[position] },
      class: [
        lit ? "lit" : (isTree.value || selected) && "dim",
        edge.delay && "delayed",
        edge.via?.length && "via",
        isSecond && "second",
        edge.relation === Relation.RelationOutput && "makes",
        (edge.relation === Relation.RelationBoost || edge.relation === Relation.RelationGives) && "boosts",
      ]
        .filter(Boolean)
        .join(" "),
    };
  });
});

/** The boxes the layout places: the nodes, and the lines of words that stand among them. */
const placed = computed(() => flow.getNodes.value.filter((box) => box.type === "atlas" || box.type === "line"));
const isMeasured = computed(() => placed.value.length > 0 && placed.value.every((box) => box.dimensions.width > 0));
/**
 * What a layout is made from, as one text: the measured sizes of the boxes, the ties and the direction. A layout
 * is made again only when it changes, so a node that grows is placed again and a click places nothing.
 */
const plan = computed(() =>
  [
    ...placed.value.map((box) => `${box.id} ${box.dimensions.width}x${box.dimensions.height}`),
    ...ties.value.map((tie) => `${tie.from}>${tie.to}`),
    isDown.value,
  ].join("\n"),
);
const zoom = computed(() => (full ? Math.round(flow.viewport.value.zoom * 100) : 100));

/** Whether the canvas stays fitted while its pane is resized. The user's first move ends it. */
let isFollowing = true;
/** The node that keeps its place on screen through the next layout: the one that was clicked. */
let held = "";
/** The plan the layout on screen was made from. */
let arranged = "";

watch([isMeasured, plan], () => void arrange(), { flush: "post" });
watch(
  () => `${flow.dimensions.value.width}x${flow.dimensions.value.height}`,
  () => isFollowing && frame(),
);

/**
 * arrange places every node from its measured size. A new subject is framed. The same subject keeps a node where it
 * was on screen: the one clicked, else the selection, else the subject.
 */
async function arrange(): Promise<void> {
  const wanted = plan.value;
  if (!isMeasured.value || wanted === arranged) return;
  arranged = wanted;
  const root = graph.root;
  const sizes = new Map(placed.value.map((box) => [box.id, box.dimensions]));
  const card = sizes.get(nodes.value[0]?.id ?? "");
  const blocks = (graph.nodes ?? []).filter((node) => node.id.startsWith(BLOCK) && view !== "chain");
  const next =
    isTree.value && card
      ? placeTree({ ...graph, nodes: [...nodes.value, ...blocks], edges: edges.value }, card, isDown.value)
      : { ...(await placeLayers(sizes, ties.value, LAYER_GAPS[kind])), regions: [] };
  if (root !== graph.root) return;
  const before = layout.value;
  layout.value = { root, ...next };
  await nextTick();
  if (before.root !== root) {
    isFollowing = true;
    frame();
    return;
  }
  const anchor = [held, selected, root].find((id) => before.points.has(id) && next.points.has(id));
  const [from, to] = [before.points.get(anchor ?? ""), next.points.get(anchor ?? "")];
  const { x, y, zoom: scale } = flow.viewport.value;
  if (from && to)
    void flow.setViewport({ x: x + (from.x - to.x) * scale, y: y + (from.y - to.y) * scale, zoom: scale });
  held = "";
}

/** frame shows the whole canvas where that stays readable, else the subject at full size. */
function frame(): void {
  const drawn = flow.getNodes.value.filter((box) => box.type === "atlas");
  if (!drawn.length) return;
  const right = Math.max(...drawn.map((box) => box.position.x + box.dimensions.width));
  const bottom = Math.max(...drawn.map((box) => box.position.y + box.dimensions.height));
  const left = Math.min(...drawn.map((box) => box.position.x));
  const top = Math.min(...drawn.map((box) => box.position.y));
  const room = 1 - 2 * FIT_PADDING;
  const { width, height } = flow.dimensions.value;
  const fitted = Math.min((width * room) / (right - left), (height * room) / (bottom - top));
  const subject = flow.findNode(graph.root);
  if (fitted >= READABLE_ZOOM || !subject) {
    void flow.fitView({ padding: FIT_PADDING, maxZoom: 1 });
    return;
  }
  const { position, dimensions } = subject;
  void flow.setCenter(position.x + dimensions.width / 2, position.y + dimensions.height / 2, { zoom: 1 });
}

/** fit shows the whole canvas, and keeps it fitted while the pane is resized. */
function fit(): void {
  isFollowing = true;
  void flow.fitView({ padding: FIT_PADDING, maxZoom: 1 });
}

/** describeMark returns a technology's mark in its tree: the subject, one of its chain, or a root. */
function describeMark(node: Node): string {
  if (!isTree.value) return "";
  if (node.id === graph.root) return "subject";
  if (chain.value.has(node.id)) return "chain";
  return node.count > 0 ? "root" : "";
}

/**
 * describeWire returns what a wire says where it touches the selection: what a link does and how much, or the block
 * a firing is written in, its delay and what it goes through.
 */
function describeWire(edge: Edge): string {
  const words = RELATIONS[edge.relation as keyof typeof RELATIONS]?.words;
  if (words) return [words, edge.amount].filter(Boolean).join(" ");
  const block = edge.steps?.at(-1)?.conditions?.[0];
  const via = edge.via?.length ? `via ${edge.via.join(", ")}` : "";
  return [[block?.text, block?.value].filter(Boolean).join(" "), edge.delay, via].filter(Boolean).join(" · ");
}

/** describeSteps returns the blocks a firing sits in as lines of words, for the wire's hover text. */
function describeSteps(edge: Edge): string {
  const say = (conditions: Condition[] | null | undefined, depth: number): string[] =>
    (conditions ?? []).flatMap((condition) => [
      "  ".repeat(depth) +
        [condition.negated && "not", condition.subject, condition.text, condition.title || condition.value]
          .filter(Boolean)
          .join(" "),
      ...say(condition.conditions, depth + 1),
    ]);
  return (edge.steps ?? []).flatMap((step) => say(step.conditions, 0)).join("\n");
}

/**
 * click selects a node, opens a fold, a group or a node the depth stopped at, or follows a marker. The subject is
 * never the selection: a click on it clears the selection, and the detail shows it again.
 */
function click(id: string): void {
  const node = byId.value.get(id);
  if (!node) return;
  const shape = shapeOf(node, kind, graph.root);
  if (isTree.value) emit("ask", id);
  else if (id === graph.root) emit("select", "");
  else if (shape === "fold" || shape === "loop") open(id, node.parent);
  else if (shape === "rest") emit("select", node.parent);
  else if (shape === "marker") emit("select", `${node.database}:${node.name}`);
  else if (shape === "group" || node.deep) open(id, id);
  else emit("select", id);
}

/** open opens a fold, a group or a node, and holds the given node where it is on screen through the new layout. */
function open(id: string, anchor: string): void {
  held = anchor;
  emit("open", id);
}

/** ask makes a node the subject, where it is a definition. */
function ask(id: string): void {
  const node = byId.value.get(id);
  if (node?.site.place.path && id !== graph.root) emit("ask", `${node.database}:${node.name}`);
}

defineExpose({ fit });
</script>

<template>
  <div class="canvas relative h-full w-full bg-(--bg-0)">
    <DefineWire v-slot="{ edge, line }">
      <BaseEdge :id="edge.id" :path="line.path" :marker-end="edge.markerEnd" />
      <path v-if="edge.data.conditions" :d="line.path" class="hover-area">
        <title>{{ edge.data.conditions }}</title>
      </path>
      <EdgeLabelRenderer v-if="edge.label">
        <span class="wire-label" :style="{ transform: `translate(-50%, -50%) translate(${line.x}px, ${line.y}px)` }">
          {{ edge.label }}
        </span>
      </EdgeLabelRenderer>
    </DefineWire>
    <VueFlow
      id="atlas"
      :nodes="boxes"
      :edges="wires"
      :nodes-draggable="false"
      :nodes-connectable="false"
      :elements-selectable="false"
      :nodes-focusable="false"
      :edges-focusable="false"
      :min-zoom="ZOOM_LIMITS.min"
      :max-zoom="ZOOM_LIMITS.max"
      class="h-full w-full"
      @node-click="({ node }) => click(node.id)"
      @node-double-click="({ node }) => ask(node.id)"
      @pane-click="emit('select', '')"
      @move-start="({ event }) => event?.sourceEvent && (isFollowing = false)"
    >
      <template #edge-wire="edge"><DrawnWire :edge="edge" :line="drawRoute(edge.data.route ?? [])" /></template>
      <template #node-atlas="{ data }: { data: Drawn }">
        <AtlasNode
          v-bind="data"
          :is-subject="data.node.id === graph.root && !isTree"
          :is-selected="data.node.id === selected"
          :selected="selected"
          :is-down="isDown"
          @pick="emit('pick', $event)"
          @select="emit('select', $event)"
          @ask="emit('ask', $event)"
        />
      </template>
      <template #node-region="{ data }: { data: Region }">
        <div class="region" :style="{ width: `${data.width}px`, height: `${data.height}px` }">
          <b>{{ data.title }}</b>
          <template v-if="data.title && data.summary"> · </template>{{ data.summary }}
        </div>
      </template>
      <template #node-words="{ data }: { data: Heading }">
        <div class="pointer-events-none max-w-80 truncate text-12 leading-4 whitespace-nowrap text-(--fg-3)">
          <b class="font-semibold text-(--fg-2)">{{ data.title }}</b>
          <template v-if="data.summary"> · {{ data.summary }}</template>
        </div>
      </template>
      <template #node-line="{ data }: { data: string }">
        <div class="pointer-events-none w-41 truncate ps-1 text-11 leading-4 text-(--fg-3)" :title="data">
          {{ data }}
        </div>
      </template>
      <MiniMap
        :width="MINIMAP.width"
        :height="MINIMAP.height"
        :node-color="
          (box) => (box.type !== 'atlas' ? 'transparent' : box.id === graph.root ? 'var(--link)' : 'var(--line-2)')
        "
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
      class="absolute start-3 bottom-3 flex max-w-[calc(100%-15rem)] items-center gap-3.5 overflow-hidden rounded-lg border border-default bg-muted px-2.5 py-1.5 text-11 whitespace-nowrap text-dimmed"
    >
      <span v-for="row in LEGENDS[kind]" :key="row.label" class="flex items-center gap-1.5">
        <span :class="row.sample" />{{ row.label }}
      </span>
    </div>
    <div class="absolute end-43 bottom-3 flex items-end gap-1.5">
      <UBadge v-if="full" :label="`${zoom}%`" />
      <div class="flex flex-col gap-1">
        <UButton icon="i-lucide-plus" size="sm" square aria-label="Zoom in" @click="flow.zoomIn()" />
        <UButton icon="i-lucide-minus" size="sm" square aria-label="Zoom out" @click="flow.zoomOut()" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.canvas {
  background-image: radial-gradient(var(--line-1) 1px, transparent 1px);
  background-size: 20px 20px;
}
.canvas :deep(.vue-flow__node) {
  cursor: pointer;
}
.canvas :deep(.vue-flow__node.unplaced) {
  visibility: hidden;
}
.canvas :deep(.vue-flow__node-words),
.canvas :deep(.vue-flow__node-line),
.canvas :deep(.vue-flow__node-region) {
  cursor: default;
  pointer-events: none;
}
.region {
  padding: 9px 14px;
  border: 1px solid var(--line-1);
  border-radius: 12px;
  background: color-mix(in srgb, var(--fg-1) 2.5%, transparent);
  color: var(--fg-3);
  font-size: 12px;
  line-height: 16px;
  white-space: nowrap;
}
.region b {
  color: var(--fg-1);
  font-size: 13px;
  font-weight: 600;
}
.canvas :deep(.vue-flow__handle) {
  opacity: 0;
}
.canvas :deep(.vue-flow__edge-path) {
  stroke: var(--line-2);
  stroke-width: 1.5;
}
.canvas :deep(.vue-flow__edge.makes .vue-flow__edge-path) {
  stroke: var(--blue);
}
.canvas :deep(.vue-flow__edge.boosts .vue-flow__edge-path) {
  stroke: var(--purple);
}
.canvas :deep(.vue-flow__edge.lit .vue-flow__edge-path) {
  stroke: var(--link);
  stroke-width: 2;
}
.hover-area {
  fill: none;
  stroke: transparent;
  stroke-width: 12;
}
.canvas :deep(.vue-flow__edge.delayed .vue-flow__edge-path),
.canvas :deep(.vue-flow__edge.second .vue-flow__edge-path) {
  stroke-dasharray: 6 4;
}
.canvas :deep(.vue-flow__edge.via .vue-flow__edge-path) {
  stroke-dasharray: 1.5 3.5;
  stroke-linecap: round;
}
.canvas :deep(.vue-flow__edge.dim) {
  opacity: 0.35;
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
.canvas :deep(.vue-flow__minimap) {
  inset: auto 12px 12px auto;
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--line-1);
  border-radius: 8px;
  background: var(--bg-2);
}
</style>
