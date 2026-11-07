/** The command table: each command of the app is one row, shaped as a menu item, with an id and a `when`. */
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { useMutation, useQueryCache } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { Dialogs } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";
import { computed } from "vue";

import { Format } from "@internal/files/models";
import * as appApi from "@services/app/service";
import * as git from "@services/git/service";
import * as lsp from "@services/lsp/service";
import * as workspaceApi from "@services/workspace/service";
import type { Fix } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import { confirm, copyText, toast } from "../lib/feedback";
import { findModel, save, saveUnsaved, unsavedModels, type Model } from "../pages/ide/editor/models";
import { copiesQuery, loadQuery, placeQuery, precedenceQuery } from "../queries";
import { useAgentStore } from "../stores/agent";
import { canSplit, useEditorStore, type Tab } from "../stores/editor";
import { STAGE, UNSTAGE, useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { COMMAND_BUS, THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore, VANILLA } from "../stores/workspace";
import { isWorkspacePage, type PageName } from "./pages";
import CreateModForm from "./CreateModForm.vue";
import { router } from "./router";

/** createMod is the app's one Create a mod dialog, shown over whatever page or dialog is open. */
const createMod = useOverlay().create(CreateModForm);

/** A Command is one command: a menu item whose id names its area, such as `editor.save`. */
export type Command = Omit<CommandPaletteItem, "kbds"> & {
  /** `<area>.<verb>`: editor.save, file.rename, graph.open, window.popOut … */
  id: string;
  /** Whether the command applies now. One that does not is left out of the palette and its shortcut is off. */
  when: () => boolean;
  /** The keys of the command's shortcut, such as meta and K. */
  kbds?: string[];
  /** The keys of a second shortcut. */
  secondKbds?: string[];
};

/** OWN_KEYS holds the commands a focused editor or input handles itself. Their shortcuts are off while typing. */
const OWN_KEYS = new Set(["editor.undo", "editor.redo", "editor.find"]);
/** BINDINGS is the setting that holds the shortcuts the user changed. */
const BINDINGS = "shortcuts.bindings";

const commandBus = useEventBus(COMMAND_BUS);
const compareBus = useEventBus<{ left: Place; right: Place }>("pmt.compare");
const choicesBus = useEventBus<CommandPaletteGroup>("pmt.choices");
const changedBindings = computed<Record<string, string[]>>(() => {
  try {
    return JSON.parse(useSettingsStore().value(BINDINGS) || "{}") as Record<string, string[]>;
  } catch {
    return {};
  }
});

// Runs work as a mutation, so a failure reaches the app's error handler. It is set when the shortcuts are registered.
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
    when: () => canSplit(activeTab()),
    onSelect: () => useEditorStore().split("row"),
  },
  {
    id: "editor.splitDown",
    label: "Split down",
    prefix: "IDE:",
    icon: "i-lucide-rows-2",
    when: () => canSplit(activeTab()),
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
    icon: "i-lucide-book-text",
    kbds: ["meta", "shift", "W"],
    when: isOpen,
    onSelect: () => useWindowsStore().sendCommand("editor.toggleWiki"),
  },
  {
    id: "editor.togglePanel",
    label: "Show or hide the bottom panel",
    prefix: "IDE:",
    icon: "i-lucide-panel-bottom",
    kbds: ["meta", "J"],
    when: isIde,
    onSelect: () => commandBus.emit("editor.togglePanel"),
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
    // Only the main window has both the editors and the explorer.
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
    when: hasGameCopy,
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
    when: () =>
      !!useEditorStore()
        .activeGroup()
        ?.tabs.some((tab) => !tab.start),
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
  // The window that shows the panel carries this out, since it holds the shells.
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
  {
    id: "mod.create",
    label: "Create a new mod",
    icon: "i-lucide-folder-plus",
    when: isOpen,
    onSelect: () => void createMod.open(),
  },
  {
    id: "help.open",
    label: "Help",
    icon: "i-lucide-circle-help",
    kbds: ["F1"],
    when: () => true,
    onSelect: () => commandBus.emit("help.open"),
  },
  {
    id: "docs.guide",
    label: "How to get the game's documentation",
    icon: "i-lucide-book-open",
    when: () => true,
    onSelect: () => commandBus.emit("docs.guide"),
  },
  {
    id: "game.launch",
    label: "Launch the game",
    icon: "i-lucide-play",
    when: isOpen,
    onSelect: () => run(() => workspaceApi.Launch()),
  },
  {
    id: "agent.togglePane",
    label: "Show or hide the agent pane",
    icon: "i-lucide-sparkles",
    kbds: ["meta", "alt", "I"],
    when: () => THIS_WINDOW === "main" && useWorkspaceStore().isOpen(),
    onSelect: () => useAgentStore().togglePane(),
  },
];

