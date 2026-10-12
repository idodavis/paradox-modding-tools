/**
 * One Find in files run: the query as the sidebar fills it, its hits streamed in by the backend and drawn once a
 * frame, its cancel, and Replace all; Find all references lists here too. The one writer of the editor store's
 * `results`.
 */
import { watch } from "vue";
import { Events } from "@wailsio/runtime";
import type { Location } from "@services/internal/lsp";
import type { TextSearchQuery } from "@services/internal/pages/models";
import { Replace, TextSearch } from "@services/searchservice";
import { plural } from "../lib/format";
import { useEditorStore } from "../stores/editor";
import { holdsFiles, isMain } from "../stores/windows";
import { editText, openText } from "./models";

/** A search as the sidebar asks it: the backend's query, whose id this module makes. */
export type SearchQuery = Omit<TextSearchQuery, "id">;

let toast: ReturnType<typeof useToast> | undefined;
// the id of the run asked last, whose hits are the ones drawn, and its call
let current = "";
let running: ReturnType<typeof TextSearch> | undefined;
let lastQuery: SearchQuery | undefined;
let frame = 0;

/** Search, cancelling the run before; an empty pattern clears the results. A failure toasts, if the run is current. */
export async function runSearch(query: SearchQuery): Promise<void> {
  const editor = useEditorStore();
  void running?.cancel();
  lastQuery = query;
  if (!query.pattern.trim()) {
    current = "";
    editor.results = null;
    return;
  }
  const id = crypto.randomUUID();
  current = id;
  editor.results = { title: `“${query.pattern}”`, rows: [], done: false, capped: false };
  running = TextSearch({ ...query, id });
  const end = await running.catch((error: unknown) => {
    if (current === id) toast?.add({ title: "The search could not run", description: String(error), color: "error" });
    return null;
  });
  if (current === id && editor.results) editor.results = { ...editor.results, done: true, capped: !!end?.limitHit };
}

/** List places as the results, as a peek lists references; the sidebar reads each one's line. */
export function showReferences(title: string, sites: Location[]): void {
  const editor = useEditorStore();
  editor.results = {
    title,
    rows: sites.map((site) => ({
      path: site.path,
      rel: site.rel,
      origin: site.origin,
      line: site.range.start.line,
      col: site.range.start.character,
      end: site.range.end.line === site.range.start.line ? site.range.end.character : site.range.start.character,
      preview: "",
    })),
    done: true,
    capped: false,
  };
  editor.sidebar = "search";
}

/** Replace every match of a query, written by the backend in the folders the query names and changed in the editor
 * where a file is open (unsaved); then search again. The query's folders are the mods' only. */
export async function replaceAll(query: SearchQuery, replacement: string): Promise<void> {
  const editor = useEditorStore();
  // the open files go with their text in the editor, unsaved edits and all; the backend replaces there as on disk,
  // and hands their new text back for the editor
  const open = Object.fromEntries(
    editor.openTabs.flatMap((path) => {
      const text = openText(path);
      return text === undefined ? [] : [[path, text]];
    }),
  );
  const done = await Replace({ search: { ...query, id: "" }, text: replacement, open });
  const texts = Object.entries(done.texts ?? {});
  for (const [path, text = ""] of texts) editText(path, () => text);
  toast?.add({
    title: plural(done.replaced, "replacement"),
    description: `${plural(done.files - texts.length, "file")} written${texts.length ? `; ${plural(texts.length, "open file")} changed, not saved yet` : ""}.`,
    color: "success",
  });
  if (lastQuery) void runSearch(lastQuery);
}

/** Listen for the search's hits, and, between windows, for the results a window with files finds. */
export function installSearch(): void {
  toast = useToast();
  const editor = useEditorStore();
  // each file's hits as they come, columns in bytes as the IDE's places are
  Events.On("search:hits", ({ data }) => {
    const results = editor.results;
    if (data.id !== current || !results) return;
    (results.rows ??= []).push(...(data.hits ?? []));
    // the rows are drawn once a frame, however many files' hits came in it
    frame ||= requestAnimationFrame(() => {
      frame = 0;
      if (editor.results) editor.results = { ...editor.results };
    });
  });
  // the main window's sidebar shows the Search results another window finds (its references)
  if (isMain)
    Events.On("window:results", ({ data }) => {
      editor.results = { ...data, rows: data.rows ?? [] };
      editor.sidebar = "search";
    });
  else if (holdsFiles)
    watch(
      () => editor.results,
      (results) => results && void Events.Emit("window:results", results),
    );
}
