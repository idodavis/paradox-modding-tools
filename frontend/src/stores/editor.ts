/**
 * The IDE's client state: the editor groups, their tabs and where they sit, the unsaved files, the open files'
 * problems, the caret, the cursor a tab is asked to show, the views and their panels, the sidebar, panel and wiki
 * pane, the explorer's selection and its undoable moves. The models and their text are editor/models.ts's, which this
 * store reaches for the save and which reaches it for the tabs, and the windows store is reached for where a file is
 * open, each inside a function.
 */
import { computed, ref, shallowRef } from "vue";
import { defineStore } from "pinia";
import { useMutation } from "@pinia/colada";
import { useRouter } from "vue-router";
import { useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { RenamePath } from "@services/fileservice";
import { DidClose } from "@services/ideservice";
import type { Diagnostic } from "@services/internal/lsp";
import type { PanelResults } from "@services/models";
import { pathKey } from "../composables/useIdeRoots";
import { saveFile } from "../editor/models";
import { IMAGES, tabOf, viewsOf, type View } from "../editor/tabs";
import { useWindowsStore, holdsFiles, isMain, windowName } from "./windows";
import { useWorkspaceStore } from "./workspace";

/** An editor group: its tabs in order and the one shown. */
export type Group = { id: number; tabs: string[]; active: string };
/** Where the groups sit: a group, or parts side by side (a row) or stacked (a column), each part's share of it. */
export type Layout = { group: number } | { split: "row" | "column"; parts: Layout[]; sizes: number[] };
/** Where a tab dropped on a group goes: a new group on one of its edges, or the group itself. */
export type Edge = "left" | "right" | "up" | "down" | "middle";
/** A place a tab is asked to show: a 0-based line and UTF-8 byte column, as the backend counts. */
export type Cursor = { path: string; line: number; col: number };
/** A rename, move or delete done in the explorer; a delete moved the path into the trash. */
export type FileMove = { from: string; to: string; deleted?: boolean };

/** What a workspace's layout was when its window last left it: its groups, the other windows' tabs, the shown tab. */
type Saved = { layout: Layout; groups: Group[]; others: string[]; active: string };

/** A layout with a new group on one of a group's edges. A group beside its sibling in a split of the same direction
 * takes half that sibling's share; anywhere else the group's node becomes a split of two. */
function beside(node: Layout, groupId: number, edge: Exclude<Edge, "middle">, added: number): Layout {
  const split = edge === "left" || edge === "right" ? "row" : "column";
  const first = edge === "left" || edge === "up";
  if ("group" in node) {
    if (node.group !== groupId) return node;
    const fresh = { group: added };
    return { split, parts: first ? [fresh, node] : [node, fresh], sizes: [0.5, 0.5] };
  }
  const index = node.parts.findIndex((part) => "group" in part && part.group === groupId);
  if (index < 0 || node.split !== split)
    return { ...node, parts: node.parts.map((part) => beside(part, groupId, edge, added)) };
  const share = (node.sizes[index] ?? 0) / 2;
  const parts = node.parts.slice();
  const sizes = node.sizes.slice();
  parts.splice(first ? index : index + 1, 0, { group: added });
  sizes.splice(first ? index : index + 1, 0, share);
  sizes[first ? index + 1 : index] = share;
  return { split: node.split, parts, sizes };
}

/** A layout with a group gone: a split left with one part becomes that part, the shares of the rest add up to 1. */
function without(node: Layout, groupId: number): Layout | null {
  if ("group" in node) return node.group === groupId ? null : node;
  const remaining = node.parts
    .map((part, i) => ({ part: without(part, groupId), size: node.sizes[i] ?? 0 }))
    .filter((one): one is { part: Layout; size: number } => !!one.part);
  const [only] = remaining;
  if (!only) return null;
  if (remaining.length === 1) return only.part;
  const total = remaining.reduce((sum, one) => sum + one.size, 0) || 1;
  return {
    split: node.split,
    parts: remaining.map((one) => one.part),
    sizes: remaining.map((one) => one.size / total),
  };
}

/** The group at the top right: a row's last part, a column's first. */
function corner(node: Layout): number {
  if ("group" in node) return node.group;
  const part = node.split === "row" ? node.parts.at(-1) : node.parts[0];
  return part ? corner(part) : 0;
}

/** The groups in reading order. */
function order(node: Layout): number[] {
  return "group" in node ? [node.group] : node.parts.flatMap(order);
}

/** A value kept on this computer by the main window; the other windows keep theirs until they close. */
function persisted<T>(key: string, start: T) {
  return isMain ? useLocalStorage(key, start) : shallowRef(start);
}

export const useEditorStore = defineStore("editor", () => {
  const workspace = useWorkspaceStore();
  const router = useRouter();

  const groups = ref<Group[]>([{ id: 1, tabs: [], active: "" }]);
  const layout = ref<Layout>({ group: 1 });
  const focused = ref(1);
  const unsaved = ref(new Set<string>());
  const editingWorkspaceId = ref("");
  const problems = ref(new Map<string, Diagnostic[]>());
  const caret = ref<{ line: number; character: number; col: number } | null>(null);
  const cursor = ref<Cursor | null>(null);
  const results = ref<PanelResults | null>(null);
  const selected = ref<string[]>([]);
  const comparing = ref("");
  const views = ref(new Map<string, View>());
  const panels = ref(new Map<string, boolean>());
  const dragging = ref<{ tab: string; sourceGroup: number } | null>(null);
  const fileMoves = ref<{ done: FileMove[]; undone: FileMove[] }>({ done: [], undone: [] });
  const ideSettingsOpen = shallowRef(false);
  const sidebar = persisted<"explorer" | "search" | "git">("pmt.ide.sidebar", "explorer");
  const panel = persisted<"problems" | "git" | "">("pmt.ide.panel", "problems");
  const wikiOpen = persisted("pmt.ide.wikiOpen", false);
  const layouts = persisted<Record<string, Saved | undefined>>("pmt.ide.layouts", {});

  const group = (id: number) => groups.value.find((one) => one.id === id);
  const activeTab = computed(() => group(focused.value)?.active ?? "");
  const activeFile = computed(() => tabOf(activeTab.value).file);
  const openTabs = computed(() => [...new Set(groups.value.flatMap((one) => one.tabs))]);
  const cornerGroup = computed(() => corner(layout.value));
  const problemCounts = computed(() => {
    const all = [...problems.value.values()].flat();
    const errors = all.filter((one) => one.severity === 1).length;
    return { errors, warnings: all.length - errors };
  });

  /** A file's view: the one set for it, else what its kind opens in, else code, or visual for an image. */
  function viewOf(path: string): View {
    return views.value.get(path) ?? viewsOf(path)?.first ?? (IMAGES.test(path) ? "visual" : "code");
  }

  /** A file as an open tab spells it, else as given: the backend and the models know it by that spelling. */
  function backendPath(file: string): string {
    const key = pathKey(file);
    return openTabs.value.find((tab) => pathKey(tab) === key) ?? file;
  }

  /** Show a file in a view; Visual also opens the panel of a kind that has one, Split and Code leave it as it is. */
  function setView(file: string, view: View): void {
    const path = backendPath(file);
    views.value.set(path, view);
    if (view === "visual" && viewsOf(path)?.panel) panels.value.set(path, true);
  }

  /** Open a file's panel, or close it. */
  function togglePanel(file: string): void {
    const path = backendPath(file);
    panels.value.set(path, !panels.value.get(path));
  }

  /** The window a file shows in: the one holding a tab of it, else this one with it, else the one whose folders hold
   * it, else this one if it holds files, else the main window. */
  function windowOf(file: string): string {
    const key = pathKey(file);
    const others = [...useWindowsStore().otherWindows.values()];
    const holds = (tab: string) => pathKey(tabOf(tab).file) === key;
    const holder = others.find((one) => (one.tabs ?? []).some(holds));
    if (holder) return holder.name;
    if (openTabs.value.some(holds)) return windowName;
    const folder = others.find((one) => (one.roots ?? []).some((root) => key.startsWith(`${pathKey(root)}/`)));
    if (folder) return folder.name;
    return holdsFiles ? windowName : "main";
  }

  /** Show a file at a place (a 0-based line and UTF-8 byte column) in the window that holds it. */
  function open(file: string, position?: { line: number; col?: number }): void {
    const path = backendPath(file);
    const target = windowOf(path);
    if (target !== windowName) {
      void Events.Emit("window:show", {
        to: target,
        route: "",
        path,
        line: position?.line ?? -1,
        col: position?.col ?? 0,
      });
      return;
    }
    const key = pathKey(path);
    const holds = (one: Group) => one.tabs.some((tab) => pathKey(tab) === key);
    const here = group(focused.value) ?? groups.value[0];
    const into = (here && holds(here) ? here : groups.value.find(holds)) ?? here;
    if (!into) return;
    let tab = into.tabs.find((one) => pathKey(one) === key);
    if (!tab) {
      tab = path;
      const index = into.tabs.indexOf(into.active);
      into.tabs.splice(index + 1, 0, tab);
    }
    into.active = tab;
    focused.value = into.id;
    if (position) cursor.value = { path, line: position.line, col: position.col ?? 0 };
    if (isMain && router.currentRoute.value.name !== "workspace-ide")
      void router.push({ name: "workspace-ide", params: { id: workspace.activeWorkspaceId } });
  }

  /** A file edited in a peek gets a tab after the shown one, shown only when its group shows nothing. */
  function addTabHidden(file: string): void {
    const key = pathKey(file);
    if (openTabs.value.some((tab) => pathKey(tab) === key)) return;
    const into = group(focused.value) ?? groups.value[0];
    if (!into) return;
    into.tabs.splice(into.tabs.indexOf(into.active) + 1, 0, file);
    if (!into.active) into.active = file;
  }

  /** Take a tab out of a group, showing its neighbor; an emptied group goes unless it is the last. */
  function removeTab(groupId: number, tab: string): void {
    const sourceGroup = group(groupId);
    const index = sourceGroup?.tabs.indexOf(tab) ?? -1;
    if (!sourceGroup || index < 0) return;
    sourceGroup.tabs.splice(index, 1);
    if (sourceGroup.active === tab) sourceGroup.active = sourceGroup.tabs[index] ?? sourceGroup.tabs.at(-1) ?? "";
    if (sourceGroup.tabs.length || groups.value.length === 1) return;
    groups.value = groups.value.filter((one) => one.id !== groupId);
    layout.value = without(layout.value, groupId) ?? { group: groups.value[0]?.id ?? 1 };
    if (focused.value === groupId) focused.value = order(layout.value)[0] ?? 1;
  }

  /** Forget a file's unsaved mark and problems once no tab shows it; whether it went. */
  function forgetFile(file: string): boolean {
    const key = pathKey(file);
    if (openTabs.value.some((tab) => pathKey(tabOf(tab).file) === key)) return false;
    for (const path of [...unsaved.value]) if (pathKey(path) === key) unsaved.value.delete(path);
    for (const path of [...problems.value.keys()]) if (pathKey(path) === key) problems.value.delete(path);
    return true;
  }

  const { mutate: closeFile } = useMutation({
    mutation: (file: string) => DidClose(editingWorkspaceId.value, file),
  });

  /** Close a tab of a group; the backend is notified when no tab shows its file any more. */
  function closeTab(groupId: number, tab: string): void {
    removeTab(groupId, tab);
    const { file } = tabOf(tab);
    if (forgetFile(file)) closeFile(file);
  }

  /** Whether a tab is the path or under it as a folder. */
  function within(path: string): (tab: string) => boolean {
    const key = pathKey(path);
    return (tab) => pathKey(tab) === key || pathKey(tab).startsWith(`${key}/`);
  }

  /** Close every tab of a path, or under it. */
  function closeTabs(path: string): void {
    const inside = within(path);
    for (const one of [...groups.value]) for (const tab of one.tabs.filter(inside)) closeTab(one.id, tab);
  }

  /** Move the tabs of a path, or under it, to its new path, each keeping its place and losing its marks. */
  function renameTabs(path: string, next: string): void {
    const length = path.replace(/[\\/]$/, "").length;
    const tabsUnder = new Set(openTabs.value.filter(within(path)));
    const renamed = (tab: string) => (tabsUnder.has(tab) ? next + tab.slice(length) : tab);
    for (const one of groups.value) {
      one.active = renamed(one.active);
      one.tabs = one.tabs.map(renamed);
    }
    for (const tab of tabsUnder) {
      unsaved.value.delete(tab);
      problems.value.delete(tab);
    }
  }

  /** Let tabs go to another window: they leave every group and Go is not notified, since another window holds them. */
  function dropTabs(tabs: string[]): void {
    for (const tab of tabs) {
      for (const one of [...groups.value]) removeTab(one.id, tab);
      forgetFile(tabOf(tab).file);
    }
  }

  /** Set the shares of a split's parts, as a splitter's drag leaves them. */
  function resizeSplit(node: Layout, shares: number[]): void {
    if ("split" in node) node.sizes = shares;
  }

  /** A new group on a group's edge, showing a tab. */
  function addGroup(groupId: number, edge: Exclude<Edge, "middle">, tab: string): void {
    const id = Math.max(...groups.value.map((one) => one.id)) + 1;
    groups.value.push({ id, tabs: [tab], active: tab });
    layout.value = beside(layout.value, groupId, edge, id);
    focused.value = id;
  }

  /** Drop a tab dragged from a group: onto an edge it makes a group there, in the middle it joins the target before
   * `nextTab` (at the end when none). */
  function drop(tab: string, sourceGroup: number, targetGroup: number, edge: Edge, nextTab?: string): void {
    const target = group(targetGroup);
    const source = group(sourceGroup);
    if (!target || !source) return;
    if (edge !== "middle") {
      if (sourceGroup === targetGroup && source.tabs.length === 1) return;
      removeTab(sourceGroup, tab);
      addGroup(targetGroup, edge, tab);
      return;
    }
    if (sourceGroup !== targetGroup) removeTab(sourceGroup, tab);
    const tabs = target.tabs.filter((one) => one !== tab);
    const index = nextTab ? tabs.indexOf(nextTab) : -1;
    tabs.splice(index < 0 ? tabs.length : index, 0, tab);
    target.tabs = tabs;
    target.active = tab;
    focused.value = targetGroup;
  }

  /** Split a tab off beside the focused group. */
  function split(edge: "right" | "down", tab = activeTab.value, groupId = focused.value): void {
    if (tab) addGroup(groupId, edge, tab);
  }

  /** Move the shown tab into the next group in reading order, making one on the right when there is only one. */
  function moveTabToNextGroup(): void {
    const tab = activeTab.value;
    if (!tab) return;
    const ids = order(layout.value);
    const next = ids[(ids.indexOf(focused.value) + 1) % ids.length] ?? focused.value;
    if (ids.length === 1) drop(tab, focused.value, focused.value, "right");
    else drop(tab, focused.value, next, "middle");
  }

  /** Mark a file unsaved or saved, under the spelling its tab has. */
  function setUnsaved(file: string, isUnsaved: boolean): void {
    const path = backendPath(file);
    if (isUnsaved) unsaved.value.add(path);
    else unsaved.value.delete(path);
  }

  function setProblems(file: string, diagnostics: Diagnostic[]): void {
    problems.value.set(backendPath(file), diagnostics);
  }

  /** Record a rename, move or delete done in the explorer, which an undo can bring back. */
  function recordMove(move: FileMove): void {
    fileMoves.value.done.push(move);
    fileMoves.value.undone = [];
  }

  // the last move done again or undone on disk, its tabs following; a delete done again closes its tabs
  const { mutate: replay } = useMutation({
    mutation: async (back: boolean) => {
      const moves = back ? fileMoves.value.done : fileMoves.value.undone;
      const move = moves.at(-1);
      if (!move) return;
      const [path, next] = back ? [move.to, move.from] : [move.from, move.to];
      await RenamePath(path, next);
      moves.pop();
      (back ? fileMoves.value.undone : fileMoves.value.done).push(move);
      if (move.deleted && !back) closeTabs(path);
      else renameTabs(path, next);
    },
  });
  const undoMove = () => replay(true);
  const redoMove = () => replay(false);

  /** Restore a workspace's layout and tabs; a fresh restore, or one with no record, is a single empty group. */
  function restore(workspaceId: string, fresh: boolean): void {
    editingWorkspaceId.value = workspaceId;
    unsaved.value = new Set();
    problems.value = new Map();
    caret.value = null;
    const record = fresh ? undefined : layouts.value[workspaceId];
    const usable = record?.layout && record.groups?.length && record.groups.every((one) => Array.isArray(one.tabs));
    if (!record || !usable) {
      groups.value = [{ id: 1, tabs: [], active: "" }];
      layout.value = { group: 1 };
      focused.value = 1;
      return;
    }
    const restoredKeys = new Set<string>();
    let restored = record.groups.map((one) => {
      const groupKeys = new Set<string>();
      const tabs = one.tabs.filter((tab) => {
        const key = pathKey(tab);
        if (groupKeys.has(key)) return false;
        groupKeys.add(key);
        restoredKeys.add(key);
        return true;
      });
      const groupActive = tabs.find((tab) => pathKey(tab) === pathKey(one.active)) ?? tabs[0] ?? "";
      return { id: one.id, tabs, active: groupActive };
    });
    let restoredLayout = record.layout;
    for (const empty of restored.filter((one) => !one.tabs.length)) {
      const folded = without(restoredLayout, empty.id);
      if (!folded) break;
      restoredLayout = folded;
      restored = restored.filter((one) => one !== empty);
    }
    const [first] = restored;
    for (const tab of record.others ?? []) {
      const key = pathKey(tab);
      if (restoredKeys.has(key) || !first) continue;
      restoredKeys.add(key);
      first.tabs.push(tab);
      first.active ||= tab;
    }
    const key = pathKey(record.active);
    const showing = restored.find((one) => one.tabs.some((tab) => pathKey(tab) === key));
    if (showing) showing.active = showing.tabs.find((tab) => pathKey(tab) === key) ?? showing.active;
    groups.value = restored;
    layout.value = restoredLayout;
    focused.value = (showing ?? first)?.id ?? 1;
  }

  /** Keep this workspace's layout, its groups' tabs, the other windows' tabs and the shown tab, to restore later. */
  function remember(workspaceId: string, elsewhere: string[]): void {
    layouts.value = {
      ...layouts.value,
      [workspaceId]: {
        layout: JSON.parse(JSON.stringify(layout.value)) as Layout,
        groups: groups.value.map((one) => ({ id: one.id, tabs: [...one.tabs], active: one.active })),
        others: elsewhere,
        active: activeTab.value,
      },
    };
  }

  /** Write files to disk one after another; a failure rejects, so a caller that awaits it can stay open. */
  async function saveFiles(files: string[]): Promise<void> {
    for (const file of files) await saveFile(file);
  }
  const { mutate: saveMutation } = useMutation({ mutation: saveFiles });
  /** Save files in the background, every unsaved file when none are named; a failed write toasts. */
  function save(files?: string[]): void {
    saveMutation(files ?? [...unsaved.value]);
  }

  return {
    groups,
    layout,
    focused,
    unsaved,
    editingWorkspaceId,
    problems,
    caret,
    cursor,
    results,
    selected,
    comparing,
    views,
    panels,
    dragging,
    ideSettingsOpen,
    sidebar,
    panel,
    wikiOpen,
    activeTab,
    activeFile,
    openTabs,
    cornerGroup,
    problemCounts,
    group,
    viewOf,
    setView,
    togglePanel,
    open,
    addTabHidden,
    resizeSplit,
    closeTab,
    closeTabs,
    renameTabs,
    dropTabs,
    drop,
    split,
    moveTabToNextGroup,
    setUnsaved,
    setProblems,
    recordMove,
    undoMove,
    redoMove,
    restore,
    remember,
    save,
    saveFiles,
  };
});
