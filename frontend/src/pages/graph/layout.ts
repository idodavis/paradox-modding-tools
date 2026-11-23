/** How the event graph's nodes look and where the layered layout puts them. */
import { type Edge as FlowEdge, type Node as FlowNode } from "@vue-flow/core";

import type { Edge, Graph, Node } from "@services/graph/models";
import { writeConditions } from "./ConditionList.vue";
import { placeLayered, type Placed } from "./layered";

/** Look is how a database's nodes are drawn. */
export type Look = { shape: "frame" | "toast" | "pill"; filled: boolean; color: string; icon: string };

/**
 * Options are what a layout is built for: the selection, text size, and the
 * nodes whose loop firers are shown.
 */
export type Options = { selected: string; unit: number; loops: string[] };

/** Flow is what the canvas draws: its nodes and the edges between them. */
export type Flow = { nodes: FlowNode[]; edges: FlowEdge[] };

/**
 * Lit is what a selection lights: the selected node, its route back to the
 * root, and the nodes it touches.
 */
type Lit = { selected: string; route: Set<string>; near: Set<string> };

/**
 * Loop is what a loop marker stands for: the firers that come round a loop, or
 * a firing back into its own chain.
 */
export type Loop = { marker: "firers"; count: number } | { marker: "back" };

/**
 * Drawn is one node of the event graph with its look, or a loop marker with
 * the node it belongs to.
 */
export type Drawn = { id: string; node: Node; look: Look; loop?: Loop };

/**
 * NODE_STATE is the classes that fade a node off the lit route and ring the
 * root in the second color and the selection in the primary.
 */
export const NODE_STATE =
  "in-[.dim]:opacity-60 in-[.root]:border-secondary in-[.root]:ring-3 in-[.root]:ring-(--link-soft) in-[.selected]:border-primary in-[.selected]:ring-3 in-[.selected]:ring-(--accent-soft)";

/** SIZES holds node sizes in pixels at text size 1. */
const SIZES = {
  frame: { width: 196, height: 97 },
  toast: { width: 196, height: 46 },
  pill: { width: 176, height: 26 },
};

/**
 * SPACING is the room between nodes of a layer, between layers and between
 * wires, and MARGIN the room round the drawing, all at text size 1.
 */
const SPACING = { nodes: 14, layers: 90, wires: 10 };
const MARGIN = { x: 20, y: 40 };

/** ACTIONS is the look of an on_action, whichever way the game names it. */
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
/** QUIET is the look of a database LOOKS lacks. */
const QUIET: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-box" };
/** ROOT_DISTANCE is the distance of the root. */
export const ROOT_DISTANCE = 0;
/** FOLD starts the id of a fold's node, as Go names it. */
export const FOLD = "more";
/**
 * LOOP_PILL starts the id of a loop's pill, before the id of the node its
 * firers enter by.
 */
export const LOOP_PILL = "loop:";
/** LOOP is the look of a loop marker. */
const LOOP: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-repeat" };

export function lookupLook(database: string): Look {
  return LOOKS[database] ?? QUIET;
}

/** isPastDepth reports whether the depth stopped at a node, never the root. */
export function isPastDepth(node: Node): boolean {
  return node.deep && node.distance !== ROOT_DISTANCE;
}

/**
 * listRoute lists the node ids from the selected node up through each parent
 * to the root.
 */
function listRoute(graph: Graph, selected: string): string[] {
  const parents = new Map((graph.nodes ?? []).map((node) => [node.id, node.parent]));
  const route: string[] = [];
  for (let id: string | undefined = selected; id; id = parents.get(id)) route.push(id);
  return route.at(-1) === graph.root ? route : [...route, graph.root];
}

/**
 * measureNode returns a node's size at text size 1. A fold and a loop marker
 * are pills.
 */
function measureNode({ node, look, loop }: Drawn): { width: number; height: number } {
  return loop || node.count ? SIZES.pill : SIZES[look.shape];
}

/**
 * buildLayout builds the canvas's nodes and edges at the places the layered
 * layout gives them, left to right at a text size, lighting the selection's
 * route. A wire from a node to itself is not laid out.
 */
