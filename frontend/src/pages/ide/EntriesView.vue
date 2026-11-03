<!--
  The Entries view of the sidebar, which Visual calls Structure. In Code it lists the active file's entries under a
  heading for each kind, each with its title in the workspace's language, its key and its line. In Visual it lists what
  the file draws: a GUI file's windows to pick from over the drawn window's widgets as a tree, a coat file's coats with
  their pictures.
-->
<script setup lang="ts">
import type { TreeItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, refDebounced } from "@vueuse/core";
import { computed, shallowRef, useTemplateRef } from "vue";

import { Format } from "@internal/files/models";
import type { Symbol } from "@services/lsp/models";

import { matchQuery, toSentence } from "../../lib/format";
import { coatQuery, drawablesQuery, loadQuery, outlineQuery, windowQuery } from "../../queries";
import { useDrawStore } from "../../stores/draw";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import CoatDialog from "./draw/CoatDialog.vue";
import { drawCoat } from "./draw/coat";

/** A Heading is the row over a kind of entry, with how many entries of the list it heads. */
type Heading = { heading: string; count: number };
/** A Row is a row of the list: a heading, or an entry. */
type Row = Heading | { entry: Symbol };

const store = useDrawStore();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const overlay = useOverlay();
const scroll = useTemplateRef<{ virtualizer?: { range?: { startIndex: number } | null } }>("scroll");
const [DefineHeading, ReuseHeading] = createReusableTemplate<{ row: Heading }>();

const place = computed(() => editor.activeFile());
const isVisual = computed(() => editor.mode === "visual");
// in Visual a GUI file lists its windows over the widgets of the drawn one, and any other file lists its coats
const isWindow = computed(() => isVisual.value && place.value?.format === Format.FormatGUI);
const isCoat = computed(() => isVisual.value && !isWindow.value);
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(place.value?.path ?? ""),
  enabled: workspace.isOpen() && !!place.value && isVisual.value,
}));
const { data: outline } = useQuery(() => ({
  ...outlineQuery(place.value?.path ?? ""),
  enabled: workspace.isOpen() && !!place.value && !isVisual.value,
}));
const entries = computed(() => (isVisual.value ? (drawables.value ?? []) : flatten(outline.value ?? [])));
const drawn = computed(() => (place.value ? store.file(place.value.path).drawable : null));
const { data: drawnWindow } = useQuery(() => ({
  ...windowQuery({ path: place.value?.path ?? "", name: drawn.value ?? "" }),
  enabled: workspace.isOpen() && isWindow.value && !!drawn.value,
}));

// the drawn window's widgets as rows of a tree, each under the widget that holds it; widget 0 is the window
const widgets = computed(() => {
  const list = drawnWindow.value?.widgets ?? [];
  const rows = list.map(({ id, name, type, role }) => ({
    value: String(id),
    label: name || type,
    role,
    defaultExpanded: id === 0,
    children: [] as TreeItem[],
  }));
  for (const { id, parent } of list) {
    const row = rows[id];
    if (id && row) rows[parent]?.children.push(row);
  }
  return rows;
});
// the names the window picker lists
const windowNames = computed(() => entries.value.map(({ name }) => name));
// the tree starts at the window, whose row holds every other widget
const windowRow = computed(() => widgets.value.slice(0, 1));
const selectedWidget = computed(() => widgets.value[store.file(place.value?.path ?? "").selected ?? -1]);

const search = shallowRef("");
const query = refDebounced(search, 150);
const rows = computed<Row[]>(() => {
  const matches = entries.value.filter(({ name, title }) => matchQuery(query.value, name, title));
  const groups = [...Map.groupBy(matches, ({ database }) => database).values()];
  // a script file's rows do not say their kind, so its list has a heading even where it holds one kind
  if (isCoat.value && groups.length < 2) return matches.map((entry) => ({ entry }));
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
 * flatten lists symbols with the symbols nested in each that are of its own database, such as a kingdom in an
 * empire, in the order they are written. A name script saves inside an entry, such as a saved scope, is left to the
 * outline.
 */
function flatten(symbols: Symbol[]): Symbol[] {
  return symbols.flatMap((symbol) => [
    symbol,
    ...flatten((symbol.children ?? []).filter((child) => child.database === symbol.database)),
  ]);
}

/**
 * openEntry opens an entry of the active file. A coat listed in Visual is drawn. Any other entry is shown in the code
 * at its first line.
 */
function openEntry(entry: Symbol): void {
  if (!place.value) return;
  if (isCoat.value) {
    store.draw(place.value.path, entry.name);
    return;
  }
  editor.show(place.value, { lineNumber: entry.range.startLineNumber, column: entry.range.startColumn });
}

/**
 * selectWidget selects the widget of a tree row in the drawn window. A click that would clear the row leaves the
 * selection as it is.
 */
function selectWidget(row: TreeItem | undefined): void {
  if (place.value && row) store.select(place.value.path, Number(row.value));
}

/** buildThumbnail draws a coat's picture on a canvas in the list. A canvas drawn already is left as it is. */
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
        <span class="truncate">{{ toSentence(row.heading) }}</span>
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
      <template v-if="isWindow">
        <USelectMenu
          :model-value="drawn ?? undefined"
          :items="windowNames"
          size="sm"
          class="mx-2 mb-1.5"
          @update:model-value="(name) => place && store.draw(place.path, name)"
        />
        <UTree
          :key="drawn ?? ''"
          :items="windowRow"
          :model-value="selectedWidget"
          :get-key="(row) => row.value"
          selection-behavior="replace"
          size="sm"
          virtualize
          class="min-h-0 flex-1 px-1"
          @update:model-value="selectWidget"
        >
          <template #item-label="{ item }">
            {{ item.label }}
            <span v-if="item.role" class="ms-1.5 text-11 font-normal text-dimmed">{{ item.role }}</span>
          </template>
        </UTree>
      </template>
      <template v-else>
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
              v-else-if="isCoat"
              type="button"
              class="flex h-19 w-full min-w-0 items-center gap-2.5 px-3 text-start hover:bg-elevated"
              :class="item.entry.name === drawn && 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]'"
              :title="item.entry.name"
              @click="openEntry(item.entry)"
            >
              <canvas
                :key="item.entry.name"
                :ref="(element) => void buildThumbnail(element, item.entry.name)"
                class="h-16 w-24 shrink-0 object-contain"
              />
              <span class="flex min-w-0 grow flex-col">
                <span class="flex min-w-0 items-baseline gap-2">
                  <span class="truncate font-mono text-12 text-highlighted">{{ item.entry.name }}</span>
                  <span class="ms-auto shrink-0 text-11 text-dimmed">L{{ item.entry.range.startLineNumber }}</span>
                </span>
                <span class="truncate text-11 text-dimmed first-letter:uppercase">{{ item.entry.title }}</span>
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
