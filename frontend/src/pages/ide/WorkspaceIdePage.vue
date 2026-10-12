<script setup lang="ts">
/**
 * The IDE: the sidebar's explorer, a section for the mods and one for the game's files, its search
 * and its source control; the editor groups where they sit; the panel's problems, search
 * results and Git output; and the Wiki pane. A workspace's tabs come back when it opens, in their groups. The status
 * bar shows the problems and the branch, then the shown file's caret, indentation, kind and encoding, or what a
 * diff compares. In a window of its own (windows.ts) it shows one part: an editor group, the game's files, or the panel.
 */
import { computed, onActivated, onDeactivated, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useLocalStorage, watchDebounced } from "@vueuse/core";
import type { DropdownMenuItem } from "@nuxt/ui";
import type { IdeRoot } from "@services/models";
import { Fetch, Pull, Push, Stage, Unstage } from "@services/gitservice";
import { AddWorkspaceMod } from "@services/workspaceservice";
import { installEditor } from "../../editor/editor";
import { setWorkspace } from "../../editor/models";
import { useCompare } from "../../composables/useCompare";
import { pathKey, useIdeRoots } from "../../composables/useIdeRoots";
import { pickDirectory } from "../../lib/nativeDialog";
import { fileName, plural } from "../../lib/format";
import { languageOf } from "../../lib/monaco";
import { textQuery } from "../../queries";
import { useGitStore } from "../../stores/git";
import { useEditorStore } from "../../stores/editor";
import { tabOf } from "../../editor/tabs";
import { useSettingsStore } from "../../stores/settings";
import { Close, Show } from "@services/windowservice";
import { holdsFiles, isMain, useWindowsStore, windowName, windowPart } from "../../stores/windows";
import EditorLayout from "./EditorLayout.vue";
import ExplorerTree from "./ExplorerTree.vue";
import IdePanel from "./IdePanel.vue";
import IdeSettingsCard from "./IdeSettingsCard.vue";
import SearchView from "./SearchView.vue";
import SourceControlView from "./SourceControlView.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import WikiPane from "./WikiPane.vue";

const { id } = defineProps<{ id: string }>();

const editor = useEditorStore();
const windows = useWindowsStore();
installEditor(useRouter());
const git = useGitStore();
const settings = useSettingsStore();
const { data: rec, roots, locate } = useIdeRoots(() => id);
const { mutate: saveWorkspace } = useMutation({ mutation: (save: () => Promise<unknown>) => save() });
const compare = useCompare(() => id);

const mods = computed(() => roots.value.filter((root) => !root.readOnly));
const game = computed(() => roots.value.filter((root) => root.readOnly));
// a window of the game's files or of one folder (a mod's own, or one in it): the roots its explorer shows
const folder = windowPart === "files" ? windowName.slice("files:".length) : "";
const own = computed<IdeRoot[]>(() => {
  if (windowPart === "game") return game.value;
  const root = roots.value.find((one) => pathKey(one.path) === pathKey(folder));
  const located = locate(folder);
  return root ? [root] : located ? [{ ...located.root, label: fileName(folder), path: folder, kind: "folder" }] : [];
});
const hideBinaries = computed(() => !!rec.value?.hideExplorerBinaries);
const gameShown = computed(() => !!game.value.length && settings.on("ide.gameFiles"));

// the tabs the other windows hold, saved with the main window's so they come back here
const elsewhere = computed(() => [...windows.otherWindows.values()].flatMap((one) => one.tabs ?? []));

// a workspace's tabs come back in the main window when it opens, unless it starts fresh; those of the one it replaces
// are saved first; another window starts empty, ready for the tabs handed to it
let restored = "";
watch(
  rec,
  (open) => {
    if (!open || restored === id) return;
    if (restored && isMain) editor.remember(restored, elsewhere.value);
    restored = id;
    windows.roots = own.value.map((root) => root.path);
    // the panel's window holds no files: its problems are the other windows'
    if (!holdsFiles) return;
    setWorkspace(id, (path) => !!locate(path)?.root.readOnly);
    editor.restore(id, !isMain || open.resetIdeOnOpen);
  },
  { immediate: true },
);
watchDebounced(
  () => [editor.openTabs.join("\n"), elsewhere.value.join("\n"), editor.activeTab, JSON.stringify(editor.layout)],
  () => {
    if (!isMain || restored !== id || rec.value?.resetIdeOnOpen) return;
    editor.remember(id, elsewhere.value);
  },
  { debounce: 1000 },
);

