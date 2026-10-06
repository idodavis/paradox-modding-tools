<script setup lang="ts">
/**
 * The IDE (the Ide board): the sidebar's explorer, a section for the mods and one for the game's files, and its
 * search; the editor groups where they sit; the panel's problems and search results; and the Wiki pane. A
 * workspace's tabs come back when it opens, in their groups. The status bar shows the problems and the shown file's
 * caret, indentation, kind and encoding.
 */
import { computed, onActivated, onDeactivated, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { useElementSize, useLocalStorage, watchDebounced } from "@vueuse/core";
import { AddWorkspaceMod, SaveIdeSession } from "@services/workspaceservice";
import { setWorkspace } from "../ide/monacoEditor";
import { useCompare } from "../composables/useCompare";
import { useIdeRoots } from "../composables/useIdeRoots";
import { useWorkspaceSave } from "../composables/useWorkspaceSave";
import { pickDirectory } from "../lib/nativeDialog";
import { plural } from "../lib/format";
import { textQuery } from "../queries";
import { useIdeStore } from "../stores/ide";
import { useSettingsStore } from "../stores/settings";

const { id } = defineProps<{ id: string }>();

const ide = useIdeStore();
const settings = useSettingsStore();
const { data: rec, roots, locate } = useIdeRoots(() => id);
const { mutate: saveWorkspace } = useWorkspaceSave(() => id);
const compare = useCompare(() => id);

const mods = computed(() => roots.value.filter((root) => !root.readOnly));
const game = computed(() => roots.value.filter((root) => root.readOnly));
const hideBinaries = computed(() => !!rec.value?.hideExplorerBinaries);
const gameShown = computed(() => !!game.value.length && settings.ideOn("ide.gameFiles"));

// a workspace's tabs come back when it opens, unless it starts fresh; those of the one it replaces are saved first
let restored = "";
watch(
  rec,
  (open) => {
    if (!open || restored === id) return;
    if (restored) {
      void SaveIdeSession(restored, ide.openFiles, ide.activePath).catch(() => undefined);
      ide.remember(restored);
    }
    restored = id;
    setWorkspace(id, (path) => !!locate(path)?.root.readOnly);
    ide.restore(id, open.resetIdeOnOpen ? [] : (open.ideOpenFiles ?? []), open.ideActiveFile ?? "");
  },
  { immediate: true },
);
watchDebounced(
  () => [ide.openFiles.join("\n"), ide.activePath, JSON.stringify(ide.layout)],
  () => {
    if (restored !== id || rec.value?.resetIdeOnOpen) return;
    void SaveIdeSession(id, ide.openFiles, ide.activePath).catch(() => undefined);
    ide.remember(id);
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
const { data: file } = useQuery(() => textQuery(ide.activePath));
const KINDS: Record<string, string> = { txt: "Paradox script", yml: "Paradox localization", gui: "Paradox GUI", mod: "Mod descriptor", info: "Mod metadata", md: "Markdown" };
const kind = computed(() => KINDS[ide.activePath.slice(ide.activePath.lastIndexOf(".") + 1).toLowerCase()] ?? "Plain text");
function showProblems(): void {
  ide.panel = "problems";
}
</script>

<template>
  <div ref="page" class="nokey flex h-full min-h-0">
    <aside class="flex min-h-0 shrink-0 flex-col border-e border-default bg-muted text-(length:--ide-ui-font)" :style="{ width: `${Math.min(sidebarWidth, sidebarMax)}px` }">
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
        <span class="grow" />
        <UTooltip v-if="ide.sidebar === 'explorer'" text="Collapse all">
          <UButton icon="i-lucide-chevrons-down-up" variant="ghost" square :ui="{ leadingIcon: 'size-4.5' }" aria-label="Collapse all" @click="collapseAll" />
        </UTooltip>
        <IdeSettingsCard />
      </div>
      <div v-show="ide.sidebar === 'explorer'" ref="explorer" class="flex min-h-0 flex-1 flex-col py-1">
        <div
          class="flex min-h-0 flex-col"
          :class="gameShown ? 'shrink-0' : 'flex-1'"
          :style="gameShown ? { height: `${Math.max(120, Math.min(modsHeight, explorerHeight - 120))}px` } : undefined"
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
          <ResizeGrip v-model="modsHeight" axis="y" :min="120" :max="Math.max(120, explorerHeight - 120)" :default-size="320" />
          <div class="flex h-8 shrink-0 items-center border-t border-default ps-2.5 pe-1 text-11 font-semibold text-toned">Game files</div>
          <ExplorerTree ref="gameTree" :workspace-id="id" :roots="game" :hide-binaries="hideBinaries" @compare="compare.files" />
        </template>
      </div>
      <SearchView v-show="ide.sidebar === 'search'" :roots="roots" :hide-binaries="hideBinaries" />
    </aside>
    <ResizeGrip v-model="sidebarWidth" :min="170" :max="sidebarMax" :default-size="264" />
    <section class="flex min-w-0 flex-1 flex-col">
      <div class="flex min-h-0 flex-1">
        <EditorLayout :node="ide.layout" :workspace-id="id" />
      </div>
      <template v-if="ide.panel">
        <ResizeGrip v-model="panelHeight" axis="y" reverse :min="120" :max="Math.max(120, height / 2)" :default-size="200" />
        <div class="flex shrink-0 border-t border-default" :style="{ height: `${panelHeight}px` }">
          <IdePanel :workspace-id="id" />
        </div>
      </template>
    </section>
    <ResizeGrip v-show="ide.wikiOpen" v-model="wikiWidth" reverse :min="320" :max="wikiMax" :default-size="440" />
    <WikiPane v-show="ide.wikiOpen" :workspace-id="id" class="shrink-0" :style="{ width: `${Math.min(wikiWidth, wikiMax)}px` }" />
  </div>
  <StatusFacts>
    <button class="flex h-6 items-center gap-1 text-error" :aria-label="plural(counts.errors, 'error')" @click="showProblems">
      <UIcon name="i-lucide-circle-x" class="size-3" />{{ counts.errors }}
    </button>
    <button class="flex h-6 items-center gap-1 text-warning" :aria-label="plural(counts.warnings, 'warning')" @click="showProblems">
      <UIcon name="i-lucide-triangle-alert" class="size-3" />{{ counts.warnings }}
    </button>
  </StatusFacts>
  <StatusFacts v-if="ide.activePath" end>
    <span v-if="ide.caret">Ln {{ ide.caret.line + 1 }}, Col {{ ide.caret.character + 1 }}</span>
    <span>{{ settings.indentSpaces ? `Spaces: ${settings.indentSize}` : "Tabs" }}</span>
    <span>{{ kind }}</span>
    <span v-if="file?.text !== undefined">{{ file.bom ? "UTF-8 BOM" : "UTF-8" }}</span>
  </StatusFacts>
</template>
