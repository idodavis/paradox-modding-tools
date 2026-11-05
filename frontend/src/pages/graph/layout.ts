/** How the graph's nodes look and where they go. Dagre lays them out left to right by what fires what. */
import dagre, { type Graph as DagreGraph } from "@dagrejs/dagre";
import type { Edge as FlowEdge, Node as FlowNode } from "@vue-flow/core";

import type { Edge, Graph, Node } from "@services/graph/models";
import { describeStep, writeConditions } from "./ConditionList.vue";

/** Look is how a database's nodes are drawn. */
export type Look = { shape: "frame" | "toast" | "pill"; filled: boolean; color: string; icon: string };

/** Options are what a layout is built for: the selection, text size, and the nodes whose loop firers are shown. */
export type Options = { selected: string; unit: number; loops: string[] };

/** Lit is what a selection lights: the selected node, its route back to the root, and the nodes it touches. */
type Lit = { selected: string; route: Set<string>; near: Set<string> };

/** Loop is what a loop item stands for: the firers that come round a loop, or a firing back into its own chain. */
export type Loop = { kind: "firers"; count: number } | { kind: "back" };

/** Drawn is one item of the canvas: a node with its look, or a loop item with the node it belongs to. */
export type Drawn = { id: string; node: Node; look: Look; loop?: Loop };

/** SIZES holds node sizes in pixels at text size 1. */
const SIZES = {
  frame: { width: 196, height: 97 },
  toast: { width: 196, height: 46 },
  pill: { width: 176, height: 26 },
};
/** SPACING is dagre's space between nodes in a rank, between ranks and between edges, at text size 1. */
const SPACING = { nodesep: 14, ranksep: 90, edgesep: 10 };
const ACTIONS: Look = { shape: "pill", filled: true, color: "var(--green)", icon: "i-lucide-timer" };
/** LOOKS is each database's look, by Go's database id. */
const LOOKS: Record<string, Look> = {
  events: { shape: "frame", filled: false, color: "var(--blue)", icon: "i-lucide-app-window" },
  messages: { shape: "toast", filled: false, color: "var(--green)", icon: "i-lucide-message-square" },
  on_action: ACTIONS,
  on_actions: ACTIONS,
  decisions: { shape: "pill", filled: false, color: "var(--orange)", icon: "i-lucide-gavel" },
  scripted_effects: { shape: "pill", filled: false, color: "var(--purple)", icon: "i-lucide-zap" },
};
const QUIET: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-box" };
/** LOOP_PILL starts the id of a loop's pill, before the id of the node its firers enter by. */
export const LOOP_PILL = "loop:";
/** LOOP is the look of a loop item. */
const LOOP: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-repeat" };

/** lookupLook returns how the nodes of a database are drawn. */
export function lookupLook(database: string): Look {
  return LOOKS[database] ?? QUIET;
}

/** listRoute lists the node ids from the selected node up through each parent to the root. */
export function listRoute(graph: Graph, selected: string): string[] {
  const parents = new Map((graph.nodes ?? []).map((node) => [node.id, node.parent]));
  const route: string[] = [];
  for (let id: string | undefined = selected; id; id = parents.get(id)) route.push(id);
  return route.at(-1) === graph.root ? route : [...route, graph.root];
}

/** measureNode returns a node's size at text size 1. A fold and a loop item are pills. */
function measureNode({ node, look, loop }: Drawn): { width: number; height: number } {
  return loop || node.count ? SIZES.pill : SIZES[look.shape];
}

/** buildLayout builds the canvas's nodes and edges at the places dagre gives them, lighting the selection's route. */
export function buildLayout(whole: Graph, options: Options): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const { graph, drawn } = foldLoops(whole, options.loops);
  const lit = findLit(graph, options.selected);
  const placed = placeNodes(graph, drawn, options.unit);
  const nodes = drawn.map((item) => {
    // dagre places a node by its center, Vue Flow by its corner
    const { x = 0, y = 0, width = 0, height = 0 } = placed.node(item.id) ?? {};
    return {
      id: item.id,
      type: "graph",
      position: { x: x - width / 2, y: y - height / 2 },
      data: item,
      class: [
        item.id === graph.root && "root",
        item.id === options.selected && "selected",
        lit && !lit.near.has(item.id) && "dim",
      ],
    };
  });
  return { nodes, edges: buildEdges(graph, placed, lit) };
}

/**
 * foldLoops lists what the canvas draws. A firer that fires the root only round a loop of the root's own chain folds
 * into a pill joined to the node it enters by, unless that node's loop firers are shown. A firing back into its own
 * chain ends in a marker naming the node it fires, in place of an edge back across the graph.
 */
