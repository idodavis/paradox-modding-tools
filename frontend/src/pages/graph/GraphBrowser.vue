<!--
  The browser pane: a search field, a tab for each catalog of the game, and the
  catalog's definitions under the game's own groups, which open and close.
-->
<script setup lang="ts">
import { computed, reactive, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { useRouter } from "vue-router";
import { Type } from "@internal/lang/models";
import type { Node } from "@services/graph/models";
import { describeCount, toSentence } from "../../lib/format";
import { graphCatalogQuery, graphCatalogsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import { arrangeCatalog, type Section } from "./catalog";
import NodeIcon from "./NodeIcon.vue";
import PaintingBand from "./PaintingBand.vue";
import { labelOf, TYPE_ICONS } from "./nodes";

const { subject = undefined } = defineProps<{
  /** The node the page is about: its row has a fill and its catalog shows. */
  subject?: Node;
}>();
const emit = defineEmits<{ select: [id: string] }>();

/** Line is one line of the list: a heading of a group, or a row. */
type Line = { group: Node; depth: number } | { row: Node; depth: number };

/** EVENTS_LISTED is how many events the list holds until typing narrows it. */
const EVENTS_LISTED = 200;
const SEARCH_DEBOUNCE_MS = 200;
/** INDENT is how far each level of groups moves a line in, in rem. */
const INDENT = 0.75;
/** VIRTUAL draws only the lines in view. */
const VIRTUAL = { estimateSize: 28, useAnimationFrameWithResizeObserver: true };

const router = useRouter();
const workspace = useWorkspaceStore();
const term = shallowRef("");
const search = refDebounced(term, SEARCH_DEBOUNCE_MS);
/** picked is the label of the catalog the user chose, "" for the subject's. */
const picked = shallowRef("");
const opened = reactive(new Set<string>());

const { data: catalogs } = useQuery(() => ({ ...graphCatalogsQuery, enabled: workspace.isOpen() }));
const followed = computed(
  () => subject && catalogs.value?.find((catalog) => catalog.types?.includes(subject.type || Type.TypeEvent)),
);
const current = computed(
  () => catalogs.value?.find((catalog) => catalog.label === picked.value) ?? followed.value ?? catalogs.value?.[0],
);
const isEvents = computed(() => !!current.value?.types?.includes(Type.TypeEvent));
const total = computed(() => current.value?.count ?? 0);
const { data: rows, isPending } = useQuery(() => ({
  ...graphCatalogQuery({
    types: current.value?.types ?? [],
    query: search.value,
    limit: isEvents.value ? EVENTS_LISTED : total.value,
  }),
  enabled: workspace.isOpen() && !!current.value,
  placeholderData: (previous) => (search.value ? previous : undefined),
}));
const lines = computed((): Line[] => {
  const { sections, loose } = arrangeCatalog(current.value?.groups ?? [], rows.value ?? []);
  return [...sections.flatMap((section) => listLines(section, 0)), ...loose.map((row) => ({ row, depth: 0 }))];
});
const hint = computed(() =>
  isEvents.value && !search.value && rows.value && rows.value.length < total.value
    ? `${describeCount(rows.value.length)} of ${describeCount(total.value)} shown. Type to narrow them.`
    : "",
);

watch(
  () => followed.value?.label,
  () => (picked.value = ""),
);
watch(
  [() => subject?.group, () => current.value?.groups],
  ([group, groups]) => {
    const walked = new Set<string>();
    for (let id = group; id && !walked.has(id); id = groups?.find((wider) => wider.id === id)?.group) {
      walked.add(id);
      opened.add(id);
    }
  },
  { immediate: true },
);

const list = useTemplateRef("list");
/** shown is the id of the subject whose row the list has scrolled to. */
let shown = "";
watch(
  [() => subject?.id, lines],
  ([id]) => {
    const position = lines.value.findIndex((line) => "row" in line && line.row.id === id);
    if (position < 0 || shown === id) return;

    shown = id ?? "";
    list.value?.virtualizer?.scrollToIndex(position, { align: "center" });
  },
  { flush: "post" },
);

/**
 * listLines lists a section's heading and, where it is open, its rows and the
 * sections inside it. A search leaves out a section it finds nothing in, and
 * a group that holds one row is that row alone.
 */
function listLines(section: Section, depth: number): Line[] {
  if (section.group.count === 1 && section.rows.length === 1) return [{ row: section.rows[0], depth }];

  const inside = isOpen(section.group)
    ? [
        ...section.rows.map((row) => ({ row, depth: depth + 1 })),
        ...section.sections.flatMap((inner) => listLines(inner, depth + 1)),
      ]
    : [];
  if (search.value && !inside.length) return [];

  return [{ group: section.group, depth }, ...inside];
}

/** isOpen reports whether a group shows its rows. All do during a search. */
function isOpen(group: Node): boolean {
  return !!search.value || opened.has(group.id);
}

/** pickCatalog shows a catalog whole: a search belongs to one catalog. */
function pickCatalog(label: string): void {
  picked.value = label;
  term.value = "";
}

function toggle(group: Node): void {
  if (!opened.delete(group.id)) opened.add(group.id);
}

function indent(depth: number): { paddingInlineStart: string } {
  return { paddingInlineStart: `${0.5 + depth * INDENT}rem` };
}
</script>

<template>
  <aside class="flex h-full min-h-0 w-full flex-col border-e border-default bg-muted">
    <div class="flex items-center gap-1 p-3 pb-2">
      <UButton
        icon="i-lucide-arrow-left"
        variant="ghost"
        color="neutral"
        size="sm"
        square
        aria-label="Back"
        @click="router.back()"
      />
      <UButton
        icon="i-lucide-arrow-right"
        variant="ghost"
        color="neutral"
        size="sm"
        square
        aria-label="Forward"
        @click="router.forward()"
      />
      <UInput
        v-model="term"
        icon="i-lucide-search"
        size="sm"
        :placeholder="`Search ${current?.label ?? ''}`"
        class="min-w-0 grow"
      />
    </div>
    <div class="flex flex-wrap gap-1 px-3 pb-3">
      <UButton
        v-for="catalog in catalogs"
        :key="catalog.label"
        :icon="TYPE_ICONS[catalog.types?.[0] ?? '']"
        :variant="catalog.label === current?.label ? 'soft' : 'ghost'"
        color="neutral"
        size="sm"
        class="grow justify-start whitespace-nowrap"
        :aria-pressed="catalog.label === current?.label"
        @click="pickCatalog(catalog.label)"
      >
        {{ toSentence(catalog.label) }}
        <span class="ms-auto ps-2 text-11 font-normal text-dimmed tabular-nums">{{
          describeCount(catalog.count)
        }}</span>
      </UButton>
    </div>
    <UScrollArea
      v-if="lines.length"
      ref="list"
      v-slot="{ item }"
      :items="lines"
      :virtualize="VIRTUAL"
      class="min-h-0 flex-1 border-t border-default p-1.5"
    >
      <PaintingBand v-if="'group' in item" :painting="item.group.painting" class="rounded-md">
        <button
          type="button"
          class="flex h-7.5 w-full items-center gap-1.5 pe-2 text-start text-12 font-semibold text-toned hover:bg-elevated/60"
          :style="indent(item.depth)"
          :aria-expanded="isOpen(item.group)"
          @click="toggle(item.group)"
        >
          <UIcon
            :name="isOpen(item.group) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
            class="size-3.5 shrink-0 text-dimmed"
          />
          <span class="truncate">{{ labelOf(item.group) }}</span>
          <span v-if="!search" class="ms-auto rounded-sm bg-muted/80 px-1 text-11 font-normal text-muted tabular-nums">
            {{ describeCount(item.group.count) }}
          </span>
        </button>
      </PaintingBand>
      <button
        v-else
        type="button"
        class="flex h-6.5 w-full items-center gap-2 rounded-sm pe-2 text-start text-12 hover:bg-elevated"
        :class="item.row.id === subject?.id ? 'bg-accented text-highlighted hover:bg-accented' : 'text-toned'"
        :style="indent(item.depth)"
        :aria-current="item.row.id === subject?.id"
        @click="emit('select', item.row.id)"
      >
        <NodeIcon :node="item.row" class="size-4.5" />
        <span class="truncate" :class="!item.row.title && 'font-mono'">{{ labelOf(item.row) }}</span>
        <code
          v-if="isEvents && item.row.title"
          class="ms-auto max-w-2/5 shrink-0 truncate font-mono text-11 text-dimmed"
        >
          {{ item.row.name }}
        </code>
      </button>
    </UScrollArea>
    <p v-else-if="search && !isPending" class="border-t border-default px-3 py-3 text-12 text-dimmed">
      Nothing matches.
    </p>
    <p v-if="hint" class="border-t border-default px-3 py-1.5 text-11 text-dimmed">{{ hint }}</p>
  </aside>
</template>
