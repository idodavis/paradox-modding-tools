/**
 * The draw store: what the Visual view remembers for the session, one record per file: the drawn drawable, the
 * selected and hovered widgets, the widgets outlined for a state, the widgets hidden for the session, the layers
 * checked for an action on several and the panel's tab, and the colors copied from a layer. The picks are kept per
 * workspace between sessions, shared by every window that asks the same thing, and so are the coat frame and the
 * backdrop. What Go answers (the window, the states, the coat) is read through queries and never stored here.
 */
import { useLocalStorage } from "@vueuse/core";
import { Range } from "monaco-editor";
import type { IRange } from "monaco-editor";
import { defineStore } from "pinia";
import { computed, reactive, shallowRef, toRaw, watch } from "vue";

import type { Place } from "@services/workspace/models";

import { useEditorStore } from "./editor";
import { useWorkspaceStore } from "./workspace";

/** What the Visual view remembers about one file for the session. It is never written to disk. */
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
  // by workspace: the value picked for each group, by the group's ID
  const saved = useLocalStorage<Record<string, Record<string, string>>>("pmt.draw.picks", {});
  /** The open workspace's picks, by the ID of each group. */
  const picks = computed(() => saved.value[workspace.activeId()] ?? {});

  watch(
    () => workspace.activeId(),
    () => files.clear(),
  );

  /** A file's record. The record is made on the first ask, and changing it changes what every reader sees. */
  const file = (path: string): DrawFile => {
    const record = files.get(path);
    if (record) return record;
    const created = reactive<DrawFile>({
      drawable: null,
      selected: null,
      instance: null,
      hovered: null,
      hoveredInstance: null,
      outlined: [],
      hidden: [],
      batch: [],
      tab: "properties",
    });
    // set on the raw map, so the reader making its record does not trigger itself
    toRaw(files).set(path, created);
    return created;
  };

  /** Selects a file's widget, or none for null. A layer's instance can be selected with it. */
  const select = (path: string, widget: number | null, instance: number | null = null): void => {
    Object.assign(file(path), { selected: widget, instance });
  };

  /** Marks a file's widget as the one under the pointer, or none for null. A layer's instance can be marked with it. */
  const hover = (path: string, widget: number | null, instance: number | null = null): void => {
    Object.assign(file(path), { hovered: widget, hoveredInstance: instance });
  };

  /** Outlines the given widgets of a file, or none for an empty list. */
  const outline = (path: string, widgets: number[]): void => {
    file(path).outlined = widgets;
  };

  /** Hides a file's widget for the session, or shows it again. */
  const hide = (path: string, widget: number, hidden: boolean): void => {
    const record = file(path);
    record.hidden = hidden
      ? [...new Set([...record.hidden, widget])]
      : record.hidden.filter((other) => other !== widget);
  };

  /** Picks the options groups are drawn at in the open workspace, by the ID of each group. */
  const pick = (options: Partial<Record<string, string>>): void => {
    const chosen = Object.entries(options).filter((entry): entry is [string, string] => entry[1] !== undefined);
    saved.value = { ...saved.value, [workspace.activeId()]: { ...picks.value, ...Object.fromEntries(chosen) } };
  };

  /** Forgets the picks of the given groups, which are then drawn as their window opens. */
  const unpick = (groups: string[]): void => {
    const kept = Object.fromEntries(Object.entries(picks.value).filter(([group]) => !groups.includes(group)));
    saved.value = { ...saved.value, [workspace.activeId()]: kept };
  };

  /** Draws a file's drawable, or the first one for null. Widgets belong to one drawable, so the rest is forgotten. */
  const draw = (path: string, drawable: string | null): void => {
    const record = file(path);
    if (record.drawable === drawable) return;
    Object.assign(record, {
      drawable,
      selected: null,
      instance: null,
      hovered: null,
      hoveredInstance: null,
      outlined: [],
      hidden: [],
      batch: [],
    });
  };

  /** Puts the caret where something is written. A file shown only as the picture is shown with its code beside it. */
  const showCode = (place: Place, range: IRange): void => {
    const tab = editor.activeTab();
    if (tab?.id === `file:${place.key}` && tab.view === "visual") editor.setView(tab.id, "split");
    editor.show(place, Range.getStartPosition(range));
  };

  return {
    frame,
    backdrop,
    colors,
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
