/**
 * The command table. Every command of the app is one row here, in the menu item shape, with the area it belongs
 * to and when it applies. The palette, the context menus, the dropdown menus, the toolbar buttons and the
 * shortcuts all read these rows; a command exists nowhere else. Undo and redo are two rows over the model's own
 * history, and they stay out of the way of a focused editor.
 *
 * A command that the IDE page or the palette carries out, because it needs their state, is sent on the event bus
 * `pmt.command` with the row's id once the page shows.
 */
import type { CommandPaletteItem } from "@nuxt/ui";
import { useMutation, useQueryCache } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { Clipboard, Dialogs } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";
import { computed, nextTick } from "vue";

import * as appApi from "@services/app/service";
import * as git from "@services/git/service";
import * as lsp from "@services/lsp/service";
import * as workspaceApi from "@services/workspace/service";
import type { Place } from "@services/workspace/models";
import { findModel, save, saveUnsaved, unsavedModels, type Model } from "../pages/ide/editor/models";
import { listViews, parseTab, VIEW_ROWS } from "../pages/ide/editor/tabs";
import { drawablesQuery, settingsCatalogQuery, workspacesQuery } from "../queries";
import { useEditorStore, type Tab, type View } from "../stores/editor";
import { useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import ConfirmModal from "../ui/ConfirmModal.vue";
import { homeRoute, isWorkspacePage, listShownTools, PAGES, type Page, type PageName } from "./pages";
import { router } from "./router";

/** One command: a menu item whose id names its area, such as `editor.save`, and whether it applies now. */
export type Command = CommandPaletteItem & {
  /** `<area>.<verb>`: editor.save, file.rename, graph.open, window.popOut … */
  id: string;
  /** Whether the command applies now. One that does not is left out of the palette and its shortcut is off. */
  when: () => boolean;
};

// the commands a focused editor or input keeps for itself, so their shortcuts stay off while one is typed in
const OWN_KEYS = new Set(["editor.undo", "editor.redo", "editor.find"]);

const bus = useEventBus<string>("pmt.command");
const confirm = useOverlay().create(ConfirmModal);

// a write run as the one mutation, so a failure reaches the app's error handler, and the toast of the app; both are
// made when the shortcuts are registered, inside the app
let run: (work: () => Promise<unknown>) => void = () => undefined;
let toast: ReturnType<typeof useToast> | undefined;

const hasModel = () => !!activeModel();
const hasTab = () => !!useEditorStore().activeTab();
const isOpen = () => useWorkspaceStore().isOpen();

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
    onSelect: () => sendCommand("editor.findInFiles"),
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
    onSelect: () => sendCommand("editor.toggleWiki"),
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
    when: () => !!useEditorStore().activeFile(),
    onSelect: onFile((place) => run(() => copyPath(place))),
  },
  {
    id: "file.reveal",
    label: "Show in the file manager",
    icon: "i-lucide-folder-open",
    when: () => !!useEditorStore().activeFile(),
    onSelect: onFile((place) => run(() => appApi.ShowFolder(place.path))),
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
  {
    id: "git.stage",
    label: "Stage the file",
    icon: "i-lucide-plus",
    when: canUseGit,
    onSelect: onFile((place) => run(() => git.Stage(useGitStore().repository(), [place.path]))),
  },
  {
    id: "git.unstage",
    label: "Unstage the file",
    icon: "i-lucide-minus",
    when: canUseGit,
    onSelect: onFile((place) => run(() => git.Unstage(useGitStore().repository(), [place.path]))),
  },
  {
    id: "git.commit",
    label: "Commit",
    icon: "i-lucide-git-commit-horizontal",
    when: () => !!useGitStore().repository(),
    onSelect: () => sendCommand("git.commit"),
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
    onSelect: () => sendCommand("draw.newCoat"),
  },
  {
    id: "workspace.open",
    label: "Open a workspace",
    icon: "i-lucide-layout-grid",
    when: () => true,
    onSelect: () => openPage("library"),
  },
  {
    id: "ide.settings",
    label: "IDE settings",
    prefix: "IDE:",
    icon: "i-lucide-settings",
    when: isOpen,
    onSelect: () => sendCommand("ide.settings"),
  },
  {
    id: "workspace.settings",
    label: "Workspace settings",
    icon: "i-lucide-settings",
    when: isOpen,
    onSelect: () => openPage("workspace-settings"),
  },
];

