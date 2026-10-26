/**
 * The editor store: the layout of groups in rows and columns with their tabs, kept per workspace in local storage,
 * the active group and tab, the view each tab shows, the files selected in the explorer, the history of the places
 * the user jumped between, and the one function that shows a place. The files' texts are the models'; this store
 * holds no text, no problem and no caret but the history's. The panes' sizes belong to the splitters.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { until, useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { ref, shallowReactive, shallowRef, toRaw, watch } from "vue";
import { useRouter } from "vue-router";

import type { Place } from "@services/workspace/models";
import * as workspaceApi from "@services/workspace/service";
import type { IPosition } from "monaco-editor";
import { closeModel, findModel, openModel, renameModel, unsavedModels } from "../pages/ide/editor/models";
import { diffTab, fileTab, parseTab, VIEWS } from "../pages/ide/editor/tabs";
import { drawablesQuery, loadQuery, placeQuery, workspacesQuery } from "../queries";
import { THIS_WINDOW, useWindowsStore } from "./windows";
import { useWorkspaceStore } from "./workspace";

/** The views a tab can show. VIEWS says which ones a format allows. */
export type View = "code" | "visual" | "image" | "split";

/** One tab: its id (see pages/ide/editor/tabs.ts), whether it is the preview tab, and its view. */
export type Tab = { id: string; preview: boolean; view: View };
/** One group of tabs with its active one. A group is one column of the layout. */
export type Group = { id: string; tabs: Tab[]; active: string | null };

/** The groups laid out in rows of columns. */
export type Layout = { rows: { columns: Group[] }[] };

/** A workspace's saved layout: the groups, the active group, and the path of each file a tab holds. */
type SavedLayout = { layout: Layout; active: string; paths: Record<string, string> };

/** One place the user was in: a file and a caret position in it. */
type Visit = { place: Place; position: IPosition };

