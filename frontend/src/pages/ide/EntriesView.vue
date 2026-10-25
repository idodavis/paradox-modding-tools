<!--
  The Entries view of the sidebar: what the active file's Visual view can draw, a coat with its picture and a GUI
  definition with its kind, each with its line, grouped by kind where the file holds several. A click draws it.
-->
<script setup lang="ts">
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { computed, shallowRef, useTemplateRef } from "vue";

import { Format } from "@internal/files/models";
import type { Symbol } from "@services/lsp/models";

import { coatQuery, drawablesQuery, loadQuery } from "../../queries";
import { useDrawStore } from "../../stores/draw";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import CoatDialog from "./draw/CoatDialog.vue";
import { drawCoat } from "./draw/coat";

/** A row of the list: a heading over a kind of entry, or an entry. */
type Row = { heading: string } | { drawable: Symbol };

const ICONS: Record<string, string> = { definition: "i-lucide-app-window" };

const store = useDrawStore();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const overlay = useOverlay();
const scroll = useTemplateRef<{ virtualizer?: { range?: { startIndex: number } | null } }>("scroll");

const place = computed(() => editor.activeFile());
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(place.value?.path ?? ""),
  enabled: workspace.isOpen() && !!place.value,
}));
const isCoat = computed(() => place.value?.format !== Format.FormatGUI);
const drawn = computed(() => (place.value ? store.file(place.value.path).drawable : null));

const search = shallowRef("");
const query = refDebounced(search, 150);
const rows = computed<Row[]>(() => {
  const matches = (drawables.value ?? []).filter(({ name }) => name.toLowerCase().includes(query.value.toLowerCase()));
  const groups = [...Map.groupBy(matches, ({ what }) => what)];
  if (groups.length < 2) return matches.map((drawable) => ({ drawable }));
  return groups.flatMap(([what, group]) => [{ heading: what }, ...group.map((drawable) => ({ drawable }))]);
});
const pinned = computed(() => {
  const top = scroll.value?.virtualizer?.range?.startIndex ?? 0;
  if (!rows.value[top] || "heading" in rows.value[top]) return "";
  const heading = rows.value.slice(0, top).findLast((row) => "heading" in row);
  return heading && "heading" in heading ? heading.heading : "";
});
const canvases = new WeakSet<HTMLCanvasElement>();

/** Draws an entry of the active file, showing the Visual view where its tab shows the code. */
function drawEntry(name: string): void {
  const tab = editor.activeTab();
  if (!place.value || !tab) return;
  store.draw(place.value.path, name);
  if (tab.view === "code") editor.setView(tab.id, "visual");
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
    <div class="flex h-9 shrink-0 items-center gap-1.5 ps-3 pe-1.5 font-semibold text-toned">
      In this file<span class="font-normal text-dimmed">{{ drawables?.length ?? 0 }}</span>
      <span class="grow" />
      <UTooltip v-if="drawables?.length && isCoat && !place?.readOnly" text="New coat of arms">
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
    <template v-if="drawables?.length">
      <UInput v-model="search" icon="i-lucide-search" size="sm" placeholder="Find an entry" class="mx-2 mb-1.5" />
      <div class="relative flex min-h-0 flex-1 flex-col">
        <div
          v-if="pinned"
          class="absolute inset-x-0 top-0 z-10 flex h-7 items-center bg-muted ps-3 text-11 font-semibold text-dimmed capitalize"
        >
          {{ pinned }}
        </div>
        <UScrollArea
          ref="scroll"
          v-slot="{ item }"
          :items="rows"
          :virtualize="{ estimateSize: isCoat ? 76 : 40, overscan: 8 }"
          class="min-h-0 flex-1"
        >
          <div
            v-if="'heading' in item"
            class="flex items-end ps-3 pb-1.5 text-11 font-semibold text-dimmed capitalize"
            :class="isCoat ? 'h-19' : 'h-10'"
          >
            {{ item.heading }}
          </div>
          <button
            v-else
            type="button"
            class="flex w-full min-w-0 items-center gap-2.5 px-3 text-start hover:bg-elevated"
            :class="[
              item.drawable.name === drawn && 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]',
              isCoat ? 'h-19' : 'h-10',
            ]"
            :title="item.drawable.name"
            @click="drawEntry(item.drawable.name)"
          >
            <canvas
              v-if="isCoat"
              :key="item.drawable.name"
              :ref="(element) => void buildThumbnail(element, item.drawable.name)"
              class="h-16 w-24 shrink-0 object-contain"
            />
            <UIcon
              v-else
              :name="ICONS[item.drawable.category] ?? 'i-lucide-shapes'"
              class="size-4 shrink-0 text-dimmed"
            />
            <span class="flex min-w-0 grow flex-col">
              <span class="truncate font-mono text-12 text-highlighted">{{ item.drawable.name }}</span>
              <span class="text-11 text-dimmed">
                <span class="capitalize">{{ isCoat ? "" : `${item.drawable.what} · ` }}</span
                >L{{ item.drawable.range.startLineNumber }}
              </span>
            </span>
          </button>
        </UScrollArea>
      </div>
    </template>
    <UEmpty
      v-else
      icon="i-lucide-shapes"
      title="Nothing to draw"
      description="Open a GUI file or a coat of arms file to list what it draws."
      variant="naked"
      size="sm"
      class="flex-1"
    />
  </div>
</template>
