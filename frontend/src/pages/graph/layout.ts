/**
 * The graph's looks and its layout. ELK lays Go's nodes out left to right, a column for each step, each open node as
 * tall as its rows of firings and each row a port, so the nodes a row fires line up in the order of the rows.
 */
import ELK, { type ElkNode, type ElkPoint, type ElkPort } from "elkjs/lib/elk-api";
import workerUrl from "elkjs/lib/elk-worker.min.js?url";
import { groupBy } from "es-toolkit";
import type { Edge as FlowEdge, Node as FlowNode } from "@vue-flow/core";

import type { Edge, Graph, Node, Step } from "@services/graph/models";
import { plural } from "../../lib/format";

/** How the nodes of a database are drawn: a frame, a toast or a pill, filled or outline, in a color with an icon. */
export type Look = { shape: "frame" | "toast" | "pill"; filled: boolean; color: string; icon: string };

/**
 * A Row is one line of an open node: the firings written in the same blocks, a block the rows under it share, or the
 * fold of the firings past the fan-out.
 */
export type Row = {
  id: string;
  /** the block that decides the row's firings, or the one a header row stands for; none for a firing written plainly */
  step?: Step;
  scopes: Step[]; // the links to other scopes its firings are written in
  fires: Edge[];
  nested: boolean; // under a header row
  header: boolean;
  more: number; // the firings a fold row stands for; 0 for any other row
  delay: string; // the delay of its first firing, in words
  lit: boolean; // one of its firings is lit, as isLit says, or it heads a row with one
};

/** What a layout is built for: which nodes open, the node selected, the fan-out, the folds opened and the text size. */
export type Options = { branches: string; selected: string; fanout: number; expand: string[]; unit: number };

/** The nodes a selection lights: the one selected, its route back to the root, and the nodes it touches. */
type Lit = { selected: string; route: Set<string>; near: Set<string> };

/** The data each node of the canvas draws from. */
export type Drawn = { node: Node; look: Look; open: boolean; rows: Row[] };

/** The sizes of a node at the page's text size: a frame with its scene, a toast, a pill, and an open node's parts. */
const SIZES = {
  frame: { width: 196, height: 97 },
  toast: { width: 196, height: 46 },
  pill: { width: 176, height: 26 },
  open: { width: 236, head: 46, line: 16, row: 22, rows: 9 },
};
/** The kinds of block that decide which of a definition's firings run. */
const DECIDING = ["branch", "weight", "list"];
const ACTIONS: Look = { shape: "pill", filled: true, color: "var(--green)", icon: "i-lucide-timer" };
/** Each database's look, by Go's database id. */
const LOOKS: Record<string, Look> = {
  events: { shape: "frame", filled: false, color: "var(--blue)", icon: "i-lucide-app-window" },
  messages: { shape: "toast", filled: false, color: "var(--green)", icon: "i-lucide-message-square" },
  on_action: ACTIONS,
  on_actions: ACTIONS,
  decisions: { shape: "pill", filled: false, color: "var(--orange)", icon: "i-lucide-gavel" },
  scripted_effects: { shape: "pill", filled: false, color: "var(--purple)", icon: "i-lucide-zap" },
};
const QUIET: Look = { shape: "pill", filled: false, color: "var(--fg-3)", icon: "i-lucide-box" };
/**
 * How ELK lays a graph out: left to right, each step a partition, each node in the first column it can take, the nodes
 * of a column centred on the wires into them. ELK's own routes are polylines, which need no room between the columns;
 * only a fire back draws one.
 */
const LAYOUT = {
  "elk.algorithm": "layered",
  "elk.direction": "RIGHT",
  "elk.partitioning.activate": "true",
  "elk.layered.layering.strategy": "LONGEST_PATH_SOURCE",
  "elk.edgeRouting": "POLYLINE",
  "elk.layered.nodePlacement.bk.fixedAlignment": "BALANCED",
};

// ELK runs in a worker, so a large graph never holds the page up
const elk = new ELK({ workerUrl });

