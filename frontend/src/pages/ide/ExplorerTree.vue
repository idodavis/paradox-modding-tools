<script setup lang="ts">
/**
 * A section of the explorer: its roots, each a mod's with its color bar or the game's
 * with a lock, their folders listed when opened. Files open in the editor; a writable section creates, renames,
 * moves and deletes in place. It follows the open file, and Go's file watcher (fs:changed).
 */
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch, watchEffect } from "vue";
import { useMutation } from "@pinia/colada";
import { Clipboard, Events } from "@wailsio/runtime";
import { FileTree, type FileTreeDirectoryHandle, type GitStatus, type GitStatusEntry } from "@pierre/trees";
import type { ContextMenuItem } from "@nuxt/ui";
import type { IdeRoot } from "@services/models";
import {
  CreateDir,
  DeletePath,
  ListDirectory,
  RenamePath,
  StatPath,
  TrashPath,
  WriteText,
} from "@services/fileservice";
import { OpenFolder, RemoveWorkspaceMod } from "@services/workspaceservice";
import { pathKey, useIdeRoots } from "../../composables/useIdeRoots";
import { viewsOf } from "../../editor/tabs";
import { BINARY_EXTENSIONS, TREE_ICONS } from "./fileKinds";
import { Close, Show } from "@services/windowservice";
import { useWindowsStore } from "../../stores/windows";
import { GIT_LETTERS, useGitStore } from "../../stores/git";
import { useDrawStore } from "../../stores/draw";
import { useEditorStore } from "../../stores/editor";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import ConfirmModal from "../../ui/ConfirmModal.vue";

const { workspaceId, roots, hideBinaries } = defineProps<{
  workspaceId: string;
  roots: IdeRoot[];
  hideBinaries: boolean;
}>();
const emit = defineEmits<{ compare: [old: string, now: string] }>();

const editor = useEditorStore();
const windows = useWindowsStore();
const git = useGitStore();
const settings = useSettingsStore();
const store = useWorkspaceStore();
const { locate, under } = useIdeRoots(() => workspaceId);
const { mutate: saveWorkspace } = useMutation({ mutation: (save: () => Promise<unknown>) => save() });

const writable = computed(() => roots.some((root) => !root.readOnly));
// a root's name in the tree is its label, made unique where two share one
const names = computed(() => {
  const counts = new Map<string, number>();
  return roots.map((root) => {
    const count = (counts.get(root.label) ?? 0) + 1;
    counts.set(root.label, count);
    return count > 1 ? `${root.label} (${count})` : root.label;
  });
});

/** A tree path's file on disk: its root's path, then the rest. */
function diskPath(treePath: string): string {
  const slash = treePath.indexOf("/");
  const root = roots[names.value.indexOf(treePath.slice(0, slash))];
  const rel = treePath.slice(slash + 1).replace(/\/$/, "");
  return root ? (rel ? under(root, rel) : root.path) : "";
}

/** The root a root's row is, by its tree path ("<name>/"). */
const rootOf = (path: string): IdeRoot | undefined => roots[names.value.indexOf(path.slice(0, -1))];

/** A file on disk's tree path, where it is under one of this section's roots. */
function treePath(path: string, dir = false): string | undefined {
  const located = locate(path, roots);
  return located && `${names.value[roots.indexOf(located.root)]}/${located.rel}${dir ? "/" : ""}`;
}

const host = useTemplateRef("host");
const tree = shallowRef<FileTree>();
// the folders read so far, and every folder the tree has
const loadedFolders = new Set<string>();
const folders = new Set<string>();

async function list(dir: string): Promise<void> {
  if (loadedFolders.has(dir)) return;
  loadedFolders.add(dir);
  const entries = (await ListDirectory(diskPath(dir), !hideBinaries)) ?? [];
  const visibleEntries = entries.filter((entry) => !hideBinaries || !BINARY_EXTENSIONS.test(entry.name));
  const paths = visibleEntries.map((entry) => `${dir}${entry.name}${entry.isDir ? "/" : ""}`);
  paths.filter((path) => path.endsWith("/")).forEach((path) => folders.add(path));
  tree.value?.batch(paths.filter((path) => !tree.value?.getItem(path)).map((path) => ({ type: "add" as const, path })));
}