/** registerShortcuts registers every command's keyboard shortcuts, on while the command's `when` holds. */
export function registerShortcuts(): void {
  const { mutate } = useMutation({ mutation: (work: () => Promise<unknown>) => work() });
  run = mutate;
  defineShortcuts(
    computed(() => {
      const shortcuts: Record<string, { handler: () => void; usingInput: boolean }> = {};
      for (const row of COMMANDS.filter((command) => command.when())) {
        const handlers = extractShortcuts(listBindings(row).map((kbds) => ({ ...row, kbds })));
        for (const [keys, handler] of Object.entries(handlers)) {
          shortcuts[keys] = { handler, usingInput: !OWN_KEYS.has(row.id) };
        }
      }
      return shortcuts;
    }),
  );
}

/** listDefaultBindings returns a command's own shortcuts. It is empty when it has none. */
export function listDefaultBindings(command: Command): string[][] {
  return [command.kbds, command.secondKbds].filter((kbds) => kbds !== undefined);
}

/** listBindings returns a command's shortcuts as the user set them. */
export function listBindings(command: Command): string[][] {
  const changed = changedBindings.value[command.id];
  return listDefaultBindings(command).map((own, slot) => (changed?.[slot] ? splitShortcut(changed[slot]) : own));
}

/** writeBinding sets the shortcut in a slot of a command to the given keys, or restores its own if there are none. */
export function writeBinding(command: Command, slot: number, kbds?: string[]): void {
  const own = listDefaultBindings(command);
  const next = listBindings(command).map((keys, index) => (index === slot ? (kbds ?? own[index]) : keys));
  const changed = { ...changedBindings.value };
  if (next.every((keys, index) => joinShortcut(keys) === joinShortcut(own[index] ?? []))) delete changed[command.id];
  else changed[command.id] = next.map(joinShortcut);
  useSettingsStore().write(BINDINGS, JSON.stringify(changed));
}

/** findBinding returns the command that has the given keys as one of its shortcuts. It is undefined if none does. */
export function findBinding(kbds: string[]): Command | undefined {
  const shortcut = joinShortcut(kbds);
  return COMMANDS.find((command) => listBindings(command).some((keys) => joinShortcut(keys) === shortcut));
}

/** listPressedKeys returns the keys of a key press as a command row writes them. */
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

/** findKbds returns the keys of a command's first shortcut. It is empty for an unknown command. */
export function findKbds(id: string): string[] {
  const command = findCommand(id);
  return (command && listBindings(command)[0]) ?? [];
}

/** joinShortcut returns keys as Nuxt UI writes a shortcut, such as meta_shift_p. */
export function joinShortcut(kbds: string[]): string {
  return kbds.join("_").toLowerCase();
}

/** splitShortcut returns a shortcut's keys as the command rows write them. */
function splitShortcut(shortcut: string): string[] {
  return shortcut.split("_").map((key) => (key.length === 1 || /^f\d+$/.test(key) ? key.toUpperCase() : key));
}

/** compare opens the compare of two files in the IDE page, the first on the left. */
export function compare(left: Place, right: Place): void {
  void useWindowsStore()
    .showIde()
    .then(() => compareBus.emit({ left, right }));
}

/** compareWithGame opens the compare of a mod's file with the game's copy. It does nothing if there is none. */
export function compareWithGame(place: Place): void {
  run(async () => {
    const copy = findGameCopy(place, await loadQuery(copiesQuery(place.path)));
    if (copy) compare(copy, place);
  });
}