/** Looks up how the nodes of a database are drawn. A database without a row gets a quiet outline pill. */
export function lookupLook(database: string): Look {
  return LOOKS[database] ?? QUIET;
}

/**
 * Lists the nodes from the selected one back to the root, in that order, through each node's parent: the root alone
 * without one.
 */
export function listRoute(graph: Graph, selected: string): string[] {
  const parents = new Map((graph.nodes ?? []).map((node) => [node.id, node.parent]));
  const route: string[] = [];
  for (let id: string | undefined = selected; id; id = parents.get(id)) route.push(id);
  return route.at(-1) === graph.root ? route : [...route, graph.root];
}

/**
 * Builds the drawn nodes of a graph, with the nodes the selection lights: each node's look, whether it opens, and the
 * rows it shows open, the rows whose firings are lit lit too.
 */
function listDrawn(graph: Graph, options: Options, lit: Lit | null): Drawn[] {
  const route = lit?.route ?? new Set([graph.root]);
  return (graph.nodes ?? []).map((node) => {
    const open = !node.count && (options.branches === "all" ? !!node.fires?.length : route.has(node.id));
    const rows = open ? listRows(node, options) : [];
    if (lit) lightRows(node.id, rows, lit);
    return { node, look: lookupLook(node.database), open, rows };
  });
}

/** Lights each row of a node with a firing that is lit, as isLit says, and the header row it sits under. */
function lightRows(id: string, rows: Row[], lit: Lit): void {
  let header: Row | undefined;
  for (const row of rows) {
    if (row.header) header = row;
    else if (!row.nested) header = undefined;
    row.lit = row.fires.some((fire) => isLit(lit, id, fire.to));
    if (row.lit && header) header.lit = true;
  }
}

/**
 * Reports whether a firing from one node to another is lit: it touches the node selected, or both its ends are on the
 * route from the selection back to the root.
 */
function isLit(lit: Lit, from: string, to: string): boolean {
  return [from, to].includes(lit.selected) || (lit.route.has(from) && lit.route.has(to));
}

/**
 * Lists the rows an open node shows, in the order its firings are written. Firings that one block decides, written in
 * the same blocks, are one row, and a firing written plainly is a row of its own. A row weighted or listed inside a
 * branch of an if sits under a header row for that branch. Past the fan-out, the rest are one fold row until the fold
 * is opened.
 */
export function listRows(node: Node, options: Pick<Options, "fanout" | "expand">): Row[] {
  const groups = Object.values(
    groupBy(node.fires ?? [], (fire, position) => {
      const steps = fire.steps ?? [];
      // a prefix keeps the keys from reading as numbers, which an object would sort ahead of the order written
      return steps.some((step) => DECIDING.includes(step.kind))
        ? `s${steps.map((step) => step.line).join(",")}`
        : `p${position}`;
    }),
  );
  const folded = options.fanout > 0 && groups.length > options.fanout && !options.expand.includes(`more:${node.id}`);
  const shown = folded ? groups.slice(0, options.fanout) : groups;
  const rows: Row[] = [];
  for (const fires of shown) {
    const steps = fires[0].steps ?? [];
    const step = steps.findLast((one) => DECIDING.includes(one.kind));
    const outer =
      step && step.kind !== "branch"
        ? steps.slice(0, steps.indexOf(step)).findLast((one) => one.kind === "branch")
        : undefined;
    if (outer && rows.findLast((row) => row.header)?.step?.line !== outer.line)
      rows.push(makeRow(rows, { step: outer, header: true }));
    const scopes = steps.filter((one) => one.kind === "scope");
    rows.push(makeRow(rows, { step, scopes, fires, nested: !!outer, delay: fires[0].delay ?? "" }));
  }
  const rest = groups.slice(shown.length).flat().length;
  if (rest) rows.push(makeRow(rows, { more: rest }));
  return rows;
}

