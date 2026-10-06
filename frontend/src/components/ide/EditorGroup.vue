<script setup lang="ts">
/**
 * An editor group (the Ide board's editor): its tabs, each with its file's icon, a dot while unsaved and a lock on
 * the game's files, and a menu of what to do with it; a tab dragged onto a tab or the strip goes there, onto the
 * editor's edge into a new group there. The breadcrumb, the file's origin, folders and name, then where the caret
 * is (Inspect.within), and a Markdown file's Raw or Rich; then the shown tab's editor. The group at the top right
 * ends its strip with the Wiki button.
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";
import { saveAll, saveFile } from "../../ide/monacoEditor";
import { useCompare } from "../../composables/useCompare";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { hoverQuery } from "../../queries";
import { useIdeStore, type Edge } from "../../stores/ide";
import { useWorkspaceStore } from "../../stores/workspace";
import { iconOf } from "./ExplorerTree.vue";

const { workspaceId, groupId } = defineProps<{ workspaceId: string; groupId: number }>();

const ide = useIdeStore();
const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);
const compare = useCompare(() => workspaceId);

const group = computed(() => ide.group(groupId));
const active = computed(() => group.value?.active ?? "");
const located = computed(() => locate(active.value));
const readOnly = (path: string) => !!locate(path)?.root.readOnly;

/** A tab's label: its file's name. */
function name(path: string): string {
  const rel = locate(path)?.rel ?? path;
  return rel.slice(Math.max(rel.lastIndexOf("/"), rel.lastIndexOf("\\")) + 1);
}

function show(path: string): void {
  if (!group.value) return;
  group.value.active = path;
  ide.focused = groupId;
}

// a file's last tab closed with unsaved edits asks first, shown so its editor can save
const asking = shallowRef("");
function close(path: string): void {
  const elsewhere = ide.groups.some((one) => one.id !== groupId && one.files.includes(path));
  if (elsewhere || !ide.unsaved.has(path)) return ide.closeIn(groupId, path);
  show(path);
  asking.value = path;
}
async function answer(save: boolean): Promise<void> {
  const path = asking.value;
  asking.value = "";
  if (save) await saveFile(path);
  ide.closeIn(groupId, path);
}

// closing many tabs closes all but the unsaved, which stay to be saved or closed one at a time
function closeAll(keep?: string): void {
  for (const path of [...(group.value?.files ?? [])]) if (path !== keep && !ide.unsaved.has(path)) ide.closeIn(groupId, path);
}
const actions = computed<DropdownMenuItem[]>(() => [
  { label: "Close the others", onSelect: () => closeAll(active.value) },
  { label: "Close all", onSelect: () => closeAll() },
]);

/** A tab's menu: save and close it, split it off, compare it. */
function tabMenu(path: string): ContextMenuItem[][] {
  const picked = ide.comparing;
  return [
    [
      { label: "Save", kbds: ["Ctrl S"], disabled: !ide.unsaved.has(path), onSelect: () => void saveFile(path) },
      { label: "Save all", disabled: !ide.unsaved.size, onSelect: () => void saveAll() },
    ],
    [
      { label: "Close", onSelect: () => close(path) },
      { label: "Close the others", onSelect: () => closeAll(path) },
      { label: "Close all", onSelect: () => closeAll() },
    ],
    [
      { label: "Split right", icon: "i-lucide-columns-2", kbds: ["Ctrl \\"], onSelect: () => ide.split("right", path, groupId) },
      { label: "Split down", icon: "i-lucide-rows-2", onSelect: () => ide.split("down", path, groupId) },
    ],
    [
      { label: "Compare with the game's copy", disabled: readOnly(path), onSelect: () => void compare.withGame(path) },
      { label: "Select for compare", onSelect: () => (ide.comparing = path) },
      {
        label: picked && picked !== path ? `Compare with ${name(picked)}` : "Compare with selected",
        disabled: !picked || picked === path,
        onSelect: () => compare.files(picked, path),
      },
    ],
    [{ label: "Copy path", onSelect: () => void Clipboard.SetText(path) }],
  ];
}