/** compareWithFile asks for another file and opens the compare of the given file with it. */
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

/** copyToMod copies a file of the game or of another mod into a mod and opens the copy. */
export function copyToMod(place: Place): void {
  chooseMod("file.copyToMod", "Choose the mod to copy into", listCopyTargets(place), (modId) =>
    run(async () => {
      const target = await loadQuery(placeQuery(await workspaceApi.CreateCopy(place.path, modId)));
      if (target) useEditorStore().show(target);
    }),
  );
}

/** chooseMod runs an action on the only mod of the given ids, or has the palette ask which when there are more. */
function chooseMod(id: string, label: string, mods: string[], act: (modId: string) => void): void {
  const [only] = mods;
  if (mods.length === 1 && only) {
    act(only);
    return;
  }
  const items = mods.map((modId) => ({ label: useWorkspaceStore().originName(modId), onSelect: () => act(modId) }));
  choicesBus.emit({ id, label, items });
}

/** listCopyTargets lists the mods a file can be copied into: every writable mod but the file's own. */
function listCopyTargets(place: Place): string[] {
  return useWorkspaceStore()
    .listWritableMods()
    .filter((mod) => mod.id !== place.origin)
    .map((mod) => mod.id);
}

/** findOverride returns the fix that overrides the entry at a position. It is null if there is nothing to override. */
export async function findOverride(place: Place, line: number, column: number): Promise<Fix | null> {
  if (!place.readOnly) return null;
  return lsp.BuildOverride(place.path, line, column, "").catch(() => null);
}

let overriding = false;

/**
 * overrideEntry copies the entry at a position of a read-only file into a mod, saves and opens the copy, and says
 * where it went. It does nothing while another override is under way. The promise stays unsettled if the palette's
 * question is closed unanswered.
 */
export function overrideEntry(place: Place, line: number, column: number, text: string, modId?: string): Promise<void> {
  return new Promise((settle) => {
    chooseMod(
      "editor.override",
      "Choose the mod to override in",
      modId ? [modId] : listCopyTargets(place),
      (target) => {
        if (overriding) {
          settle();
          return;
        }
        overriding = true;
        run(async () => {
          try {
            const fix = await lsp.BuildOverride(place.path, line, column, target);
            if (!fix) return;
            await useEditorStore().applyFixAsync(fix);
            const copy = fix.reveal?.place;
            if (!copy) return;
            // Saving puts the copy in the index, so the entry cannot be overridden into the mod a second time.
            const model = findModel(copy.key);
            if (model) await save(model);
            const gameId = useWorkspaceStore().active()?.game?.id ?? "";
            const precedence = await loadQuery(
              precedenceQuery({ gameId, database: copy.database, folder: copy.folder }),
            );
            toast?.add({
              title: `${text || "The entry"} copied into ${copy.rel}`,
              description: precedence,
              color: "success",
            });
          } finally {
            overriding = false;
            settle();
          }
        });
      },
    );
  });
}

/** canSaveAsDds reports whether a file is a mod's picture that is not a DDS already. */
export function canSaveAsDds(place: Place | null | undefined): boolean {
  return !!place && !place.readOnly && place.format === Format.FormatImage && !/\.dds$/i.test(place.name);
}

/** findGameCopy returns the game's copy of a mod's file, or undefined if there is none. */
export function findGameCopy(place: Place, copies: Place[] | null | undefined): Place | undefined {
  const [first] = copies ?? [];
  return place.origin !== VANILLA && first?.origin === VANILLA ? first : undefined;
}

/**
 * trashFile asks whether to move a file or folder to PMT's trash, moves it and offers to bring it back. It returns
 * where the file went in the trash, or "" if the user said no.
 */
export async function trashFile(path: string, label: string, note = ""): Promise<string> {
  const body = `${label} goes to PMT's trash.${note}`;
  if (!(await confirm.open({ title: "Delete?", body, confirm: "Delete", destructive: true }))) return "";
  const trashed = await workspaceApi.Delete(path);
  const undo = () => run(() => workspaceApi.Restore(trashed, path));
  toast?.add({ title: "Deleted", description: label, actions: [{ label: "Undo", onClick: undo }] });
  return trashed;
}