function mount(): void {
  tree.value?.cleanUp();
  loadedFolders.clear();
  folders.clear();
  if (!host.value || !roots.length) return;
  // a root's row, and its copy that sticks at the top, at 600 with its origin's bar on its start, in its origin's
  // color where the explorer colors the roots (the game's too)
  const rootRows = roots
    .map((root, i) => {
      const color = store.originColor(root.origin ?? "");
      const text = settings.on("ide.rootColors") ? color : "var(--fg-1)";
      return `[data-item-path="${CSS.escape(`${names.value[i]}/`)}"] { font-weight: 600; color: ${text}; border-radius: 0; box-shadow: inset 3px 0 0 ${color}; }`;
    })
    .join("\n");
  const paths = names.value.map((name) => `${name}/`);
  paths.forEach((path) => folders.add(path));
  const fileTree = new FileTree({
    paths,
    initialExpansion: "closed",
    density: settings.treeDensity,
    stickyFolders: settings.on("ide.stickyFolders"),
    icons: TREE_ICONS,
    gitStatus: gitStatus.value,
    unsafeCSS: rootRows,
    // a read-only root says so with a lock; a folder in a window of its own with that window's mark
    renderRowDecoration: ({ item }) => {
      if (!writable.value && paths.includes(`${item.path.replace(/\/$/, "")}/`))
        return { icon: "file-tree-icon-lock", title: "Read only" };
      return windowOf(item.path)
        ? { icon: { name: "pmt-window", viewBox: "0 0 24 24" }, title: "In its own window: show it" }
        : null;
    },
    onSelectionChange: (selected) => {
      editor.selected = selected.filter((path) => !path.endsWith("/")).map(diskPath);
      // the open file selected again (the tree following it) opens nothing: from another page it would go to the IDE
      const [only] = selected;
      if (selected.length === 1 && only && !only.endsWith("/") && pathKey(diskPath(only)) !== pathKey(editor.activeTab))
        editor.open(diskPath(only), undefined, true);
    },
    renaming: writable.value && {
      canRename: ({ path }) => path.indexOf("/") < path.length - 1,
      onRename: ({ sourcePath, destinationPath }) => rename({ path: sourcePath, next: destinationPath }),
    },
    dragAndDrop: writable.value && {
      canDrop: ({ target }) => target.kind === "directory",
      onDropComplete: ({ draggedPaths, target }) => {
        for (const path of draggedPaths)
          rename({ path, next: `${target.directoryPath}${path.slice(path.replace(/\/$/, "").lastIndexOf("/") + 1)}` });
      },
    },
  });
  // a folder's children are read when it first opens
  fileTree.subscribe(() => {
    for (const dir of folders) if (!loadedFolders.has(dir) && isOpen(fileTree, dir)) void list(dir);
  });
  fileTree.render({ containerWrapper: host.value });
  const shadow = host.value.querySelector("*")?.shadowRoot;
  if (shadow) shadow.adoptedStyleSheets = [...shadow.adoptedStyleSheets, ignoredSheet];
  tree.value = fileTree;
  void follow();
}
/** A folder's handle in a tree; none for a file or a path it lacks. */
function folder(fileTree: FileTree | undefined, dir: string): FileTreeDirectoryHandle | undefined {
  const item = fileTree?.getItem(dir);
  return item && "expand" in item ? item : undefined;
}
function isOpen(fileTree: FileTree, dir: string): boolean {
  return !!folder(fileTree, dir)?.isExpanded();
}
onMounted(mount);
// each source on its own, so the tree is built again only when one of them changes, not whenever any setting is saved
watch(
  [
    () => roots,
    () => hideBinaries,
    () => settings.treeDensity,
    () => settings.on("ide.stickyFolders"),
    () => settings.on("ide.rootColors"),
    () => names.value.join("|"),
  ],
  mount,
);
onBeforeUnmount(() => tree.value?.cleanUp());

// each repository's changes on its files and folders: staged, then unstaged, a file added kept as added
const gitStatus = computed<GitStatusEntry[]>(() => {
  const marked = new Map<string, GitStatus>();
  for (const status of Object.values(git.statuses ?? {})) {
    if (!status) continue;
    for (const change of [...(status.staged ?? []), ...(status.changes ?? []), ...(status.conflicts ?? [])]) {
      const path = treePath(change.path);
      if (path && marked.get(path) !== "added") marked.set(path, GIT_LETTERS[change.letter]?.status ?? "modified");
    }
  }
  return [...marked].map(([path, status]) => ({ path, status }));
});
watch(gitStatus, (entries) => tree.value?.setGitStatus(entries));

