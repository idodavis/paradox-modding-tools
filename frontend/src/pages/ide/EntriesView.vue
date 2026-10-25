<!--
  The Entries view of the sidebar: the active file's entries under a heading for each kind, each with its title in the
  workspace's language, its key and its line. A coat shows its picture, and a click opens the entry.
-->
<script setup lang="ts">
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, refDebounced } from "@vueuse/core";
import { computed, shallowRef, useTemplateRef } from "vue";

import { Format } from "@internal/files/models";
import type { Symbol } from "@services/lsp/models";

import { coatQuery, drawablesQuery, loadQuery, outlineQuery } from "../../queries";
import { useDrawStore } from "../../stores/draw";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import CoatDialog from "./draw/CoatDialog.vue";
import { drawCoat } from "./draw/coat";

/** A Heading is the row over a kind of entry, with how many entries of the list it heads. */
type Heading = { heading: string; count: number };
/** A row of the list: a heading, or an entry. */
type Row = Heading | { entry: Symbol };

const store = useDrawStore();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const overlay = useOverlay();
const scroll = useTemplateRef<{ virtualizer?: { range?: { startIndex: number } | null } }>("scroll");
const [DefineHeading, ReuseHeading] = createReusableTemplate<{ row: Heading }>();

const place = computed(() => editor.activeFile());
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(place.value?.path ?? ""),
  enabled: workspace.isOpen() && !!place.value,
}));
// a file the Visual view draws lists what it draws; any other lists its outline
const drawable = computed(() => !!drawables.value?.length);
const { data: outline } = useQuery(() => ({
  ...outlineQuery(place.value?.path ?? ""),
  enabled: workspace.isOpen() && !!place.value && drawables.value?.length === 0,
}));
const entries = computed(() => (drawable.value ? (drawables.value ?? []) : flatten(outline.value ?? [])));
const isCoat = computed(() => drawable.value && place.value?.format !== Format.FormatGUI);
const drawn = computed(() => (place.value ? store.file(place.value.path).drawable : null));

const search = shallowRef("");
const query = refDebounced(search, 150);
const rows = computed<Row[]>(() => {
  const text = query.value.toLowerCase();
  const matches = entries.value.filter(
    ({ name, title }) => name.toLowerCase().includes(text) || title.toLowerCase().includes(text),
  );
  const groups = [...Map.groupBy(matches, ({ database }) => database).values()];
  // a script file's rows do not say their kind, so its list has a heading even where it holds one kind
  if (drawable.value && groups.length < 2) return matches.map((entry) => ({ entry }));
  return groups.flatMap((group) => [
    { heading: group[0].what, count: group.length },
    ...group.map((entry) => ({ entry })),
  ]);
});
// the heading of the top row's kind, drawn over the list so that it stays at the top while its rows scroll
const pinned = computed(() => {
  const top = scroll.value?.virtualizer?.range?.startIndex ?? 0;
  return rows.value.slice(0, top + 1).findLast((row): row is Heading => "heading" in row);
});
const canvases = new WeakSet<HTMLCanvasElement>();

/**
 * Lists symbols with the symbols nested in each that are of its own database, such as a kingdom in an empire, in the
 * order they are written. A name script saves inside an entry, such as a saved scope, is left to the outline.
 */
function flatten(symbols: Symbol[]): Symbol[] {
  return symbols.flatMap((symbol) => [
    symbol,
    ...flatten((symbol.children ?? []).filter((child) => child.database === symbol.database)),
  ]);
}

/** Describes an entry under its name: its title, then its kind unless it is a coat. */
function describe(entry: Symbol): string {
  return [entry.title, isCoat.value ? "" : entry.what].filter(Boolean).join(" · ");
}

/**
 * Opens an entry of the active file. One the Visual view draws is drawn, in the Visual view where its tab shows the
 * code. Any other is shown in the code at its first line.
 */
function openEntry(entry: Symbol): void {
  const tab = editor.activeTab();
  if (!place.value || !tab) return;
  if (drawable.value) {
    store.draw(place.value.path, entry.name);
    if (tab.view === "code") editor.setView(tab.id, "visual");
    return;
  }
  editor.show(place.value, { lineNumber: entry.range.startLineNumber, column: entry.range.startColumn });
}

