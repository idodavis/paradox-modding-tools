/** The draw store: what Visual remembers for the session about each file, and the picks it keeps per workspace. */
import { useLocalStorage } from "@vueuse/core";
import { Range } from "monaco-editor/editor/editor.api";
import type { IRange } from "monaco-editor";
import { defineStore } from "pinia";
import { computed, reactive, shallowRef, toRaw, watch } from "vue";

import type { Place } from "@services/workspace/models";

import { useEditorStore } from "./editor";
import { useWorkspaceStore } from "./workspace";

/** A DrawFile is what Visual remembers about one file for the session. */
export type DrawFile = {
  drawable: string | null;
  selected: number | null;
  instance: number | null;
  hovered: number | null;
  hoveredInstance: number | null;
  /** The widgets whose condition the state under the pointer decides. */
  outlined: number[];
  hidden: number[];
  /** The layers checked for an action on several at once. */
  batch: number[];
  tab: "properties" | "states" | "layers";
};

export const useDrawStore = defineStore("draw", () => {
  const workspace = useWorkspaceStore();
  const editor = useEditorStore();
  const files = reactive(new Map<string, DrawFile>());
  const frame = useLocalStorage("pmt.draw.frame", "");
  /** What a coat or an image sits on: dark, light, checks, or a color as #rrggbb. */
  const backdrop = useLocalStorage("pmt.draw.backdrop", "dark");
  /** The colors copied from a layer, each as a coat writes it, by the key of its slot. */
  const colors = shallowRef<Record<string, string>>({});
  /**
   * Whether the panel beside the picture is open. It stays so across a
   * reload, and closes when another workspace opens.
   */
  const isPanelOpen = useLocalStorage("pmt.draw.panel", false);
  /**
   * Whether the code beside the picture is open. It stays so across a reload,
   * and closes when another workspace opens.
   */
  const isCodeOpen = useLocalStorage("pmt.draw.code", false);
  // The value picked for each group, by group ID, by workspace ID.
  const saved = useLocalStorage<Record<string, Record<string, string>>>("pmt.draw.picks", {});
  /** The open workspace's picks, by the ID of each group. */
  const picks = computed(() => saved.value[workspace.activeId()] ?? {});

  watch(
    () => workspace.activeId(),
    () => {
      files.clear();
      isPanelOpen.value = false;
      isCodeOpen.value = false;
    },
  );

  /** file returns a file's record. It makes the record on the first call. */
  const file = (path: string): DrawFile => {
    const record = files.get(path);
    if (record) return record;
    const created = reactive<DrawFile>({ ...makeRecord(null), tab: "properties" });
    // Set on the raw map, so the reader making its record does not trigger itself.
    toRaw(files).set(path, created);
    return created;
  };

  /** select selects a file's widget, or none for null. */
  const select = (path: string, widget: number | null, instance: number | null = null): void => {
    Object.assign(file(path), { selected: widget, instance });
  };

  /** hover marks a file's widget as the one under the pointer, or none for null. */
  const hover = (path: string, widget: number | null, instance: number | null = null): void => {
    Object.assign(file(path), { hovered: widget, hoveredInstance: instance });
  };

  /** outline outlines the given widgets of a file, or none for an empty list. */
  const outline = (path: string, widgets: number[]): void => {
    file(path).outlined = widgets;
  };

  /** hide hides a file's widget for the session, or shows it again. */
  const hide = (path: string, widget: number, hidden: boolean): void => {
    const record = file(path);
    record.hidden = hidden
      ? [...new Set([...record.hidden, widget])]
      : record.hidden.filter((other) => other !== widget);
  };

  /** pick saves the option chosen for each group in the open workspace. Undefined options are skipped. */
  const pick = (options: Partial<Record<string, string>>): void => {
    const chosen = Object.entries(options).filter((entry): entry is [string, string] => entry[1] !== undefined);
    saved.value = { ...saved.value, [workspace.activeId()]: { ...picks.value, ...Object.fromEntries(chosen) } };
  };

  /** unpick forgets the picks of the given groups. */
  const unpick = (groups: string[]): void => {
    const kept = Object.fromEntries(Object.entries(picks.value).filter(([group]) => !groups.includes(group)));
    saved.value = { ...saved.value, [workspace.activeId()]: kept };
  };

  /** draw draws a file's drawable, or the first one for null. Another drawable resets the file's record. */
  const draw = (path: string, drawable: string | null): void => {
    const record = file(path);
    if (record.drawable === drawable) return;
    Object.assign(record, makeRecord(drawable));
  };

  /** showCode opens the code pane beside the picture with the caret where something is written. */
  const showCode = (place: Place, range: IRange): void => {
    isCodeOpen.value = true;
    editor.show(place, Range.getStartPosition(range));
  };

  return {
    frame,
    backdrop,
    colors,
    isPanelOpen,
    isCodeOpen,
    picks,
    file,
    select,
    hover,
    outline,
    hide,
    pick,
    unpick,
    draw,
    showCode,
  };
});

/** makeRecord returns a file's record of a drawable with nothing selected, hovered or hidden. */
function makeRecord(drawable: string | null): Omit<DrawFile, "tab"> {
  return {
    drawable,
    selected: null,
    instance: null,
    hovered: null,
    hoveredInstance: null,
    outlined: [],
    hidden: [],
    batch: [],
  };
}
