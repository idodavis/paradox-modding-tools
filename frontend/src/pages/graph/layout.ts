/** How the graph's nodes look and where they go. ELK lays them out left to right, a column for each step. */
import ELK, { type ElkNode, type ElkPoint, type ElkPort } from "elkjs/lib/elk-api";
import workerUrl from "elkjs/lib/elk-worker.min.js?url";
import { groupBy } from "es-toolkit";
import type { Edge as FlowEdge, Node as FlowNode } from "@vue-flow/core";

import type { Edge, Graph, Node, Row as GraphRow } from "@services/graph/models";
import { plural } from "../../lib/format";

/** Look is how a database's nodes are drawn. */
export type Look = { shape: "frame" | "toast" | "pill"; filled: boolean; color: string; icon: string };

/** Row is one line of an open node: Go's row with an id and whether the selection lights it. */
export type Row = GraphRow & { id: string; lit: boolean };

/** Options are what a layout is built for: which nodes open ("all" or only the route), the selection and text size. */
export type Options = { branches: string; selected: string; unit: number };

/** Lit is what a selection lights: the selected node, its route back to the root, and the nodes it touches. */
type Lit = { selected: string; route: Set<string>; near: Set<string> };

/** Drawn is a node with its look, whether it is open, and its rows. */
export type Drawn = { node: Node; look: Look; open: boolean; rows: Row[] };

/** SIZES holds node sizes in pixels at text size 1; an open node's padding is the space around its rows. */
const SIZES = {
  frame: { width: 196, height: 97 },
  toast: { width: 196, height: 46 },
  pill: { width: 176, height: 26 },
  open: { width: 236, head: 46, line: 16, row: 22, padding: 9 },
};
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
/** LAYOUT is ELK's options: layered, left to right, each step a partition. */
const LAYOUT = {
  "elk.algorithm": "layered",
  "elk.direction": "RIGHT",
  "elk.partitioning.activate": "true",
  "elk.layered.layering.strategy": "LONGEST_PATH_SOURCE",
  "elk.edgeRouting": "POLYLINE",
  "elk.layered.nodePlacement.bk.fixedAlignment": "BALANCED",
};

// ELK runs in a worker, so a large graph never holds the page up.
const elk = new ELK({ workerUrl });

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

/** listDrawn lists every node of a graph with its look, whether it is open and its rows. A fold never opens. */
function listDrawn(graph: Graph, options: Options, lit: Lit | null): Drawn[] {
  const route = lit?.route ?? new Set([graph.root]);
  return (graph.nodes ?? []).map((node) => {
    const open = !node.count && (options.branches === "all" ? !!node.fires?.length : route.has(node.id));
    const rows = open ? lightRows(node, lit) : [];
    return { node, look: lookupLook(node.database), open, rows };
  });
}

/** lightRows lists a node's rows with their ids, lighting those with a lit firing and the header above them. */
function lightRows(node: Node, lit: Lit | null): Row[] {
  const rows = (node.rows ?? []).map((row, position) => ({ ...row, id: `row:${position}`, lit: false }));
  if (!lit) return rows;
  let header: Row | undefined;
  for (const row of rows) {
    if (row.header) header = row;
    else if (!row.nested) header = undefined;
    row.lit = !!row.fires?.some((fire) => isLit(lit, node.id, fire.to));
    if (row.lit && header) header.lit = true;
  }
  return rows;
}

/** isLit reports whether a firing from one node to another is lit by the selection. */
function isLit(lit: Lit, source: string, target: string): boolean {
  return [source, target].includes(lit.selected) || (lit.route.has(source) && lit.route.has(target));
}

/** measureNode returns a node's size at text size 1. */
export function measureNode(drawn: Drawn): { width: number; height: number } {
  const { node, look, open, rows } = drawn;
  if (!open) return node.count ? SIZES.pill : SIZES[look.shape];
  const { width, row, padding } = SIZES.open;
  return { width, height: measureHead(drawn) + (rows.length ? padding + rows.length * row : 0) + 2 };
}

/** measureHead returns the height of an open node's head. */
function measureHead({ node }: Drawn): number {
  const { head, line } = SIZES.open;
  return head + ((node.title && node.display === "window" ? 1 : 0) + (node.comment ? 1 : 0)) * line;
}

/** buildLayout builds the canvas's nodes and edges at the places ELK gives them, lighting the selection's route. */
export async function buildLayout(graph: Graph, options: Options): Promise<{ nodes: FlowNode[]; edges: FlowEdge[] }> {
  const lit = findLit(graph, options.selected);
  const drawn = listDrawn(graph, options, lit);
  const placed = await placeNodes(graph, drawn, options.unit);
  const boxes = new Map((placed.children ?? []).map((box) => [box.id, box]));
  const positions = drawn.map((item) => {
    const { x = 0, y = 0 } = boxes.get(item.node.id) ?? {};
    return { item, position: { x, y } };
  });
  const top = Math.min(...positions.map(({ position }) => position.y)) - 28 * options.unit;
  // ELK starts a column's nodes at one x.
  const heads = Object.entries(groupBy(positions, ({ position }) => Math.round(position.x))).map(([x, members]) => ({
    id: `head:${x}`,
    type: "head",
    position: { x: Number(x), y: top },
    data: { label: describeColumn(Math.min(...members.map(({ item }) => item.node.step))) },
    selectable: false,
  }));
  const nodes = positions.map(({ item, position }) => ({
    id: item.node.id,
    type: "graph",
    position,
    data: item,
    class: [
      item.node.id === graph.root && "root",
      item.node.id === options.selected && "selected",
      lit && !lit.near.has(item.node.id) && "dim",
    ],
  }));
  const routes = new Map((placed.edges ?? []).map((edge) => [edge.id, edge.sections?.[0]]));
  return { nodes: [...heads, ...nodes], edges: buildEdges(graph, drawn, routes, lit) };
}

