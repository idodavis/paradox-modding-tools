/**
 * The editor store: this window's groups of tabs, saved per workspace, its mode, and the history of the places the
 * user jumped between. It holds no file text; the models do.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { StorageSerializers, until, useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { ref, shallowReactive, shallowRef, toRaw, watch } from "vue";
import { useRouter } from "vue-router";

import type { Fix } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import * as workspaceApi from "@services/workspace/service";
import type { IPosition, IRange } from "monaco-editor";
import { confirm } from "../lib/feedback";
import { list } from "../lib/format";
import {
  applyChange,
  applyEdits,
  closeModel,
  findModel,
  openModel,
  renameModel,
  unsavedModels,
} from "../pages/ide/editor/models";
import { VIEWS } from "../pages/ide/editor/tabs";
import { fileQuery, loadQuery, placeQuery, workspacesQuery } from "../queries";
import { useSettingsStore } from "./settings";
import { HAS_EDITORS, THIS_WINDOW, useWindowsStore } from "./windows";
import { useWorkspaceStore } from "./workspace";

/**
 * A Mode is what the IDE shows of the active file: its code, or Visual, the picture it draws with the tools around it.
 */
export type Mode = "code" | "visual";

/** A DiffSide names the two revisions a diff tab compares. */
export type DiffSide = "changes" | "staged" | "conflict" | "commit";

/**
 * A Tab is one tab: a file by its key, or a diff of the file on one side, in its repository and at its commit for a
 * commit's diff, and whether it is the preview tab.
 */
export type Tab = { id: string; key: string; side?: DiffSide; root?: string; hash?: string; preview: boolean };
/** A Group is one group of tabs with its active one. A group is one column of the layout. */
export type Group = { id: string; tabs: Tab[]; active: string | null };

/** A Layout is the groups laid out in rows of columns. */
export type Layout = { rows: { columns: Group[] }[] };

/** A SavedLayout is a workspace's saved layout: the groups, the active group, and the path of each file a tab holds. */
type SavedLayout = { layout: Layout; active: string; paths: Record<string, string> };

/**
 * A Source is what showed a place: the explorer, a jump, the history, or Follow the agent for a file an agent reads or
 * edits.
 */
export type Source = "explorer" | "navigate" | "history" | "agent" | "edit";

/** A Visit is one place the user was in: a file and a caret position in it. */
type Visit = { place: Place; position: IPosition };

/** How many places the history keeps. */
const HISTORY = 50;

/**
 * SOURCES holds what each source does to the place it shows: whether it opens the preview tab, whether the history
 * records it, and whether Follow the agent holds its tab.
 */
const SOURCES: Record<Source, { preview?: true; record?: false; follow?: true }> = {
  explorer: { preview: true },
  navigate: {},
  history: { record: false },
  agent: { preview: true, follow: true },
  edit: { follow: true },
};

