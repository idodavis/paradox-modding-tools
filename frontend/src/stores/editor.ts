/** The editor store: this window's groups of tabs, its mode, and the history of places the user jumped between. */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { StorageSerializers, until, useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { ref, shallowReactive, shallowRef, toRaw, watch } from "vue";
import { useRouter } from "vue-router";

import { Format } from "@internal/files/models";
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

/** A Mode is what the IDE shows of the active file: its code, or Visual, the picture it draws. */
export type Mode = "code" | "visual";

/** A DiffSide names the two revisions a diff tab compares. */
export type DiffSide = "changes" | "staged" | "conflict" | "commit";

/** A Tab is one tab in a group. It shows a file, a diff of a file, or the start page, whose key is empty. */
export type Tab = {
  id: string;
  key: string;
  side?: DiffSide;
  root?: string;
  hash?: string;
  start?: true;
  preview: boolean;
};
/** A Group is one column of tabs, one of them active. */
export type Group = { id: string; tabs: Tab[]; active: string | null };

/** A Side is a side of a group where a new group goes. */
export type Side = "left" | "right" | "up" | "down";

/** A Layout is the groups laid out in rows of columns. */
export type Layout = { rows: { columns: Group[] }[] };

/** A SavedLayout is a workspace's saved layout. */
type SavedLayout = { layout: Layout; active: string; paths: Record<string, string> };

/** A Source is what showed a place: the explorer, Source control's lists, a jump, the history, or Follow the agent. */
export type Source = "explorer" | "changes" | "navigate" | "history" | "agent" | "edit";

/** A Visit is one place the user was in: a file and a caret position in it. */
type Visit = { place: Place; position: IPosition };

/** HISTORY is how many places the history keeps. */
const HISTORY = 50;
/** RECENT is how many recent files the start page lists. */
const RECENT = 5;
/** START is the start page's tab. */
const START = { key: "", start: true } as const;

/** SOURCES holds what each source does to the place it shows: preview tab, history record, Follow the agent. */
const SOURCES: Record<Source, { preview?: true; record?: false; follow?: true }> = {
  explorer: { preview: true },
  changes: { preview: true },
  navigate: {},
  history: { record: false },
  agent: { preview: true, follow: true },
  edit: { follow: true },
};

export const useEditorStore = defineStore("editor", () => {
  const workspace = useWorkspaceStore();
  const { data: records } = useQuery(workspacesQuery);
  const router = useRouter();
  // Listening to storage would pause this store's writes for a tick and drop a change.
  const layouts = useLocalStorage<Record<string, SavedLayout>>(
    "pmt.ide.layouts",
    {},
    { listenToStorageChanges: false },
  );
  const wiki = useLocalStorage("pmt.ide.wiki", false);
  const panel = useLocalStorage("pmt.ide.panel", "");
  const compareFile = useLocalStorage<Place | null>("pmt.ide.compare", null, { serializer: StorageSerializers.object });
  const recent = useLocalStorage<Place[]>(() => `pmt.ide.recent.${workspace.activeId()}`, []);
  const mode = shallowRef<Mode>("code");
  const state = ref<Layout>(makeLayout());
  const activeId = shallowRef(state.value.rows[0]?.columns[0]?.id ?? "");
  // A tab holds only a file's key, which is not a path, so the place is kept here.
  const places = shallowReactive(new Map<string, Place>());
  // Layout changes are saved only in the main window, and not while a layout loads.
  const tracking = shallowRef(false);
  const request = shallowRef<{ key: string; range: IRange } | null>(null);
  const agentTab = shallowRef("");
  const selectedFiles = shallowRef<Place[]>([]);
  const visits = shallowRef<Visit[]>([]);
  const current = shallowRef(-1);

  const { mutate: openPlace } = useMutation({
    mutation: ({ place, range }: { place: Place; range?: IRange }) => openFile(place, range),
    onError: (_error, { place }) => {
      recent.value = recent.value.filter((candidate) => candidate.key !== place.key);
    },
  });
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

  /** applyFixAsync applies a quick fix: creates its file, edits the models, and shows the place it reveals. */
  async function applyFixAsync(fix: Fix): Promise<void> {
    if (fix.creates) {
      const place = await loadQuery(placeQuery(fix.creates));
      if (place?.format === Format.FormatLocalization) await workspaceApi.CreateLocalization(fix.creates);
      else await workspaceApi.Create(fix.creates, false);
    }
    for (const [path, edits] of Object.entries(fix.edits ?? {})) {
      const place = await loadQuery(placeQuery(path));
      if (place) applyEdits(await openModel(place), edits ?? []);
    }
    if (!fix.reveal) return;
    const group = activeGroup();
    const row = state.value.rows.find((candidate) => !!group && candidate.columns.includes(group));
    const neighbor = group && row?.columns[row.columns.indexOf(group) + 1];
    if (neighbor) activate(neighbor.id);
    else split("row");
    show(fix.reveal.place, fix.reveal.range);
  }

  const { mutate: applyFix } = useMutation({ mutation: applyFixAsync });

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

  /** listUnder lists the places of the open files at the given key or under it. */
  function listUnder(key: string): Place[] {
    return [...places.values()].filter((place) => place.key === key || place.key.startsWith(`${key}/`));
  }

  /** makeLayout makes a layout of one empty group. */
  function makeLayout(): Layout {
    return { rows: [{ columns: [{ id: crypto.randomUUID(), tabs: [], active: null }] }] };
  }

  /** loadLayout restores the given workspace's saved layout, or starts an empty one. Only the main window restores. */
  async function loadLayout(id: string): Promise<void> {
    const saved = layouts.value[id];
    tracking.value = false;
    for (const key of places.keys()) closeModel(key);
    places.clear();
    selectedFiles.value = [];
    visits.value = [];
    current.value = -1;
    state.value = makeLayout();
    activeId.value = state.value.rows[0]?.columns[0]?.id ?? "";
    if (!id || THIS_WINDOW !== "main") return;
    const { record } = await until(() => records.value?.find((row) => row.record.id === id)).toBeTruthy();
    // Go finds a file's place only once the workspace is open.
    await until(() => workspace.isOpen()).toBe(true);
    if (workspace.activeId() !== id) return;
    if (saved && !record.resetTabs) {
      state.value = structuredClone(toRaw(saved.layout));
      const groups = listGroups();
      activeId.value = groups.some((group) => group.id === saved.active) ? saved.active : (groups[0]?.id ?? "");
      // Saving before the files are found would lose the saved paths.
      await Promise.allSettled(Object.values(saved.paths).map((path) => openPath(path)));
      if (workspace.activeId() !== id) return;
    }
    if (!record.hideStartPage) showStart();
    tracking.value = true;
  }

  /** showStart makes the start tab the first group's active one. */
  function showStart(): void {
    const group = listGroups()[0];
    const id = tabId(START);
    if (!group) return;
    if (find(id)) move(id, group.id, 0);
    else group.tabs.unshift({ ...START, id, preview: false });
    group.active = id;
    activeId.value = group.id;
  }

  /** openFile remembers a file's place and opens its model, if the file is text. */
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

  /** find returns the group and index of the tab with the given id, or null. It looks in the given group first. */
  function find(id: string, first = activeGroup()): { group: Group; index: number } | null {
    for (const group of [first, ...listGroups()]) {
      const index = group?.tabs.findIndex((tab) => tab.id === id) ?? -1;
      if (group && index >= 0) return { group, index };
    }
    return null;
  }

  /** remove takes a tab out of its group and returns it. */
  function remove(group: Group, index: number): Tab | undefined {
    const [tab] = group.tabs.splice(index, 1);
    if (tab && group.active === tab.id) group.active = (group.tabs[index] ?? group.tabs[index - 1])?.id ?? null;
    return tab;
  }

  /** add makes a tab the active tab of a group. A new preview tab replaces the group's preview tab. */
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

  /** closeGroup removes a group that has no tabs, unless it is the last. */
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

  /** closeFile closes the model and forgets the place of a file no tab shows. */
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

  /** placeOf returns the place of the file with the given key, or null if no open tab shows it. */
  const placeOf = (key: string): Place | null => places.get(key) ?? null;

  /** activeFile returns the place of the active tab's file, for a diff tab too, or null when there is none. */
  const activeFile = (): Place | null => {
    const tab = activeTab();
    return tab ? placeOf(tab.key) : null;
  };

  /** isDrawable reports whether the given tab is a file tab whose file Visual can draw. */
  function isDrawable(tab: Tab | null | undefined): boolean {
    return !!tab && !tab.side && !!placeOf(tab.key)?.drawable;
  }

  watch(
    () => isDrawable(activeTab()),
    (drawable) => {
      if (!drawable) mode.value = "code";
    },
  );

  /** isFollowed reports whether the active tab is the one Follow the agent showed last. */
  const isFollowed = (): boolean => !!agentTab.value && activeTab()?.id === agentTab.value;

  /**
   * open makes a tab of a file, a diff or the start page the active one, as the user's pick. A new tab is a preview tab
   * if the given source opens one.
   */
  const open = (
    target: Omit<Tab, "id" | "preview">,
    place?: Place | null,
    groupId?: string,
    source: Source = "navigate",
  ): void => {
    const id = tabId(target);
    const preferred = listGroups().find((candidate) => candidate.id === groupId) ?? activeGroup();
    const group = find(id, preferred)?.group ?? preferred;
    if (!group) return;
    if (place) {
      places.set(place.key, place);
      record(locate(place));
    }
    add(group, { ...target, id, preview: !!SOURCES[source].preview });
    activeId.value = group.id;
    release(id);
  };

  /** show opens a file at a position or range, in the window that holds it. It does nothing for a folder. */
  const show = (place: Place, selection?: IPosition | IRange, source: Source = "navigate"): void => {
    if (place.isDir) return;
    const windows = useWindowsStore();
    const target = windows.holderOf(place.key) || (HAS_EDITORS ? THIS_WINDOW : "main");
    if (target !== THIS_WINDOW) return windows.showElsewhere(target, place, selection, source);
    const group = activeGroup();
    if (!group) return;
    const row = SOURCES[source];
    recent.value = [place, ...recent.value.filter((candidate) => candidate.key !== place.key)].slice(0, RECENT);
    const range = selection && toRange(selection);
    const start = range && { lineNumber: range.startLineNumber, column: range.startColumn };
    if (row.record !== false) record(start ? { place, position: start } : locate(place));
    const page = router.currentRoute.value.name;
    if (THIS_WINDOW === "main" && page && page !== "workspace-ide") {
      void router.push({ name: "workspace-ide", params: { id: workspace.activeId() } });
    }
    const id = tabId({ key: place.key });
    const isShown = activeTab()?.id === id;
    // Follow replaces only its own preview tab, so the user's is pinned.
    if (row.follow && row.preview) {
      group.tabs.filter((tab) => tab.preview && tab.id !== agentTab.value).forEach((tab) => pin(tab.id));
    }
    agentTab.value = row.follow ? id : "";
    const tab = group.tabs.find((candidate) => candidate.id === id);
    if (tab && !row.preview) tab.preview = false;
    places.set(place.key, place);
    add(group, { id, key: place.key, preview: !!row.preview });
    openPlace({ place, range });
    if (!isShown && !row.follow && place.drawable && useSettingsStore().isOn("draw.openVisual")) {
      mode.value = "visual";
    }
  };

  /** showVisual shows a place, then enters Visual if the file is drawable and active in this window. */
  const showVisual = (place: Place, selection?: IPosition | IRange): void => {
    show(place, selection);
    if (place.drawable && activeFile()?.key === place.key) mode.value = "visual";
  };

  /** findRequest returns the range a file was shown with, or undefined once an editor has moved to it. */
  const findRequest = (key: string): IRange | undefined =>
    request.value?.key === key ? request.value.range : undefined;

  /** clearRequest forgets the range asked of an editor. */
  const clearRequest = (): void => {
    request.value = null;
  };

  /** listSelection lists the files selected in the explorer. */
  const listSelection = (): Place[] => selectedFiles.value;

  /** listRecent lists the last files shown in the active workspace, the newest first. */
  const listRecent = (): Place[] => recent.value;

  /** showPanel shows the bottom panel on the given tab, leaving Visual. */
  const showPanel = (tab: string): void => {
    mode.value = "code";
    panel.value = tab;
  };

  /** showWiki shows the Wiki pane, leaving Visual. */
  const showWiki = (): void => {
    mode.value = "code";
    wiki.value = true;
  };

  /** selectFiles keeps the files the explorer has selected. */
  const selectFiles = (files: Place[]): void => {
    selectedFiles.value = files;
  };

  /** record records a jump to a place in the history, dropping the places ahead of the current one. */
  const record = (target: Visit | undefined): void => {
    if (!target) return;
    const here = locate();
    const kept = visits.value.slice(0, current.value + 1);
    if (here && kept.at(-1)?.place.key === here.place.key) kept.pop();
    for (const visit of here ? [here, target] : [target]) if (!isSame(kept.at(-1), visit)) kept.push(visit);
    visits.value = kept.slice(-HISTORY);
    current.value = visits.value.length - 1;
  };

  /** go moves through the history by the given number of places, -1 back and 1 forward. It stops at either end. */
  const go = (steps: number): void => {
    const here = locate();
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

  /** locate returns a visit to the given file, or the active one, at its caret. It is undefined for no file. */
  function locate(place = activeFile()): Visit | undefined {
    const caret = place && findModel(place.key)?.caret;
    return place ? { place, position: { lineNumber: caret?.line ?? 1, column: caret?.column ?? 1 } } : undefined;
  }

  /** isSame reports whether a visit is to the same line of the same file as another. */
  function isSame(visit: Visit | undefined, other: Visit): boolean {
    return !!visit && visit.place.key === other.place.key && visit.position.lineNumber === other.position.lineNumber;
  }

  /** activate makes the given group the active one, as the user's pick. */
  const activate = (groupId: string): void => {
    const group = listGroups().find((candidate) => candidate.id === groupId);
    if (!group) return;
    activeId.value = groupId;
    release(group.active);
  };

  /** release ends Follow the agent's hold on its tab, pinning the tab if the user picked it. */
  function release(id: string | null): void {
    if (id && id === agentTab.value) pin(id);
    agentTab.value = "";
  }

  /** pin keeps a preview tab open. */
  const pin = (id: string): void => {
    listGroups().forEach((group) => group.tabs.filter((tab) => tab.id === id).forEach((tab) => (tab.preview = false)));
  };

  /** confirmClose asks whether to close the given tabs if that leaves unsaved files. It is true if none would. */
  const confirmClose = async (tabs: Tab[]): Promise<boolean> => {
    const unsaved = listUnsaved(tabs);
    const body = `${list(unsaved)} ${unsaved.length > 1 ? "have" : "has"} changes that are not saved.`;
    const question = { title: "Close without saving?", body, confirm: "Close", destructive: true };
    return !unsaved.length || (await confirm.open(question)) === true;
  };

  /** listUnsaved lists the relative paths of the given tabs' unsaved files that no other tab shows. */
  function listUnsaved(tabs: Tab[]): string[] {
    const others = listTabs()
      .filter((tab) => !tabs.includes(tab))
      .map((tab) => tab.key);
    return [...new Set(tabs.map((tab) => tab.key))].flatMap((key) => {
      const model = findModel(key);
      return model?.unsaved && !others.includes(key) ? [model.place.rel] : [];
    });
  }

  /** close closes a tab, and its file's model if no other tab shows the file. */
  const close = (id: string): void => {
    const home = find(id);
    if (!home) return;
    const tab = remove(home.group, home.index);
    closeGroup(home.group);
    if (tab) closeFile(tab.key);
  };

  /** insertGroup adds a group with the given tabs on a side of a group and returns it as the layout holds it. */
  function insertGroup(group: Group, side: Side, tabs: Tab[], active: string | null): Group {
    const rows = state.value.rows;
    const row = rows.find((candidate) => candidate.columns.includes(group)) ?? rows[0];
    const newGroup: Group = { id: crypto.randomUUID(), tabs, active };
    const offset = side === "right" || side === "down" ? 1 : 0;
    if (side === "left" || side === "right") row.columns.splice(row.columns.indexOf(group) + offset, 0, newGroup);
    else rows.splice(rows.indexOf(row) + offset, 0, { columns: [newGroup] });
    // Return the reactive copy, so a tab added later is drawn at once.
    return listGroups().find((candidate) => candidate.id === newGroup.id) ?? newGroup;
  }

  /** split copies the active tab into a new group beside it ("row") or below it ("column"), if canSplit allows. */
  const split = (direction: "row" | "column"): void => {
    const group = activeGroup();
    if (!group || !canSplit(activeTab())) return;
    const active = group.tabs.filter((tab) => tab.id === group.active).map((tab) => ({ ...tab, preview: false }));
    activeId.value = insertGroup(group, direction === "row" ? "right" : "down", active, group.active).id;
  };

  /** move moves a tab to an index of a group, or into a new group on a side of it. */
  const move = (id: string, groupId: string, destination: number | Side): void => {
    const home = find(id);
    const group = listGroups().find((candidate) => candidate.id === groupId);
    const tab = home && group && remove(home.group, home.index);
    if (!home || !group || !tab) return;
    const target = typeof destination === "number" ? group : insertGroup(group, destination, [], null);
    const existing = target.tabs.findIndex((other) => other.id === id);
    if (existing >= 0) target.tabs.splice(existing, 1);
    target.tabs.splice(Math.min(typeof destination === "number" ? destination : 0, target.tabs.length), 0, tab);
    target.active = id;
    activeId.value = target.id;
    closeGroup(home.group);
  };

  /** rename renames or moves a file or folder on disk and retargets its tabs. It throws if Go refuses. */
  const rename = async (source: string, target: string): Promise<void> => {
    await workspaceApi.Rename(source, target);
    const key = (await loadQuery(placeQuery(source)))?.key;
    if (!key) return;
    for (const old of listUnder(key)) {
      const place = await loadQuery(placeQuery(target + old.path.slice(source.length)));
      if (place) retarget(old.key, place);
    }
  };

  /** retarget moves the tabs, place and model of a renamed file to its new place. */
  function retarget(key: string, place: Place): void {
    for (const group of listGroups()) {
      for (const tab of group.tabs.filter((candidate) => candidate.key === key)) {
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

  // A rename or a fix can leave an unsaved file with no tab, so it gets one.
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
    listRecent,
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
    applyFixAsync,
  };
});

/** tabId returns the id of the tab that opens the given file, diff or start page. */
export function tabId({ key, side, start, root = "", hash = "" }: Omit<Tab, "id" | "preview">): string {
  if (start) return "start";
  return side ? `diff:${side}@${hash}:${root}:${key}` : `file:${key}`;
}

/** canSplit reports whether a group can be split from the given tab. The start page and no tab cannot. */
export function canSplit(tab: Tab | null | undefined): boolean {
  return !!tab && !tab.start;
}

/** toRange returns a position as the empty range at it, and a range as it is. */
function toRange(selection: IPosition | IRange): IRange {
  if ("startLineNumber" in selection) return selection;
  const { lineNumber, column } = selection;
  return { startLineNumber: lineNumber, startColumn: column, endLineNumber: lineNumber, endColumn: column };
}
