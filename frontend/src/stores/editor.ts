/**
 * The editor store: the layout of groups in rows and columns with their tabs, kept per workspace in local storage,
 * the active group and tab, the view each tab shows, and the one function that shows a place. The files' texts
 * are the models'; this store holds no text, no problem and no caret. The panes' sizes belong to the splitters.
 */
import { defineStore } from "pinia";

import type { Place } from "@services/workspace/models";
import type { IPosition } from "monaco-editor";
/** The views a tab can show. VIEWS says which ones a format allows. */
export type View = "code" | "visual" | "form" | "image" | "split";

/** One tab: its id (see pages/ide/editor/tabs.ts), whether it is the preview tab, and its view. */
export type Tab = { id: string; preview: boolean; view: View };
/** One group of tabs with its active one. A group is one column of the layout. */
export type Group = { id: string; tabs: Tab[]; active: string | null };

/** The groups laid out in rows of columns. */
export type Layout = { rows: { columns: Group[] }[] };

export const useEditorStore = defineStore("editor", () => {
  const layout = (): Layout => {
    throw new Error("stub");
  };
  const activeGroup = (): Group | null => {
    throw new Error("stub");
  };
  const activeTab = (): Tab | null => {
    throw new Error("stub");
  };
  /** The place of the active tab's file, for a diff tab too, or null when there is none. */
  const activeFile = (): Place | null => {
    throw new Error("stub");
  };
  /**
   * Shows a place. It opens the file in the view the request calls for: the explorer's single click opens a
   * preview tab, a definition opens the code view at its position, and a coat opens the Visual view. The file
   * opens in this window, or in the window that already holds it.
   */
  const show = (
    place: Place,
    position?: IPosition,
    source?: "explorer" | "navigate" | "palette" | "graph" | "health",
  ): void => {
    throw new Error("stub", { cause: [place, position, source] });
  };
  const pin = (tabId: string): void => {
    throw new Error("stub", { cause: tabId });
  };
  const close = (tabId: string): void => {
    throw new Error("stub", { cause: tabId });
  };
  const setView = (tabId: string, view: View): void => {
    throw new Error("stub", { cause: [tabId, view] });
  };
  const split = (direction: "row" | "column"): void => {
    throw new Error("stub", { cause: direction });
  };
  /** Moves a tab to a group, or into a new one, by drag. */
  const move = (tabId: string, groupId: string, index: number): void => {
    throw new Error("stub", { cause: [tabId, groupId, index] });
  };
  return { layout, activeGroup, activeTab, activeFile, show, pin, close, setView, split, move };
});
