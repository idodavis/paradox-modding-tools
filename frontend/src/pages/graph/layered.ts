// ELK's layered layout for the Graph's canvases: where cards go, how the wires
// between them run, and the paths that draw those wires.
import ELK, { type ElkNode } from "elkjs/lib/elk.bundled.js";

/** Box is where something is on the canvas, in pixels. */
export type Box = { x: number; y: number; width: number; height: number };

export type Point = [x: number, y: number];

/**
 * Card is something to place: its size and, where groups stand in order, the
 * number of the band it stands in.
 */
export type Card = { id: string; width: number; height: number; band?: number };

/** Link is a wire to route from one card to another. */
export type Link = { id: string; from: string; to: string };

/**
 * Placed is where each card is, the points each wire runs through from its
 * start to its end, and how big the canvas is.
 */
export type Placed = { width: number; height: number; boxes: Record<string, Box>; routes: Record<string, Point[]> };

/** BEND is how round a wire's corner is, and ARROW the length of its head. */
const BEND = 5;
const ARROW = 9;
/**
 * LAYERED is what ELK is always asked for: a layered drawing with wires at
 * right angles, bands in the order of their numbers, a card in the middle of
 * what it links to, and the wires that leave one card as one trunk.
 */
const LAYERED = {
  "elk.algorithm": "layered",
  "elk.edgeRouting": "ORTHOGONAL",
  "elk.partitioning.activate": "true",
  "elk.layered.nodePlacement.bk.fixedAlignment": "BALANCED",
  "elk.layered.mergeEdges": "true",
};

const elk = new ELK();

/**
 * placeLayered asks ELK where the cards go and how the links between them
 * run, each card after every card that links to it. The options are ELK's own
 * for the direction, the spacing and the padding.
 */
export async function placeLayered(cards: Card[], links: Link[], options: Record<string, string>): Promise<Placed> {
  const placed = await elk.layout<ElkNode>({
    id: "canvas",
    layoutOptions: { ...LAYERED, ...options },
    children: cards.map(({ id, width, height, band }) => ({
      id,
      width,
      height,
      layoutOptions: band === undefined ? undefined : { "elk.partitioning.partition": String(band) },
    })),
    edges: links.map((link) => ({ id: link.id, sources: [link.from], targets: [link.to] })),
  });
  const boxes = Object.fromEntries(
    (placed.children ?? []).map((card) => [
      card.id,
      { x: card.x ?? 0, y: card.y ?? 0, width: card.width ?? 0, height: card.height ?? 0 },
    ]),
  );
  const routes = Object.fromEntries(
    (placed.edges ?? []).flatMap((edge) => {
      const route = edge.sections?.[0];
      if (!route) return [];

      const points = [route.startPoint, ...(route.bendPoints ?? []), route.endPoint];

      return [[edge.id, points.map(({ x, y }): Point => [x, y])]];
    }),
  );

  return { width: placed.width ?? 0, height: placed.height ?? 0, boxes, routes };
}

/**
 * roundPath returns a path through points that each run straight along x or
 * along y to the next, with every corner rounded.
 */
export function roundPath(points: Point[]): string {
  const parts = [`M${points[0].join(",")}`];
  for (let position = 1; position < points.length - 1; position++) {
    const [before, corner, after] = points.slice(position - 1, position + 2);
    const radius = Math.min(BEND, measure(before, corner) / 2, measure(corner, after) / 2);
    parts.push(
      `L${step(corner, before, radius).join(",")}Q${corner.join(",")} ${step(corner, after, radius).join(",")}`,
    );
  }

  return `${parts.join("")}L${points[points.length - 1].join(",")}`;
}

/**
 * findMiddle returns the middle of a wire's longest straight run, the last
 * of several as long, where a label stands clear of the cards at its ends.
 */
export function findMiddle(points: Point[]): Point {
  let middle = points[0];
  let longest = 0;
  for (let position = 1; position < points.length; position++) {
    const [start, end] = [points[position - 1], points[position]];
    if (measure(start, end) < longest) continue;

    longest = measure(start, end);
    middle = [(start[0] + end[0]) / 2, (start[1] + end[1]) / 2];
  }

  return middle;
}

/** drawHead returns the arrowhead at the end of a wire through points. */
export function drawHead(points: Point[]): string {
  const [before, end] = points.slice(-2);
  const base = step(end, before, ARROW);
  const [across, down] = [Math.sign(end[1] - before[1]) * (ARROW / 2), Math.sign(end[0] - before[0]) * (ARROW / 2)];

  return `M${base[0] - across},${base[1] - down}L${end.join(",")}L${base[0] + across},${base[1] + down}Z`;
}

/** measure returns how far apart two points are that share an x or a y. */
function measure(first: Point, second: Point): number {
  return Math.abs(first[0] - second[0]) + Math.abs(first[1] - second[1]);
}

/**
 * step returns the point a distance from one point toward another that shares
 * its x or its y.
 */
function step(start: Point, target: Point, distance: number): Point {
  return [start[0] + Math.sign(target[0] - start[0]) * distance, start[1] + Math.sign(target[1] - start[1]) * distance];
}
