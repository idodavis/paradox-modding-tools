/**
 * The windows store: what passes between windows. Messages ride two broadcast channels, one every window hears and one
 * per window name.
 */
import { defineStore } from "pinia";
import { useMutation } from "@pinia/colada";
import { until, useBroadcastChannel, useEventBus, useEventListener, type EventBusKey } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import type { IPosition, IRange } from "monaco-editor";
import { computed, nextTick, shallowReactive, watch } from "vue";
import { useRouter } from "vue-router";

import * as app from "@services/app/service";
import type { Place } from "@services/workspace/models";
import { isPage, PAGES, WORKSPACE_TOOLS, type WorkspacePage } from "../app/pages";
import { findModel, openModel, saveUnsaved } from "../pages/ide/editor/models";
import { useEditorStore, type Source, type Tab } from "./editor";
import { useWorkspaceStore } from "./workspace";

/** A WindowName is a window's name from its URL: `main`, `page:<route>`, `editor:<n>`, `terminal`, `wiki` and so on. */
type WindowName = string;

/** A WindowState is what a window holds, announced on the shared channel. */
export type WindowState = {
  name: WindowName;
  files: string[];
  active: Place | null;
};

/** A WikiLookup is a wiki page by its title, or a lexicon entry by class and name. */
export type WikiLookup = { title: string } | { class: string; name: string };

/** A HandedTab is a tab on its way to another window, with its file's place and any unsaved text. */
type HandedTab = { tab: Tab; place: Place; text: string | null };

type Message =
  | { type: "state"; state: WindowState }
  | { type: "tab-requested"; tab: string; from: WindowName }
  | { type: "tab-handed"; from: WindowName; tabs: HandedTab[] }
  | { type: "show"; place: Place; selection?: IPosition | IRange; source?: Source }
  | { type: "page"; path: string }
  | { type: "wiki"; lookup: WikiLookup }
  | { type: "command"; command: string }
  | { type: "closing"; from: WindowName; tabs: HandedTab[] };

type Channel = ReturnType<typeof useBroadcastChannel<Message, Message>>;

/** WINDOW_QUERY holds the query of this window's URL. */
export const WINDOW_QUERY = new URLSearchParams(location.search);

/** THIS_WINDOW is this window's name, from its URL. */
export const THIS_WINDOW: WindowName = WINDOW_QUERY.get("window") ?? "main";

/** HAS_EDITORS reports whether this window holds editors, as the main window and an editor window do. */
export const HAS_EDITORS = THIS_WINDOW === "main" || THIS_WINDOW.startsWith("editor:");

/** POP_OUT_PAGES holds the pages that open in a window of their own: every workspace page but the IDE. */
export const POP_OUT_PAGES: readonly WorkspacePage[] = WORKSPACE_TOOLS.filter((tool) => tool !== "workspace-ide");

/** IDE_WINDOWS holds each kind of window that shows one part of the IDE, by its kind, with its title. */
const IDE_WINDOWS: Record<string, string> = {
  editor: "Editor",
  files: "Folder",
  game: "Game files",
  external: "External mods",
  terminal: "Terminal",
  wiki: "Wiki",
  agent: "Agent",
};

/** COMMAND_BUS is the key of the bus a command travels on. */
export const COMMAND_BUS: EventBusKey<string> = Symbol("pmt.command");

/** pageWindow returns the name of the window of its own that shows the page with the given route name. */
export const pageWindow = (name: string): WindowName => `page:${name}`;

/** describeWindow returns what the given window shows, such as Health, Editor 2 or a folder's name. */
export function describeWindow(name: WindowName): string {
  const [kind = "", rest = ""] = name.split(/:(.*)/s);
  if (kind === "page") return isPage(rest) ? PAGES[rest].title : rest;
  if (kind === "editor") return `Editor ${rest}`;
  if (kind === "files") return rest.split(/[\\/]/).at(-1) ?? rest;
  if (kind === "main") return "Main window";
  return IDE_WINDOWS[kind] ?? name;
}

