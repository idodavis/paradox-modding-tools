/**
 * The IDE's client state: the editor groups, their tabs and where they sit, the files with unsaved edits, the open
 * files' problems, the caret, a place a tab is asked to show, and which panes are open. The editors themselves are
 * the CodeEditor components', the files' text ide/monacoEditor.ts's.
 */
import { computed, ref, shallowRef } from "vue";
import { defineStore } from "pinia";
import { useRouter } from "vue-router";
import { useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { DidClose } from "@services/ideservice";
import type { Diagnostic } from "@services/internal/lsp";
import type { PanelResults } from "@services/models";
import { pathKey } from "../composables/useIdeRoots";
import { holdsFiles, isMain, others } from "../windows";
import { useWorkspaceStore } from "./workspace";

/** An editor group: its tabs in order and the one shown. */
export type Group = { id: number; files: string[]; active: string };
/** Where the groups sit: a group, or parts side by side (a row) or stacked (a column), each part's share of it. */
export type Layout = { group: number } | { split: "row" | "column"; parts: Layout[]; sizes: number[] };
/** Where a tab dropped on a group goes: a new group on one of its edges, or the group itself. */
export type Edge = "left" | "right" | "up" | "down" | "middle";
/** A place a tab is asked to show: a 0-based line and UTF-8 byte column, as the backend counts. */
export type Place = { path: string; line: number; col: number };
/** A diff tab's kind: a file's changes (its staged copy against it), its staged changes (the last commit against
 * the staged copy), a conflict's two sides, or what a commit changed in it (its first parent against it). */
export type Diff = "changes" | "staged" | "conflict" | "commit";

/** The tab of a file's diff; a commit's names the commit. */
export const diffTab = (diff: Diff, path: string, commit = "") => `git:${diff}${commit && `@${commit}`}:${path}`;
/** A tab's file, and its diff (and commit) when it shows one; a file's own tab is its path. */
/** A file's view: its text (Code), or what it draws (Visual). */
export type View = "code" | "visual";
/** The images the IDE draws (ImageView); the raster ones have no text to show. */
export const IMAGES = /\.(png|jpe?g|gif|webp|bmp|ico|dds|tga|svg)$/i;
const VIEWS: Record<string, { code: string; visual: string; first: View }> = {
  md: { code: "Raw", visual: "Rich", first: "code" },
  svg: { code: "Text", visual: "Image", first: "visual" },
};
/** A file's two views where it has both, named as its kind names them, and the one it opens in. */
export function viewsOf(path: string): (typeof VIEWS)[string] | undefined {
  return VIEWS[path.slice(path.lastIndexOf(".") + 1).toLowerCase()];
}

export function tabOf(tab: string): { file: string; diff?: Diff; commit?: string } {
  const found = /^git:(changes|staged|conflict|commit)(?:@([0-9a-f]+))?:(.+)$/s.exec(tab);
  return found ? { file: found[3] ?? "", diff: found[1] as Diff, commit: found[2] } : { file: tab };
}

/** A layout with a group put beside another, on one of its edges. */
function beside(node: Layout, target: number, edge: Exclude<Edge, "middle">, added: number): Layout {
  const split = edge === "left" || edge === "right" ? "row" : "column";
  const first = edge === "left" || edge === "up";
  if ("group" in node) {
    if (node.group !== target) return node;
    return { split, parts: first ? [{ group: added }, node] : [node, { group: added }], sizes: [0.5, 0.5] };
  }
  const at = node.parts.findIndex((part) => "group" in part && part.group === target);
  // a group beside its sibling in the same direction shares that sibling's room
  if (node.split === split && at >= 0) {
    const parts = [...node.parts];
    const sizes = [...node.sizes];
    const half = (sizes[at] ?? 1) / 2;
    sizes[at] = half;
    parts.splice(first ? at : at + 1, 0, { group: added });
    sizes.splice(first ? at : at + 1, 0, half);
    return { ...node, parts, sizes };
  }
  return { ...node, parts: node.parts.map((part) => beside(part, target, edge, added)) };
}

/** A layout without a group; a split left with one part becomes that part. */
function without(node: Layout, gone: number): Layout | null {
  if ("group" in node) return node.group === gone ? null : node;
  const kept = node.parts
    .map((part, i) => ({ part: without(part, gone), size: node.sizes[i] ?? 0 }))
    .filter((one) => one.part);
  if (kept.length === 1) return kept[0]?.part ?? null;
  const total = kept.reduce((sum, one) => sum + one.size, 0) || 1;
  return kept.length
    ? { ...node, parts: kept.map((one) => one.part as Layout), sizes: kept.map((one) => one.size / total) }
    : null;
}

/** The group at a layout's top right, whose tab strip ends with the Wiki button. */
function corner(node: Layout): number {
  if ("group" in node) return node.group;
  const part = node.split === "row" ? node.parts.at(-1) : node.parts[0];
  return part ? corner(part) : 0;
}

/** A layout's groups in order. */
function order(node: Layout): number[] {
  return "group" in node ? [node.group] : node.parts.flatMap(order);
}

export const useIdeStore = defineStore("ide", () => {
  const workspace = useWorkspaceStore();
  const router = useRouter();
  const groups = ref<Group[]>([{ id: 1, files: [], active: "" }]);
  const layout = ref<Layout>({ group: 1 });
  const focused = shallowRef(1);
  const unsaved = ref(new Set<string>());
  /** The workspace whose files the editors hold, the one last restored. */
  const editing = shallowRef("");
  const problems = ref(new Map<string, Diagnostic[]>());
  /** The caret: its line, its column in the editor's characters and in UTF-8 bytes (`col`), as the backend counts. */
  const caret = shallowRef<{ line: number; character: number; col: number } | null>(null);
  const place = shallowRef<Place | null>(null);
  // the sidebar's Search results: what was looked for, its rows (hits or references) so far, done or capped
  const results = shallowRef<PanelResults | null>(null);
  // the files selected in the explorer, which the palette compares; the file picked to compare with another
  const selected = shallowRef<string[]>([]);
  const comparing = shallowRef("");
  // each file's view where it was switched from the one its kind opens in
  const views = ref(new Map<string, View>());
  function viewOf(path: string): View {
    return views.value.get(path) ?? viewsOf(path)?.first ?? (IMAGES.test(path) ? "visual" : "code");
  }
  // a tab being dragged, and the group it is dragged from
  const dragging = shallowRef<{ path: string; from: number } | null>(null);
  // which panes are open, kept on this computer by the main window; another window's are its own (the windows share
  // what is kept)
  const kept = <T>(key: string, start: T) => (isMain ? useLocalStorage(key, start) : shallowRef(start));
  const sidebar = kept<"explorer" | "search" | "git">("pmt.ide.sidebar", "explorer");
  const panel = kept<"problems" | "git" | "">("pmt.ide.panel", "problems");
  const wikiOpen = kept("pmt.ide.wikiOpen", false);
  // each workspace's groups and where they sit, kept on this computer
  const layouts = useLocalStorage<
    Record<string, { layout: Layout; groups: Group[]; others?: string[]; active?: string }>
  >("pmt.ide.layouts", {});

  const group = (id: number) => groups.value.find((one) => one.id === id);
  /** The tab shown in the focused group, and its file (a diff tab's too). */
  const activePath = computed(() => group(focused.value)?.active ?? "");
  const activeFile = computed(() => tabOf(activePath.value).file);
  /** Every open file once, in the groups' order. */
  const openFiles = computed(() => [...new Set(groups.value.flatMap((one) => one.files))]);
  const cornerGroup = computed(() => corner(layout.value));

  /** A path as an open tab spells it, else as given. */
  const spelled = (file: string) => openFiles.value.find((open) => pathKey(open) === pathKey(file)) ?? file;

  /** Show a file: in the focused group if it has it, else the first that does, else after the focused group's tab;
   * at a place when given one. A file is shown in the window holding it, else in one whose explorer shows its folder;
   * a window that holds no files asks the main window. The main window goes to the IDE to show it. */
  function open(file: string, at?: { line: number; col?: number }): void {
    const path = spelled(file);
    const key = pathKey(path);
    const all = [...others.values()];
    const holder = all.find((one) => one.tabs?.some((tab) => pathKey(tabOf(tab).file) === key));
    const home = all.find((one) => one.roots?.some((root) => key.startsWith(`${pathKey(root)}/`)));
    const here = openFiles.value.some((tab) => pathKey(tabOf(tab).file) === key);
    const to = holder?.name ?? (here ? "" : home?.name) ?? (holdsFiles ? "" : "main");
    if (to) return void Events.Emit("window:show", { to, route: "", path, line: at?.line ?? -1, col: at?.col ?? 0 });
    const target =
      [group(focused.value), ...groups.value].find((one) => one?.files.includes(path)) ??
      group(focused.value) ??
      groups.value[0];
    if (!target) return;
    if (!target.files.includes(path)) target.files.splice(target.files.indexOf(target.active) + 1, 0, path);
    target.active = path;
    focused.value = target.id;
    if (at) place.value = { path, line: at.line, col: at.col ?? 0 };
    if (isMain && router.currentRoute.value.name !== "workspace-ide")
      void router.push({ name: "workspace-ide", params: { id: workspace.activeWorkspaceId } });
  }

  /** A file edited where no tab shows it (a peek): a tab after the focused group's, the shown tab kept. */
  function adopt(file: string): void {
    const target = group(focused.value);
    if (!target || openFiles.value.some((open) => pathKey(open) === pathKey(file))) return;
    target.files.splice(target.files.indexOf(target.active) + 1, 0, file);
    if (!target.active) target.active = file;
  }

  /** Take a tab out of a group: its neighbor shows, and an emptied group goes unless it is the last. */
  function remove(id: number, path: string): void {
    const from = group(id);
    if (!from) return;
    const at = from.files.indexOf(path);
    if (at < 0) return;
    from.files = from.files.filter((file) => file !== path);
    if (from.active === path) from.active = from.files[Math.min(at, from.files.length - 1)] ?? "";
    if (from.files.length || groups.value.length < 2) return;
    groups.value = groups.value.filter((one) => one !== from);
    layout.value = without(layout.value, id) ?? { group: groups.value[0]?.id ?? 1 };
    if (focused.value === id) focused.value = order(layout.value)[0] ?? 1;
  }

  /** Forget a file no tab shows any more (its own or a diff); true when it went. */
  function forget(file: string): boolean {
    if (openFiles.value.some((open) => tabOf(open).file === file)) return false;
    unsaved.value.delete(file);
    problems.value.delete(file);
    return true;
  }

  /** Close a tab in a group; a file no tab shows any more is forgotten, the backend too. */
  function closeIn(id: number, tab: string): void {
    remove(id, tab);
    const { file } = tabOf(tab);
    if (forget(file)) void DidClose(workspace.activeWorkspaceId, file).catch(() => undefined);
  }

  /** Let tabs go to another window, which holds their files now: the backend keeps them open. */
  function release(tabs: string[]): void {
    for (const tab of tabs) {
      for (const one of [...groups.value]) remove(one.id, tab);
      forget(tabOf(tab).file);
    }
  }

  /** Close a file in every group. */
  function close(path: string): void {
    for (const one of [...groups.value]) closeIn(one.id, path);
  }

  /** The tabs of a file or folder moved on disk follow it; those of one deleted close. */
  function moved(from: string, to?: string): void {
    const base = pathKey(from);
    for (const path of openFiles.value) {
      const key = pathKey(path);
      if (key !== base && !key.startsWith(`${base}/`)) continue;
      if (!to) {
        close(path);
        continue;
      }
      const next = to + path.slice(from.length);
      for (const one of groups.value) {
        one.files = one.files.map((file) => (file === path ? next : file));
        if (one.active === path) one.active = next;
      }
      unsaved.value.delete(path);
      problems.value.delete(path);
    }
  }

  /** A new group on an edge of another, showing a file. */
  function splitAt(id: number, edge: Exclude<Edge, "middle">, path: string): void {
    const added = Math.max(...groups.value.map((one) => one.id)) + 1;
    groups.value.push({ id: added, files: [path], active: path });
    layout.value = beside(layout.value, id, edge, added);
    focused.value = added;
  }

  /** A tab dropped on a group: on an edge it moves into a new group there, in the middle into the group, before a
   * tab when dropped on it. */
  function drop(path: string, from: number, to: number, edge: Edge, before?: string): void {
    const source = group(from);
    const target = group(to);
    if (!source || !target) return;
    if (edge !== "middle") {
      // a group's only tab splits nothing off it
      if (from === to && source.files.length < 2) return;
      remove(from, path);
      return splitAt(to, edge, path);
    }
    if (from !== to) remove(from, path);
    const files = target.files.filter((file) => file !== path);
    const at = before ? files.indexOf(before) : -1;
    files.splice(at < 0 ? files.length : at, 0, path);
    target.files = files;
    target.active = path;
    focused.value = to;
  }

  /** A group's file (the focused group's shown one by default) shown in a new group beside it as well. */
  function split(edge: "right" | "down", path = activePath.value, id = focused.value): void {
    if (path) splitAt(id, edge, path);
  }

  /** The focused group's file moved into the next group, made on the right if there is none. */
  function moveToNext(): void {
    const path = activePath.value;
    const ids = order(layout.value);
    const next = ids[(ids.indexOf(focused.value) + 1) % ids.length];
    if (!path) return;
    if (next === undefined || next === focused.value) return drop(path, focused.value, focused.value, "right");
    drop(path, focused.value, next, "middle");
  }

  /** Mark a file unsaved or not, under its tab's spelling of its path. */
  function setUnsaved(file: string, on: boolean): void {
    const path = spelled(file);
    if (on) unsaved.value.add(path);
    else unsaved.value.delete(path);
  }

  /** A workspace's saved tabs, in the groups they were in where this computer kept them, else in one group; a tab
   * saved twice in a group comes back once. */
  function restore(id: string, fresh: boolean): void {
    editing.value = id;
    const saved = fresh ? undefined : layouts.value[id];
    const once = (list: string[]) => [...new Map(list.map((file) => [pathKey(file), file])).values()];
    const kept = (saved?.groups ?? [])
      .map((one) => ({ ...one, files: once(one.files) }))
      .filter((one) => one.files.length);
    let shape: Layout | null = saved?.layout ?? null;
    for (const one of saved?.groups ?? [])
      if (!kept.some((k) => k.id === one.id) && shape) shape = without(shape, one.id);
    const placed = new Set(kept.flatMap((one) => one.files.map(pathKey)));
    const rest = once((saved?.others ?? []).filter((file) => !placed.has(pathKey(file))));
    const active = saved?.active ?? "";
    if (!kept.length || !shape) {
      kept.splice(0, kept.length, { id: 1, files: [], active: "" });
      shape = { group: 1 };
    }
    kept[0]?.files.push(...rest);
    for (const one of kept) if (!one.files.includes(one.active)) one.active = one.files[0] ?? "";
    // the file shown last is shown again, in the group that has it
    const home = kept.find((one) => one.files.some((file) => pathKey(file) === pathKey(active)));
    if (home) home.active = home.files.find((file) => pathKey(file) === pathKey(active)) ?? home.active;
    groups.value = kept;
    layout.value = shape;
    focused.value = home?.id ?? kept[0]?.id ?? 1;
    unsaved.value.clear();
    problems.value.clear();
    caret.value = null;
  }

  /** Keep a workspace's tabs on this computer: its groups and where they sit, the tabs other windows hold (they come
   * back into the first group), and the file shown. */
  function remember(id: string, others: string[]): void {
    layouts.value = {
      ...layouts.value,
      [id]: { layout: layout.value, groups: groups.value, others, active: activePath.value },
    };
  }

  return {
    groups,
    layout,
    focused,
    unsaved,
    editing,
    problems,
    caret,
    place,
    results,
    selected,
    comparing,
    views,
    viewOf,
    dragging,
    sidebar,
    panel,
    wikiOpen,
    activePath,
    activeFile,
    openFiles,
    cornerGroup,
    group,
    open,
    adopt,
    closeIn,
    close,
    release,
    moved,
    drop,
    split,
    moveToNext,
    setUnsaved,
    restore,
    remember,
  };
});