// the shown tab scrolls into view in a strip of more tabs than fit
const strip = useTemplateRef("strip");
watch(active, (path) => strip.value?.querySelector(`[data-tab="${CSS.escape(path)}"]`)?.scrollIntoView({ inline: "nearest" }), { flush: "post" });
// the wheel turned over the strip scrolls it sideways
function scrollTabs(e: WheelEvent): void {
  if (strip.value && !e.deltaX) strip.value.scrollLeft += e.deltaY;
}

// a tab dropped on a tab goes before it, on the strip after the last; on the editor, where its zone says
function dropTab(edge: Edge, before?: string): void {
  const dragged = ide.dragging;
  ide.dragging = null;
  zone.value = null;
  if (dragged) ide.drop(dragged.path, dragged.from, groupId, edge, before);
}
const zone = shallowRef<Edge | null>(null);
function zoneAt(e: DragEvent): void {
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
const caret = refDebounced(computed(() => (ide.focused === groupId ? ide.caret : null)), 300);
const { data: card } = useQuery(() => ({
  ...hoverQuery({ id: workspaceId, path: active.value, line: caret.value?.line ?? 0, col: caret.value?.col ?? 0 }),
  enabled: !!caret.value && !!active.value,
}));
const crumbs = computed(() => [...(located.value?.rel.split("/") ?? []), ...(card.value?.within ?? [])]);
const folders = computed(() => (located.value?.rel.split("/").length ?? 0) - 1);

const markdown = computed(() => active.value.toLowerCase().endsWith(".md"));
const VIEWS = [
  { label: "Raw", value: "raw" },
  { label: "Rich", value: "rich" },
];
function setView(view: string | number): void {
  if (view === "rich") ide.rich.add(active.value);
  else ide.rich.delete(active.value);
}
</script>

<template>
  <section class="flex min-h-0 min-w-0 flex-1 flex-col" @pointerdown="ide.focused = groupId">
    <UModal :open="!!asking" title="Save your changes?" @update:open="(open) => !open && (asking = '')">
      <template #body>
        <p class="text-13 text-toned">{{ name(asking) }} has changes that are not saved.</p>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton label="Cancel" @click="asking = ''" />
          <UButton label="Don't save" @click="answer(false)" />
          <UButton label="Save" color="primary" variant="solid" @click="answer(true)" />
        </div>
      </template>
    </UModal>
    <div class="flex h-9 shrink-0 items-stretch border-b border-default bg-muted" @dragover.prevent @drop="dropTab('middle')">
      <!-- the tabs keep their names; more than fit scroll, by the wheel or a bar under the pointer, the app's 6px one
           (Chromium draws ::-webkit-scrollbar only where scrollbar-width and -color are auto) over the tabs' foot -->
      <div
        ref="strip"
        class="flex min-w-0 items-start overflow-x-auto overflow-y-hidden [scrollbar-color:auto] [scrollbar-width:auto]"
        @wheel.passive="scrollTabs"
      >
        <UContextMenu v-for="path in group?.files" :key="path" :items="tabMenu(path)" :ui="{ content: 'w-64' }">
          <div
            :data-tab="path"
            draggable="true"
            class="group relative flex h-8.75 shrink-0 cursor-pointer items-center gap-1.75 border-e border-default ps-3 pe-1.5 text-12 whitespace-nowrap"
            :class="
              path === active
                ? 'bg-(--bg-0) text-highlighted before:absolute before:inset-x-0 before:top-0 before:h-px before:bg-primary'
                : 'text-dimmed hover:text-toned'
            "
            :title="locate(path)?.rel ?? path"
            @click="show(path)"
            @auxclick.middle="close(path)"
            @dragstart="ide.dragging = { path, from: groupId }"
            @dragend="ide.dragging = null"
            @dragover.prevent
            @drop.stop="dropTab('middle', path)"
          >
            <img v-if="iconOf(path)" :src="iconOf(path)" alt="" class="size-3.5 shrink-0" />
            <UIcon v-else name="i-lucide-file" class="size-3.5 shrink-0" />
            <span class="truncate">{{ name(path) }}</span>
            <UIcon v-if="readOnly(path)" name="i-lucide-lock" class="size-3 shrink-0 text-dimmed" />
            <!-- an unsaved tab's dot turns into its close button under the pointer -->
            <span class="flex size-5 shrink-0 items-center justify-center">
              <span v-if="ide.unsaved.has(path)" class="size-1.5 rounded-full bg-(--fg-2) group-hover:hidden" />
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Close"
                :class="ide.unsaved.has(path) ? 'hidden group-hover:flex' : path === active ? '' : 'invisible group-hover:visible'"
                @click.stop="close(path)"
              />
            </span>
          </div>
        </UContextMenu>
      </div>
      <span class="grow" />
      <div class="flex shrink-0 items-center gap-0.5 px-2">
        <UTooltip text="Split right" :kbds="['Ctrl \\']">
          <UButton icon="i-lucide-columns-2" variant="ghost" size="sm" square aria-label="Split right" :disabled="!active" @click="ide.split('right', active, groupId)" />
        </UTooltip>
        <UDropdownMenu :items="actions">
          <UButton icon="i-lucide-ellipsis" variant="ghost" size="sm" square aria-label="Tab actions" />
        </UDropdownMenu>
        <template v-if="groupId === ide.cornerGroup">
          <USeparator orientation="vertical" class="mx-1 h-4" />
          <UTooltip text="Wiki pane" :kbds="['Ctrl Shift W']">
            <UButton
              label="Wiki"
              icon="i-lucide-book-open"
              size="xs"
              :color="ide.wikiOpen ? 'primary' : 'neutral'"
              :variant="ide.wikiOpen ? 'soft' : 'outline'"
              :aria-pressed="ide.wikiOpen"
              @click="ide.wikiOpen = !ide.wikiOpen"
            />
          </UTooltip>
        </template>
      </div>
    </div>
    <div v-if="active" class="flex h-7 shrink-0 items-center gap-1.5 overflow-hidden border-b border-default bg-(--bg-0) px-3.5 text-12 whitespace-nowrap text-dimmed">
      <template v-if="located">
        <OriginDot :color="store.originColor(located.root.origin ?? '')" small />
        <span>{{ store.originName(located.root.origin ?? "") }}</span>
      </template>
      <template v-for="(part, i) in crumbs" :key="i">
        <UIcon name="i-lucide-chevron-right" class="size-3 shrink-0" />
        <span :class="{ 'font-mono text-highlighted': i > folders, 'text-toned': i === folders }">{{ part }}</span>
      </template>
      <span class="grow" />
      <UTabs v-if="markdown" :model-value="ide.rich.has(active) ? 'rich' : 'raw'" :items="VIEWS" :content="false" size="xs" @update:model-value="setView" />
    </div>
    <div class="relative flex min-h-0 flex-1 flex-col">
      <CodeEditor v-if="active" :key="active" :workspace-id="workspaceId" :path="active" :group="groupId" />
      <div v-else class="flex flex-1 items-center justify-center bg-(--bg-0) text-12 text-dimmed">Open a file from the explorer, or Ctrl P</div>
      <!-- while a tab is dragged: where it would go -->
      <div v-if="ide.dragging" class="absolute inset-0 z-20" @dragover.prevent="zoneAt" @dragleave="zone = null" @drop.prevent="dropTab(zone ?? 'middle')">
        <div v-if="zone" class="pointer-events-none absolute bg-(--accent-soft) ring-1 ring-primary ring-inset" :class="ZONES[zone]" />
      </div>
    </div>
  </section>
</template>
