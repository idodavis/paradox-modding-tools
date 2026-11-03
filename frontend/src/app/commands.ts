/**
 * The command table: each command of the app is one row, in the menu item shape, with the area it belongs to and when
 * it applies. A command that the IDE page or the palette carries out, because it needs their state, is sent on the
 * command bus with the row's id.
 */
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { useMutation } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { Dialogs } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";
import { computed } from "vue";

import { Format } from "@internal/files/models";
import * as appApi from "@services/app/service";
import * as git from "@services/git/service";
import * as lsp from "@services/lsp/service";
import * as workspaceApi from "@services/workspace/service";
import type { Place } from "@services/workspace/models";
import { confirm, copyText, toast } from "../lib/feedback";
import { findModel, save, saveUnsaved, unsavedModels, type Model } from "../pages/ide/editor/models";
import { copiesQuery, loadQuery, placeQuery } from "../queries";
import { useAgentStore } from "../stores/agent";
import { useEditorStore, type Tab } from "../stores/editor";
import { STAGE, UNSTAGE, useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { COMMAND_BUS, THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import { isWorkspacePage, type PageName } from "./pages";
import { router } from "./router";

/** A Command is one command: a menu item whose id names its area, such as `editor.save`, and whether it applies now. */
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

const commandBus = useEventBus(COMMAND_BUS);
const compareBus = useEventBus<{ left: Place; right: Place }>("pmt.compare");
// a list a command hands the palette to pick one from, under the command's id
const choicesBus = useEventBus<CommandPaletteGroup>("pmt.choices");
// the shortcuts the user changed, by command, parsed from the setting once per change; a setting that is not JSON
// counts as none
const changedBindings = computed<Record<string, string[]>>(() => {
  try {
    return JSON.parse(useSettingsStore().value(BINDINGS) || "{}") as Record<string, string[]>;
  } catch {
    return {};
  }
});

// a write run as the one mutation, so a failure reaches the app's error handler; it is made when the shortcuts are
// registered, inside the app
let run: (work: () => Promise<unknown>) => void = () => undefined;

const activeTab = () => useEditorStore().activeTab();
const activeFile = () => useEditorStore().activeFile();
const hasModel = () => !!activeModel();
const hasTab = () => !!activeTab();
const hasFile = () => !!activeFile();
const isOpen = () => useWorkspaceStore().isOpen();
const isIde = () => router.currentRoute.value.name === "workspace-ide";

/** COMMANDS holds every command of the app. */
export const COMMANDS: readonly Command[] = [
  {
    id: "palette.open",
    label: "Search and commands",
    icon: "i-lucide-search",
    kbds: ["meta", "K"],
    secondKbds: ["meta", "shift", "P"],
    when: () => true,
    onSelect: () => commandBus.emit("palette.open"),
  },
  {
    id: "editor.save",
    label: "Save",
    prefix: "IDE:",
    icon: "i-lucide-save",
    kbds: ["meta", "S"],
    when: hasModel,
    onSelect: actOn(activeModel, (model) => run(() => save(model))),
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
    onSelect: actOn(activeTab, (tab) =>
      run(async () => {
        if (await useEditorStore().confirmClose([tab])) useEditorStore().close(tab.id);
      }),
    ),
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
    onSelect: actOn(activeTab, moveToNextGroup),
  },
  {
    id: "editor.format",
    label: "Format document",
    prefix: "IDE:",
    icon: "i-lucide-align-left",
    kbds: ["shift", "alt", "F"],
    when: () => hasModel() && canChange(),
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
    onSelect: () => commandBus.emit("editor.goToFile"),
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
    onSelect: actOn(activeModel, (model) => run(() => openGraph(model))),
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
    onSelect: actOn(activeModel, (model) => void model.model.undo()),
  },
  {
    id: "editor.redo",
    label: "Redo",
    icon: "i-lucide-redo-2",
    kbds: ["meta", "Y"],
    when: hasModel,
    onSelect: actOn(activeModel, (model) => void model.model.redo()),
  },
  {
    id: "file.new",
    label: "New file",
    icon: "i-lucide-file-plus",
    when: canChange,
    onSelect: actOn(activeFile, (place) => run(() => createPath(place, false))),
  },
  {
    id: "file.newFolder",
    label: "New folder",
    icon: "i-lucide-folder-plus",
    when: canChange,
    onSelect: actOn(activeFile, (place) => run(() => createPath(place, true))),
  },
  {
    id: "file.rename",
    label: "Rename",
    icon: "i-lucide-pencil",
    when: canChange,
    onSelect: actOn(activeFile, (place) =>
      run(async () => {
        const target = await Dialogs.SaveFile({
          Title: "Rename",
          Directory: place.root,
          Filename: place.name,
          ButtonText: "Rename",
        });
        if (target) await useEditorStore().rename(place.path, target);
      }),
    ),
  },
  {
    id: "file.delete",
    label: "Delete",
    icon: "i-lucide-trash-2",
    color: "error",
    when: canChange,
    onSelect: actOn(activeFile, (place) => run(() => trashFile(place.path, place.rel))),
  },
  {
    id: "file.copyPath",
    label: "Copy the path",
    icon: "i-lucide-copy",
    when: hasFile,
    onSelect: actOn(activeFile, (place) => run(() => copyText(place.path, "Path", place.path))),
  },
  {
    id: "file.reveal",
    label: "Show in the file manager",
    icon: "i-lucide-folder-open",
    when: hasFile,
    onSelect: actOn(activeFile, (place) => run(() => appApi.ShowFolder(place.path))),
  },
  {
    id: "file.copyToMod",
    label: "Copy into a mod…",
    icon: "i-lucide-copy-plus",
    when: () => canCopyToMod(useEditorStore().activeFile()),
    onSelect: actOn(activeFile, copyToMod),
  },
  {
    id: "image.saveDds",
    label: "Save as DDS…",
    prefix: "Image:",
    icon: "i-lucide-file-image",
    when: () => canSaveAsDds(useEditorStore().activeFile()),
    onSelect: () => useWindowsStore().sendCommand("image.saveDds"),
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
    onSelect: actOn(
      activeFile,
      (place) =>
        void useWindowsStore()
          .showIde()
          .then(() =>
            useEditorStore().open({ key: place.key, side: "changes", root: useGitStore().findRoot(place) }, place),
          ),
    ),
  },
  {
    id: "compare.file",
    label: "Active file with another file",
    prefix: "Compare:",
    icon: "i-lucide-diff",
    when: hasFile,
    onSelect: actOn(activeFile, compareWithFile),
  },
  {
    id: "compare.game",
    label: "Active file with the game's copy",
    prefix: "Compare:",
    icon: "i-lucide-diff",
    when: canChange,
    onSelect: actOn(activeFile, compareWithGame),
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
    onSelect: actOn(activeFile, (place) => {
      const params = { id: useWorkspaceStore().activeId() };
      void router.push({ name: "release", params, query: { mod: place.origin, compare: "steam" } });
    }),
  },
  {
    id: "ide.mode",
    label: "Switch between Code and Visual",
    prefix: "IDE:",
    icon: "i-lucide-eye",
    kbds: ["meta", "shift", "V"],
    when: () => useEditorStore().isDrawable(useEditorStore().activeTab()),
    onSelect: () => {
      const editor = useEditorStore();
      editor.mode = editor.mode === "visual" ? "code" : "visual";
    },
  },
  {
    id: "window.popOut",
    label: "Open the group in a new window",
    icon: "i-lucide-app-window",
    when: () => !!useEditorStore().activeGroup()?.tabs.length,
    onSelect: () =>
      useWindowsStore().popOut(
        "editor",
        useEditorStore()
          .activeGroup()
          ?.tabs.map((tab) => tab.id),
      ),
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
    onSelect: actOn(activeFile, (place) => run(() => git.Run(useGitStore().findRoot(place), ...STAGE, place.path))),
  },
  {
    id: "git.unstage",
    label: "Unstage the file",
    icon: "i-lucide-minus",
    when: canUseGit,
    onSelect: actOn(activeFile, (place) => run(() => git.Run(useGitStore().findRoot(place), ...UNSTAGE, place.path))),
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
    onSelect: actOn(activeFile, (place) =>
      run(async () => {
        const body = `The changes to ${place.rel} are lost.`;
        if (await confirm.open({ title: "Discard the changes?", body, confirm: "Discard", destructive: true })) {
          await git.Discard(useGitStore().findRoot(place), [place.path]);
        }
      }),
    ),
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
    onSelect: () => commandBus.emit("settings.open"),
  },
  // the pane sits in the main window, beside a workspace's pages
  {
    id: "agent.togglePane",
    label: "Show or hide the agent pane",
    icon: "i-lucide-sparkles",
    kbds: ["meta", "alt", "I"],
    when: () => THIS_WINDOW === "main" && useWorkspaceStore().isOpen(),
    onSelect: () => useAgentStore().togglePane(),
  },
];

/**
 * registerShortcuts registers every command's keyboard shortcuts as the user set them. A command's shortcuts are on
 * while its `when` holds.
 */
export function registerShortcuts(): void {
  const { mutate } = useMutation({ mutation: (work: () => Promise<unknown>) => work() });
  run = mutate;
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
  const changed = changedBindings.value[command.id];
  return listDefaultBindings(command).map((own, slot) => (changed?.[slot] ? splitShortcut(changed[slot]) : own));
}

/**
 * writeBinding sets one of a command's shortcuts, the given place among them, to the given keys. With no keys it puts
 * the command's own back. A command whose shortcuts are all its own again leaves the setting.
 */
export function writeBinding(command: Command, slot: number, kbds?: string[]): void {
  const own = listDefaultBindings(command);
  const next = listBindings(command).map((keys, index) => (index === slot ? (kbds ?? own[index]) : keys));
  const changed = { ...changedBindings.value };
  if (next.every((keys, index) => joinShortcut(keys) === joinShortcut(own[index] ?? []))) delete changed[command.id];
  else changed[command.id] = next.map(joinShortcut);
  useSettingsStore().write(BINDINGS, JSON.stringify(changed));
}

/** findBinding returns the command one of whose shortcuts is the given keys, or undefined when none is. */
export function findBinding(kbds: string[]): Command | undefined {
  const shortcut = joinShortcut(kbds);
  return COMMANDS.find((command) => listBindings(command).some((keys) => joinShortcut(keys) === shortcut));
}

/**
 * listPressedKeys returns the keys of a key press as a command row writes them, Ctrl as meta and a letter in
 * capitals.
 */
export function listPressedKeys(event: KeyboardEvent): string[] {
  const key = event.key.length === 1 ? event.key.toUpperCase() : event.key.toLowerCase();
  return [event.ctrlKey && "meta", event.shiftKey && "shift", event.altKey && "alt", key].filter(
    (part) => typeof part === "string",
  );
}

/** findCommand returns the command with the given id, or undefined when there is none. */
export function findCommand(id: string): Command | undefined {
  return COMMANDS.find((row) => row.id === id);
}

/** findKbds returns the keys of a command's first shortcut as the user set it. It is empty for an unknown command. */
export function findKbds(id: string): string[] {
  const command = findCommand(id);
  return (command && listBindings(command)[0]) ?? [];
}

/** joinShortcut returns keys as Nuxt UI writes a shortcut, such as meta_shift_p. */
export function joinShortcut(kbds: string[]): string {
  return kbds.join("_").toLowerCase();
}

/** splitShortcut returns a shortcut's keys, a letter or a function key in capitals as the command rows write them. */
function splitShortcut(shortcut: string): string[] {
  return shortcut.split("_").map((key) => (key.length === 1 || /^f\d+$/.test(key) ? key.toUpperCase() : key));
}

/** compare shows the IDE page, then opens the compare of two files in it, the first on the left. */
export function compare(left: Place, right: Place): void {
  void useWindowsStore()
    .showIde()
    .then(() => compareBus.emit({ left, right }));
}

/**
 * compareWithGame opens the compare of a mod's file with the game's copy of it. A file the game has no copy of gets a
 * message.
 */
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

/** canCopyToMod reports whether a file, not a folder, has a mod to be copied into. */
export function canCopyToMod(place: Place | null | undefined): boolean {
  return !!place && !place.isDir && listCopyTargets(place).length > 0;
}

/**
 * copyToMod copies a file of the game or of another mod to the same place in a mod, and opens the copy. When it can
 * go into more than one mod, the palette asks which.
 */
export function copyToMod(place: Place): void {
  const copy = (modId: string) =>
    run(async () => {
      const target = await loadQuery(placeQuery(await workspaceApi.CreateCopy(place.path, modId)));
      if (target) useEditorStore().show(target);
    });
  const mods = listCopyTargets(place);
  const [only] = mods;
  if (mods.length === 1 && only) {
    copy(only);
    return;
  }
  const items = mods.map((id) => ({ label: useWorkspaceStore().originName(id), onSelect: () => copy(id) }));
  choicesBus.emit({ id: "file.copyToMod", label: "Choose the mod to copy into", items });
}

/** listCopyTargets lists the mods a file can be copied into: every mod of the workspace but the file's own. */
function listCopyTargets(place: Place): string[] {
  return useWorkspaceStore()
    .listModIds()
    .filter((id) => id !== place.origin);
}

/** canSaveAsDds reports whether a file can be saved as a DDS: it is a mod's picture that is not a DDS already. */
export function canSaveAsDds(place: Place | null | undefined): boolean {
  return !!place && !place.readOnly && place.format === Format.FormatImage && !/\.dds$/i.test(place.name);
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
  const tab = activeTab();
  return tab ? findModel(tab.key) : null;
}

/** actOn makes a handler that calls the given action on what the given lookup finds, if it finds anything. */
function actOn<T>(find: () => T | null | undefined, act: (value: T) => void): () => void {
  return () => {
    const value = find();
    if (value) act(value);
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
    label: (one.mods ?? []).map(useWorkspaceStore().originName).join(", "),
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

/** moveToNextGroup moves a tab into the group after its own. The last group has none after it, so nothing moves. */
function moveToNextGroup(tab: Tab): void {
  const editor = useEditorStore();
  const groups = editor.listGroups();
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
export function openPage(name: PageName): void {
  void (isWorkspacePage(name)
    ? router.push({ name, params: { id: useWorkspaceStore().activeId() } })
    : router.push({ name }));
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
