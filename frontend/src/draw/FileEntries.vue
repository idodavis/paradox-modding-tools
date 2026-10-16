<script setup lang="ts">
/**
 * What a file's Visual view can draw, as a feed: "In this file" with the count, a new coat on a coat file the editor
 * can write, a search over the ids, and a row for each entry, built only while it is in view: a coat's with its
 * picture, a GUI entry's with its kind, each with its line; grouped by kind where the file holds several, the group's
 * heading pinned while its rows scroll. A click draws the entry. A file the Visual view does not draw says so.
 */
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced, useScroll, useVirtualList } from "@vueuse/core";
import type { Coat as DrawCoat } from "@services/internal/draw";
import type { Symbol } from "@services/internal/lsp";
import { useIdeRoots } from "../composables/useIdeRoots";
import { isReadOnly } from "../editor/models";
import { viewsOf } from "../editor/tabs";
import { coatsQuery, framesQuery, symbolsQuery } from "../queries";
import { COATS, ENTRY_GROUPS, entriesOf, useDrawStore } from "../stores/draw";
import { useEditorStore } from "../stores/editor";
import { CoatPicture } from "./coat";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A row of the feed: a group's heading, or an entry. */
type Row = { heading: string } | { entry: Symbol };

/** A row's height in pixels by the kind the file draws, a coat file's taller for its pictures; and a coat's picture's
 * height in its row, its box as wide as the widest field (3:2, Vic3's and EU5's). */
const ROW_HEIGHTS = { coat: 76, gui: 40 };
const THUMBNAIL = 64;
const THUMBNAIL_BOX = { height: `${THUMBNAIL}px`, width: `${(THUMBNAIL * 3) / 2}px` };

/** What a thumbnail hides: nothing. */
const NOTHING_HIDDEN = new Set<number>();

const store = useDrawStore();
const editor = useEditorStore();
const { locate } = useIdeRoots(() => workspaceId);
const drawing = computed(() => viewsOf(path)?.drawing);
const rowHeight = computed(() => ROW_HEIGHTS[drawing.value ?? "gui"]);
const record = computed(() => store.file(path));

// the file's entries the Visual view draws, those the search finds, grouped by kind where there are several kinds
const { data: symbols } = useQuery(() => symbolsQuery({ id: drawing.value ? workspaceId : "", path }));
const entries = computed(() => entriesOf(symbols.value ?? []));
const search = shallowRef("");
const rows = computed<Row[]>(() => {
  const matches = entries.value.filter((entry) => entry.name.toLowerCase().includes(search.value.toLowerCase()));
  const groups = [...Map.groupBy(matches, (entry) => ENTRY_GROUPS[entry.database].order)].sort(
    ([first], [second]) => first - second,
  );
  if (groups.length < 2) return matches.map((entry) => ({ entry }));
  return groups.flatMap(([, group]) => [
    { heading: ENTRY_GROUPS[group[0].database].label },
    ...group.map((entry) => ({ entry })),
  ]);
});
const { list, containerProps, wrapperProps, scrollTo } = useVirtualList(rows, {
  itemHeight: () => rowHeight.value,
  overscan: 8,
});
// a new search starts the feed at its top, where what it finds is
watch(search, () => scrollTo(0));
// the heading of the group the topmost row in view is in, pinned over the feed
const { y: scrollTop } = useScroll(containerProps.ref);
const pinned = computed(() => {
  const top = Math.floor(scrollTop.value / rowHeight.value);
  if (!rows.value[top] || "heading" in rows.value[top]) return "";
  const heading = rows.value.slice(0, top).findLast((row) => "heading" in row);
  return heading && "heading" in heading ? heading.heading : "";
});

// the pictures of the coats in view, read once the feed rests, cut to the frame the picture is
const idsInView = refDebounced(
  computed(() =>
    list.value.flatMap(({ data }) => ("entry" in data && data.entry.database === COATS ? [data.entry.name] : [])),
  ),
  150,
);
const { data: coats } = useQuery(() =>
  coatsQuery({ id: drawing.value === "coat" ? workspaceId : "", path, ids: idsInView.value }),
);
const pictures = computed(() => new Map(idsInView.value.map((id, index) => [id, coats.value?.[index] ?? null])));
/** A coat's picture in the feed, as a list of one where it is read, else none. */
const picturesOf = (id: string): DrawCoat[] => {
  const coat = pictures.value.get(id);
  return coat ? [coat] : [];
};
const { data: frames } = useQuery(() => framesQuery({ id: drawing.value === "coat" ? workspaceId : "" }));
const frame = computed(() => frames.value?.find((one) => one.label === record.value.frame)?.url ?? "");

