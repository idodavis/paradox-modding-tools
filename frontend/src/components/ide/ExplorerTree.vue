<script lang="ts">
import scriptSvg from "../../ide/fileicons/paradox-script.svg?raw";
import locSvg from "../../ide/fileicons/paradox-loc.svg?raw";
import guiSvg from "../../ide/fileicons/paradox-gui.svg?raw";
import modSvg from "../../ide/fileicons/paradox-mod.svg?raw";
import scriptUrl from "../../ide/fileicons/paradox-script.svg?url";
import locUrl from "../../ide/fileicons/paradox-loc.svg?url";
import guiUrl from "../../ide/fileicons/paradox-gui.svg?url";
import modUrl from "../../ide/fileicons/paradox-mod.svg?url";

/** The Paradox files' icons by extension: script, localization, GUI, and a mod's descriptor. */
const KINDS: Record<string, "script" | "loc" | "gui" | "mod"> = { txt: "script", yml: "loc", yaml: "loc", gui: "gui", mod: "mod", info: "mod" };
const URLS = { script: scriptUrl, loc: locUrl, gui: guiUrl, mod: modUrl };

/** A Paradox file's icon, for the editor's tabs; none for other files. */
export function iconOf(path: string): string | undefined {
  const kind = KINDS[path.slice(path.lastIndexOf(".") + 1).toLowerCase()];
  return kind && URLS[kind];
}

