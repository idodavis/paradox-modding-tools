/**
 * The windows store: what passes between windows. Messages ride two broadcast channels, one every window hears
 * (`pmt.windows`) and one per window name. Go keeps only the OS close hook and the calls that act on another
 * window.
 *
 * The protocol, each message a row:
 * | message        | from → to        | carries                                   |
 * | state          | any → all        | the window's name and what it holds       |
 * | tab-requested  | any → the holder | a tab's id the asker wants                |
 * | tab-handed     | holder → asker   | the tab with its unsaved text             |
 * | show           | any → one        | a place to show                           |
 * | results        | the searcher → all | one file's search hits                  |
 * | closing        | the closer → all | its tabs handed back before it closes     |
 */
import { defineStore } from "pinia";

import type { Place } from "@services/workspace/models";

/** A window's name from its URL: `main`, or `page:<route>`, `editor:<n>`, `game`, `files:<folder>`, `panel`. */
export type WindowName = string;

/** What a window holds, announced on the shared channel. */
export type WindowState = { name: WindowName; files: string[]; folders: string[]; page: string | null };

export const useWindowsStore = defineStore("windows", () => {
  const name = (): WindowName => {
    throw new Error("stub");
  };
  /** The window that holds a file, a folder or a page, or "" when none does. */
  const holderOf = (what: { file?: string; folder?: string; page?: string }): WindowName => {
    throw new Error("stub", { cause: what });
  };
  const popOut = (what: { page?: string; group?: string; folder?: string; panel?: true; game?: true }): void => {
    throw new Error("stub", { cause: what });
  };
  const dockBack = (): void => {
    throw new Error("stub");
  };
  /** Asks the window that holds a tab to hand it over. The tab arrives with its unsaved text and opens here. */
  const requestTab = (tabId: string): void => {
    throw new Error("stub", { cause: tabId });
  };
  const showElsewhere = (window: WindowName, place: Place): void => {
    throw new Error("stub", { cause: [window, place] });
  };
  return { name, holderOf, popOut, dockBack, requestTab, showElsewhere };
});
