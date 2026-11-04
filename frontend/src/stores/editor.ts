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

/**
 * A Mode is what the IDE shows of the active file: its code, or Visual, the picture it draws with the tools around it.
 */
export type Mode = "code" | "visual";

/** A DiffSide names the two revisions a diff tab compares. */
export type DiffSide = "changes" | "staged" | "conflict" | "commit";

/**
 * A Tab is one tab in a group. It shows a file by its key, a diff of that file on one side, with the repository root
 * and commit hash for a commit's diff, or the start page, whose key is empty. The next file shown replaces a preview
 * tab.
 */
export type Tab = {
  id: string;
  key: string;
  side?: DiffSide;
  root?: string;
  hash?: string;
  start?: true;
  preview: boolean;
};
/** A Group is one group of tabs with its active one. A group is one column of the layout. */
export type Group = { id: string; tabs: Tab[]; active: string | null };

/** A Side is a side of a group where a new group goes: left or right in its row, up or down in a row of its own. */
export type Side = "left" | "right" | "up" | "down";

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

/** HISTORY is how many places the history keeps. */
const HISTORY = 50;
/** How many files the start page lists as recent. */
const RECENT = 5;
/** START is the start page's tab. A window holds at most one. */
const START = { key: "", start: true } as const;

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
  // shared by every window: whether the Wiki pane is shown, the bottom panel's tab, and the file selected for a compare
  const wiki = useLocalStorage("pmt.ide.wiki", false);
  const panel = useLocalStorage("pmt.ide.panel", "");
  const compareFile = useLocalStorage<Place | null>("pmt.ide.compare", null, { serializer: StorageSerializers.object });
  // the last files shown in the active workspace, the newest first
  const recent = useLocalStorage<Place[]>(() => `pmt.ide.recent.${workspace.activeId()}`, []);
  // this window's mode, which is never saved: every window opens in Code
  const mode = shallowRef<Mode>("code");
  const state = ref<Layout>(makeLayout());
  const activeId = shallowRef(state.value.rows[0]?.columns[0]?.id ?? "");
  // the place of each file a tab holds, by key: a tab holds only the key, which is not a path
  const places = shallowReactive(new Map<string, Place>());
  // whether layout changes are saved: not while a workspace's layout loads, and only in the main window
  const tracking = shallowRef(false);
  // the range a file was shown with, which the active group's editor of that file selects once
  const request = shallowRef<{ key: string; range: IRange } | null>(null);
  // the tab Follow the agent showed last, until the user picks a tab or a group
  const agentTab = shallowRef("");
  // the files the explorer's last selection holds, for the compare of two
  const selectedFiles = shallowRef<Place[]>([]);
  // the places the user jumped between, the oldest first, and the index of the one the user is at
  const visits = shallowRef<Visit[]>([]);
  const current = shallowRef(-1);

  // a file that cannot open is taken out of the recent files
  const { mutate: openPlace } = useMutation({
    mutation: ({ place, range }: { place: Place; range?: IRange }) => openFile(place, range),
    onError: (_error, { place }) => {
      recent.value = recent.value.filter((candidate) => candidate.key !== place.key);
    },
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
   * applyFixAsync applies a quick fix. It creates the file the fix needs, a localization file with its header and any
   * other as an empty script file, applies the fix's edits to each file's model as one undoable step, then shows the
   * place the fix reveals in the group to the right of the active one. It splits a new group if there is none. It
   * resolves once all of that is done.
   */
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

  // a fix that fails is reported once, by the mutation
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
  // A file changed on disk is read into its model. A deleted file, or every file in a deleted folder, has its tabs
  // closed unless it has unsaved edits.
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

  /** listUnder lists the places of the open files at the given key or under it, as in a folder. */
  function listUnder(key: string): Place[] {
    return [...places.values()].filter((place) => place.key === key || place.key.startsWith(`${key}/`));
  }

  /** makeLayout makes a layout of one empty group. */
  function makeLayout(): Layout {
    return { rows: [{ columns: [{ id: crypto.randomUUID(), tabs: [], active: null }] }] };
  }

  /**
   * loadLayout brings the given workspace's saved layout back, or starts an empty one. Tabs are reopened unless the
   * workspace's record says to reset them. Then the start tab is put in front of the first group, unless the record
   * says to leave it out. Only the main window keeps a layout; another window starts empty. The layout is written again
   * only once every reopened tab's file is found or dropped.
   */
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
    if (!record.hideStartPage) showStart();
    tracking.value = true;
  }

  /** showStart puts the start tab in front of the first group and makes it the active one, moving a kept tab there. */
  function showStart(): void {
    const group = listGroups()[0];
    const id = tabId(START);
    if (!group) return;
    if (find(id)) move(id, group.id, 0);
    else group.tabs.unshift({ ...START, id, preview: false });
    group.active = id;
    activeId.value = group.id;
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
   * find locates the tab with the given id and returns its group and index. It looks in the given group first, the
   * active group by default. It returns null if no group holds the tab.
   */
  function find(id: string, first = activeGroup()): { group: Group; index: number } | null {
    for (const group of [first, ...listGroups()]) {
      const index = group?.tabs.findIndex((tab) => tab.id === id) ?? -1;
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
   * add makes a tab the active tab of a group. If the group lacks the tab, it goes after the active tab. A new preview
   * tab replaces the group's preview tab, unless that one has unsaved edits: it then stops being a preview.
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

  /** closeGroup removes a group that has no tabs, unless it is the last. If it was active, the group after it is. */
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

  /** placeOf returns the place of the file with the given key, or null if no open tab shows it. */
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

  // Visual draws only a drawable file, so the mode goes back to Code whenever the active tab is not one.
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
   * open makes a tab of a file, a diff or the start page the active one, as the user's pick. It uses the group that
   * holds the tab, else the given group, else the active group, which gets the tab. It records the jump to a file in
   * the history. The start page has no place.
   */
  const open = (target: Omit<Tab, "id" | "preview">, place?: Place | null, groupId?: string): void => {
    const id = tabId(target);
    const preferred = listGroups().find((candidate) => candidate.id === groupId) ?? activeGroup();
    const group = find(id, preferred)?.group ?? preferred;
    if (!group) return;
    if (place) {
      places.set(place.key, place);
      record(locate(place));
    }
    add(group, { ...target, id, preview: false });
    activeId.value = group.id;
    release(id);
  };

  /**
   * show opens a file at a position or range. The window that holds the file shows it, else this window, else the main
   * window if this one has no editors. The main window switches to the IDE page first. The source's row in SOURCES
   * decides the preview tab, the history and Follow the agent. A drawable file opens in Visual if the setting is on,
   * unless it was already active or the agent showed it. A folder is not shown.
   */
  const show = (place: Place, selection?: IPosition | IRange, source: Source = "navigate"): void => {
    if (place.isDir) return;
    const windows = useWindowsStore();
    const target = windows.holderOf(place.key) || (HAS_EDITORS ? THIS_WINDOW : "main");
    if (target !== THIS_WINDOW) return windows.showElsewhere(target, place, selection, source);
    const group = activeGroup();
    if (!group) return;
    const row = SOURCES[source];
    recent.value = [place, ...recent.value.filter((one) => one.key !== place.key)].slice(0, RECENT);
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

  /**
   * showVisual shows a place as show does, then enters Visual if the file is drawable and is now this window's active
   * file. A file shown in another window leaves this window's mode alone.
   */
  const showVisual = (place: Place, selection?: IPosition | IRange): void => {
    show(place, selection);
    if (place.drawable && activeFile()?.key === place.key) mode.value = "visual";
  };

  /**
   * findRequest returns the range a file was shown with, if no editor has moved to it yet. The editor selects the
   * range, or only puts the caret at it if the range is empty. It is undefined if there is none.
   */
  const findRequest = (key: string): IRange | undefined =>
    request.value?.key === key ? request.value.range : undefined;

  /** clearRequest forgets the range asked of an editor, once the editor has moved its caret there. */
  const clearRequest = (): void => {
    request.value = null;
  };

  /** listSelection lists the files selected in the explorer, in the order the tree lists them. A folder is left out. */
  const listSelection = (): Place[] => selectedFiles.value;

  /** listRecent lists the last files shown in the active workspace, the newest first. */
  const listRecent = (): Place[] => recent.value;

  /** showPanel shows the bottom panel on the given tab. Visual hides it, so the window goes back to Code first. */
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
    selectedFiles.value = files;
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
   * go moves through the history by the given number of places: -1 is back and 1 is forward. It reopens a place whose
   * file has closed, and does nothing past either end.
   */
  const go = (steps: number): void => {
    const here = locate();
    // Save the current position, so going the other way returns to it. A place no jump led to is recorded as a jump.
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
    const group = listGroups().find((candidate) => candidate.id === groupId);
    if (!group) return;
    activeId.value = groupId;
    release(group.active);
  };

  /**
   * release ends Follow the agent's hold on its tab when the user picks a tab. If the picked tab is the one Follow
   * showed, it is pinned so it stays open.
   */
  function release(id: string | null): void {
    if (id && id === agentTab.value) pin(id);
    agentTab.value = "";
  }

  /** pin keeps a preview tab: it is no longer replaced by the next file shown. */
  const pin = (id: string): void => {
    listGroups().forEach((group) => group.tabs.filter((tab) => tab.id === id).forEach((tab) => (tab.preview = false)));
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

  /** listUnsaved lists the relative paths of the given tabs' files that have unsaved changes and no other tab. */
  function listUnsaved(tabs: Tab[]): string[] {
    const others = listTabs()
      .filter((tab) => !tabs.includes(tab))
      .map((tab) => tab.key);
    return [...new Set(tabs.map((tab) => tab.key))].flatMap((key) => {
      const model = findModel(key);
      return model?.unsaved && !others.includes(key) ? [model.place.rel] : [];
    });
  }

  /** close closes a tab. The model of its file closes too when no other tab shows the file. */
  const close = (id: string): void => {
    const home = find(id);
    if (!home) return;
    const tab = remove(home.group, home.index);
    closeGroup(home.group);
    if (tab) closeFile(tab.key);
  };

  /**
   * insertGroup adds a group with the given tabs and active tab on a side of a group: left or right, a column before or
   * after it in its row; up or down, a row before or after its row. It returns the new group as the layout holds it.
   */
  function insertGroup(group: Group, side: Side, tabs: Tab[], active: string | null): Group {
    const rows = state.value.rows;
    const row = rows.find((candidate) => candidate.columns.includes(group)) ?? rows[0];
    const newGroup: Group = { id: crypto.randomUUID(), tabs, active };
    const offset = side === "right" || side === "down" ? 1 : 0;
    if (side === "left" || side === "right") row.columns.splice(row.columns.indexOf(group) + offset, 0, newGroup);
    else rows.splice(rows.indexOf(row) + offset, 0, { columns: [newGroup] });
    // the reactive copy, so a tab added to it later is drawn at once and stored as plain data the layout can save
    return listGroups().find((one) => one.id === newGroup.id) ?? newGroup;
  }

  /**
   * split splits the active group: a copy of its active tab in a new group beside it ("row") or below it ("column").
   * It does nothing when the active tab cannot be split (canSplit).
   */
  const split = (direction: "row" | "column"): void => {
    const group = activeGroup();
    if (!group || !canSplit(activeTab())) return;
    const active = group.tabs.filter((tab) => tab.id === group.active).map((tab) => ({ ...tab, preview: false }));
    activeId.value = insertGroup(group, direction === "row" ? "right" : "down", active, group.active).id;
  };

  /**
   * move moves a tab, as a drag does, to the given index of a group, or into a new group on the given side of the
   * group. A tab no group holds stays where it is.
   */
  const move = (id: string, groupId: string, place: number | Side): void => {
    const home = find(id);
    const group = listGroups().find((candidate) => candidate.id === groupId);
    const tab = home && group && remove(home.group, home.index);
    if (!home || !group || !tab) return;
    const target = typeof place === "number" ? group : insertGroup(group, place, [], null);
    const existing = target.tabs.findIndex((other) => other.id === id);
    if (existing >= 0) target.tabs.splice(existing, 1);
    target.tabs.splice(Math.min(typeof place === "number" ? place : 0, target.tabs.length), 0, tab);
    target.active = id;
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

  // A file with unsaved edits that no tab shows, as a rename or a fix can leave, gets a tab after the active one.
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

/**
 * tabId returns the id of the tab that opens the given file, diff or start page. The DOM, a drag between windows and
 * the saved layout know a tab by its id.
 */
export function tabId({ key, side, start, root = "", hash = "" }: Omit<Tab, "id" | "preview">): string {
  if (start) return "start";
  return side ? `diff:${side}@${hash}:${root}:${key}` : `file:${key}`;
}

/**
 * canSplit reports whether a group can be split from the given tab: a file or a diff can, the start page cannot, since
 * a window holds one start page and a group left with no tab has nothing to close it by. It is false for no tab.
 */
export function canSplit(tab: Tab | null | undefined): boolean {
  return !!tab && !tab.start;
}

/** toRange returns what the editor selects as a range: a position as the empty range at it. */
function toRange(selection: IPosition | IRange): IRange {
  if ("startLineNumber" in selection) return selection;
  const { lineNumber, column } = selection;
  return { startLineNumber: lineNumber, startColumn: column, endLineNumber: lineNumber, endColumn: column };
}
