/**
 * The Visual view's client state, shared by a file's picture, its Widgets panel and its code beside the picture: its
 * settings, kept in the app's settings and set from the toolbar or the IDE settings' Visual view tab, one record per
 * file of what each of them marks and asks, and the list drawn for the file's entry, one query for all of them.
 */
import { computed, shallowReactive, toValue, type MaybeRefOrGetter } from "vue";
import { defineStore } from "pinia";
import { useQuery } from "@pinia/colada";
import type { Location } from "@services/internal/lsp";
import { pathKey } from "../composables/useIdeRoots";
import { drawQuery } from "../queries";
import { useEditorStore } from "./editor";
import { useSettingsStore } from "./settings";

/** A file's Visual view for the session: the entry drawn and its database; the element selected and the one under
 * the pointer; the elements hidden; the value each group of conditions is drawn at, by call, and the groups closed
 * until opened; where the view last put the caret, which then selects nothing anew; what the caret is on in the code;
 * and each element's rects read back from the picture, in its pixels, one per drawn copy. */
export type DrawFile = {
  entry: string;
  database: string;
  selection: number;
  hover: number;
  hidden: Set<number>;
  states: Map<string, string>;
  closedGroups: Set<string>;
  caretTarget: { line: number; col: number } | null;
  caret: { element: number; key: string; token: string } | null;
  rects: Map<number, DOMRect[]>;
};

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

  return { width, height, rows, bounds, file, toggleHidden, setState, goTo };
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
