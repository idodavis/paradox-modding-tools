<!--
  The file explorer: the roots in the workspace's load order and their folders, with rename, drag and drop, git marks,
  the context menu, and a box, shown on asking, that filters the tree to the paths holding its text, in folders opened
  or not. A folder's listing is fetched when it expands, and a double click pins a preview tab.
-->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { useDebounceFn, useEventBus, watchDebounced } from "@vueuse/core";
import { Clipboard, Events } from "@wailsio/runtime";
import {
  FileTree,
  preparePresortedFileTreeInput,
  type FileTreeDensityKeyword,
  type FileTreeDirectoryHandle,
  type GitStatus,
  type GitStatusEntry,
} from "@pierre/trees";
import type { ContextMenuItem } from "@nuxt/ui";

import * as app from "@services/app/service";
import * as service from "@services/workspace/service";
import type { FileChange, Place } from "@services/workspace/models";

import { canCopyToMod, canSaveAsDds, COMMANDS, compare, copyToMod, findCommand, trashFile } from "../../app/commands";
import { fileSearchQuery, folderQuery, gitStatusQuery, loadQuery, workspacesQuery } from "../../queries";
import { tabId, useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSettingsStore } from "../../stores/settings";
import { COMMAND_BUS, useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import { findIcon, listIcons } from "./editor/tabs";
import SaveDdsModal from "./SaveDdsModal.vue";

const { roots } = defineProps<{ roots: Place[] }>();
const emit = defineEmits<{ open: [place: Place, preview: boolean] }>();
// whether the filter box shows; hiding it clears the filter, so a hidden box hides no row
const filtering = defineModel<boolean>("filtering", { default: false });
defineExpose({ collapseAll, reveal });

/** The mark each status git reports gets in the tree. */
const GIT_MARKS: Record<string, GitStatus> = {
  modified: "modified",
  added: "added",
  deleted: "deleted",
  renamed: "renamed",
  untracked: "untracked",
  conflict: "modified",
};
/** The symbol of the tree's sprite that draws each file icon. */
const SYMBOLS = new Map(listIcons().map((icon, index) => [icon, `pmt-${index}`]));
const SPRITE = `<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position: absolute">${[...SYMBOLS]
  .map(
    ([icon, id]) =>
      `<symbol id="${id}" viewBox="0 0 800 800"><image href="${icon}" width="800" height="800"/></symbol>`,
  )
  .join("")}</svg>`;
/** How many files the filter asks the file search for. The search lists the mods' files before the game's. */
const FILTER_HITS = 500;

/** A Move is a rename or a move done in the explorer. */
type Move = { from: string; to: string };

const editor = useEditorStore();
const git = useGitStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const saveDds = useOverlay().create(SaveDdsModal);
const host = useTemplateRef("host");
const view = shallowRef<FileTree>();
const menu = shallowRef<ContextMenuItem[][]>([]);
const filter = shallowRef("");

// every row of the tree is a path with its root's label first and a slash after each folder
const places = new Map<string, Place>(); // a row's place: each root's, and each row a folder listing gave
const members = new Map<string, Set<string>>(); // a listed folder row's rows
// a new row's path on disk, and its way to stop watching for its rename to be cancelled
const drafts = new Map<string, { path: string; stop: () => void }>();
const icons: Record<string, { name: string; viewBox: string }> = {}; // a file's icon by its name
// the rows git ignores, dimmed; a git status would count them as changes in every folder above
const ignoredSheet = new CSSStyleSheet();
// in Visual, the files it cannot draw are dimmed too
const undrawableSheet = new CSSStyleSheet();
const history = { done: [] as Move[], undone: [] as Move[] };
let afterClose: (() => void) | undefined;

const { data: workspaces } = useQuery(workspacesQuery);
const labels = computed(() => {
  const counts = new Map<string, number>();
  return roots.map((root) => {
    // the tree reads a "/" as a folder, so a mod's name writes it as a look-alike slash
    const label = workspace.originName(root.origin).replaceAll("/", "∕");
    const count = (counts.get(label) ?? 0) + 1;
    counts.set(label, count);
    return count > 1 ? `${label} (${count})` : label;
  });
});
const writable = computed(() => roots.some((root) => root.origin !== VANILLA));
const shape = computed(() =>
  [
    roots.map((root) => root.path).join("|"),
    labels.value.join("|"),
    writable.value,
    settings.isOn("ide.stickyFolders"),
    settings.isOn("ide.rootColors"),
    settings.value("ide.density"),
    workspaces.value?.find((row) => row.record.id === workspace.activeId())?.record.showBinaries,
  ].join("\n"),
);

const stops = [Events.On("fs:changed", ({ data }) => changed(data)), Events.On("git:changed", () => void markGit())];
onMounted(mount);
onBeforeUnmount(() => {
  stops.forEach((stop) => stop());
  view.value?.cleanUp();
});
watch(shape, mount);
watch(() => git.repositories, markGit);
watch(() => editor.activeFile()?.key, follow);
watch(() => editor.mode, dimUndrawable);
watchDebounced(filter, (query) => void filterRows(query), { debounce: 200 });
watch(filtering, (shown) => {
  if (shown) return;
  filter.value = "";
  void filterRows("");
});

/** mount builds the tree of the roots, with nothing listed yet. */
function mount(): void {
  // cleaning up leaves the old tree's element in the host
  view.value?.getFileTreeContainer()?.remove();
  view.value?.cleanUp();
  [places, members, drafts].forEach((map) => map.clear());
  Object.keys(icons).forEach((name) => delete icons[name]);
  if (!host.value || !roots.length) return;
  roots.forEach((root, index) => places.set(`${labels.value[index]}/`, root));
  const tree = new FileTree({
    // the roots stay in the order they come, the workspace's load order; what a root holds the tree sorts itself
    preparedInput: preparePresortedFileTreeInput(labels.value.map((label) => `${label}/`)),
    initialExpansion: "closed",
    icons: { set: "none", spriteSheet: SPRITE },
    density: settings.value("ide.density") as FileTreeDensityKeyword,
    stickyFolders: settings.isOn("ide.stickyFolders"),
    unsafeCSS: buildStyles(),
    renderRowDecoration: ({ item }) =>
      isRoot(item.path) && !isWritable(item.path) ? { icon: "file-tree-icon-lock", title: "Read only" } : null,
    onSelectionChange: select,
    renaming: writable.value && {
      canRename: ({ path, isFolder }) => {
        const row = rowOf(path, isFolder);
        return isWritable(row) && !isRoot(row);
      },
      onRename: ({ sourcePath, destinationPath, isFolder }) =>
        rename({ source: rowOf(sourcePath, isFolder), destination: rowOf(destinationPath, isFolder) }),
    },
    dragAndDrop: writable.value && {
      canDrop: ({ target: drop }) => drop.kind === "directory" && isWritable(drop.directoryPath ?? ""),
      onDropComplete: ({ draggedPaths, target: drop }) => {
        for (const path of draggedPaths)
          rename({
            source: path,
            destination: `${drop.directoryPath}${takeName(path)}${path.endsWith("/") ? "/" : ""}`,
          });
      },
    },
  });
  tree.subscribe(loadExpanded);
  tree.render({ containerWrapper: host.value });
  const shadow = host.value.querySelector("*")?.shadowRoot;
  if (shadow) shadow.adoptedStyleSheets = [...shadow.adoptedStyleSheets, ignoredSheet, undrawableSheet];
  view.value = tree;
  void markGit();
  follow();
  void filterRows(filter.value);
}

/**
 * buildStyles builds the styles of the root rows: a bar in the origin's color at the start, and the name in it if
 * asked.
 */
function buildStyles(): string {
  return roots
    .map((root, index) => {
      const row = CSS.escape(`${labels.value[index]}/`);
      const color = workspace.originColor(root.origin);
      const text = settings.isOn("ide.rootColors") ? `color: ${color};` : "";
      return `[data-item-path="${row}"] { font-weight: 600; ${text} border-radius: 0; box-shadow: inset 3px 0 0 ${color}; }`;
    })
    .join("\n");
}

/** rootIndex returns the index of the root a row is under. */
const rootIndex = (row: string) => labels.value.indexOf(row.slice(0, row.indexOf("/")));
const isRoot = (row: string) => row.indexOf("/") === row.length - 1;
const isWritable = (row: string) => (roots[rootIndex(row)]?.origin ?? VANILLA) !== VANILLA;

/** rowOf returns the row of a path the tree hands out. The tree leaves off the slash a folder's row ends in. */
function rowOf(path: string, isFolder: boolean): string {
  return isFolder && !path.endsWith("/") ? `${path}/` : path;
}

/** findPath finds a row's path on disk: its place's, or a new row's. It is "" for a row the tree lacks. */
function findPath(row: string): string {
  return places.get(row)?.path ?? drafts.get(row)?.path ?? "";
}

/** listRows lists the rows the explorer knows: each root's, then each row a folder's listing gave. */
function listRows(): string[] {
  return [...places.keys()];
}

/** findParent finds the folder a row is in. */
function findParent(row: string): string {
  return row.slice(0, row.replace(/\/$/, "").lastIndexOf("/") + 1);
}

/** findFolder finds a row's folder: the row itself for a folder, else the folder it is in. */
function findFolder(row: string): string {
  return row.endsWith("/") ? row : findParent(row);
}

/** takeName takes the last part of a row. */
function takeName(row: string): string {
  return row.replace(/\/$/, "").split("/").at(-1) ?? "";
}

/** findHandle finds the handle of a folder row in the tree. It is undefined for a file or a row the tree lacks. */
function findHandle(row: string): FileTreeDirectoryHandle | undefined {
  const item = view.value?.getItem(row);
  return item && "expand" in item ? item : undefined;
}

/** loadExpanded lists every folder that has been expanded and not listed yet. */
function loadExpanded(): void {
  for (const row of listRows())
    if (row.endsWith("/") && !members.has(row) && findHandle(row)?.isExpanded()) void load(row);
}

/**
 * load lists a folder row and puts its rows in the tree. A listing already on its way is shared, and one that no change
 * on disk has made stale is read from the cache. A folder gone from disk lists nothing and changes nothing: its
 * parent's listing takes its row away.
 */
async function load(row: string): Promise<void> {
  const children = await loadQuery(folderQuery(findPath(row))).then(
    (listed) => listed ?? [],
    () => undefined,
  );
  if (children) apply(row, children);
}

/** apply makes a folder row hold the listed rows, adding the new ones and removing those that are gone. */
function apply(row: string, children: Place[]): void {
  const next = new Set<string>();
  for (const child of children) {
    const path = `${row}${child.name}${child.isDir ? "/" : ""}`;
    next.add(path);
    places.set(path, child);
    if (!child.isDir) icons[child.name] = { name: SYMBOLS.get(findIcon(child)) ?? "", viewBox: "0 0 800 800" };
  }
  const tree = view.value;
  const gone = [...(members.get(row) ?? [])].filter((path) => !next.has(path) && tree?.getItem(path));
  const added = [...next].filter((path) => !tree?.getItem(path));
  tree?.batch([
    ...gone.map((path) => ({ type: "remove" as const, path, recursive: true })),
    ...added.map((path) => ({ type: "add" as const, path })),
  ]);
  tree?.setIcons({ set: "none", spriteSheet: SPRITE, byFileName: icons });
  members.set(row, next);
  dimUndrawable();
}

/** forget forgets a row and the rows under it, which the tree has moved or removed. */
function forget(row: string): void {
  for (const map of [places, members]) for (const path of [...map.keys()]) if (path.startsWith(row)) map.delete(path);
}

const refresh = useDebounceFn(() => {
  const rows = [...pending];
  pending.clear();
  rows.forEach((row) => void load(row));
}, 150);
const pending = new Set<string>();

/** changed lists again the listed folders that hold a changed path. */
function changed(change: FileChange): void {
  for (const row of members.keys()) if (change.path.startsWith(findPath(row) || "\0")) pending.add(row);
  void refresh();
}

/** markGit marks the rows git reports as changed and dims the ones it ignores, for each repository the mods are in. */
async function markGit(): Promise<void> {
  const tree = view.value;
  if (!tree) return;
  const marks: GitStatusEntry[] = [];
  const dimmed: string[] = [];
  for (const { root: dir } of git.repositories) {
    // a read on its way is shared; after git:changed the events table has made the status stale already
    const data = await loadQuery(gitStatusQuery(dir));
    for (const change of data?.changes ?? []) {
      const row = buildRow(change.place);
      if (row) marks.push({ path: row, status: GIT_MARKS[change.status] ?? "modified" });
    }
    for (const place of data?.ignored ?? []) {
      const row = buildRow(place);
      if (!row) continue;
      // a folder's row ends in a slash, and the folder stands for every row under it
      dimmed.push(buildSelector(row + (place.isDir ? "/" : ""), place.isDir));
    }
  }
  tree.setGitStatus(marks);
  dim(ignoredSheet, dimmed);
}

/** dim dims the rows in a sheet that the given selectors match, or no row for none. */
function dim(sheet: CSSStyleSheet, selectors: string[]): void {
  sheet.replaceSync(selectors.length ? `${selectors.join(",")} { color: var(--fg-3); }` : "");
}

/** buildSelector builds the CSS selector of a row, or of every row under it when it is a folder's. */
function buildSelector(row: string, isFolder: boolean): string {
  return `[data-item-path${isFolder ? "^" : ""}="${row.replaceAll('"', '\\"')}"]`;
}

/** dimUndrawable dims the files listed that Visual cannot draw. In Code nothing is dimmed. */
function dimUndrawable(): void {
  const rows = editor.mode === "visual" ? [...places].filter(([, place]) => !place.isDir && !place.drawable) : [];
  dim(
    undrawableSheet,
    rows.map(([row]) => buildSelector(row, false)),
  );
}

/**
 * buildRow builds the row of a place, without the slash a folder's row ends in. The row is relative to the root that
 * holds the place, so in a folder's window it is relative to that folder. It is "" for a place under none of the roots.
 */
function buildRow(place: Place): string {
  const index = roots.findIndex(
    (root) =>
      root.origin === place.origin && (!root.rel || place.rel === root.rel || place.rel.startsWith(`${root.rel}/`)),
  );
  const root = roots[index];
  return root ? `${labels.value[index]}/${place.rel.slice(root.rel ? root.rel.length + 1 : 0)}` : "";
}

/**
 * select keeps the files the user selected, and opens a file selected alone as a preview tab. A folder or the file
 * already shown opens nothing.
 */
function select(paths: readonly string[]): void {
  const files = paths.flatMap((path) => (path.endsWith("/") ? [] : (places.get(path) ?? [])));
  editor.selectFiles(files);
  const [place] = files;
  if (paths.length === 1 && place && place.key !== editor.activeFile()?.key) emit("open", place, true);
}

/** pin pins the selected file's tab. */
function pin(): void {
  const [only] = view.value?.getSelectedPaths() ?? [];
  const place = only && !only.endsWith("/") ? places.get(only) : undefined;
  if (place) emit("open", place, false);
}

/** follow reveals the active file in the tree, when the explorer follows the active file. */
function follow(): void {
  if (settings.isOn("ide.followFile")) void reveal();
}

/** reveal opens the folders down to the active file and selects it. A file the tree has no row for changes nothing. */
async function reveal(): Promise<void> {
  const place = editor.activeFile();
  const folders = place ? await loadFolders(place) : [];
  const tree = view.value;
  if (!place || !tree || !folders.length) return;
  folders.forEach((folder) => findHandle(folder)?.expand());
  const row = `${folders.at(-1)}${place.name}`;
  tree.getItem(row)?.select();
  tree.scrollToPath(row, { focus: false, offset: "nearest" });
}

/**
 * loadFolders lists the folders down to a file, so the tree holds the file's row, and returns their rows, the root's
 * first. A file under none of the tree's roots gives none, and so does a file under a folder gone from disk, such as a
 * deleted one.
 */
async function loadFolders(place: Place): Promise<string[]> {
  const row = buildRow(place);
  if (!row) return [];
  const folders = row
    .split("/")
    .slice(0, -1)
    .reduce<string[]>((rows, part) => [...rows, `${rows.at(-1) ?? ""}${part}/`], []);
  for (const folder of folders) {
    // a folder has a row once the listing of its parent found it on disk
    if (!findPath(folder)) return [];
    if (!members.has(folder)) await load(folder);
  }
  return folders;
}

/**
 * filterRows shows only the rows whose path holds the given query. The file search finds the files first and their
 * folders are listed, so a file in a folder not opened yet shows too. An empty query shows every row again.
 */
async function filterRows(query: string): Promise<void> {
  const text = query.trim();
  const hits = text ? await loadQuery(fileSearchQuery({ query: text, limit: FILTER_HITS })) : [];
  await Promise.all((hits ?? []).map(loadFolders));
  // a query typed since this one began is filtered by its own call
  if (filter.value.trim() === text) view.value?.setSearch(text || null);
}

/** collapseAll folds every folder of the tree. */
function collapseAll(): void {
  for (const row of listRows()) findHandle(row)?.collapse();
}

// the tree has moved the row already; a move Go refuses puts it back
const { mutate: rename } = useMutation({
  mutation: async ({ source, destination }: { source: string; destination: string }): Promise<Move> => {
    const move = { from: findPath(source), to: `${findPath(findParent(destination))}/${takeName(destination)}` };
    await editor.rename(move.from, move.to);
    return move;
  },
  onSuccess: (move, { source }) => {
    drafts.get(source)?.stop();
    drafts.delete(source);
    history.done.push(move);
    history.undone = [];
    forget(source);
  },
  onError: (_error, { source, destination }) => view.value?.move(destination, source),
});

const { mutate: replay } = useMutation({
  mutation: async (back: boolean) => {
    const move = (back ? history.done : history.undone).pop();
    if (!move) return;
    await (back ? editor.rename(move.to, move.from) : editor.rename(move.from, move.to));
    (back ? history.undone : history.done).push(move);
  },
});

/**
 * step steps the history back for Ctrl Z, and forward for Ctrl Y or Ctrl Shift Z. A key typed in an input is left
 * alone.
 */
function step(event: KeyboardEvent): void {
  const key = event.key.toLowerCase();
  const typing = event
    .composedPath()
    .some((node) => node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement);
  if (!event.ctrlKey || typing || (key !== "z" && key !== "y")) return;
  event.preventDefault();
  replay(key === "z" && !event.shiftKey);
}

/** create makes a new file or folder in a folder row and asks for its name in the tree. */
const { mutate: create } = useMutation({
  mutation: async ({ folder, dir }: { folder: string; dir: boolean }) => {
    if (!members.has(folder)) await load(folder);
    const occupied = (label: string) =>
      view.value?.getItem(`${folder}${label}`) ?? view.value?.getItem(`${folder}${label}/`);
    let label = dir ? "new_folder" : "new_file.txt";
    for (let count = 2; occupied(label); count++) label = dir ? `new_folder_${count}` : `new_file_${count}.txt`;
    const row = `${folder}${label}${dir ? "/" : ""}`;
    const path = `${findPath(folder)}/${label}`;
    await service.Create(path, dir);
    return { row, path };
  },
  onSuccess: ({ row, path }) => {
    const tree = view.value;
    if (!tree) return;
    tree.add(row);
    // a name left empty or cancelled takes the new row away, and the file with it
    const stop = tree.onMutation("remove", (event) => {
      if (event.path !== row) return;
      stop();
      remove(row);
    });
    drafts.set(row, { path, stop });
    tree.startRenaming(row, { removeIfCanceled: true });
  },
});

/**
 * remove moves a row to PMT's trash once the user agrees, and offers to bring it back. A mod's root leaves the
 * workspace. A row made a moment ago and left unnamed goes without a question.
 */
const { mutate: remove } = useMutation({
  mutation: async (row: string) => {
    const path = findPath(row);
    if (!drafts.has(row)) return trashFile(path, path, isRoot(row) ? " The mod also leaves this workspace." : "");
    await service.Delete(path);
    return true;
  },
  onSuccess: (deleted, row) => {
    drafts.get(row)?.stop();
    drafts.delete(row);
    if (!deleted) return;
    if (view.value?.getItem(row)) view.value.remove(row, { recursive: true });
    forget(row);
  },
});

// what the menu's rows of the command table do to the row clicked, whether they change the files, and on which rows
// they show when not on every one
const ACTIONS: Record<string, { writes: boolean; shows?: (row: string) => boolean; run: (row: string) => void }> = {
  "file.new": { writes: true, run: (row) => (afterClose = () => create({ folder: findFolder(row), dir: false })) },
  "file.newFolder": { writes: true, run: (row) => (afterClose = () => create({ folder: findFolder(row), dir: true })) },
  "file.rename": { writes: true, run: (row) => (afterClose = () => view.value?.startRenaming(row)) },
  "file.delete": { writes: true, run: (row) => remove(row) },
  "file.copyPath": { writes: false, run: (row) => void Clipboard.SetText(findPath(row)) },
  "file.reveal": { writes: false, run: (row) => void app.ShowFolder(findPath(row)) },
  "file.copyToMod": { writes: false, shows: (row) => canCopyToMod(places.get(row)), run: onPlace(copyToMod) },
  "image.saveDds": {
    writes: true,
    shows: (row) => row.endsWith("/") || canSaveAsDds(places.get(row)),
    run: onPlace((place) => void saveDds.open({ place })),
  },
};

/** onPlace makes a menu row's action that acts on the place of the row clicked, when the explorer has it. */
function onPlace(act: (place: Place) => void): (row: string) => void {
  return (row) => {
    const place = places.get(row);
    if (place) act(place);
  };
}

/** buildMenu builds the menu of a row. It is built when the menu opens, so it reads the selection as it is then. */
function buildMenu(row: string): ContextMenuItem[][] {
  const place = places.get(row);
  const file = row.endsWith("/") ? undefined : place;
  const dir = findPath(row);
  const commands = COMMANDS.flatMap((command) => {
    const action = ACTIONS[command.id];
    if (!action || (action.writes && !isWritable(row)) || (action.shows && !action.shows(row))) return [];
    const disabled = command.id === "file.rename" && isRoot(row);
    const label = command.id === "file.delete" && isRoot(row) ? "Delete the mod's folder" : command.label;
    return [{ label, icon: command.icon, kbds: command.kbds, disabled, onSelect: () => action.run(row) }];
  });
  const coat: ContextMenuItem[] =
    !file && place?.drawable && isWritable(row)
      ? [{ label: "New coat of arms", icon: "i-lucide-shield-plus", onSelect: () => commandBus.emit("draw.newCoat") }]
      : [];
  // a mod's root that is in no repository offers one
  const init: ContextMenuItem[] =
    isRoot(row) && isWritable(row) && !git.findRoot(roots[rootIndex(row)])
      ? [
          {
            label: "Initialize a git repository",
            icon: "i-lucide-git-branch-plus",
            onSelect: () => git.initialize(dir),
          },
        ]
      : [];
  const selected = findCommand("compare.selected");
  const comparisons = file
    ? [
        { label: "Select for compare", onSelect: () => (editor.compareFile = file) },
        {
          label: "Compare with selected",
          disabled: !editor.compareFile || editor.compareFile.key === file.key,
          onSelect: () => editor.compareFile && compare(editor.compareFile, file),
        },
        ...(selected?.when() ? [{ label: "Compare the two selected", onSelect: selected.onSelect }] : []),
      ]
    : [];
  return [buildOpenItems(file, dir), coat, init, commands, comparisons].filter((group) => group.length);
}

/**
 * buildOpenItems builds the menu items that open a row elsewhere. A file opens to the side, in a new window, and in
 * Visual when it can be drawn. A folder opens in a window of its own, or closes it when it is open already.
 */
function buildOpenItems(file: Place | undefined, dir: string): ContextMenuItem[] {
  if (file) {
    return [
      {
        label: "Open to the side",
        icon: "i-lucide-columns-2",
        onSelect: () => {
          editor.show(file);
          editor.split("row");
        },
      },
      {
        label: "Open in a new window",
        icon: "i-lucide-app-window",
        onSelect: () => {
          editor.show(file);
          windows.popOut("editor", [tabId({ key: file.key })]);
        },
      },
      ...(file.drawable
        ? [{ label: "Open as Visual", icon: "i-lucide-eye", onSelect: () => editor.showVisual(file) }]
        : []),
    ];
  }
  const folderWindow = `files:${dir}`;
  return windows.listWindows().includes(folderWindow)
    ? [
        {
          label: "Close the window",
          icon: "i-lucide-panel-top-close",
          onSelect: () => windows.closeOther(folderWindow),
        },
      ]
    : [{ label: "Open in a new window", icon: "i-lucide-app-window", onSelect: () => windows.popOut(folderWindow) }];
}

/** pick finds the row under a right click and builds its menu. Off a row there is no menu. */
function pick(event: MouseEvent): void {
  const element = event
    .composedPath()
    .find((one): one is HTMLElement => one instanceof HTMLElement && one.dataset.itemPath !== undefined);
  menu.value = element?.dataset.itemPath ? buildMenu(element.dataset.itemPath) : [];
  if (element) return;
  event.preventDefault();
  event.stopPropagation();
}

/** resume starts what the menu chose to do in a row, which needs the keyboard the menu held until it closed. */
function resume(event: Event): void {
  if (!afterClose) return;
  event.preventDefault();
  afterClose();
  afterClose = undefined;
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="filtering" class="shrink-0 px-2 pt-1 pb-1.5" @keydown.esc="filtering = false">
      <UInput v-model="filter" placeholder="Filter files" icon="i-lucide-search" size="xs" autofocus class="w-full" />
    </div>
    <!-- the menu's trigger takes its child's ref, so the tree's element is the one inside it -->
    <UContextMenu :items="menu" :content="{ onCloseAutoFocus: resume }">
      <div class="flex min-h-0 flex-1 flex-col">
        <div
          ref="host"
          class="min-h-0 flex-1"
          :style="{
            '--trees-bg-override': 'var(--bg-2)',
            '--trees-fg-override': 'color-mix(in oklab, var(--fg-2), var(--fg-1) 35%)',
            '--trees-fg-muted-override': 'var(--fg-3)',
            '--trees-selected-bg-override': 'var(--bg-4)',
            '--trees-selected-fg-override': 'var(--fg-1)',
            '--trees-accent-override': 'var(--accent)',
            '--trees-border-color-override': 'var(--line-1)',
            '--trees-focus-ring-color-override': 'var(--accent)',
            '--trees-font-family-override': 'var(--font-sans)',
            '--trees-font-size-override': 'var(--ide-ui-font)',
            '--trees-status-added-override': 'var(--green)',
            '--trees-status-untracked-override': 'var(--green)',
            '--trees-status-modified-override': 'var(--orange)',
            '--trees-status-deleted-override': 'var(--red)',
            '--trees-status-renamed-override': 'var(--blue)',
          }"
          @contextmenu.capture="pick"
          @keydown="step"
          @dblclick="pin"
        />
      </div>
    </UContextMenu>
  </div>
</template>