/**
 * removeMod asks, then closes the mod's tabs and takes it out of a workspace but not off the disk. The mod is named
 * by the given name. It asks once more when closing the tabs would drop unsaved text, and keeps the mod on a no.
 */
export async function removeMod(workspaceId: string, modId: string, name: string): Promise<void> {
  const confirmed = await confirm.open({
    title: `Remove ${name || "this mod"}?`,
    body: "The mod leaves this workspace. Its folder stays on disk.",
    confirm: "Remove mod",
    destructive: true,
  });
  const editor = useEditorStore();
  const isActive = workspaceId === useWorkspaceStore().activeId();
  const tabs = isActive ? editor.listTabs().filter((tab) => editor.placeOf(tab.key)?.origin === modId) : [];
  if (!confirmed || !(await editor.confirmClose(tabs))) return;
  tabs.forEach((tab) => editor.close(tab.id));
  await workspaceApi.RemoveMod(workspaceId, modId);
}

/** activeModel returns the model of the active tab's file, or null. */
function activeModel(): Model | null {
  const tab = activeTab();
  return tab ? findModel(tab.key) : null;
}

/** actOn makes a handler that calls an action on what a lookup finds, if anything. */
function actOn<T>(find: () => T | null | undefined, act: (value: T) => void): () => void {
  return () => {
    const value = find();
    if (value) act(value);
  };
}

/** canChange reports whether the active tab's file is a mod's. */
function canChange(): boolean {
  const place = useEditorStore().activeFile();
  return !!place && !place.readOnly;
}

/** hasGameCopy reports whether the active tab's file is a mod's with a game copy already loaded. */
function hasGameCopy(): boolean {
  const place = useEditorStore().activeFile();
  return !!place && !!findGameCopy(place, useQueryCache().getQueryData(copiesQuery(place.path).key));
}

/** canUseGit reports whether the active tab's file is a mod's, and that mod is in a repository. */
function canUseGit(): boolean {
  return canChange() && !!useGitStore().findRoot(useEditorStore().activeFile());
}

/** chooseRepository runs an action on the active file's repository, or has the palette ask which. */
function chooseRepository(id: string, label: string, act: (root: string) => void): void {
  const repositories = useGitStore().repositories;
  let root = useGitStore().findRoot(useEditorStore().activeFile());
  if (!root && repositories.length === 1) root = repositories[0]?.root ?? "";
  if (root) {
    act(root);
    return;
  }
  const items = repositories.map((repository) => ({
    label: (repository.mods ?? []).map(useWorkspaceStore().originName).join(", "),
    suffix: repository.root,
    icon: "i-lucide-folder-git-2",
    onSelect: () => act(repository.root),
  }));
  choicesBus.emit({ id, label, items });
}

/** showCommit shows the IDE's Source Control view with the given repository's commit box focused. */
function showCommit(root: string): void {
  void useWindowsStore()
    .showIde()
    .then(() => {
      useGitStore().folded.delete(root);
      useGitStore().commitTarget = root;
      useWindowsStore().sendCommand("git.commit");
    });
}

/** moveToNextGroup moves a tab into the group after its own, if there is one. */
function moveToNextGroup(tab: Tab): void {
  const editor = useEditorStore();
  const groups = editor.listGroups();
  const next = groups[groups.findIndex((group) => group.id === editor.activeGroup()?.id) + 1];
  if (next) editor.move(tab.id, next.id, next.tabs.length);
}

/** runAction runs one of Monaco's actions in the focused editor, or the one showing the active file. */
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

/** openGraph opens the Event Graph on the name under the model's caret, if the graph has it. */
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

/** createPath asks where to make a new file or folder. Cancelling does nothing. */
async function createPath(place: Place, folder: boolean): Promise<void> {
  const title = folder ? "New folder" : "New file";
  const path = await Dialogs.SaveFile({ Title: title, Directory: place.root, ButtonText: "Create" });
  if (path) await workspaceApi.Create(path, folder);
}
