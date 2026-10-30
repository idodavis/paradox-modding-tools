<!--
  The file explorer: the roots in the workspace's load order and their folders, with rename, drag and drop, git marks,
  the context menu, and a box, shown on asking, that filters the tree to the paths holding its text, in folders opened
  or not. A folder's listing is fetched when it expands, and a double click pins a preview tab.
-->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useDebounceFn, useEventBus, useLocalStorage, watchDebounced } from "@vueuse/core";
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

import type { Root } from "@internal/files/models";
import * as app from "@services/app/service";
import * as service from "@services/workspace/service";
import type { Child, FileChange, Place } from "@services/workspace/models";

import { canCopyToMod, canSaveAsDds, COMMANDS, compare, copyToMod, trashFile } from "../../app/commands";
import { fileSearchQuery, folderQuery, gitStatusQuery, loadQuery, workspacesQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSettingsStore } from "../../stores/settings";
import { useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import { fileTab, findIcon, listIcons, VIEWS } from "./editor/tabs";
import SaveDdsModal from "./SaveDdsModal.vue";

const { roots } = defineProps<{ roots: Root[] }>();
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

/** A rename or a move done in the explorer. */
type Move = { from: string; to: string };

const editor = useEditorStore();
const git = useGitStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const queryCache = useQueryCache();
const commandBus = useEventBus<string>("pmt.command");
const saveDds = useOverlay().create(SaveDdsModal);
const compareWith = useLocalStorage<Place | null>("pmt.ide.compare", null);
const host = useTemplateRef("host");
const view = shallowRef<FileTree>();
const menu = shallowRef<ContextMenuItem[][]>([]);
const filter = shallowRef("");

// every row of the tree is a path with its root's label first and a slash after each folder
const disks = new Map<string, string>(); // a row's path on disk
const places = new Map<string, Place>(); // a row's place, for the rows a folder listing gave
const members = new Map<string, Set<string>>(); // a folder row's rows
const listings = new Map<string, Promise<void>>(); // a folder row's listing, started when it expanded
const drafts = new Map<string, () => void>(); // a new row's way to stop watching for its rename to be cancelled
const icons: Record<string, { name: string; viewBox: string }> = {}; // a file's icon by its name
// the rows git ignores, dimmed; a git status would count them as changes in every folder above
const ignoredSheet = new CSSStyleSheet();
const history = { done: [] as Move[], undone: [] as Move[] };
let afterClose: (() => void) | undefined;

const { data: workspaces } = useQuery(workspacesQuery);
const labels = computed(() => {
  const counts = new Map<string, number>();
  return roots.map((root) => {
    const label = workspace.originName(root.origin);
    const count = (counts.get(label) ?? 0) + 1;
    counts.set(label, count);
    return count > 1 ? `${label} (${count})` : label;
  });
});
const writable = computed(() => roots.some((root) => root.origin !== VANILLA));
const shape = computed(() =>
  [
    roots.map((root) => root.dir).join("|"),
    labels.value.join("|"),
    writable.value,
    settings.isOn("ide.stickyFolders"),
    settings.isOn("ide.rootColors"),
    settings.value("ide.density"),
    workspaces.value?.find((one) => one.id === workspace.activeId())?.showBinaries,
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
watchDebounced(filter, (query) => void filterRows(query), { debounce: 200 });
watch(filtering, (shown) => {
  if (shown) return;
  filter.value = "";
  void filterRows("");
});

/** Builds the tree of the roots, with nothing listed yet. */
function mount(): void {
  // cleaning up leaves the old tree's element in the host
  view.value?.getFileTreeContainer()?.remove();
  view.value?.cleanUp();
  [disks, places, members, listings, drafts].forEach((map) => map.clear());
  Object.keys(icons).forEach((name) => delete icons[name]);
  if (!host.value || !roots.length) return;
  roots.forEach((root, index) => disks.set(`${labels.value[index]}/`, root.dir));
  const tree = new FileTree({
    // the roots stay in the order they come, the workspace's load order; what a root holds the tree sorts itself
    preparedInput: preparePresortedFileTreeInput([...disks.keys()]),
    initialExpansion: "closed",
    icons: { set: "none", spriteSheet: SPRITE },
    density: settings.value("ide.density") as FileTreeDensityKeyword,
    stickyFolders: settings.isOn("ide.stickyFolders"),
    unsafeCSS: buildStyles(),
    renderRowDecoration: ({ item }) =>
      isRoot(item.path) && !isWritable(item.path) ? { icon: "file-tree-icon-lock", title: "Read only" } : null,
    onSelectionChange: select,
    renaming: writable.value && {
      canRename: ({ path }) => isWritable(path) && !isRoot(path),
      onRename: ({ sourcePath, destinationPath }) => rename({ source: sourcePath, destination: destinationPath }),
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
  if (shadow) shadow.adoptedStyleSheets = [...shadow.adoptedStyleSheets, ignoredSheet];
  view.value = tree;
  void markGit();
  follow();
  void filterRows(filter.value);
}

/** Builds the styles of the root rows: a bar in the origin's color at the start, and the name in it if asked. */
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

/** The index of the root a row is under. */
const rootIndex = (row: string) => labels.value.indexOf(row.slice(0, row.indexOf("/")));
const isRoot = (row: string) => row.indexOf("/") === row.length - 1;
const isWritable = (row: string) => (roots[rootIndex(row)]?.origin ?? VANILLA) !== VANILLA;

/** Finds the folder a row is in. */
function findParent(row: string): string {
  return row.slice(0, row.replace(/\/$/, "").lastIndexOf("/") + 1);
}

/** Finds a row's folder: the row itself for a folder, else the folder it is in. */
function findFolder(row: string): string {
  return row.endsWith("/") ? row : findParent(row);
}

/** Takes the last part of a row. */
function takeName(row: string): string {
  return row.replace(/\/$/, "").split("/").at(-1) ?? "";
}

/** Finds the handle of a folder row in the tree. It is undefined for a file or a row the tree lacks. */
function findHandle(row: string): FileTreeDirectoryHandle | undefined {
  const item = view.value?.getItem(row);
  return item && "expand" in item ? item : undefined;
}

/** Lists every folder that has been expanded and not listed yet. */
function loadExpanded(): void {
  for (const row of disks.keys())
    if (row.endsWith("/") && !listings.has(row) && findHandle(row)?.isExpanded()) void load(row);
}

/** Lists a folder row once and puts its rows in the tree. */
function load(row: string): Promise<void> {
  let listing = listings.get(row);
  if (!listing) {
    listing = list(disks.get(row) ?? "").then((children) => apply(row, children));
    listings.set(row, listing);
  }
  return listing;
}

/** Lists a folder from Go. */
async function list(dir: string): Promise<Child[]> {
  const { data } = await queryCache.fetch(queryCache.ensure(folderQuery(dir)));
  return data ?? [];
}

/** Makes a folder row hold the listed rows, adding the new ones and removing those that are gone. */
function apply(row: string, children: Child[]): void {
  const next = new Set<string>();
  for (const child of children) {
    const path = `${row}${child.name}${child.place.isDir ? "/" : ""}`;
    next.add(path);
    disks.set(path, child.place.path);
    places.set(path, child.place);
    if (!child.place.isDir)
      icons[child.name] = { name: SYMBOLS.get(findIcon(child.place)) ?? "", viewBox: "0 0 800 800" };
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
}

/** Forgets a row and the rows under it, which the tree has moved or removed. */
function forget(row: string): void {
  for (const map of [disks, places, members, listings])
    for (const path of [...map.keys()]) if (path.startsWith(row)) map.delete(path);
}

const refresh = useDebounceFn(() => {
  const rows = [...pending];
  pending.clear();
  rows.forEach((row) => void list(disks.get(row) ?? "").then((children) => apply(row, children)));
}, 150);
const pending = new Set<string>();

/** Lists again the folders that hold a changed path. */
function changed(change: FileChange): void {
  for (const row of listings.keys()) if (change.path.startsWith(disks.get(row) ?? "\0")) pending.add(row);
  void refresh();
}

/** Marks the rows git reports as changed and dims the ones it ignores, for each repository the mods are in. */
async function markGit(): Promise<void> {
  const tree = view.value;
  if (!tree) return;
  const marks: GitStatusEntry[] = [];
  const dimmed: string[] = [];
  for (const { root: dir } of git.repositories) {
    const { data } = await queryCache.fetch(queryCache.ensure(gitStatusQuery(dir)));
    for (const change of data?.changes ?? []) {
      const index = roots.findIndex((root) => root.origin === change.place.origin);
      if (index >= 0) {
        marks.push({
          path: `${labels.value[index]}/${change.place.rel}`,
          status: GIT_MARKS[change.status] ?? "modified",
        });
      }
    }
    for (const path of data?.ignored ?? []) {
      const row = findRow(path);
      if (row) dimmed.push(`[data-item-path${row.endsWith("/") ? "^" : ""}="${row.replaceAll('"', '\\"')}"]`);
    }
  }
  tree.setGitStatus(marks);
  ignoredSheet.replaceSync(dimmed.length ? `${dimmed.join(",")} { color: var(--fg-3); }` : "");
}

/** Finds the row of a path on disk. It is "" for a path under none of the tree's roots. A folder's path ends in "/". */
function findRow(path: string): string {
  const slashed = path.replaceAll("\\", "/");
  for (const [index, root] of roots.entries()) {
    const base = root.dir.replaceAll("\\", "/").replace(/\/$/, "");
    if (slashed.toLowerCase().startsWith(`${base.toLowerCase()}/`))
      return `${labels.value[index]}${slashed.slice(base.length)}`;
  }
  return "";
}

/**
 * Keeps the files the user selected, and opens a file selected alone as a preview tab. A folder or the file already
 * shown opens nothing.
 */
function select(paths: readonly string[]): void {
  const files = paths.flatMap((path) => (path.endsWith("/") ? [] : (places.get(path) ?? [])));
  editor.selectFiles(files);
  const [place] = files;
  if (paths.length === 1 && place && place.key !== editor.activeFile()?.key) emit("open", place, true);
}

/** Pins the selected file's tab. */
function pin(): void {
  const [only] = view.value?.getSelectedPaths() ?? [];
  const place = only && !only.endsWith("/") ? places.get(only) : undefined;
  if (place) emit("open", place, false);
}

/** Reveals the active file in the tree, when the explorer follows the active file. */
function follow(): void {
  if (settings.isOn("ide.followFile")) void reveal();
}

/** Opens the folders down to the active file and selects it. A file the tree has no row for changes nothing. */
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
 * Lists the folders down to a file, so the tree holds the file's row, and returns their rows, the root's first. A file
 * under none of the tree's roots gives none, and so does a file under a folder gone from disk, such as a deleted one.
 */
async function loadFolders(place: Place): Promise<string[]> {
  const index = roots.findIndex((root) => root.dir === place.root);
  if (index < 0) return [];
  const folders = place.rel
    .split("/")
    .slice(0, -1)
    .reduce<string[]>((rows, part) => [...rows, `${rows.at(-1)}${part}/`], [`${labels.value[index]}/`]);
  for (const folder of folders) {
    // a folder has a row once the listing of its parent found it on disk
    if (!disks.has(folder)) return [];
    await load(folder);
  }
  return folders;
}

/**
 * Shows only the rows whose path holds the query. The file search finds the files first and their folders are listed,
 * so a file in a folder not opened yet shows too. An empty query shows every row again.
 */
async function filterRows(query: string): Promise<void> {
  const text = query.trim();
  const hits = text ? await loadQuery(fileSearchQuery({ query: text, limit: FILTER_HITS })) : [];
  await Promise.all((hits ?? []).map(loadFolders));
  // a query typed since this one began is filtered by its own call
  if (filter.value.trim() === text) view.value?.setSearch(text || null);
}

/** Folds every folder of the tree. */
function collapseAll(): void {
  for (const row of disks.keys()) findHandle(row)?.collapse();
}

// the tree has moved the row already; a move Go refuses puts it back
const { mutate: rename } = useMutation({
  mutation: async ({ source, destination }: { source: string; destination: string }): Promise<Move> => {
    const move = {
      from: disks.get(source) ?? "",
      to: `${disks.get(findParent(destination)) ?? ""}/${takeName(destination)}`,
    };
    await editor.rename(move.from, move.to);
    return move;
  },
  onSuccess: (move, { source }) => {
    drafts.get(source)?.();
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

/** Steps the history back for Ctrl Z, and forward for Ctrl Y or Ctrl Shift Z. A key typed in an input is left alone. */
function step(event: KeyboardEvent): void {
  const key = event.key.toLowerCase();
  const typing = event
    .composedPath()
    .some((node) => node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement);
  if (!event.ctrlKey || typing || (key !== "z" && key !== "y")) return;
  event.preventDefault();
  replay(key === "z" && !event.shiftKey);
}

/** Makes a new file or folder in a folder row and asks for its name in the tree. */
const { mutate: create } = useMutation({
  mutation: async ({ folder, dir }: { folder: string; dir: boolean }) => {
    await load(folder);
    const occupied = (label: string) =>
      view.value?.getItem(`${folder}${label}`) ?? view.value?.getItem(`${folder}${label}/`);
    let label = dir ? "new_folder" : "new_file.txt";
    for (let count = 2; occupied(label); count++) label = dir ? `new_folder_${count}` : `new_file_${count}.txt`;
    const row = `${folder}${label}${dir ? "/" : ""}`;
    const path = `${disks.get(folder)}/${label}`;
    await service.Create(path, dir);
    disks.set(row, path);
    return row;
  },
  onSuccess: (row) => {
    const tree = view.value;
    if (!tree) return;
    tree.add(row);
    // a name left empty or cancelled takes the new row away, and the file with it
    const stop = tree.onMutation("remove", (event) => {
      if (event.path !== row) return;
      stop();
      remove(row);
    });
    drafts.set(row, stop);
    tree.startRenaming(row, { removeIfCanceled: true });
  },
});

/**
 * Moves a row to PMT's trash once the user agrees, and offers to bring it back. A mod's root leaves the workspace. A
 * row made a moment ago and left unnamed goes without a question.
 */
const { mutate: remove } = useMutation({
  mutation: async (row: string) => {
    const path = disks.get(row) ?? "";
    if (!drafts.has(row)) return trashFile(path, path, isRoot(row) ? " The mod also leaves this workspace." : "");
    await service.Delete(path);
    return true;
  },
  onSuccess: (deleted, row) => {
    drafts.get(row)?.();
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
  "file.copyPath": { writes: false, run: (row) => void Clipboard.SetText(disks.get(row) ?? "") },
  "file.reveal": { writes: false, run: (row) => void app.ShowFolder(disks.get(row) ?? "") },
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

/** Builds the menu of a row. It is built when the menu opens, so it reads the selection as it is then. */
function buildMenu(row: string): ContextMenuItem[][] {
  const place = places.get(row);
  const file = row.endsWith("/") ? undefined : place;
  const dir = disks.get(row) ?? "";
  const holder = windows.holderOf({ folder: dir });
  const open: ContextMenuItem[] = file
    ? [
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
            windows.popOut({ tab: fileTab(file.key) });
          },
        },
        ...(VIEWS[file.format].views.includes("visual")
          ? [
              {
                label: "Open as Visual",
                icon: "i-lucide-eye",
                onSelect: () => {
                  editor.show(file);
                  editor.setView(fileTab(file.key), "visual");
                },
              },
            ]
          : []),
      ]
    : holder
      ? [{ label: "Close the window", icon: "i-lucide-panel-top-close", onSelect: () => windows.closeOther(holder) }]
      : [
          {
            label: "Open in a new window",
            icon: "i-lucide-app-window",
            onSelect: () => windows.popOut({ folder: dir }),
          },
        ];
  const commands = COMMANDS.flatMap((command) => {
    const action = ACTIONS[command.id];
    if (!action || (action.writes && !isWritable(row)) || (action.shows && !action.shows(row))) return [];
    const disabled = command.id === "file.rename" && isRoot(row);
    const label = command.id === "file.delete" && isRoot(row) ? "Delete the mod's folder" : command.label;
    return [{ label, icon: command.icon, kbds: command.kbds, disabled, onSelect: () => action.run(row) }];
  });
  const coat: ContextMenuItem[] =
    !file && place?.database.startsWith("coat_of_arms") && isWritable(row)
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
  const comparisons = file
    ? [
        { label: "Select for compare", onSelect: () => (compareWith.value = file) },
        {
          label: "Compare with selected",
          disabled: !compareWith.value || compareWith.value.key === file.key,
          onSelect: () => compareWith.value && compare(compareWith.value, file),
        },
        ...COMMANDS.filter((command) => command.id === "compare.selected" && command.when()).map((command) => ({
          label: "Compare the two selected",
          onSelect: command.onSelect,
        })),
      ]
    : [];
  return [open, coat, init, commands, comparisons].filter((group) => group.length);
}

/** Finds the row under a right click and builds its menu. Off a row there is no menu. */
function pick(event: MouseEvent): void {
  const element = event
    .composedPath()
    .find((one): one is HTMLElement => one instanceof HTMLElement && one.dataset.itemPath !== undefined);
  menu.value = element?.dataset.itemPath ? buildMenu(element.dataset.itemPath) : [];
  if (element) return;
  event.preventDefault();
  event.stopPropagation();
}

/** Starts what the menu chose to do in a row, which needs the keyboard the menu held until it closed. */
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
