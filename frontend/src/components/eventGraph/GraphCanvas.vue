<script setup lang="ts">
/**
 * The graph on its canvas: Vue Flow over the laid-out nodes, each a card under its kind's bar. With
 * a node selected, whatever is off a route from the root to it, but for its neighbours, dims.
 */
import { computed } from "vue";
import {
  Handle,
  MarkerType,
  Position,
  VueFlow,
  useVueFlow,
  type Edge as FlowEdge,
  type Node as FlowNode,
  type NodeMouseEvent,
} from "@vue-flow/core";
import { Background } from "@vue-flow/background";
import { Controls } from "@vue-flow/controls";
import { MiniMap } from "@vue-flow/minimap";
import "@vue-flow/minimap/dist/style.css";
import type { Edge, Node } from "@services/models";
import { NODE_HEIGHT, NODE_WIDTH, kindColour } from "../../composables/useEventGraph";
import { originHexByOriginId } from "../../ide/rootDecorations";
import { useWorkspaceStore } from "../../stores/workspace";

const props = defineProps<{
  nodes: Node[];
  edges: Edge[];
  positions: Map<string, { x: number; y: number }>;
  rootId: string;
  selectedId: string;
}>();
const emit = defineEmits<{ select: [id: string]; reroot: [id: string]; clear: [] }>();
const store = useWorkspaceStore();
const { fitView } = useVueFlow("event-graph");

const ICONS: Record<string, string> = {
  events: "i-lucide-scroll-text",
  on_action: "i-lucide-timer",
  scripted_effects: "i-lucide-zap",
};

// what stays lit: every route from the root to the selected node, and that node's neighbours
const lit = computed(() => {
  const selected = props.selectedId;
  if (!selected) return null;
  const reach = (start: string, forward: boolean) => {
    const reached = new Set([start]);
    for (let frontier = [start]; frontier.length; ) {
      const next: string[] = [];
      for (const edge of props.edges) {
        const [from, to] = forward ? [edge.from, edge.to] : [edge.to, edge.from];
        if (frontier.includes(from) && !reached.has(to)) {
          next.push(to);
          reached.add(to);
        }
      }
      frontier = next;
    }
    return reached;
  };
  const back = reach(selected, false);
  const route = new Set([...reach(props.rootId, true)].filter((id) => back.has(id)));
  const nodes = new Set([...route, selected]);
  for (const edge of props.edges) {
    if (edge.from === selected) nodes.add(edge.to);
    if (edge.to === selected) nodes.add(edge.from);
  }
  return { route, nodes };
});

const flowNodes = computed((): FlowNode<Node>[] =>
  props.nodes.map((node) => ({
    id: node.id,
    type: "event",
    position: props.positions.get(node.id) ?? { x: 0, y: 0 },
    data: node,
    class: lit.value && !lit.value.nodes.has(node.id) ? "dimmed" : "",
  })),
);

// a lit edge is labelled by the macro its source writes, with … when that macro fires through
// others, else by the firing key; the whole chain is in the detail's links
const flowEdges = computed((): FlowEdge[] =>
  props.edges.map((edge, i) => {
    const onRoute = lit.value?.route.has(edge.from) && lit.value.route.has(edge.to);
    const touches = edge.from === props.selectedId || edge.to === props.selectedId;
    const hops = edge.via.split(" → ");
    return {
      id: `${edge.from}->${edge.to}:${i}`,
      source: edge.from,
      target: edge.to,
      label: !lit.value || !(onRoute || touches) ? "" : hops.length > 1 ? `${hops[0]} …` : edge.via,
      labelBgPadding: [6, 3],
      labelBgBorderRadius: 4,
      markerEnd: MarkerType.ArrowClosed,
      class: [
        edge.from.startsWith("more") || edge.to.startsWith("more") ? "stub" : "",
        lit.value && (onRoute || touches) ? "lit" : "",
        lit.value && !onRoute && !touches ? "dimmed" : "",
      ].join(" "),
    };
  }),
);

/** A double-click draws the graph from a node; on a stub it opens it, as a click does. */
function reroot({ node }: NodeMouseEvent): void {
  if (node.data.stub) emit("select", node.id);
  else emit("reroot", node.id);
}

function cardClass(node: Node): string {
  if (node.id === props.selectedId) return "border-success ring-2 ring-success/50 shadow-lg";
  if (node.stub) return "border-dashed border-accented bg-transparent";
  return node.root ? "border-primary ring-1 ring-primary/50" : "border-default";
}
</script>

<template>
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
    @node-click="emit('select', $event.node.id)"
    @node-double-click="reroot"
    @pane-click="emit('clear')"
    @nodes-initialized="fitView({ padding: 0.15, minZoom: 0.75, maxZoom: 1 })"
  >
    <template #node-event="{ data }">
      <div
        class="overflow-hidden rounded-lg border bg-elevated text-left shadow-sm"
        :class="cardClass(data)"
        :style="{ width: `${NODE_WIDTH}px`, height: `${NODE_HEIGHT}px` }"
      >
        <Handle type="target" :position="Position.Left" :connectable="false" />
        <div v-if="data.stub" class="flex h-full items-center justify-center text-sm text-muted">{{ data.title }}</div>
        <template v-else>
          <div
            class="flex h-7 items-center gap-1.5 border-t-2 px-3 text-xs font-semibold"
            :style="{ backgroundColor: kindColour(data.kind, 12), borderColor: kindColour(data.kind, 100) }"
          >
            <UIcon
              :name="ICONS[data.kind] ?? 'i-lucide-box'"
              class="size-4 shrink-0"
              :style="{ color: kindColour(data.kind, 100) }"
            />
            <span class="truncate" :style="{ color: kindColour(data.kind, 100) }">{{ data.kind }}</span>
          </div>
          <div class="space-y-1 px-3 py-2">
            <div class="truncate text-base font-semibold text-highlighted">{{ data.id }}</div>
            <div v-if="data.title" class="truncate text-sm text-toned">{{ data.title }}</div>
            <div class="flex items-center gap-1.5 text-xs text-muted">
              <span
                class="size-2 shrink-0 rounded-full"
                :style="{ backgroundColor: originHexByOriginId(data.origin ?? '') }"
              />
              <span class="truncate">{{ store.originName(data.origin ?? "") }}</span>
            </div>
          </div>
        </template>
        <Handle type="source" :position="Position.Right" :connectable="false" />
      </div>
    </template>
    <Background />
    <MiniMap
      pannable
      zoomable
      :width="160"
      :height="110"
      mask-color="rgb(0 0 0 / 0.55)"
      node-color="var(--ui-border-accented)"
    />
    <Controls />
  </VueFlow>
</template>

<style scoped>
:deep(.vue-flow__edge-path) {
  stroke: var(--ui-border-accented);
  stroke-width: 1.5;
}
:deep(.vue-flow__edge.lit .vue-flow__edge-path) {
  stroke: var(--ui-success);
}
:deep(.vue-flow__edge.stub .vue-flow__edge-path) {
  stroke-dasharray: 6 4;
}
:deep(.vue-flow__edge-text) {
  fill: var(--ui-text-muted);
  font-size: 11px;
}
:deep(.vue-flow__edge-textbg) {
  fill: var(--ui-bg);
}
:deep(.vue-flow__node.dimmed),
:deep(.vue-flow__edge.dimmed) {
  opacity: 0.3;
}
:deep(.vue-flow__minimap) {
  background-color: var(--ui-bg-elevated);
  border: 1px solid var(--ui-border);
}
</style>