/** Makes the next row of a node from the parts given, the rest empty. */
function makeRow(rows: Row[], parts: Partial<Row>): Row {
  return {
    id: `row:${rows.length}`,
    scopes: [],
    fires: [],
    nested: false,
    header: false,
    more: 0,
    delay: "",
    lit: false,
    ...parts,
  };
}

/** Measures a node as it draws: an open node grows with its title, its comment and its rows. */
export function measureNode(drawn: Drawn): { width: number; height: number } {
  const { node, look, open, rows } = drawn;
  if (!open) return node.count ? SIZES.pill : SIZES[look.shape];
  const { width, row, rows: padding } = SIZES.open;
  return { width, height: measureHead(drawn) + (rows.length ? padding + rows.length * row : 0) + 2 };
}

/** Measures an open node's head: its kind line and name, and its title and comment where it shows them. */
function measureHead({ node }: Drawn): number {
  const { head, line } = SIZES.open;
  return head + (Number(!!node.title && node.display === "window") + Number(!!node.comment)) * line;
}

/**
 * Builds the canvas's nodes at the places ELK gives them, with a head over each step's column, and the edges between
 * them. The selection lights its route and the nodes it touches, and dims the rest.
 */
export async function buildLayout(graph: Graph, options: Options): Promise<{ nodes: FlowNode[]; edges: FlowEdge[] }> {
  const lit = findLit(graph, options.selected);
  const drawn = listDrawn(graph, options, lit);
  const placed = await placeNodes(graph, drawn, options.unit);
  const boxes = new Map((placed.children ?? []).map((box) => [box.id, box]));
  const positions = drawn.map((one) => {
    const { x = 0, y = 0 } = boxes.get(one.node.id) ?? {};
    return { one, position: { x, y } };
  });
  const top = Math.min(...positions.map(({ position }) => position.y)) - 28 * options.unit;
  // ELK starts a column's nodes at one x; a column is headed by the step of its nodes
  const heads = Object.entries(groupBy(positions, ({ position }) => Math.round(position.x))).map(([x, members]) => ({
    id: `head:${x}`,
    type: "head",
    position: { x: Number(x), y: top },
    data: { label: describeColumn(Math.min(...members.map(({ one }) => one.node.step))) },
    selectable: false,
  }));
  const nodes = positions.map(({ one, position }) => ({
    id: one.node.id,
    type: "graph",
    position,
    data: one,
    class: [
      one.node.id === graph.root && "root",
      one.node.id === options.selected && "selected",
      lit && !lit.near.has(one.node.id) && "dim",
    ],
  }));
  const routes = new Map((placed.edges ?? []).map((edge) => [edge.id, edge.sections?.[0]]));
  return { nodes: [...heads, ...nodes], edges: buildEdges(graph, drawn, routes, lit) };
}

/**
 * Lays a graph's nodes out with ELK, each as large as it draws, in the partition of its step. An open node's rows are
 * ports on its right side where they draw; a firing leaves its row's port, else the node's own. A step takes a second
 * column where a node of it fires another of it, such as a scripted effect that the events it fires share a step with.
 */
