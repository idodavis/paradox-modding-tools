/**
 * A GUI file's window as its pane and its Widgets panel read it: the entries the file holds, the one drawn, the
 * part selected, the request the toolbar's choices make of it and the list Go draws. Both read the one query, so a
 * window is drawn once.
 */
import { computed, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import { drawQuery, symbolsQuery } from "../queries";
import { useDrawStore } from "../stores/draw";
import { useEditorStore } from "../stores/editor";
import { pathKey } from "./useIdeRoots";

// the databases of what a window is drawn from: a top-level widget, or a type or template drawn alone
const ENTRY_DATABASES = ["gui.widget", "gui.type", "gui.template", "gui.local_type", "gui.local_template"];

/** Reads a GUI file's window for its pane and its panel: the entries it holds, the one drawn and the list. */
export function useDraw(workspaceId: MaybeRefOrGetter<string>, path: MaybeRefOrGetter<string>) {
  const store = useDrawStore();
  const editor = useEditorStore();

  const { data: symbols } = useQuery(() => symbolsQuery({ id: toValue(workspaceId), path: toValue(path) }));
  const entrySymbols = computed(() => (symbols.value ?? []).filter((one) => ENTRY_DATABASES.includes(one.database)));
  const entries = computed(() => [...new Set(entrySymbols.value.map((one) => one.name))]);

  /** The entry the caret is in, while this file's code is shown with the caret in it. */
  function caretEntry(): string | undefined {
    const caret = editor.caret;
    const file = toValue(path);
    if (!caret || editor.viewOf(file) !== "split" || pathKey(editor.activeFile) !== pathKey(file)) return undefined;
    return entrySymbols.value.findLast(({ location: { range } }) => {
      const start = range.start;
      return start.line < caret.line || (start.line === caret.line && start.character <= caret.col);
    })?.name;
  }

  // the entry starts at the caret's, else the first; the picker's choice replaces it
  watch(
    entries,
    (current) => {
      const file = toValue(path);
      const [first] = current;
      if (first && !store.picked.has(file)) store.picked.set(file, caretEntry() ?? first);
    },
    { immediate: true },
  );

  // the entry picked while the file still holds it, else its first
  const entry = computed(() => {
    const chosen = store.picked.get(toValue(path));
    return chosen && entries.value.includes(chosen) ? chosen : (entries.value[0] ?? "");
  });
  const selected = computed({
    get: () => store.selected.get(toValue(path)) ?? -1,
    set: (part: number) => store.selected.set(toValue(path), part),
  });

  const request = computed(() => store.requestOf(toValue(path), entry.value));
  const { data, error } = useQuery(() => drawQuery({ id: toValue(workspaceId), request: request.value }));
  const list = computed(() => data.value?.list ?? null);
  const parts = computed(() => list.value?.parts ?? []);
  const renderedMs = computed(() => data.value?.renderedMs ?? 0);

  return { entries, entry, selected, request, list, parts, renderedMs, error };
}
