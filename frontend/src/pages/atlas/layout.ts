/** Where each node of a canvas stands: ELK's layered layout for every canvas but a tree, and a tree's own rule. */
import ELK from "elkjs/lib/elk.bundled.js";
import type { ElkExtendedEdge } from "elkjs/lib/elk-api";

import type { Graph, Node } from "@services/atlas/models";
import { describeCount, plural } from "../../lib/format";

/** A Size is a node's measured width and height. */
export type Size = { width: number; height: number };
/** A Point is a place on the canvas. A node's is its top left corner. */
export type Point = { x: number; y: number };
/** A Tie is one wire the layered layout places, from one node to another. */
export type Tie = { from: string; to: string };
/** A Region is a named part of a tree, drawn as a panel behind its cards: a root's tree, an era or a block. */
export type Region = Point & Size & { id: string; title: string; summary: string };
/** A Slot is a card's place in its tree: how many steps from the root, and how far across in pixels. */
type Slot = { depth: number; cross: number };
/** A Forest is a tree's technologies as the layout reads them: each one's requirements, and what follows each. */
type Forest = { nodes: Node[]; blocks: Node[]; requirements: Map<string, string[]>; kids: Map<string, string[]> };

/** BLOCK starts the id of a block of the advances that no requirement ties to a tree. */
export const BLOCK = "block:";

/** LAYERED is the layered layout's settings: 14 between the nodes of a layer and 10 between wires. */
const LAYERED = {
  "elk.algorithm": "layered",
  "elk.direction": "RIGHT",
  "elk.edgeRouting": "ORTHOGONAL",
  "elk.layered.nodePlacement.bk.fixedAlignment": "BALANCED",
  "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
  "elk.layered.mergeEdges": "true",
  "elk.spacing.nodeNode": "14",
  "elk.spacing.edgeEdge": "10",
  "elk.spacing.edgeNode": "10",
  "elk.padding": "[top=40,left=20,bottom=40,right=20]",
};

/**
 * TREE is a tree's spacing in pixels: between a card and the one it leads to, between the cards of a column, the
 * strip a region's title takes, the room inside a region's edge, the gap between regions and the room round the
 * tree. A block's advances stand five to a row.
 */
const TREE = { main: 56, cross: 10, header: 34, pad: 14, gap: 28, margin: 24, row: 5 };
/** How round a wire's corners are. */
const CORNER = 5;

const elk = new ELK();

/**
 * placeLayers hands ELK's layered layout the nodes with their sizes and the ties between them, left to right, the
 * given gap between layers. It returns where each node stands, and the points each tie's wire runs through, in the
 * ties' order. A tie that names a node with no size has no wire.
 */
export async function placeLayers(
  sizes: ReadonlyMap<string, Size>,
  ties: readonly Tie[],
  gap: number,
): Promise<{ points: Map<string, Point>; routes: (Point[] | undefined)[] }> {
  const layout = await elk.layout({
    id: "canvas",
    layoutOptions: { ...LAYERED, "elk.layered.spacing.nodeNodeBetweenLayers": String(gap) },
    children: [...sizes].map(([id, size]) => ({ id, ...size })),
    edges: ties.flatMap((tie, position) =>
      sizes.has(tie.from) && sizes.has(tie.to) ? { id: String(position), sources: [tie.from], targets: [tie.to] } : [],
    ),
  });
  const routes: (Point[] | undefined)[] = ties.map(() => undefined);
  for (const edge of (layout.edges ?? []) as ElkExtendedEdge[]) {
    const section = edge.sections?.[0];
    if (section) routes[Number(edge.id)] = [section.startPoint, ...(section.bendPoints ?? []), section.endPoint];
  }
  return {
    points: new Map((layout.children ?? []).map((child) => [child.id, { x: child.x ?? 0, y: child.y ?? 0 }])),
    routes,
  };
}

/**
 * drawRoute returns the path of a wire through the given points, its corners rounded, and the middle of its longest
 * stretch, where its words stand.
 */