function placeNodes(graph: Graph, drawn: Drawn[], unit: number): Promise<ElkNode> {
  const first = Math.min(...drawn.map((one) => one.node.step));
  const byId = new Map(drawn.map((one) => [one.node.id, one]));
  const children = drawn.map((one) => ({
    id: one.node.id,
    ...scale(measureNode(one), unit),
    layoutOptions: {
      "elk.partitioning.partition": String(one.node.step - first),
      "elk.portConstraints": "FIXED_POS",
      "elk.alignment": "LEFT",
    },
    ports: listPorts(one, unit),
  }));
  const edges = listSpans(graph)
    .filter(([edge]) => edge.from !== edge.to)
    .map(([edge, , id]) => {
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

/** Lists the ports of an open node: one on its right side for each row that fires, and the node's own in the middle. */
function listPorts(drawn: Drawn, unit: number): ElkPort[] {
  if (!drawn.open) return [];
  const { width, height } = measureNode(drawn);
  const { row, rows: padding } = SIZES.open;
  const top = measureHead(drawn) + padding / 2;
  const ports = drawn.rows
    .map((one, index) => ({
      id: `${drawn.node.id}|${one.id}`,
      y: top + (index + 0.5) * row,
      isFiring: !!one.fires.length,
    }))
    .filter((port) => port.isFiring)
    .map(({ id, y }) => ({ id, y }));
  return [...ports, { id: `${drawn.node.id}|node`, y: height / 2 }].map(({ id, y }) => ({
    id,
    x: width * unit,
    y: y * unit,
    width: 0,
    height: 0,
    layoutOptions: { "elk.port.side": "EAST" },
  }));
}

/** Scales a node's size by the page's text size. */
function scale(box: { width: number; height: number }, unit: number): { width: number; height: number } {
  return { width: box.width * unit, height: box.height * unit };
}

/**
 * Finds the handle a firing leaves its firer by: the row that writes it where the firer is open, else the open node's
 * own. A closed firer has one handle and gets none.
 */
function findHandle(firer: Drawn | undefined, edge: Edge): string | undefined {
  if (!firer?.open) return undefined;
  return firer.rows.find((row) => row.fires.some((fire) => isSameFiring(fire, edge)))?.id ?? "node";
}

/** Pairs each edge with the steps it spans, negative for an edge back to an earlier step, and with its id. */
function listSpans(graph: Graph): [Edge, number, string][] {
  const steps = new Map((graph.nodes ?? []).map((node) => [node.id, node.step]));
  return (graph.edges ?? []).map((edge, position) => [
    edge,
    (steps.get(edge.to) ?? 0) - (steps.get(edge.from) ?? 0),
    `${edge.from}>${edge.to}:${position}`,
  ]);
}

/**
 * Builds the canvas's edges, with the nodes the selection lights. An edge leaves the handle findHandle gives, and one
 * that goes back follows ELK's route around the nodes between, faded. An edge isLit says is lit is lit and the rest
 * dimmed; only an edge from a closed node that touches the selection keeps its label.
 */
function buildEdges(
  graph: Graph,
  drawn: Drawn[],
  routes: Map<string, { startPoint: ElkPoint; bendPoints?: ElkPoint[]; endPoint: ElkPoint } | undefined>,
  lit: Lit | null,
): FlowEdge[] {
  const byId = new Map(drawn.map((one) => [one.node.id, one]));
  return listSpans(graph).map(([edge, span, id]) => {
    const firer = byId.get(edge.from);
    const touches = !!lit && [edge.from, edge.to].includes(lit.selected);
    const route = span < 0 ? routes.get(id) : undefined;
    return {
      id,
      source: edge.from,
      target: edge.to,
      sourceHandle: findHandle(firer, edge),
      type: "fire",
      label: touches && !firer?.open ? describeEdge(edge) : "",
      class: lit && isLit(lit, edge.from, edge.to) ? "lit" : [lit && "dim", span < 0 && "back"],
      data: { points: route ? [route.startPoint, ...(route.bendPoints ?? []), route.endPoint] : [] },
    };
  });
}

/** Finds the nodes the selection lights: its route back to the root and the nodes it touches. None without one. */
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

/** Reports whether a node's firing is the one a drawn edge stands for: the same firer, file and line. */
function isSameFiring(fire: Edge, edge: Edge): boolean {
  return fire.from === edge.from && fire.line === edge.line && fire.place.path === edge.place.path;
}

/** Describes a column by its step from the root: those before it fire the root. */
function describeColumn(step: number): string {
  if (step === 0) return "Root";
  if (step === -1) return "Fired by";
  return step < 0 ? `${-step} steps before` : plural(step, "step");
}

/** Describes an edge in words: its delay and the scripted effects it passes through. It is empty for neither. */
function describeEdge(edge: Edge): string {
  return [edge.delay, edge.via?.length ? `via ${edge.via.join(", ")}` : ""].filter(Boolean).join(" · ");
}