export async function buildLayout(whole: Graph, { selected, unit, loops }: Options): Promise<Flow> {
  const { graph, drawn } = foldLoops(whole, loops);
  const lit = findLit(graph, selected);
  const cards = drawn.map((mark) => {
    const { width, height } = measureNode(mark);

    return { id: mark.id, width: width * unit, height: height * unit };
  });
  const links = listSpans(graph).flatMap(({ edge, id }) =>
    edge.from === edge.to ? [] : [{ id, from: edge.from, to: edge.to }],
  );
  const placed = await placeLayered(cards, links, {
    "elk.direction": "RIGHT",
    "elk.spacing.nodeNode": String(SPACING.nodes * unit),
    "elk.layered.spacing.nodeNodeBetweenLayers": String(SPACING.layers * unit),
    "elk.spacing.edgeEdge": String(SPACING.wires * unit),
    "elk.padding": `[top=${MARGIN.y * unit},left=${MARGIN.x * unit},bottom=${MARGIN.y * unit},right=${MARGIN.x * unit}]`,
  });
  const nodes = drawn.map((mark) => ({
    id: mark.id,
    type: "graph",
    position: placed.boxes[mark.id],
    data: mark,
    class: [
      mark.id === graph.root ? "root" : mark.id === selected && "selected",
      lit && !lit.near.has(mark.id) && "dim",
    ],
  }));
  return { nodes, edges: buildEdges(graph, placed, lit) };
}

/**
 * foldLoops lists what the canvas draws. A firer that fires the root only
 * round a loop of the root's own chain folds into a pill joined to the node it
 * enters by, unless that node's loop firers are shown. A firing back into its
 * own chain ends in a marker naming the node it fires, in place of an edge
 * back across the graph.
 */
function foldLoops(whole: Graph, loops: string[]): { graph: Graph; drawn: Drawn[] } {
  const kept = new Map<string, Node>();
  const foldedByEntry = new Map<string, number>();
  // Go lists a node after its parent, so a parent is kept first
  for (const node of whole.nodes ?? []) {
    if (!node.looped || (kept.has(node.parent) && loops.includes(node.parent))) kept.set(node.id, node);
    else if (kept.has(node.parent)) foldedByEntry.set(node.parent, (foldedByEntry.get(node.parent) ?? 0) + 1);
  }
  const tree = { ...whole, nodes: [...kept.values()] };
  const drawn: Drawn[] = tree.nodes.map((node) => ({ id: node.id, node, look: lookupLook(node.database) }));
  const edges: Edge[] = [];
  for (const edge of whole.edges ?? []) {
    const source = kept.get(edge.from);
    const target = kept.get(edge.to);
    if (!source || !target) continue;
    const isBack =
      source.distance >= ROOT_DISTANCE && edge.from !== edge.to && listRoute(tree, edge.from).includes(edge.to);
    if (!isBack) {
      edges.push(edge);
      continue;
    }
    const id = `back:${edges.length}`;
    drawn.push({ id, node: target, look: LOOP, loop: { marker: "back" } });
    edges.push({ ...edge, to: id });
  }
  for (const [entryId, count] of foldedByEntry) {
    const node = kept.get(entryId);
    if (!node) continue;
    const id = LOOP_PILL + entryId;
    drawn.push({ id, node, look: LOOP, loop: { marker: "firers", count } });
    edges.push({ from: id, to: entryId, site: node.site });
  }
  return { graph: { ...tree, edges }, drawn };
}

/**
 * listSpans lists each edge with its id and the distance it spans, negative
 * for an edge going back.
 */
function listSpans(graph: Graph): { edge: Edge; span: number; id: string }[] {
  const distances = new Map((graph.nodes ?? []).map((node) => [node.id, node.distance]));
  return (graph.edges ?? []).map((edge, position) => ({
    edge,
    span: (distances.get(edge.to) ?? 0) - (distances.get(edge.from) ?? 0),
    id: `${edge.from}>${edge.to}:${position}`,
  }));
}

/** describeLabel returns an edge's label: its delay and what it goes via. */
function describeLabel(edge: Edge): string {
  const via = edge.via?.length ? `via ${edge.via.join(", ")}` : "";
  return [edge.delay, via].filter(Boolean).join(" · ");
}

/**
 * buildEdges builds the canvas's edges. Each keeps the points the layout
 * routes it through, none for a wire from a node to itself. A firing that
 * waits or goes through a macro is marked as delayed or via, so it draws
 * dashed or dotted. Its conditions in words are its hover text.
 */
function buildEdges(graph: Graph, placed: Placed, lit: Lit | null): FlowEdge[] {
  return listSpans(graph).map(({ edge, span, id }) => {
    const touches = !!lit && [edge.from, edge.to].includes(lit.selected);
    const isRoute = !!lit && isLit(lit, edge.from, edge.to);
    return {
      id,
      source: edge.from,
      target: edge.to,
      type: "wire",
      label: touches ? describeLabel(edge) : "",
      class: [
        isRoute ? "lit" : lit && "dim",
        !isRoute && span < 0 && "back",
        edge.delay && "delayed",
        edge.via?.length && "via",
      ],
      data: {
        points: placed.routes[id] ?? [],
        conditions: writeConditions((edge.steps ?? []).flatMap((step) => step.conditions ?? [])),
      },
    };
  });
}

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