// what git ignores dimmed, a folder with all in it, by a sheet of its own in the tree's shadow root: as a git status,
// an ignored path would mark every folder above it as changed
const ignoredSheet = new CSSStyleSheet();
const quoted = (path: string) => `"${path.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
watchEffect(() => {
  const rows = Object.values(git.statuses ?? {}).flatMap((status) =>
    (status?.ignored ?? []).flatMap((ignored) => {
      const dir = /[\\/]$/.test(ignored);
      const path = treePath(ignored.replace(/[\\/]$/, ""), dir);
      return path ? [`[data-item-path${dir ? "^" : ""}=${quoted(path)}]`] : [];
    }),
  );
  ignoredSheet.replaceSync(rows.length ? `${rows.join(",")} { color: var(--trees-status-ignored-override); }` : "");
});

/** Open the folders down to the open file and select it, while the explorer follows the open file; a diff tab is
 * not followed, since selecting a file opens it. */
async function follow(): Promise<void> {
  const path = editor.activeTab;
  const target = path && settings.on("ide.followFile") ? treePath(path) : undefined;
  const fileTree = tree.value;
  if (!target || !fileTree) return;
  const parts = target.split("/");
  for (let i = 1; i < parts.length; i++) {
    const dir = `${parts.slice(0, i).join("/")}/`;
    await list(dir);
    const item = folder(fileTree, dir);
    if (item && !item.isExpanded()) item.expand();
  }
  if (fileTree.getSelectedPaths()[0] !== target) fileTree.getItem(target)?.select();
  fileTree.scrollToPath(target, { focus: false, offset: "nearest" });
}
watch(
  () => editor.activeTab,
  () => void follow(),
);

/** Collapse every folder. */
function collapseAll(): void {
  for (const dir of folders) folder(tree.value, dir)?.collapse();
}
defineExpose({ collapseAll });

// Go's file watcher: a path gone leaves the tree, a new one in a folder already read joins it
async function onDiskChange(data: { path: string; deleted: boolean }): Promise<void> {
  const fileTree = tree.value;
  const treeItemPath = treePath(data.path);
  if (!fileTree || !treeItemPath) return;
  if (data.deleted) {
    if (fileTree.getItem(treeItemPath)) fileTree.remove(treeItemPath, { recursive: true });
    else if (fileTree.getItem(`${treeItemPath}/`)) fileTree.remove(`${treeItemPath}/`, { recursive: true });
    return;
  }
  const parent = treeItemPath.slice(0, treeItemPath.lastIndexOf("/") + 1);
  if (!loadedFolders.has(parent) || fileTree.getItem(treeItemPath) || fileTree.getItem(`${treeItemPath}/`)) return;
  const stat = await StatPath(data.path);
  if (!stat.exists || (hideBinaries && BINARY_EXTENSIONS.test(treeItemPath.slice(parent.length)))) return;
  const path = stat.isDir ? `${treeItemPath}/` : treeItemPath;
  if (stat.isDir) folders.add(path);
  if (!fileTree.getItem(path)) fileTree.add(path);
}
const stop = Events.On("fs:changed", ({ data }) => void onDiskChange(data));
onBeforeUnmount(stop);

// the tree has moved the path already; a failed rename on disk moves it back
const { mutate: rename } = useMutation({
  mutation: ({ path, next }: { path: string; next: string }) => RenamePath(diskPath(path), diskPath(next)),
  onSuccess: (_data, { path, next }) => {
    const move = { from: diskPath(path), to: diskPath(next) };
    editor.renameTabs(move.from, move.to);
    editor.recordMove(move);
  },
  onError: (_error, { path, next }) => tree.value?.move(next, path),
});

// a name cancelled in the tree drops its row: the file and its tab go too (a path already gone deletes harmlessly)
const { mutate: deleteCancelled } = useMutation({
  mutation: (path: string) => DeletePath(diskPath(path)),
  onSuccess: (_data, path) => editor.closeTabs(diskPath(path)),
});

/** A new file or folder in a folder, named in place; cancelled (Escape, or no name), it is deleted again. */
const { mutate: create } = useMutation({
  mutation: async ({ dir, isFolder }: { dir: string; isFolder: boolean }) => {
    await list(dir);
    const occupied = (name: string) => tree.value?.getItem(`${dir}${name}`) || tree.value?.getItem(`${dir}${name}/`);
    let name = isFolder ? "new_folder" : "new_file.txt";
    for (let n = 2; occupied(name); n++) name = isFolder ? `new_folder_${n}` : `new_file_${n}.txt`;
    const path = `${dir}${name}${isFolder ? "/" : ""}`;
    await (isFolder ? CreateDir(diskPath(path)) : WriteText(diskPath(path), "", false, false));
    if (isFolder) folders.add(path);
    const fileTree = tree.value;
    if (!fileTree) return;
    if (!fileTree.getItem(path)) fileTree.add(path);
    const stop = fileTree.onMutation("remove", (event) => {
      if (event.path !== path) return;
      stop();
      deleteCancelled(path);
    });
    fileTree.startRenaming(path, { removeIfCanceled: true });
  },
});

// the context menu's row, and the delete it asks about; off a row there is no menu at all
const target = shallowRef("");
const confirm = useOverlay().create(ConfirmModal);
function onContext(e: MouseEvent): void {
  const row = e
    .composedPath()
    .find((el): el is HTMLElement => el instanceof HTMLElement && el.dataset.itemPath !== undefined);
  target.value = row?.dataset.itemPath ?? "";
  if (row) return;
  e.preventDefault();
  e.stopPropagation();
}
// what types a name in the row (Rename, New file, New folder) starts once the menu has closed, as the menu holds the
// keyboard while open and hands it back to the tree as it closes, which would leave the name unfocused
let afterClose: (() => void) | undefined;
function onCloseAutoFocus(e: Event): void {
  if (!afterClose) return;
  e.preventDefault();
  afterClose();
  afterClose = undefined;
}
const menu = computed<ContextMenuItem[][]>(() => {
  const path = target.value;
  if (!path) return [];
  const dir = path.endsWith("/") ? path : path.slice(0, path.lastIndexOf("/") + 1);
  const root = path.indexOf("/") === path.length - 1;
  // a folder window's own folder is not a mod's: it stays as it is there
  const fixed = root && rootOf(path)?.kind === "folder";
  // a folder in a window of its own: that window closes, its tabs back here
  const windowed = windowOf(path);
  const selected = (tree.value?.getSelectedPaths() ?? []).filter((p) => !p.endsWith("/"));
  const file = path.endsWith("/") ? "" : diskPath(path);
  const comparing = editor.comparing;
  const views = file ? viewsOf(locate(file, roots)?.rel ?? file) : undefined;
  // a mod's own folder in no repository is made one, and source control shown
  const bare = root ? git.bare.find((mod) => pathKey(mod.path) === pathKey(diskPath(path))) : undefined;
  // a folder of coats of arms (a file in it would be drawn with the Layers panel) offers a new coat for its mod
  const coatsMod =
    viewsOf(`${dir}x.txt`)?.panel === "Layers" ? rootOf(path.slice(0, path.indexOf("/") + 1))?.origin : "";
  return [
    file
      ? [
          { label: "Open to the side", icon: "i-lucide-columns-2", onSelect: () => editor.split("right", file) },
          {
            label: "Open in a new window",
            icon: "i-lucide-app-window",
            onSelect: () => void windows.openInNewWindow(file),
          },
          ...(views?.visual
            ? [
                {
                  label: "Open as Visual",
                  icon: "i-lucide-eye",
                  onSelect: () => {
                    editor.setView(file, "visual");
                    editor.open(file);
                  },
                },
              ]
            : []),
        ]
      : windowed
        ? [{ label: "Close the window", icon: "i-lucide-panel-top-close", onSelect: () => void Close(windowed) }]
        : [{ label: "Open in a new window", icon: "i-lucide-app-window", onSelect: () => void popOutFolder(path) }],
    writable.value
      ? [
          {
            label: "New file",
            icon: "i-lucide-file-plus",
            onSelect: () => (afterClose = () => create({ dir, isFolder: false })),
          },
          {
            label: "New folder",
            icon: "i-lucide-folder-plus",
            onSelect: () => (afterClose = () => create({ dir, isFolder: true })),
          },
          ...(coatsMod
            ? [
                {
                  label: "New coat of arms",
                  icon: "i-lucide-shield-plus",
                  onSelect: () => (useDrawStore().coatDialog = { mode: "new", id: "", text: "", mod: coatsMod }),
                },
              ]
            : []),
        ]
      : [],
    writable.value
      ? [
          { label: "Rename", disabled: root, onSelect: () => (afterClose = () => tree.value?.startRenaming(path)) },
          {
            label: root ? "Delete the mod's folder" : "Delete",
            color: "error" as const,
            disabled: fixed,
            onSelect: () => remove(path),
          },
        ]
      : [],
    file
      ? [
          { label: "Select for compare", onSelect: () => (editor.comparing = file) },
          {
            label: "Compare with selected",
            disabled: !comparing || comparing === file,
            onSelect: () => emit("compare", comparing, file),
          },
          ...(selected.length === 2
            ? [
                {
                  label: "Compare the two selected",
                  onSelect: () => emit("compare", diskPath(selected[0] ?? ""), diskPath(selected[1] ?? "")),
                },
              ]
            : []),
        ]
      : [],
    bare
      ? [
          {
            label: "Initialize a git repository",
            icon: "i-lucide-git-branch-plus",
            onSelect: () => {
              git.initialize(bare);
              editor.sidebar = "git";
            },
          },
        ]
      : [],
    [
      { label: "Copy path", onSelect: () => void Clipboard.SetText(diskPath(path)) },
      { label: "Reveal in the file manager", onSelect: () => void OpenFolder(diskPath(dir)) },
    ],
  ].filter((group) => group.length);
});

// the folders in windows of their own, as each window tells them, and the window each is in
const folderWindows = computed(
  () =>
    new Map(
      [...windows.otherWindows.values()]
        .filter((one) => one.name.startsWith("files:"))
        .flatMap((one) => (one.roots ?? []).map((root) => [pathKey(root), one.name])),
    ),
);
/** The window a folder's row (a tree path ending in "/") is open in, if it is. */
const windowOf = (path: string) => (path.endsWith("/") ? folderWindows.value.get(pathKey(diskPath(path))) : undefined);
// the marks follow the windows as they open and close (the tree draws its rows again)
watch(
  () => [...folderWindows.value.keys()].join("|"),
  () => tree.value?.setIcons(TREE_ICONS),
);
/** A click on a folder's window mark brings that window up, and leaves the folder as it was. */
function showFolderWindow(e: MouseEvent): void {
  const elements = e.composedPath().filter((el): el is HTMLElement => el instanceof HTMLElement);
  const name = elements.some((el) => el.dataset.itemSection === "decoration")
    ? windowOf(elements.find((el) => el.dataset.itemPath)?.dataset.itemPath ?? "")
    : undefined;
  if (!name) return;
  e.stopPropagation();
  e.preventDefault();
  void Show(name);
}

/** A folder (a root's too) in a window of its own, with this window's tabs of its files. */
function popOutFolder(path: string): void {
  const disk = diskPath(path);
  void windows.openFolderWindow(`files:${disk}`, path.replace(/\/$/, "").split("/").at(-1) ?? disk, [disk]);
}

// Ctrl Z moves the last rename, move or delete back, Ctrl Y or Ctrl Shift Z does it again
function onKey(e: KeyboardEvent): void {
  const key = e.key.toLowerCase();
  if (!e.ctrlKey || (key !== "z" && key !== "y") || e.composedPath().some((el) => el instanceof HTMLInputElement))
    return;
  e.preventDefault();
  if (key === "z" && !e.shiftKey) editor.undoMove();
  else editor.redoMove();
}

/** Delete a path once asked, into PMT's trash; a mod's own folder (its only slash the last) also leaves the workspace. */
const { mutate: remove } = useMutation({
  mutation: async (path: string) => {
    const root = path.indexOf("/") === path.length - 1;
    const text = `${diskPath(path)} is deleted from disk. ${root ? "The mod also leaves this workspace." : "Ctrl Z in the explorer brings it back until PMT closes."}`;
    if (!(await confirm.open({ title: "Delete from disk?", text, action: "Delete", danger: true }))) return;
    const disk = diskPath(path);
    const rootRow = rootOf(path);
    const trashed = await TrashPath(disk);
    tree.value?.remove(path, { recursive: true });
    editor.closeTabs(disk);
    if (!root) editor.recordMove({ from: disk, to: trashed, deleted: true });
    if (rootRow?.kind === "mod") saveWorkspace(() => RemoveWorkspaceMod(workspaceId, rootRow.origin ?? ""));
  },
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <UContextMenu :items="menu" :content="{ onCloseAutoFocus }">
      <!-- files a step brighter than the muted text, and what git ignores a step past the quiet text, in every theme -->
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
          // git's marks in Source control's letters' colors
          '--trees-status-added-override': 'var(--green)',
          '--trees-status-untracked-override': 'var(--green)',
          '--trees-status-modified-override': 'var(--orange)',
          '--trees-status-deleted-override': 'var(--red)',
          '--trees-status-renamed-override': 'var(--blue)',
          '--trees-status-ignored-override': 'color-mix(in oklab, var(--fg-3), var(--bg-2) 40%)',
        }"
        @contextmenu.capture="onContext"
        @keydown="onKey"
        @click.capture="showFolderWindow"
        @dblclick="editor.pin(editor.activeTab)"
      />
    </UContextMenu>
  </div>
</template>