async function addMod(): Promise<void> {
  const path = await pickDirectory("A mod folder");
  // Go names it from its descriptor, and finds its thumbnail
  if (path) saveWorkspace(() => AddWorkspaceMod(id, path));
}

const modTree = useTemplateRef("modTree");
const gameTree = useTemplateRef("gameTree");
function collapseAll(): void {
  modTree.value?.collapseAll();
  gameTree.value?.collapseAll();
}

// the panes, their sizes kept by the splitters: the sidebar, the editors (at least a third of the page) and the Wiki
// pane while open; under the editors the panel (the editors keeping half); in the explorer the mods' section and the
// game's files under it, which fold to their header and give the mods their room
const gameOpen = useLocalStorage("pmt.ide.gameOpen", true);
const panes = computed(() => [
  ...(isMain || own.value.length
    ? [{ id: "sidebar", slot: "sidebar", order: 1, sizeUnit: "px" as const, defaultSize: 264, minSize: 170 }]
    : []),
  ...(windowPart !== "panel" ? [{ id: "editors", slot: "editors", order: 2, minSize: 33 }] : []),
  ...(isMain && editor.wikiOpen
    ? [{ id: "wiki", slot: "wiki", order: 3, sizeUnit: "px" as const, defaultSize: 440, minSize: 320 }]
    : []),
]);
const editorPanes = computed(() => [
  { id: "code", slot: "code", order: 1, minSize: 50 },
  ...(isMain && editor.panel && !windows.otherWindows.has("panel")
    ? [{ id: "panel", slot: "panel", order: 2, sizeUnit: "px" as const, defaultSize: 200, minSize: 120 }]
    : []),
]);
const explorerPanes = computed(() => [
  {
    id: "mods",
    slot: "mods",
    order: 1,
    ...(gameShown.value && gameOpen.value ? { sizeUnit: "px" as const, defaultSize: 320, minSize: 120 } : {}),
  },
  ...(!gameShown.value
    ? []
    : gameOpen.value
      ? [{ id: "game", slot: "game", order: 2, sizeUnit: "px" as const, minSize: 120 }]
      : [
          {
            id: "game-folded",
            slot: "game",
            order: 2,
            sizeUnit: "px" as const,
            defaultSize: 33,
            minSize: 33,
            maxSize: 33,
          },
        ]),
]);

// the IDE's keys while it is the page shown, in the editor too
const visible = shallowRef(true);
onActivated(() => (visible.value = true));
onDeactivated(() => (visible.value = false));
const key = (handler: () => void) => ({ handler, usingInput: true });
defineShortcuts(
  computed(() =>
    visible.value
      ? {
          meta_shift_w: key(() => (editor.wikiOpen = !editor.wikiOpen)),
          meta_shift_f: key(() => (editor.sidebar = "search")),
          meta_shift_e: key(() => (editor.sidebar = "explorer")),
          meta_j: key(() => (editor.panel = editor.panel ? "" : "problems")),
          "meta_\\": key(() => editor.split("right")),
        }
      : {},
  ),
);

// the status bar: the open files' problems, then the shown file's facts
const { data: file } = useQuery(() => textQuery(editor.activeFile));
const diff = computed(() => tabOf(editor.activeTab).diff);
const COMPARES = {
  changes: "Comparing the working tree with the staged copy",
  staged: "Comparing the staged copy with the last commit",
  conflict: "Comparing a conflict's two sides",
  commit: "Comparing a commit with its parent",
};

// source control's More: the actions on every change at once, and the Git output
const gitMore = computed<DropdownMenuItem[]>(() => [
  {
    label: "Stage all changes",
    icon: "i-lucide-plus",
    onSelect: () =>
      git.run((root) =>
        Stage(
          root,
          (git.status?.changes ?? []).map((one) => one.path),
        ),
      ),
  },
  {
    label: "Unstage all",
    icon: "i-lucide-minus",
    onSelect: () =>
      git.run((root) =>
        Unstage(
          root,
          (git.status?.staged ?? []).map((one) => one.path),
        ),
      ),
  },
  { label: "Read the repository again", icon: "i-lucide-refresh-cw", onSelect: () => git.refresh() },
  { label: "Show the Git output", icon: "i-lucide-terminal", onSelect: () => (editor.panel = "git") },
]);
// the language the editor reads the file in, which Go's format decides
const KINDS: Record<string, string> = {
  paradox: "Paradox script",
  "paradox-loc": "Paradox localization",
  "paradox-gui": "Paradox GUI",
  markdown: "Markdown",
};
const kind = computed(() => KINDS[languageOf(editor.activeFile, file.value?.format)] ?? "Plain text");
function showProblems(): void {
  editor.panel = "problems";
}
</script>

