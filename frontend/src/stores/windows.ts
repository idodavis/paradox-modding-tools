/**
 * This window and the others: its name and part from the URL, what the other windows hold as each last reported, the
 * folders its explorer shows, a dragged tab's landing place and the arrival queue of tabs handed to it. The main
 * window has the top bar; each other window shows one thing: a workspace page (`page:<route name>`), an editor group
 * (`editor:<n>`), the game's files (`game`), a folder of the workspace (`files:<path>`) or the panel (`panel`). The
 * windows send each other what they hold, so a file is open in one window at a time; closing one hands its tabs
 * back to the main window, and only the main window's close ends the app. The store subscribes to the events about
 * windows and sends this window's state; it reads the editor store for what this window holds, inside its functions.
 */
import { computed, reactive, shallowRef, watch } from "vue";
import { defineStore } from "pinia";
import { useRouter } from "vue-router";
import { useQueryCache } from "@pinia/colada";
import { until, watchDebounced } from "@vueuse/core";
import { Events, Window } from "@wailsio/runtime";
import type { WindowState } from "@services/models";
import { Close, Open, QuitAnswer, Show } from "@services/windowservice";
import { PAGES } from "../app/pages";
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

  /** Receive tabs handed to this window, after those handed before. */
  function receiveTabs(tabs: string[], work: () => Promise<void>): void {
    tabs.forEach((tab) => arrived.add(tab));
    receiving = receiving.then(work);
  }

  /** Tell the other windows, if any, that this one saved something they read, so they read it again. */
  function saved(): void {
    if (otherWindows.size) void Events.Emit("window:saved", windowName);
  }

  if (!isMain) watch(title, (text) => void Window.SetTitle(text), { immediate: true });

  // what this window holds, sent once ready and again when it changes while another window is open; a window
  // opened asks what the others hold
  watchDebounced([state, ready, () => otherWindows.size], () => otherWindows.size && broadcastState(), {
    debounce: 100,
  });
  Events.On("window:opened", ({ data: name }) => name !== windowName && broadcastState());
  Events.On("window:state", ({ data }) => data.name !== windowName && recordWindow(data));
  void Events.Emit("window:opened", windowName);

  // a window closed: its tabs came back before; the panel shows here again
  Events.On("window:closed", ({ data: name }) => {
    otherWindows.delete(name);
    if (name === "panel" && isMain) useEditorStore().panel ||= "problems";
  });

  // this window asked to close: the main window's close hands the unsaved files here, then they are saved, dropped,
  // or PMT stays; a window of one page or the panel just closes
  async function closing(): Promise<void> {
    if (!isMain) {
      if (!holdsFiles) await Window.Close();
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
      useEditorStore().replaceProblems(new Map(all));
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
    title,
    state,
    ready,
    broadcastState,
    recordWindow,
    openWindow,
    dock,
    receiveTabs,
    saved,
  };
});
