/**
 * The Visual view's client state, shared by a file's picture, its panel and its code beside the picture: its
 * settings, kept in the app's settings and set from the toolbar or the IDE settings' Visual view tab, one record per
 * file of what each of them marks and asks, the list drawn for the file's entry, one query for all of them, and a
 * coat's edits, each one change of the file's text through its model.
 */
import { computed, shallowReactive, shallowRef, toValue, type MaybeRefOrGetter } from "vue";
import { defineStore } from "pinia";
import { useQuery, useQueryCache } from "@pinia/colada";
import { CoatFile } from "@services/drawservice";
import { ReadText, WriteText } from "@services/fileservice";
import type { Location, Range } from "@services/internal/lsp";
import { pathKey } from "../composables/useIdeRoots";
import { editText, openFile, openText, replaceText, showText, textAt } from "../editor/models";
import { fileName } from "../lib/format";
import { drawKeys, drawQuery } from "../queries";
import { useEditorStore } from "./editor";
import { useSettingsStore } from "./settings";

/** A file's Visual view for the session: the entry drawn and its database; the frame a coat is cut to ("" its
 * field); the element selected and the one under the pointer; the elements hidden; the value each group of conditions
 * is drawn at, by call, and the groups closed until opened; where the view last put the caret, which then selects
 * nothing anew; what the caret is on in the code; and each element's rects read back from the picture, in its pixels,
 * one per drawn copy. */
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

  const queryCache = useQueryCache();
  // the files whose last coat edit is not drawn again yet: every range drawn of them is the text's before it
  const editing = new Set<string>();

  /** Makes a coat edit of a file and waits until the backend has it and its list and properties are read again; an
   * edit moves every range after it, so one asked meanwhile is dropped rather than written at a range gone stale. */
  async function edit(path: string, write: () => Promise<unknown>): Promise<void> {
    if (editing.has(path)) return;
    editing.add(path);
    try {
      await write();
      await Promise.all([drawKeys.list, drawKeys.properties].map((key) => queryCache.invalidateQueries({ key })));
    } finally {
      editing.delete(path);
    }
  }

  /** Writes what a location holds anew: a property's value, or a coat's whole entry. */
  function rewrite(path: string, location: Location, text: string): Promise<void> {
    return edit(path, () => replaceText(path, location.range, text));
  }

  /** The whitespace a location's first line starts with, and the step its file indents by. */
  function indentOf(path: string, location: Location): { indent: string; step: string } {
    const start = location.range.start;
    const indent =
      /^\s*/.exec(textAt(path, { start: { line: start.line, character: 0 }, end: start }) ?? "")?.[0] ?? "";
    return { indent, step: indent.includes(" ") && !indent.includes("\t") ? "    " : "\t" };
  }

  /** Adds a block after a sibling, on a line of its own indented as the sibling's first line: on one line where the
   * sibling is, else a line each, the inner ones a step further. */
  function addAfter(path: string, sibling: Location, lines: string[]): Promise<void> {
    return edit(path, () => {
      const { indent, step } = indentOf(path, sibling);
      const { start, end } = sibling.range;
      const text =
        start.line === end.line
          ? lines.join(" ")
          : lines.map((line, i) => (i === 0 || i === lines.length - 1 ? line : step + line)).join(`\n${indent}`);
      return replaceText(path, { start: end, end }, `\n${indent}${text}`);
    });
  }

  /** Adds a line as the last of a holder's block: before its closing brace, a space from what precedes it, where the
   * holder is one line, else on a line of its own before the brace's, a step further in than the holder; nothing
   * where the holder ends in no brace (an alias names another coat). */
  function addInside(path: string, holder: Location, line: string): Promise<void> {
    return edit(path, () => {
      const block = textAt(path, holder.range) ?? "";
      if (!block.endsWith("}")) return Promise.resolve();
      const { indent, step } = indentOf(path, holder);
      const { start, end } = holder.range;
      if (start.line !== end.line) {
        const lineStart = { line: end.line, character: 0 };
        return replaceText(path, { start: lineStart, end: lineStart }, `${indent}${step}${line}\n`);
      }
      const brace = { line: end.line, character: end.character - 1 };
      const gap = /\s$/.test(block.slice(0, -1)) ? "" : " ";
      return replaceText(path, { start: brace, end: brace }, `${gap}${line} `);
    });
  }

  /** Removes what a location writes, its whole lines where nothing else is written on them. */
  function removeBlock(path: string, location: Location): Promise<void> {
    return edit(path, () => {
      const { start, end } = location.range;
      const before = textAt(path, { start: { line: start.line, character: 0 }, end: start }) ?? "";
      const after = textAt(path, { start: end, end: { line: end.line + 1, character: 0 } }) ?? "";
      const isAlone = !before.trim() && !after.trim();
      const range: Range = isAlone
        ? { start: { line: start.line, character: 0 }, end: { line: end.line + 1, character: 0 } }
        : location.range;
      return replaceText(path, range, "");
    });
  }

  /** Writes a coat into a mod's coat file (the last that defines coats, else a new one Go names), as an edit of its
   * model left unsaved, and shows it beside its code; a file the editor refuses to read is never written over. */
  async function addCoat(workspaceId: string, modId: string, id: string, entry: string): Promise<void> {
    const path = await CoatFile(workspaceId, modId);
    const open = openText(path);
    if (open === undefined) {
      const disk = await ReadText(path);
      if (disk.reject) throw new Error(`${fileName(path)}: ${disk.reject}`);
      if (disk.missing) await WriteText(path, "", true, false);
      await showText(path, `${disk.missing ? "" : `${disk.text.trimEnd()}\n\n`}${entry}\n`);
    } else {
      openFile(path, { text: open, bom: false });
      editText(path, (text) => `${text.trimEnd()}\n\n${entry}\n`);
    }
    const editor = useEditorStore();
    editor.open(path);
    editor.setView(path, "split");
    Object.assign(file(path), { entry: id, database: COATS, selection: -1 });
  }

  return {
    width,
    height,
    rows,
    bounds,
    file,
    toggleHidden,
    setState,
    goTo,
    coatDialog,
    rewrite,
    addAfter,
    addInside,
    removeBlock,
    addCoat,
  };
});

/** A file's record and the list drawn for its entry, which the pane, the panel and the code read alike. */
export function useDraw(workspaceId: MaybeRefOrGetter<string>, path: MaybeRefOrGetter<string>) {
  const store = useDrawStore();
  const file = computed(() => store.file(toValue(path)));
  const request = computed(() => ({
    path: toValue(path),
    id: file.value.entry,
    database: file.value.database,
  }));
  const { data, error, asyncStatus, refetch } = useQuery(() =>
    drawQuery({ id: toValue(workspaceId), request: request.value }),
  );
  const list = computed(() => data.value?.list ?? null);
  const elements = computed(() => list.value?.elements ?? []);
  const drawing = computed(() => asyncStatus.value === "loading");
  const renderedMs = computed(() => data.value?.renderedMs ?? 0);
  return { file, request, list, elements, drawing, renderedMs, error, refetch };
}
