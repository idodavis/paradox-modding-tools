<!--
  One editor group: its tab strip, with tabs dragged between groups and windows, and the active tab shown as code, an
  image or a diff, or in Visual as the picture it draws. A file that changed on disk under unsaved text shows a notice
  and a compare.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { watchDebounced } from "@vueuse/core";
import { insertNodeAt, removeNode, useSortable } from "@vueuse/integrations/useSortable";
import { Clipboard } from "@wailsio/runtime";
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";

import * as lsp from "@services/lsp/service";
import type { Place } from "@services/workspace/models";

import { compare, compareWithGame, findCommand, findGameCopy, findKbds } from "../../app/commands";
import { copiesQuery, fileQuery } from "../../queries";
import { useEditorStore, type Group, type Tab } from "../../stores/editor";
import { THIS_WINDOW, useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";
import CodeEditor from "./CodeEditor.vue";
import DrawPane from "./draw/DrawPane.vue";
import GitDiff from "./GitDiff.vue";
import ImageView from "./ImageView.vue";
import { findModel, save, setText, type Model } from "./editor/models";
import { findIcon, VIEWS } from "./editor/tabs";

const { group, active } = defineProps<{ group: Group; active: boolean }>();

/** The data a dragged tab carries to another window. */
const TAB = "application/x-pmt-tab";
type Zone = "left" | "right" | "up" | "down" | "middle";
/** The half of the editor each drop zone lights. */
const ZONES: Record<Zone, string> = {
  left: "inset-y-0 left-0 w-1/2",
  right: "inset-y-0 right-0 w-1/2",
  up: "inset-x-0 top-0 h-1/2",
  down: "inset-x-0 bottom-0 h-1/2",
  middle: "inset-0",
};
/** The two modes the switch offers. */
const MODES = [
  { label: "Code", icon: "i-lucide-code", value: "code" },
  { label: "Visual", icon: "i-lucide-eye", value: "visual" },
];
/** The command that shows the active file in the explorer, and the one that saves every file with unsaved text. */
const REVEAL = findCommand("explorer.reveal");
const SAVE_ALL = findCommand("editor.saveAll");

const editor = useEditorStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const strip = useTemplateRef("strip");
// the tabs as the strip draws them: sortable moves them, and the store hears of it when the drag ends
const order = shallowRef<Tab[]>([]);
const over = shallowRef(false);
const zone = shallowRef<Zone | null>(null);

const activeTab = computed(() => group.tabs.find((tab) => tab.id === group.active));
const place = computed(() => (activeTab.value ? editor.placeOf(activeTab.value.key) : null));
const model = computed(() => (activeTab.value && !activeTab.value.side ? findModel(activeTab.value.key) : null));
const hasCode = computed(() => !activeTab.value?.side && !!place.value && VIEWS[place.value.format].view === "code");
const { data: file } = useQuery(() => ({ ...fileQuery(place.value?.path ?? ""), enabled: hasCode.value }));
// a file with no view of its own, or whose bytes are not text, says so rather than stay blank
const isUnshown = computed(() => !!place.value && (!VIEWS[place.value.format].view || !!file.value?.binary));
const isDrawable = computed(() => editor.isDrawable(activeTab.value));
// this group is the one Visual shows, and its active file is drawn
const isVisual = computed(() => active && editor.mode === "visual" && isDrawable.value);
const corner = computed(() => THIS_WINDOW === "main" && editor.layout().rows[0]?.columns.at(-1)?.id === group.id);
const rows = computed(() => order.value.map(describe));

// where the caret is, as the definition and the blocks around it
const { mutate: locateCaret, data: card } = useMutation({
  mutation: (open: Model) => lsp.Hover(open.place.path, open.caret.line, open.caret.column),
});
const crumbs = computed(() => [...(place.value?.rel.split("/") ?? []), ...(card.value?.within ?? [])]);
const folders = computed(() => (place.value?.rel.split("/").length ?? 1) - 1);
// a file of the game that a mod replaces shows the mod's name; a mod file's copies, which the cache may hold, do not
const { data: copies } = useQuery(() => ({
  ...copiesQuery(place.value?.path ?? ""),
  enabled: !!place.value?.readOnly && !activeTab.value?.side,
}));
const replacement = computed(() =>
  place.value?.readOnly && copies.value && copies.value.length > 1 ? copies.value.at(-1) : undefined,
);
// the file of the tab whose menu is open, with the game's copy of it, read when the menu opens
const menuPlace = shallowRef<Place | null>(null);
const { data: menuCopies } = useQuery(() => ({
  ...copiesQuery(menuPlace.value?.path ?? ""),
  enabled: !!menuPlace.value && !menuPlace.value.readOnly,
}));
const menuGameCopy = computed(() => menuPlace.value && findGameCopy(menuPlace.value, menuCopies.value));

watch(
  () => group.tabs,
  (tabs) => (order.value = [...tabs]),
  { immediate: true, deep: true },
);
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
useSortable(strip, order, {
  group: "pmt-tabs",
  animation: 120,
  draggable: "[data-tab]",
  watchElement: true,
  setData: (data, item) => data.setData(TAB, item.dataset.tab ?? ""),
  onEnd: ({ item, from, to, oldIndex, newIndex }) => {
    if (from !== to) {
      removeNode(item);
      insertNodeAt(from, item, oldIndex ?? 0);
    }
    const id = item.dataset.tab;
    if (id && to.dataset.group && newIndex !== undefined && (from !== to || oldIndex !== newIndex))
      editor.move(id, to.dataset.group, newIndex);
  },
});

const { mutate: saveModel } = useMutation({ mutation: (open: Model) => save(open) });
// the compare of the active file's unsaved text with the disk's is open
const comparing = shallowRef(false);

/** saveMine saves a model's text over the disk's text it kept, and closes the compare. */
function saveMine(open: Model): void {
  saveModel(open);
  comparing.value = false;
}

/** applyDisk puts the disk's text a model kept in the model, dropping its unsaved text, and closes the compare. */
function applyDisk(open: Model): void {
  if (open.diskText) setText(open, open.diskText);
  comparing.value = false;
}

/** describe describes a tab for the strip: its label, its icon and what marks it. */
function describe(tab: Tab) {
  const tabPlace = editor.placeOf(tab.key);
  const path = tabPlace?.rel ?? tab.key;
  return {
    tab,
    tabPlace,
    path,
    label: tabPlace?.name ?? path,
    icon: tabPlace ? findIcon(tabPlace) : "",
    suffix: tab.side ? (tab.hash?.slice(0, 7) ?? tab.side) : "",
    readOnly: !!tabPlace?.readOnly && !tab.side,
    unsaved: !!findModel(tab.key)?.unsaved,
    // the file changed on disk under its unsaved text
    stale: !!findModel(tab.key)?.diskText,
  };
}
type Row = ReturnType<typeof describe>;

/**
 * setMode sets the mode to the one the switch picked. Picking Visual in a group that is not the active one activates
 * it.
 */
function setMode(choice: string | number): void {
  editor.activate(group.id);
  editor.mode = choice === "visual" ? "visual" : "code";
}

/** select shows a tab of this group, which makes the group the active one. */
function select(row: Row): void {
  if (row.tabPlace) editor.open(row.tab, row.tabPlace, group.id);
  else editor.activate(group.id);
}

/**
 * activateGroup makes this group the active one when the pointer goes down in it. On a tab it waits for the click to
 * show the tab, so the history keeps the place the user leaves in the group active before.
 */
function activateGroup(event: PointerEvent): void {
  if (!(event.target instanceof Element && event.target.closest("[data-tab]"))) editor.activate(group.id);
}

/**
 * closeTabs closes the given tabs of this group. When that leaves a file with changes that are not saved in no tab, it
 * asks first, once for all of them, and saying no closes none.
 */
async function closeTabs(tabs: Tab[]): Promise<void> {
  if (!(await editor.confirmClose(tabs))) return;
  editor.activate(group.id);
  for (const tab of [...tabs]) editor.close(tab.id);
}

/** listSaved lists the tabs of this group that hold no changes that are not saved. */
function listSaved(): Tab[] {
  return group.tabs.filter((tab) => !findModel(tab.key)?.unsaved);
}

/** listOthers lists the tabs of this group other than the given one. */
function listOthers(keep: string | null): Tab[] {
  return group.tabs.filter((tab) => tab.id !== keep);
}

/** split splits the group at a tab, which shows in the new group too. */
function split(row: Row, direction: "row" | "column"): void {
  select(row);
  editor.split(direction);
}

/** dropOutside moves a tab into a window of its own when it was dropped outside this window. */
function dropOutside(event: DragEvent): void {
  const id = event.target instanceof HTMLElement ? event.target.closest<HTMLElement>("[data-tab]")?.dataset.tab : "";
  const outside =
    event.screenX < window.screenX ||
    event.screenX > window.screenX + window.outerWidth ||
    event.screenY < window.screenY ||
    event.screenY > window.screenY + window.outerHeight;
  if (id && outside && event.dataTransfer?.dropEffect === "none") windows.popOut("editor", [id]);
}

/** drop asks the window that holds a dragged tab for it. */
function drop(event: DragEvent): void {
  const id = event.dataTransfer?.getData(TAB);
  if (!id) return;
  editor.activate(group.id);
  windows.requestTab(id);
}

/** allow lets a tab from another window be dropped on the strip. */
function allow(event: DragEvent): void {
  if (event.dataTransfer?.types.includes(TAB)) event.preventDefault();
}

/** enter starts to show where a dragged tab would land in the editor. */
function enter(event: DragEvent): void {
  if (event.dataTransfer?.types.includes(TAB)) over.value = true;
}

/** leave stops showing where a dragged tab would land once the pointer is out of the group. */
function leave(event: DragEvent): void {
  if (
    event.currentTarget instanceof Node &&
    event.relatedTarget instanceof Node &&
    event.currentTarget.contains(event.relatedTarget)
  )
    return;
  over.value = false;
  zone.value = null;
}

/** lightZone lights the zone of the editor the dragged tab is over. */
function lightZone(event: DragEvent): void {
  event.preventDefault();
  if (!(event.currentTarget instanceof HTMLElement)) return;
  const box = event.currentTarget.getBoundingClientRect();
  const x = (event.clientX - box.left) / box.width;
  const y = (event.clientY - box.top) / box.height;
  zone.value = x < 0.25 ? "left" : x > 0.75 ? "right" : y < 0.25 ? "up" : y > 0.75 ? "down" : "middle";
}

/** dropOnEditor drops a tab on the lit zone: into this group in the middle, else into a new group. */
function dropOnEditor(event: DragEvent): void {
  const id = event.dataTransfer?.getData(TAB);
  const middle = zone.value === "middle" || !zone.value;
  over.value = false;
  zone.value = null;
  if (!id) return;
  editor.activate(group.id);
  windows.requestTab(id);
  editor.move(id, middle ? group.id : crypto.randomUUID(), middle ? group.tabs.length : 0);
}

/** buildMenu builds the menu of a tab. */
function buildMenu(row: Row): ContextMenuItem[][] {
  const mine = row.tabPlace;
  const open = findModel(row.tab.key);
  return [
    [
      {
        label: "Save",
        kbds: findKbds("editor.save"),
        disabled: !open?.unsaved,
        onSelect: () => open && saveModel(open),
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
      { label: "Close saved", onSelect: () => void closeTabs(listSaved()) },
      { label: "Close all", onSelect: () => void closeTabs(group.tabs) },
    ],
    [
      {
        label: "Split right",
        icon: "i-lucide-columns-2",
        kbds: findKbds("editor.splitRight"),
        onSelect: () => split(row, "row"),
      },
      { label: "Split down", icon: "i-lucide-rows-2", onSelect: () => split(row, "column") },
    ],
    [
      {
        label: "Compare with the game's copy",
        // the game's copy read for this tab's file, which the menu opening asked for
        disabled: !!row.tab.side || menuPlace.value?.key !== mine?.key || !menuGameCopy.value,
        onSelect: () => mine && compareWithGame(mine),
      },
      { label: "Select for compare", disabled: !mine, onSelect: () => (editor.compareFile = mine ?? null) },
      {
        label: editor.compareFile ? `Compare with ${editor.compareFile.rel}` : "Compare with selected",
        disabled: !mine || !editor.compareFile || editor.compareFile.key === mine.key,
        onSelect: () => mine && editor.compareFile && compare(editor.compareFile, mine),
      },
    ],
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
              label: "Dock back into the main window",
              icon: "i-lucide-panel-top-close",
              onSelect: () => windows.dockBack(),
            },
          ]),
    ],
    [
      { label: "Copy path", onSelect: () => void Clipboard.SetText(mine?.path ?? row.path) },
      {
        label: "Reveal in explorer",
        disabled: !REVEAL?.when(),
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
    disabled: !activeTab.value,
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
          :data-group="group.id"
          class="flex min-w-0 items-start overflow-x-auto overflow-y-hidden [scrollbar-color:auto] [scrollbar-width:auto]"
          @wheel.passive="strip && !$event.deltaX && (strip.scrollLeft += $event.deltaY)"
          @dragover="allow"
          @drop="drop"
          @dragend="dropOutside"
        >
          <UContextMenu
            v-for="row in rows"
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
              @click="select(row)"
              @dblclick="editor.pin(row.tab.id)"
              @auxclick.middle="closeTabs([row.tab])"
            >
              <UIcon v-if="row.tab.side" name="i-lucide-git-compare" class="size-3.5 shrink-0 text-dimmed" />
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
                  :class="
                    row.unsaved
                      ? 'hidden group-hover:flex'
                      : row.tab.id === group.active
                        ? ''
                        : 'invisible group-hover:visible'
                  "
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
          <UTooltip v-if="corner" text="Wiki pane" :kbds="findKbds('editor.toggleWiki')">
            <UButton
              icon="i-lucide-book-open"
              square
              size="sm"
              :color="editor.wiki ? 'primary' : 'neutral'"
              :variant="editor.wiki ? 'soft' : 'ghost'"
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
      <OriginChip :origin="place.origin" dot />
      <span>{{ workspace.originName(place.origin) }}</span>
      <template v-for="(part, index) in crumbs" :key="index">
        <UIcon name="i-lucide-chevron-right" class="size-3 shrink-0" />
        <span :class="{ 'font-mono text-highlighted': index > folders, 'text-toned': index === folders }">{{
          part
        }}</span>
      </template>
      <span class="grow" />
      <template v-if="replacement">
        Replaced by <OriginChip :origin="replacement.origin" dot />
        <span class="text-toned">{{ workspace.originName(replacement.origin) }}</span>
        <UButton label="Compare" variant="ghost" size="xs" @click="compare(place, replacement)" />
      </template>
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
      <div v-else-if="!activeTab" class="flex flex-1 items-center justify-center bg-(--bg-0) text-12 text-dimmed">
        Open a file from the explorer, or Ctrl P
      </div>
      <div
        v-if="over"
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