/** Draws a coat's picture on a canvas in the list. A canvas that is drawn already is left as it is. */
async function buildThumbnail(canvas: unknown, name: string): Promise<void> {
  if (!(canvas instanceof HTMLCanvasElement) || canvases.has(canvas) || !place.value) return;
  canvases.add(canvas);
  const result = await loadQuery(coatQuery({ path: place.value.path, name, picks: {} }));
  if (result) await drawCoat(canvas, result, 64);
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <DefineHeading v-slot="{ row }">
      <div class="flex h-7 items-center gap-2 bg-muted px-3 text-11 font-semibold text-dimmed">
        <span class="truncate capitalize">{{ row.heading }}</span>
        <span class="ms-auto font-normal tabular-nums">{{ row.count }}</span>
      </div>
    </DefineHeading>
    <div class="flex h-9 shrink-0 items-center gap-1.5 ps-3 pe-1.5 font-semibold text-toned">
      In this file<span class="font-normal text-dimmed">{{ entries.length }}</span>
      <span class="grow" />
      <UTooltip v-if="isCoat && !place?.readOnly" text="New coat of arms">
        <UButton
          icon="i-lucide-plus"
          aria-label="New coat of arms"
          variant="ghost"
          color="neutral"
          size="xs"
          square
          @click="void overlay.create(CoatDialog).open()"
        />
      </UTooltip>
    </div>
    <template v-if="entries.length">
      <UInput v-model="search" icon="i-lucide-search" size="sm" placeholder="Find an entry" class="mx-2 mb-1.5" />
      <div class="relative flex min-h-0 flex-1 flex-col">
        <div v-if="pinned" class="absolute inset-x-0 top-0 z-10">
          <ReuseHeading :row="pinned" />
        </div>
        <UScrollArea
          ref="scroll"
          v-slot="{ item }"
          :items="rows"
          :virtualize="{ estimateSize: isCoat ? 76 : 40, overscan: 8 }"
          class="min-h-0 flex-1"
        >
          <ReuseHeading v-if="'heading' in item" :row="item" />
          <button
            v-else-if="drawable"
            type="button"
            class="flex w-full min-w-0 items-center gap-2.5 px-3 text-start hover:bg-elevated"
            :class="[
              item.entry.name === drawn && 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]',
              isCoat ? 'h-19' : 'h-10',
            ]"
            :title="item.entry.name"
            @click="openEntry(item.entry)"
          >
            <canvas
              v-if="isCoat"
              :key="item.entry.name"
              :ref="(element) => void buildThumbnail(element, item.entry.name)"
              class="h-16 w-24 shrink-0 object-contain"
            />
            <UIcon v-else name="i-lucide-app-window" class="size-4 shrink-0 text-dimmed" />
            <span class="flex min-w-0 grow flex-col">
              <span class="flex min-w-0 items-baseline gap-2">
                <span class="truncate font-mono text-12 text-highlighted">{{ item.entry.name }}</span>
                <span class="ms-auto shrink-0 text-11 text-dimmed">L{{ item.entry.range.startLineNumber }}</span>
              </span>
              <span class="truncate text-11 text-dimmed first-letter:uppercase">{{ describe(item.entry) }}</span>
            </span>
          </button>
          <button
            v-else
            type="button"
            class="flex w-full min-w-0 flex-col px-3 py-1 text-start hover:bg-elevated"
            :title="item.entry.name"
            @click="openEntry(item.entry)"
          >
            <span class="flex min-w-0 items-baseline gap-2">
              <span class="truncate text-highlighted" :class="!item.entry.title && 'font-mono'">
                {{ item.entry.title || item.entry.name }}
              </span>
              <span class="ms-auto shrink-0 text-11 text-dimmed tabular-nums">
                {{ item.entry.range.startLineNumber }}
              </span>
            </span>
            <span v-if="item.entry.title" class="truncate font-mono text-11 text-dimmed">{{ item.entry.name }}</span>
          </button>
        </UScrollArea>
      </div>
    </template>
    <UEmpty
      v-else
      icon="i-lucide-shapes"
      title="No entries"
      description="Open a script, GUI or localization file to list its entries."
      variant="naked"
      size="sm"
      class="flex-1"
    />
  </div>
</template>
