<!-- The IDE page: the sidebar, the editors, the bottom panel and the Wiki pane. A pop-out shows some of them. -->
<script setup lang="ts">
import { computed, nextTick, reactive, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useEventBus, useEventListener, useLocalStorage } from "@vueuse/core";
import { countBy } from "es-toolkit";
import { FILE_TREE_DENSITY_PRESETS, type FileTreeDensityKeyword } from "@pierre/trees";
import type { SplitterItem } from "@nuxt/ui";

import type { Diagnostic, Level } from "@services/lsp/models";
import type { Chip } from "@services/mcp/models";
import type { Place } from "@services/workspace/models";

import { PAGES, useShown } from "../../app/pages";
import { joinParts, matchQuery, plural } from "../../lib/format";
import {
  codesQuery,
  fileQuery,
  gitStatusQuery,
  indentationQuery,
  levelsQuery,
  placeQuery,
  reportQuery,
  rootsQuery,
} from "../../queries";
import { useChip } from "../../stores/toolView";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSettingsStore } from "../../stores/settings";
import { useTerminalStore } from "../../stores/terminal";
import { COMMAND_BUS, describeWindow, THIS_WINDOW, useWindowsStore } from "../../stores/windows";
import { deleteLayouts, LAYOUT_KEY, useWorkspaceStore, VANILLA } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import AddModMenu from "../../ui/AddModMenu.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ProblemRow from "../../ui/ProblemRow.vue";
import { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";
import { STATUS_ITEM } from "../../ui/StatusFacts.vue";
import CoatDialog from "./draw/CoatDialog.vue";
import SaveDdsModal from "./SaveDdsModal.vue";
import EditorGroup from "./EditorGroup.vue";
import EntriesView from "./EntriesView.vue";
import ExplorerTree from "./ExplorerTree.vue";
import SearchView from "./SearchView.vue";
import SourceControlView from "./SourceControlView.vue";
import TerminalPanel from "./terminal/TerminalPanel.vue";
import WikiPane from "./WikiPane.vue";
import { describeLanguage } from "./editor/monaco";
import { findModel } from "./editor/models";

// The route names the workspace; the page reads the open one from the workspace store.
defineProps<{ id: string }>();

/** A Problem is one entry in the problems list: an editor diagnostic, or a Health finding marked with `health`. */
type Problem = { problem: Diagnostic; health?: true };
/** A Row is one row of the problems list: a file with its problem count, or one of its problems. */
type Row = { place: Place; count: number } | ({ place: Place } & Problem);

const PIXELS = "px" as const;
/** The sidebar's tabs. A tab with a `visual` label also shows in Visual, under that label. */
const SIDEBAR: { value: string; icon: string; label: string; visual?: string }[] = [
  { value: "explorer", icon: "i-lucide-files", label: "Explorer", visual: "Explorer" },
  { value: "search", icon: "i-lucide-search", label: "Search" },
  { value: "git", icon: "i-lucide-git-compare", label: "Source control" },
  { value: "entries", icon: "i-lucide-shapes", label: "Entries", visual: "Structure" },
];
const PANEL = [
  { value: "problems", label: "Problems" },
  { value: "output", label: "Output" },
  { value: "terminal", label: "Terminal" },
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
/** MOUSE_BACK and MOUSE_FORWARD are the button numbers of the mouse's back and forward buttons. */
const MOUSE_BACK = 3;
const MOUSE_FORWARD = 4;

const editor = useEditorStore();
const git = useGitStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const coatDialog = useOverlay().create(CoatDialog);
const saveDds = useOverlay().create(SaveDdsModal);
const sidebar = useLocalStorage("pmt.ide.sidebar", "explorer");
const isVisual = computed(() => editor.mode === "visual");
// A saved tab that Visual hides falls back to Entries.
const modeTabs = computed(() =>
  isVisual.value ? SIDEBAR.flatMap(({ visual, ...tab }) => (visual ? [{ ...tab, label: visual }] : [])) : SIDEBAR,
);
const shownSidebar = computed(() =>
  modeTabs.value.some((tab) => tab.value === sidebar.value) ? sidebar.value : "entries",
);
// The simple interface leaves out the tabs it hides, unless one is the open one.
const tabs = computed(() =>
  modeTabs.value.filter((tab) => tab.value === shownSidebar.value || !settings.isHidden(tab.value)),
);
const visualGroup = computed(() => (isVisual.value ? editor.activeGroup() : null));
const gameOpen = useLocalStorage("pmt.ide.gameOpen", true);
const externalOpen = useLocalStorage("pmt.ide.externalOpen", true);
/** filtering says which trees show their filter box. */
const filtering = reactive({ mods: false, own: false });
const [DefineFilterButton, FilterButton] = createReusableTemplate<{ tree: keyof typeof filtering }>();
const visible = useShown();
// The active file and its selected lines go with an agent's prompt as a chip. While the active tab is one Follow the
// agent opened, the chip keeps the file the user had before.
const fileChip = computed<Chip | null>((previous) => {
  if (editor.isFollowed()) return previous ?? null;
  const place = editor.activeFile();
  const lines = place && findModel(place.key)?.lines;
  return place
    ? { label: place.name, path: place.path, startLine: lines?.start ?? 0, endLine: lines?.end ?? 0, text: "" }
    : null;
});
useChip(() => fileChip.value);
const splitter = useTemplateRef("splitter");
const modTree = useTemplateRef("modTree");
const externalTree = useTemplateRef("externalTree");
const gameTree = useTemplateRef("gameTree");
const commandBus = useEventBus(COMMAND_BUS);
const comparing = shallowRef<{ left: Place; right: Place }>();
useEventBus<{ left: Place; right: Place }>("pmt.compare").on((pair) => (comparing.value = pair));
commandBus.on((command) => {
  if (command === "editor.findInFiles") showSidebar("search");
  if (command === "git.commit") showSidebar("git");
  if (command === "editor.toggleWiki") toggleWiki();
  if (command === "editor.togglePanel") togglePanel();
  if (command === "draw.newCoat") void coatDialog.open();
  if (command === "image.saveDds") {
    const active = editor.activeFile();
    if (active) void saveDds.open({ place: active });
  }
  if (command === "explorer.reveal") {
    showSidebar("explorer");
    if (editor.activeFile()?.origin === VANILLA) gameOpen.value = true;
    void nextTick().then(() =>
      Promise.all([modTree.value?.reveal(), externalTree.value?.reveal(), gameTree.value?.reveal()]),
    );
  }
  if (command === "terminal.sendFile") {
    if (layout.value === "full") editor.showPanel("terminal");
    useTerminalStore().sendFile();
  }
  if (command === "terminal.show" && layout.value === "full") editor.showPanel("terminal");
});
defineShortcuts(
  computed(() => (visible.value ? { meta_shift_e: { handler: () => showSidebar("explorer"), usingInput: true } } : {})),
);
// Following the active file unfolds Game files when the file is one of the game's.
watch(
  () => editor.activeFile()?.key,
  () => {
    if (editor.activeFile()?.origin === VANILLA && settings.isOn("ide.followFile")) gameOpen.value = true;
  },
);
// The mouse's back and forward buttons walk the IDE's history, not the page's.
useEventListener(document, "mouseup", (event) => {
  if (!visible.value || (event.button !== MOUSE_BACK && event.button !== MOUSE_FORWARD)) return;
  event.preventDefault();
  editor.go(event.button === MOUSE_BACK ? -1 : 1);
});

const part = THIS_WINDOW.split(":")[0];
const layout = computed(() => {
  if (part === "terminal") return "terminal";
  if (part === "wiki") return "wiki";
  if (part === "editor") return "editor";
  return part === "game" || part === "external" || part === "files" ? "files" : "full";
});

const { data: roots } = useQuery(() => ({ ...rootsQuery, enabled: workspace.isOpen() }));
const { data: folderPlace } = useQuery(() => ({
  ...placeQuery(THIS_WINDOW.slice("files:".length)),
  enabled: part === "files",
}));
const mods = computed(() =>
  (roots.value ?? []).filter((root) => root.origin !== VANILLA && !workspace.isExternal(root.origin)),
);
const externals = computed(() => (roots.value ?? []).filter((root) => workspace.isExternal(root.origin)));
const game = computed(() => (roots.value ?? []).filter((root) => root.origin === VANILLA));
// Only the setting decides, because a pane that appears as the roots load, while the explorer is hidden, gets no room.
const gameShown = computed(() => settings.isOn("ide.gameFiles"));
const own = computed(() => {
  if (part === "game") return game.value;
  if (part === "external") return externals.value;
  return folderPlace.value ? [folderPlace.value] : [];
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
  ...(layout.value === "wiki" ? [] : [{ id: "editors", slot: "editors", order: 2, minSize: 33 }]),
  // The Wiki pane leaves the main window while it has a window of its own, and while Visual takes the screen.
  ...((layout.value === "full" && editor.wiki && !isVisual.value && !windows.listWindows().includes("wiki")) ||
  layout.value === "wiki"
    ? [{ id: "wiki", slot: "wiki", order: 3, sizeUnit: PIXELS, defaultSize: 440, minSize: 320 }]
    : []),
]);
const editorPanes = computed<SplitterItem[]>(() => [
  ...(layout.value === "terminal" ? [] : [{ id: "code", slot: "code", order: 1, minSize: 50 }]),
  // The terminal's own window shows the panel on the Terminal tab alone, and Visual hides the panel.
  ...(layout.value === "terminal" || (editor.panel && !isVisual.value)
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
const editorSplitter = useTemplateRef("editorSplitter");
watch(
  () => editor.panel,
  (tab) => tab && void nextTick(() => editorSplitter.value?.panelsRef.at(-1)?.expand()),
);
// The Workspace section opens as high as its header and a row for each mod, so External mods sits right under it.
const modsHeight = computed(() => {
  const row = FILE_TREE_DENSITY_PRESETS[settings.value("ide.density") as FileTreeDensityKeyword].itemHeight;
  return Math.min(320, settings.toPixels(2.25) + mods.value.length * row);
});
const explorerPanes = computed<SplitterItem[]>(() => [
  {
    id: "mods",
    slot: "mods",
    order: 1,
    ...(gameShown.value && gameOpen.value
      ? { sizeUnit: PIXELS, defaultSize: modsHeight.value, minSize: Math.min(120, modsHeight.value) }
      : {}),
  },
  ...(!externals.value.length
    ? []
    : externalOpen.value
      ? [{ id: "external", slot: "external", order: 2, sizeUnit: PIXELS, defaultSize: 160, minSize: 90 }]
      : [foldPane("external-folded", "external", 2)]),
  ...(!gameShown.value
    ? []
    : gameOpen.value
      ? [{ id: "game", slot: "game", order: 3, sizeUnit: PIXELS, minSize: 120 }]
      : [foldPane("folded", "game", 3)]),
]);
// The terminal's own window shows the Terminal tab alone. The simple interface leaves out the tabs it hides, unless
// one is the open one.
const panelTabs = computed(() =>
  PANEL.filter((tab) =>
    layout.value === "terminal"
      ? tab.value === "terminal"
      : tab.value === editor.panel || !settings.isHidden(tab.value),
  ),
);
const panelTab = computed(() =>
  layout.value === "terminal" ? "terminal" : (PANEL.find((tab) => tab.value === editor.panel)?.value ?? "problems"),
);

const problemFilter = shallowRef("");
const output = useTemplateRef("output");
const { data: levels } = useQuery(levelsQuery);
const problemLevels = computed(() => (levels.value ?? []).filter((level) => level.problem));
const { data: codes } = useQuery(codesQuery);
const advice = computed(() => new Map((codes.value ?? []).map((code) => [code.id, code.advice])));
// Health's report is built only while the panel shows Problems, because building it checks every mod.
const { data: report } = useQuery(() => ({
  ...reportQuery(workspace.listModIds()),
  enabled: panelTab.value === "problems" && !!editor.panel && workspace.isOpen(),
}));
const panelProblems = computed(listProblems);
const problemCounts = computed(() =>
  countBy(
    panelProblems.value.flatMap((file) => file.problems),
    (entry) => entry.problem.level,
  ),
);
// The counts are of the open files. The worst level also shows Health's last check when that has more.
const healthWorst = computed(() => {
  const level = problemLevels.value[0];
  const count = (level && report.value?.counts?.[level.id]) ?? 0;
  if (!level || count <= (problemCounts.value[level.id] ?? 0)) return null;
  return {
    label: `Health: ${count} ${level.label.toLowerCase()}`,
    text: `${plural(count, level.noun, level.nouns)} in Health's last check. Open Health to see them.`,
  };
});
const rows = computed<Row[]>(() =>
  panelProblems.value.flatMap(({ place, problems: listed }) => {
    const shown = listed
      .map((entry) => ({ place, ...entry }))
      .filter(({ problem }) => matchQuery(problemFilter.value, problem.message, problem.code, place.rel));
    return shown.length ? [{ place, count: shown.length }, ...shown] : [];
  }),
);
watch(
  () => git.output().length,
  () => output.value?.scrollTo({ top: output.value.scrollHeight }),
  { flush: "post" },
);

const { data: status } = useQuery(() => ({
  ...gitStatusQuery(git.findRoot(editor.activeFile())),
  enabled: !!git.findRoot(editor.activeFile()),
}));
const activeModel = computed(() => findModel(editor.activeFile()?.key ?? ""));
const languageName = computed(() => describeLanguage(EDITOR_LANGUAGES[activeModel.value?.place.format ?? ""]));
const groupRows = computed(() =>
  editor.layout().rows.map((row, order) => ({
    id: row.columns[0]?.id ?? String(order),
    slot: "row",
    order,
    minSize: 10,
    columns: row.columns.map((group, index) => ({ id: group.id, slot: "column", order: index, minSize: 10, group })),
  })),
);
const layoutKey = computed(() => `${LAYOUT_KEY}${workspace.activeId()}`);
// A row's splitter keeps its sizes under its first group's id, which never comes back once the row loses that group.
// The sizes of rows gone are removed once the saved layout has loaded.
watch(
  [() => groupRows.value.map((row) => row.id), () => editor.tracking],
  ([ids, loaded]) => loaded && deleteLayouts(workspace.activeId(), ["rows", ...ids]),
  { immediate: true },
);
const leftFile = useQuery(() => ({ ...fileQuery(comparing.value?.left.path ?? ""), enabled: !!comparing.value }));
const rightFile = useQuery(() => ({ ...fileQuery(comparing.value?.right.path ?? ""), enabled: !!comparing.value }));
const side = computed(() => editor.activeTab()?.side);
const { data: indentation } = useQuery(() => ({
  ...indentationQuery(activeModel.value?.place.path ?? ""),
  enabled: !side.value && !!activeModel.value,
}));

/** foldPane returns an explorer pane folded to its header, which is 2rem tall at the app's scale. */
function foldPane(id: string, slot: string, order: number): SplitterItem {
  const size = settings.toPixels(2);
  return { id, slot, order, sizeUnit: PIXELS, defaultSize: size, minSize: size, maxSize: size };
}

/** show opens a place from the explorer, in a preview tab or a pinned one. */
function show(place: Place, preview: boolean): void {
  editor.show(place, undefined, preview ? "explorer" : "navigate");
}

/** choosePanelTab shows a tab of the panel. The Terminal tab brings the terminal's own window to the front if open. */
function choosePanelTab(value: string): void {
  if (layout.value !== "full") return;
  if (value === "terminal" && windows.listWindows().includes("terminal")) windows.showWindow("terminal");
  else editor.panel = value;
}

/** popOutTerminal puts the terminal in a window of its own, and shows Problems here. */
async function popOutTerminal(): Promise<void> {
  await useTerminalStore().handOver();
  windows.popOut("terminal");
  editor.panel = "problems";
}

/** showSidebar shows a sidebar tab, unfolding the sidebar. A tab Visual hides brings Code back. */
function showSidebar(value: string): void {
  if (!modeTabs.value.some((tab) => tab.value === value)) editor.mode = "code";
  sidebar.value = value;
  splitter.value?.panelsRef[0]?.expand();
}

/** choose shows a sidebar tab, or folds the sidebar when its open tab is chosen again. */
function choose(value: string, collapsed: boolean, collapse: () => void): void {
  if (!collapsed && shownSidebar.value === value) collapse();
  else showSidebar(value);
}

/** toggleProblems shows the panel on Problems, or closes it if it shows Problems already. */
function toggleProblems(): void {
  if (editor.panel === "problems" && !isVisual.value) editor.panel = "";
  else editor.showPanel("problems");
}

/** togglePanel shows the panel, or closes it if it shows. */
function togglePanel(): void {
  if (editor.panel && !isVisual.value) editor.panel = "";
  else editor.showPanel(editor.panel || "problems");
}

/** toggleWiki shows the Wiki pane, or closes it if it shows. */
function toggleWiki(): void {
  if (editor.wiki && !isVisual.value) editor.wiki = false;
  else editor.showWiki();
}

/** listProblems lists the editor's problems and Health's findings for each file open in a tab. */
function listProblems(): { place: Place; problems: Problem[] }[] {
  const places = editor.listTabs().flatMap((tab) => editor.placeOf(tab.key) ?? []);
  return [...new Map(places.map((place) => [place.key, place])).values()].flatMap((place) => {
    const editorProblems = (findModel(place.key)?.problems ?? []).map((problem) => ({ problem }));
    const listed = [...editorProblems, ...listFindings(place)];
    return listed.length ? [{ place, problems: listed }] : [];
  });
}

/** listFindings returns Health's findings in a file that the editor does not make itself, as problems. */
function listFindings(place: Place): Problem[] {
  const disabled = settings.value("ide.checks").split(",");
  const groups = report.value ? [report.value.compatibility, ...(report.value.mods ?? [])] : [];
  return groups
    .flatMap((group) => group.findings ?? [])
    .flatMap((finding) => {
      if (finding.diagnosed || disabled.includes(finding.code)) return [];
      const { code, level, facts } = finding;
      const message = joinParts(finding.message);
      return [...(finding.sites ?? []), ...(finding.repeats ?? [])].flatMap(({ place: site, line, range }) => {
        if (site.key !== place.key) return [];
        // a definition's site carries its line but no range, since Go reads no file to make it
        const shown = range.startLineNumber
          ? range
          : { startLineNumber: line, startColumn: 1, endLineNumber: line, endColumn: 1 };
        return [{ problem: { code, level, message, range: shown, facts }, health: true as const }];
      });
    });
}

/** describeProblems says how many problems of a level the open files have, such as "2 errors in the open files". */
function describeProblems(level: Level): string {
  return `${plural(problemCounts.value[level.id] ?? 0, level.noun, level.nouns)} in the open files`;
}

/** describePlace returns the label of a file in the compare. */
function describePlace(place: Place): string {
  return `${workspace.originName(place.origin)} · ${place.rel}`;
}

/** collapseAll folds every folder of both explorers. */
function collapseAll(): void {
  modTree.value?.collapseAll();
  externalTree.value?.collapseAll();
  gameTree.value?.collapseAll();
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
          @click="filtering[tree] = !filtering[tree]"
        />
      </UTooltip>
    </DefineFilterButton>
    <USplitter ref="splitter" :items="panes" class="min-w-0 flex-1">
      <template #sidebar="{ collapsed, collapse }">
        <aside
          v-if="layout === 'full'"
          class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted text-(length:--ide-ui-font)"
        >
          <div
            class="flex shrink-0 items-center gap-0.5"
            :class="collapsed ? 'flex-col px-1 py-1.5' : 'h-9 border-b border-default px-1.5'"
          >
            <UTooltip v-for="tab in tabs" :key="tab.value" :text="tab.label" :content="{ side: 'right' }">
              <UButton
                :icon="tab.icon"
                square
                :ui="{ leadingIcon: 'size-4.5' }"
                :aria-label="tab.label"
                :color="!collapsed && shownSidebar === tab.value ? 'primary' : 'neutral'"
                :variant="!collapsed && shownSidebar === tab.value ? 'soft' : 'ghost'"
                @click="choose(tab.value, collapsed, collapse)"
              />
            </UTooltip>
            <span class="grow" />
            <span id="sidebar-actions" class="contents" />
            <UTooltip v-if="!collapsed && shownSidebar === 'explorer'" text="Collapse all">
              <UButton
                icon="i-lucide-chevrons-down-up"
                variant="ghost"
                square
                :ui="{ leadingIcon: 'size-4.5' }"
                aria-label="Collapse all"
                @click="collapseAll"
              />
            </UTooltip>
          </div>
          <!-- The explorer turns invisible, not hidden, behind another view, so its splitter keeps its height. -->
          <div v-if="!collapsed" class="relative flex min-h-0 flex-1 flex-col">
            <USplitter
              v-if="roots"
              :items="explorerPanes"
              orientation="vertical"
              :disabled="!gameOpen && !(externals.length && externalOpen)"
              auto-save-id="pmt.ide.explorer"
              class="absolute inset-0 py-1"
              :class="shownSidebar !== 'explorer' && 'invisible'"
            >
              <template #mods>
                <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                  <div
                    class="flex h-7 shrink-0 items-center gap-1 ps-2.5 pe-1 text-11 font-semibold whitespace-nowrap text-toned"
                  >
                    Workspace<span class="ms-1 font-normal text-dimmed">{{ plural(mods.length, "mod") }}</span>
                    <span class="grow" />
                    <FilterButton tree="mods" />
                    <AddModMenu>
                      <UTooltip text="Add a mod">
                        <UButton icon="i-lucide-plus" variant="ghost" size="xs" square aria-label="Add a mod" />
                      </UTooltip>
                    </AddModMenu>
                  </div>
                  <ExplorerTree ref="modTree" v-model:filtering="filtering.mods" :roots="mods" @open="show" />
                </div>
              </template>
              <template #external>
                <div class="flex min-h-0 min-w-0 flex-1 flex-col">
                  <div class="flex h-8 shrink-0 items-center border-t border-default pe-1">
                    <button
                      type="button"
                      class="flex h-8 min-w-0 flex-1 cursor-pointer items-center gap-1 ps-1.5 text-11 font-semibold whitespace-nowrap text-toned hover:text-highlighted"
                      :aria-expanded="externalOpen"
                      @click="externalOpen = !externalOpen"
                    >
                      <UIcon
                        :name="externalOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                        class="size-3.5 shrink-0"
                      />External mods<span class="ms-1 truncate font-normal text-dimmed">read only</span>
                    </button>
                    <UTooltip text="Open in its own window">
                      <UButton
                        icon="i-lucide-app-window"
                        variant="ghost"
                        size="xs"
                        square
                        aria-label="Open in its own window"
                        @click="windows.popOut('external')"
                      />
                    </UTooltip>
                  </div>
                  <div
                    v-if="windows.listWindows().includes('external')"
                    v-show="externalOpen"
                    class="flex flex-col gap-2 px-3 py-1.5 text-12 text-dimmed"
                  >
                    In its own window.
                    <div class="flex gap-1.5">
                      <UButton label="Bring back" size="xs" variant="soft" @click="windows.closeOther('external')" />
                      <UButton label="Show window" size="xs" variant="ghost" @click="windows.showWindow('external')" />
                    </div>
                  </div>
                  <ExplorerTree v-else v-show="externalOpen" ref="externalTree" :roots="externals" @open="show" />
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
                      <UIcon :name="gameOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />
                      Game files<span class="ms-1 truncate font-normal text-dimmed">read only</span>
                    </button>
                    <UTooltip text="Open in its own window">
                      <UButton
                        icon="i-lucide-app-window"
                        variant="ghost"
                        size="xs"
                        square
                        aria-label="Open in its own window"
                        @click="windows.popOut('game')"
                      />
                    </UTooltip>
                  </div>
                  <div
                    v-if="windows.listWindows().includes('game')"
                    v-show="gameOpen"
                    class="flex flex-col gap-2 px-3 py-1.5 text-12 text-dimmed"
                  >
                    In its own window.
                    <div class="flex gap-1.5">
                      <UButton label="Bring back" size="xs" variant="soft" @click="windows.closeOther('game')" />
                      <UButton label="Show window" size="xs" variant="ghost" @click="windows.showWindow('game')" />
                    </div>
                  </div>
                  <ExplorerTree v-else v-show="gameOpen" ref="gameTree" :roots="game" @open="show" />
                </div>
              </template>
            </USplitter>
            <SearchView v-show="shownSidebar === 'search'" />
            <SourceControlView v-if="shownSidebar === 'git'" />
            <EntriesView v-if="shownSidebar === 'entries'" />
          </div>
        </aside>
        <aside v-else class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted pb-1">
          <div
            class="flex h-7 shrink-0 items-center gap-1 ps-2.5 pe-1 text-11 font-semibold whitespace-nowrap text-toned"
          >
            {{ part === "game" || part === "external" ? describeWindow(part) : folderPlace?.name }}
            <span class="grow" />
            <FilterButton v-if="part === 'files'" tree="own" />
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
          @collapse="(index) => editorPanes[index]?.id === 'panel' && (editor.panel = '')"
        >
          <template #code>
            <EditorGroup v-if="visualGroup" :group="visualGroup" active />
            <div v-else class="flex min-h-0 min-w-0 flex-1">
              <USplitter
                :items="groupRows"
                orientation="vertical"
                :auto-save-id="`${layoutKey}.rows`"
                class="min-h-0 min-w-0 flex-1"
              >
                <template #row="{ item: row }">
                  <USplitter
                    :key="row.id"
                    :items="row.columns"
                    :auto-save-id="`${layoutKey}.${row.id}`"
                    class="min-h-0 min-w-0 flex-1"
                  >
                    <template #column="{ item: column }">
                      <EditorGroup :group="column.group" :active="column.group.id === editor.activeGroup()?.id" />
                    </template>
                  </USplitter>
                </template>
              </USplitter>
            </div>
          </template>
          <template #panel>
            <div class="flex min-h-0 min-w-0 flex-1 flex-col border-t border-default bg-muted">
              <div class="flex h-8 shrink-0 items-center gap-0.5 border-b border-default ps-2 pe-1.5">
                <UTabs
                  :model-value="panelTab"
                  :items="panelTabs"
                  :content="false"
                  variant="link"
                  size="sm"
                  @update:model-value="choosePanelTab(String($event))"
                >
                  <template #trailing="{ item }">
                    <template v-if="item.value === 'problems'">
                      <template v-for="level in problemLevels" :key="level.id">
                        <UTooltip v-if="problemCounts[level.id]" :text="describeProblems(level)">
                          <UBadge
                            :label="String(problemCounts[level.id])"
                            :color="level.color"
                            size="sm"
                            :aria-label="describeProblems(level)"
                          />
                        </UTooltip>
                      </template>
                    </template>
                  </template>
                </UTabs>
                <span class="grow" />
                <span id="panel-actions" class="contents" />
                <template v-if="layout === 'full'">
                  <UTooltip v-if="panelTab === 'terminal'" text="Open the terminal in its own window">
                    <UButton
                      icon="i-lucide-app-window"
                      variant="ghost"
                      size="sm"
                      square
                      aria-label="Open the terminal in its own window"
                      @click="popOutTerminal"
                    />
                  </UTooltip>
                  <UButton
                    icon="i-lucide-x"
                    variant="ghost"
                    size="sm"
                    square
                    aria-label="Close the panel"
                    @click="editor.panel = ''"
                  />
                </template>
              </div>
              <div class="flex min-h-0 flex-1 flex-col bg-muted text-(length:--ide-ui-font)">
                <template v-if="panelTab === 'problems'">
                  <Teleport defer to="#panel-actions">
                    <UInput
                      v-model="problemFilter"
                      placeholder="Filter problems"
                      size="xs"
                      class="w-50"
                      icon="i-lucide-filter"
                    />
                    <UButton
                      label="Open in Health"
                      variant="ghost"
                      size="xs"
                      :to="{ name: 'health', params: { id: workspace.activeId() } }"
                    />
                  </Teleport>
                  <UScrollArea
                    v-if="rows.length"
                    v-slot="{ item }"
                    :items="rows"
                    :virtualize="{ estimateSize: settings.toPixels(1.5) }"
                    class="min-h-0 flex-1 py-1"
                  >
                    <!-- The row's tooltip holds its whole message, so the message takes no title. -->
                    <UTooltip
                      v-if="'problem' in item"
                      :content="{ side: 'top', align: 'start' }"
                      :ui="{ content: 'h-auto bg-elevated shadow-lg ring-accented' }"
                    >
                      <ProblemRow
                        dense
                        class="ps-7.5"
                        :level="item.problem.level"
                        :message="item.problem.message"
                        @open="
                          editor.show(
                            item.place,
                            { lineNumber: item.problem.range.startLineNumber, column: item.problem.range.startColumn },
                            'navigate',
                          )
                        "
                      >
                        <template #message>{{ item.problem.message }}</template>
                        <template #trailing>
                          <span class="shrink-0 font-mono text-11 text-dimmed">{{ item.problem.code }}</span>
                          <span class="shrink-0 font-mono text-11 text-dimmed">
                            {{ item.problem.range.startLineNumber }}:{{ item.problem.range.startColumn }}
                          </span>
                          <UBadge
                            v-if="item.health"
                            label="Health"
                            color="neutral"
                            variant="subtle"
                            size="sm"
                            class="shrink-0"
                          />
                        </template>
                      </ProblemRow>
                      <template #content>
                        <div class="flex max-w-120 flex-col gap-1 py-0.5 text-12 whitespace-normal">
                          <span>{{ item.problem.message }}</span>
                          <span v-if="advice.get(item.problem.code)"
                            ><span class="text-dimmed">What to do:</span> {{ advice.get(item.problem.code) }}</span
                          >
                        </div>
                      </template>
                    </UTooltip>
                    <div
                      v-else
                      class="flex h-6 items-center gap-1.5 px-3 text-12 font-medium whitespace-nowrap text-highlighted"
                    >
                      <OriginChip :origin="item.place.origin" dot />
                      <span class="truncate">{{ item.place.rel }}</span>
                      <span class="text-11 font-normal text-dimmed">{{ item.count }}</span>
                    </div>
                  </UScrollArea>
                  <p v-else class="px-3 py-2 text-12 text-dimmed">
                    {{ problemFilter ? "No problem matches the filter." : "No problems in the open files." }}
                  </p>
                </template>
                <TerminalPanel v-else-if="panelTab === 'terminal'" />
                <div v-else ref="output" class="min-h-0 flex-1 overflow-y-auto px-3 py-1.5 font-mono text-11">
                  <pre v-for="(line, index) in git.output()" :key="index" class="pb-0.5 whitespace-pre-wrap text-toned"
                    >{{ line }}
    </pre>
                  <p v-if="!git.output().length" class="font-sans text-12 text-dimmed">The output of git shows here.</p>
                </div>
              </div>
            </div>
          </template>
        </USplitter>
      </template>
      <template #wiki>
        <!-- In a window of its own, the Wiki pane follows the main window's active file. -->
        <WikiPane
          :place="layout === 'wiki' ? windows.findActive('main') : editor.activeFile()"
          class="min-w-0 flex-1"
        />
      </template>
    </USplitter>
    <Teleport v-if="!['terminal', 'wiki'].includes(layout) && visible" defer to="#status-start">
      <UTooltip v-for="level in problemLevels" :key="level.id" :text="describeProblems(level)">
        <button
          :class="STATUS_ITEM"
          :style="{ color: `var(--ui-${level.color})` }"
          :aria-label="describeProblems(level)"
          @click="toggleProblems"
        >
          <UIcon :name="level.icon" class="size-3" />{{ problemCounts[level.id] ?? 0 }}
        </button>
      </UTooltip>
      <UTooltip v-if="healthWorst" :text="healthWorst.text">
        <RouterLink
          :to="{ name: 'health', params: { id: workspace.activeId() } }"
          :class="STATUS_ITEM"
          :aria-label="healthWorst.text"
        >
          <UIcon :name="PAGES.health.icon" class="size-3" />{{ healthWorst.label }}
        </RouterLink>
      </UTooltip>
      <button v-if="status" :class="STATUS_ITEM" @click="showSidebar('git')">
        <UIcon name="i-lucide-git-branch" class="size-3" />{{ status.branch || "detached HEAD" }}
        <template v-if="status.upstream">
          <UIcon name="i-lucide-arrow-down-up" class="size-3" />{{ status.ahead }} up, {{ status.behind }} down
        </template>
      </button>
    </Teleport>
    <Teleport v-if="!['terminal', 'wiki'].includes(layout) && visible" defer to="#status-end">
      <span v-if="side">{{ COMPARES[side] }}</span>
      <template v-else-if="activeModel">
        <span>Ln {{ activeModel.caret.line }}, Col {{ activeModel.caret.column }}</span>
        <span v-if="indentation">{{ indentation }}</span>
        <span>{{ languageName }}</span>
        <span>{{ ENCODINGS[activeModel.encoding] ?? activeModel.encoding }}</span>
      </template>
    </Teleport>
    <UModal :open="!!comparing" title="Compare" fullscreen @update:open="comparing = undefined">
      <template #body>
        <div v-if="comparing" class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-default">
          <p v-if="leftFile.isPending.value || rightFile.isPending.value" class="p-4 text-12 text-dimmed">
            Reading the two files…
          </p>
          <p v-else-if="leftFile.error.value || rightFile.error.value" class="p-4 text-12 text-dimmed">
            {{ describePlace(leftFile.error.value ? comparing.left : comparing.right) }} could not be read.
          </p>
          <CompareView
            v-else-if="leftFile.data.value && rightFile.data.value"
            :left="leftFile.data.value.text.text"
            :right="rightFile.data.value.text.text"
            :language="EDITOR_LANGUAGES[comparing.right.format]"
            :left-label="describePlace(comparing.left)"
            :right-label="describePlace(comparing.right)"
          />
        </div>
      </template>
    </UModal>
  </div>
</template>
