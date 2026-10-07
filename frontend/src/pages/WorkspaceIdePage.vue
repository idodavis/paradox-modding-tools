<script setup lang="ts">
/**
 * The IDE (the Ide board): the sidebar's explorer, a section for the mods and one for the game's files, its search
 * and its source control (the IdeDiff board); the editor groups where they sit; the panel's problems, search
 * results and Git output; and the Wiki pane. A workspace's tabs come back when it opens, in their groups. The status
 * bar shows the problems and the branch, then the shown file's caret, indentation, kind and encoding, or what a
 * diff compares. In a window of its own (windows.ts) it shows one part: an editor group, the game's files, or the panel.
 */
import { computed, onActivated, onDeactivated, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery, useQueryCache } from "@pinia/colada";
import { useElementSize, useLocalStorage, watchDebounced } from "@vueuse/core";
import type { DropdownMenuItem } from "@nuxt/ui";
import type { IdeRoot, IdeRoots } from "@services/models";
import { Fetch, Pull, Push, Stage, Unstage } from "@services/gitservice";
import { AddWorkspaceMod, SaveIdeSession } from "@services/workspaceservice";
import { openFilesWindow, setWorkspace } from "../ide/monacoEditor";
import { useCompare } from "../composables/useCompare";
import { pathKey, useIdeRoots } from "../composables/useIdeRoots";
import { useWorkspaceSave } from "../composables/useWorkspaceSave";
import { pickDirectory } from "../lib/nativeDialog";
import { fileName, plural } from "../lib/format";
import { ideRootsQuery, textQuery } from "../queries";
import { useGitStore } from "../stores/git";
import { tabOf, useIdeStore } from "../stores/ide";
import { useSettingsStore } from "../stores/settings";
import { Close, Show } from "@services/windowservice";
import { holdsFiles, isMain, others, roots as windowRoots, windowName, windowPart } from "../windows";

const { id } = defineProps<{ id: string }>();

const ide = useIdeStore();
const git = useGitStore();
const settings = useSettingsStore();
const { data: rec, roots, locate } = useIdeRoots(() => id);
const { mutate: saveWorkspace } = useWorkspaceSave(() => id);
const compare = useCompare(() => id);

const mods = computed(() => roots.value.filter((root) => !root.readOnly));
const game = computed(() => roots.value.filter((root) => root.readOnly));
// a window of the game's files or of one folder (a mod's own, or one in it): the roots its explorer shows
const folder = windowPart === "files" ? windowName.slice("files:".length) : "";
const own = computed<IdeRoot[]>(() => {
  if (windowPart === "game") return game.value;
  const root = roots.value.find((one) => pathKey(one.path) === pathKey(folder));
  const at = locate(folder);
  return root ? [root] : at ? [{ ...at.root, label: fileName(folder), path: folder, kind: "folder" }] : [];
});
const hideBinaries = computed(() => !!rec.value?.hideExplorerBinaries);
const gameShown = computed(() => !!game.value.length && settings.ideOn("ide.gameFiles"));

// the tabs the other windows hold, saved with the main window's so they come back here
const elsewhere = computed(() => [...others.values()].flatMap((one) => one.tabs ?? []));

// a workspace's tabs are saved, and its cached roots told, so opening it again restores them and not the roots'
// first read
const queryCache = useQueryCache();
function saveTabs(workspaceId: string): void {
  const files = [...new Set([...ide.openFiles, ...elsewhere.value])];
  const { key } = ideRootsQuery(workspaceId);
  const cached = queryCache.getQueryData(key);
  if (cached) {
    const next: IdeRoots = { ...cached, ideOpenFiles: files, ideActiveFile: ide.activePath };
    queryCache.setQueryData(key, next);
  }
  void SaveIdeSession(workspaceId, files, ide.activePath).catch(() => undefined);
  ide.remember(workspaceId);
}

// a workspace's tabs come back in the main window when it opens, unless it starts fresh; those of the one it replaces
// are saved first; another window starts empty, ready for the tabs handed to it
let restored = "";
watch(
  rec,
  (open) => {
    if (!open || restored === id) return;
    if (restored && isMain) saveTabs(restored);
    restored = id;
    windowRoots.value = own.value.map((root) => root.path);
    // the panel's window holds no files: its problems are the other windows'
    if (!holdsFiles) return;
    setWorkspace(id, (path) => !!locate(path)?.root.readOnly);
    const tabs = isMain && !open.resetIdeOnOpen ? (open.ideOpenFiles ?? []) : [];
    ide.restore(id, tabs, isMain ? (open.ideActiveFile ?? "") : "");
  },
  { immediate: true },
);
watchDebounced(
  () => [ide.openFiles.join("\n"), elsewhere.value.join("\n"), ide.activePath, JSON.stringify(ide.layout)],
  () => {
    if (!isMain || restored !== id || rec.value?.resetIdeOnOpen) return;
    saveTabs(id);
  },
  { debounce: 1000 },
);