/** Registers every command's keyboard shortcut. A command's shortcut is on while its `when` holds. */
export function registerShortcuts(): void {
  const { mutate } = useMutation({ mutation: (work: () => Promise<unknown>) => work() });
  run = mutate;
  toast = useToast();
  defineShortcuts(
    computed(() =>
      Object.fromEntries(
        COMMANDS.filter((row) => row.when()).flatMap((row) =>
          Object.entries(extractShortcuts([row])).map(([keys, handler]) => [
            keys,
            { handler, usingInput: !OWN_KEYS.has(row.id) },
          ]),
        ),
      ),
    ),
  );
}

/** The palette's groups: the commands that apply now, the pages, the workspaces, and the settings by type. */
export function paletteGroups(): { id: string; label: string; items: Command[] }[] {
  const settings = useSettingsStore();
  const cache = useQueryCache();
  const id = useWorkspaceStore().activeId();
  const names: PageName[] = ["library", "wizard", ...(id ? [...listShownTools(), "workspace-settings" as const] : [])];
  const pages = names.map((name) => {
    const page: Page = PAGES[name];
    return makeRow(`page.${name}`, {
      label: page.title,
      icon: page.icon ?? "i-lucide-plus",
      onSelect: () => openPage(name),
    });
  });
  const workspaces = (cache.getQueryData(workspacesQuery.key) ?? []).map((record) =>
    makeRow(`workspace.${record.id}`, {
      label: record.name,
      icon: record.id === id ? "i-lucide-check" : "i-lucide-folder",
      onSelect: () => void router.push(homeRoute(record.id, record.homePage, listShownTools())),
    }),
  );
  // the app's switches toggle here; every other setting opens where it is edited, the IDE's in the IDE's settings
  const catalog = cache.getQueryData(settingsCatalogQuery.key) ?? [];
  const switches = catalog
    .filter((setting) => setting.type === "switch" && !setting.workspace)
    .map((setting) => {
      const on = settings.value(setting.key) === "true";
      const toggle = () => settings.write(setting.key, String(!on));
      const row = { label: setting.label, words: setting.words, scope: setting.section };
      return makeRow(`setting.${setting.key}`, { ...row, prefix: "Toggle:", slot: "setting", on, onSelect: toggle });
    });
  const values = catalog
    .filter((setting) => setting.type !== "switch" || setting.workspace)
    .map((setting) => {
      const opens = setting.workspace
        ? () => sendCommand("ide.settings")
        : () => void router.push({ name: "workspace-settings", params: { id }, query: { section: setting.section } });
      const row = { label: setting.label, words: setting.words, scope: setting.workspace ? "IDE" : setting.section };
      return makeRow(`setting.${setting.key}`, { ...row, icon: "i-lucide-settings", slot: "setting", onSelect: opens });
    });
  return [
    { id: "commands", label: "Commands", items: COMMANDS.filter((row) => row.when()) },
    { id: "pages", label: "Pages", items: pages },
    { id: "workspaces", label: "Workspaces", items: workspaces },
    { id: "switches", label: "Settings you can switch", items: id ? switches : [] },
    { id: "values", label: "Settings with a value", items: id ? values : [] },
  ];
}

/**
 * Asks whether to move a file or folder to PMT's trash under the given label, moves it, and offers to bring it back.
 * A note, when given, is added to the question. It returns false when the user says no.
 */
export async function trashFile(path: string, label: string, note = ""): Promise<boolean> {
  const body = `${label} goes to PMT's trash.${note}`;
  if (!(await confirm.open({ title: "Delete?", body, confirm: "Delete", destructive: true }))) return false;
  const trashed = await workspaceApi.Delete(path);
  const undo = () => run(() => workspaceApi.Restore(trashed, path));
  toast?.add({ title: "Deleted", description: label, actions: [{ label: "Undo", onClick: undo }] });
  return true;
}

/** The model of the active tab's file, or null when the tab shows none. */
function activeModel(): Model | null {
  const tab = useEditorStore().activeTab();
  return tab ? findModel(parseTab(tab.id).key) : null;
}

