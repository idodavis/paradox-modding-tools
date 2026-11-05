<!-- One editor group: its tab strip and the active tab, as code, an image, a diff, a picture or the start page. -->
<script setup lang="ts">
import { computed, shallowReactive, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { useEventListener, watchDebounced } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";

import { Format } from "@internal/files/models";
import * as lsp from "@services/lsp/service";
import type { Place } from "@services/workspace/models";

import {
  canCopyToMod,
  compare,
  compareWithGame,
  copyToMod,
  findCommand,
  findGameCopy,
  findKbds,
  findOverride,
  overrideEntry,
} from "../../app/commands";
import { copiesQuery, fileQuery, folderQuery, loadQuery, outlineQuery } from "../../queries";
import { canSplit, useEditorStore, type Group, type Side, type Tab } from "../../stores/editor";
import { THIS_WINDOW, useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";
import CodeEditor from "./CodeEditor.vue";
import DrawPane from "./draw/DrawPane.vue";
import GitDiff from "./GitDiff.vue";
import ImageView from "./ImageView.vue";
import StartPage from "./StartPage.vue";
import { findModel, save, setText, type Model } from "./editor/models";
import { findIcon, VIEWS } from "./editor/tabs";

const { group, active } = defineProps<{ group: Group; active: boolean }>();

/** TAB is the drag data type that carries a tab to another window. */
const TAB = "application/x-pmt-tab";
type Zone = Side | "middle";
/** ZONES gives the classes that light each drop zone: a half of the editor, or all of it for the middle. */
const ZONES: Record<Zone, string> = {
  left: "inset-y-0 left-0 w-1/2",
  right: "inset-y-0 right-0 w-1/2",
  up: "inset-x-0 top-0 h-1/2",
  down: "inset-x-0 bottom-0 h-1/2",
  middle: "inset-0",
};
/** MODES are the options of the Code or Visual switch. */
const MODES = [
  { label: "Code", icon: "i-lucide-code", value: "code" },
  { label: "Visual", icon: "i-lucide-eye", value: "visual" },
];
/** CRUMB holds the classes of a breadcrumb that opens a menu. */
const CRUMB = "-mx-1 rounded-sm px-1 hover:bg-accented hover:text-highlighted";
const REVEAL = findCommand("explorer.reveal");
const CREATE = findCommand("mod.create");
const SAVE_ALL = findCommand("editor.saveAll");

const editor = useEditorStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const strip = useTemplateRef("strip");
// The index a tab dragged over the strip would land at.
const landing = shallowRef<number | null>(null);
const isDraggedOver = shallowRef(false);
const zone = shallowRef<Zone | null>(null);

const activeTab = computed(() => group.tabs.find((tab) => tab.id === group.active));
const place = computed(() => (activeTab.value ? editor.placeOf(activeTab.value.key) : null));
const model = computed(() => (activeTab.value && !activeTab.value.side ? findModel(activeTab.value.key) : null));
const hasCode = computed(() => !activeTab.value?.side && !!place.value && VIEWS[place.value.format].view === "code");
const { data: file } = useQuery(() => ({ ...fileQuery(place.value?.path ?? ""), enabled: hasCode.value }));
const isUnshown = computed(() => !!place.value && (!VIEWS[place.value.format].view || !!file.value?.binary));
const isDrawable = computed(() => editor.isDrawable(activeTab.value));
const isVisual = computed(() => active && editor.mode === "visual" && isDrawable.value);
const isTopRight = computed(() => THIS_WINDOW === "main" && editor.layout().rows[0]?.columns.at(-1)?.id === group.id);
const rows = computed(() => group.tabs.map(describe));

// The hover card at the caret, and the fix that overrides the entry it is in.
const { mutate: locateCaret, data: located } = useMutation({
  mutation: async (fileModel: Model) => {
    const [card, override] = await Promise.all([
      lsp.Hover(fileModel.place.path, fileModel.caret.line, fileModel.caret.column),
      findOverride(fileModel.place, fileModel.caret.line, fileModel.caret.column),
    ]);
    return { card, override };
  },
});
const card = computed(() => located.value?.card);
const crumbs = computed(() => [...(place.value?.rel.split("/") ?? []), ...(card.value?.within ?? [])]);
const folderCount = computed(() => (place.value?.rel.split("/").length ?? 1) - 1);
// The menu of the breadcrumb pressed, filled as it opens.
const crumbMenu = shallowRef<DropdownMenuItem[]>([]);
// A game file that a mod replaces shows the replacing mod.
const { data: copies } = useQuery(() => ({
  ...copiesQuery(place.value?.path ?? ""),
  enabled: place.value?.origin === VANILLA && !activeTab.value?.side,
}));
const replacement = computed(() =>
  place.value?.origin === VANILLA && copies.value && copies.value.length > 1 ? copies.value.at(-1) : undefined,
);
// The file of the tab whose menu is open.
const menuPlace = shallowRef<Place | null>(null);
const { data: menuCopies } = useQuery(() => ({
  ...copiesQuery(menuPlace.value?.path ?? ""),
  enabled: !!menuPlace.value && menuPlace.value.origin !== VANILLA,
}));
const menuGameCopy = computed(() => menuPlace.value && findGameCopy(menuPlace.value, menuCopies.value));
const closedNotices = shallowReactive(new Set<string>());
// The other ways to change a read only file when the caret is in no entry to override. Null until the caret is known.
const hint = computed(() => {
  const activePlace = place.value;
  if (!activePlace || !located.value || located.value.override) return null;
  if (activePlace.database) return { text: "Put the caret in an entry to override it.", copy: false };
  if (activePlace.format === Format.FormatLocalization) {
    return {
      text: "To change a line, write its key again in your mod's localization replace folder, which the game reads first.",
      copy: false,
    };
  }
  return { text: "To change this file, copy it into a mod.", copy: canCopyToMod(activePlace) };
});
// What the notice over a read only file says. Null for a file a mod can write to, and for the game's readmes/info,
// which a mod has no reason to override.
const notice = computed(() => {
  const activePlace = place.value;
  if (!activePlace?.readOnly || activeTab.value?.side || closedNotices.has(group.active ?? "")) return null;
  if (activePlace.origin === VANILLA) {
    if (activePlace.format === Format.FormatReadme || activePlace.format === Format.FormatMarkdown) return null;
    return { text: "Game files are read only.", detail: "", workshop: false };
  }
  return {
    text: `${workspace.originName(activePlace.origin)} is from the Steam Workshop, so PMT keeps it read only.`,
    detail: `Steam replaces ${activePlace.root} each time its author updates it.`,
    workshop: true,
  };
});

watch(
  () => group.active,
  (id) => strip.value?.querySelector(`[data-tab="${CSS.escape(id ?? "")}"]`)?.scrollIntoView({ inline: "nearest" }),
  { flush: "post" },
);
watchDebounced(
  () => [model.value, model.value?.caret.line, model.value?.caret.column],
  () => model.value && locateCaret(model.value),
  { debounce: 300, immediate: true },
);
// A tab drag ends with a drop or a drag end that reaches the window wherever it lands.
useEventListener(window, ["drop", "dragend"], endDrag);
const { mutate: saveModel } = useMutation({ mutation: save });
const comparing = shallowRef(false);

/** saveMine saves a model's text over the disk's text, and closes the compare. */
function saveMine(fileModel: Model): void {
  saveModel(fileModel);
  comparing.value = false;
}

/** applyDisk replaces a model's unsaved text with the disk's text, and closes the compare. */
function applyDisk(fileModel: Model): void {
  if (fileModel.diskText) setText(fileModel, fileModel.diskText);
  comparing.value = false;
}

/** overrideHere overrides the entry at a model's caret in the mod the notice's button names. */
function overrideHere(fileModel: Model): void {
  const modId = located.value?.override?.reveal?.place.origin;
  void overrideEntry(
    fileModel.place,
    fileModel.caret.line,
    fileModel.caret.column,
    card.value?.within?.[0] ?? "",
    modId,
  ).then(() => locateCaret(fileModel));
}

/**
 * loadCrumb fills the breadcrumbs' menu as one opens: the entries of the given folder, or the active file's entries
 * when no folder is given.
 */
async function loadCrumb(dir?: string): Promise<void> {
  crumbMenu.value = [];
  crumbMenu.value = dir === undefined ? await describeEntries() : await describeFolder(dir);
}

/** toFolder returns the path of the folder a breadcrumb names, by its index among the crumbs. */
function toFolder(index: number): string {
  const shown = place.value;
  return shown
    ? `${shown.root}/${shown.rel
        .split("/")
        .slice(0, index + 1)
        .join("/")}`
    : "";
}

/** describeFolder returns the menu of a folder's entries. A file opens, and a folder lists its own entries instead. */
async function describeFolder(dir: string): Promise<DropdownMenuItem[]> {
  const entries = (await loadQuery(folderQuery(dir))) ?? [];
  return entries.map((entry) => ({
    label: entry.name,
    icon: entry.isDir ? "i-lucide-folder" : findIcon(entry),
    onSelect: entry.isDir
      ? (event: Event) => {
          event.preventDefault();
          void loadCrumb(entry.path);
        }
      : () => editor.show(entry),
  }));
}

/** describeEntries returns the menu of the active file's entries, each going to its line. */
async function describeEntries(): Promise<DropdownMenuItem[]> {
  const shown = place.value;
  if (!shown) return [];
  const symbols = (await loadQuery(outlineQuery(shown.path))) ?? [];
  return symbols.map((symbol) => ({
    label: symbol.title || symbol.name,
    onSelect: () => editor.show(shown, { lineNumber: symbol.range.startLineNumber, column: symbol.range.startColumn }),
  }));
}

/** describe returns what the strip shows for a tab: its file, label, icon and marks. */
function describe(tab: Tab) {
  const tabPlace = editor.placeOf(tab.key);
  const path = tabPlace?.rel ?? tab.key;
  return {
    tab,
    tabPlace,
    path,
    label: tab.start ? "Start" : (tabPlace?.name ?? path),
    symbol: tab.start ? "i-lucide-house" : tab.side ? "i-lucide-git-compare" : "",
    icon: tabPlace ? findIcon(tabPlace) : "",
    suffix: tab.side ? (tab.hash?.slice(0, 7) ?? tab.side) : "",
    readOnly: !!tabPlace?.readOnly && !tab.side,
    unsaved: !!findModel(tab.key)?.unsaved,
    stale: !!findModel(tab.key)?.diskText,
  };
}
type Row = ReturnType<typeof describe>;

/** setMode activates this group and sets the editor mode to the one the switch picked. */
function setMode(choice: string | number): void {
  editor.activate(group.id);
  editor.mode = choice === "visual" ? "visual" : "code";
}

/** select makes this group active and shows a row's tab, if the tab has a file or is the start tab. */
function select(row: Row): void {
  if (row.tabPlace || row.tab.start) editor.open(row.tab, row.tabPlace, group.id);
  else editor.activate(group.id);
}

/** activateGroup makes this group active when the pointer goes down in it. On a tab it waits for the click. */
function activateGroup(event: PointerEvent): void {
  if (!(event.target instanceof Element && event.target.closest("[data-tab]"))) editor.activate(group.id);
}

/** closeTabs closes the given tabs of this group. It asks about unsaved files first and closes none on a no. */
async function closeTabs(tabs: Tab[]): Promise<void> {
  if (!(await editor.confirmClose(tabs))) return;
  editor.activate(group.id);
  for (const tab of [...tabs]) editor.close(tab.id);
}

/** listOthers lists the tabs of this group except the one with the given id. */
function listOthers(keep: string | null): Tab[] {
  return group.tabs.filter((tab) => tab.id !== keep);
}

/** split shows a row's tab, then splits the group in the given direction. The tab shows in the new group too. */
function split(row: Row, direction: "row" | "column"): void {
  select(row);
  editor.split(direction);
}

/** startDrag starts the drag of a tab. The drag carries the tab's id. */
function startDrag(event: DragEvent, row: Row): void {
  if (!event.dataTransfer) return;
  event.dataTransfer.setData(TAB, row.tab.id);
  event.dataTransfer.effectAllowed = "move";
}

/** dropOutside moves a tab into a window of its own when it was dropped outside this window. The start tab stays. */
function dropOutside(event: DragEvent): void {
  const id = event.target instanceof HTMLElement ? event.target.closest<HTMLElement>("[data-tab]")?.dataset.tab : "";
  const outside =
    event.screenX < window.screenX ||
    event.screenX > window.screenX + window.outerWidth ||
    event.screenY < window.screenY ||
    event.screenY > window.screenY + window.outerHeight;
  const isStart = group.tabs.some((tab) => tab.id === id && tab.start);
  if (id && !isStart && outside && event.dataTransfer?.dropEffect === "none") windows.popOut("editor", [id]);
}

/** dropTab brings a dragged tab, from this window or another, to this group or to a new group beside it. */
function dropTab(event: DragEvent, destination: number | Side): void {
  const id = event.dataTransfer?.getData(TAB);
  if (!id) return;
  editor.activate(group.id);
  windows.requestTab(id);
  editor.move(id, group.id, destination);
}

/** findLanding returns the index in this group a tab dragged over the strip would land at. */
function findLanding(event: DragEvent): number {
  const tabs = [...(strip.value?.querySelectorAll("[data-tab]") ?? [])];
  const index = tabs.findIndex((tab) => {
    const box = tab.getBoundingClientRect();
    return event.clientX < box.left + box.width / 2;
  });
  return index < 0 ? tabs.length : index;
}

/** dropOnStrip drops a tab on the strip where the line shows. */
function dropOnStrip(event: DragEvent): void {
  const index = findLanding(event);
  const start = group.tabs.findIndex((tab) => tab.id === event.dataTransfer?.getData(TAB));
  dropTab(event, start >= 0 && start < index ? index - 1 : index);
}

/** overStrip lets a dragged tab be dropped on the strip. */
function overStrip(event: DragEvent): void {
  if (!event.dataTransfer?.types.includes(TAB)) return;
  event.preventDefault();
  landing.value = findLanding(event);
}

/** leaveStrip takes the line away once the pointer is out of the strip. */
function leaveStrip(event: DragEvent): void {
  if (!isEnteringChild(event)) landing.value = null;
}

/** enter shows the drop zones while a tab is dragged over the group. */
function enter(event: DragEvent): void {
  if (event.dataTransfer?.types.includes(TAB)) isDraggedOver.value = true;
}

/** isEnteringChild tells whether a leaving drag goes to an element inside the one that handles the event. */
function isEnteringChild(event: DragEvent): boolean {
  return (
    event.currentTarget instanceof Node &&
    event.relatedTarget instanceof Node &&
    event.currentTarget.contains(event.relatedTarget)
  );
}

/** leave hides the drop zones once the pointer leaves the group. */
function leave(event: DragEvent): void {
  if (isEnteringChild(event)) return;
  isDraggedOver.value = false;
  zone.value = null;
}

/** lightZone lights the drop zone under the dragged tab. */
function lightZone(event: DragEvent): void {
  event.preventDefault();
  if (!(event.currentTarget instanceof HTMLElement)) return;
  const box = event.currentTarget.getBoundingClientRect();
  const x = (event.clientX - box.left) / box.width;
  const y = (event.clientY - box.top) / box.height;
  if (x < 0.25) zone.value = "left";
  else if (x > 0.75) zone.value = "right";
  else if (y < 0.25) zone.value = "up";
  else if (y > 0.75) zone.value = "down";
  else zone.value = "middle";
}

/** dropOnEditor drops a tab on the lit zone. */
function dropOnEditor(event: DragEvent): void {
  const target = zone.value;
  dropTab(event, !target || target === "middle" ? group.tabs.length : target);
}

/** endDrag stops showing a tab drag in this group. */
function endDrag(): void {
  landing.value = null;
  isDraggedOver.value = false;
  zone.value = null;
}

/** buildMenu returns the context menu of a tab, as groups of items. */
function buildMenu(row: Row): ContextMenuItem[][] {
  const filePlace = row.tabPlace;
  const fileModel = findModel(row.tab.key);
  return [
    [
      {
        label: "Save",
        kbds: findKbds("editor.save"),
        disabled: !fileModel?.unsaved,
        onSelect: () => fileModel && saveModel(fileModel),
      },
      { label: "Save all", disabled: !SAVE_ALL?.when(), onSelect: (event) => SAVE_ALL?.onSelect?.(event) },
    ],
    [
      { label: "Close", onSelect: () => void closeTabs([row.tab]) },
      { label: "Close the others", onSelect: () => void closeTabs(listOthers(row.tab.id)) },
      {
        label: "Close tabs to the right",
        onSelect: () => void closeTabs(group.tabs.slice(group.tabs.findIndex((tab) => tab.id === row.tab.id) + 1)),
      },
      {
        label: "Close saved",
        onSelect: () => void closeTabs(group.tabs.filter((tab) => !findModel(tab.key)?.unsaved)),
      },
      { label: "Close all", onSelect: () => void closeTabs(group.tabs) },
    ],
    [
      {
        label: "Split right",
        icon: "i-lucide-columns-2",
        kbds: findKbds("editor.splitRight"),
        disabled: !canSplit(row.tab),
        onSelect: () => split(row, "row"),
      },
      {
        label: "Split down",
        icon: "i-lucide-rows-2",
        disabled: !canSplit(row.tab),
        onSelect: () => split(row, "column"),
      },
    ],
    [
      {
        label: "Compare with the game's copy",
        // menuGameCopy belongs to the tab whose menu opened last.
        disabled: !!row.tab.side || menuPlace.value?.key !== filePlace?.key || !menuGameCopy.value,
        onSelect: () => filePlace && compareWithGame(filePlace),
      },
      { label: "Select for compare", disabled: !filePlace, onSelect: () => (editor.compareFile = filePlace ?? null) },
      {
        label: editor.compareFile ? `Compare with ${editor.compareFile.rel}` : "Compare with selected",
        disabled: !filePlace || !editor.compareFile || editor.compareFile.key === filePlace.key,
        onSelect: () => filePlace && editor.compareFile && compare(editor.compareFile, filePlace),
      },
    ],
    // The start page never leaves the main window.
    ...(row.tab.start
      ? []
      : [
          [
            {
              label: "Move into a new window",
              icon: "i-lucide-app-window",
              onSelect: () => windows.popOut("editor", [row.tab.id]),
            },
            ...(THIS_WINDOW === "main"
              ? []
              : [
                  {
                    label: "Back into the main window",
                    icon: "i-lucide-panel-top-close",
                    onSelect: () => windows.dockBack(),
                  },
                ]),
          ],
        ]),
    [
      { label: "Copy path", disabled: !filePlace, onSelect: () => filePlace && void Clipboard.SetText(filePlace.path) },
      {
        label: "Reveal in explorer",
        disabled: !filePlace || !REVEAL?.when(),
        onSelect: (event) => {
          select(row);
          REVEAL?.onSelect?.(event);
        },
      },
    ],
  ];
}

const actions = computed<DropdownMenuItem[]>(() => [
  {
    label: "Split right",
    icon: "i-lucide-columns-2",
    kbds: findKbds("editor.splitRight"),
    disabled: !canSplit(activeTab.value),
    onSelect: () => editor.split("row"),
  },
  { label: "Close the others", onSelect: () => void closeTabs(listOthers(group.active)) },
  { label: "Close all", onSelect: () => void closeTabs(group.tabs) },
]);
</script>

<template>
  <section
    class="flex min-h-0 min-w-0 flex-1 flex-col"
    @pointerdown="activateGroup"
    @focusin="editor.activate(group.id)"
    @dragenter.capture="enter"
    @dragleave="leave"
  >
    <div class="flex h-9 shrink-0 items-stretch border-b border-default bg-muted">
      <template v-if="!isVisual">
        <div
          ref="strip"
          class="flex min-w-0 items-start overflow-x-auto overflow-y-hidden [scrollbar-color:auto] [scrollbar-width:auto]"
          @wheel.passive="strip && !$event.deltaX && (strip.scrollLeft += $event.deltaY)"
          @dragover="overStrip"
          @dragleave="leaveStrip"
          @drop="dropOnStrip"
          @dragend="dropOutside"
        >
          <UContextMenu
            v-for="(row, index) in rows"
            :key="row.tab.id"
            :items="buildMenu(row)"
            :ui="{ content: 'w-64' }"
            @update:open="(open) => open && (menuPlace = row.tabPlace)"
          >
            <div
              :data-tab="row.tab.id"
              class="group relative flex h-8.75 shrink-0 cursor-pointer items-center gap-1.75 border-e border-default ps-3 pe-1.5 text-12 whitespace-nowrap"
              :class="
                row.tab.id === group.active
                  ? 'bg-(--bg-0) text-highlighted before:absolute before:inset-x-0 before:bottom-0 before:h-0.5 before:bg-primary'
                  : 'text-dimmed hover:text-toned'
              "
              :title="row.path"
              draggable="true"
              @dragstart="startDrag($event, row)"
              @click="select(row)"
              @dblclick="editor.pin(row.tab.id)"
              @auxclick.middle="closeTabs([row.tab])"
            >
              <span
                v-if="landing === index || (landing === rows.length && index === rows.length - 1)"
                class="pointer-events-none absolute inset-y-0 z-10 w-0.5 bg-primary"
                :class="landing === index ? 'left-0' : 'right-0'"
              />
              <UIcon
                v-if="row.symbol"
                :name="row.symbol"
                class="size-3.5 shrink-0"
                :class="row.tab.start ? 'text-primary' : 'text-dimmed'"
              />
              <img v-else-if="row.icon" :src="row.icon" alt="" class="size-3.5 shrink-0" />
              <UIcon v-else name="i-lucide-file" class="size-3.5 shrink-0 text-dimmed" />
              <span class="truncate" :class="{ italic: row.tab.preview }">{{ row.label }}</span>
              <span v-if="row.suffix" class="text-dimmed">{{ row.suffix }}</span>
              <UIcon v-else-if="row.readOnly" name="i-lucide-lock" class="size-3 shrink-0 text-dimmed" />
              <span v-if="row.stale" class="size-1.5 shrink-0 rounded-full bg-warning" title="Changed on disk" />
              <span class="flex size-5 shrink-0 items-center justify-center">
                <span v-if="row.unsaved" class="size-1.5 rounded-full bg-(--fg-2) group-hover:hidden" />
                <UButton
                  icon="i-lucide-x"
                  variant="ghost"
                  size="xs"
                  square
                  aria-label="Close"
                  :class="{
                    'hidden group-hover:flex': row.unsaved,
                    'invisible group-hover:visible': !row.unsaved && row.tab.id !== group.active,
                  }"
                  @click.stop="closeTabs([row.tab])"
                />
              </span>
            </div>
          </UContextMenu>
        </div>
        <span class="grow" />
        <div class="flex shrink-0 items-center gap-0.5 px-2">
          <UDropdownMenu :items="actions">
            <UButton icon="i-lucide-ellipsis" variant="ghost" size="sm" square aria-label="Tab actions" />
          </UDropdownMenu>
          <UTooltip v-if="isTopRight" text="Wiki pane" :kbds="findKbds('editor.toggleWiki')">
            <UButton
              icon="i-lucide-book-text"
              square
              size="sm"
              :color="editor.wiki ? 'primary' : 'neutral'"
              :variant="editor.wiki ? 'soft' : 'ghost'"
              :ui="{ leadingIcon: !editor.wiki && 'text-info' }"
              aria-label="Wiki pane"
              :aria-pressed="editor.wiki"
              @click="editor.wiki = !editor.wiki"
            />
          </UTooltip>
        </div>
      </template>
      <div v-else id="visual-tools" class="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto px-2" />
      <UTooltip v-if="isDrawable" text="Code or Visual" :kbds="findKbds('ide.mode')">
        <UTabs
          :model-value="isVisual ? 'visual' : 'code'"
          :items="MODES"
          :content="false"
          size="xs"
          class="shrink-0 self-center pe-2"
          :ui="{ list: 'h-5.5', trigger: 'h-4.5 px-2 text-11', leadingIcon: 'size-3' }"
          @update:model-value="setMode"
        />
      </UTooltip>
    </div>
    <div
      v-if="model?.diskText"
      class="flex h-9.5 shrink-0 items-center gap-2.5 border-b border-default bg-(--orange-soft) ps-3.5 pe-3 text-12"
    >
      <UIcon name="i-lucide-triangle-alert" class="size-3.5 shrink-0 text-warning" />
      <span class="grow">This file changed on disk while you have unsaved text here.</span>
      <UButton
        label="Compare the two"
        icon="i-lucide-git-compare"
        size="xs"
        color="neutral"
        variant="outline"
        @click="comparing = true"
      />
      <UButton label="Keep mine" size="xs" color="neutral" variant="ghost" @click="saveMine(model)" />
      <UButton label="Take the disk's" size="xs" color="neutral" variant="ghost" @click="applyDisk(model)" />
      <UModal
        :open="comparing"
        fullscreen
        :ui="{ header: 'gap-2.5', body: 'flex min-h-0 flex-col' }"
        @update:open="comparing = $event"
      >
        <template #header>
          <span class="text-13 font-semibold">Compare</span>
          <UBadge label="Changed on disk while you had unsaved text" color="warning" variant="subtle" />
          <span class="font-mono text-11.5 text-dimmed">{{ model.place.rel }}</span>
          <span class="grow" />
          <UButton label="Keep mine" color="neutral" variant="outline" @click="saveMine(model)" />
          <UButton label="Take the disk's" @click="applyDisk(model)" />
          <UButton icon="i-lucide-x" color="neutral" variant="ghost" aria-label="Close" @click="comparing = false" />
        </template>
        <template #body>
          <div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-default">
            <CompareView
              :left="model.diskText.text"
              :right="model.model.getValue()"
              :language="EDITOR_LANGUAGES[model.place.format] ?? 'plaintext'"
              left-label="The file on disk"
              right-label="Your unsaved text"
            />
          </div>
        </template>
        <template #footer>
          <span class="text-12 text-dimmed">
            Keep mine saves your text over the disk's; Take the disk's drops your unsaved text.
          </span>
        </template>
      </UModal>
    </div>
    <div
      v-if="place && !activeTab?.side"
      class="flex h-7 shrink-0 items-center gap-1.5 overflow-hidden border-b border-default bg-(--bg-0) px-3.5 text-12 whitespace-nowrap text-dimmed"
    >
      <UDropdownMenu
        :items="crumbMenu"
        :content="{ align: 'start' }"
        @update:open="(open) => open && loadCrumb(place?.root)"
      >
        <button type="button" :class="CRUMB" class="flex items-center gap-1.5">
          <OriginChip :origin="place.origin" dot />{{ workspace.originName(place.origin) }}
        </button>
      </UDropdownMenu>
      <template v-for="(part, index) in crumbs" :key="index">
        <UIcon name="i-lucide-chevron-right" class="size-3 shrink-0" />
        <UDropdownMenu
          v-if="index <= folderCount"
          :items="crumbMenu"
          :content="{ align: 'start' }"
          @update:open="(open) => open && loadCrumb(index < folderCount ? toFolder(index) : undefined)"
        >
          <button type="button" :class="[CRUMB, index === folderCount && 'text-toned']">{{ part }}</button>
        </UDropdownMenu>
        <span v-else class="font-mono text-highlighted">{{ part }}</span>
      </template>
      <span class="grow" />
      <template v-if="replacement">
        Replaced by <OriginChip :origin="replacement.origin" dot />
        <span class="text-toned">{{ workspace.originName(replacement.origin) }}</span>
        <UButton label="Compare" variant="ghost" size="xs" @click="compare(place, replacement)" />
      </template>
    </div>
    <div
      v-if="notice"
      class="flex shrink-0 items-center gap-2.5 border-b border-default bg-(--bg-3) py-1.5 ps-3.5 pe-2.5 text-12"
    >
      <UIcon name="i-lucide-lock" class="size-3.5 shrink-0 text-dimmed" />
      <div class="flex min-w-0 grow flex-col gap-0.5">
        <span class="text-toned">{{ notice.text }}</span>
        <span v-if="notice.detail" class="truncate text-dimmed">{{ notice.detail }}</span>
        <span v-if="hint" class="text-dimmed">{{ hint.text }}</span>
      </div>
      <UButton
        v-if="model && located?.override"
        :label="located.override.title"
        icon="i-lucide-copy-plus"
        size="xs"
        @click="overrideHere(model)"
      />
      <UButton
        v-if="place && hint?.copy"
        label="Copy into a mod"
        icon="i-lucide-copy-plus"
        size="xs"
        @click="copyToMod(place)"
      />
      <UButton
        v-if="notice.workshop"
        label="New mod for my changes…"
        size="xs"
        color="neutral"
        variant="outline"
        @click="CREATE?.onSelect?.()"
      />
      <UButton
        icon="i-lucide-x"
        size="xs"
        color="neutral"
        variant="ghost"
        square
        aria-label="Close the notice"
        @click="closedNotices.add(group.active ?? '')"
      />
    </div>
    <div class="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <GitDiff
        v-if="activeTab?.side"
        :key="activeTab.id"
        :tab="activeTab"
        :side="activeTab.side"
        :root="activeTab.root"
      />
      <template v-else-if="place">
        <ImageView v-if="VIEWS[place.format].view === 'image'" :key="place.key" :place="place" />
        <template v-else-if="model">
          <DrawPane v-if="isVisual" :key="place.key" :model="model" />
          <CodeEditor v-else :key="place.key" :model="model" :active="active" />
        </template>
        <p v-else-if="isUnshown" class="flex flex-1 items-center justify-center bg-(--bg-0) text-12 text-dimmed">
          PMT can't show this file.
        </p>
      </template>
      <StartPage v-else-if="!activeTab || activeTab.start" />
      <div
        v-if="isDraggedOver"
        class="absolute inset-0 z-20"
        @dragover="lightZone"
        @dragleave="zone = null"
        @drop.prevent="dropOnEditor"
      >
        <div
          v-if="zone"
          class="pointer-events-none absolute bg-(--accent-soft) ring-1 ring-primary ring-inset"
          :class="ZONES[zone]"
        />
      </div>
    </div>
  </section>
</template>
