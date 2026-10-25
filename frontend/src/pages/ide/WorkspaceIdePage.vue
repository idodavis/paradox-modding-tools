<!--
  The IDE page: the sidebar, the editor layout, the bottom panel and the Wiki pane in collapsible panes. Their sizes
  and folds belong to the splitter, not to the store. A window that holds one part of the IDE shows that part alone.
-->
<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { useEventBus, useLocalStorage } from "@vueuse/core";
import { Dialogs } from "@wailsio/runtime";
import { editor as monacoEditor, MarkerSeverity } from "monaco-editor/editor/editor.api";
import type { SplitterItem } from "@nuxt/ui";

import * as app from "@services/app/service";
import * as workspaceApi from "@services/workspace/service";
import type { Place } from "@services/workspace/models";

import { plural } from "../../lib/format";
import {
  fileQuery,
  gitStatusQuery,
  indentationQuery,
  placeQuery,
  rootsQuery,
  settingsCatalogQuery,
  stateQuery,
  wikiPagesQuery,
  windowsQuery,
} from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSettingsStore } from "../../stores/settings";
import { THIS_WINDOW, useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import SettingsSection from "../../ui/SettingsSection.vue";
import CoatDialog from "./draw/CoatDialog.vue";
import EditorLayout from "./EditorLayout.vue";
import EntriesView from "./EntriesView.vue";
import ExplorerTree from "./ExplorerTree.vue";
import IdePanel from "./IdePanel.vue";
import SearchView from "./SearchView.vue";
import SourceControlView from "./SourceControlView.vue";
import WikiPane from "./WikiPane.vue";
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
/** The tabs of the IDE's settings, by the catalog's section of each. */
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
const FORMATS: Record<string, string> = {
  script: "Paradox Script",
  gui: "Paradox GUI",
  localization: "Paradox localization",
};
const ENCODINGS: Record<string, string> = {
  "utf-8": "UTF-8",
  "utf-8-bom": "UTF-8 BOM",
  "windows-1252": "Windows-1252",
};

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
const settingsTab = shallowRef<string | number>("editor");
const visible = shallowRef(true);
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
});
onActivated(() => (visible.value = true));
onDeactivated(() => (visible.value = false));
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

// a window of its own holds one part of the IDE: the editors, the game's files, one folder or the panel
const part = THIS_WINDOW.split(":")[0];
const layout = computed(() => {
  if (part === "panel") return "panel";
  if (part === "editor") return "editor";
  return part === "game" || part === "files" ? "files" : "full";
});

const { data: roots } = useQuery(() => ({ ...rootsQuery, enabled: workspace.isOpen() }));
const { data: catalog } = useQuery(settingsCatalogQuery);
const { data: state } = useQuery(stateQuery);
const { data: windowNames } = useQuery(windowsQuery);
const { mutate: closeGame } = useMutation({ mutation: () => app.CloseWindow("game") });
const { mutate: showGame } = useMutation({ mutation: () => app.ShowWindow("game") });
const { data: folderPlace } = useQuery(() => ({
  ...placeQuery(THIS_WINDOW.slice("files:".length)),
  enabled: part === "files",
}));
const modOrigins = computed(() => new Set((state.value?.mods ?? []).map((mod) => mod.id)));
const mods = computed(() => (roots.value ?? []).filter((root) => modOrigins.value.has(root.origin)));
const game = computed(() => (roots.value ?? []).filter((root) => !modOrigins.value.has(root.origin)));
// the setting alone decides, not the roots: they are read again on a switch of workspace, and a pane the splitter
// gets back while the explorer is hidden is given no room
const gameShown = computed(() => settings.value("ide.gameFiles") === "true");
const own = computed(() => {
  if (part === "game") return game.value;
  const dir = THIS_WINDOW.slice("files:".length);
  return folderPlace.value ? [{ dir, origin: folderPlace.value.origin }] : [];
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
  ...(layout.value === "files" ? [] : [{ id: "editors", slot: "editors", order: 2, minSize: 33 }]),
  ...(layout.value === "full" && wiki.value
    ? [{ id: "wiki", slot: "wiki", order: 3, sizeUnit: PIXELS, defaultSize: 440, minSize: 320 }]
    : []),
]);
const editorPanes = computed<SplitterItem[]>(() => [
  ...(layout.value === "panel" ? [] : [{ id: "code", slot: "code", order: 1, minSize: 50 }]),
  ...(layout.value === "panel" || panel.value
    ? [{ id: "panel", slot: "panel", order: 2, sizeUnit: PIXELS, defaultSize: 200, minSize: 120 }]
    : []),
]);
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

