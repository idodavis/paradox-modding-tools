<script setup lang="ts">
/**
 * An editor group: its tabs, each with its file's icon, a dot while unsaved and a lock on
 * the game's files, and a menu of what to do with it (a diff tab's is its file's, its diff named after it); a tab dragged onto a tab or the strip goes there, onto the
 * editor's edge into a new group there. The breadcrumb, the file's origin, folders and name, then where the caret
 * is (Inspect.within), and the view switch of a kind of file with views (Markdown's Raw or Rich, a GUI file's Code,
 * Visual or Split) with its panel's toggle; then the shown tab's editor. The group at the top right
 * ends its strip with the Wiki button. A diff tab shows its file's diff (GitDiff) in place of the breadcrumb and editor.
 * A game file a mod replaces says so, with a compare. A tab moves into a window of its own, and back.
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";
import { useCompare } from "../../composables/useCompare";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { hoverQuery, replacedQuery } from "../../queries";
import { useEditorStore, type Edge } from "../../stores/editor";
import { IMAGES, tabOf, viewsOf } from "../../editor/tabs";
import { useWorkspaceStore } from "../../stores/workspace";
import { Close } from "@services/windowservice";
import { isMain, useWindowsStore, windowName, windowPart } from "../../stores/windows";
import { fileName } from "../../lib/format";
import UnsavedChangesModal from "../../ui/UnsavedChangesModal.vue";
import DrawPane from "../../draw/DrawPane.vue";
import { iconOf } from "./fileKinds";
import CodeEditor from "./CodeEditor.vue";
import GitDiff from "./GitDiff.vue";
import ImageView from "./ImageView.vue";
import OriginDot from "../../ui/OriginDot.vue";

const { workspaceId, groupId } = defineProps<{ workspaceId: string; groupId: number }>();

const editor = useEditorStore();
const windows = useWindowsStore();
const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);
const compare = useCompare(() => workspaceId);

const group = computed(() => editor.group(groupId));
const active = computed(() => group.value?.active ?? "");
const diff = computed(() => tabOf(active.value).diff);
const located = computed(() => locate(active.value));
const readOnly = (path: string) => !!locate(path)?.root.readOnly;

/** A tab's label: its file's name. */
function name(tab: string): string {
  const path = tabOf(tab).file;
  const rel = locate(path)?.rel ?? path;
  return fileName(rel);
}

function show(path: string): void {
  if (!group.value) return;
  group.value.active = path;
  editor.focused = groupId;
}

// a file's last tab (its own or a diff) closed with unsaved edits asks first, shown so its editor can save
const unsavedModal = useOverlay().create(UnsavedChangesModal);
async function close(tab: string): Promise<void> {
  const { file } = tabOf(tab);
  const elsewhere = editor.groups.some((one) =>
    one.tabs.some((other) => tabOf(other).file === file && (other !== tab || one.id !== groupId)),
  );
  if (elsewhere || !editor.unsaved.has(file)) return editor.closeTab(groupId, tab);
  show(tab);
  if (await unsavedModal.open({ files: [file], going: "Closing it" })) editor.closeTab(groupId, tab);
}

// closing many tabs closes all but the unsaved, which stay to be saved or closed one at a time
function closeAll(keep?: string): void {
  for (const tab of [...(group.value?.tabs ?? [])])
    if (tab !== keep && !editor.unsaved.has(tabOf(tab).file)) editor.closeTab(groupId, tab);
}
const actions = computed<DropdownMenuItem[]>(() => [
  { label: "Close the others", onSelect: () => closeAll(active.value) },
  { label: "Close all", onSelect: () => closeAll() },
]);

/** A tab's menu: save and close it, split it off, compare its file. */
function tabMenu(tab: string): ContextMenuItem[][] {
  const comparing = editor.comparing;
  const path = tabOf(tab).file;
  return [
    [
      { label: "Save", kbds: ["Ctrl S"], disabled: !editor.unsaved.has(path), onSelect: () => editor.save([path]) },
      { label: "Save all", disabled: !editor.unsaved.size, onSelect: () => editor.save() },
    ],
    [
      { label: "Close", onSelect: () => void close(tab) },
      { label: "Close the others", onSelect: () => closeAll(tab) },
      { label: "Close all", onSelect: () => closeAll() },
    ],
    [
      {
        label: "Split right",
        icon: "i-lucide-columns-2",
        kbds: ["Ctrl \\"],
        onSelect: () => editor.split("right", tab, groupId),
      },
      { label: "Split down", icon: "i-lucide-rows-2", onSelect: () => editor.split("down", tab, groupId) },
    ],
    [
      { label: "Compare with the game's copy", disabled: readOnly(path), onSelect: () => void compare.withGame(path) },
      { label: "Select for compare", onSelect: () => (editor.comparing = path) },
      {
        label: comparing && comparing !== path ? `Compare with ${name(comparing)}` : "Compare with selected",
        disabled: !comparing || comparing === path,
        onSelect: () => compare.files(comparing, path),
      },
    ],
    [
      {
        label: "Move into a new window",
        icon: "i-lucide-app-window",
        onSelect: () => void windows.openInNewWindow(tab),
      },
      ...(isMain
        ? []
        : [
            {
              label: "Dock back into the main window",
              icon: "i-lucide-panel-top-close",
              onSelect: () => void Close(windowName),
            },
          ]),
    ],
    [{ label: "Copy path", onSelect: () => void Clipboard.SetText(path) }],
  ];
}

