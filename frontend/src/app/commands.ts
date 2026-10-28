/**
 * The command table. Every command of the app is one row here, in the menu item shape, with the area it belongs
 * to and when it applies. The palette, the context menus, the dropdown menus, the toolbar buttons and the
 * shortcuts all read these rows; a command exists nowhere else. Undo and redo are two rows over the model's own
 * history, and they stay out of the way of a focused editor.
 *
 * A command that the IDE page or the palette carries out, because it needs their state, is sent on the event bus
 * `pmt.command` with the row's id once the page shows. The palette keeps the commands it last ran, for each viewer.
 */
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { useMutation, useQueryCache } from "@pinia/colada";
import { useEventBus, useLocalStorage } from "@vueuse/core";
import { Clipboard, Dialogs } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";
import { computed } from "vue";

import * as appApi from "@services/app/service";
import * as git from "@services/git/service";
import * as lsp from "@services/lsp/service";
import * as workspaceApi from "@services/workspace/service";
import type { Place } from "@services/workspace/models";
import { toSentence } from "../lib/format";
import { findModel, save, saveUnsaved, unsavedModels, type Model } from "../pages/ide/editor/models";
import { diffTab, listViews, parseTab, VIEW_ROWS } from "../pages/ide/editor/tabs";
import {
  codesQuery,
  copiesQuery,
  drawablesQuery,
  loadQuery,
  placeQuery,
  settingsCatalogQuery,
  workspacesQuery,
} from "../queries";
import { useAgentStore } from "../stores/agent";
import { useEditorStore, type Tab, type View } from "../stores/editor";
import { useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import ConfirmModal from "../ui/ConfirmModal.vue";
import { homeRoute, isWorkspacePage, listShownTools, PAGES, WORKSPACE_TOOLS, type Page, type PageName } from "./pages";
import { router } from "./router";

/** One command: a menu item whose id names its area, such as `editor.save`, and whether it applies now. */
export type Command = Omit<CommandPaletteItem, "kbds"> & {
  /** `<area>.<verb>`: editor.save, file.rename, graph.open, window.popOut … */
  id: string;
  /** Whether the command applies now. One that does not is left out of the palette and its shortcut is off. */
  when: () => boolean;
  /** The keys of the command's shortcut, such as meta and K. */
  kbds?: string[];
  /** The keys of a second shortcut that runs the command too. */
  secondKbds?: string[];
};

// the commands a focused editor or input keeps for itself, so their shortcuts stay off while one is typed in
const OWN_KEYS = new Set(["editor.undo", "editor.redo", "editor.find"]);
// the setting that holds the shortcuts the user changed: each changed command's shortcuts, as Nuxt UI writes them
const BINDINGS = "shortcuts.bindings";
// the settings that hold a list, each item of which the palette switches on a row of its own
const LISTS = ["appearance.pages", "ide.checks"];
// how many commands the palette's Recently used lists
const RECENT = 5;

const bus = useEventBus<string>("pmt.command");
const compareBus = useEventBus<{ left: Place; right: Place }>("pmt.compare");
// a list a command hands the palette to pick one from, under the command's id
const choicesBus = useEventBus<CommandPaletteGroup>("pmt.choices");
const confirm = useOverlay().create(ConfirmModal);
// the ids of the commands the palette ran last, the latest first
const recent = useLocalStorage<string[]>("pmt.palette.recent", []);

// a write run as the one mutation, so a failure reaches the app's error handler, and the toast of the app; both are
// made when the shortcuts are registered, inside the app
let run: (work: () => Promise<unknown>) => void = () => undefined;
let toast: ReturnType<typeof useToast> | undefined;

const hasModel = () => !!activeModel();
const hasTab = () => !!useEditorStore().activeTab();
const hasFile = () => !!useEditorStore().activeFile();
const isOpen = () => useWorkspaceStore().isOpen();
const isIde = () => router.currentRoute.value.name === "workspace-ide";

const VIEW_COMMANDS: Command[] = VIEW_ROWS.map(({ view, label, icon }) => ({
  label: `Open as ${label}`,
  prefix: "IDE:",
  icon,
  id: `view.${view}`,
  when: canShow(view),
  onSelect: onTab((tab) => useEditorStore().setView(tab.id, view)),
}));

/** Every command of the app. */
export const COMMANDS: readonly Command[] = [
  {
    id: "palette.open",
    label: "Search and commands",
    icon: "i-lucide-search",
    kbds: ["meta", "K"],
    secondKbds: ["meta", "shift", "P"],
    when: () => true,
    onSelect: () => bus.emit("palette.open"),
  },
  {
    id: "editor.save",
    label: "Save",
    prefix: "IDE:",
    icon: "i-lucide-save",
    kbds: ["meta", "S"],
    when: hasModel,
    onSelect: onActive((model) => run(() => save(model))),
  },
  {
    id: "editor.saveAll",
    label: "Save all",
    prefix: "IDE:",
    icon: "i-lucide-save-all",
    kbds: ["meta", "alt", "S"],
    when: () => unsavedModels().length > 0,
    onSelect: () => run(() => Promise.all(unsavedModels().map(save))),
  },
  {
    id: "editor.close",
    label: "Close the tab",
    icon: "i-lucide-x",
    kbds: ["meta", "W"],
    when: hasTab,
    onSelect: onTab((tab) => useEditorStore().close(tab.id)),
  },
  {
    id: "editor.splitRight",
    label: "Split right",
    prefix: "IDE:",
    icon: "i-lucide-columns-2",
    kbds: ["meta", "\\"],
    when: hasTab,
    onSelect: () => useEditorStore().split("row"),
  },
  {
    id: "editor.splitDown",
    label: "Split down",
    prefix: "IDE:",
    icon: "i-lucide-rows-2",
    when: hasTab,
    onSelect: () => useEditorStore().split("column"),
  },
  {
    id: "editor.moveToNextGroup",
    label: "Move the editor into the next group",
    prefix: "IDE:",
    icon: "i-lucide-arrow-right-to-line",
    when: hasTab,
    onSelect: onTab(moveToNextGroup),
  },
  {
    id: "editor.format",
    label: "Format document",
    prefix: "IDE:",
    icon: "i-lucide-align-left",
    kbds: ["shift", "alt", "F"],
    when: hasModel,
    onSelect: () => runAction("editor.action.formatDocument"),
  },
  {
    id: "editor.find",
    label: "Find in the file",
    icon: "i-lucide-search",
    kbds: ["meta", "F"],
    when: hasModel,
    onSelect: () => runAction("actions.find"),
  },
  {
    id: "editor.findInFiles",
    label: "Find in files",
    prefix: "IDE:",
    icon: "i-lucide-text-search",
    kbds: ["meta", "shift", "F"],
    when: isOpen,
    onSelect: () => useWindowsStore().sendCommand("editor.findInFiles"),
  },
  {
    id: "editor.goToFile",
    label: "Go to file",
    icon: "i-lucide-file-search",
    kbds: ["meta", "P"],
    when: isOpen,
    onSelect: () => bus.emit("editor.goToFile"),
  },
  {
    id: "editor.back",
    label: "Go back",
    prefix: "IDE:",
    icon: "i-lucide-arrow-left",
    kbds: ["alt", "arrowleft"],
    when: isIde,
    onSelect: () => useEditorStore().go(-1),
  },
  {
    id: "editor.forward",
    label: "Go forward",
    prefix: "IDE:",
    icon: "i-lucide-arrow-right",
    kbds: ["alt", "arrowright"],
    when: isIde,
    onSelect: () => useEditorStore().go(1),
  },
  {
    id: "editor.openGraph",
    label: "Open the event under the cursor",
    prefix: "Event Graph:",
    icon: "i-lucide-git-branch",
    kbds: ["meta", "shift", "G"],
    when: hasModel,
    onSelect: onActive((model) => run(() => openGraph(model))),
  },
  {
    id: "editor.toggleWiki",
    label: "Show or hide the Wiki pane",
    prefix: "IDE:",
    icon: "i-lucide-book-open",
    kbds: ["meta", "shift", "W"],
    when: isOpen,
    onSelect: () => useWindowsStore().sendCommand("editor.toggleWiki"),
  },
  {
    id: "editor.undo",
    label: "Undo",
    icon: "i-lucide-undo-2",
    kbds: ["meta", "Z"],
    when: hasModel,
    onSelect: onActive((model) => void model.model.undo()),
  },
  {
    id: "editor.redo",
    label: "Redo",
    icon: "i-lucide-redo-2",
    kbds: ["meta", "Y"],
    when: hasModel,
    onSelect: onActive((model) => void model.model.redo()),
  },
  {
    id: "file.new",
    label: "New file",
    icon: "i-lucide-file-plus",
    when: canChange,
    onSelect: onFile((place) => run(() => createPath(place, false))),
  },
  {
    id: "file.newFolder",
    label: "New folder",
    icon: "i-lucide-folder-plus",
    when: canChange,
    onSelect: onFile((place) => run(() => createPath(place, true))),
  },
  {
    id: "file.rename",
    label: "Rename",
    icon: "i-lucide-pencil",
    when: canChange,
    onSelect: onFile((place) => run(() => renameFile(place))),
  },
  {
    id: "file.delete",
    label: "Delete",
    icon: "i-lucide-trash-2",
    color: "error",
    when: canChange,
    onSelect: onFile((place) => run(() => trashFile(place.path, place.rel))),
  },
  {
    id: "file.copyPath",
    label: "Copy the path",
    icon: "i-lucide-copy",
    when: hasFile,
    onSelect: onFile((place) => run(() => copyPath(place))),
  },
  {
    id: "file.reveal",
    label: "Show in the file manager",
    icon: "i-lucide-folder-open",
    when: hasFile,
    onSelect: onFile((place) => run(() => appApi.ShowFolder(place.path))),
  },
  {
    id: "explorer.reveal",
    label: "Reveal in explorer",
    prefix: "IDE:",
    icon: "i-lucide-list-tree",
    // only the main window has both the editors and the explorer
    when: () => THIS_WINDOW === "main" && hasFile(),
    onSelect: () => useWindowsStore().sendCommand("explorer.reveal"),
  },
  {
    id: "compare.lastCommit",
    label: "Active file with the last commit",
    prefix: "Compare:",
    icon: "i-lucide-git-compare",
    kbds: ["meta", "shift", "D"],
    when: canUseGit,
    onSelect: onFile(
      (place) =>
        void useWindowsStore()
          .showIde()
          .then(() => useEditorStore().open(diffTab("changes", useGitStore().findRoot(place), place.key), place)),
    ),
  },
  {
    id: "compare.file",
    label: "Active file with another file",
    prefix: "Compare:",
    icon: "i-lucide-diff",
    when: hasFile,
    onSelect: onFile(compareWithFile),
  },
  {
    id: "compare.game",
    label: "Active file with the game's copy",
    prefix: "Compare:",
    icon: "i-lucide-diff",
    when: canChange,
    onSelect: onFile(compareWithGame),
  },
  {
    id: "compare.selected",
    label: "Two files selected in the explorer",
    prefix: "Compare:",
    icon: "i-lucide-diff",
    when: () => useEditorStore().listSelection().length === 2,
    onSelect: compareSelection,
  },
  {
    id: "publish.compare",
    label: "Compare this mod with Steam",
    prefix: "Publish:",
    icon: "i-lucide-cloud",
    // the mod is the active file's
    when: canChange,
    onSelect: onFile((place) => {
      const params = { id: useWorkspaceStore().activeId() };
      void router.push({ name: "release", params, query: { mod: place.origin, compare: "steam" } });
    }),
  },
  ...VIEW_COMMANDS,
  {
    id: "window.popOut",
    label: "Open the group in a new window",
    icon: "i-lucide-app-window",
    when: () => !!useEditorStore().activeGroup()?.tabs.length,
    onSelect: () => useWindowsStore().popOut({ group: useEditorStore().activeGroup()?.id }),
  },
  {
    id: "window.dockBack",
    label: "Back into the main window",
    icon: "i-lucide-panel-top-close",
    when: () => THIS_WINDOW !== "main",
    onSelect: () => useWindowsStore().dockBack(),
  },
  {
    id: "window.reload",
    label: "Reload the window",
    icon: "i-lucide-refresh-cw",
    when: () => true,
    onSelect: () =>
      run(async () => {
        if (await saveUnsaved("Reloading the window")) location.reload();
      }),
  },
  // carried out by the window that shows the panel, which holds the shells
  {
    id: "terminal.sendFile",
    label: "Send the file to the shell",
    prefix: "IDE:",
    icon: "i-lucide-at-sign",
    when: hasFile,
    onSelect: () => useWindowsStore().sendCommand("terminal.sendFile", useWindowsStore().resolveTerminal()),
  },
  {
    id: "git.stage",
    label: "Stage the file",
    icon: "i-lucide-plus",
    when: canUseGit,
    onSelect: onFile((place) => run(() => git.Stage(useGitStore().findRoot(place), [place.path]))),
  },
  {
    id: "git.unstage",
    label: "Unstage the file",
    icon: "i-lucide-minus",
    when: canUseGit,
    onSelect: onFile((place) => run(() => git.Unstage(useGitStore().findRoot(place), [place.path]))),
  },
  {
    id: "git.commit",
    label: "Commit",
    icon: "i-lucide-git-commit-horizontal",
    when: () => useGitStore().repositories.length > 0,
    onSelect: () => chooseRepository("git.commit", "Choose the repository to commit in", showCommit),
  },
  {
    id: "git.discard",
    label: "Discard the file's changes",
    icon: "i-lucide-undo",
    color: "error",
    when: canUseGit,
    onSelect: onFile((place) => run(() => discardFile(place))),
  },
  {
    id: "draw.newCoat",
    label: "New coat of arms",
    prefix: "Coat of arms:",
    icon: "i-lucide-shield-plus",
    when: isOpen,
    onSelect: () => useWindowsStore().sendCommand("draw.newCoat"),
  },
  {
    id: "workspace.open",
    label: "Open a workspace",
    icon: "i-lucide-layout-grid",
    when: () => true,
    onSelect: () => openPage("library"),
  },
  {
    id: "settings.open",
    label: "Settings",
    icon: "i-lucide-settings",
    kbds: ["meta", ","],
    when: () => true,
    onSelect: () => bus.emit("settings.open"),
  },
  // the pane sits in the main window, beside a workspace's pages but the Agents page
  {
    id: "agent.togglePane",
    label: "Show or hide the agent pane",
    icon: "i-lucide-sparkles",
    kbds: ["meta", "alt", "I"],
    when: () => THIS_WINDOW === "main" && useWorkspaceStore().isOpen() && router.currentRoute.value.name !== "agents",
    onSelect: () => {
      const agent = useAgentStore();
      agent.isPaneOpen = !agent.isPaneOpen;
    },
  },
];

/**
 * registerShortcuts registers every command's keyboard shortcuts as the user set them. A command's shortcuts are on
 * while its `when` holds.
 */
export function registerShortcuts(): void {
  const { mutate } = useMutation({ mutation: (work: () => Promise<unknown>) => work() });
  run = mutate;
  toast = useToast();
  defineShortcuts(
    computed(() =>
      Object.fromEntries(
        COMMANDS.filter((row) => row.when()).flatMap((row) =>
          Object.entries(extractShortcuts(listBindings(row).map((kbds) => ({ ...row, kbds })))).map(
            ([keys, handler]) => [keys, { handler, usingInput: !OWN_KEYS.has(row.id) }],
          ),
        ),
      ),
    ),
  );
}

/** listDefaultBindings returns a command's own shortcuts: its kbds, then its second. It is empty when it has none. */
export function listDefaultBindings(command: Command): string[][] {
  return [command.kbds, command.secondKbds].filter((kbds) => kbds !== undefined);
}

/** listBindings returns a command's shortcuts as the user set them: each changed one as changed, the others its own. */
export function listBindings(command: Command): string[][] {
  const changed = parseBindings()[command.id];
  return listDefaultBindings(command).map((own, slot) => (changed?.[slot] ? splitShortcut(changed[slot]) : own));
}

/**
 * writeBinding sets one of a command's shortcuts, the given place among them, to the given keys. With no keys it puts
 * the command's own back. A command whose shortcuts are all its own again leaves the setting.
 */
export function writeBinding(command: Command, slot: number, kbds?: string[]): void {
  const own = listDefaultBindings(command);
  const next = listBindings(command).map((keys, index) => (index === slot ? (kbds ?? own[index]) : keys));
  const changed = parseBindings();
  if (next.every((keys, index) => joinShortcut(keys) === joinShortcut(own[index] ?? []))) delete changed[command.id];
  else changed[command.id] = next.map(joinShortcut);
  useSettingsStore().write(BINDINGS, JSON.stringify(changed));
}

/** findBinding returns the command one of whose shortcuts is the given keys, or undefined when none is. */
export function findBinding(kbds: string[]): Command | undefined {
  const shortcut = joinShortcut(kbds);
  return COMMANDS.find((command) => listBindings(command).some((keys) => joinShortcut(keys) === shortcut));
}

/** listPressedKeys returns the keys of a key press as a command row writes them, Ctrl as meta and a letter in capitals. */
export function listPressedKeys(event: KeyboardEvent): string[] {
  const key = event.key.length === 1 ? event.key.toUpperCase() : event.key.toLowerCase();
  return [event.ctrlKey && "meta", event.shiftKey && "shift", event.altKey && "alt", key].filter(
    (part) => typeof part === "string",
  );
}

/** findKbds returns the keys of a command's first shortcut as the user set it. It is empty for an unknown command. */
export function findKbds(id: string): string[] {
  const command = COMMANDS.find((row) => row.id === id);
  return (command && listBindings(command)[0]) ?? [];
}

/** parseBindings returns the shortcuts the user changed, by command. A setting that is not JSON counts as none. */
function parseBindings(): Record<string, string[]> {
  try {
    return JSON.parse(useSettingsStore().value(BINDINGS) || "{}") as Record<string, string[]>;
  } catch {
    return {};
  }
}

/** joinShortcut returns keys as Nuxt UI writes a shortcut, such as meta_shift_p. */
export function joinShortcut(kbds: string[]): string {
  return kbds.join("_").toLowerCase();
}

/** splitShortcut returns a shortcut's keys, a letter or a function key in capitals as the command rows write them. */
function splitShortcut(shortcut: string): string[] {
  return shortcut.split("_").map((key) => (key.length === 1 || /^f\d+$/.test(key) ? key.toUpperCase() : key));
}

/**
 * paletteGroups returns the palette's groups: the commands it ran last that apply now, the other commands that
 * apply, the pages, the workspaces, and the settings by type.
 */
export function paletteGroups(): { id: string; label: string; items: Command[] }[] {
  const id = useWorkspaceStore().activeId();
  // each row shows its first shortcut as the user set it
  const applicable = COMMANDS.filter((row) => row.when()).map((row) => ({ ...row, kbds: listBindings(row)[0] }));
  const recentRows = recent.value.flatMap((one) => applicable.find((row) => row.id === one) ?? []);
  const names: PageName[] = ["library", "wizard", ...(id ? listShownTools() : [])];
  const pages = names.map((name) => {
    const page: Page = PAGES[name];
    return makeRow(`page.${name}`, {
      label: page.title,
      icon: page.icon ?? "i-lucide-plus",
      onSelect: () => openPage(name),
    });
  });
  const workspaces = (useQueryCache().getQueryData(workspacesQuery.key) ?? []).map((record) =>
    makeRow(`workspace.${record.id}`, {
      label: record.name,
      icon: record.id === id ? "i-lucide-check" : "i-lucide-folder",
      onSelect: () => void router.push(homeRoute(record.id, record.homePage)),
    }),
  );
  return [
    { id: "recent", label: "Recently used", items: recentRows },
    { id: "commands", label: "Commands", items: applicable.filter((row) => !recentRows.includes(row)) },
    { id: "pages", label: "Pages", items: pages },
    { id: "workspaces", label: "Workspaces", items: workspaces },
    { id: "switches", label: "Settings you can switch", items: id ? listSwitches() : [] },
    { id: "values", label: "Settings with a value", items: id ? listValues() : [] },
  ];
}

/**
 * rememberCommand puts a command the palette ran first in its Recently used. A row that is no command, such as a
 * file, is skipped.
 */
export function rememberCommand(id: string): void {
  if (!COMMANDS.some((row) => row.id === id)) return;
  recent.value = [id, ...recent.value.filter((one) => one !== id)].slice(0, RECENT);
}

/** compare shows the IDE page, then opens the compare of two files in it, the first on the left. */
export function compare(left: Place, right: Place): void {
  void useWindowsStore()
    .showIde()
    .then(() => compareBus.emit({ left, right }));
}

/** compareWithGame opens the compare of a mod's file with the game's copy of it. A file the game has no copy of gets a message. */
export function compareWithGame(place: Place): void {
  run(async () => {
    const copy = findGameCopy(place, await loadQuery(copiesQuery(place.path)));
    if (copy) compare(copy, place);
    else toast?.add({ title: `The game has no ${place.rel}` });
  });
}

/**
 * compareWithFile asks for another file and opens the compare of the given file with it. The dialog starts in the
 * file's mod, and cancelling it does nothing.
 */
export function compareWithFile(place: Place): void {
  run(async () => {
    const path = await Dialogs.OpenFile({ Title: "Compare with", Directory: place.root, CanChooseFiles: true });
    const other = path ? await loadQuery(placeQuery(path)) : undefined;
    if (other) compare(other, place);
  });
}

/**
 * findGameCopy finds the game's copy of a mod's file among the file's copies, which come in load order. It is
 * undefined for a file of the game, which is no copy of itself, and when the game has no copy.
 */
export function findGameCopy(place: Place, copies: Place[] | null | undefined): Place | undefined {
  const [first] = copies ?? [];
  return !place.readOnly && first?.readOnly ? first : undefined;
}

/**
 * trashFile asks whether to move a file or folder to PMT's trash under the given label, moves it, and offers to
 * bring it back. A note, when given, is added to the question. It returns false when the user says no.
 */
export async function trashFile(path: string, label: string, note = ""): Promise<boolean> {
  const body = `${label} goes to PMT's trash.${note}`;
  if (!(await confirm.open({ title: "Delete?", body, confirm: "Delete", destructive: true }))) return false;
  const trashed = await workspaceApi.Delete(path);
  const undo = () => run(() => workspaceApi.Restore(trashed, path));
  toast?.add({ title: "Deleted", description: label, actions: [{ label: "Undo", onClick: undo }] });
  return true;
}

/** activeModel returns the model of the active tab's file, or null when the tab shows none. */
function activeModel(): Model | null {
  const tab = useEditorStore().activeTab();
  return tab ? findModel(parseTab(tab.id).key) : null;
}

/** onTab makes a handler that acts on the active tab, if there is one. */
function onTab(act: (tab: Tab) => void): () => void {
  return () => {
    const tab = useEditorStore().activeTab();
    if (tab) act(tab);
  };
}

/** onActive makes a handler that acts on the active model, if there is one. */
function onActive(act: (model: Model) => void): () => void {
  return () => {
    const model = activeModel();
    if (model) act(model);
  };
}

/** onFile makes a handler that acts on the place of the active tab's file, if there is one. */
function onFile(act: (place: Place) => void): () => void {
  return () => {
    const place = useEditorStore().activeFile();
    if (place) act(place);
  };
}

/**
 * canChange reports whether the active tab's file is one a mod holds, which can be created beside, renamed and
 * deleted.
 */
function canChange(): boolean {
  const place = useEditorStore().activeFile();
  return !!place && !place.readOnly;
}

/** canUseGit reports whether the active tab's file is a mod's, and that mod is in a repository. */
function canUseGit(): boolean {
  return canChange() && !!useGitStore().findRoot(useEditorStore().activeFile());
}

/**
 * chooseRepository runs a git command's action on the repository it acts on: the active file's, else the only one
 * listed. When the active file is in none and several are listed, the palette first asks which, under the given
 * command id and question.
 */
function chooseRepository(id: string, label: string, act: (root: string) => void): void {
  const repositories = useGitStore().repositories;
  const root =
    useGitStore().findRoot(useEditorStore().activeFile()) ||
    (repositories.length === 1 ? (repositories[0]?.root ?? "") : "");
  if (root) {
    act(root);
    return;
  }
  const items = repositories.map((one) => ({
    label: one.mods.map((mod) => useWorkspaceStore().originName(mod.id)).join(", "),
    suffix: one.root,
    icon: "i-lucide-folder-git-2",
    onSelect: () => act(one.root),
  }));
  choicesBus.emit({ id, label, items });
}

/** showCommit shows the IDE's Source Control view with the commit box of the given repository unfolded and focused. */
function showCommit(root: string): void {
  void useWindowsStore()
    .showIde()
    .then(() => {
      useGitStore().folded.delete(root);
      useGitStore().commitTarget = root;
      useWindowsStore().sendCommand("git.commit");
    });
}

/** canShow makes the `when` of a view row: whether the active file can be shown in that view. */
function canShow(view: View): () => boolean {
  return () => {
    const place = useEditorStore().activeFile();
    const drawables = place && useQueryCache().getQueryData(drawablesQuery(place.path).key);
    return !!place && listViews(place, !!drawables?.length).includes(view);
  };
}

/** moveToNextGroup moves a tab into the group after its own. The last group has none after it, so nothing moves. */
function moveToNextGroup(tab: Tab): void {
  const editor = useEditorStore();
  const groups = editor.layout().rows.flatMap((row) => row.columns);
  const next = groups[groups.findIndex((group) => group.id === editor.activeGroup()?.id) + 1];
  if (next) editor.move(tab.id, next.id, next.tabs.length);
}

/** runAction runs one of Monaco's actions in the editor with the focus, else in the one showing the active file. */
function runAction(id: string): void {
  const editors = monaco.editor.getEditors();
  const model = activeModel()?.model;
  const target =
    editors.find((editor) => editor.hasTextFocus()) ?? editors.find((editor) => editor.getModel() === model);
  void target?.getAction(id)?.run();
}

/** compareSelection opens the compare of the two files selected in the explorer. */
function compareSelection(): void {
  const [left, right] = useEditorStore().listSelection();
  if (left && right) compare(left, right);
}

/** openPage shows a page. A workspace's pages are shown for the active workspace. */
function openPage(name: PageName): void {
  void (isWorkspacePage(name)
    ? router.push({ name, params: { id: useWorkspaceStore().activeId() } })
    : router.push({ name }));
}

/** makeRow makes a row of the palette that is made at the time and always applies. */
function makeRow(id: string, row: Omit<CommandPaletteItem, "kbds">): Command {
  return { ...row, id, when: () => true };
}

/**
 * listSwitches lists the palette's switches: each setting that is one, each page a workspace can show and each of
 * the checks.
 */
function listSwitches(): Command[] {
  const settings = useSettingsStore();
  const cache = useQueryCache();
  const catalog = cache.getQueryData(settingsCatalogQuery.key) ?? [];
  const findSetting = (key: string) => catalog.find((setting) => setting.key === key);
  const switches = catalog
    .filter((setting) => setting.type === "switch")
    .map((setting) =>
      makeToggle(
        `setting.${setting.key}`,
        { label: setting.label, words: setting.words, scope: setting.workspace ? "IDE" : "App" },
        settings.isOn(setting.key),
        (on) => settings.write(setting.key, String(on)),
      ),
    );
  const shown = listShownTools();
  const pages = WORKSPACE_TOOLS.map((tool) =>
    makeToggle(
      `setting.appearance.pages.${tool}`,
      { label: `Show the ${PAGES[tool].title} page`, words: findSetting("appearance.pages")?.words, scope: "App" },
      shown.includes(tool),
      (on) => settings.writeItem("appearance.pages", tool, on),
    ),
  );
  // the setting lists the checks turned off
  const off = settings.value("ide.checks").split(",");
  const checks = (cache.getQueryData(codesQuery.key) ?? []).map((code) =>
    makeToggle(
      `setting.ide.checks.${code.id}`,
      { label: `Check: ${code.label}`, words: [code.id, findSetting("ide.checks")?.words].join(" "), scope: "IDE" },
      !off.includes(code.id),
      (on) => settings.writeItem("ide.checks", code.id, !on),
    ),
  );
  return [...switches, ...pages, ...checks];
}

/**
 * makeToggle makes a row of the palette that flips a setting. It shows whether the setting is on, and picking it
 * flips it.
 */
function makeToggle(id: string, row: CommandPaletteItem, on: boolean, write: (on: boolean) => void): Command {
  return makeRow(id, {
    ...row,
    prefix: "Toggle:",
    icon: "i-lucide-check",
    slot: "setting",
    on,
    onSelect: () => write(!on),
  });
}

/** listValues lists the palette's settings with a value. Each opens Settings at its section. */
function listValues(): Command[] {
  return (useQueryCache().getQueryData(settingsCatalogQuery.key) ?? [])
    .filter((setting) => setting.type !== "switch" && !LISTS.includes(setting.key))
    .map((setting) =>
      makeRow(`setting.${setting.key}`, {
        label: setting.label,
        words: setting.words,
        icon: "i-lucide-settings",
        slot: "setting",
        opens: `opens ${toSentence(setting.section)}`,
        onSelect: () => bus.emit("settings.open", setting.section),
      }),
    );
}

/**
 * openGraph opens the Event Graph on the name under the model's caret. A caret on a name the graph lacks does
 * nothing.
 */
async function openGraph(model: Model): Promise<void> {
  const card = await lsp.Hover(model.place.path, model.caret.line, model.caret.column);
  if (card?.node) {
    await router.push({
      name: "event-graph",
      params: { id: useWorkspaceStore().activeId() },
      query: { root: card.node },
    });
  }
}

/** createPath asks where to make a new file or folder, starting in the active file's mod. Cancelling does nothing. */
async function createPath(place: Place, folder: boolean): Promise<void> {
  const title = folder ? "New folder" : "New file";
  const path = await Dialogs.SaveFile({ Title: title, Directory: place.root, ButtonText: "Create" });
  if (path) await workspaceApi.Create(path, folder);
}

/** renameFile asks where to move the active file to, and moves it. Cancelling does nothing. */
async function renameFile(place: Place): Promise<void> {
  const target = await Dialogs.SaveFile({
    Title: "Rename",
    Directory: place.root,
    Filename: place.name,
    ButtonText: "Rename",
  });
  if (target) await useEditorStore().rename(place.path, target);
}

/** copyPath puts the active file's path on the clipboard. */
async function copyPath(place: Place): Promise<void> {
  await Clipboard.SetText(place.path);
  toast?.add({ title: "Path copied", description: place.path, color: "success" });
}

/** discardFile asks whether to throw away the active file's changes, and does. */
async function discardFile(place: Place): Promise<void> {
  const body = `The changes to ${place.rel} are lost.`;
  if (await confirm.open({ title: "Discard the changes?", body, confirm: "Discard", destructive: true })) {
    await git.Discard(useGitStore().findRoot(place), [place.path]);
  }
}