export function drawRoute(points: readonly Point[]): { path: string; x: number; y: number } {
  const [first, ...rest] = points;
  if (!first) return { path: "", x: 0, y: 0 };
  let path = `M${first.x},${first.y}`;
  let middle = first;
  let longest = -1;
  for (const [position, point] of rest.entries()) {
    const before = points[position] ?? point;
    const after = rest[position + 1];
    const length = Math.abs(point.x - before.x) + Math.abs(point.y - before.y);
    if (length > longest) [longest, middle] = [length, { x: (point.x + before.x) / 2, y: (point.y + before.y) / 2 }];
    if (!after) {
      path += `L${point.x},${point.y}`;
      continue;
    }
    const reach = Math.abs(after.x - point.x) + Math.abs(after.y - point.y);
    const radius = Math.min(CORNER, length / 2, reach / 2);
    const enter = {
      x: point.x + Math.sign(before.x - point.x) * radius,
      y: point.y + Math.sign(before.y - point.y) * radius,
    };
    const leave = {
      x: point.x + Math.sign(after.x - point.x) * radius,
      y: point.y + Math.sign(after.y - point.y) * radius,
    };
    path += `L${enter.x},${enter.y}Q${point.x},${point.y} ${leave.x},${leave.y}`;
  }
  return { path, ...middle };
}

/**
 * placeTree places a tree of cards of one size. It returns where each card stands, the tree's regions and the points
 * each edge's wire runs through, in the edges' order. A technology with nothing after it takes the next row, and one
 * with others after it stands at the middle of the first and the last of them, where "after" follows a technology's
 * first requirement. Where the game's trees start from roots, each root's tree is a region of its own, one under the
 * other, and the blocks follow. Otherwise each era is a region, in order, and a technology with no wire stands at
 * the foot of its era. Read top to bottom, a tree grows down from its root.
 */
export function placeTree(
  graph: Graph,
  card: Size,
  isDown: boolean,
): { points: Map<string, Point>; regions: Region[]; routes: (Point[] | undefined)[] } {
  const all = graph.nodes ?? [];
  const nodes = all.filter((node) => !node.id.startsWith(BLOCK));
  const ids = new Set(nodes.map((node) => node.id));
  const requirements = new Map<string, string[]>();
  for (const edge of graph.edges ?? []) {
    if (ids.has(edge.from) && ids.has(edge.to))
      requirements.set(edge.to, [...(requirements.get(edge.to) ?? []), edge.from]);
  }
  const kids = new Map<string, string[]>();
  for (const node of nodes) {
    const first = requirements.get(node.id)?.[0];
    if (first) kids.set(first, [...(kids.get(first) ?? []), node.id]);
  }
  const forest = { nodes, blocks: all.filter((node) => node.id.startsWith(BLOCK)), requirements, kids };
  const isRooted = forest.blocks.length > 0 || nodes.some((node) => node.root);
  const { points, regions } = isRooted ? placeRoots(forest, card, isDown) : placeEras(graph, forest, card, isDown);
  const routes = (graph.edges ?? []).map((edge) => {
    const [source, target] = [points.get(edge.from), points.get(edge.to)];
    if (!source || !target) return undefined;
    return routeWire(source, target, card, isDown, requirements.get(edge.to)?.[0] === edge.from);
  });
  return { points, regions, routes };
}

/**
 * routeWire returns the points of the wire between two cards of a tree. It leaves the middle of the first card's
 * right edge and enters the middle of the second card's left edge, or the bottom and the top in a tree read top to
 * bottom. It turns half a gap after the first card, so the wires to what follows one technology share a trunk; the
 * wire of a requirement that is not a technology's first turns half a gap before the second card.
 */
function routeWire(source: Point, target: Point, card: Size, isDown: boolean, isFirst: boolean): Point[] {
  const turn = TREE.main / 2;
  if (isDown) {
    const start = { x: source.x + card.width / 2, y: source.y + card.height };
    const end = { x: target.x + card.width / 2, y: target.y };
    const y = isFirst ? start.y + turn : end.y - turn;
    return [start, { x: start.x, y }, { x: end.x, y }, end];
  }
  const start = { x: source.x + card.width, y: source.y + card.height / 2 };
  const end = { x: target.x, y: target.y + card.height / 2 };
  const x = isFirst ? start.x + turn : end.x - turn;
  return [start, { x, y: start.y }, { x, y: end.y }, end];
}

/**
 * layTree lays the tree under one technology. It returns each card's slot, how deep the tree runs and how many rows
 * it takes.
 */
