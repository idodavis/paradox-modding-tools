// Where the cards of a technology tree stand and the wires between them, as
// ELK's layered layout places them.
import type { Edge, Technology, TreeGraph } from "@services/graph/models";
import { drawHead, placeLayered, roundPath, type Box } from "./layered";
import { labelOf } from "./nodes";

/** Direction is which way a tree reads: left to right, or top to bottom. */
export type Direction = "across" | "down";

/**
 * Shape is how a game groups its technologies: Vic3's by era, and EU5's as
 * one tree for each advance an age starts from.
 */
export type Shape = "eras" | "trees";

/** Mark is the part a technology has in the chain of the one asked about. */
export type Mark = "subject" | "chain" | "";

/**
 * Block is the cards of one era, or of one tree inside an age, with its title
 * and how many cards it holds. The title is "" for a chain drawn alone.
 */
export type Block = Box & { id: string; title: string; count: number };

/**
 * Wire is the line from a technology to one that requires it, with the
 * arrowhead at the end that requires it.
 */
export type Wire = { requirement: string; dependent: string; path: string; head: string };

/** TreeLayout is where every card, block and wire of a tree is. */
export type TreeLayout = {
  shape: Shape;
  width: number;
  height: number;
  cards: Record<string, Box>;
  blocks: Block[];
  wires: Wire[];
};

/** Group is the technologies one block holds, under its title. */
type Group = { id: string; title: string; members: Technology[] };

/** SIZES is the size of a card, by shape. */
const SIZES: Record<Shape, { width: number; height: number }> = {
  eras: { width: 212, height: 104 },
  trees: { width: 252, height: 88 },
};
/** ALONE is what heads the technologies of no era, or that start no tree. */
const ALONE: Record<Shape, string> = { eras: "No era", trees: "On their own" };
/** HEAD is the room over a block of cards for its heading. */
const HEAD = 44;
/** GAP is the room between two layers of cards; half of it is round a block. */
const GAP = 48;
/** SPACING is the room ELK leaves, with enough for a heading over a group. */
const SPACING = {
  "elk.spacing.nodeNode": "24",
  "elk.spacing.edgeNode": "10",
  "elk.spacing.componentComponent": String(HEAD + GAP),
  "elk.padding": `[top=${HEAD + GAP / 2},left=${GAP / 2},bottom=${GAP / 2},right=${GAP / 2}]`,
};

/** EMPTY is the layout of no tree, shown while one is worked out. */
export const EMPTY: TreeLayout = { shape: "eras", width: 0, height: 0, cards: {}, blocks: [], wires: [] };

/** chainOf returns the ids of the subject and of all it needs and leads to. */
export function chainOf(tree: TreeGraph): Set<string> {
  return new Set([tree.subject, ...(tree.needs ?? []), ...(tree.leads ?? [])]);
}

/** markOf returns a technology's part in the subject's chain. */
export function markOf(tree: TreeGraph, chain: ReadonlySet<string>, id: string): Mark {
  if (id === tree.subject) return "subject";

  return chain.has(id) ? "chain" : "";
}

/**
 * shapeOf returns how a game groups the technologies of a tree: as trees
 * where a technology names the one its tree starts from, else by era.
 */
export function shapeOf(tree: TreeGraph): Shape {
  return tree.technologies?.some((technology) => technology.root) ? "trees" : "eras";
}

/** toStyle returns a box as CSS, at a scale of its size on the canvas. */
export function toStyle({ x, y, width, height }: Box, scale = 1) {
  return {
    left: `${x * scale}px`,
    top: `${y * scale}px`,
    width: `${width * scale}px`,
    height: `${height * scale}px`,
  };
}

/**
 * layoutTree returns where the technologies of a tree go: a block for each
 * era in the game's order, or for each tree of an age, then one for the
 * technologies of none. A technology stands after everything it requires.
 */
export function layoutTree(tree: TreeGraph, direction: Direction): Promise<TreeLayout> {
  const shape = shapeOf(tree);
  const technologies = tree.technologies ?? [];
  const heads = new Map([
    ...(tree.eras ?? []).map((era): [string, string] => [era.id, labelOf(era)]),
    ...technologies.map(({ node }): [string, string] => [node.id, labelOf(node)]),
  ]);
  const members = Map.groupBy(technologies, (technology) =>
    shape === "trees" ? (technology.root ?? "") : (technology.node.era ?? ""),
  );
  const order = [...(tree.eras ?? []).map((era) => era.id), ...members.keys()].filter((id) => id && members.has(id));
  const groups = [...new Set(order), ""].flatMap((id) => {
    const held = members.get(id);

    return held ? [{ id, title: heads.get(id) ?? ALONE[shape], members: held }] : [];
  });

  return place(tree, groups, direction, shape === "eras");
}

/**
 * layoutChain returns where the subject and its chain go, in one block with
 * no title.
 */
export function layoutChain(tree: TreeGraph, chain: ReadonlySet<string>, direction: Direction): Promise<TreeLayout> {
  const members = (tree.technologies ?? []).filter((technology) => chain.has(technology.node.id));

  return place(tree, [{ id: "", title: "", members }], direction, false);
}

/**
 * place asks ELK where the cards of the given groups go and how the wires of
 * the requirements between them run. Groups that are bands stand one after
 * the other along the way the tree reads, each across the whole canvas, as
 * eras do. Any other group is the box round its cards.
 */
async function place(tree: TreeGraph, groups: Group[], direction: Direction, isBands: boolean): Promise<TreeLayout> {
  const shape = shapeOf(tree);
  const isAcross = direction === "across";
  const drawn = new Set(groups.flatMap((group) => group.members.map(({ node }) => node.id)));
  const requires = (tree.requires ?? []).filter((edge) => drawn.has(edge.from) && drawn.has(edge.to));
  const {
    width,
    height,
    boxes: cards,
    routes,
  } = await placeLayered(
    groups.flatMap((group, position) =>
      group.members.map(({ node }) => ({ id: node.id, ...SIZES[shape], band: isBands ? position : undefined })),
    ),
    requires.map((edge) => ({ id: wireId(edge), from: edge.to, to: edge.from })),
    {
      ...SPACING,
      "elk.direction": isAcross ? "RIGHT" : "DOWN",
      "elk.layered.spacing.nodeNodeBetweenLayers": String(isAcross ? GAP : HEAD + GAP),
    },
  );
  const blocks = groups.map((group): Block => {
    const boxes = group.members.map(({ node }) => cards[node.id]);
    const left = Math.min(...boxes.map((box) => box.x)) - GAP / 2;
    const top = Math.min(...boxes.map((box) => box.y)) - HEAD;
    const box = {
      x: left,
      y: top,
      width: Math.max(...boxes.map((card) => card.x + card.width)) + GAP / 2 - left,
      height: Math.max(...boxes.map((card) => card.y + card.height)) + GAP / 2 - top,
    };
    const band = isAcross ? { y: 0, height } : { x: 0, width };

    return { ...box, ...(isBands ? band : {}), id: group.id, title: group.title, count: group.members.length };
  });
  const wires = requires.flatMap((edge): Wire[] => {
    const points = routes[wireId(edge)];

    return points
      ? [{ requirement: edge.to, dependent: edge.from, path: roundPath(points), head: drawHead(points) }]
      : [];
  });

  return { shape, width, height, cards, blocks, wires };
}

function wireId(edge: Edge): string {
  return `${edge.to}>${edge.from}`;
}