// a game file the mods replace: the copy the game loads, which it compares with
const { data: copies } = useQuery(() => ({
  ...replacedQuery({ id: workspaceId, path: active.value }),
  enabled: !!active.value && !diff.value && readOnly(active.value),
}));
const loaded = computed(() => (readOnly(active.value) ? copies.value?.at(-1) : undefined));

// the shown tab scrolls into view in a strip of more tabs than fit
const strip = useTemplateRef("strip");
watch(
  active,
  (path) => strip.value?.querySelector(`[data-tab="${CSS.escape(path)}"]`)?.scrollIntoView({ inline: "nearest" }),
  { flush: "post" },
);
// the wheel turned over the strip scrolls it sideways
function scrollTabs(e: WheelEvent): void {
  if (strip.value && !e.deltaX) strip.value.scrollLeft += e.deltaY;
}

// a tab dragged, between windows too: the tab rides in the drag, and whether it is a game file, which alone the game's
// files' window takes
const TAB = "application/x-pmt-tab";
const GAME = "application/x-pmt-game";
function startDrag(e: DragEvent, path: string): void {
  editor.dragging = { tab: path, sourceGroup: groupId };
  e.dataTransfer?.setData(TAB, path);
  if (readOnly(tabOf(path).file)) e.dataTransfer?.setData(GAME, "");
  if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
}
// dropped outside this window where no window took it: a new window of its own
function endDrag(e: DragEvent, path: string): void {
  editor.dragging = null;
  const out =
    e.screenX < window.screenX ||
    e.screenX > window.screenX + window.outerWidth ||
    e.screenY < window.screenY ||
    e.screenY > window.screenY + window.outerHeight;
  if (e.dataTransfer?.dropEffect === "none" && out) void windows.openInNewWindow(path);
}
const takes = (e: DragEvent) =>
  !!e.dataTransfer?.types.includes(TAB) && (windowPart !== "game" || e.dataTransfer.types.includes(GAME));

// while a tab is dragged over the group: where it would go on the strip (before a tab, or at the end: null), and on
// the editor (its zone)
const over = shallowRef(false);
const landing = shallowRef<string | null>();
const zone = shallowRef<Edge | null>(null);
function hover(e: DragEvent, nextTab?: string | null): void {
  if (!takes(e)) return;
  e.preventDefault();
  over.value = true;
  if (nextTab !== undefined) landing.value = nextTab;
}
function leave(e: DragEvent): void {
  if (e.currentTarget instanceof Node && e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))
    return;
  over.value = false;
  landing.value = undefined;
  zone.value = null;
}
// a tab dropped on a tab goes before it, on the strip after the last; on the editor, where its zone says; one from
// another window (none dragged here) is asked of the window holding it, and placed when it comes
function dropTab(e: DragEvent, edge: Edge, nextTab?: string): void {
  const dragged = editor.dragging;
  const tab = e.dataTransfer?.getData(TAB);
  editor.dragging = null;
  over.value = false;
  landing.value = undefined;
  zone.value = null;
  if (!tab || !takes(e)) return;
  e.preventDefault();
  if (dragged) return editor.drop(dragged.tab, dragged.sourceGroup, groupId, edge, nextTab);
  windows.placing.set(tab, { group: groupId, edge, nextTab });
  editor.focused = groupId;
  void windows.requestTab(windowName, tab);
}
function zoneAt(e: DragEvent): void {
  if (!takes(e)) return;
  e.preventDefault();
  if (!(e.currentTarget instanceof HTMLElement)) return;
  const box = e.currentTarget.getBoundingClientRect();
  const x = (e.clientX - box.left) / box.width;
  const y = (e.clientY - box.top) / box.height;
  zone.value = x < 0.25 ? "left" : x > 0.75 ? "right" : y < 0.25 ? "up" : y > 0.75 ? "down" : "middle";
}
const ZONES: Record<Edge, string> = {
  left: "inset-y-0 left-0 w-1/2",
  right: "inset-y-0 right-0 w-1/2",
  up: "inset-x-0 top-0 h-1/2",
  down: "inset-x-0 bottom-0 h-1/2",
  middle: "inset-0",
};

