/**
 * Merge conflicts in the editor, the way VS Code shows them inline: each conflict's two sides in bands, and above it
 * Accept current, Accept incoming, Accept both and Compare (the file's two sides, from git's index).
 */
import { monaco } from "../lib/monaco";
import { useEditorStore } from "../stores/editor";
import { diffTab } from "./tabs";

/** A conflict's marker lines, 1-based: <<<<<<<, ||||||| (a base, when git writes one), ======= and >>>>>>>. */
type Conflict = { start: number; base?: number; middle: number; end: number };
type Take = "current" | "incoming" | "both";

// each watched model's conflicts and the decorations that band them, by its address
const banded = new Map<string, { path: string; conflicts: Conflict[]; bands: string[] }>();
const changed = new monaco.Emitter<monaco.languages.CodeLensProvider>();

/** A model's conflicts, the lines between its markers. */
function find(model: monaco.editor.ITextModel): Conflict[] {
  const out: Conflict[] = [];
  let open: Partial<Conflict> = {};
  for (let line = 1; line <= model.getLineCount(); line++) {
    const text = model.getLineContent(line);
    if (text.startsWith("<<<<<<<")) open = { start: line };
    else if (open.start && !open.middle && text.startsWith("|||||||")) open.base = line;
    else if (open.start && text.startsWith("=======")) open.middle = line;
    else if (open.middle && text.startsWith(">>>>>>>")) {
      out.push({ ...(open as Conflict), end: line });
      open = {};
    }
  }
  return out;
}

/** Band a model's conflicts, and keep doing so as it changes. */
export function watchConflicts(model: monaco.editor.ITextModel, path: string): void {
  const key = model.uri.toString();
  const mark = () => {
    const conflicts = model.findMatches("<<<<<<<", false, false, true, null, false, 1).length ? find(model) : [];
    const previous = banded.get(key);
    if (!conflicts.length && !previous?.conflicts.length) return;
    const band = (first: number, last: number, className: string) => ({
      range: new monaco.Range(first, 1, last, 1),
      options: { isWholeLine: true, className },
    });
    const bands = model.deltaDecorations(
      previous?.bands ?? [],
      conflicts.flatMap((conflict) => [
        band(conflict.start, (conflict.base ?? conflict.middle) - 1, "pmt-conflict-current"),
        band(conflict.middle + 1, conflict.end, "pmt-conflict-incoming"),
      ]),
    );
    banded.set(key, { path, conflicts, bands });
    changed.fire(provider);
  };
  mark();
  model.onDidChangeContent(mark);
  model.onWillDispose(() => banded.delete(key));
}

/** Resolve a conflict: keep one side, or both, the current first, as one undoable edit. */
function accept(model: monaco.editor.ITextModel, conflict: Conflict, take: Take): void {
  const lines = (first: number, last: number) => (last < first ? [] : model.getLinesContent().slice(first - 1, last));
  const current = lines(conflict.start + 1, (conflict.base ?? conflict.middle) - 1);
  const incoming = lines(conflict.middle + 1, conflict.end - 1);
  const kept = take === "current" ? current : take === "incoming" ? incoming : [...current, ...incoming];
  const range = new monaco.Range(conflict.start, 1, conflict.end + 1, 1);
  const text = kept.map((line) => line + model.getEOL()).join("");
  model.pushStackElement();
  model.pushEditOperations([], [{ range, text }], () => null);
  model.pushStackElement();
}

const provider: monaco.languages.CodeLensProvider = {
  onDidChange: changed.event,
  provideCodeLenses(model) {
    const uri = model.uri.toString();
    const lens = (conflict: Conflict, title: string, take: Take | "compare") => ({
      range: new monaco.Range(conflict.start, 1, conflict.start, 1),
      command: { id: "pmt.conflict", title, arguments: [uri, conflict.start, take] },
    });
    const lenses = (banded.get(uri)?.conflicts ?? []).flatMap((conflict) => [
      lens(conflict, "Accept current", "current"),
      lens(conflict, "Accept incoming", "incoming"),
      lens(conflict, "Accept both", "both"),
      lens(conflict, "Compare", "compare"),
    ]);
    return { lenses, dispose: () => undefined };
  },
};

/** Register the lenses and their command, once per window. */
export function installConflicts(): void {
  monaco.editor.registerCommand("pmt.conflict", (_, uri: string, line: number, take: Take | "compare") => {
    const model = monaco.editor.getModel(monaco.Uri.parse(uri));
    const watched = banded.get(uri);
    const conflict = watched?.conflicts.find((one) => one.start === line);
    if (!model || !watched || !conflict) return;
    if (take === "compare") return useEditorStore().open(diffTab("conflict", watched.path));
    accept(model, conflict, take);
  });
  monaco.languages.registerCodeLensProvider("*", provider);
}
