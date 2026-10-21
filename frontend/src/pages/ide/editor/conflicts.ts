/**
 * Merge conflicts in the editor: finding the conflict blocks in a text, marking them, and resolving a block by one
 * side with a single edit.
 */
import type { Model } from "./models";

/** One conflict block: its lines and the two sides' line ranges. */
export type Conflict = { start: number; end: number; ours: [number, number]; theirs: [number, number] };

/** Finds the conflict blocks in a model's text. */
export function findConflicts(model: Model): Conflict[] {
  throw new Error("stub", { cause: model });
}

/** Resolves a block by keeping a side, or both, as one undoable edit. */
export function resolve(model: Model, conflict: Conflict, side: "ours" | "theirs" | "both"): void {
  throw new Error("stub", { cause: [model, conflict, side] });
}

/** Draws the blocks' decorations and the resolve links over them. */
export function decorate(model: Model): void {
  throw new Error("stub", { cause: model });
}