// the breadcrumb's end: where the caret rests, read a moment after it stops
const caret = refDebounced(
  computed(() => (editor.focused === groupId ? editor.caret : null)),
  300,
);
const { data: card } = useQuery(() => ({
  ...hoverQuery({ id: workspaceId, path: active.value, line: caret.value?.line ?? 0, col: caret.value?.col ?? 0 }),
  enabled: !!caret.value && !!active.value && !diff.value,
}));
const crumbs = computed(() => [...(located.value?.rel.split("/") ?? []), ...(card.value?.within ?? [])]);
const folders = computed(() => (located.value?.rel.split("/").length ?? 0) - 1);

// a file with views (Markdown, SVG, GUI) switches on its bar; an image's visual is the image pane, and a kind that
// splits draws its visual in a pane of its own (Markdown's Rich is the editor's mode), beside the code in Split
const row = computed(() => viewsOf(located.value?.rel ?? active.value));
const shown = computed(() => editor.viewOf(active.value));
const ownPane = computed(() => !!row.value?.split);
const views = computed(
  () =>
    row.value && [
      { label: row.value.code, value: "code" },
      { label: row.value.visual, value: "visual" },
      ...(row.value.split ? [{ label: "Split", value: "split" }] : []),
    ],
);
function setView(view: string | number): void {
  editor.setView(active.value, view === "visual" || view === "split" ? view : "code");
}
const image = computed(() => IMAGES.test(active.value) && shown.value === "visual");
const halves = [
  { id: "code", slot: "code", order: 1, defaultSize: 50, minSize: 20 },
  { id: "visual", slot: "visual", order: 2, defaultSize: 50, minSize: 20 },
];
</script>

