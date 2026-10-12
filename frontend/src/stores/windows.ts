/**
 * This window and the others: its name and part from the URL, what the other windows hold as each last reported, the
 * folders its explorer shows, and the tabs that pass between windows. The main window has the top bar; each other
 * window shows one thing: a workspace page (`page:<route name>`), an editor group (`editor:<n>`), the game's files
 * (`game`), a folder of the workspace (`files:<path>`) or the panel (`panel`). A file is open in one window at a time:
 * a tab handed on carries its unsaved text, and the giver never tells the backend it closed; a tab dragged in is asked
 * of the window holding it and placed where it was dropped when it comes; a window closing hands its tabs back to the
 * main window, and only the main window's close ends the app. The store reads the editor store for what this window
 * holds, inside its functions. The table says who sends and answers each window event.
 *
 * | event                | data             | sent by                               | answered by                   |
 * | -------------------- | ---------------- | ------------------------------------- | ----------------------------- |
 * | window:opened        | its name         | every window at start                 | broadcastState                |
 * | window:state         | WindowState      | a window with files, when ready, when | recordWindow                  |
 * |                      |                  | its state changes, and when asked     |                               |
 * | window:closed        | the name         | the backend, when a window closed     | here; the panel shows again   |
 * | window:closing       | the name         | the backend, on its first close       | closing                       |
 * | window:tabs-handed   | WindowTabs       | handOver, or requestTab for a tab no  | receive                       |
 * |                      |                  | window holds                          |                               |
 * | window:tab-requested | WindowTabRequest | requestTab, when another window holds | the holder's handOver         |
 * |                      |                  | the tab                               |                               |
 * | window:show          | WindowShow       | the editor store's open; the router's | showing                       |
 * |                      |                  | guard                                 |                               |
 * | window:results       | PanelResults     | a window with files, when its results | editor/search.ts, in the main |
 * |                      |                  | change                                | window                        |
 * | window:saved         | its name         | saved, after a mutation               | git and Publish read again    |
 */
import { computed, reactive, shallowRef, watch } from "vue";
import { defineStore } from "pinia";
import { useRouter } from "vue-router";
import { useQueryCache } from "@pinia/colada";
import { until, watchDebounced } from "@vueuse/core";
import { Events, Window } from "@wailsio/runtime";
import type { WindowState, WindowTabs } from "@services/models";
import { Close, Open, QuitAnswer, Show } from "@services/windowservice";
import { PAGES } from "../app/pages";
import { pathKey } from "../composables/useIdeRoots";
import { showText, unsavedText } from "../editor/models";
import { installSearch } from "../editor/search";
import { tabOf } from "../editor/tabs";
import { fileName } from "../lib/format";
import UnsavedChangesModal from "../ui/UnsavedChangesModal.vue";
import { useEditorStore, type Edge } from "./editor";
import { useWorkspaceStore } from "./workspace";

/** This window's name, and what it shows: the main window, a page, an editor group, the game's files or the panel. */
export const windowName = new URLSearchParams(location.search).get("window") ?? "main";
export const windowPart = windowName.split(":")[0] as "main" | "page" | "editor" | "game" | "files" | "panel";
export const isMain = windowPart === "main";
/** Whether this window holds files: the others ask the main window, or the window holding one, to show it. */
export const holdsFiles = isMain || windowPart === "editor" || windowPart === "game" || windowPart === "files";

