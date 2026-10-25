/**
 * Merge conflicts in the editor: finding the conflict blocks in a text, marking them, and resolving a block by one
 * side with a single edit.
 */
import * as monaco from "monaco-editor/editor/editor.api";

import { useEditorStore } from "../../../stores/editor";
import { useGitStore } from "../../../stores/git";
import { applyEdits, type Model } from "./models";
import { diffTab } from "./tabs";

/** One conflict block: its lines and the two sides' line ranges. */
export type Conflict = { start: number; end: number; ours: [number, number]; theirs: [number, number] };

type Side = "ours" | "theirs" | "both";

// the links over a block, and the side each one keeps
const LINKS: { title: string; side: Side | "compare" }[] = [
  { title: "Accept current", side: "ours" },
  { title: "Accept incoming", side: "theirs" },
  { title: "Accept both", side: "both" },
  { title: "Compare", side: "compare" },
];

// the blocks of each decorated model, by the model's address, and the decorations that band them
const decorations = new Map<string, { model: Model; conflicts: Conflict[]; bands: string[] }>();
const lensChange = new monaco.Emitter<monaco.languages.CodeLensProvider>();

const lenses: monaco.languages.CodeLensProvider = {
  onDidChange: lensChange.event,
  provideCodeLenses: (textModel) => {
    const uri = textModel.uri.toString();
    return {
      lenses: (decorations.get(uri)?.conflicts ?? []).flatMap((conflict) =>
        LINKS.map(({ title, side }) => ({
          range: new monaco.Range(conflict.start, 1, conflict.start, 1),
          command: { id: "pmt.conflict", title, arguments: [uri, conflict.start, side] },
        })),
      ),
      dispose: () => undefined,
    };
  },
};

/** Registers the links over the conflict blocks and the command each one runs. */
export function registerConflicts(): void {
  monaco.languages.registerCodeLensProvider("*", lenses);
  monaco.editor.registerCommand("pmt.conflict", (_accessor, uri: string, line: number, side: Side | "compare") => {
    const block = decorations.get(uri);
    const conflict = block?.conflicts.find((candidate) => candidate.start === line);
    if (!block || !conflict) return;
    if (side !== "compare") resolve(block.model, conflict, side);
    else {
      const { place } = block.model;
      useEditorStore().open(diffTab("conflict", useGitStore().findRoot(place), place.key), place);
    }
  });
}

/** Finds the conflict blocks in a model's text. A side with no line has a range that ends before it starts. */
export function findConflicts(model: Model): Conflict[] {
  const conflicts: Conflict[] = [];
  let start = 0;
  let base = 0;
  let middle = 0;
  model.model.getLinesContent().forEach((text, index) => {
    const line = index + 1;
    if (text.startsWith("<<<<<<<")) {
      [start, base, middle] = [line, 0, 0];
    } else if (start && !middle && text.startsWith("|||||||")) {
      base = line;
    } else if (start && !middle && text.startsWith("=======")) {
      middle = line;
    } else if (middle && text.startsWith(">>>>>>>")) {
      conflicts.push({ start, end: line, ours: [start + 1, (base || middle) - 1], theirs: [middle + 1, line - 1] });
      [start, base, middle] = [0, 0, 0];
    }
  });
  return conflicts;
}

/** Resolves a block by keeping a side, or both, as one undoable edit. */
export function resolve(model: Model, conflict: Conflict, side: Side): void {
  const lines = model.model.getLinesContent();
  const sideLines = ([first, last]: [number, number]) => lines.slice(first - 1, Math.max(last, first - 1));
  const ours = sideLines(conflict.ours);
  const theirs = sideLines(conflict.theirs);
  const kept = { ours, theirs, both: [...ours, ...theirs] }[side];
  const text = kept.map((line) => line + model.model.getEOL()).join("");
  const range = { startLineNumber: conflict.start, startColumn: 1, endLineNumber: conflict.end + 1, endColumn: 1 };
  applyEdits(model, [{ range, text }]);
}

/** Draws the blocks' decorations and the resolve links over them. The drawing follows the model's edits. */
export function decorate(model: Model): void {
  if (!decorations.has(model.uri)) {
    model.model.onDidChangeContent(() => decorate(model));
    model.model.onWillDispose(() => decorations.delete(model.uri));
  }
  const conflicts = findConflicts(model);
  const band = (first: number, last: number, className: string) => ({
    range: new monaco.Range(first, 1, last, 1),
    options: { isWholeLine: true, className },
  });
  const bands = model.model.deltaDecorations(
    decorations.get(model.uri)?.bands ?? [],
    conflicts.flatMap((conflict) => [
      band(conflict.start, conflict.ours[1], "pmt-conflict-current"),
      band(conflict.theirs[0], conflict.end, "pmt-conflict-incoming"),
    ]),
  );
  decorations.set(model.uri, { model, conflicts, bands });
  lensChange.fire(lenses);
}