<template>
  <section
    class="flex min-h-0 min-w-0 flex-1 flex-col"
    @pointerdown="editor.focused = groupId"
    @dragover="hover($event)"
    @dragleave="leave"
  >
    <div
      class="flex h-9 shrink-0 items-stretch border-b border-default bg-muted"
      @dragover="hover($event, null)"
      @drop="dropTab($event, 'middle')"
    >
      <!-- the tabs keep their names; more than fit scroll, by the wheel or a bar under the pointer, the app's 6px one
           (Chromium draws ::-webkit-scrollbar only where scrollbar-width and -color are auto) over the tabs' foot -->
      <div
        ref="strip"
        class="flex min-w-0 items-start overflow-x-auto overflow-y-hidden [scrollbar-color:auto] [scrollbar-width:auto]"
        @wheel.passive="scrollTabs"
      >
        <UContextMenu v-for="path in group?.tabs" :key="path" :items="tabMenu(path)" :ui="{ content: 'w-64' }">
          <div
            :data-tab="path"
            draggable="true"
            class="group relative flex h-8.75 shrink-0 cursor-pointer items-center gap-1.75 border-e border-default ps-3 pe-1.5 text-12 whitespace-nowrap"
            :class="
              path === active
                ? 'bg-(--bg-0) text-highlighted before:absolute before:inset-x-0 before:top-0 before:h-px before:bg-primary'
                : 'text-dimmed hover:text-toned'
            "
            :title="locate(tabOf(path).file)?.rel ?? path"
            @click="show(path)"
            @auxclick.middle="close(path)"
            @dragstart="startDrag($event, path)"
            @dragend="endDrag($event, path)"
            @dragover.stop="hover($event, path)"
            @drop.stop="dropTab($event, 'middle', path)"
          >
            <UIcon v-if="tabOf(path).diff" name="i-lucide-git-compare" class="size-3.5 shrink-0 text-dimmed" />
            <img v-else-if="iconOf(path)" :src="iconOf(path)" alt="" class="size-3.5 shrink-0" />
            <UIcon v-else name="i-lucide-file" class="size-3.5 shrink-0" />
            <!-- a tab dragged here goes before this one -->
            <span
              v-if="landing === path"
              class="pointer-events-none absolute inset-y-1 -start-px w-0.5 rounded-full bg-primary"
            />
            <span class="truncate">{{ name(path) }}</span>
            <span v-if="tabOf(path).diff" class="text-dimmed">{{
              tabOf(path).commit?.slice(0, 7) ?? tabOf(path).diff
            }}</span>
            <UIcon v-else-if="readOnly(path)" name="i-lucide-lock" class="size-3 shrink-0 text-dimmed" />
            <!-- an unsaved tab's dot turns into its close button under the pointer -->
            <span class="flex size-5 shrink-0 items-center justify-center">
              <span
                v-if="editor.unsaved.has(tabOf(path).file)"
                class="size-1.5 rounded-full bg-(--fg-2) group-hover:hidden"
              />
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Close"
                :class="
                  editor.unsaved.has(tabOf(path).file)
                    ? 'hidden group-hover:flex'
                    : path === active
                      ? ''
                      : 'invisible group-hover:visible'
                "
                @click.stop="close(path)"
              />
            </span>
          </div>
        </UContextMenu>
        <span v-if="landing === null" class="pointer-events-none mt-1.5 h-6 w-0.5 shrink-0 rounded-full bg-primary" />
      </div>
      <span class="grow" />
      <div class="flex shrink-0 items-center gap-0.5 px-2">
        <UTooltip text="Split right" :kbds="['Ctrl \\']">
          <UButton
            icon="i-lucide-columns-2"
            variant="ghost"
            size="sm"
            square
            aria-label="Split right"
            :disabled="!active"
            @click="editor.split('right', active, groupId)"
          />
        </UTooltip>
        <UDropdownMenu :items="actions">
          <UButton icon="i-lucide-ellipsis" variant="ghost" size="sm" square aria-label="Tab actions" />
        </UDropdownMenu>
        <template v-if="isMain && groupId === editor.cornerGroup">
          <USeparator orientation="vertical" class="mx-1 h-4" />
          <UTooltip text="Wiki pane" :kbds="['Ctrl Shift W']">
            <UButton
              label="Wiki"
              icon="i-lucide-book-open"
              size="xs"
              color="primary"
              :variant="editor.wikiOpen ? 'soft' : 'outline'"
              :aria-pressed="editor.wikiOpen"
              @click="editor.wikiOpen = !editor.wikiOpen"
            />
          </UTooltip>
        </template>
      </div>
    </div>
    <div
      v-if="active && !diff"
      class="flex h-7 shrink-0 items-center gap-1.5 overflow-hidden border-b border-default bg-(--bg-0) px-3.5 text-12 whitespace-nowrap text-dimmed"
    >
      <template v-if="located">
        <OriginDot :color="store.originColor(located.root.origin ?? '')" small />
        <span>{{ store.originName(located.root.origin ?? "") }}</span>
      </template>
      <template v-for="(part, i) in crumbs" :key="i">
        <UIcon name="i-lucide-chevron-right" class="size-3 shrink-0" />
        <span :class="{ 'font-mono text-highlighted': i > folders, 'text-toned': i === folders }">{{ part }}</span>
      </template>
      <span class="grow" />
      <template v-if="loaded">
        Replaced by
        <OriginDot :color="store.originColor(loaded.origin)" small />
        <span class="text-toned">{{ store.originName(loaded.origin) }}</span>
        <UButton label="Compare" variant="ghost" size="xs" @click="compare.files(active, loaded.path)" />
      </template>
      <UTabs
        v-if="views"
        :model-value="shown"
        :items="views"
        :content="false"
        size="xs"
        @update:model-value="setView"
      />
      <UTooltip v-if="row?.panel" :text="`${row.panel} panel`">
        <UButton
          icon="i-lucide-panel-right"
          size="xs"
          square
          color="primary"
          :variant="editor.panels.get(active) ? 'soft' : 'ghost'"
          :aria-label="`${row.panel} panel`"
          :aria-pressed="!!editor.panels.get(active)"
          @click="editor.togglePanel(active)"
        />
      </UTooltip>
    </div>
    <div class="relative flex min-h-0 flex-1 flex-col">
      <GitDiff v-if="diff" :key="active" :workspace-id="workspaceId" :tab="active" />
      <ImageView v-else-if="image" :key="`image:${active}`" :path="active" />
      <USplitter
        v-else-if="ownPane && shown === 'split'"
        :items="halves"
        auto-save-id="pmt.ide.split"
        class="min-h-0 min-w-0 flex-1"
      >
        <template #code>
          <div class="relative flex min-h-0 min-w-0 flex-1 flex-col">
            <CodeEditor :key="active" :workspace-id="workspaceId" :path="active" :group="groupId" />
          </div>
        </template>
        <template #visual>
          <DrawPane :key="active" :workspace-id="workspaceId" :path="active" />
        </template>
      </USplitter>
      <DrawPane v-else-if="ownPane && shown === 'visual'" :key="active" :workspace-id="workspaceId" :path="active" />
      <CodeEditor v-else-if="active" :key="active" :workspace-id="workspaceId" :path="active" :group="groupId" />
      <div v-else class="flex flex-1 items-center justify-center bg-(--bg-0) text-12 text-dimmed">
        Open a file from the explorer, or Ctrl P
      </div>
      <!-- while a tab is dragged: where it would go -->
      <div
        v-if="editor.dragging || over"
        class="absolute inset-0 z-20"
        @dragover="zoneAt"
        @dragleave="zone = null"
        @drop="dropTab($event, zone ?? 'middle')"
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