/** Whether an entry is the one drawn. */
const isDrawn = (entry: Symbol) => entry.name === record.value.entry && entry.database === record.value.database;

/** Draws an entry, its file shown with the picture where only its code shows, the code at the entry's line. */
function pick(entry: Symbol): void {
  store.pickEntry(path, entry);
  if (editor.viewOf(path) === "code") editor.setView(path, "split");
  const { start } = entry.location.range;
  editor.open(path, { line: start.line, col: start.character });
}

/** Opens the new coat dialog for the file's mod. */
function newCoat(): void {
  store.coatDialog = { mode: "new", id: "", text: "", mod: locate(path)?.root.origin ?? "" };
}
</script>

<template>
  <div v-if="drawing" class="flex min-h-0 flex-1 flex-col">
    <div class="flex h-9 shrink-0 items-center gap-1.5 ps-3 pe-1.5 text-12 font-semibold text-toned">
      In this file<span class="font-normal text-dimmed">{{ entries.length }}</span>
      <span class="grow" />
      <UTooltip v-if="drawing === 'coat' && !isReadOnly(path)" text="New coat of arms">
        <UButton
          icon="i-lucide-plus"
          aria-label="New coat of arms"
          variant="ghost"
          color="neutral"
          size="xs"
          square
          @click="newCoat"
        />
      </UTooltip>
    </div>
    <UInput v-model="search" icon="i-lucide-search" size="sm" placeholder="Find an entry" class="mx-2 mb-1.5" />
    <div class="relative flex min-h-0 flex-1 flex-col">
      <div
        v-if="pinned"
        class="absolute inset-x-0 top-0 z-10 flex h-7 items-center bg-muted ps-3 text-11 font-semibold text-dimmed"
      >
        {{ pinned }}
      </div>
      <div v-bind="containerProps" class="min-h-0 flex-1">
        <div v-bind="wrapperProps">
          <template v-for="{ data, index } in list" :key="index">
            <div
              v-if="'heading' in data"
              class="flex items-end ps-3 pb-1.5 text-11 font-semibold text-dimmed"
              :style="{ height: `${rowHeight}px` }"
            >
              {{ data.heading }}
            </div>
            <button
              v-else
              type="button"
              class="flex w-full min-w-0 items-center gap-2.5 px-3 text-start hover:bg-elevated"
              :class="isDrawn(data.entry) && 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]'"
              :style="{ height: `${rowHeight}px` }"
              :title="data.entry.name"
              @click="pick(data.entry)"
            >
              <span
                v-if="data.entry.database === COATS"
                class="flex shrink-0 items-center justify-center overflow-hidden"
                :style="THUMBNAIL_BOX"
              >
                <span
                  v-for="(picture, ordinal) in picturesOf(data.entry.name)"
                  :key="ordinal"
                  class="block"
                  :style="{ zoom: THUMBNAIL / (picture.height || THUMBNAIL) }"
                >
                  <CoatPicture :coat="picture" :hidden="NOTHING_HIDDEN" :frame="frame" />
                </span>
              </span>
              <UIcon v-else :name="ENTRY_GROUPS[data.entry.database].icon" class="size-4 shrink-0 text-dimmed" />
              <span class="flex min-w-0 grow flex-col">
                <span class="truncate font-mono text-12 text-highlighted">{{ data.entry.name }}</span>
                <span class="text-11 text-dimmed"
                  >{{ data.entry.database === COATS ? "" : `${ENTRY_GROUPS[data.entry.database].kind} · ` }}L{{
                    data.entry.location.range.start.line + 1
                  }}</span
                >
              </span>
            </button>
          </template>
        </div>
      </div>
    </div>
  </div>
  <UEmpty
    v-else
    icon="i-lucide-shapes"
    description="Open a GUI or coat of arms file to list what it draws."
    variant="naked"
    size="sm"
    class="flex-1"
  />
</template>