export const useEditorStore = defineStore("editor", () => {
  const workspace = useWorkspaceStore();
  const { data: records } = useQuery(workspacesQuery);
  const router = useRouter();
  // only this store reads the layouts: listening to storage would pause its writes for a tick and drop a change in it
  const layouts = useLocalStorage<Record<string, SavedLayout>>(
    "pmt.ide.layouts",
    {},
    { listenToStorageChanges: false },
  );
  // the flags every part of the IDE and every window shares: the Wiki pane and the bottom panel shown, and the file
  // selected for a compare
  const wiki = useLocalStorage("pmt.ide.wiki", false);
  const panel = useLocalStorage("pmt.ide.panel", "");
  const compareFile = useLocalStorage<Place | null>("pmt.ide.compare", null, { serializer: StorageSerializers.object });
  // this window's mode, which is never saved: every window opens in Code
  const mode = shallowRef<Mode>("code");
  const state = ref<Layout>(makeLayout());
  const activeId = shallowRef(state.value.rows[0]?.columns[0]?.id ?? "");
  // the place of each file a tab holds, by key: a tab holds only the key, which is not a path
  const places = shallowReactive(new Map<string, Place>());
  // whether changes are written down: not while a workspace's layout is being brought back, and in the main window only
  const tracking = shallowRef(false);
  // the range a file was shown with, which the active group's editor of that file selects once
  const request = shallowRef<{ key: string; range: IRange } | null>(null);
  // the tab Follow the agent showed last, until the user picks a tab or a group
  const agentTab = shallowRef("");
  // the files the explorer's last selection holds, for the compare of two
  const selection = shallowRef<Place[]>([]);
  // the places the user jumped between, the oldest first, and the index of the one the user is at
  const visits = shallowRef<Visit[]>([]);
  const current = shallowRef(-1);

  const { mutate: openPlace } = useMutation({
    mutation: (request: { place: Place; range?: IRange }) => openFile(request.place, request.range),
  });
  // a file that cannot open is closed and forgotten
  const { mutateAsync: openPath } = useMutation({
    mutation: async (path: string) => {
      const place = await loadQuery(placeQuery(path));
      if (!place) return;
      await openFile(place).catch((error: unknown) => {
        const id = tabId({ key: place.key });
        while (find(id)) close(id);
        places.delete(place.key);
        throw error;
      });
    },
  });

  /**
   * applyFix applies a quick fix: it writes the file the fix creates, applies its edits to each file's model as one
   * undoable step, then shows the place it reveals in the group to the right of the active one, made when there is
   * none.
   */
  const { mutate: applyFix } = useMutation({
    mutation: async (fix: Fix) => {
      if (fix.creates) await workspaceApi.CreateLocalization(fix.creates);
      for (const [path, edits] of Object.entries(fix.edits ?? {})) {
        const place = await loadQuery(placeQuery(path));
        if (place) applyEdits(await openModel(place), edits ?? []);
      }
      if (!fix.reveal) return;
      const group = activeGroup();
      const row = state.value.rows.find((candidate) => !!group && candidate.columns.includes(group));
      const beside = group && row?.columns[row.columns.indexOf(group) + 1];
      if (beside) activate(beside.id);
      else split("row");
      show(fix.reveal.place, fix.reveal.range);
    },
  });

  watch(() => workspace.activeId(), loadLayout, { immediate: true });
  watch(
    [state, activeId, places],
    () => {
      const id = workspace.activeId();
      if (!id || !tracking.value) return;
      layouts.value = {
        ...layouts.value,
        [id]: {
          layout: structuredClone(toRaw(state.value)),
          active: activeId.value,
          paths: Object.fromEntries([...places].map(([key, place]) => [key, place.path])),
        },
      };
    },
    { deep: true },
  );
  // a file changed on disk is read into its model; a deleted file, or every file in a deleted folder, closes its tabs,
  // unless it has edits not saved, which stay to be saved again
  Events.On("fs:changed", ({ data }) => {
    if (!data.deleted) {
      void applyChange(data.path);
      return;
    }
    void loadQuery(placeQuery(data.path)).then((place) => {
      if (!place) return;
      for (const { key } of listUnder(place.key)) {
        const id = tabId({ key });
        while (!findModel(key)?.unsaved && find(id)) close(id);
      }
    });
  });

  /** listUnder lists the places of the open files whose key is the given one or lies under it, as in a folder. */
  function listUnder(key: string): Place[] {
    return [...places.values()].filter((place) => place.key === key || place.key.startsWith(`${key}/`));
  }

  /** makeLayout makes a layout of one empty group. */
  function makeLayout(): Layout {
    return { rows: [{ columns: [{ id: crypto.randomUUID(), tabs: [], active: null }] }] };
  }

  /**
   * loadLayout brings the given workspace's saved layout back, or starts an empty one. Tabs are reopened unless the
   * workspace's record says to reset them. Only the main window keeps a layout; another window starts empty. The
   * layout is written again only once every reopened tab's file is found or dropped.
   */
  async function loadLayout(id: string): Promise<void> {
    const saved = layouts.value[id];
    tracking.value = false;
    for (const key of places.keys()) closeModel(key);
    places.clear();
    selection.value = [];
    visits.value = [];
    current.value = -1;
    state.value = makeLayout();
    activeId.value = state.value.rows[0]?.columns[0]?.id ?? "";
    if (!id || THIS_WINDOW !== "main") return;
    const { record } = await until(() => records.value?.find((row) => row.record.id === id)).toBeTruthy();
    // Go finds a file's place only once the workspace is open
    await until(() => workspace.isOpen()).toBe(true);
    if (workspace.activeId() !== id) return;
    if (saved && !record.resetTabs) {
      state.value = structuredClone(toRaw(saved.layout));
      const groups = listGroups();
      activeId.value = groups.some((group) => group.id === saved.active) ? saved.active : (groups[0]?.id ?? "");
      // a reload while the files are being found keeps the saved paths, which a write now would lose
      await Promise.allSettled(Object.values(saved.paths).map((path) => openPath(path)));
      if (workspace.activeId() !== id) return;
    }
    tracking.value = true;
  }

  /**
   * openFile remembers a file's place and opens its model, with the caret at the start of the given range for the
   * file's editor to select. An image or a file that is not text gets no model.
   */
  async function openFile(place: Place, range?: IRange): Promise<void> {
    places.set(place.key, place);
    if (VIEWS[place.format].view !== "code") return;
    if ((await loadQuery(fileQuery(place.path)))?.binary) return;
    const model = await openModel(place);
    if (!range) return;
    model.caret = { line: range.startLineNumber, column: range.startColumn };
    request.value = { key: place.key, range };
  }

  /** listGroups lists all the groups, row by row. */
  function listGroups(): Group[] {
    return state.value.rows.flatMap((row) => row.columns);
  }

  /** listTabs lists every open tab, group by group. */
  function listTabs(): Tab[] {
    return listGroups().flatMap((group) => group.tabs);
  }

  /**
   * find returns the tab with the given id and the group holding it, looked for in the given group first, the active
   * group unless one is given. It is null when none holds it.
   */
  function find(tabId: string, first = activeGroup()): { group: Group; index: number } | null {
    for (const group of [first, ...listGroups()]) {
      const index = group?.tabs.findIndex((tab) => tab.id === tabId) ?? -1;
      if (group && index >= 0) return { group, index };
    }
    return null;
  }

  /** remove removes a tab from its group and returns it. The tab next to it becomes the active one if it was. */
  function remove(group: Group, index: number): Tab | undefined {
    const [tab] = group.tabs.splice(index, 1);
    if (tab && group.active === tab.id) group.active = (group.tabs[index] ?? group.tabs[index - 1])?.id ?? null;
    return tab;
  }

  /**
   * add makes a tab the active one of a group, adding it after the active tab when the group lacks it. A new preview
   * tab takes the place of the one already there, unless that one has unsaved edits: those keep it as a tab of its own.
   */
  function add(group: Group, tab: Tab): void {
    if (!group.tabs.some((other) => other.id === tab.id)) {
      const index = tab.preview ? group.tabs.findIndex((other) => other.preview) : -1;
      const preview = group.tabs[index];
      if (preview && !findModel(preview.key)?.unsaved) {
        group.tabs.splice(index, 1, tab);
        closeFile(preview.key);
      } else {
        if (preview) preview.preview = false;
        group.tabs.splice(group.tabs.findIndex((other) => other.id === group.active) + 1, 0, tab);
      }
    }
    group.active = tab.id;
  }

  /** closeGroup closes a group left with no tab, unless it is the last. The group that takes its place is focused. */
  function closeGroup(group: Group): void {
    const all = listGroups();
    const row = state.value.rows.find((candidate) => candidate.columns.includes(group));
    if (group.tabs.length || all.length === 1 || !row) return;
    row.columns.splice(row.columns.indexOf(group), 1);
    if (!row.columns.length) state.value.rows.splice(state.value.rows.indexOf(row), 1);
    if (activeId.value === group.id) {
      activeId.value = listGroups()[Math.min(all.indexOf(group), all.length - 2)]?.id ?? "";
    }
  }

  /** closeFile closes the model of a file no tab shows any more, and forgets its place. */
  function closeFile(key: string): void {
    if (listGroups().some((group) => group.tabs.some((tab) => tab.key === key))) return;
    places.delete(key);
    closeModel(key);
  }

  /** activeGroup returns the active group, or null when there are none. */
  const activeGroup = (): Group | null =>
    listGroups().find((group) => group.id === activeId.value) ?? listGroups()[0] ?? null;

  /** activeTab returns the active group's active tab, or null when there is none. */
  const activeTab = (): Tab | null => {
    const group = activeGroup();
    return group?.tabs.find((tab) => tab.id === group.active) ?? null;
  };

  /** layout returns the layout of groups in rows of columns. */
  const layout = (): Layout => state.value;

  /** placeOf returns the place of the file with the given key, or null when no open tab, file or diff, shows it. */
  const placeOf = (key: string): Place | null => places.get(key) ?? null;

  /** activeFile returns the place of the active tab's file, for a diff tab too, or null when there is none. */
  const activeFile = (): Place | null => {
    const tab = activeTab();
    return tab ? placeOf(tab.key) : null;
  };

  /**
   * isDrawable reports whether the given tab is a file tab whose file Visual can draw. A diff tab, no tab and a tab
   * whose file has no place are not.
   */
  function isDrawable(tab: Tab | null | undefined): boolean {
    return !!tab && !tab.side && !!placeOf(tab.key)?.drawable;
  }

  // Visual draws only a drawable file: when the active file is not one, by any way it got there, the mode is Code
  watch(
    () => isDrawable(activeTab()),
    (drawable) => {
      if (!drawable) mode.value = "code";
    },
  );

  /**
   * isFollowed reports whether the active tab is the one Follow the agent showed last, which the user has not picked
   * since.
   */
  const isFollowed = (): boolean => !!agentTab.value && activeTab()?.id === agentTab.value;

  /**
   * open makes a tab of a file or a diff the active one, as the user's pick. It switches to the group that holds the
   * tab, the given or active group first, or adds the tab there. The history keeps the switch.
   */
  const open = (target: Omit<Tab, "id" | "preview">, place: Place, groupId?: string): void => {
    const id = tabId(target);
    places.set(place.key, place);
    const preferred = listGroups().find((one) => one.id === groupId) ?? activeGroup();
    const group = find(id, preferred)?.group ?? preferred;
    if (!group) return;
    record(locate(place));
    add(group, { ...target, id, preview: false });
    activeId.value = group.id;
    release(id);
  };

  /**
   * show opens a file at a position or range. It opens in the window that holds the file, else in this one, or in the
   * main window if this one has no editors; the main window switches to the IDE first. The source's row in SOURCES
   * decides the preview tab, the history and Follow the agent. A drawable file opens in Visual when the setting is on,
   * unless it was active already or the agent showed it. A folder is not shown.
   */
  const show = (place: Place, selection?: IPosition | IRange, source: Source = "navigate"): void => {
    if (place.isDir) return;
    const windows = useWindowsStore();
    const target = windows.holderOf(place.key) || (HAS_EDITORS ? THIS_WINDOW : "main");
    if (target !== THIS_WINDOW) return windows.showElsewhere(target, place, selection, source);
    const group = activeGroup();
    if (!group) return;
    const row = SOURCES[source];
    const range = selection && toRange(selection);
    const start = range && { lineNumber: range.startLineNumber, column: range.startColumn };
    if (row.record !== false) record(start ? { place, position: start } : locate(place));
    const page = router.currentRoute.value.name;
    if (THIS_WINDOW === "main" && page && page !== "workspace-ide") {
      void router.push({ name: "workspace-ide", params: { id: workspace.activeId() } });
    }
    const id = tabId({ key: place.key });
    const isShown = activeTab()?.id === id;
    // Follow replaces only its own preview tab, so the user's is pinned
    if (row.follow && row.preview) {
      group.tabs.filter((one) => one.preview && one.id !== agentTab.value).forEach((one) => pin(one.id));
    }
    agentTab.value = row.follow ? id : "";
    const tab = group.tabs.find((one) => one.id === id);
    if (tab && !row.preview) tab.preview = false;
    places.set(place.key, place);
    add(group, { id, key: place.key, preview: !!row.preview });
    openPlace({ place, range });
    if (!isShown && !row.follow && place.drawable && useSettingsStore().isOn("draw.openVisual")) {
      mode.value = "visual";
    }
  };

  /**
   * showVisual shows a place as show does, then enters Visual if the file is drawable and is now this window's active
   * file. A file shown in another window leaves this window's mode alone.
   */
  const showVisual = (place: Place, selection?: IPosition | IRange): void => {
    show(place, selection);
    if (place.drawable && activeFile()?.key === place.key) mode.value = "visual";
  };

  /**
   * findRequest returns the range a file was last shown with, to select, or empty to put the caret at, that no editor
   * has moved to yet. It is undefined when there is none.
   */
  const findRequest = (key: string): IRange | undefined =>
    request.value?.key === key ? request.value.range : undefined;

  /** clearRequest forgets the range asked of an editor, once the editor has moved its caret there. */
  const clearRequest = (): void => {
    request.value = null;
  };

  /** listSelection lists the files selected in the explorer, in the order the tree lists them. A folder is left out. */
  const listSelection = (): Place[] => selection.value;

  /**
   * showPanel shows the bottom panel on the given tab. Visual hides the panel, so the window goes back to Code first.
   */
  const showPanel = (tab: string): void => {
    mode.value = "code";
    panel.value = tab;
  };

  /** showWiki shows the Wiki pane. Visual hides the pane, so the window goes back to Code first. */
  const showWiki = (): void => {
    mode.value = "code";
    wiki.value = true;
  };

  /** selectFiles keeps the files the explorer has selected. */
  const selectFiles = (files: Place[]): void => {
    selection.value = files;
  };

  /**
   * record records a jump to a place in the history. The place the user leaves is kept before it, and the places
   * ahead of the one the user is at are dropped, as a browser drops them.
   */
  const record = (target: Visit | undefined): void => {
    if (!target) return;
    const here = locate();
    const kept = visits.value.slice(0, current.value + 1);
    // the place the user arrived at in a file becomes where the user is in it now
    if (here && kept.at(-1)?.place.key === here.place.key) kept.pop();
    for (const visit of here ? [here, target] : [target]) if (!isSame(kept.at(-1), visit)) kept.push(visit);
    visits.value = kept.slice(-HISTORY);
    current.value = visits.value.length - 1;
  };

  /**
   * go goes the given number of places through the history: -1 back, 1 forward. A place whose file has closed opens
   * it again. Past either end it does nothing.
   */
  const go = (steps: number): void => {
    const here = locate();
    // where the user is now is kept, so going the other way comes back to it; a place no jump led to is a jump
    if (here && visits.value[current.value]?.place.key === here.place.key) {
      visits.value = visits.value.with(current.value, here);
    } else if (here) {
      record(here);
    }
    const target = visits.value[current.value + steps];
    if (!target) return;
    current.value += steps;
    show(target.place, target.position, "history");
  };

  /** locate returns a visit to a file at its caret: the active tab's file unless one is given. Undefined if none. */
  function locate(place = activeFile()): Visit | undefined {
    const caret = place && findModel(place.key)?.caret;
    return place ? { place, position: { lineNumber: caret?.line ?? 1, column: caret?.column ?? 1 } } : undefined;
  }

  /** isSame reports whether a visit is to the same line of the same file as another. */
  function isSame(visit: Visit | undefined, other: Visit): boolean {
    return !!visit && visit.place.key === other.place.key && visit.position.lineNumber === other.position.lineNumber;
  }

  /** activate makes the given group the active one, as the user's pick. A group the layout lacks changes nothing. */
  const activate = (groupId: string): void => {
    const group = listGroups().find((one) => one.id === groupId);
    if (!group) return;
    activeId.value = groupId;
    release(group.active);
  };

  /**
   * release ends Follow the agent's hold on the tab it showed, as the user picks a tab. The tab Follow showed stays
   * open when it is the one the user picks.
   */
  function release(tabId: string | null): void {
    if (tabId && tabId === agentTab.value) pin(tabId);
    agentTab.value = "";
  }

  /** pin keeps a preview tab: it is no longer replaced by the next file shown. */
  const pin = (tabId: string): void => {
    listGroups().forEach((group) =>
      group.tabs.filter((tab) => tab.id === tabId).forEach((tab) => (tab.preview = false)),
    );
  };

  /**
   * confirmClose asks once whether to close the given tabs when that would leave unsaved files with no tab, and returns
   * the answer. It returns true when there is nothing to ask.
   */
  const confirmClose = async (tabs: Tab[]): Promise<boolean> => {
    const unsaved = listUnsaved(tabs);
    const body = `${list(unsaved)} ${unsaved.length > 1 ? "have" : "has"} changes that are not saved.`;
    const question = { title: "Close without saving?", body, confirm: "Close", destructive: true };
    return !unsaved.length || (await confirm.open(question)) === true;
  };

  /** listUnsaved lists the paths of the files with changes that are not saved that no tab but the given ones shows. */
  function listUnsaved(tabs: Tab[]): string[] {
    const others = listTabs()
      .filter((tab) => !tabs.includes(tab))
      .map((tab) => tab.key);
    return [...new Set(tabs.map((tab) => tab.key))].flatMap((key) => {
      const open = findModel(key);
      return open?.unsaved && !others.includes(key) ? [open.place.rel] : [];
    });
  }

  /** close closes a tab. The model of its file closes too when no other tab shows the file. */
  const close = (tabId: string): void => {
    const home = find(tabId);
    if (!home) return;
    const tab = remove(home.group, home.index);
    closeGroup(home.group);
    if (tab) closeFile(tab.key);
  };

  /**
   * split splits the active group: a copy of its active tab in a new group beside it ("row") or below it ("column").
   */
  const split = (direction: "row" | "column"): void => {
    const group = activeGroup();
    const rows = state.value.rows;
    const row = group && rows.find((candidate) => candidate.columns.includes(group));
    if (!group || !row) return;
    const active = group.tabs.filter((tab) => tab.id === group.active).map((tab) => ({ ...tab, preview: false }));
    const copy: Group = { id: crypto.randomUUID(), tabs: active, active: group.active };
    if (direction === "row") row.columns.splice(row.columns.indexOf(group) + 1, 0, copy);
    else rows.splice(rows.indexOf(row) + 1, 0, { columns: [copy] });
    activeId.value = copy.id;
  };

  /**
   * move moves a tab to a group, or into a new one, by drag. The tab lands at the given place in the group. The id of
   * a group the layout lacks makes a new group right after the tab's own. A tab no group holds stays where it is.
   */
  const move = (tabId: string, groupId: string, index: number): void => {
    const home = find(tabId);
    if (!home) return;
    if (!listGroups().some((group) => group.id === groupId)) {
      const row = state.value.rows.find((candidate) => candidate.columns.includes(home.group));
      row?.columns.splice(row.columns.indexOf(home.group) + 1, 0, { id: groupId, tabs: [], active: null });
    }
    const target = listGroups().find((group) => group.id === groupId);
    const tab = target && remove(home.group, home.index);
    if (!target || !tab) return;
    const twin = target.tabs.findIndex((other) => other.id === tabId);
    if (twin >= 0) target.tabs.splice(twin, 1);
    target.tabs.splice(Math.min(index, target.tabs.length), 0, tab);
    target.active = tabId;
    activeId.value = target.id;
    closeGroup(home.group);
  };

  /**
   * rename renames or moves a file or folder on disk, then moves the tabs of every file in it to the file's new
   * place. Each model keeps its text and whether it is unsaved. It throws if Go refuses the rename.
   */
  const rename = async (source: string, target: string): Promise<void> => {
    await workspaceApi.Rename(source, target);
    const key = (await loadQuery(placeQuery(source)))?.key;
    if (!key) return;
    for (const old of listUnder(key)) {
      const place = await loadQuery(placeQuery(target + old.path.slice(source.length)));
      if (place) retarget(old.key, place);
    }
  };

  /** retarget moves the tabs, the place and the model of a renamed file to its new place. */
  function retarget(key: string, place: Place): void {
    for (const group of listGroups()) {
      for (const tab of group.tabs.filter((one) => one.key === key)) {
        tab.key = place.key;
        const id = tabId(tab);
        if (group.active === tab.id) group.active = id;
        tab.id = id;
      }
    }
    places.delete(key);
    places.set(place.key, place);
    visits.value = visits.value.map((visit) => (visit.place.key === key ? { ...visit, place } : visit));
    renameModel(key, place);
  }

  // a file a rename or a fix edited where no tab shows it gets a tab after the shown one, which stays shown
  watch(
    () => unsavedModels().filter((model) => !find(tabId({ key: model.place.key }))),
    (untabbed) => {
      const group = activeGroup();
      if (!group) return;
      for (const model of untabbed) {
        places.set(model.place.key, model.place);
        const index = group.tabs.findIndex((tab) => tab.id === group.active) + 1;
        const id = tabId({ key: model.place.key });
        group.tabs.splice(index, 0, { id, key: model.place.key, preview: false });
        group.active ??= id;
      }
    },
  );

  return {
    wiki,
    panel,
    compareFile,
    mode,
    layout,
    listGroups,
    listTabs,
    activeGroup,
    activeTab,
    activeFile,
    isFollowed,
    placeOf,
    isDrawable,
    show,
    showVisual,
    open,
    findRequest,
    clearRequest,
    listSelection,
    showPanel,
    showWiki,
    selectFiles,
    record,
    go,
    activate,
    pin,
    confirmClose,
    close,
    split,
    move,
    rename,
    applyFix,
  };
});

/**
 * tabId returns the id of the tab that opens the given file or diff. The DOM, a drag between windows and the saved
 * layout know a tab by its id.
 */
export function tabId({ key, side, root = "", hash = "" }: Omit<Tab, "id" | "preview">): string {
  return side ? `diff:${side}@${hash}:${root}:${key}` : `file:${key}`;
}

/** toRange returns what the editor selects as a range: a position as the empty range at it. */
function toRange(selection: IPosition | IRange): IRange {
  if ("startLineNumber" in selection) return selection;
  const { lineNumber, column } = selection;
  return { startLineNumber: lineNumber, startColumn: column, endLineNumber: lineNumber, endColumn: column };
}