function layTree(
  head: string,
  kids: ReadonlyMap<string, string[]>,
  step: number,
): { slots: Map<string, Slot>; depth: number; rows: number } {
  const slots = new Map<string, Slot>();
  let rows = 0;
  let deepest = 0;
  const lay = (id: string, depth: number): number => {
    deepest = Math.max(deepest, depth);
    const after = (kids.get(id) ?? []).map((kid) => lay(kid, depth + 1));
    const cross = after.length ? ((after[0] ?? 0) + (after.at(-1) ?? 0)) / 2 : rows++ * step;
    slots.set(id, { depth, cross });
    return cross;
  };
  lay(head, 0);
  return { slots, depth: deepest, rows };
}

/** placeRoots places the trees that start from roots, each a region, then the blocks, each a grid five to a row. */
function placeRoots(forest: Forest, card: Size, isDown: boolean): { points: Map<string, Point>; regions: Region[] } {
  const size = isDown ? { main: card.height, cross: card.width } : { main: card.width, cross: card.height };
  const step = { main: size.main + TREE.main, cross: size.cross + TREE.cross };
  const span = (count: number, each: number, gap: number) => Math.max(count, 1) * each - gap;
  const heads = forest.nodes.filter((node) => !forest.requirements.has(node.id) && forest.kids.has(node.id));
  const trees = heads.map((head) => {
    const tree = layTree(head.id, forest.kids, step.cross);
    const [along, across] = [span(tree.depth + 1, step.main, TREE.main), span(tree.rows, step.cross, TREE.cross)];
    return { head, slots: tree.slots, width: isDown ? across : along, height: isDown ? along : across };
  });
  const placed = new Set(trees.flatMap((tree) => [...tree.slots.keys()]));
  const blocks = [...forest.blocks, { id: "", title: "", count: 0 }].flatMap((block) => {
    const members = forest.nodes.filter((node) => (block.id ? node.parent === block.id : !placed.has(node.id)));
    for (const member of members) placed.add(member.id);
    if (!members.length) return [];
    const width = span(Math.min(members.length, TREE.row), card.width + TREE.cross, TREE.cross);
    const height = span(Math.ceil(members.length / TREE.row), card.height + TREE.cross, TREE.cross);
    return { block, members, width, height };
  });
  const inner = Math.max(0, ...trees.map((tree) => tree.width), ...blocks.map((block) => block.width));
  const points = new Map<string, Point>();
  const regions: Region[] = [];
  let top = TREE.margin;
  const frame = (id: string, title: string, summary: string, height: number): Point => {
    regions.push({
      id,
      title,
      summary,
      x: TREE.margin,
      y: top,
      width: inner + 2 * TREE.pad,
      height: TREE.header + height + TREE.pad,
    });
    const corner = { x: TREE.margin + TREE.pad, y: top + TREE.header };
    top += TREE.header + height + TREE.pad + TREE.gap;
    return corner;
  };

  for (const tree of trees) {
    const corner = frame(
      `band:${tree.head.id}`,
      tree.head.title || tree.head.name,
      plural(tree.head.count, tree.head.what),
      tree.height,
    );
    const left = corner.x + (isDown ? (inner - tree.width) / 2 : 0);
    for (const [id, slot] of tree.slots) {
      points.set(
        id,
        isDown
          ? { x: left + slot.cross, y: corner.y + slot.depth * step.main }
          : { x: left + slot.depth * step.main, y: corner.y + slot.cross },
      );
    }
  }
  for (const { block, members, height } of blocks) {
    const corner = frame(block.id || "rest", block.title, describeCount(members.length), height);
    for (const [position, member] of members.entries()) {
      points.set(member.id, {
        x: corner.x + (position % TREE.row) * (card.width + TREE.cross),
        y: corner.y + Math.floor(position / TREE.row) * (card.height + TREE.cross),
      });
    }
  }
  return { points, regions };
}

/**
 * placeEras places a tree whose technologies stand in eras: each era is a region, in order, a technology stands
 * after what it requires in its own era, the trees stand one beside the other across the eras, and a technology
 * with no wire stands at the foot of its era.
 */
