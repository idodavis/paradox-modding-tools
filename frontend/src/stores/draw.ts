/**
 * The Visual view's client state, shared by a file's picture, its panel and its code beside the picture: its
 * settings, kept in the app's settings and set from the toolbar or the IDE settings' Visual view tab, one record per
 * file of what each of them marks and asks, what is drawn for the file's entry (a GUI list or a coat), one query for
 * all of them, and a coat's edits, each one change of the file's text through its model.
 */
import { computed, shallowReactive, shallowRef, toValue, type MaybeRefOrGetter } from "vue";
import { defineStore } from "pinia";
import { useQuery } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import { ReadText, WriteText } from "@services/fileservice";
import type { Widget } from "@services/internal/draw";
import type { Location, Position, Range, Symbol } from "@services/internal/lsp";
import { pathKey } from "../composables/useIdeRoots";
import { editText, openFile, openText, redoText, replaceText, showText, textAt, undoText } from "../editor/models";
import { viewsOf } from "../editor/tabs";
import { fileName } from "../lib/format";
import { drawQuery, redraw } from "../queries";
import { useEditorStore } from "./editor";
import { useSettingsStore } from "./settings";

/** What every drawn thing has, a GUI widget or a coat layer: where it sits in the tree, how it is named on the way
 * down, and where it is written. */
export type DrawElement = Pick<
  Widget,
  "parent" | "key" | "type" | "name" | "label" | "path" | "ancestors" | "location"
>;

/** A file's Visual view for the session: the entry drawn and its database; the frame a coat is cut to, by its label
 * ("" or "field" none); the element selected and the one under the pointer; the elements hidden; the value each group
 * of conditions is drawn at, by call, and the groups closed until opened; where the view last put the caret, which
 * then selects nothing anew; what the caret is on in the code; and each element's rects read back from the picture,
 * in its pixels, one per drawn copy. */
export type DrawFile = {
  entry: string;
  database: string;
  frame: string;
  selection: number;
  hover: number;
  hidden: Set<number>;
  states: Map<string, string>;
  closedGroups: Set<string>;
  caretTarget: { line: number; col: number } | null;
  caret: { element: number; key: string; token: string } | null;
  rects: Map<number, DOMRect[]>;
};

/** The database a coat of arms is an entry of. */
export const COATS = "coat_of_arms/coat_of_arms";

/** Each database whose entries the Visual view draws: its group's heading, an entry's kind, its icon, and the group's
 * place among the groups. */
export const ENTRY_GROUPS: Record<string, { label: string; kind: string; icon: string; order: number }> = {
  "gui.widget": { label: "Windows", kind: "Window", icon: "i-lucide-app-window", order: 0 },
  "gui.type": { label: "Types", kind: "Type", icon: "i-lucide-shapes", order: 1 },
  "gui.local_type": { label: "Types", kind: "Type", icon: "i-lucide-shapes", order: 1 },
  "gui.template": { label: "Templates", kind: "Template", icon: "i-lucide-layers", order: 2 },
  "gui.local_template": { label: "Templates", kind: "Template", icon: "i-lucide-layers", order: 2 },
  "gui.texticon": { label: "Text icons", kind: "Text icon", icon: "i-lucide-image", order: 3 },
  "gui.format": { label: "Formats", kind: "Format", icon: "i-lucide-type", order: 4 },
  [COATS]: { label: "Coats", kind: "Coat", icon: "i-lucide-shield", order: 0 },
};

/** The entries of a file the Visual view draws, from its symbols, each once, in the file's order. */
export function entriesOf(symbols: Symbol[]): Symbol[] {
  const keys = new Set<string>();
  return symbols.filter((symbol) => {
    const key = `${symbol.database}|${symbol.name}`;
    const isNew = symbol.database in ENTRY_GROUPS && !keys.has(key);
    keys.add(key);
    return isNew;
  });
}

/** Whether a position comes at or before another, by line and byte column. */
export const isAtOrBefore = (first: Position, second: Position) =>
  first.line < second.line || (first.line === second.line && first.character <= second.character);

/** The entry a position of the file is in: the last drawn entry that starts at or before it; undefined for none. */
export const entryAt = (entries: Symbol[], line: number, character: number): Symbol | undefined =>
  entries.findLast(({ location: { range } }) => isAtOrBefore(range.start, { line, character }));

/** What the new coat dialog writes: a coat of its own, the game's copy pasted, or a read-only coat's copy. */
export type CoatDialog = { mode: "new" | "paste" | "copy"; id: string; text: string; mod: string };