// the four icons as one sprite in the tree's shadow root, each gradient renamed so the four never share an id
const SPRITE = `<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position: absolute">${Object.entries({ script: scriptSvg, loc: locSvg, gui: guiSvg, mod: modSvg })
  .map(([kind, svg]) =>
    svg
      .replace(/<svg[^>]*>/, `<symbol id="pmt-${kind}" viewBox="0 0 800 800">`)
      .replace("</svg>", "</symbol>")
      .replaceAll('id="pmt"', `id="pmt-${kind}-fill"`)
      .replaceAll("url(#pmt)", `url(#pmt-${kind}-fill)`),
  )
  .join("")}</svg>`;
// Pierre's icons for every other kind of file
const ICONS = {
  set: "complete" as const,
  spriteSheet: SPRITE,
  byFileExtension: Object.fromEntries(Object.entries(KINDS).map(([ext, kind]) => [ext, { name: `pmt-${kind}`, viewBox: "0 0 800 800" }])),
};
// hidden while the workspace hides binaries, as search leaves them out
export const BINARIES = /\.(dll|exe|pdb|hash|hex|bin|dat)$|^\.git$/i;
</script>

<script setup lang="ts">
/**
 * A section of the explorer (the Ide board's sidebar): its roots, each a mod's with its color bar or the game's
 * with a lock, their folders listed when opened. Files open in the editor; a writable section creates, renames,
 * moves and deletes in place. It follows the open file, and Go's file watcher (fs:changed).
 */
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation } from "@pinia/colada";
import { Clipboard, Events } from "@wailsio/runtime";
import { FileTree, type FileTreeDirectoryHandle } from "@pierre/trees";
import type { ContextMenuItem } from "@nuxt/ui";
import type { IdeRoot } from "@services/models";
import { CreateDir, DeletePath, ListDirectory, RenamePath, StatPath, WriteFileBase64 } from "@services/fileservice";
import { OpenFolder, RemoveWorkspaceMod } from "@services/workspaceservice";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { useWorkspaceSave } from "../../composables/useWorkspaceSave";
import { useIdeStore } from "../../stores/ide";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";

const { workspaceId, roots, hideBinaries } = defineProps<{ workspaceId: string; roots: IdeRoot[]; hideBinaries: boolean }>();
const emit = defineEmits<{ compare: [old: string, now: string] }>();

const ide = useIdeStore();
const settings = useSettingsStore();
const store = useWorkspaceStore();
const { locate, under } = useIdeRoots(() => workspaceId);
const { mutate: saveWorkspace } = useWorkspaceSave(() => workspaceId);

const writable = computed(() => roots.some((root) => !root.readOnly));
// a root's name in the tree is its label, made unique where two share one
const names = computed(() => {
  const seen = new Map<string, number>();
  return roots.map((root) => {
    const count = (seen.get(root.label) ?? 0) + 1;
    seen.set(root.label, count);
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

/** A file on disk's tree path, where it is under one of this section's roots. */
function treePath(path: string, dir = false): string | undefined {
  const at = locate(path);
  const i = at ? roots.indexOf(at.root) : -1;
  return at && i >= 0 ? `${names.value[i]}/${at.rel}${dir ? "/" : ""}` : undefined;
}

const host = useTemplateRef("host");
const tree = shallowRef<FileTree>();
// the folders read so far, and every folder the tree has
const listed = new Set<string>();
const folders = new Set<string>();

async function list(dir: string): Promise<void> {
  if (listed.has(dir)) return;
  listed.add(dir);
  const entries = (await ListDirectory(diskPath(dir), !hideBinaries).catch(() => null)) ?? [];
  const shown = entries.filter((entry) => !hideBinaries || !BINARIES.test(entry.name));
  const paths = shown.map((entry) => `${dir}${entry.name}${entry.isDir ? "/" : ""}`);
  paths.filter((path) => path.endsWith("/")).forEach((path) => folders.add(path));
  tree.value?.batch(paths.filter((path) => !tree.value?.getItem(path)).map((path) => ({ type: "add" as const, path })));
}

function mount(): void {
  tree.value?.cleanUp();
  listed.clear();
  folders.clear();
  if (!host.value || !roots.length) return;
  const comfortable = settings.treeDensity === "comfortable";
  // a root's row, and its copy that sticks at the top, at 600 with its origin's bar on its start, in its origin's
  // color where the explorer colors the mods
  const rootRows = roots
    .map((root, i) => {
      const color = store.originColor(root.origin ?? "");
      const text = settings.ideOn("ide.rootColors") && !root.readOnly ? color : "var(--fg-1)";
      return `[data-item-path="${CSS.escape(`${names.value[i]}/`)}"] { font-weight: 600; color: ${text}; border-radius: 0; box-shadow: inset 3px 0 0 ${color}; }`;
    })
    .join("\n");
  const paths = names.value.map((name) => `${name}/`);
  paths.forEach((path) => folders.add(path));
  const made = new FileTree({
    paths,
    initialExpansion: "closed",
    density: comfortable ? "default" : "compact",
    itemHeight: comfortable ? 26 : 22,
    stickyFolders: settings.ideOn("ide.stickyFolders"),
    icons: ICONS,
    unsafeCSS: rootRows,
    // a read-only root says so with a lock
    renderRowDecoration: ({ item }) => (!writable.value && paths.includes(`${item.path.replace(/\/$/, "")}/`) ? { icon: "file-tree-icon-lock", title: "Read only" } : null),
    onSelectionChange: (selected) => {
      ide.selected = selected.filter((path) => !path.endsWith("/")).map(diskPath);
      const [only] = selected;
      if (selected.length === 1 && only && !only.endsWith("/")) ide.open(diskPath(only));
    },
    renaming: writable.value && {
      canRename: ({ path }) => path.indexOf("/") < path.length - 1,
      onRename: ({ sourcePath, destinationPath }) => rename({ from: sourcePath, to: destinationPath }),
    },
    dragAndDrop: writable.value && {
      canDrop: ({ target }) => target.kind === "directory",
      onDropComplete: ({ draggedPaths, target }) => {
        for (const from of draggedPaths) rename({ from, to: `${target.directoryPath}${from.slice(from.replace(/\/$/, "").lastIndexOf("/") + 1)}` });
      },
    },
  });
  // a folder's children are read when it first opens
  made.subscribe(() => {
    for (const dir of folders) if (!listed.has(dir) && isOpen(made, dir)) void list(dir);
  });
  made.render({ containerWrapper: host.value });
  tree.value = made;
  follow();
}
/** A folder's handle in a tree; none for a file or a path it lacks. */
function folder(made: FileTree | undefined, dir: string): FileTreeDirectoryHandle | undefined {
  const item = made?.getItem(dir);
  return item && "expand" in item ? item : undefined;
}
function isOpen(made: FileTree, dir: string): boolean {
  return !!folder(made, dir)?.isExpanded();
}
onMounted(mount);
watch(
  () => [roots, hideBinaries, settings.treeDensity, settings.ideOn("ide.stickyFolders"), settings.ideOn("ide.rootColors"), names.value.join("|")],
  mount,
);
onBeforeUnmount(() => tree.value?.cleanUp());

/** Open the folders down to the open file and select it, while the explorer follows the open file. */
async function follow(): Promise<void> {
  const path = ide.activePath;
  const target = path && settings.ideOn("ide.followFile") ? treePath(path) : undefined;
  const made = tree.value;
  if (!target || !made) return;
  const parts = target.split("/");
  for (let i = 1; i < parts.length; i++) {
    const dir = `${parts.slice(0, i).join("/")}/`;
    await list(dir);
    const item = folder(made, dir);
    if (item && !item.isExpanded()) item.expand();
  }
  if (made.getSelectedPaths()[0] !== target) made.getItem(target)?.select();
  made.scrollToPath(target, { focus: false, offset: "nearest" });
}
watch(() => ide.activePath, () => void follow());

/** Collapse every folder. */
function collapseAll(): void {
  for (const dir of folders) folder(tree.value, dir)?.collapse();
}
defineExpose({ collapseAll });

// Go's file watcher: a path gone leaves the tree, a new one in a folder already read joins it
const stop = Events.On("fs:changed", async ({ data }) => {
  const made = tree.value;
  const at = treePath(data.path);
  if (!made || !at) return;
  if (data.deleted) {
    if (made.getItem(at)) made.remove(at, { recursive: true });
    else if (made.getItem(`${at}/`)) made.remove(`${at}/`, { recursive: true });
    return;
  }
  const parent = at.slice(0, at.lastIndexOf("/") + 1);
  if (!listed.has(parent) || made.getItem(at) || made.getItem(`${at}/`)) return;
  const stat = await StatPath(data.path).catch(() => null);
  if (!stat?.exists || (hideBinaries && BINARIES.test(at.slice(parent.length)))) return;
  const path = stat.isDir ? `${at}/` : at;
  if (stat.isDir) folders.add(path);
  if (!made.getItem(path)) made.add(path);
});
onBeforeUnmount(stop);

// the tree has moved the path already; a failed rename on disk moves it back
const { mutate: rename } = useMutation({
  mutation: ({ from, to }: { from: string; to: string }) => RenamePath(diskPath(from), diskPath(to)),
  onSuccess: (_data, { from, to }) => ide.moved(diskPath(from), diskPath(to)),
  onError: (_error, { from, to }) => tree.value?.move(to, from),
});

/** A new file or folder in a folder, named in place. */
async function create(dir: string, isFolder: boolean): Promise<void> {
  await list(dir);
  const taken = (name: string) => tree.value?.getItem(`${dir}${name}`) || tree.value?.getItem(`${dir}${name}/`);
  let name = isFolder ? "new_folder" : "new_file.txt";
  for (let n = 2; taken(name); n++) name = isFolder ? `new_folder_${n}` : `new_file_${n}.txt`;
  const path = `${dir}${name}${isFolder ? "/" : ""}`;
  await (isFolder ? CreateDir(diskPath(path)) : WriteFileBase64(diskPath(path), ""));
  if (isFolder) folders.add(path);
  if (!tree.value?.getItem(path)) tree.value?.add(path);
  folder(tree.value, dir)?.expand();
  tree.value?.startRenaming(path);
}

// the context menu's row, and the delete it asks about
const target = shallowRef("");
const deleting = shallowRef("");
function onContext(e: MouseEvent): void {
  const row = e.composedPath().find((el): el is HTMLElement => el instanceof HTMLElement && el.dataset.itemPath !== undefined);
  target.value = row?.dataset.itemPath ?? "";
}
const menu = computed<ContextMenuItem[][]>(() => {
  const path = target.value;
  if (!path) return [];
  const dir = path.endsWith("/") ? path : path.slice(0, path.lastIndexOf("/") + 1);
  const root = path.indexOf("/") === path.length - 1;
  const selected = (tree.value?.getSelectedPaths() ?? []).filter((p) => !p.endsWith("/"));
  const file = path.endsWith("/") ? "" : diskPath(path);
  const picked = ide.comparing;
  return [
    file ? [{ label: "Open to the side", icon: "i-lucide-columns-2", onSelect: () => ide.split("right", file) }] : [],
    writable.value
      ? [
          { label: "New file", icon: "i-lucide-file-plus", onSelect: () => void create(dir, false) },
          { label: "New folder", icon: "i-lucide-folder-plus", onSelect: () => void create(dir, true) },
        ]
      : [],
    writable.value
      ? [
          { label: "Rename", disabled: root, onSelect: () => tree.value?.startRenaming(path) },
          { label: root ? "Delete the mod's folder" : "Delete", color: "error" as const, onSelect: () => (deleting.value = path) },
        ]
      : [],
    file
      ? [
          { label: "Select for compare", onSelect: () => (ide.comparing = file) },
          {
            label: "Compare with selected",
            disabled: !picked || picked === file,
            onSelect: () => emit("compare", picked, file),
          },
          ...(selected.length === 2
            ? [{ label: "Compare the two selected", onSelect: () => emit("compare", diskPath(selected[0] ?? ""), diskPath(selected[1] ?? "")) }]
            : []),
        ]
      : [],
    [
      { label: "Copy path", onSelect: () => void Clipboard.SetText(diskPath(path)) },
      { label: "Reveal in the file manager", onSelect: () => void OpenFolder(diskPath(dir)) },
    ],
  ].filter((group) => group.length);
});

/** Delete the asked path; a mod's own folder also leaves the workspace. */
async function remove(): Promise<void> {
  const path = deleting.value;
  deleting.value = "";
  const disk = diskPath(path);
  const root = roots[names.value.indexOf(path.slice(0, -1))];
  await DeletePath(disk);
  tree.value?.remove(path, { recursive: true });
  ide.moved(disk);
  if (root) saveWorkspace({ save: () => RemoveWorkspaceMod(workspaceId, root.origin ?? ""), rebuilds: true });
}
const { mutate: confirmDelete, isLoading: removing } = useMutation({ mutation: remove });
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
  <UContextMenu :items="menu">
    <div
      ref="host"
      class="min-h-0 flex-1"
      :style="{
        '--trees-bg-override': 'var(--bg-2)',
        '--trees-fg-override': 'var(--fg-2)',
        '--trees-fg-muted-override': 'var(--fg-3)',
        '--trees-selected-bg-override': 'var(--bg-4)',
        '--trees-selected-fg-override': 'var(--fg-1)',
        '--trees-accent-override': 'var(--accent)',
        '--trees-border-color-override': 'var(--line-1)',
        '--trees-focus-ring-color-override': 'var(--accent)',
        '--trees-font-family-override': 'var(--font-sans)',
        '--trees-font-size-override': 'var(--ide-ui-font)',
      }"
      @contextmenu.capture="onContext"
    />
  </UContextMenu>
  <UModal :open="!!deleting" title="Delete from disk?" @update:open="(open) => !open && (deleting = '')">
    <template #body>
      <p class="text-13 text-toned">
        {{ diskPath(deleting) }} is deleted from disk.
        <template v-if="deleting.indexOf('/') === deleting.length - 1">The mod also leaves this workspace.</template>
      </p>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton label="Cancel" @click="deleting = ''" />
        <UButton label="Delete" color="error" variant="soft" :loading="removing" @click="confirmDelete()" />
      </div>
    </template>
  </UModal>
  </div>
</template>