<template>
  <div class="nokey flex h-full min-h-0">
    <IdePanel v-if="windowPart === 'panel'" :workspace-id="id" />
    <USplitter :items="panes" auto-save-id="pmt.ide" class="min-w-0 flex-1">
      <template #sidebar>
        <aside
          v-if="isMain"
          class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted text-(length:--ide-ui-font)"
        >
          <div class="flex h-9 shrink-0 items-center gap-0.5 border-b border-default px-1.5">
            <UButton
              icon="i-lucide-files"
              square
              :ui="{ leadingIcon: 'size-4.5' }"
              aria-label="Explorer"
              :color="editor.sidebar === 'explorer' ? 'primary' : 'neutral'"
              :variant="editor.sidebar === 'explorer' ? 'soft' : 'ghost'"
              @click="editor.sidebar = 'explorer'"
            />
            <UButton
              icon="i-lucide-search"
              square
              :ui="{ leadingIcon: 'size-4.5' }"
              aria-label="Search"
              :color="editor.sidebar === 'search' ? 'primary' : 'neutral'"
              :variant="editor.sidebar === 'search' ? 'soft' : 'ghost'"
              @click="editor.sidebar = 'search'"
            />
            <UButton
              icon="i-lucide-git-compare"
              square
              :ui="{ leadingIcon: 'size-4.5' }"
              aria-label="Source control"
              :color="editor.sidebar === 'git' ? 'primary' : 'neutral'"
              :variant="editor.sidebar === 'git' ? 'soft' : 'ghost'"
              @click="editor.sidebar = 'git'"
            />
            <span class="grow" />
            <template v-if="editor.sidebar === 'git' && git.repo">
              <UTooltip text="Fetch"
                ><UButton
                  icon="i-lucide-cloud-download"
                  variant="ghost"
                  square
                  aria-label="Fetch"
                  @click="git.run(Fetch)"
              /></UTooltip>
              <UTooltip text="Pull"
                ><UButton
                  icon="i-lucide-arrow-down-to-line"
                  variant="ghost"
                  square
                  aria-label="Pull"
                  @click="git.run((root) => Pull(root, '', ''))"
              /></UTooltip>
              <UTooltip text="Push"
                ><UButton
                  icon="i-lucide-arrow-up-from-line"
                  variant="ghost"
                  square
                  aria-label="Push"
                  @click="git.run(Push)"
              /></UTooltip>
              <UDropdownMenu :items="gitMore">
                <UButton icon="i-lucide-ellipsis" variant="ghost" square aria-label="More source control actions" />
              </UDropdownMenu>
              <USeparator orientation="vertical" class="mx-0.5 h-4" />
            </template>
            <UTooltip v-if="editor.sidebar === 'explorer'" text="Collapse all">
              <UButton
                icon="i-lucide-chevrons-down-up"
                variant="ghost"
                square
                :ui="{ leadingIcon: 'size-4.5' }"
                aria-label="Collapse all"
                @click="collapseAll"
              />
            </UTooltip>
            <IdeSettingsCard />
          </div>
          <USplitter
            v-show="editor.sidebar === 'explorer'"
            :items="explorerPanes"
            orientation="vertical"
            :disabled="!gameOpen"
            auto-save-id="pmt.ide.explorer"
            class="min-h-0 flex-1 py-1"
          >
            <template #mods>
              <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                <div
                  class="flex h-7 shrink-0 items-center gap-1 ps-2.5 pe-1 text-11 font-semibold whitespace-nowrap text-toned"
                >
                  Workspace<span class="ms-1 font-normal text-dimmed">{{ plural(mods.length, "mod") }}</span>
                  <span class="grow" />
                  <UTooltip text="Add a mod">
                    <UButton
                      icon="i-lucide-plus"
                      variant="ghost"
                      size="xs"
                      square
                      aria-label="Add a mod"
                      @click="addMod"
                    />
                  </UTooltip>
                </div>
                <ExplorerTree
                  ref="modTree"
                  :workspace-id="id"
                  :roots="mods"
                  :hide-binaries="hideBinaries"
                  @compare="compare.files"
                />
              </div>
            </template>
            <template #game>
              <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                <div class="flex h-8 shrink-0 items-center border-t border-default pe-1">
                  <button
                    type="button"
                    class="flex h-8 flex-1 cursor-pointer items-center gap-1 ps-1.5 text-11 font-semibold text-toned hover:text-highlighted"
                    :aria-expanded="gameOpen"
                    @click="gameOpen = !gameOpen"
                  >
                    <UIcon :name="gameOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />Game
                    files
                  </button>
                  <UTooltip v-if="!windows.otherWindows.has('game')" text="Open in its own window">
                    <UButton
                      icon="i-lucide-app-window"
                      variant="ghost"
                      size="xs"
                      square
                      aria-label="Open in its own window"
                      @click="
                        windows.openFolderWindow(
                          'game',
                          'Game files',
                          game.map((root) => root.path),
                        )
                      "
                    />
                  </UTooltip>
                </div>
                <div
                  v-if="windows.otherWindows.has('game')"
                  v-show="gameOpen"
                  class="flex flex-col gap-2 px-3 py-1.5 text-12 text-dimmed"
                >
                  In its own window.
                  <div class="flex gap-1.5">
                    <UButton label="Bring back" size="xs" variant="soft" @click="Close('game')" />
                    <UButton label="Show window" size="xs" variant="ghost" @click="Show('game')" />
                  </div>
                </div>
                <ExplorerTree
                  v-else
                  v-show="gameOpen"
                  ref="gameTree"
                  :workspace-id="id"
                  :roots="game"
                  :hide-binaries="hideBinaries"
                  @compare="compare.files"
                />
              </div>
            </template>
          </USplitter>
          <SearchView v-show="editor.sidebar === 'search'" :roots="roots" :hide-binaries="hideBinaries" />
          <SourceControlView v-if="editor.sidebar === 'git'" />
        </aside>
        <aside
          v-else
          class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted py-1 text-(length:--ide-ui-font)"
        >
          <ExplorerTree :workspace-id="id" :roots="own" :hide-binaries="hideBinaries" @compare="compare.files" />
        </aside>
      </template>
      <template #editors>
        <USplitter :items="editorPanes" orientation="vertical" auto-save-id="pmt.ide.editors" class="min-w-0 flex-1">
          <template #code>
            <div class="flex min-h-0 min-w-0 flex-1"><EditorLayout :node="editor.layout" :workspace-id="id" /></div>
          </template>
          <template #panel>
            <div class="flex min-h-0 min-w-0 flex-1 border-t border-default"><IdePanel :workspace-id="id" /></div>
          </template>
        </USplitter>
      </template>
      <template #wiki>
        <WikiPane :workspace-id="id" class="min-w-0 flex-1" />
      </template>
    </USplitter>
  </div>
  <StatusFacts v-if="windowPart !== 'panel'">
    <button
      class="flex h-6 items-center gap-1 text-error"
      :aria-label="plural(editor.problemCounts.errors, 'error')"
      @click="showProblems"
    >
      <UIcon name="i-lucide-circle-x" class="size-3" />{{ editor.problemCounts.errors }}
    </button>
    <button
      class="flex h-6 items-center gap-1 text-warning"
      :aria-label="plural(editor.problemCounts.warnings, 'warning')"
      @click="showProblems"
    >
      <UIcon name="i-lucide-triangle-alert" class="size-3" />{{ editor.problemCounts.warnings }}
    </button>
    <button
      v-if="git.status"
      class="flex h-6 items-center gap-1 hover:text-highlighted"
      @click="editor.sidebar = 'git'"
    >
      <UIcon name="i-lucide-git-branch" class="size-3" />{{ git.status.branch || git.status.commit }}
      <template v-if="git.status.upstream"
        ><UIcon name="i-lucide-arrow-down-up" class="size-3" />{{ git.status.ahead }} up,
        {{ git.status.behind }} down</template
      >
    </button>
  </StatusFacts>
  <StatusFacts v-if="diff" end>
    <span>{{ COMPARES[diff] }}</span>
  </StatusFacts>
  <StatusFacts v-else-if="editor.activeTab" end>
    <span v-if="editor.caret">Ln {{ editor.caret.line + 1 }}, Col {{ editor.caret.character + 1 }}</span>
    <span>{{ settings.indentSpaces ? `Spaces: ${settings.indentSize}` : "Tabs" }}</span>
    <span>{{ kind }}</span>
    <span v-if="file && !file.reject">{{ file.legacy ? "Windows-1252" : file.bom ? "UTF-8 BOM" : "UTF-8" }}</span>
  </StatusFacts>
</template>