export const useWindowsStore = defineStore("windows", () => {
  const editor = useEditorStore();
  const workspace = useWorkspaceStore();
  const router = useRouter();
  const others = shallowReactive(new Map<WindowName, WindowState>());
  const shared = useBroadcastChannel<Message, Message>({ name: "pmt.windows" });
  const own = useBroadcastChannel<Message, Message>({ name: `pmt.window.${THIS_WINDOW}` });
  const links = new Map<WindowName, Channel>();
  const commandBus = useEventBus(COMMAND_BUS);
  const wikiBus = useEventBus<WikiLookup>("pmt.wiki");
  let received: Promise<void> = Promise.resolve();

  const { mutate: receive } = useMutation({
    mutation: (handed: { from: WindowName; tabs: HandedTab[] }) => {
      const work = received.then(() => receiveTabs(handed));
      received = work.catch(() => undefined);
      return work;
    },
  });
  const { mutate: showWindow } = useMutation({ mutation: (name: WindowName) => app.ShowWindow(name) });
  const { mutate: closeOther } = useMutation({ mutation: (name: WindowName) => app.CloseWindow(name) });
  const { mutate: closeWindow } = useMutation({
    mutation: async () => {
      if (THIS_WINDOW === "main") {
        const holders = [...others.values()].filter((held) => held.files.length).map((held) => held.name);
        await Promise.all(holders.map((name) => app.CloseWindow(name)));
        await until(() => holders.every((name) => !others.has(name))).toBe(true, { timeout: 5_000 });
        await received;
        if (!(await saveUnsaved("Closing PMT"))) return;
      }
      // Imported here because the terminal store needs the command table, which needs this store.
      if (THIS_WINDOW === "terminal") await (await import("./terminal")).useTerminalStore().handOver();
      await app.CloseWindow(THIS_WINDOW);
    },
  });
  const { mutate: open } = useMutation({
    mutation: async (opened: { name: WindowName; url: string; tabs: HandedTab[] }) => {
      await app.OpenWindow(opened.name, opened.url);
      if (!opened.tabs.length) return;
      // A window is ready for tabs once it has said what it holds.
      await until(() => others.has(opened.name)).toBe(true, { timeout: 10_000, throwOnTimeout: true });
      link(opened.name).post({ type: "tab-handed", from: THIS_WINDOW, tabs: opened.tabs });
      opened.tabs.forEach(({ tab }) => editor.close(tab.id));
    },
  });

  // The active file goes as a copy, since a message cannot carry a proxy.
  const state = computed<WindowState>(() => {
    const active = editor.activeFile();
    return {
      name: THIS_WINDOW,
      files: [...new Set(editor.listTabs().flatMap((tab) => (tab.start ? [] : tab.key)))],
      active: active ? { ...active } : null,
    };
  });

  watch(state, sendState, { immediate: true });
  useEventListener(shared.channel, "message", ({ data }: MessageEvent<Message>) => {
    if (data.type === "state") updateWindow(data.state);
    else if (data.type === "tab-requested") {
      const tab = editor.listTabs().find((other) => other.id === data.tab);
      if (!tab || tab.start) return;
      link(data.from).post({ type: "tab-handed", from: THIS_WINDOW, tabs: toHanded(tab) });
      editor.close(tab.id);
    } else if (data.type === "closing" && THIS_WINDOW === "main") receive(data);
  });
  useEventListener(own.channel, "message", ({ data }: MessageEvent<Message>) => {
    if (data.type === "tab-handed") receive(data);
    else if (data.type === "show") editor.show(data.place, data.selection, data.source);
    else if (data.type === "page") void router.push(data.path);
    else if (data.type === "wiki") void showLookup(data.lookup);
    else if (data.type === "command") void runCommand(data.command);
  });
  // The first close of a window waits for its tabs to go back to the main window.
  Events.On("window:closing", ({ data }) => {
    if (data !== THIS_WINDOW) return;
    if (THIS_WINDOW !== "main") {
      const mine = editor.listTabs();
      shared.post({ type: "closing", from: THIS_WINDOW, tabs: mine.flatMap(toHanded) });
      mine.forEach((tab) => editor.close(tab.id));
    }
    closeWindow();
  });
  Events.On("window:closed", ({ data }) => others.delete(data));

  /** link returns the channel that reaches the given window alone. */
  function link(name: WindowName): Channel {
    const channel = links.get(name) ?? useBroadcastChannel<Message, Message>({ name: `pmt.window.${name}` });
    links.set(name, channel);
    return channel;
  }

  /** sendState sends the other windows what this window holds. */
  function sendState(): void {
    shared.post({ type: "state", state: state.value });
  }

  /** updateWindow notes what another window holds, and answers a new window with this one's state. */
  function updateWindow(announced: WindowState): void {
    const isNew = !others.has(announced.name);
    others.set(announced.name, announced);
    if (isNew) sendState();
  }

  /** toHanded returns a tab ready to send as a list of one, or an empty list if its file has no place. */
  function toHanded(tab: Tab): HandedTab[] {
    const place = editor.placeOf(tab.key);
    const model = findModel(tab.key);
    return place
      ? [{ tab: { ...tab }, place: { ...place }, text: model?.unsaved ? model.model.getValue() : null }]
      : [];
  }

  /** receiveTabs opens the tabs another window handed over, with their unsaved text. */
  async function receiveTabs(handed: { from: WindowName; tabs: HandedTab[] }): Promise<void> {
    const sender = others.get(handed.from);
    if (sender) {
      const keysHanded = handed.tabs.map(({ tab }) => tab.key);
      others.set(handed.from, { ...sender, files: sender.files.filter((key) => !keysHanded.includes(key)) });
    }
    for (const { tab, place, text } of handed.tabs) {
      if (tab.side) editor.open(tab, place);
      else editor.show(place);
      if (text !== null) (await openModel(place)).model.setValue(text);
    }
    // Showing a drawable file could open it in Visual, so the editor goes back to Code.
    if (handed.tabs.length) editor.mode = "code";
  }

  /** showLookup shows a look-up in the IDE page's Wiki pane. */
  async function showLookup(lookup: WikiLookup): Promise<void> {
    await showIde();
    editor.showWiki();
    await nextTick();
    wikiBus.emit(lookup);
  }

  /** runCommand has the IDE page carry out a command. */
  async function runCommand(command: string): Promise<void> {
    await showIde();
    commandBus.emit(command);
  }

  /** resolveEditor returns the next editor window's name: the lowest number no window has. */
  function resolveEditor(): WindowName {
    let number = 1;
    while (others.has(`editor:${number}`) || THIS_WINDOW === `editor:${number}`) number++;
    return `editor:${number}`;
  }

  /** listWindows lists the open windows by name, this one first. */
  const listWindows = (): WindowName[] => [THIS_WINDOW, ...others.keys()];

  /** holderOf returns the window that holds the file with the given key, or "" when none does. */
  const holderOf = (key: string): WindowName =>
    [state.value, ...others.values()].find((held) => held.files.includes(key))?.name ?? "";

  /**
   * popOut opens the window of the given name with the given tabs, or brings it to the front if it is open. The name is
   * a page's window or one of the IDE's parts; any other name does nothing.
   */
  const popOut = (name: WindowName, tabIds: string[] = [], query: Record<string, string> = {}): void => {
    const target = name === "editor" ? resolveEditor() : name;
    const tool = POP_OUT_PAGES.find((candidate) => pageWindow(candidate) === target);
    if (!tool && !(target.split(":")[0] in IDE_WINDOWS)) return;
    if (others.has(target)) {
      showWindow(target);
      return;
    }
    const route = router.resolve({ name: tool ?? "workspace-ide", params: { id: workspace.activeId() } });
    const url = `/?${new URLSearchParams({ window: target, ...query })}${route.href}`;
    const tabs = tabIds.flatMap((id) => editor.listTabs().find((tab) => tab.id === id && !tab.start) ?? []);
    open({ name: target, url, tabs: tabs.flatMap(toHanded) });
  };

  /** dockBack closes this window after its tabs go back to the main window. The main window stays. */
  const dockBack = (): void => {
    if (THIS_WINDOW !== "main") closeWindow();
  };

  /** requestTab asks the other windows for a tab by its id. The window that holds it hands it over. */
  const requestTab = (tabId: string): void => shared.post({ type: "tab-requested", tab: tabId, from: THIS_WINDOW });

  /** showElsewhere shows a place in the given window and brings that window to the front. */
  const showElsewhere = (window: WindowName, place: Place, selection?: IPosition | IRange, source?: Source): void => {
    link(window).post({ type: "show", place: { ...place }, selection: selection && { ...selection }, source });
    showWindow(window);
  };

  /** openPage shows a route in the given window, the main one unless another is given, and brings it to the front. */
  const openPage = (path: string, window: WindowName = "main"): void => {
    link(window).post({ type: "page", path });
    showWindow(window);
  };

  /** holderOfPage returns the other window that shows the page with the given route name, or "" when none does. */
  const holderOfPage = (name: string): WindowName => (others.has(pageWindow(name)) ? pageWindow(name) : "");

  /** findActive returns the place the given window's active tab shows, or null. */
  const findActive = (name: WindowName): Place | null => others.get(name)?.active ?? null;

  /** resolveTerminal returns the window that shows the terminal: its own while open, else the main one. */
  const resolveTerminal = (): WindowName =>
    THIS_WINDOW === "terminal" || others.has("terminal") ? "terminal" : "main";

  /** lookUp shows a look-up in the Wiki pane, in its own window if it has one. */
  const lookUp = (lookup: WikiLookup): void => {
    const holder = others.has("wiki") ? "wiki" : "main";
    if (holder === THIS_WINDOW) {
      void showLookup(lookup);
      return;
    }
    link(holder).post({ type: "wiki", lookup });
    showWindow(holder);
  };

  /** sendCommand has a window's IDE page carry out a command, in this window unless another is given. */
  const sendCommand = (command: string, name: WindowName = THIS_WINDOW): void => {
    if (name === THIS_WINDOW) {
      void runCommand(command);
      return;
    }
    link(name).post({ type: "command", command });
    showWindow(name);
  };

  /** showIde shows the IDE page of the active workspace in this window and waits for it to draw. */
  const showIde = async (): Promise<void> => {
    await router.push({ name: "workspace-ide", params: { id: workspace.activeId() } });
    await nextTick();
  };

  return {
    listWindows,
    holderOf,
    popOut,
    dockBack,
    requestTab,
    showElsewhere,
    openPage,
    holderOfPage,
    findActive,
    resolveTerminal,
    showWindow,
    closeOther,
    lookUp,
    sendCommand,
    showIde,
  };
});