/** placeNodes lays a graph's nodes out with ELK, each in the partition of its step. */
function placeNodes(graph: Graph, drawn: Drawn[], unit: number): Promise<ElkNode> {
  const first = Math.min(...drawn.map((item) => item.node.step));
  const byId = new Map(drawn.map((item) => [item.node.id, item]));
  const children = drawn.map((item) => {
    const { width, height } = measureNode(item);
    return {
      id: item.node.id,
      width: width * unit,
      height: height * unit,
      layoutOptions: {
        "elk.partitioning.partition": String(item.node.step - first),
        "elk.portConstraints": "FIXED_POS",
        "elk.alignment": "LEFT",
      },
      ports: listPorts(item, unit),
    };
  });
  const edges = listSpans(graph)
    .filter(({ edge }) => edge.from !== edge.to)
    .map(({ edge, id }) => {
      const handle = findHandle(byId.get(edge.from), edge);
      return { id, sources: [handle ? `${edge.from}|${handle}` : edge.from], targets: [edge.to] };
    });
  const spacing = {
    "elk.spacing.nodeNode": String(12 * unit),
    "elk.layered.spacing.nodeNodeBetweenLayers": String(60 * unit),
    "elk.padding": `[top=${40 * unit},left=${20 * unit},bottom=${40 * unit},right=${20 * unit}]`,
  };
  return elk.layout({ id: "graph", layoutOptions: { ...LAYOUT, ...spacing }, children, edges });
}

/** listPorts lists the ports of an open node: one for each row that fires, and the node's own. */
function listPorts(drawn: Drawn, unit: number): ElkPort[] {
  if (!drawn.open) return [];
  const { width, height } = measureNode(drawn);
  const { row: rowHeight, padding } = SIZES.open;
  const top = measureHead(drawn) + padding / 2;
  const ports = drawn.rows.flatMap((row, index) =>
    row.fires?.length ? [{ id: `${drawn.node.id}|${row.id}`, y: top + (index + 0.5) * rowHeight }] : [],
  );
  ports.push({ id: `${drawn.node.id}|node`, y: height / 2 });
  return ports.map(({ id, y }) => ({
    id,
    x: width * unit,
    y: y * unit,
    width: 0,
    height: 0,
    layoutOptions: { "elk.port.side": "EAST" },
  }));
}

/** findHandle returns the id of the handle an edge leaves its firer by, or undefined if the firer is closed. */
function findHandle(firer: Drawn | undefined, edge: Edge): string | undefined {
  if (!firer?.open) return undefined;
  return firer.rows.find((row) => row.fires?.some((fire) => isSameFiring(fire, edge)))?.id ?? "node";
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

/** buildEdges builds the canvas's edges. An edge that goes back follows ELK's route around the nodes between. */
function buildEdges(
  graph: Graph,
  drawn: Drawn[],
  routes: Map<string, { startPoint: ElkPoint; bendPoints?: ElkPoint[]; endPoint: ElkPoint } | undefined>,
  lit: Lit | null,
): FlowEdge[] {
  const byId = new Map(drawn.map((item) => [item.node.id, item]));
  return listSpans(graph).map(({ edge, span, id }) => {
    const firer = byId.get(edge.from);
    const touches = !!lit && [edge.from, edge.to].includes(lit.selected);
    const route = span < 0 ? routes.get(id) : undefined;
    const via = edge.via?.length ? `via ${edge.via.join(", ")}` : "";
    return {
      id,
      source: edge.from,
      target: edge.to,
      sourceHandle: findHandle(firer, edge),
      type: "fire",
      label: touches && !firer?.open ? [edge.delay, via].filter(Boolean).join(" · ") : "",
      class: lit && isLit(lit, edge.from, edge.to) ? "lit" : [lit && "dim", span < 0 && "back"],
      data: { points: route ? [route.startPoint, ...(route.bendPoints ?? []), route.endPoint] : [] },
    };
  });
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

/** isSameFiring reports whether a node's firing is the one a drawn edge stands for. */
function isSameFiring(fire: Edge, edge: Edge): boolean {
  return fire.from === edge.from && fire.site.line === edge.site.line && fire.site.place.path === edge.site.place.path;
}

/** describeColumn labels a column by its step from the root. */
function describeColumn(step: number): string {
  if (step === 0) return "Root";
  if (step === -1) return "Fired by";
  return step < 0 ? `${-step} steps before` : plural(step, "step");
}