export const useWindowsStore = defineStore("windows", () => {
  const router = useRouter();
  const queryCache = useQueryCache();
  const workspace = useWorkspaceStore();

  /** What the other windows hold, by name, as each last reported. */
  const otherWindows = reactive(new Map<string, WindowState>());
  /** The folders this window's explorer shows, whose files open here (the game's files, or a folder's). */
  const roots = shallowRef<string[]>([]);
  /** Where a tab dragged in from another window goes once it is here: its group, the edge, the tab it goes before. */
  const placing = new Map<string, { group: number; edge: Edge; nextTab?: string }>();

  // a window opened, waiting for it to say what it holds (it is ready for tabs then)
  const opening = new Map<string, () => void>();
  // the tabs handed to this window, received one hand at a time, and those that have arrived
  const arrived = reactive(new Set<string>());
  let receiving = Promise.resolve();

  /** A window's title: what it shows, then its workspace. */
  const title = computed(() => {
    const active = workspace.activeWorkspace;
    const label = (what: string) => (active ? `${what}, ${active.name}` : what);
    const route = router.currentRoute.value;
    if (windowPart === "game") return `Game files, ${workspace.game(active?.gameId ?? "")?.name ?? ""}`;
    if (windowPart === "files") return label(fileName(windowName));
    if (windowPart === "panel") return label("Panel");
    if (windowPart === "editor") return label(fileName(useEditorStore().activeFile) || "Editor");
    if (windowPart === "page") return label(route.name ? PAGES[route.name].title : "");
    return "Paradox Modding Tools";
  });

  /** What this window holds, as the others see it. */
  const state = computed<WindowState>(() => {
    const editor = useEditorStore();
    return {
      name: windowName,
      title: title.value,
      roots: roots.value,
      tabs: editor.openTabs,
      unsaved: [...editor.unsaved],
      problems: Object.fromEntries(editor.problems),
    };
  });
  /** A window holding files is ready once its editors are set up; the others at once. */
  const ready = computed(() => !holdsFiles || isMain || !!useEditorStore().editingWorkspaceId);

  /** Tell the other windows what this window holds. */
  function broadcastState(): void {
    if (ready.value) void Events.Emit("window:state", state.value);
  }

  /** A window reported what it holds. */
  function recordWindow(data: WindowState): void {
    otherWindows.set(data.name, data);
    opening.get(data.name)?.();
    opening.delete(data.name);
  }

  /** Open a window at a route, or show it; resolved once it is ready for tabs. */
  function openWindow(route: string, name: string, windowTitle: string): Promise<void> {
    if (otherWindows.has(name)) return Show(name);
    return new Promise((resolve) => {
      opening.set(name, resolve);
      void Open(route, name, windowTitle);
    });
  }

  /** Close the windows that match, which hand their tabs back; resolved once they are gone and their tabs here, in
   * whichever order Wails brings the two (it keeps no order between events). */
  async function dock(which: (one: WindowState) => boolean): Promise<void> {
    const closing = [...otherWindows.values()].filter(which);
    const tabs = closing.flatMap((one) => one.tabs ?? []);
    tabs.forEach((tab) => arrived.delete(tab));
    closing.forEach((one) => void Close(one.name));
    await until(() => closing.some((one) => otherWindows.has(one.name)) || tabs.some((tab) => !arrived.has(tab))).toBe(
      false,
      { timeout: 5000 },
    );
    await receiving;
  }

  /** Hand tabs to another window, each with its file's unsaved text; they go from here, the backend keeping them open.
   * Resolved once they are sent. */
  function handOver(target: string, tabs: string[], active = tabs[0] ?? ""): Promise<boolean> {
    const entries = tabs.map((tab) => {
      const text = unsavedText(tab);
      return { tab, unsaved: text !== undefined, text: text ?? "" };
    });
    useEditorStore().dropTabs(tabs);
    return Events.Emit("window:tabs-handed", { to: target, tabs: entries, active });
  }

  /** Tabs handed to this window, taken after those handed before: each hidden, the one shown last shown, and one
   * dragged here placed where it was dropped; a file's unsaved text is an edit to the text saved, so its tab is marked
   * and an undo goes back to the file. */
  function receive(payload: WindowTabs): void {
    const tabs = payload.tabs ?? [];
    tabs.forEach(({ tab }) => arrived.add(tab));
    receiving = receiving.then(async () => {
      const editor = useEditorStore();
      for (const { tab, unsaved, text } of tabs) {
        editor.addTabHidden(tab);
        if (unsaved) await showText(tabOf(tab).file, text);
      }
      const group = editor.group(editor.focused);
      if (group?.tabs.includes(payload.active)) group.active = payload.active;
      for (const { tab } of tabs) {
        const place = placing.get(tab);
        placing.delete(tab);
        if (place) editor.drop(tab, editor.focused, place.group, place.edge, place.nextTab);
      }
    });
  }

  /** A tab into another window: handed on from here, asked of the window holding it, or opened there. */
  function requestTab(target: string, tab: string): Promise<unknown> {
    if (useEditorStore().openTabs.includes(tab)) return handOver(target, [tab]);
    const holder = [...otherWindows.values()].find((one) => one.tabs?.includes(tab));
    if (holder) return Events.Emit("window:tab-requested", { to: holder.name, tab, window: target });
    return Events.Emit("window:tabs-handed", { to: target, tabs: [{ tab, unsaved: false, text: "" }], active: tab });
  }

  /** A tab in a new editor window of its own. */
  async function openInNewWindow(tab: string): Promise<void> {
    const name = `editor:${Date.now().toString(36)}`;
    await openWindow(`/workspace/${useEditorStore().editingWorkspaceId}`, name, fileName(tabOf(tab).file));
    await requestTab(name, tab);
  }

  /** A window whose explorer shows folders (the game's files, or one folder), given this window's tabs of files in
   * them. */
  async function openFolderWindow(name: string, windowTitle: string, folders: string[]): Promise<void> {
    const editor = useEditorStore();
    await openWindow(`/workspace/${editor.editingWorkspaceId}`, name, windowTitle);
    const keys = folders.map(pathKey);
    const tabs = editor.openTabs.filter((tab) => keys.some((key) => pathKey(tabOf(tab).file).startsWith(`${key}/`)));
    if (tabs.length) await handOver(name, tabs);
  }

  /** Tell the other windows, if any, that this one saved something they read, so they read it again. */
  function saved(): void {
    if (otherWindows.size) void Events.Emit("window:saved", windowName);
  }

  if (!isMain) watch(title, (text) => void Window.SetTitle(text), { immediate: true });

  // results shown to this window arrive on any page: the main window holds files on all of them
  installSearch();

  // what this window holds, sent once ready and again when it changes while another window is open; a window
  // opened asks what the others hold
  watchDebounced([state, ready, () => otherWindows.size], () => otherWindows.size && broadcastState(), {
    debounce: 100,
  });
  Events.On("window:opened", ({ data: name }) => name !== windowName && broadcastState());
  Events.On("window:state", ({ data }) => data.name !== windowName && recordWindow(data));
  void Events.Emit("window:opened", windowName);

  // tabs handed to this window, and a tab it holds asked for by another window, on any page
  Events.On("window:tabs-handed", ({ data }) => data.to === windowName && receive(data));
  Events.On("window:tab-requested", ({ data }) => data.to === windowName && void handOver(data.window, [data.tab]));

  // a window closed: its tabs came back before; the panel shows here again
  Events.On("window:closed", ({ data: name }) => {
    otherWindows.delete(name);
    if (name === "panel" && isMain) useEditorStore().panel ||= "problems";
  });

  // this window asked to close: a window with files hands them to the main window first; the main window's close
  // hands the unsaved files here, then they are saved, dropped, or PMT stays
  async function closing(): Promise<void> {
    if (!isMain) {
      const editor = useEditorStore();
      if (holdsFiles && editor.openTabs.length) await handOver("main", editor.openTabs, editor.activeTab);
      await Window.Close();
      return;
    }
    await dock((one) => !!one.unsaved?.length);
    const modal = useOverlay().create(UnsavedChangesModal);
    await QuitAnswer(!useEditorStore().unsaved.size || (await modal.open({ going: "Closing PMT" })));
  }
  Events.On("window:closing", ({ data: name }) => name === windowName && void closing());

  // this window asked to the front: at a link, or showing a file (in the IDE, in the main window)
  async function showing(data: { route: string; path: string; line: number; col: number }): Promise<void> {
    if (data.route) await router.push(data.route);
    if (data.path) useEditorStore().open(data.path, data.line < 0 ? undefined : { line: data.line, col: data.col });
    await Show(windowName);
  }
  Events.On("window:show", ({ data }) => data.to === windowName && void showing(data));

  // another window saved something config.json does not hold (config:changed tells of that): its repositories' state
  // and Publish's files are read again
  Events.On("window:saved", ({ data: name }) => {
    if (name === windowName) return;
    for (const key of [["git"], ["release"]]) void queryCache.invalidateQueries({ key });
  });

  // the panel's window shows every window's problems
  if (windowPart === "panel") {
    watch(otherWindows, () => {
      const all = [...otherWindows.values()].flatMap((one) =>
        Object.entries(one.problems ?? {}).map(([path, list]) => [path, list ?? []] as const),
      );
      useEditorStore().problems = new Map(all);
    });
  }

  // an editor group's window whose last tab went (closed, or moved to another window) closes
  if (windowPart === "editor") {
    watch(
      () => useEditorStore().openTabs.length,
      (count, was) => !count && was && void Window.Close(),
    );
  }

  // a window of one thing follows the main window to another workspace
  if (!isMain) {
    watch(
      () => workspace.activeWorkspaceId,
      (id) => {
        const route = router.currentRoute.value;
        if ("id" in route.params && route.params.id !== id)
          void router.replace({ path: route.fullPath.replace(`/workspace/${route.params.id}`, `/workspace/${id}`) });
      },
    );
  }

  return {
    otherWindows,
    roots,
    placing,
    openWindow,
    dock,
    handOver,
    requestTab,
    openInNewWindow,
    openFolderWindow,
    saved,
  };
});