/** A coat's entry from the game designer's copy or any script, named by an id: the text's own entry renamed where it
 * is one (a key, then a block that ends the text), else the text as the entry's block. Braces in a quoted string are
 * counted, as no coat writes one. */
export function coatEntry(id: string, text: string): string {
  const trimmed = text.trim();
  const open = trimmed.indexOf("{");
  let depth = 0;
  let close = -1;
  for (let i = open; open >= 0 && i < trimmed.length && close < 0; i++) {
    depth += trimmed[i] === "{" ? 1 : trimmed[i] === "}" ? -1 : 0;
    if (depth === 0) close = i;
  }
  const isEntry = close === trimmed.length - 1 && /^[^\s={}#]+\s*=\s*$/.test(trimmed.slice(0, open));
  if (isEntry) return `${id} = ${trimmed.slice(open)}`;
  return `${id} = {\n\t${trimmed.split(/\r?\n/).join("\n\t")}\n}`;
}

/** The resolutions a window can be laid out at, each by its width. */
export const RESOLUTIONS = [
  { label: "1280 × 720", value: 1280, height: 720 },
  { label: "1600 × 900", value: 1600, height: 900 },
  { label: "1920 × 1080", value: 1920, height: 1080 },
  { label: "2560 × 1440", value: 2560, height: 1440 },
  { label: "3440 × 1440", value: 3440, height: 1440 },
  { label: "3840 × 2160", value: 3840, height: 2160 },
];

/** Holds the Visual view's settings and each file's record. */
export const useDrawStore = defineStore("draw", () => {
  const settings = useSettingsStore();

  /** A number of the Visual view's settings, else its default. */
  const numberSetting = (key: string, fallback: number) =>
    computed({
      get: () => Number(settings.values[key]) || fallback,
      set: (value: number) => settings.set(key, String(value)),
    });

  const width = numberSetting("ide.draw.width", 1920);
  const height = computed(() => RESOLUTIONS.find((one) => one.value === width.value)?.height ?? 1080);
  // how many rows a data model's item is drawn: its count is the game's to know
  const rows = numberSetting("ide.draw.rows", 1);
  const bounds = computed({
    get: () => settings.on("ide.draw.bounds", false),
    set: (isOn: boolean) => settings.setOn("ide.draw.bounds", isOn),
  });

  // each file's record, made when the file is first asked for and kept for the session: a plain map, since a record
  // is only ever added, of shallow records, their sets and maps replaced whole, so the rects are not each reactive
  const files = new Map<string, DrawFile>();

  /** A file's record. */
  function file(path: string): DrawFile {
    let record = files.get(path);
    if (!record) {
      record = shallowReactive<DrawFile>({
        entry: "",
        database: "",
        frame: "",
        selection: -1,
        hover: -1,
        hidden: new Set<number>(),
        states: new Map<string, string>(),
        closedGroups: new Set<string>(),
        caretTarget: null,
        caret: null,
        rects: new Map<number, DOMRect[]>(),
      });
      files.set(path, record);
    }
    return record;
  }

  /** Hides a file's elements, or shows them again where every one is hidden; what they hold goes with them. */
  function toggleHidden(path: string, elements: number[]): void {
    const record = file(path);
    const hidden = new Set(record.hidden);
    const isShowing = elements.every((element) => hidden.has(element));
    for (const element of elements) {
      if (isShowing) hidden.delete(element);
      else hidden.add(element);
    }
    record.hidden = hidden;
  }

  /** Draws another entry of a file, with nothing selected or hidden and its states at their first values. */
  function pickEntry(path: string, entry: Pick<Symbol, "database" | "name">): void {
    Object.assign(file(path), {
      entry: entry.name,
      database: entry.database,
      hidden: new Set<number>(),
      states: new Map<string, string>(),
      selection: -1,
    });
  }

  /** Draws a file's group of conditions at a value. */
  function setState(path: string, group: string, value: string): void {
    const record = file(path);
    record.states = new Map(record.states).set(group, value);
  }

  /** Puts the caret at a location. A location in the file shows its code beside the picture where only the picture
   * shows, and is kept, so the caret put there selects nothing anew (a property's line can lie in another element's
   * block, as a block override does). */
  function goTo(path: string, location?: Location): void {
    if (!location) return;
    const editor = useEditorStore();
    const position = { line: location.range.start.line, col: location.range.start.character };
    if (pathKey(location.path) === pathKey(path)) {
      if (editor.viewOf(path) === "visual") editor.setView(path, "split");
      file(path).caretTarget = position;
    }
    editor.open(location.path, position);
  }

  // the new coat dialog, open while it holds what it writes
  const coatDialog = shallowRef<CoatDialog | null>(null);

  // the files whose last coat edit is not drawn again yet: every range drawn of them is the text's before it
  const editing = new Set<string>();

  /** Makes a coat edit of a file and waits until the backend has it and what it draws is read again (its elements'
   * properties are read with each render); an edit moves every range after it, so one asked meanwhile is dropped
   * rather than written at a range gone stale. */
  async function edit(path: string, write: () => Promise<unknown>): Promise<void> {
    if (editing.has(path)) return;
    editing.add(path);
    try {
      await write();
      await redraw();
    } finally {
      editing.delete(path);
    }
  }

  /** Writes what a location holds anew: a property's value, or a coat's whole entry. */
  function rewrite(path: string, location: Location, text: string): Promise<void> {
    return edit(path, () => replaceText(path, [{ range: location.range, text }]));
  }

  /** The whitespace a location's first line starts with, and the step its file indents by. */
  function indentOf(path: string, location: Location): { indent: string; step: string } {
    const start = location.range.start;
    const indent =
      /^\s*/.exec(textAt(path, { start: { line: start.line, character: 0 }, end: start }) ?? "")?.[0] ?? "";
    return { indent, step: indent.includes(" ") && !indent.includes("\t") ? "    " : "\t" };
  }

  /** The whole lines a location is written on where nothing else is written on them; undefined where something is. */
  function wholeLines(path: string, location: Location): Range | undefined {
    const { start, end } = location.range;
    const before = textAt(path, { start: { line: start.line, character: 0 }, end: start }) ?? "";
    const after = textAt(path, { start: end, end: { line: end.line + 1, character: 0 } }) ?? "";
    if (before.trim() || after.trim()) return undefined;
    return { start: { line: start.line, character: 0 }, end: { line: end.line + 1, character: 0 } };
  }

  /** A block's lines as written at an indent: its first and last there, each between a step further in; a block given
   * as one line is that line. */
  function blockText(lines: string[], indent: string, step: string): string {
    return lines.map((line, i) => (i === 0 || i === lines.length - 1 ? line : step + line)).join(`\n${indent}`);
  }

  /** Adds a block after a sibling, on a line of its own indented as the sibling's first line, laid out as its lines are
   * given. */
  function addAfter(path: string, sibling: Location, lines: string[]): Promise<void> {
    return edit(path, () => {
      const { indent, step } = indentOf(path, sibling);
      const { end } = sibling.range;
      return replaceText(path, [{ range: { start: end, end }, text: `\n${indent}${blockText(lines, indent, step)}` }]);
    });
  }

  /** Adds a block as the last of a holder's: where the holder is written over lines, on a line of its own before its
   * closing brace's, a step further in than the holder and laid out as its lines are given; where the holder is one
   * line, its lines joined before the brace, a space from what precedes it; nothing where the holder ends in no brace
   * (an alias names another coat). */
  function addInside(path: string, holder: Location, lines: string[]): Promise<void> {
    return edit(path, () => {
      const block = textAt(path, holder.range) ?? "";
      if (!block.endsWith("}")) return Promise.resolve();
      const { indent, step } = indentOf(path, holder);
      const { start, end } = holder.range;
      if (start.line !== end.line) {
        const lineStart = { line: end.line, character: 0 };
        const text = `${indent}${step}${blockText(lines, indent + step, step)}\n`;
        return replaceText(path, [{ range: { start: lineStart, end: lineStart }, text }]);
      }
      const brace = { line: end.line, character: end.character - 1 };
      const gap = /\s$/.test(block.slice(0, -1)) ? "" : " ";
      return replaceText(path, [{ range: { start: brace, end: brace }, text: `${gap}${lines.join(" ")} ` }]);
    });
  }

  /** Removes what a location writes, its whole lines where nothing else is written on them. */
  function removeBlock(path: string, location: Location): Promise<void> {
    return edit(path, () => replaceText(path, [{ range: wholeLines(path, location) ?? location.range, text: "" }]));
  }

  /** Moves a block before or after a sibling's, as one change: its whole lines put at the start of the sibling's first
   * line or after its last, where both are written alone on their lines, else the block put a space from the
   * sibling. */
  function moveBlock(path: string, block: Location, sibling: Location, isAfter: boolean): Promise<void> {
    return edit(path, () => {
      const [source, target] = [wholeLines(path, block), wholeLines(path, sibling)];
      if (source && target) {
        const point = isAfter ? target.end : target.start;
        const text = textAt(path, source) ?? "";
        return replaceText(path, [
          { range: source, text: "" },
          { range: { start: point, end: point }, text },
        ]);
      }
      const point = isAfter ? sibling.range.end : sibling.range.start;
      const text = textAt(path, block.range) ?? "";
      return replaceText(path, [
        { range: block.range, text: "" },
        { range: { start: point, end: point }, text: isAfter ? ` ${text}` : `${text} ` },
      ]);
    });
  }

  /** A file's history as the picture and the panel step it: its text's, which Ctrl Z and Ctrl Y step in its code. */
  const historyOf = (path: string) => ({
    undo: () => edit(path, () => undoText(path)),
    redo: () => edit(path, () => redoText(path)),
  });

  /** Copies a coat as the game's designer pastes it: its entry as written. */
  function copyCoat(coat: DrawElement): void {
    const text = textAt(coat.location.path, coat.location.range);
    if (text) void Clipboard.SetText(text);
  }

  /** Replaces a file's coat with the game designer's copy, as one change it can undo; what nothing draws is listed. */
  async function pasteCoat(path: string, coat: DrawElement): Promise<void> {
    const text = await Clipboard.Text();
    if (text.trim()) await rewrite(path, coat.location, coatEntry(file(path).entry, text));
  }

  /** Opens the new coat dialog on a copy of a file's coat, to write into a mod. */
  function copyToMod(path: string, coat: DrawElement): void {
    const text = textAt(coat.location.path, coat.location.range);
    if (text) coatDialog.value = { mode: "copy", id: file(path).entry, text, mod: "" };
  }

  /** Writes a coat into a mod's coat file (the last that defines coats, else a new one Go names), as an edit of its
   * model left unsaved, and shows it beside its code; a file the editor refuses to read is never written over. */
  async function addCoat(path: string, id: string, entry: string): Promise<void> {
    const open = openText(path);
    if (open === undefined) {
      const disk = await ReadText(path);
      // a file that is not there is made (its folders too); one there that the editor refuses is never written over
      if (disk.missing) await WriteText(path, "", true, false);
      else if (disk.reject) throw new Error(`${fileName(path)}: ${disk.reject}`);
      await showText(path, `${disk.missing ? "" : `${disk.text.trimEnd()}\n\n`}${entry}\n`);
    } else {
      openFile(path, { text: open, bom: false });
      editText(path, (text) => `${text.trimEnd()}\n\n${entry}\n`);
    }
    // the caret on the new coat's last line, so the code beside the picture draws it, not the coat the caret was in
    const editor = useEditorStore();
    Object.assign(file(path), { entry: id, database: COATS, selection: -1 });
    editor.open(path, { line: (openText(path) ?? "").trimEnd().split(/\r?\n/).length - 1, col: 0 });
    editor.setView(path, "split");
  }

  return {
    width,
    height,
    rows,
    bounds,
    file,
    toggleHidden,
    pickEntry,
    setState,
    goTo,
    coatDialog,
    rewrite,
    addAfter,
    addInside,
    removeBlock,
    moveBlock,
    historyOf,
    copyCoat,
    pasteCoat,
    copyToMod,
    addCoat,
  };
});

/** A file's record and what is drawn for its entry, a GUI list or a coat as its kind draws, which the pane, the panel
 * and the code read alike: its elements (the list's widgets or the coat's layers) and what is missing either way. */
export function useDraw(workspaceId: MaybeRefOrGetter<string>, path: MaybeRefOrGetter<string>) {
  const store = useDrawStore();
  const file = computed(() => store.file(toValue(path)));
  const drawing = computed(() => viewsOf(toValue(path))?.drawing ?? "gui");
  const request = computed(() => ({
    path: toValue(path),
    id: file.value.entry,
    database: file.value.database,
  }));
  const { data, error, asyncStatus, refetch } = useQuery(() =>
    drawQuery({ id: toValue(workspaceId), request: request.value, drawing: drawing.value }),
  );
  const list = computed(() => data.value?.list ?? null);
  const coat = computed(() => data.value?.coat ?? null);
  const elements = computed<DrawElement[]>(() => list.value?.elements ?? coat.value?.layers ?? []);
  const missing = computed(() => list.value?.missing ?? coat.value?.missing ?? []);
  const isDrawing = computed(() => asyncStatus.value === "loading");
  const renderedMs = computed(() => data.value?.renderedMs ?? 0);
  const render = computed(() => data.value?.render ?? 0);
  return { file, request, drawing, list, coat, elements, missing, isDrawing, renderedMs, render, error, refetch };
}