// the problems of the open files are Monaco's markers
const markersChanged = shallowRef(0);
const subscription = monacoEditor.onDidChangeMarkers(() => markersChanged.value++);
onBeforeUnmount(() => subscription.dispose());
const problems = computed(() => {
  void markersChanged.value;
  const markers = monacoEditor.getModelMarkers({});
  return {
    errors: markers.filter((marker) => marker.severity === MarkerSeverity.Error).length,
    warnings: markers.filter((marker) => marker.severity === MarkerSeverity.Warning).length,
  };
});

const { data: status } = useQuery(() => ({
  ...gitStatusQuery(git.repository()),
  enabled: !!git.repository(),
}));
const { data: indentation } = useQuery(() => ({
  ...indentationQuery(editor.activeFile()?.path ?? ""),
  enabled: !!editor.activeFile() && VIEWS[editor.activeFile()?.format ?? ""].views.includes("code"),
}));
const open = computed(() => {
  const active = editor.activeFile();
  return active ? findModel(active.key) : null;
});
const diff = computed(() => {
  const target = parseTab(editor.activeTab()?.id ?? "");
  return target.tab === "diff" ? target.side : null;
});
const left = useQuery(() => ({
  ...fileQuery(comparing.value?.left.path ?? ""),
  enabled: !!comparing.value,
}));
const right = useQuery(() => ({
  ...fileQuery(comparing.value?.right.path ?? ""),
  enabled: !!comparing.value,
}));

