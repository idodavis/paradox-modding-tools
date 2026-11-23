// What the Graph's views share: what they say of a definition, its words and
// its icon, the ground of a canvas, and the filter of a view that reaches no
// further than one step.
import { Type } from "@internal/lang/models";
import type { Filter, LineRow, Node } from "@services/graph/models";

/** GROUND is the dotted ground a canvas of cards and wires sits on. */
export const GROUND = "radial-gradient(var(--line-1) 1px, transparent 1px) 0 0 / 20px 20px, var(--bg-0)";

/**
 * NO_REACH is a filter that reaches nowhere: a good's chain and a tree are
 * one step each way, so only the filter's origins, changes, era and audience
 * count.
 */
export const NO_REACH: Filter = {
  databases: null,
  origins: null,
  depth: 0,
  limit: 0,
  fanout: 0,
  expand: null,
  follow: false,
};

/** Recent is a subject opened before, with what the start page shows of it. */
export type Recent = Pick<
  Node,
  "id" | "database" | "name" | "what" | "title" | "display" | "picture" | "silhouette" | "type"
>;

/**
 * TYPE_ICONS is the icon of each type a catalog lists, for a node with no
 * picture.
 */
export const TYPE_ICONS: Record<string, string> = {
  [Type.TypeGood]: "i-lucide-package",
  [Type.TypeGoodCategory]: "i-lucide-boxes",
  [Type.TypeBuilding]: "i-lucide-factory",
  [Type.TypeProductionMethod]: "i-lucide-cog",
  [Type.TypeProductionMethodGroup]: "i-lucide-layers",
  [Type.TypeTechnology]: "i-lucide-lightbulb",
  [Type.TypeEvent]: "i-lucide-app-window",
};

/** GOOD_TYPES are the types of node whose view is the sheet of a good. */
export const GOOD_TYPES: (Type | undefined)[] = [Type.TypeGood, Type.TypeGoodCategory];
/** LINE_TYPES are the types of node whose view is the line of a building. */
export const LINE_TYPES: (Type | undefined)[] = [Type.TypeBuilding, Type.TypeProductionMethod];

/**
 * hasView reports whether the Graph has a view of a node: a good, a building,
 * a production method, a technology, or a definition of one of the databases
 * that fire, which the event graph draws.
 */
export function hasView(node: Pick<Node, "type" | "database">, firing: readonly string[]): boolean {
  return [...GOOD_TYPES, ...LINE_TYPES, Type.TypeTechnology].includes(node.type) || firing.includes(node.database);
}

/**
 * SAYS is how a row of a line is headed and colored by what it says, in the
 * order they are listed: an amount of a good is blue and a share is purple.
 */
export const SAYS: Record<string, { title: string; tone: string }> = {
  output: { title: "Makes", tone: "text-(--blue)" },
  boost: { title: "Boosts", tone: "text-(--purple)" },
  input: { title: "Takes", tone: "text-(--blue)" },
  need: { title: "Needs", tone: "text-(--blue)" },
  gives: { title: "Gives when a need is met", tone: "text-(--purple)" },
  employment: { title: "Employs", tone: "" },
  cost: { title: "Costs", tone: "" },
  bonus: { title: "Bonuses", tone: "" },
};

/** labelOf returns what a node is called: its title, else its name. */
export function labelOf(node: Node): string {
  return node.title || node.name;
}

/** TONES is the color of a value by its tone: a gain green, a loss red. */
export const TONES: Record<string, string> = { gain: "text-(--green)", loss: "text-(--red)", "": "" };

/**
 * conditionOf returns the condition of one row of a line in words: what it
 * applies to, the need whose meeting gives it, then the condition its own
 * block writes. It is "" for a row that always holds everywhere.
 */
export function conditionOf(rows: LineRow[], row: LineRow): string {
  const need = rows.find((other) => other.says === "need" && other.subject?.id === row.need);

  return [row.where, need && `when its need for ${need.label} is met`, row.when].filter(Boolean).join(", ");
}

/** keyOf returns what tells one row of a line from every other. */
export function keyOf(row: LineRow): string {
  return `${row.says}|${row.subject?.id}|${row.label}|${row.when}|${row.where}|${row.need}`;
}

/** whatOf returns what a node is in the game's words: "hidden event". */
export function whatOf(node: Pick<Node, "what" | "display">): string {
  return node.display === "hidden" ? `hidden ${node.what}` : node.what;
}
