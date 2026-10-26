/**
 * The windows store: what passes between windows. Messages ride two broadcast channels, one every window hears
 * (`pmt.windows`) and one per window name. Go keeps only the OS close hook and the calls that act on another
 * window.
 *
 * The protocol, each message a row:
 * | message        | from → to        | carries                                   |
 * | state          | any → all        | its name, what it holds, its active file  |
 * | tab-requested  | any → the holder | a tab's id the asker wants                |
 * | tab-handed     | holder → asker   | the tab with its unsaved text             |
 * | show           | any → one        | a place to show                           |
 * | page           | any → the main   | a route to show                           |
 * | wiki           | any → the Wiki pane's | a page or lexicon entry to look up   |
 * | command        | any → one        | a command for its IDE page to carry out   |
 * | results        | the searcher → all | one file's search hits                  |
 * | closing        | the closer → all | its tabs handed back before it closes     |
 *
 * The results message belongs to the search store, which sends and reads it on the shared channel.
 */
import { defineStore } from "pinia";
import { useMutation, useQueryCache } from "@pinia/colada";
import { until, useBroadcastChannel, useEventBus, useEventListener, useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { computed, nextTick, shallowReactive, watch } from "vue";
import { useRouter } from "vue-router";

import * as app from "@services/app/service";
import type { Place } from "@services/workspace/models";
import { WORKSPACE_TOOLS } from "../app/pages";
import { findModel, openModel, saveUnsaved } from "../pages/ide/editor/models";
import { parseTab } from "../pages/ide/editor/tabs";
import { keys } from "../queries";
import { useEditorStore, type Tab } from "./editor";
import { useWorkspaceStore } from "./workspace";

/** A window's name from its URL: `main`, or `page:<route>`, `editor:<n>`, `game`, `files:<folder>`, `panel`, `wiki`. */
export type WindowName = string;

/** What a window holds, announced on the shared channel. */
export type WindowState = {
  name: WindowName;
  files: string[];
  folders: string[];
  page: string | null;
  active: Place | null; // the place its active tab shows
};

/** What the editor looks up in the wiki: a page by its title, or a lexicon entry by its class and name. */
export type WikiLookup = { title: string } | { class: string; name: string };

/** A tab on its way to another window: the tab, its file's place and the unsaved text if any. */
type HandedTab = { tab: Tab; place: Place; text: string | null };

type Message =
  | { type: "state"; state: WindowState }
  | { type: "tab-requested"; tab: string; from: WindowName }
  | { type: "tab-handed"; from: WindowName; tabs: HandedTab[] }
  | { type: "show"; place: Place }
  | { type: "page"; path: string }
  | { type: "wiki"; lookup: WikiLookup }
  | { type: "command"; command: string }
  | { type: "closing"; from: WindowName; tabs: HandedTab[] };

type Channel = ReturnType<typeof useBroadcastChannel<Message, Message>>;

/** This window's name, from its URL. */
export const THIS_WINDOW: WindowName = new URLSearchParams(location.search).get("window") ?? "main";

export const useWindowsStore = defineStore("windows", () => {
  const editor = useEditorStore();
  const workspace = useWorkspaceStore();
  const router = useRouter();
  const cache = useQueryCache();
  // the other windows as each last announced itself
  const others = shallowReactive(new Map<WindowName, WindowState>());
  const shared = useBroadcastChannel<Message, Message>({ name: "pmt.windows" });
  const own = useBroadcastChannel<Message, Message>({ name: `pmt.window.${THIS_WINDOW}` });
  const links = new Map<WindowName, Channel>();
  const commandBus = useEventBus<string>("pmt.command");
  const wikiBus = useEventBus<WikiLookup>("pmt.wiki");
  const wiki = useLocalStorage("pmt.ide.wiki", false);
  // ends when the last batch of handed tabs is open
  let received: Promise<void> = Promise.resolve();

  const { mutate: receive } = useMutation({
    mutation: (handed: { from: WindowName; tabs: HandedTab[] }) => {
      const work = received.then(() => receiveTabs(handed));
      received = work.catch(() => undefined);
      return work;
    },
  });
  /** showWindow shows another window and brings it to the front. */
  const { mutate: showWindow } = useMutation({ mutation: (name: WindowName) => app.ShowWindow(name) });
  // closing another window makes it hand its tabs back to this one first
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
      await app.CloseWindow(THIS_WINDOW);
    },
  });
  const { mutate: open } = useMutation({
    mutation: async (opened: { name: WindowName; url: string; tabs: HandedTab[] }) => {
      await app.OpenWindow(opened.name, opened.url);
      if (!opened.tabs.length) return;
      // a window is ready for tabs once it has said what it holds
      await until(() => others.has(opened.name)).toBe(true, { timeout: 10_000, throwOnTimeout: true });
      link(opened.name).post({ type: "tab-handed", from: THIS_WINDOW, tabs: opened.tabs });
      opened.tabs.forEach(({ tab }) => editor.close(tab.id));
    },
    onSuccess: () => cache.invalidateQueries({ key: keys.windows }),
  });

  /** What this window holds, as the others see it. The active file goes as a copy, since a message carries no proxy. */
  const state = computed<WindowState>(() => {
    const active = editor.activeFile();
    return {
      name: THIS_WINDOW,
      files: [...new Set(editor.listTabs().map((tab) => parseTab(tab.id).key))],
      folders: THIS_WINDOW.startsWith("files:") ? [THIS_WINDOW.slice("files:".length)] : [],
      page: THIS_WINDOW.startsWith("page:") ? THIS_WINDOW.slice("page:".length) : null,
      active: active ? { ...active } : null,
    };
  });

  watch(state, sendState, { immediate: true });
  useEventListener(shared.channel, "message", ({ data }: MessageEvent<Message>) => {
    if (data.type === "state") updateWindow(data.state);
    else if (data.type === "closing" && THIS_WINDOW === "main") receive(data);
  });
  useEventListener(own.channel, "message", ({ data }: MessageEvent<Message>) => {
    if (data.type === "tab-handed") receive(data);
    else if (data.type === "tab-requested") sendTab(data.tab, data.from);
    else if (data.type === "show") editor.show(data.place);
    else if (data.type === "page") void router.push(data.path);
    else if (data.type === "wiki") void showLookup(data.lookup);
    else if (data.type === "command") void runCommand(data.command);
  });
  // the first close of a window waits for its tabs to go back to the main window
  Events.On("window:closing", ({ data }) => {
    if (data !== THIS_WINDOW) return;
    if (THIS_WINDOW !== "main") {
      const mine = editor.listTabs();
      shared.post({ type: "closing", from: THIS_WINDOW, tabs: mine.flatMap(toHanded) });
      mine.forEach((tab) => editor.close(tab.id));
    }
    closeWindow();
  });
  Events.On("window:closed", ({ data }) => {
    others.delete(data);
    void cache.invalidateQueries({ key: keys.windows });
  });

  /** The channel that reaches the given window alone. */
  function link(name: WindowName): Channel {
    const channel = links.get(name) ?? useBroadcastChannel<Message, Message>({ name: `pmt.window.${name}` });
    links.set(name, channel);
    return channel;
  }

  /** Sends the other windows what this window holds. */
  function sendState(): void {
    shared.post({ type: "state", state: state.value });
  }

  /** Notes what another window holds. A window not seen before is answered with this window's own state. */
  function updateWindow(announced: WindowState): void {
    const isNew = !others.has(announced.name);
    others.set(announced.name, announced);
    if (!isNew) return;
    sendState();
    void cache.invalidateQueries({ key: keys.windows });
  }

  /** Makes a tab ready to send: a copy of it, its file's place and its unsaved text. None if the tab has no place. */
  function toHanded(tab: Tab): HandedTab[] {
    const place = editor.placeOf(tab.id);
    const model = findModel(parseTab(tab.id).key);
    return place
      ? [{ tab: { ...tab }, place: { ...place }, text: model?.unsaved ? model.model.getValue() : null }]
      : [];
  }

  /** Sends the asking window a tab of this one, and closes it here. */
  function sendTab(tabId: string, asker: WindowName): void {
    const tab = editor.listTabs().find((other) => other.id === tabId);
    if (!tab) return;
    link(asker).post({ type: "tab-handed", from: THIS_WINDOW, tabs: toHanded(tab) });
    editor.close(tab.id);
  }

  /** Opens the tabs another window handed over, with their unsaved text, as this window's. */
  async function receiveTabs(handed: { from: WindowName; tabs: HandedTab[] }): Promise<void> {
    const sender = others.get(handed.from);
    if (sender) {
      const keysHanded = handed.tabs.map(({ tab }) => parseTab(tab.id).key);
      others.set(handed.from, { ...sender, files: sender.files.filter((key) => !keysHanded.includes(key)) });
    }
    for (const { tab, place, text } of handed.tabs) {
      if (parseTab(tab.id).tab === "file") editor.show(place);
      else editor.open(tab.id, place);
      if (text !== null) (await openModel(place)).model.setValue(text);
      editor.setView(tab.id, tab.view);
    }
  }

  /** Shows the IDE page and opens its Wiki pane, then has the pane show a look-up. */
  async function showLookup(lookup: WikiLookup): Promise<void> {
    await showIde();
    wiki.value = true;
    await nextTick();
    wikiBus.emit(lookup);
  }

  /** Shows the IDE page, then has it carry out a command. */
  async function runCommand(command: string): Promise<void> {
    await showIde();
    commandBus.emit(command);
  }

  /** Resolves the next editor window's name: the lowest number no window has. */
  function resolveEditor(): WindowName {
    let number = 1;
    while (others.has(`editor:${number}`) || THIS_WINDOW === `editor:${number}`) number++;
    return `editor:${number}`;
  }

  /** The window that holds a file, a folder or a page, or "" when none does. */
  const holderOf = (what: { file?: string; folder?: string; page?: string }): WindowName => {
    const holds = (held: WindowState): boolean => {
      if (what.file !== undefined) return held.files.includes(what.file);
      if (what.folder !== undefined) return held.folders.includes(what.folder);
      return what.page !== undefined && held.page === what.page;
    };
    return [state.value, ...others.values()].find(holds)?.name ?? "";
  };

  /**
   * Opens a window on a page, an editor group, one tab, a folder, the panel, the Wiki pane or the game's files, or
   * brings it to the front if it is open. A group's or a tab's tabs go to the new window.
   */
  const popOut = (what: {
    page?: string;
    group?: string;
    tab?: string;
    folder?: string;
    panel?: true;
    wiki?: true;
    game?: true;
  }): void => {
    let target: WindowName = "game";
    if (what.page) target = `page:${what.page}`;
    else if (what.group || what.tab) target = resolveEditor();
    else if (what.folder) target = `files:${what.folder}`;
    else if (what.panel) target = "panel";
    else if (what.wiki) target = "wiki";
    if (others.has(target)) {
      showWindow(target);
      return;
    }
    const tool = WORKSPACE_TOOLS.find((candidate) => candidate === what.page);
    const route = router.resolve({ name: tool ?? "workspace-ide", params: { id: workspace.activeId() } });
    const group = editor
      .layout()
      .rows.flatMap((row) => row.columns)
      .find((candidate) => candidate.id === what.group);
    const url = `/?window=${encodeURIComponent(target)}${route.href}`;
    const tabs = what.tab ? editor.listTabs().filter((tab) => tab.id === what.tab) : (group?.tabs ?? []);
    open({ name: target, url, tabs: tabs.flatMap(toHanded) });
  };

  /** Closes this window; its tabs go back to the main window first. The main window stays. */
  const dockBack = (): void => {
    if (THIS_WINDOW !== "main") closeWindow();
  };

  /** Asks the window that holds a tab to hand it over. The tab arrives with its unsaved text and opens here. */
  const requestTab = (tabId: string): void => {
    const holder = holderOf({ file: parseTab(tabId).key });
    if (holder && holder !== THIS_WINDOW) link(holder).post({ type: "tab-requested", tab: tabId, from: THIS_WINDOW });
  };

  /** Shows a place in the given window and brings the window to the front. */
  const showElsewhere = (window: WindowName, place: Place): void => {
    link(window).post({ type: "show", place: { ...place } });
    showWindow(window);
  };

  /** Shows a route in the main window and brings the window to the front. */
  const openPage = (path: string): void => {
    link("main").post({ type: "page", path });
    showWindow("main");
  };

  /** The place the given window's active tab shows, as it last said, or null when it shows none or is not open. */
  const findActive = (name: WindowName): Place | null => others.get(name)?.active ?? null;

  /** Shows a look-up in the Wiki pane: in the pane's own window when it has one, else in the main window. */
  const lookUp = (lookup: WikiLookup): void => {
    const holder = others.has("wiki") ? "wiki" : "main";
    if (holder === THIS_WINDOW) {
      void showLookup(lookup);
      return;
    }
    link(holder).post({ type: "wiki", lookup });
    showWindow(holder);
  };

  /**
   * Has a window's IDE page carry out a command, this window's unless another is given. The window shows its IDE page
   * first, and another window comes to the front.
   */
  const sendCommand = (command: string, name: WindowName = THIS_WINDOW): void => {
    if (name === THIS_WINDOW) {
      void runCommand(command);
      return;
    }
    link(name).post({ type: "command", command });
    showWindow(name);
  };

  /** Shows the IDE page of the active workspace in this window, and waits for it to draw. */
  const showIde = async (): Promise<void> => {
    await router.push({ name: "workspace-ide", params: { id: workspace.activeId() } });
    await nextTick();
  };

  return {
    holderOf,
    popOut,
    dockBack,
    requestTab,
    showElsewhere,
    openPage,
    findActive,
    showWindow,
    closeOther,
    lookUp,
    sendCommand,
    showIde,
  };
});