/** How many places the history keeps. */
const HISTORY = 50;

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
  const state = ref<Layout>(makeLayout());
  const activeId = shallowRef(state.value.rows[0]?.columns[0]?.id ?? "");
  // the place of each file a tab holds, by key: the tab's id has only the key, which is not a path
  const places = shallowReactive(new Map<string, Place>());
  // whether changes are written down: not while a workspace's layout is being brought back, and in the main window only
  const tracking = shallowRef(false);
  // the position a file was shown at, which the active group's editor of that file moves its caret to once
  const request = shallowRef<{ key: string; position: IPosition } | null>(null);
  // the files the explorer's last selection holds, for the compare of two
  const selection = shallowRef<Place[]>([]);
  // the places the user jumped between, the oldest first, and the index of the one the user is at
  const visits = shallowRef<Visit[]>([]);
  const current = shallowRef(-1);

  const { mutate: openPlace } = useMutation({
    mutation: async (request: { place: Place; position?: IPosition; created: boolean }) => {
      await openFile(request.place, request.position);
      // a file shown as code that defines something the view draws, a coat of arms, opens drawn
      if (!request.created || defaultView(request.place) !== "code") return;
      if ((await loadQuery(drawablesQuery(request.place.path)))?.length) setView(fileTab(request.place.key), "visual");
    },
  });
  // a file that cannot open is closed and forgotten
  const { mutate: openPath } = useMutation({
    mutation: async (path: string) => {
      const place = await loadQuery(placeQuery(path));
      if (!place) return;
      await openFile(place).catch((error: unknown) => {
        const id = fileTab(place.key);
        while (find(id)) close(id);
        places.delete(place.key);
        throw error;
      });
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
  // a file deleted on disk closes its tabs, unless it has edits not saved, which stay to be saved again
  Events.On("fs:changed", ({ data }) => {
    if (!data.deleted) return;
    void loadQuery(placeQuery(data.path)).then((place) => {
      if (!place) return;
      const id = fileTab(place.key);
      while (!findModel(place.key)?.unsaved && find(id)) close(id);
    });
  });

  /** Makes a layout of one empty group. */
  function makeLayout(): Layout {
    return { rows: [{ columns: [{ id: crypto.randomUUID(), tabs: [], active: null }] }] };
  }

  /**
   * Brings the given workspace's saved layout back, or starts an empty one. Tabs are reopened unless the workspace's
   * record says to reset them. Only the main window keeps a layout; another window starts empty.
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
    const record = await until(() => records.value?.find((candidate) => candidate.id === id)).toBeTruthy();
    // Go finds a file's place only once the workspace is open
    await until(() => workspace.isOpen()).toBe(true);
    if (workspace.activeId() !== id) return;
    if (saved && !record.resetTabs) {
      state.value = structuredClone(toRaw(saved.layout));
      const groups = listGroups();
      activeId.value = groups.some((group) => group.id === saved.active) ? saved.active : (groups[0]?.id ?? "");
      Object.values(saved.paths).forEach((path) => openPath(path));
    }
    tracking.value = true;
  }

  /**
   * Remembers a file's place and opens its model. A position, when given, becomes the model's caret, which the Form
   * view shows the entry at, and is asked of the file's editor.
   */
  async function openFile(place: Place, position?: IPosition): Promise<void> {
    places.set(place.key, place);
    if (!VIEWS[place.format].views.includes("code")) return;
    const model = await openModel(place);
    if (!position) return;
    model.caret = { line: position.lineNumber, column: position.column };
    request.value = { key: place.key, position };
  }

  /** Lists all the groups, row by row. */
  function listGroups(): Group[] {
    return state.value.rows.flatMap((row) => row.columns);
  }

  /** Lists every open tab, group by group. */
  function listTabs(): Tab[] {
    return listGroups().flatMap((group) => group.tabs);
  }

  /**
   * The tab with the given id and the group holding it, looked for in the given group first, the active group unless
   * one is given. It is null when none holds it.
   */
  function find(tabId: string, first = activeGroup()): { group: Group; index: number } | null {
    for (const group of [first, ...listGroups()]) {
      const index = group?.tabs.findIndex((tab) => tab.id === tabId) ?? -1;
      if (group && index >= 0) return { group, index };
    }
    return null;
  }

  /** Removes a tab from its group and returns it. The tab next to it becomes the active one if it was. */
  function remove(group: Group, index: number): Tab | undefined {
    const [tab] = group.tabs.splice(index, 1);
    if (tab && group.active === tab.id) group.active = (group.tabs[index] ?? group.tabs[index - 1])?.id ?? null;
    return tab;
  }

  /**
   * Adds a tab to a group after its active tab. A preview tab takes the place of the one already there, unless that
   * one has unsaved edits: those keep it, as a tab of its own.
   */
  function add(group: Group, tab: Tab): void {
    const index = tab.preview ? group.tabs.findIndex((other) => other.preview) : -1;
    const preview = group.tabs[index];
    if (preview && !findModel(parseTab(preview.id).key)?.unsaved) {
      group.tabs.splice(index, 1, tab);
      closeFile(parseTab(preview.id).key);
    } else {
      if (preview) preview.preview = false;
      group.tabs.splice(group.tabs.findIndex((other) => other.id === group.active) + 1, 0, tab);
    }
    group.active = tab.id;
  }

  /** Closes a group left with no tab, unless it is the last. The group that takes its place gets the focus. */
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

  /** Closes the model of a file no tab shows any more, and forgets its place. */
  function closeFile(key: string): void {
    if (listGroups().some((group) => group.tabs.some((tab) => parseTab(tab.id).key === key))) return;
    places.delete(key);
    closeModel(key);
  }

  /** The view a file opens in: Split for a GUI file, else its format's first. */
  function defaultView(place: Place): View {
    const views = VIEWS[place.format].views;
    return views.includes("split") ? "split" : (views[0] ?? "code");
  }

  /** The active group, or null when there are none. */
  const activeGroup = (): Group | null =>
    listGroups().find((group) => group.id === activeId.value) ?? listGroups()[0] ?? null;

  /** The active group's active tab, or null when there is none. */
  const activeTab = (): Tab | null => {
    const group = activeGroup();
    return group?.tabs.find((tab) => tab.id === group.active) ?? null;
  };

  /** The layout of groups in rows of columns. */
  const layout = (): Layout => state.value;

  /** The place of a tab's file, a diff tab's too, or null when no open tab shows the file. */
  const placeOf = (tabId: string): Place | null => places.get(parseTab(tabId).key) ?? null;

  /** The place of the active tab's file, for a diff tab too, or null when there is none. */
  const activeFile = (): Place | null => {
    const tab = activeTab();
    return tab ? placeOf(tab.id) : null;
  };

  /**
   * Opens a tab of a place, a diff tab among them, and makes its group the active one. The tab is looked for in the
   * given group first, the active group unless one is given, then in the others; a tab no group holds goes into the
   * first. The history keeps the switch, from where the user was before it.
   */
  const open = (tabId: string, place: Place, groupId?: string): void => {
    places.set(place.key, place);
    const preferred = listGroups().find((one) => one.id === groupId) ?? activeGroup();
    const home = find(tabId, preferred);
    const group = home?.group ?? preferred;
    if (!group) return;
    record(locate(place));
    if (!home) add(group, { id: tabId, preview: false, view: "code" });
    group.active = tabId;
    activeId.value = group.id;
  };

  /**
   * Shows a place. It opens the file in the view the request calls for: the explorer's single click opens a
   * preview tab, a definition opens the code view at its position, and a coat opens the Visual view. The file
   * opens in this window, or in the window that already holds it. The main window shows the IDE first. The history
   * keeps the jump, unless the history is what shows the place.
   */
  const show = (
    place: Place,
    position?: IPosition,
    source: "explorer" | "navigate" | "palette" | "graph" | "health" | "history" = "navigate",
  ): void => {
    const windows = useWindowsStore();
    const holder = windows.holderOf({ file: place.key });
    if (holder && holder !== THIS_WINDOW) {
      windows.showElsewhere(holder, place);
      return;
    }
    const group = activeGroup();
    if (!group) return;
    if (source !== "history") record(position ? { place, position } : locate(place));
    const page = router.currentRoute.value.name;
    if (THIS_WINDOW === "main" && page && page !== "workspace-ide") {
      void router.push({ name: "workspace-ide", params: { id: workspace.activeId() } });
    }
    const id = fileTab(place.key);
    const tab = group.tabs.find((other) => other.id === id);
    if (tab) {
      if (source !== "explorer") tab.preview = false;
      group.active = id;
    } else {
      add(group, { id, preview: source === "explorer", view: defaultView(place) });
    }
    openPlace({ place, position, created: !tab });
  };

  /** The position a file was last shown at that no editor has moved to yet, or undefined when there is none. */
  const findRequest = (key: string): IPosition | undefined =>
    request.value?.key === key ? request.value.position : undefined;

  /** Forgets the position asked of an editor, once the editor has moved its caret there. */
  const clearRequest = (): void => {
    request.value = null;
  };

  /** The files selected in the explorer, in the order the tree lists them. A folder selected is left out. */
  const listSelection = (): Place[] => selection.value;

  /** Keeps the files the explorer has selected. */
  const selectFiles = (files: Place[]): void => {
    selection.value = files;
  };

  /**
   * Records a jump to a place in the history. The place the user leaves is kept before it, and the places ahead of
   * the one the user is at are dropped, as a browser drops them.
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
   * Goes the given number of places through the history: -1 back, 1 forward. A place whose file has closed opens it
   * again. Past either end it does nothing.
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

  /** A visit to a file at its caret: the active tab's file unless one is given. It is undefined when there is none. */
  function locate(place = activeFile()): Visit | undefined {
    const caret = place && findModel(place.key)?.caret;
    return place ? { place, position: { lineNumber: caret?.line ?? 1, column: caret?.column ?? 1 } } : undefined;
  }

  /** Whether a visit is to the same line of the same file as another. */
  function isSame(visit: Visit | undefined, other: Visit): boolean {
    return !!visit && visit.place.key === other.place.key && visit.position.lineNumber === other.position.lineNumber;
  }

  /** Makes the given group the active one. A group the layout lacks changes nothing. */
  const activate = (groupId: string): void => {
    if (listGroups().some((group) => group.id === groupId)) activeId.value = groupId;
  };

  /** Keeps a preview tab: it is no longer replaced by the next file shown. */
  const pin = (tabId: string): void => {
    listGroups().forEach((group) =>
      group.tabs.filter((tab) => tab.id === tabId).forEach((tab) => (tab.preview = false)),
    );
  };

  /** Closes a tab. The model of its file closes too when no other tab shows the file. */
  const close = (tabId: string): void => {
    const home = find(tabId);
    if (!home) return;
    remove(home.group, home.index);
    closeGroup(home.group);
    closeFile(parseTab(tabId).key);
  };

  /** Sets the view a tab shows. */
  const setView = (tabId: string, view: View): void => {
    const home = find(tabId);
    const tab = home?.group.tabs[home.index];
    if (tab) tab.view = view;
  };

  /** Splits the active group: a copy of its active tab in a new group beside it ("row") or below it ("column"). */
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
   * Moves a tab to a group, or into a new one, by drag. The tab lands at the given place in the group. The id of a
   * group the layout lacks makes a new group right after the tab's own. A tab no group holds stays where it is.
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
   * Renames or moves a file or folder on disk, then moves the tabs of every file in it to the file's new place. Each
   * model keeps its text and whether it is unsaved. It throws if Go refuses the rename.
   */
  const rename = async (from: string, to: string): Promise<void> => {
    await workspaceApi.Rename(from, to);
    const key = (await loadQuery(placeQuery(from)))?.key;
    if (!key) return;
    const moved = [...places.values()].filter((place) => place.key === key || place.key.startsWith(`${key}/`));
    for (const old of moved) {
      const place = await loadQuery(placeQuery(to + old.path.slice(from.length)));
      if (place) retarget(old.key, place);
    }
  };

  /** Moves the tabs, the place and the model of a renamed file to its new place. */
  function retarget(key: string, place: Place): void {
    for (const group of listGroups()) {
      for (const tab of group.tabs) {
        const target = parseTab(tab.id);
        if (target.key !== key) continue;
        const id =
          target.tab === "file" ? fileTab(place.key) : diffTab(target.side, target.root, place.key, target.hash);
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
    () => unsavedModels().filter((model) => !find(fileTab(model.place.key))),
    (untabbed) => {
      const group = activeGroup();
      if (!group) return;
      for (const model of untabbed) {
        places.set(model.place.key, model.place);
        const index = group.tabs.findIndex((tab) => tab.id === group.active) + 1;
        const id = fileTab(model.place.key);
        group.tabs.splice(index, 0, { id, preview: false, view: "code" });
        group.active ??= id;
      }
    },
  );

  return {
    layout,
    listTabs,
    activeGroup,
    activeTab,
    activeFile,
    placeOf,
    show,
    open,
    findRequest,
    clearRequest,
    listSelection,
    selectFiles,
    record,
    go,
    activate,
    pin,
    close,
    setView,
    split,
    move,
    rename,
  };
});
