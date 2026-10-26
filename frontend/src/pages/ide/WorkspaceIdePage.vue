<!--
  The IDE page, with the sidebar, the editors, the bottom panel and the Wiki pane. A window of its own shows one part
  alone.
-->
<script setup lang="ts">
import { computed, nextTick, reactive, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate, useEventBus, useEventListener, useLocalStorage } from "@vueuse/core";
import type { SplitterItem } from "@nuxt/ui";

import * as workspaceApi from "@services/workspace/service";
import type { Place } from "@services/workspace/models";

import { useShown } from "../../app/pages";
import { countLevels, plural } from "../../lib/format";
import {
  codesQuery,
  gitStatusQuery,
  indentationQuery,
  placeQuery,
  reportQuery,
  rootsQuery,
  settingsCatalogQuery,
  wikiPagesQuery,
  windowsQuery,
} from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSettingsStore } from "../../stores/settings";
import { THIS_WINDOW, useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import FileSelector from "../../ui/FileSelector.vue";
import { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";
import SettingsSection from "../../ui/SettingsSection.vue";
import CoatDialog from "./draw/CoatDialog.vue";
import EditorLayout from "./EditorLayout.vue";
import EntriesView from "./EntriesView.vue";
import ExplorerTree from "./ExplorerTree.vue";
import FileCompare from "./FileCompare.vue";
import IdePanel, { listPanelProblems } from "./IdePanel.vue";
import SearchView from "./SearchView.vue";
import SourceControlView from "./SourceControlView.vue";
import WikiPane from "./WikiPane.vue";
import { LANGUAGES } from "./editor/monaco";
import { findModel } from "./editor/models";
import { parseTab, VIEWS } from "./editor/tabs";

const { id } = defineProps<{ id: string }>();

const PIXELS = "px" as const;
const SIDEBAR = [
  { value: "explorer", icon: "i-lucide-files", label: "Explorer" },
  { value: "search", icon: "i-lucide-search", label: "Search" },
  { value: "git", icon: "i-lucide-git-compare", label: "Source control" },
  { value: "entries", icon: "i-lucide-shapes", label: "Entries" },
];
/** The tabs of the IDE's settings, one for each section of the catalog. */
const SETTINGS_TABS = [
  { value: "editor", label: "Editor" },
  { value: "explorer", label: "Explorer" },
  { value: "language", label: "Language" },
  { value: "checks", label: "Checks" },
  { value: "visual", label: "Visual view" },
  { value: "wiki", label: "Wiki" },
];
const PANEL = [
  { value: "problems", label: "Problems" },
  { value: "search", label: "Search" },
  { value: "output", label: "Output" },
] as const;
const COMPARES = {
  changes: "Comparing the working tree with the staged copy",
  staged: "Comparing the staged copy with the last commit",
  conflict: "Comparing a conflict's two sides",
  commit: "Comparing a commit with its parent",
};
const ENCODINGS: Record<string, string> = {
  "utf-8": "UTF-8",
  "utf-8-bom": "UTF-8 BOM",
  "windows-1252": "Windows-1252",
};
/** The numbers a mouse event gives the back and forward buttons. */
const MOUSE_BACK = 3;
const MOUSE_FORWARD = 4;

const editor = useEditorStore();
const git = useGitStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const toast = useToast();
const coatDialog = useOverlay().create(CoatDialog);
const sidebar = useLocalStorage("pmt.ide.sidebar", "explorer");
const panel = useLocalStorage("pmt.ide.panel", "");
const wiki = useLocalStorage("pmt.ide.wiki", false);
const gameOpen = useLocalStorage("pmt.ide.gameOpen", true);
const settingsOpen = shallowRef(false);
/** Which trees show their filter box. It is never saved. */
const filtering = reactive({ mods: false, game: false, own: false });
const [DefineFilterButton, FilterButton] = createReusableTemplate<{ tree: keyof typeof filtering }>();
const settingsTab = shallowRef<string | number>("editor");
const visible = useShown();
const splitter = useTemplateRef("splitter");
const modTree = useTemplateRef("modTree");
const gameTree = useTemplateRef("gameTree");
const commands = useEventBus<string>("pmt.command");
const comparing = shallowRef<{ left: Place; right: Place }>();
useEventBus<{ left: Place; right: Place }>("pmt.compare").on((pair) => (comparing.value = pair));
commands.on((command) => {
  if (command === "editor.findInFiles") showSidebar("search");
  if (command === "git.commit") showSidebar("git");
  if (command === "editor.toggleWiki") wiki.value = !wiki.value;
  if (command === "draw.newCoat") void coatDialog.open();
  if (command === "ide.settings") settingsOpen.value = true;
  if (command === "explorer.reveal") void revealFile();
});
defineShortcuts(
  computed(() =>
    visible.value
      ? {
          meta_j: { handler: () => (panel.value = panel.value ? "" : "problems"), usingInput: true },
          meta_shift_e: { handler: () => showSidebar("explorer"), usingInput: true },
        }
      : {},
  ),
);
// the mouse's back and forward buttons walk the IDE's history, not the page's
useEventListener(document, "mouseup", (event) => {
  if (!visible.value || (event.button !== MOUSE_BACK && event.button !== MOUSE_FORWARD)) return;
  event.preventDefault();
  editor.go(event.button === MOUSE_BACK ? -1 : 1);
});

/** The part of the IDE this window holds. */
const part = THIS_WINDOW.split(":")[0];
const layout = computed(() => {
  if (part === "panel") return "panel";
  if (part === "wiki") return "wiki";
  if (part === "editor") return "editor";
  return part === "game" || part === "files" ? "files" : "full";
});

const { data: roots } = useQuery(() => ({ ...rootsQuery, enabled: workspace.isOpen() }));
const { data: catalog } = useQuery(settingsCatalogQuery);
const { data: windowNames } = useQuery(windowsQuery);
const { data: folderPlace } = useQuery(() => ({
  ...placeQuery(THIS_WINDOW.slice("files:".length)),
  enabled: part === "files",
}));
const mods = computed(() => (roots.value ?? []).filter((root) => root.origin !== VANILLA));
const game = computed(() => (roots.value ?? []).filter((root) => root.origin === VANILLA));
// the setting alone decides, as a pane the roots bring back while the explorer is hidden gets no room
const gameShown = computed(() => settings.isOn("ide.gameFiles"));
const own = computed(() => {
  if (part === "game") return game.value;
  return folderPlace.value ? [{ dir: folderPlace.value.path, origin: folderPlace.value.origin }] : [];
});

const panes = computed<SplitterItem[]>(() => [
  ...(layout.value === "full" || layout.value === "files"
    ? [
        {
          id: "sidebar",
          slot: "sidebar",
          order: 1,
          sizeUnit: PIXELS,
          defaultSize: 264,
          minSize: 170,
          collapsible: layout.value === "full",
          collapsedSize: 36,
        },
      ]
    : []),
  ...(layout.value === "files" || layout.value === "wiki"
    ? []
    : [{ id: "editors", slot: "editors", order: 2, minSize: 33 }]),
  // the Wiki pane leaves the main window while it has a window of its own
  ...((layout.value === "full" && wiki.value && !windowNames.value?.includes("wiki")) || layout.value === "wiki"
    ? [{ id: "wiki", slot: "wiki", order: 3, sizeUnit: PIXELS, defaultSize: 440, minSize: 320 }]
    : []),
]);
const editorPanes = computed<SplitterItem[]>(() => [
  ...(layout.value === "panel" ? [] : [{ id: "code", slot: "code", order: 1, minSize: 50 }]),
  ...(layout.value === "panel" || panel.value
    ? [
        {
          id: "panel",
          slot: "panel",
          order: 2,
          sizeUnit: PIXELS,
          defaultSize: 200,
          minSize: 120,
          collapsible: layout.value === "full",
          collapsedSize: 0,
        },
      ]
    : []),
]);
// the panel opens again at the size it had when it was dragged closed
const editorSplitter = useTemplateRef("editorSplitter");
watch(panel, (tab) => tab && void nextTick(() => editorSplitter.value?.panelsRef.at(-1)?.expand()));
const explorerPanes = computed<SplitterItem[]>(() => [
  {
    id: "mods",
    slot: "mods",
    order: 1,
    ...(gameShown.value && gameOpen.value ? { sizeUnit: PIXELS, defaultSize: 320, minSize: 120 } : {}),
  },
  ...(!gameShown.value
    ? []
    : gameOpen.value
      ? [{ id: "game", slot: "game", order: 2, sizeUnit: PIXELS, minSize: 120 }]
      : [{ id: "folded", slot: "game", order: 2, sizeUnit: PIXELS, defaultSize: 33, minSize: 33, maxSize: 33 }]),
]);
const panelTab = computed(() => PANEL.find((tab) => tab.value === panel.value)?.value ?? "problems");

// the status bar counts the panel's problems. It only reads Health's report, as building one checks every mod
const { data: codes } = useQuery(codesQuery);
const { data: report } = useQuery(() => ({ ...reportQuery(workspace.listModIds()), enabled: false }));
const problems = computed(() =>
  countLevels(listPanelProblems(codes.value ?? [], report.value?.findings ?? []).flatMap((file) => file.problems)),
);

const { data: status } = useQuery(() => ({
  ...gitStatusQuery(git.findRoot(editor.activeFile())),
  enabled: !!git.findRoot(editor.activeFile()),
}));
const { data: indentation } = useQuery(() => ({
  ...indentationQuery(editor.activeFile()?.path ?? ""),
  enabled: !!editor.activeFile() && VIEWS[editor.activeFile()?.format ?? ""].views.includes("code"),
}));
const open = computed(() => {
  const active = editor.activeFile();
  return active ? findModel(active.key) : null;
});
const languageName = computed(
  () => LANGUAGES.find((one) => one.id === EDITOR_LANGUAGES[open.value?.place.format ?? ""])?.label ?? "Plain text",
);
const diff = computed(() => {
  const target = parseTab(editor.activeTab()?.id ?? "");
  return target.tab === "diff" ? target.side : null;
});

// while the Wiki pane is closed, a kind of file the wiki has pages for is suggested once
const suggestions = new Set<string>();
const isSuggesting = computed(() => !wiki.value && settings.isOn("ide.wikiToast"));
const { data: pages } = useQuery(() => ({
  ...wikiPagesQuery(editor.activeFile()?.path ?? ""),
  enabled: !!editor.activeFile() && workspace.isOpen() && isSuggesting.value,
}));
watch(pages, (summaries) => {
  const active = editor.activeFile();
  const kind = active ? `${active.format}:${active.database}` : "";
  // a query turned off still hands back its pages
  if (!summaries?.length || !kind || suggestions.has(kind) || !isSuggesting.value) return;
  suggestions.add(kind);
  toast.add({
    title: `Wiki: ${summaries[0]?.title}`,
    actions: [{ label: "Open", color: "neutral", variant: "outline", onClick: () => (wiki.value = true) }],
  });
});
watch(
  () => id,
  () => suggestions.clear(),
);

const { mutate: addMod } = useMutation({ mutation: (dir: string) => workspaceApi.AddMod(id, dir) });

/** show opens a place from the explorer, in a preview tab or a pinned one. */
function show(place: Place, preview: boolean): void {
  editor.show(place, undefined, preview ? "explorer" : "navigate");
}

/** popOutPanel puts the panel in a window of its own. */
function popOutPanel(): void {
  windows.popOut({ panel: true });
  panel.value = "";
}

/** showSidebar shows a sidebar tab, unfolding the sidebar if it is folded. */
function showSidebar(value: string): void {
  sidebar.value = value;
  splitter.value?.panelsRef[0]?.expand();
}

/** choose shows a sidebar tab, or folds the sidebar when its open tab is chosen again. */
function choose(value: string, collapsed: boolean, collapse: () => void): void {
  if (!collapsed && sidebar.value === value) collapse();
  else showSidebar(value);
}

/** resetSettings puts every setting of the open settings tab back to its default for this workspace. */
function resetSettings(): void {
  for (const row of catalog.value ?? []) if (row.section === settingsTab.value) settings.write(row.key, row.default);
}

/** toggleFilter shows or hides a tree's filter box. Showing the game files' box unfolds them. */
function toggleFilter(tree: keyof typeof filtering): void {
  filtering[tree] = !filtering[tree];
  if (tree === "game" && filtering.game) gameOpen.value = true;
}

/** toggleProblems shows the panel on Problems, or closes it if it shows Problems already. */
function toggleProblems(): void {
  panel.value = panel.value === "problems" ? "" : "problems";
}

/** collapseAll folds every folder of both explorers. */
function collapseAll(): void {
  modTree.value?.collapseAll();
  gameTree.value?.collapseAll();
}

/** revealFile selects the active file in the explorer. It unfolds the game's files for a file of the game. */
async function revealFile(): Promise<void> {
  showSidebar("explorer");
  if (editor.activeFile()?.readOnly) gameOpen.value = true;
  await nextTick();
  await Promise.all([modTree.value?.reveal(), gameTree.value?.reveal()]);
}
</script>

<template>
  <div class="flex h-full min-h-0">
    <DefineFilterButton v-slot="{ tree }">
      <UTooltip text="Filter files">
        <UButton
          icon="i-lucide-list-filter"
          variant="ghost"
          size="xs"
          square
          :color="filtering[tree] ? 'primary' : 'neutral'"
          aria-label="Filter files"
          :aria-pressed="filtering[tree]"
          @click="toggleFilter(tree)"
        />
      </UTooltip>
    </DefineFilterButton>
    <USplitter ref="splitter" :items="panes" auto-save-id="pmt.ide.panes" class="min-w-0 flex-1">
      <template #sidebar="{ collapsed, collapse }">
        <aside
          v-if="layout === 'full'"
          class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted text-(length:--ide-ui-font)"
        >
          <div
            class="flex shrink-0 items-center gap-0.5"
            :class="collapsed ? 'flex-col px-1 py-1.5' : 'h-9 border-b border-default px-1.5'"
          >
            <UTooltip v-for="tab in SIDEBAR" :key="tab.value" :text="tab.label" :content="{ side: 'right' }">
              <UButton
                :icon="tab.icon"
                square
                :ui="{ leadingIcon: 'size-4.5' }"
                :aria-label="tab.label"
                :color="!collapsed && sidebar === tab.value ? 'primary' : 'neutral'"
                :variant="!collapsed && sidebar === tab.value ? 'soft' : 'ghost'"
                @click="choose(tab.value, collapsed, collapse)"
              />
            </UTooltip>
            <span class="grow" />
            <span id="sidebar-actions" class="contents" />
            <UTooltip v-if="!collapsed && sidebar === 'explorer'" text="Collapse all">
              <UButton
                icon="i-lucide-chevrons-down-up"
                variant="ghost"
                square
                :ui="{ leadingIcon: 'size-4.5' }"
                aria-label="Collapse all"
                @click="collapseAll"
              />
            </UTooltip>
            <UButton
              icon="i-lucide-settings"
              square
              variant="ghost"
              :ui="{ leadingIcon: 'size-4.5' }"
              aria-label="IDE settings"
              @click="settingsOpen = true"
            />
          </div>
          <!-- the explorer turns invisible, not hidden, behind another view, so its splitter keeps its height -->
          <div v-if="!collapsed" class="relative flex min-h-0 flex-1 flex-col">
            <USplitter
              :items="explorerPanes"
              orientation="vertical"
              :disabled="!gameOpen"
              auto-save-id="pmt.ide.explorer"
              class="absolute inset-0 py-1"
              :class="sidebar !== 'explorer' && 'invisible'"
            >
              <template #mods>
                <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                  <div
                    class="flex h-7 shrink-0 items-center gap-1 ps-2.5 pe-1 text-11 font-semibold whitespace-nowrap text-toned"
                  >
                    Workspace<span class="ms-1 font-normal text-dimmed">{{ plural(mods.length, "mod") }}</span>
                    <span class="grow" />
                    <FilterButton tree="mods" />
                    <FileSelector path="" folder :filter="[]" @update:path="addMod">
                      <template #default="{ browse }">
                        <UTooltip text="Add a mod">
                          <UButton
                            icon="i-lucide-plus"
                            variant="ghost"
                            size="xs"
                            square
                            aria-label="Add a mod"
                            @click="browse"
                          />
                        </UTooltip>
                      </template>
                    </FileSelector>
                  </div>
                  <ExplorerTree ref="modTree" v-model:filtering="filtering.mods" :roots="mods" @open="show" />
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
                      <UIcon
                        :name="gameOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                        class="size-3.5"
                      />Game files
                    </button>
                    <FilterButton v-if="!windowNames?.includes('game')" tree="game" />
                    <UTooltip text="Open in its own window">
                      <UButton
                        icon="i-lucide-app-window"
                        variant="ghost"
                        size="xs"
                        square
                        aria-label="Open in its own window"
                        @click="windows.popOut({ game: true })"
                      />
                    </UTooltip>
                  </div>
                  <div
                    v-if="windowNames?.includes('game')"
                    v-show="gameOpen"
                    class="flex flex-col gap-2 px-3 py-1.5 text-12 text-dimmed"
                  >
                    In its own window.
                    <div class="flex gap-1.5">
                      <UButton label="Bring back" size="xs" variant="soft" @click="windows.closeOther('game')" />
                      <UButton label="Show window" size="xs" variant="ghost" @click="windows.showWindow('game')" />
                    </div>
                  </div>
                  <ExplorerTree
                    v-else
                    v-show="gameOpen"
                    ref="gameTree"
                    v-model:filtering="filtering.game"
                    :roots="game"
                    @open="show"
                  />
                </div>
              </template>
            </USplitter>
            <SearchView v-show="sidebar === 'search'" />
            <SourceControlView v-if="sidebar === 'git'" />
            <EntriesView v-if="sidebar === 'entries'" />
          </div>
        </aside>
        <aside v-else class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted pb-1">
          <div
            class="flex h-7 shrink-0 items-center gap-1 ps-2.5 pe-1 text-11 font-semibold whitespace-nowrap text-toned"
          >
            {{ part === "game" ? "Game files" : folderPlace?.name }}
            <span class="grow" />
            <FilterButton tree="own" />
          </div>
          <ExplorerTree v-model:filtering="filtering.own" :roots="own" @open="show" />
        </aside>
      </template>
      <template #editors>
        <USplitter
          ref="editorSplitter"
          :items="editorPanes"
          orientation="vertical"
          auto-save-id="pmt.ide.editors"
          class="min-w-0 flex-1"
          @collapse="(index) => editorPanes[index]?.id === 'panel' && (panel = '')"
        >
          <template #code>
            <div class="flex min-h-0 min-w-0 flex-1">
              <EditorLayout />
            </div>
          </template>
          <template #panel>
            <div class="flex min-h-0 min-w-0 flex-1 flex-col border-t border-default bg-muted">
              <div class="flex h-8 shrink-0 items-center gap-0.5 border-b border-default ps-2 pe-1.5">
                <UTabs
                  :model-value="panelTab"
                  :items="[...PANEL]"
                  :content="false"
                  variant="link"
                  size="sm"
                  @update:model-value="panel = String($event)"
                >
                  <template #trailing="{ item }">
                    <template v-if="item.value === 'problems'">
                      <UBadge v-if="problems.serious" :label="String(problems.serious)" color="error" size="sm" />
                      <UBadge v-if="problems.warning" :label="String(problems.warning)" color="warning" size="sm" />
                    </template>
                  </template>
                </UTabs>
                <span class="grow" />
                <span id="panel-actions" class="contents" />
                <UTooltip v-if="layout === 'full'" text="Open in its own window">
                  <UButton
                    icon="i-lucide-app-window"
                    variant="ghost"
                    size="sm"
                    square
                    aria-label="Open in its own window"
                    @click="popOutPanel"
                  />
                </UTooltip>
                <UButton
                  icon="i-lucide-x"
                  variant="ghost"
                  size="sm"
                  square
                  :aria-label="layout === 'full' ? 'Close the panel' : 'Back into the main window'"
                  @click="layout === 'full' ? (panel = '') : windows.dockBack()"
                />
              </div>
              <IdePanel :tab="panelTab" />
            </div>
          </template>
        </USplitter>
      </template>
      <template #wiki>
        <!-- in a window of its own, the Wiki pane follows the main window's active file -->
        <WikiPane
          :place="layout === 'wiki' ? windows.findActive('main') : editor.activeFile()"
          class="min-w-0 flex-1"
        />
      </template>
    </USplitter>
    <Teleport v-if="!['panel', 'wiki'].includes(layout) && visible" defer to="#status-start">
      <button class="flex items-center gap-1 text-error" @click="toggleProblems">
        <UIcon name="i-lucide-circle-x" class="size-3" />{{ problems.serious }}
      </button>
      <button class="flex items-center gap-1 text-warning" @click="toggleProblems">
        <UIcon name="i-lucide-triangle-alert" class="size-3" />{{ problems.warning }}
      </button>
      <button v-if="status" class="flex items-center gap-1 hover:text-highlighted" @click="showSidebar('git')">
        <UIcon name="i-lucide-git-branch" class="size-3" />{{ status.branch || "detached HEAD" }}
        <template v-if="status.upstream">
          <UIcon name="i-lucide-arrow-down-up" class="size-3" />{{ status.ahead }} up, {{ status.behind }} down
        </template>
      </button>
    </Teleport>
    <Teleport v-if="!['panel', 'wiki'].includes(layout) && visible" defer to="#status-end">
      <span v-if="diff">{{ COMPARES[diff] }}</span>
      <template v-else-if="open">
        <span>Ln {{ open.caret.line }}, Col {{ open.caret.column }}</span>
        <span v-if="indentation">{{ indentation }}</span>
        <span>{{ languageName }}</span>
        <span>{{ ENCODINGS[open.encoding] ?? open.encoding }}</span>
      </template>
    </Teleport>
    <UModal :open="!!comparing" title="Compare" fullscreen @update:open="comparing = undefined">
      <template #body>
        <FileCompare v-if="comparing" :left="comparing.left" :right="comparing.right" />
      </template>
    </UModal>
    <UModal
      v-model:open="settingsOpen"
      title="IDE settings"
      description="Kept for this workspace."
      :ui="{ content: 'h-170 sm:max-w-4xl', body: 'flex min-h-0 flex-col p-0 sm:p-0' }"
    >
      <template #body>
        <UTabs
          v-model="settingsTab"
          :items="SETTINGS_TABS"
          variant="link"
          :ui="{ root: 'min-h-0 flex-1 gap-0', list: 'px-4', content: 'min-h-0 flex-1 overflow-y-auto px-5' }"
        >
          <template #content="{ item }">
            <SettingsSection :section="String(item.value)" />
          </template>
        </UTabs>
      </template>
      <template #footer>
        <span class="grow" />
        <UButton
          :label="settingsTab === 'checks' ? 'Reset the checks' : 'Reset this tab'"
          variant="ghost"
          @click="resetSettings"
        />
      </template>
    </UModal>
  </div>
</template>