function foldLoops(whole: Graph, loops: string[]): { graph: Graph; drawn: Drawn[] } {
  const kept = new Map<string, Node>();
  const folded = new Map<string, number>(); // how many looped firers stay folded, by the node they enter by
  // the reach adds a node after the one it was reached from
  for (const node of whole.nodes ?? []) {
    if (!node.looped || (kept.has(node.parent) && loops.includes(node.parent))) kept.set(node.id, node);
    else if (kept.has(node.parent)) folded.set(node.parent, (folded.get(node.parent) ?? 0) + 1);
  }
  const tree = { ...whole, nodes: [...kept.values()] };
  const drawn: Drawn[] = tree.nodes.map((node) => ({ id: node.id, node, look: lookupLook(node.database) }));
  const edges: Edge[] = [];
  for (const edge of whole.edges ?? []) {
    const source = kept.get(edge.from);
    const target = kept.get(edge.to);
    if (!source || !target) continue;
    const isBack = source.step >= 0 && edge.from !== edge.to && listRoute(tree, edge.from).includes(edge.to);
    if (!isBack) {
      edges.push(edge);
      continue;
    }
    const id = `back:${edges.length}`;
    drawn.push({ id, node: target, look: LOOP, loop: { kind: "back" } });
    edges.push({ ...edge, to: id });
  }
  for (const [entry, count] of folded) {
    const node = kept.get(entry);
    if (!node) continue;
    const id = LOOP_PILL + entry;
    drawn.push({ id, node, look: LOOP, loop: { kind: "firers", count } });
    edges.push({ from: id, to: entry, site: node.site });
  }
  return { graph: { ...tree, edges }, drawn };
}

/** placeNodes lays a graph's nodes out with dagre. */
function placeNodes(graph: Graph, drawn: Drawn[], unit: number): DagreGraph {
  const placed = new dagre.graphlib.Graph({ multigraph: true });
  placed.setGraph({
    rankdir: "LR",
    nodesep: SPACING.nodesep * unit,
    ranksep: SPACING.ranksep * unit,
    edgesep: SPACING.edgesep * unit,
    marginx: 20 * unit,
    marginy: 40 * unit,
  });
  for (const item of drawn) {
    const { width, height } = measureNode(item);
    placed.setNode(item.id, { width: width * unit, height: height * unit });
  }
  for (const { edge, id } of listSpans(graph)) {
    if (edge.from !== edge.to) placed.setEdge(edge.from, edge.to, {}, id);
  }
  dagre.layout(placed);
  return placed;
}

/** listSpans lists each edge with its id and the steps it spans, negative for an edge going back. */
function listSpans(graph: Graph): { edge: Edge; span: number; id: string }[] {
  const steps = new Map((graph.nodes ?? []).map((node) => [node.id, node.step]));
  return (graph.edges ?? []).map((edge, position) => ({
    edge,
    span: (steps.get(edge.to) ?? 0) - (steps.get(edge.from) ?? 0),
    id: `${edge.from}>${edge.to}:${position}`,
  }));
}

/** describeLabel returns an edge's label: its delay and the events it goes via. */
function describeLabel(edge: Edge): string {
  const via = edge.via?.length ? `via ${edge.via.join(", ")}` : "";
  return [edge.delay, via].filter(Boolean).join(" · ");
}

/**
 * buildEdges builds the canvas's edges. Each keeps the points dagre routes it through between ranks; its ends are the
 * nodes' handles. A firing that waits or goes through a macro is marked as delayed or via, so it draws dashed or
 * dotted. Its conditions in words are its hover text.
 */
function buildEdges(graph: Graph, placed: DagreGraph, lit: Lit | null): FlowEdge[] {
  return listSpans(graph).map(({ edge, span, id }) => {
    const touches = !!lit && [edge.from, edge.to].includes(lit.selected);
    const isRoute = !!lit && isLit(lit, edge.from, edge.to);
    const route = edge.from === edge.to ? undefined : placed.edge(edge.from, edge.to, id);
    return {
      id,
      source: edge.from,
      target: edge.to,
      type: "fire",
      label: touches ? describeLabel(edge) : "",
      class: [
        isRoute ? "lit" : lit && "dim",
        !isRoute && span < 0 && "back",
        edge.delay && "delayed",
        edge.via?.length && "via",
      ],
      data: {
        // dagre's first and last points sit on the nodes' boxes, and the canvas starts and ends at the handles. Dagre
        // also gives every edge a point halfway, where a label would sit; an edge between neighbouring ranks has only
        // that one, and it bends the curve, so such an edge keeps none.
        points: dropHalfway(route?.points?.slice(1, -1) ?? []),
        conditions: writeConditions((edge.steps ?? []).flatMap(describeStep)),
      },
    };
  });
}

/** dropHalfway returns an edge's inner points, or none if its only one is the halfway point of a short edge. */
function dropHalfway<T>(points: T[]): T[] {
  return points.length > 1 ? points : [];
}

/** isLit reports whether a firing from one node to another is lit by the selection. */
function isLit(lit: Lit, source: string, target: string): boolean {
  return [source, target].includes(lit.selected) || (lit.route.has(source) && lit.route.has(target));
}

/** findLit finds what a selection lights, or null if nothing is selected. */
function findLit(graph: Graph, selected: string): Lit | null {
  if (!selected) return null;
  const route = new Set(listRoute(graph, selected));
  const near = new Set(route);
  for (const edge of graph.edges ?? []) {
    if (edge.from === selected) near.add(edge.to);
    if (edge.to === selected) near.add(edge.from);
  }
  return { selected, route, near };
}
