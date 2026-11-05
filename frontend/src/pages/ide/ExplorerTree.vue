<!--
  The file explorer: the roots in the workspace's load order and their folders, with rename, drag and drop, git marks,
  a filter box and a context menu.
-->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation } from "@pinia/colada";
import { useDebounceFn, useEventBus, watchDebounced } from "@vueuse/core";
import { Clipboard, Events } from "@wailsio/runtime";
import {
  FileTree,
  preparePresortedFileTreeInput,
  type FileTreeDensityKeyword,
  type FileTreeDirectoryHandle,
  type FileTreeRowDecoration,
  type GitStatus,
  type GitStatusEntry,
} from "@pierre/trees";
import type { ContextMenuItem } from "@nuxt/ui";

import * as app from "@services/app/service";
import * as lsp from "@services/lsp/service";
import * as service from "@services/workspace/service";
import type { FileChange, Place } from "@services/workspace/models";

import {
  canCopyToMod,
  canSaveAsDds,
  COMMANDS,
  compare,
  copyToMod,
  findCommand,
  removeMod,
  trashFile,
} from "../../app/commands";
import { fileSearchQuery, folderQuery, gitStatusQuery, loadQuery, placeQuery } from "../../queries";
import { tabId, useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSettingsStore } from "../../stores/settings";
import { COMMAND_BUS, useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import { findIcon, listIcons } from "./editor/tabs";
import SaveDdsModal from "./SaveDdsModal.vue";

const { roots } = defineProps<{ roots: Place[] }>();
const emit = defineEmits<{ open: [place: Place, preview: boolean] }>();
// Hiding the filter box clears the filter, so a hidden box hides no row.
const filtering = defineModel<boolean>("filtering", { default: false });
defineExpose({ collapseAll, reveal });

/** GIT_MARKS maps each git status to the mark the tree shows. */
const GIT_MARKS: Record<string, GitStatus> = {
  modified: "modified",
  added: "added",
  deleted: "deleted",
  renamed: "renamed",
  untracked: "untracked",
  conflict: "modified",
};
/** SYMBOLS maps each file icon to its symbol id in the tree's sprite. */
const SYMBOLS = new Map(listIcons().map((icon, index) => [icon, `pmt-${index}`]));
/** PACKAGE_PATHS and LOCK_PATHS draw the package of an external mod and the lock of a read-only root. */
const PACKAGE_PATHS = `<path d="m7.5 4.27 9 5.15"/><path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>`;
const LOCK_PATHS = `<rect width="18" height="11" x="3" y="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`;
/** STROKE is how the root marks are drawn: lines in the row's text color. */
const STROKE = `fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"`;
/** EXTERNAL and EXTERNAL_READ_ONLY are the root marks of an external mod, and of Steam's copy, which is also locked. */
const EXTERNAL = { name: "pmt-external", width: 16, height: 16, viewBox: "0 0 16 16" };
const EXTERNAL_READ_ONLY = { name: "pmt-external-read-only", width: 32, height: 16, viewBox: "0 0 32 16" };
/** SPRITE is the SVG sheet of the file icons, one symbol for each, and of the root marks. */
const SPRITE = `<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position: absolute">${[...SYMBOLS]
  .map(
    ([icon, id]) =>
      `<symbol id="${id}" viewBox="0 0 800 800"><image href="${icon}" width="800" height="800"/></symbol>`,
  )
  .join(
    "",
  )}<symbol id="${EXTERNAL.name}" viewBox="0 0 24 24" ${STROKE}>${PACKAGE_PATHS}</symbol><symbol id="${EXTERNAL_READ_ONLY.name}" viewBox="0 0 48 24" ${STROKE}>${PACKAGE_PATHS}<g transform="translate(24 0)">${LOCK_PATHS}</g></symbol></svg>`;
/** FILTER_HITS is how many files the filter asks the file search for. */
const FILTER_HITS = 500;
/** MENU_CONTENT is the context menu's content options, one object so the menu is not drawn again on each render. */
const MENU_CONTENT = { onCloseAutoFocus: resume };

/**
 * A Move is one rename, move or delete done in the explorer, with the path on disk before it and the path after it.
 * A delete's path after it is in PMT's trash, and `isTrash` marks it.
 */
type Move = { oldPath: string; newPath: string; isTrash?: true };

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

// Every row of the tree is a path with its root's label first and a slash after each folder.
const places = new Map<string, Place>(); // a row's place: each root's, and each row a folder listing gave
const members = new Map<string, Set<string>>(); // the rows of each listed folder row
// Each new row's path on disk, and the function that stops watching for its rename to be cancelled.
const drafts = new Map<string, { path: string; stop: () => void }>();
const icons: Record<string, { name: string; viewBox: string }> = {}; // a file's icon by its name
// The rows git ignores are dimmed; a git status would count them as changes in every folder above.
const ignoredSheet = new CSSStyleSheet();
const undrawableSheet = new CSSStyleSheet();
const history = { done: [] as Move[], undone: [] as Move[] };
// What a menu item chose to do, run once the menu has closed and released the keyboard.
let deferred: (() => void) | undefined;

const labels = computed(() => {
  const counts = new Map<string, number>();
  return roots.map((root) => {
    // The tree reads a "/" as a folder, so a mod's name writes it as a look-alike slash.
    const label = workspace.originName(root.origin).replaceAll("/", "∕");
    const count = (counts.get(label) ?? 0) + 1;
    counts.set(label, count);
    return count > 1 ? `${label} (${count})` : label;
  });
});
const writable = computed(() => roots.some((root) => !root.readOnly));
// Changes whenever the tree must be built again.
const shape = computed(() =>
  [
    roots.map((root) => root.path).join("|"),
    labels.value.join("|"),
    writable.value,
    settings.isOn("ide.stickyFolders"),
    settings.isOn("ide.rootColors"),
    settings.value("ide.density"),
    settings.isOn("ide.hideBinaries"),
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
  // Cleaning up leaves the old tree's element in the host.
  view.value?.getFileTreeContainer()?.remove();
  view.value?.cleanUp();
  [places, members, drafts].forEach((map) => map.clear());
  Object.keys(icons).forEach((name) => delete icons[name]);
  if (!host.value || !roots.length) return;
  roots.forEach((root, index) => places.set(`${labels.value[index]}/`, root));
  const tree = new FileTree({
    // The roots stay in the workspace's load order; the tree sorts what each root holds.
    preparedInput: preparePresortedFileTreeInput(labels.value.map((label) => `${label}/`)),
    initialExpansion: "closed",
    icons: { set: "none", spriteSheet: SPRITE },
    density: settings.value("ide.density") as FileTreeDensityKeyword,
    stickyFolders: settings.isOn("ide.stickyFolders"),
    unsafeCSS: buildStyles(),
    renderRowDecoration: ({ item }) => (isRoot(item.path) ? markRoot(roots[rootIndex(item.path)]) : null),
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
      canDrop: ({ target }) => target.kind === "directory" && isWritable(target.directoryPath ?? ""),
      onDropComplete: ({ draggedPaths, target }) => {
        for (const path of draggedPaths)
          rename({
            source: path,
            destination: `${target.directoryPath}${takeName(path)}${path.endsWith("/") ? "/" : ""}`,
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

/** buildStyles returns the CSS of the root rows: a bar in the origin's color, and the name too if root colors on. */
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

/** markRoot returns the mark at the end of a root's row: a package for an external mod, a lock for read-only files. */
function markRoot(root: Place | undefined): FileTreeRowDecoration | null {
  if (!root) return null;
  if (!workspace.isExternal(root.origin)) {
    return root.readOnly ? { icon: "file-tree-icon-lock", title: "Read only" } : null;
  }
  return root.readOnly
    ? { icon: EXTERNAL_READ_ONLY, title: "External mod, read only: Steam's copy of a Workshop mod" }
    : { icon: EXTERNAL, title: "External mod" };
}

/** rootIndex returns the index of the root a row is under, or -1 for a row under none. */
const rootIndex = (row: string) => labels.value.indexOf(row.slice(0, row.indexOf("/")));
/** isRoot reports whether a row is a root's own row. */
const isRoot = (row: string) => row.indexOf("/") === row.length - 1;
/** isWritable reports whether a row is under a root that can be changed. */
const isWritable = (row: string) => roots[rootIndex(row)]?.readOnly === false;

/** rowOf returns the row of a path the tree hands out. The tree leaves off the slash a folder's row ends in. */
function rowOf(path: string, isFolder: boolean): string {
  return isFolder && !path.endsWith("/") ? `${path}/` : path;
}

/** findPath finds a row's path on disk. It is "" for a row the tree lacks. */
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

/** takeName returns the last part of a row, without the slash a folder's row ends in. */
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

/** load lists a folder row and puts its rows in the tree. A folder gone from disk changes nothing. */
async function load(row: string): Promise<void> {
  let children: Place[];
  try {
    children = (await loadQuery(folderQuery(findPath(row)))) ?? [];
  } catch {
    return;
  }
  apply(row, children);
}

/** apply makes a folder row hold the listed rows, adding the new ones and removing those that are gone. */
function apply(row: string, children: Place[]): void {
  const listing = new Set<string>();
  for (const child of children) {
    const path = `${row}${child.name}${child.isDir ? "/" : ""}`;
    listing.add(path);
    places.set(path, child);
    if (!child.isDir) icons[child.name] = { name: SYMBOLS.get(findIcon(child)) ?? "", viewBox: "0 0 800 800" };
  }
  const tree = view.value;
  const removed = [...(members.get(row) ?? [])].filter((path) => !listing.has(path) && tree?.getItem(path));
  const added = [...listing].filter((path) => !tree?.getItem(path));
  tree?.batch([
    ...removed.map((path) => ({ type: "remove" as const, path, recursive: true })),
    ...added.map((path) => ({ type: "add" as const, path })),
  ]);
  tree?.setIcons({ set: "none", spriteSheet: SPRITE, byFileName: icons });
  members.set(row, listing);
  dimUndrawable();
}

/** forget drops a row and the rows under it from places and members, after the tree moved or removed them. */
function forget(row: string): void {
  for (const map of [places, members]) for (const path of map.keys()) if (path.startsWith(row)) map.delete(path);
}

const pending = new Set<string>();
const refresh = useDebounceFn(() => {
  const rows = [...pending];
  pending.clear();
  rows.forEach((row) => void load(row));
}, 150);

/** changed queues the listed folders that hold a changed path to be listed again. */
function changed(change: FileChange): void {
  for (const row of members.keys()) {
    const dir = findPath(row);
    if (dir && change.path.startsWith(dir)) pending.add(row);
  }
  void refresh();
}

/** markGit marks the rows git reports as changed and dims the ones it ignores, for each repository the mods are in. */
async function markGit(): Promise<void> {
  const tree = view.value;
  if (!tree) return;
  const marks: GitStatusEntry[] = [];
  const dimmed: string[] = [];
  for (const { root: dir } of git.repositories) {
    // git:changed has already marked the status stale, and loadQuery shares a fetch under way.
    const data = await loadQuery(gitStatusQuery(dir));
    for (const change of data?.changes ?? []) {
      const row = buildRow(change.place);
      if (row) marks.push({ path: row, status: GIT_MARKS[change.status] ?? "modified" });
    }
    for (const place of data?.ignored ?? []) {
      const row = buildRow(place);
      if (!row) continue;
      // A folder stands for every row under it.
      dimmed.push(buildSelector(row + (place.isDir ? "/" : ""), place.isDir));
    }
  }
  tree.setGitStatus(marks);
  dim(ignoredSheet, dimmed);
}

/** dim sets a sheet to dim the rows the given selectors match. No selectors clear the sheet. */
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

/** buildRow returns the row of a place, with no trailing slash for a folder. It is "" if no root holds the place. */
function buildRow(place: Place): string {
  const index = roots.findIndex(
    (root) =>
      root.origin === place.origin && (!root.rel || place.rel === root.rel || place.rel.startsWith(`${root.rel}/`)),
  );
  const root = roots[index];
  return root ? `${labels.value[index]}/${place.rel.slice(root.rel ? root.rel.length + 1 : 0)}` : "";
}

/** select records the selected files with the editor store, and opens a file selected alone as a preview tab. */
function select(paths: readonly string[]): void {
  const files = paths.flatMap((path) => (path.endsWith("/") ? [] : (places.get(path) ?? [])));
  editor.selectFiles(files);
  const [place] = files;
  if (paths.length === 1 && place && place.key !== editor.activeFile()?.key) emit("open", place, true);
}

/** pin opens the selected file as a pinned tab instead of a preview. */
function pin(): void {
  const [only] = view.value?.getSelectedPaths() ?? [];
  const place = only && !only.endsWith("/") ? places.get(only) : undefined;
  if (place) emit("open", place, false);
}

/** follow reveals the active file in the tree, when the explorer follows the active file. */
function follow(): void {
  if (settings.isOn("ide.followFile")) void reveal();
}

/** reveal opens the folders down to the active file and selects it. A file with no row changes nothing. */
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
 * loadFolders lists the folders from the root down to a file's folder and returns their rows, the root's first. It
 * returns none for a file under none of the roots or under a folder gone from disk.
 */
async function loadFolders(place: Place): Promise<string[]> {
  const row = buildRow(place);
  if (!row) return [];
  const parts = row.split("/").slice(0, -1);
  const folders = parts.map((_, index) => `${parts.slice(0, index + 1).join("/")}/`);
  for (const folder of folders) {
    // A folder has a path once its parent's listing has included it.
    if (!findPath(folder)) return [];
    if (!members.has(folder)) await load(folder);
  }
  return folders;
}

/**
 * filterRows shows only the rows whose path contains the given query, in folders not yet opened too. An empty query
 * shows every row.
 */
async function filterRows(query: string): Promise<void> {
  const text = query.trim();
  const hits = text ? await loadQuery(fileSearchQuery({ query: text, limit: FILTER_HITS })) : [];
  await Promise.all((hits ?? []).map(loadFolders));
  // A query typed since this one began is filtered by its own call.
  if (filter.value.trim() === text) view.value?.setSearch(text || null);
}

/** collapseAll folds every folder of the tree. */
function collapseAll(): void {
  for (const row of listRows()) findHandle(row)?.collapse();
}

// The tree has moved the row already; a move Go refuses puts it back.
const { mutate: rename } = useMutation({
  mutation: async ({ source, destination }: { source: string; destination: string }): Promise<Move> => {
    const move = {
      oldPath: findPath(source),
      newPath: `${findPath(findParent(destination))}/${takeName(destination)}`,
    };
    await editor.rename(move.oldPath, move.newPath);
    return move;
  },
  onSuccess: (move, { source }) => {
    drafts.get(source)?.stop();
    drafts.delete(source);
    record(move);
    forget(source);
  },
  onError: (_error, { source, destination }) => view.value?.move(destination, source),
});

/** record adds a move to the history and drops the moves undone before it. */
function record(move: Move): void {
  history.done.push(move);
  history.undone = [];
}

// Undoing a delete brings the file back from the trash, and redoing it deletes it again. The watcher updates the tree.
const { mutate: replay } = useMutation({
  mutation: async (isUndo: boolean) => {
    const [stack, opposite] = isUndo ? [history.done, history.undone] : [history.undone, history.done];
    const move = stack.pop();
    if (!move) return;
    if (!move.isTrash) {
      await (isUndo ? editor.rename(move.newPath, move.oldPath) : editor.rename(move.oldPath, move.newPath));
    } else if (isUndo) {
      await service.Restore(move.newPath, move.oldPath);
    } else {
      move.newPath = await service.Delete(move.oldPath);
    }
    opposite.push(move);
  },
});

/** step moves through the explorer's history: back for Ctrl Z, forward for Ctrl Y or Ctrl Shift Z. */
function step(event: KeyboardEvent): void {
  const key = event.key.toLowerCase();
  const isTyping = event
    .composedPath()
    .some((node) => node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement);
  if (!event.ctrlKey || isTyping || (key !== "z" && key !== "y")) return;
  event.preventDefault();
  replay(key === "z" && !event.shiftKey);
}

/** create makes a new file or folder in a folder row and asks for its name in the tree. */
const { mutate: create } = useMutation({
  mutation: async ({ folder, dir }: { folder: string; dir: boolean }) => {
    if (!members.has(folder)) await load(folder);
    const draft = dir ? undefined : await lsp.DraftFile(findPath(folder));
    const occupied = (label: string) =>
      view.value?.getItem(`${folder}${label}`) ?? view.value?.getItem(`${folder}${label}/`);
    let label = draft?.name ?? "new_folder";
    if (dir) for (let count = 2; occupied(label); count++) label = `new_folder_${count}`;
    const row = `${folder}${label}${dir ? "/" : ""}`;
    const path = `${findPath(folder)}/${label}`;
    await service.Create(path, dir);
    if (draft?.text.text) await service.Write(path, draft.text);
    return { row, path };
  },
  onSuccess: ({ row, path }) => {
    const tree = view.value;
    if (!tree) return;
    tree.add(row);
    // A name left empty or cancelled takes the new row away, and the file with it.
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
 * remove moves a row to PMT's trash once the user agrees, and Ctrl Z brings it back. A new row that never got a name
 * is deleted unasked, and a mod's own folder is not in the history, since the mod left the workspace with it.
 */
const { mutate: remove } = useMutation({
  mutation: async (row: string) => {
    const path = findPath(row);
    if (drafts.has(row)) return service.Delete(path);
    return trashFile(path, path, isRoot(row) ? " The mod also leaves this workspace." : "");
  },
  onSuccess: (trashed, row) => {
    if (trashed && !drafts.has(row) && !isRoot(row))
      record({ oldPath: findPath(row), newPath: trashed, isTrash: true });
    drafts.get(row)?.stop();
    drafts.delete(row);
    if (!trashed) return;
    if (view.value?.getItem(row)) view.value.remove(row, { recursive: true });
    forget(row);
  },
});

const { mutate: leaveWorkspace } = useMutation({
  mutation: (root: Place) => removeMod(workspace.activeId(), root.origin, workspace.originName(root.origin)),
});

// What each command of the command table does in the context menu. `writes` marks one that changes files, `shows`
// limits one to some rows, and `label` names it otherwise on some.
const ACTIONS: Record<
  string,
  {
    writes: boolean;
    shows?: (row: string) => boolean;
    label?: (row: string) => string | undefined;
    run: (row: string) => void;
  }
> = {
  "file.new": { writes: true, run: (row) => (deferred = () => create({ folder: findFolder(row), dir: false })) },
  "file.newFolder": { writes: true, run: (row) => (deferred = () => create({ folder: findFolder(row), dir: true })) },
  "file.rename": { writes: true, run: (row) => (deferred = () => view.value?.startRenaming(row)) },
  "file.delete": {
    writes: true,
    label: (row) => (isRoot(row) ? "Delete the mod's folder" : undefined),
    run: (row) => remove(row),
  },
  "file.copyPath": { writes: false, run: (row) => void Clipboard.SetText(findPath(row)) },
  "file.reveal": { writes: false, run: (row) => void app.ShowFolder(findPath(row)) },
  "file.copyToMod": { writes: false, shows: (row) => canCopyToMod(places.get(row)), run: onPlace(copyToMod) },
  "image.saveDds": {
    writes: true,
    shows: (row) => !isRoot(row) && (row.endsWith("/") ? holdsPictures(row) : canSaveAsDds(places.get(row))),
    label: (row) => (row.endsWith("/") ? "Save its pictures as DDS…" : undefined),
    run: onPlace((place) => void saveDds.open({ place })),
  },
};

/**
 * holdsPictures reports whether a folder row directly holds a picture Save as DDS takes. A folder not yet listed holds
 * none, since the explorer does not know what is in it.
 */
function holdsPictures(row: string): boolean {
  return [...(members.get(row) ?? [])].some((child) => canSaveAsDds(places.get(child)));
}

/** onPlace returns a menu action that runs the given function on the clicked row's place. */
function onPlace(act: (place: Place) => void): (row: string) => void {
  return (row) => {
    const place = places.get(row);
    if (place) act(place);
  };
}

/** buildMenu returns the context menu of a row. */
function buildMenu(row: string): ContextMenuItem[][] {
  const place = places.get(row);
  const file = row.endsWith("/") ? undefined : place;
  const dir = findPath(row);
  const commands = COMMANDS.flatMap((command) => {
    const action = ACTIONS[command.id];
    if (!action || (action.writes && !isWritable(row)) || (action.shows && !action.shows(row))) return [];
    const disabled = command.id === "file.rename" && isRoot(row);
    const label = action.label?.(row) ?? command.label;
    return [{ label, icon: command.icon, kbds: command.kbds, disabled, onSelect: () => action.run(row) }];
  });
  const readme: ContextMenuItem[] = place?.readme
    ? [
        {
          label: "Open the game's readme/info",
          icon: "i-lucide-book-open",
          onSelect: () => void loadQuery(placeQuery(place.readme)).then((doc) => doc && emit("open", doc, false)),
        },
      ]
    : [];
  const coat: ContextMenuItem[] =
    !file && place?.drawable && isWritable(row)
      ? [{ label: "New coat of arms", icon: "i-lucide-shield-plus", onSelect: () => commandBus.emit("draw.newCoat") }]
      : [];
  const init: ContextMenuItem[] =
    git.isInstalled && isRoot(row) && isWritable(row) && !git.findRoot(roots[rootIndex(row)])
      ? [
          {
            label: "Initialize a git repository",
            icon: "i-lucide-git-branch-plus",
            onSelect: () => git.initialize(dir),
          },
        ]
      : [];
  const leave: ContextMenuItem[] =
    isRoot(row) && place && place.origin !== VANILLA
      ? [
          {
            label: "Remove from workspace",
            icon: "i-lucide-circle-minus",
            onSelect: () => leaveWorkspace(place),
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
  return [buildOpenItems(file, dir), readme, coat, init, commands, leave, comparisons].filter((group) => group.length);
}

/** buildOpenItems builds the menu items that open a row elsewhere: beside the editor, in a window or in Visual. */
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

/** findRowElement finds the element of the row under a mouse event. It is undefined off a row. */
function findRowElement(event: MouseEvent): HTMLElement | undefined {
  return event
    .composedPath()
    .find((node): node is HTMLElement => node instanceof HTMLElement && node.dataset.itemPath !== undefined);
}

/** pick finds the row under a right click and builds its menu. A click off every row opens no menu. */
function pick(event: MouseEvent): void {
  const element = findRowElement(event);
  menu.value = element?.dataset.itemPath ? buildMenu(element.dataset.itemPath) : [];
  if (element) return;
  event.preventDefault();
  event.stopPropagation();
}

/** resume runs the action the menu deferred, now that the menu has closed and released the keyboard. */
function resume(event: Event): void {
  if (!deferred) return;
  event.preventDefault();
  deferred();
  deferred = undefined;
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="filtering" class="shrink-0 px-2 pt-1 pb-1.5" @keydown.esc="filtering = false">
      <UInput v-model="filter" placeholder="Filter files" icon="i-lucide-search" size="xs" autofocus class="w-full" />
    </div>
    <!-- The menu's trigger takes its child's ref, so the tree's element sits inside the wrapper div. -->
    <UContextMenu :items="menu" :content="MENU_CONTENT">
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