function placeEras(
  graph: Graph,
  forest: Forest,
  card: Size,
  isDown: boolean,
): { points: Map<string, Point>; regions: Region[] } {
  const size = isDown ? { main: card.height, cross: card.width } : { main: card.width, cross: card.height };
  const step = { main: size.main + TREE.main, cross: size.cross + TREE.cross };
  const { columns, starts } = settleEras(graph, forest);
  const crosses = new Map<string, number>();
  let cursor = 0;
  const lay = (id: string): number => {
    const after = (forest.kids.get(id) ?? []).map(lay);
    const cross = after.length ? ((after[0] ?? 0) + (after.at(-1) ?? 0)) / 2 : cursor;
    if (!after.length) cursor += step.cross;
    crosses.set(id, cross);
    return cross;
  };
  for (const node of forest.nodes) {
    if (forest.requirements.has(node.id) || !forest.kids.has(node.id)) continue;
    lay(node.id);
    cursor += TREE.gap;
  }
  const stacked = new Map<number, number>();
  for (const node of forest.nodes) {
    if (crosses.has(node.id)) continue;
    const column = columns.get(node.id) ?? 0;
    const count = stacked.get(column) ?? 0;
    crosses.set(node.id, cursor + count * step.cross);
    stacked.set(column, count + 1);
  }
  const across = Math.max(0, ...crosses.values()) + size.cross;

  // Each era's region starts where the one before it ends, a gap apart. Its title takes a strip at its top.
  const eras = starts
    .slice(0, -1)
    .map((start, position) => ({ start, count: Math.max((starts[position + 1] ?? start) - start, 1) }));
  const title = isDown ? TREE.header : 0;
  const offsets: number[] = [];
  let offset = TREE.margin;
  for (const era of eras) {
    offsets.push(offset);
    offset += title + era.count * step.main - TREE.main + 2 * TREE.pad + TREE.gap;
  }
  const points = new Map<string, Point>();
  for (const node of forest.nodes) {
    const column = columns.get(node.id) ?? 0;
    const era = Math.max(
      starts.findLastIndex((start, position) => position < eras.length && start <= column),
      0,
    );
    const main = (offsets[era] ?? 0) + title + TREE.pad + (column - (eras[era]?.start ?? 0)) * step.main;
    const cross = TREE.margin + (isDown ? 0 : TREE.header) + TREE.pad + (crosses.get(node.id) ?? 0);
    points.set(node.id, isDown ? { x: cross, y: main } : { x: main, y: cross });
  }
  const listed = graph.eras ?? [];
  const regions = eras.flatMap((era, position) => {
    const named = listed[position];
    if (!named && !forest.nodes.some((node) => (columns.get(node.id) ?? 0) >= era.start)) return [];
    const along = title + era.count * step.main - TREE.main + 2 * TREE.pad;
    const wide = (isDown ? 0 : TREE.header) + across + 2 * TREE.pad;
    return {
      id: `era:${named?.id ?? ""}`,
      title: named ? named.title || named.name : "",
      summary: named ? describeCount(named.count) : "",
      ...(isDown
        ? { x: TREE.margin, y: offsets[position] ?? 0, width: wide, height: along }
        : { x: offsets[position] ?? 0, y: TREE.margin, width: along, height: wide }),
    };
  });
  return { points, regions };
}

/**
 * settleEras returns the column each technology of a tree with eras stands in, and the column each era starts at,
 * with the column after the last. The eras stand in order, a technology of no era the game lists after them, and a
 * technology stands after what it requires in its own era.
 */
function settleEras(graph: Graph, forest: Forest): { columns: Map<string, number>; starts: number[] } {
  const order = (graph.eras ?? []).map((era) => era.id);
  const eraOf = new Map(forest.nodes.map((node) => [node.id, order.indexOf(node.era ?? "")]));
  const columns = new Map<string, number>();
  const starts: number[] = [];
  let next = 0;
  for (const era of [...order.keys(), -1]) {
    const start = next;
    starts.push(start);
    const settle = (id: string): number => {
      const known = columns.get(id);
      if (known !== undefined) return known;
      columns.set(id, start);
      const needed = (forest.requirements.get(id) ?? []).map((required) =>
        eraOf.get(required) === era ? settle(required) : (columns.get(required) ?? start - 1),
      );
      const column = Math.max(start, ...needed.map((required) => required + 1));
      columns.set(id, column);
      return column;
    };
    for (const node of forest.nodes) if (eraOf.get(node.id) === era) next = Math.max(next, settle(node.id) + 1);
  }
  return { columns, starts: [...starts, next] };
}