/** Makes a handler that acts on the active tab, if there is one. */
function onTab(act: (tab: Tab) => void): () => void {
  return () => {
    const tab = useEditorStore().activeTab();
    if (tab) act(tab);
  };
}

/** Makes a handler that acts on the active model, if there is one. */
function onActive(act: (model: Model) => void): () => void {
  return () => {
    const model = activeModel();
    if (model) act(model);
  };
}

/** Makes a handler that acts on the place of the active tab's file, if there is one. */
function onFile(act: (place: Place) => void): () => void {
  return () => {
    const place = useEditorStore().activeFile();
    if (place) act(place);
  };
}

/** Whether the active tab's file is one a mod holds, which can be created beside, renamed and deleted. */
function canChange(): boolean {
  const place = useEditorStore().activeFile();
  return !!place && !place.readOnly;
}

/** Whether git has a repository and the active tab's file is a mod's. */
function canUseGit(): boolean {
  return canChange() && !!useGitStore().repository();
}

/** Makes the `when` of a view row: whether the active file can be shown in that view. */
function canShow(view: View): () => boolean {
  return () => {
    const place = useEditorStore().activeFile();
    const drawables = place && useQueryCache().getQueryData(drawablesQuery(place.path).key);
    return !!place && listViews(place, !!drawables?.length).includes(view);
  };
}

/** Moves a tab into the group after its own. The last group has none after it, so nothing moves. */
function moveToNextGroup(tab: Tab): void {
  const editor = useEditorStore();
  const groups = editor.layout().rows.flatMap((row) => row.columns);
  const next = groups[groups.findIndex((group) => group.id === editor.activeGroup()?.id) + 1];
  if (next) editor.move(tab.id, next.id, next.tabs.length);
}

/** Runs one of Monaco's actions in the editor with the focus, else in the one showing the active file. */
function runAction(id: string): void {
  const editors = monaco.editor.getEditors();
  const model = activeModel()?.model;
  const target =
    editors.find((editor) => editor.hasTextFocus()) ?? editors.find((editor) => editor.getModel() === model);
  void target?.getAction(id)?.run();
}

/** Shows the IDE page, then sends it the command, which only it can carry out. */
function sendCommand(command: string): void {
  void router
    .push({ name: "workspace-ide", params: { id: useWorkspaceStore().activeId() } })
    .then(nextTick)
    .then(() => bus.emit(command));
}

/** Shows a page. A workspace's pages are shown for the active workspace. */
function openPage(name: PageName): void {
  void (isWorkspacePage(name)
    ? router.push({ name, params: { id: useWorkspaceStore().activeId() } })
    : router.push({ name }));
}

/** Makes a row of the palette that is made at the time and always applies. */
function makeRow(id: string, row: CommandPaletteItem): Command {
  return { ...row, id, when: () => true };
}

/** Opens the Event Graph on the name under the model's caret. A caret on a name the graph lacks does nothing. */
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

/** Asks where to make a new file or folder, starting in the active file's mod. Cancelling does nothing. */
async function createPath(place: Place, folder: boolean): Promise<void> {
  const title = folder ? "New folder" : "New file";
  const path = await Dialogs.SaveFile({ Title: title, Directory: place.root, ButtonText: "Create" });
  if (path) await workspaceApi.Create(path, folder);
}

/** Asks where to move the active file to, and moves it. Cancelling does nothing. */
async function renameFile(place: Place): Promise<void> {
  const target = await Dialogs.SaveFile({
    Title: "Rename",
    Directory: place.root,
    Filename: place.name,
    ButtonText: "Rename",
  });
  if (target) await useEditorStore().rename(place.path, target);
}

/** Puts the active file's path on the clipboard. */
async function copyPath(place: Place): Promise<void> {
  await Clipboard.SetText(place.path);
  toast?.add({ title: "Path copied", description: place.path, color: "success" });
}

/** Asks whether to throw away the active file's changes, and does. */
async function discardFile(place: Place): Promise<void> {
  const body = `The changes to ${place.rel} are lost.`;
  if (await confirm.open({ title: "Discard the changes?", body, confirm: "Discard", destructive: true })) {
    await git.Discard(useGitStore().repository(), [place.path]);
  }
}