// while the Wiki pane is closed and the setting asks for it, a kind of file the wiki has pages for is suggested once;
// a query turned off still hands back the pages it holds, so the toast asks the same
const suggestions = new Set<string>();
const isSuggesting = computed(() => !wiki.value && settings.value("ide.wikiToast") === "true");
const { data: pages } = useQuery(() => ({
  ...wikiPagesQuery(editor.activeFile()?.path ?? ""),
  enabled: !!editor.activeFile() && workspace.isOpen() && isSuggesting.value,
}));
watch(pages, (summaries) => {
  const active = editor.activeFile();
  const kind = active ? `${active.format}:${active.database}` : "";
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

const { mutate: addMod } = useMutation({
  mutation: async () => {
    const dir = await Dialogs.OpenFile({ CanChooseFiles: false, CanChooseDirectories: true });
    if (dir) await workspaceApi.AddMod(id, dir);
  },
});

/** Shows a file the explorer opened. A single click opens a preview tab and a double click pins the tab. */
function show(place: Place, preview: boolean): void {
  editor.show(place, undefined, preview ? "explorer" : "navigate");
}

/** Puts the panel in a window of its own. */
function popOutPanel(): void {
  windows.popOut({ panel: true });
  panel.value = "";
}

/** Shows a sidebar tab, unfolding the sidebar if it is folded. */
function showSidebar(value: string): void {
  sidebar.value = value;
  splitter.value?.panelsRef[0]?.expand();
}

/** Chooses a sidebar tab, or folds the sidebar when its open tab is chosen again. */
function choose(value: string, collapsed: boolean, collapse: () => void): void {
  if (!collapsed && sidebar.value === value) collapse();
  else showSidebar(value);
}

/** Puts every setting of the open settings tab back to its default for this workspace. */
function resetSettings(): void {
  for (const row of catalog.value ?? []) if (row.section === settingsTab.value) settings.write(row.key, row.default);
}

/** Folds every folder of both explorers. */
function collapseAll(): void {
  modTree.value?.collapseAll();
  gameTree.value?.collapseAll();
}

/** Names a place for a compare: its origin and its path in it. */
const describePlace = (place: Place) => `${workspace.originName(place.origin)} · ${place.rel}`;
</script>

<template>
  <div class="flex h-full min-h-0">
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
          <!-- the explorer only turns invisible behind another view: its splitter sizes its panes in pixels from its own
               height, which an element taken out of the layout does not have -->
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
                    <UTooltip text="Add a mod">
                      <UButton
                        icon="i-lucide-plus"
                        variant="ghost"
                        size="xs"
                        square
                        aria-label="Add a mod"
                        @click="addMod()"
                      />
                    </UTooltip>
                  </div>
                  <ExplorerTree ref="modTree" :roots="mods" @open="show" />
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
                      <UButton label="Bring back" size="xs" variant="soft" @click="closeGame()" />
                      <UButton label="Show window" size="xs" variant="ghost" @click="showGame()" />
                    </div>
                  </div>
                  <ExplorerTree v-else v-show="gameOpen" ref="gameTree" :roots="game" @open="show" />
                </div>
              </template>
            </USplitter>
            <SearchView v-show="sidebar === 'search'" />
            <SourceControlView v-if="sidebar === 'git'" />
            <EntriesView v-if="sidebar === 'entries'" />
          </div>
        </aside>
        <aside v-else class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted py-1">
          <ExplorerTree :roots="own" @open="show" />
        </aside>
      </template>
      <template #editors>
        <USplitter :items="editorPanes" orientation="vertical" auto-save-id="pmt.ide.editors" class="min-w-0 flex-1">
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
                      <UBadge v-if="problems.errors" :label="String(problems.errors)" color="error" size="sm" />
                      <UBadge v-if="problems.warnings" :label="String(problems.warnings)" color="warning" size="sm" />
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
        <WikiPane :place="editor.activeFile()" class="min-w-0 flex-1" />
      </template>
    </USplitter>
    <Teleport v-if="layout !== 'panel' && visible" defer to="#status-start">
      <button class="flex items-center gap-1 text-error" @click="panel = 'problems'">
        <UIcon name="i-lucide-circle-x" class="size-3" />{{ problems.errors }}
      </button>
      <button class="flex items-center gap-1 text-warning" @click="panel = 'problems'">
        <UIcon name="i-lucide-triangle-alert" class="size-3" />{{ problems.warnings }}
      </button>
      <button v-if="status" class="flex items-center gap-1 hover:text-highlighted" @click="showSidebar('git')">
        <UIcon name="i-lucide-git-branch" class="size-3" />{{ status.branch || "detached HEAD" }}
        <template v-if="status.upstream">
          <UIcon name="i-lucide-arrow-down-up" class="size-3" />{{ status.ahead }} up, {{ status.behind }} down
        </template>
      </button>
    </Teleport>
    <Teleport v-if="layout !== 'panel' && visible" defer to="#status-end">
      <span v-if="diff">{{ COMPARES[diff] }}</span>
      <template v-else-if="open">
        <span>Ln {{ open.caret.line }}, Col {{ open.caret.column }}</span>
        <span v-if="indentation">{{ indentation }}</span>
        <span>{{ FORMATS[open.place.format] ?? "Plain text" }}</span>
        <span>{{ ENCODINGS[open.encoding] ?? open.encoding }}</span>
      </template>
    </Teleport>
    <UModal :open="!!comparing" title="Compare" fullscreen @update:open="comparing = undefined">
      <template #body>
        <div class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-default">
          <p v-if="left.isPending.value || right.isPending.value" class="p-4 text-12 text-dimmed">
            Reading the two files…
          </p>
          <p v-else-if="comparing && (left.error.value || right.error.value)" class="p-4 text-12 text-dimmed">
            {{ describePlace(left.error.value ? comparing.left : comparing.right) }} could not be read.
          </p>
          <CompareView
            v-else-if="comparing && left.data.value && right.data.value"
            :left="left.data.value.text.text"
            :right="right.data.value.text.text"
            :language="VIEWS[comparing.right.format].language"
            :left-label="describePlace(comparing.left)"
            :right-label="describePlace(comparing.right)"
          />
        </div>
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