async function addMod(): Promise<void> {
  const path = await pickDirectory("A mod folder");
  // Go names it from its descriptor, and finds its thumbnail
  if (path) saveWorkspace({ save: () => AddWorkspaceMod(id, path), rebuilds: true });
}

const modTree = useTemplateRef("modTree");
const gameTree = useTemplateRef("gameTree");
function collapseAll(): void {
  modTree.value?.collapseAll();
  gameTree.value?.collapseAll();
}

// the panes' sizes, each at most half the space it divides, also in a window smaller than the one they were set in:
// the sidebar half the page, the Wiki pane half of what the sidebar leaves
const page = useTemplateRef("page");
const { width, height } = useElementSize(page);
const sidebarWidth = useLocalStorage("pmt.ide.sidebarWidth", 264);
const panelHeight = useLocalStorage("pmt.ide.panelHeight", 200);
const wikiWidth = useLocalStorage("pmt.ide.wikiWidth", 440);
const sidebarMax = computed(() => Math.max(170, width.value / 2));
const wikiMax = computed(() => Math.max(320, (width.value - Math.min(sidebarWidth.value, sidebarMax.value)) / 2));
// the mods' section's height, the game's files under it
const explorer = useTemplateRef("explorer");
const { height: explorerHeight } = useElementSize(explorer);
const modsHeight = useLocalStorage("pmt.ide.modsHeight", 320);
// the Game files pane folds to its header, and the mods take its room
const gameOpen = useLocalStorage("pmt.ide.gameOpen", true);
// unfolded and not in a window of its own: the pane takes its height
const gameHere = computed(() => gameOpen.value && !others.has("game"));

// the IDE's keys while it is the page shown, in the editor too
const shown = shallowRef(true);
onActivated(() => (shown.value = true));
onDeactivated(() => (shown.value = false));
const key = (handler: () => void) => ({ handler, usingInput: true });
defineShortcuts(
  computed(() =>
    shown.value
      ? {
          meta_shift_w: key(() => (ide.wikiOpen = !ide.wikiOpen)),
          meta_shift_f: key(() => (ide.sidebar = "search")),
          meta_shift_e: key(() => (ide.sidebar = "explorer")),
          meta_j: key(() => (ide.panel = ide.panel ? "" : "problems")),
          "meta_\\": key(() => ide.split("right")),
        }
      : {},
  ),
);

// the status bar: the open files' problems, then the shown file's facts
const counts = computed(() => {
  const all = [...ide.problems.values()].flat();
  return { errors: all.filter((found) => found.severity === 1).length, warnings: all.filter((found) => found.severity !== 1).length };
});
const { data: file } = useQuery(() => textQuery(ide.activeFile));
const diff = computed(() => tabOf(ide.activePath).diff);
const COMPARES = { changes: "Comparing the working tree with the staged copy", staged: "Comparing the staged copy with the last commit", conflict: "Comparing a conflict's two sides", commit: "Comparing a commit with its parent" };

// source control's More: the actions on every change at once, and the Git output
const gitMore = computed<DropdownMenuItem[]>(() => [
  { label: "Stage all changes", icon: "i-lucide-plus", onSelect: () => git.run((root) => Stage(root, (git.status?.changes ?? []).map((one) => one.path))) },
  { label: "Unstage all", icon: "i-lucide-minus", onSelect: () => git.run((root) => Unstage(root, (git.status?.staged ?? []).map((one) => one.path))) },
  { label: "Read the repository again", icon: "i-lucide-refresh-cw", onSelect: () => git.refresh() },
  { label: "Show the Git output", icon: "i-lucide-terminal", onSelect: () => (ide.panel = "git") },
]);
const KINDS: Record<string, string> = { txt: "Paradox script", yml: "Paradox localization", gui: "Paradox GUI", mod: "Mod descriptor", info: "Mod metadata", md: "Markdown" };
const kind = computed(() => KINDS[ide.activeFile.slice(ide.activeFile.lastIndexOf(".") + 1).toLowerCase()] ?? "Plain text");
function showProblems(): void {
  ide.panel = "problems";
}
</script>

