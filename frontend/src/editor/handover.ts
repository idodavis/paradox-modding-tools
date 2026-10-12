/**
 * Tabs between windows. A file is open in one window at a time: a tab handed on carries its unsaved text, and the
 * giver never tells the backend it closed; a dragged tab is asked of the window holding it and placed where it was
 * dropped when it comes. The table says who sends and answers each window event; stores/windows.ts answers the ones
 * about windows.
 *
 * | event                  | data                                   | sent by                                  | answered by                                             |
 * | ---------------------- | -------------------------------------- | ---------------------------------------- | ------------------------------------------------------- |
 * | window:opened          | the sender's name                      | every window at start                    | stores/windows.ts: broadcastState                       |
 * | window:state           | WindowState                            | a window holding files, when ready and   | stores/windows.ts: recordWindow                         |
 * |                        |                                        | when its state changes, and when asked   |                                                         |
 * | window:closed          | the name                               | the backend, when a window closed        | stores/windows.ts                                       |
 * | window:closing         | the name                               | the backend, on a window's first close   | stores/windows.ts for the main window (dock, ask) and a |
 * |                        |                                        |                                          | window without files; here for a window with files      |
 * | window:tabs-handed     | WindowTabs                             | handOver; requestTab for a tab no        | here: receive, after the earlier hands                  |
 * |                        |                                        | window holds                             |                                                         |
 * | window:tab-requested   | WindowTabRequest                       | requestTab, when another window holds it | here, in the holder: handOver to the asker              |
 * | window:show            | WindowShow                             | the editor store's open; the router's    | stores/windows.ts                                       |
 * |                        |                                        | guard for a page in another window       |                                                         |
 * | window:results         | PanelResults                           | a window with files, when its results    | editor/search.ts, in the main window                    |
 * |                        |                                        | change                                   |                                                         |
 * | window:saved           | the sender's name                      | saved, after a mutation or the settings  | stores/windows.ts                                       |
 */
import { Events, Window } from "@wailsio/runtime";
import type { WindowTabs } from "@services/models";
import { pathKey } from "../composables/useIdeRoots";
import { fileName } from "../lib/format";
import { useEditorStore } from "../stores/editor";
import { holdsFiles, isMain, useWindowsStore, windowName } from "../stores/windows";
import { showText, unsavedText } from "./models";
import { tabOf } from "./tabs";

/** Answer the events about tabs: tabs handed here, a tab asked of this window, and, in a window with files other than
 * the main one, its close, which hands its tabs back first. */
export function installHandover(): void {
  const windows = useWindowsStore();
  Events.On("window:tabs-handed", ({ data }) => {
    if (data.to === windowName)
      windows.receiveTabs(
        (data.tabs ?? []).map((one) => one.tab),
        () => receive(data),
      );
  });
  Events.On("window:tab-requested", ({ data }) => {
    if (data.to === windowName) void handOver(data.window, [data.tab]);
  });
  if (holdsFiles && !isMain)
    Events.On("window:closing", ({ data: name }) => {
      if (name === windowName) void handBack();
    });
}

/** Hand this window's tabs to the main window and close. */
async function handBack(): Promise<void> {
  const editor = useEditorStore();
  if (editor.openTabs.length) await handOver("main", editor.openTabs, editor.activeTab);
  await Window.Close();
}

/** Hand tabs to another window, each with its file's unsaved text; they go from here, the backend keeping them open.
 * Resolved once they are sent. */
export function handOver(target: string, tabs: string[], active = tabs[0] ?? ""): Promise<boolean> {
  const entries = tabs.map((tab) => {
    const text = unsavedText(tab);
    return { tab, unsaved: text !== undefined, text: text ?? "" };
  });
  useEditorStore().dropTabs(tabs);
  return Events.Emit("window:tabs-handed", { to: target, tabs: entries, active });
}

/** Tabs handed to this window, the last shown one shown, and one dragged here where it was dropped; a file's unsaved
 * text is an edit to the text saved, so its tab is marked and an undo goes back to the file. */
export async function receive(payload: WindowTabs): Promise<void> {
  const editor = useEditorStore();
  const windows = useWindowsStore();
  for (const { tab, unsaved, text } of payload.tabs ?? []) {
    editor.addTabHidden(tab);
    if (unsaved) await showText(tabOf(tab).file, text);
  }
  const group = editor.group(editor.focused);
  if (group?.tabs.includes(payload.active)) group.active = payload.active;
  for (const { tab } of payload.tabs ?? []) {
    const place = windows.placing.get(tab);
    windows.placing.delete(tab);
    if (place) editor.drop(tab, editor.focused, place.group, place.edge, place.nextTab);
  }
}

/** A tab into another window: handed on from here, asked of the window holding it, or opened there. */
export function requestTab(target: string, tab: string): Promise<unknown> {
  if (useEditorStore().openTabs.includes(tab)) return handOver(target, [tab]);
  const holder = [...useWindowsStore().otherWindows.values()].find((one) => one.tabs?.includes(tab));
  if (holder) return Events.Emit("window:tab-requested", { to: holder.name, tab, window: target });
  return Events.Emit("window:tabs-handed", { to: target, tabs: [{ tab, unsaved: false, text: "" }], active: tab });
}

/** A tab in a new editor window of its own. */
export async function openInNewWindow(tab: string): Promise<void> {
  const name = `editor:${Date.now().toString(36)}`;
  const workspaceId = useEditorStore().editingWorkspaceId;
  await useWindowsStore().openWindow(`/workspace/${workspaceId}`, name, fileName(tabOf(tab).file));
  await requestTab(name, tab);
}

/** A window whose explorer shows folders (the game's files, or one folder), given this window's tabs of files in
 * them. */
export async function openFolderWindow(name: string, title: string, folders: string[]): Promise<void> {
  const editor = useEditorStore();
  await useWindowsStore().openWindow(`/workspace/${editor.editingWorkspaceId}`, name, title);
  const keys = folders.map(pathKey);
  const tabs = editor.openTabs.filter((tab) => keys.some((key) => pathKey(tabOf(tab).file).startsWith(`${key}/`)));
  if (tabs.length) await handOver(name, tabs);
}
