/**
 * The graph's layout: Go's nodes and edges laid out left to right by step, as the canvas draws them. Nothing else
 * about the graph is computed here; the reach, the steps and the splicing are Go's.
 */
import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { Graph as Dagre, layout } from "@dagrejs/dagre";
import { groupBy } from "es-toolkit";
import type { Edge as FlowEdge, Node as FlowNode } from "@vue-flow/core";

import type { Edge, Graph } from "@services/graph/models";
import { plural } from "../../lib/format";
import { useSettingsStore } from "../../stores/settings";

/** How the nodes of a database are drawn: a card or a pill, filled or outline, in a color with an icon. */
type Look = { shape: "card" | "pill"; filled: boolean; color: string; icon: string };

/** The size of a card, which has a title line, and of a pill, at the page's text size. */
const BOXES = { card: { width: 196, height: 56 }, pill: { width: 176, height: 26 } };
const ACTIONS: Look = { shape: "pill", filled: true, color: "var(--green)", icon: "i-lucide-timer" };
/** Each database's look, by Go's database id. */
const LOOKS: Record<string, Look> = {
  events: { shape: "card", filled: false, color: "var(--blue)", icon: "i-lucide-scroll" },
  on_action: ACTIONS,
  on_actions: ACTIONS,
  decisions: { shape: "pill", filled: false, color: "var(--orange)", icon: "i-lucide-gavel" },
  scripted_effects: { shape: "pill", filled: false, color: "var(--purple)", icon: "i-lucide-zap" },
};
const QUIET: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-box" };

/** Looks up how the nodes of a database are drawn. A database without a row gets a quiet outline pill. */
export function lookupLook(database: string): Look {
  return LOOKS[database] ?? QUIET;
}

/** Lays a graph out. The result follows the graph and the interface scale when they change. */
export function useEventGraph(graph: MaybeRefOrGetter<Graph | undefined>): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const settings = useSettingsStore();
  const flow = computed(() => buildLayout(toValue(graph), Number(settings.value("appearance.scale")) / 100 || 1));
  return {
    get nodes() {
      return flow.value.nodes;
    },
    get edges() {
      return flow.value.edges;
    },
  };
}

/**
 * Builds the canvas's nodes at the places dagre gives them, a head over each column, and the edges with their labels
 * and, for a fire that goes back, the route dagre gives it. Every fire forward is a column at least, a scripted
 * effect's included, so a column's head counts the columns from the root's.
 */
function buildLayout(graph: Graph | undefined, unit: number): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const nodes = graph?.nodes ?? [];
  const edges = graph?.edges ?? [];
  const dagre = new Dagre();
  dagre.setGraph({ rankdir: "LR", nodesep: 12 * unit, ranksep: 60 * unit, marginx: 20 * unit, marginy: 40 * unit });
  dagre.setDefaultEdgeLabel(() => ({}));
  for (const node of nodes) {
    const box = BOXES[lookupLook(node.database).shape];
    dagre.setNode(node.id, { width: box.width * unit, height: box.height * unit });
  }
  // an edge spans the steps between its ends, one at least; one that goes back to an earlier step is turned around
  const steps = new Map(nodes.map((node) => [node.id, node.step]));
  const spans = edges.map((edge) => (steps.get(edge.to) ?? 0) - (steps.get(edge.from) ?? 0));
  edges.forEach((edge, position) => {
    if (spans[position] < 0) dagre.setEdge(edge.to, edge.from, { minlen: -spans[position] });
    else if (edge.from !== edge.to) dagre.setEdge(edge.from, edge.to, { minlen: Math.max(spans[position], 1) });
  });
  layout(dagre);
  const positions = nodes.map((node) => {
    const { x, y, width, height } = dagre.node(node.id);
    return { node, column: Math.round(x), position: { x: x - width / 2, y: y - height / 2 } };
  });
  const columns = [...new Set(positions.map(({ column }) => column))].sort((first, second) => first - second);
  const rootColumn = columns.indexOf(positions.find(({ node }) => node.id === graph?.root)?.column ?? 0);
  const top = Math.min(...positions.map(({ position }) => position.y)) - 28 * unit;
  return {
    nodes: [
      ...Object.entries(groupBy(positions, ({ column }) => column)).map(([column, members]) => ({
        id: `head:${column}`,
        type: "head",
        position: { x: Math.min(...members.map(({ position }) => position.x)), y: top },
        data: { label: describeStep(columns.indexOf(Number(column)) - rootColumn) },
        selectable: false,
      })),
      ...positions.map(({ node, position }) => ({
        id: node.id,
        type: "graph",
        position,
        data: node,
        class: node.id === graph?.root ? "root" : "",
      })),
    ],
    edges: edges.map((edge, position) => {
      // a fire back leaves its source's left side and meets its target's right, through dagre's bends between
      const bends = spans[position] < 0 ? (dagre.edge(edge.to, edge.from)?.points ?? []).slice(1, -1).reverse() : [];
      const [source, target] = [dagre.node(edge.from), dagre.node(edge.to)];
      return {
        id: `${edge.from}>${edge.to}:${position}`,
        source: edge.from,
        target: edge.to,
        type: "fire",
        label: describeEdge(edge),
        data: {
          back: spans[position] < 0,
          points: bends.length
            ? [
                { x: source.x - source.width / 2, y: source.y },
                ...bends,
                { x: target.x + target.width / 2, y: target.y },
              ]
            : [],
        },
      };
    }),
  };
}

/** Describes a column by its place from the root's: those before it fire the root. */
function describeStep(step: number): string {
  if (step === 0) return "Root";
  if (step === -1) return "Fired by";
  return step < 0 ? `${-step} steps before` : plural(step, "step");
}

/** Describes an edge in words: its delay and the scripted effects it passes through. It is empty for neither. */
function describeEdge(edge: Edge): string {
  return [edge.days, edge.via?.length ? `via ${edge.via.join(", ")}` : ""].filter(Boolean).join(" · ");
}