<template>
  <div ref="page" class="nokey flex h-full min-h-0">
    <IdePanel v-if="windowPart === 'panel'" :workspace-id="id" />
    <aside v-if="isMain" class="flex min-h-0 shrink-0 flex-col border-e border-default bg-muted text-(length:--ide-ui-font)" :style="{ width: `${Math.min(sidebarWidth, sidebarMax)}px` }">
      <div class="flex h-9 shrink-0 items-center gap-0.5 border-b border-default px-1.5">
        <UButton
          icon="i-lucide-files"
          square
          :ui="{ leadingIcon: 'size-4.5' }"
          aria-label="Explorer"
          :color="ide.sidebar === 'explorer' ? 'primary' : 'neutral'"
          :variant="ide.sidebar === 'explorer' ? 'soft' : 'ghost'"
          @click="ide.sidebar = 'explorer'"
        />
        <UButton
          icon="i-lucide-search"
          square
          :ui="{ leadingIcon: 'size-4.5' }"
          aria-label="Search"
          :color="ide.sidebar === 'search' ? 'primary' : 'neutral'"
          :variant="ide.sidebar === 'search' ? 'soft' : 'ghost'"
          @click="ide.sidebar = 'search'"
        />
        <UButton
          icon="i-lucide-git-compare"
          square
          :ui="{ leadingIcon: 'size-4.5' }"
          aria-label="Source control"
          :color="ide.sidebar === 'git' ? 'primary' : 'neutral'"
          :variant="ide.sidebar === 'git' ? 'soft' : 'ghost'"
          @click="ide.sidebar = 'git'"
        />
        <span class="grow" />
        <template v-if="ide.sidebar === 'git' && git.repo">
          <UTooltip text="Fetch"><UButton icon="i-lucide-cloud-download" variant="ghost" square aria-label="Fetch" @click="git.run(Fetch)" /></UTooltip>
          <UTooltip text="Pull"><UButton icon="i-lucide-arrow-down-to-line" variant="ghost" square aria-label="Pull" @click="git.run((root) => Pull(root, '', ''))" /></UTooltip>
          <UTooltip text="Push"><UButton icon="i-lucide-arrow-up-from-line" variant="ghost" square aria-label="Push" @click="git.run(Push)" /></UTooltip>
          <UDropdownMenu :items="gitMore">
            <UButton icon="i-lucide-ellipsis" variant="ghost" square aria-label="More source control actions" />
          </UDropdownMenu>
          <USeparator orientation="vertical" class="mx-0.5 h-4" />
        </template>
        <UTooltip v-if="ide.sidebar === 'explorer'" text="Collapse all">
          <UButton icon="i-lucide-chevrons-down-up" variant="ghost" square :ui="{ leadingIcon: 'size-4.5' }" aria-label="Collapse all" @click="collapseAll" />
        </UTooltip>
        <IdeSettingsCard />
      </div>
      <div v-show="ide.sidebar === 'explorer'" ref="explorer" class="flex min-h-0 flex-1 flex-col py-1">
        <div
          class="flex min-h-0 flex-col"
          :class="gameShown && gameHere ? 'shrink-0' : 'flex-1'"
          :style="gameShown && gameHere ? { height: `${Math.max(120, Math.min(modsHeight, explorerHeight - 120))}px` } : undefined"
        >
          <div class="flex h-7 shrink-0 items-center gap-1 ps-2.5 pe-1 text-11 font-semibold whitespace-nowrap text-toned">
            Workspace<span class="ms-1 font-normal text-dimmed">{{ plural(mods.length, "mod") }}</span>
            <span class="grow" />
            <UTooltip text="Add a mod">
              <UButton icon="i-lucide-plus" variant="ghost" size="xs" square aria-label="Add a mod" @click="addMod" />
            </UTooltip>
          </div>
          <ExplorerTree ref="modTree" :workspace-id="id" :roots="mods" :hide-binaries="hideBinaries" @compare="compare.files" />
        </div>
        <template v-if="gameShown">
          <ResizeGrip v-if="gameHere" v-model="modsHeight" axis="y" :min="120" :max="Math.max(120, explorerHeight - 120)" :default-size="320" />
          <div class="flex h-8 shrink-0 items-center border-t border-default pe-1">
            <button
              type="button"
              class="flex h-8 flex-1 cursor-pointer items-center gap-1 ps-1.5 text-11 font-semibold text-toned hover:text-highlighted"
              :aria-expanded="gameOpen"
              @click="gameOpen = !gameOpen"
            >
              <UIcon :name="gameOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />Game files
            </button>
            <UTooltip v-if="!others.has('game')" text="Open in its own window">
              <UButton icon="i-lucide-app-window" variant="ghost" size="xs" square aria-label="Open in its own window" @click="openFilesWindow('game', 'Game files', game.map((root) => root.path))" />
            </UTooltip>
          </div>
          <div v-if="others.has('game')" v-show="gameOpen" class="flex flex-col gap-2 px-3 py-1.5 text-12 text-dimmed">
            In its own window.
            <div class="flex gap-1.5">
              <UButton label="Bring back" size="xs" variant="soft" @click="Close('game')" />
              <UButton label="Show window" size="xs" variant="ghost" @click="Show('game')" />
            </div>
          </div>
          <ExplorerTree v-else v-show="gameOpen" ref="gameTree" :workspace-id="id" :roots="game" :hide-binaries="hideBinaries" @compare="compare.files" />
        </template>
      </div>
      <SearchView v-show="ide.sidebar === 'search'" :roots="roots" :hide-binaries="hideBinaries" />
      <SourceControlView v-if="ide.sidebar === 'git'" />
    </aside>
    <aside v-else-if="own.length" class="flex min-h-0 shrink-0 flex-col border-e border-default bg-muted py-1 text-(length:--ide-ui-font)" :style="{ width: `${Math.min(sidebarWidth, sidebarMax)}px` }">
      <ExplorerTree :workspace-id="id" :roots="own" :hide-binaries="hideBinaries" @compare="compare.files" />
    </aside>
    <ResizeGrip v-if="isMain || own.length" v-model="sidebarWidth" :min="170" :max="sidebarMax" :default-size="264" />
    <section v-if="windowPart !== 'panel'" class="flex min-w-0 flex-1 flex-col">
      <div class="flex min-h-0 flex-1">
        <EditorLayout :node="ide.layout" :workspace-id="id" />
      </div>
      <template v-if="isMain && ide.panel && !others.has('panel')">
        <ResizeGrip v-model="panelHeight" axis="y" reverse :min="120" :max="Math.max(120, height / 2)" :default-size="200" />
        <div class="flex shrink-0 border-t border-default" :style="{ height: `${panelHeight}px` }">
          <IdePanel :workspace-id="id" />
        </div>
      </template>
    </section>
    <ResizeGrip v-show="isMain && ide.wikiOpen" v-model="wikiWidth" reverse :min="320" :max="wikiMax" :default-size="440" />
    <WikiPane v-if="isMain" v-show="ide.wikiOpen" :workspace-id="id" class="shrink-0" :style="{ width: `${Math.min(wikiWidth, wikiMax)}px` }" />
  </div>
  <StatusFacts v-if="windowPart !== 'panel'">
    <button class="flex h-6 items-center gap-1 text-error" :aria-label="plural(counts.errors, 'error')" @click="showProblems">
      <UIcon name="i-lucide-circle-x" class="size-3" />{{ counts.errors }}
    </button>
    <button class="flex h-6 items-center gap-1 text-warning" :aria-label="plural(counts.warnings, 'warning')" @click="showProblems">
      <UIcon name="i-lucide-triangle-alert" class="size-3" />{{ counts.warnings }}
    </button>
    <button v-if="git.status" class="flex h-6 items-center gap-1 hover:text-highlighted" @click="ide.sidebar = 'git'">
      <UIcon name="i-lucide-git-branch" class="size-3" />{{ git.status.branch || git.status.commit }}
      <template v-if="git.status.upstream"><UIcon name="i-lucide-arrow-down-up" class="size-3" />{{ git.status.ahead }} up, {{ git.status.behind }} down</template>
    </button>
  </StatusFacts>
  <StatusFacts v-if="diff" end>
    <span>{{ COMPARES[diff] }}</span>
  </StatusFacts>
  <StatusFacts v-else-if="ide.activePath" end>
    <span v-if="ide.caret">Ln {{ ide.caret.line + 1 }}, Col {{ ide.caret.character + 1 }}</span>
    <span>{{ settings.indentSpaces ? `Spaces: ${settings.indentSize}` : "Tabs" }}</span>
    <span>{{ kind }}</span>
    <span v-if="file?.text !== undefined">{{ file.bom ? "UTF-8 BOM" : "UTF-8" }}</span>
  </StatusFacts>
</template>
