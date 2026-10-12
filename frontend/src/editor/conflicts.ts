/**
 * Merge conflicts in the editor, VS Code's inline way (O-R58): each conflict's two sides in bands, and above it
 * Accept current, Accept incoming, Accept both and Compare (the file's two sides, from git's index).
 */
import { monaco } from "../lib/monaco";
import { useEditorStore } from "../stores/editor";
import { diffTab } from "./tabs";

/** A conflict's marker lines, 1-based: <<<<<<<, ||||||| (a base, when git writes one), ======= and >>>>>>>. */
type Conflict = { start: number; base?: number; middle: number; end: number };

const shown = new Map<string, { path: string; conflicts: Conflict[]; bands: string[] }>();
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
    const before = shown.get(key);
    if (!conflicts.length && !before?.conflicts.length) return;
    const band = (from: number, to: number, className: string) => ({
      range: new monaco.Range(from, 1, to, 1),
      options: { isWholeLine: true, className },
    });
    const bands = model.deltaDecorations(
      before?.bands ?? [],
      conflicts.flatMap((c) => [
        band(c.start, (c.base ?? c.middle) - 1, "pmt-conflict-current"),
        band(c.middle + 1, c.end, "pmt-conflict-incoming"),
      ]),
    );
    shown.set(key, { path, conflicts, bands });
    changed.fire(provider);
  };
  mark();
  model.onDidChangeContent(mark);
  model.onWillDispose(() => shown.delete(key));
}

/** Resolve a conflict: keep one side, or both, the current first. */
function accept(model: monaco.editor.ITextModel, conflict: Conflict, take: "current" | "incoming" | "both"): void {
  const lines = (from: number, to: number) => (to < from ? [] : model.getLinesContent().slice(from - 1, to));
  const current = lines(conflict.start + 1, (conflict.base ?? conflict.middle) - 1);
  const incoming = lines(conflict.middle + 1, conflict.end - 1);
  const kept = take === "current" ? current : take === "incoming" ? incoming : [...current, ...incoming];
  const range = new monaco.Range(conflict.start, 1, conflict.end + 1, 1);
  const text = kept.map((line) => line + model.getEOL()).join("");
  model.pushStackElement();
  model.pushEditOperations([], [{ range, text }], () => null);
  model.pushStackElement();
}

monaco.editor.registerCommand(
  "pmt.conflict",
  (_, uri: string, line: number, take: "current" | "incoming" | "both" | "compare") => {
    const model = monaco.editor.getModel(monaco.Uri.parse(uri));
    const found = shown.get(uri);
    const conflict = found?.conflicts.find((c) => c.start === line);
    if (!model || !found || !conflict) return;
    if (take === "compare") return useEditorStore().open(diffTab("conflict", found.path));
    accept(model, conflict, take);
  },
);

const provider: monaco.languages.CodeLensProvider = {
  onDidChange: changed.event,
  provideCodeLenses(model) {
    const uri = model.uri.toString();
    const lens = (c: Conflict, title: string, take: string) => ({
      range: new monaco.Range(c.start, 1, c.start, 1),
      command: { id: "pmt.conflict", title, arguments: [uri, c.start, take] },
    });
    const lenses = (shown.get(uri)?.conflicts ?? []).flatMap((c) => [
      lens(c, "Accept current", "current"),
      lens(c, "Accept incoming", "incoming"),
      lens(c, "Accept both", "both"),
      lens(c, "Compare", "compare"),
    ]);
    return { lenses, dispose: () => undefined };
  },
};
monaco.languages.registerCodeLensProvider("*", provider);
