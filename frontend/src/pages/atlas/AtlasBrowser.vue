<!-- The browser: the game's goods, buildings, technologies and events, a tab each, listed under the game's own groups. -->
<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { useRouter } from "vue-router";

import { Type } from "@internal/lang/models";
import type { CatalogSection, Node } from "@services/atlas/models";
import { describeCount, toSentence } from "../../lib/format";
import { catalogQuery, catalogsQuery, EVERY_ROW } from "../../queries";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import NodePicture, { labelOf, TYPE_ICONS } from "./NodePicture.vue";
import PaintingBand from "./PaintingBand.vue";

const { subject = undefined } = defineProps<{
  /** The subject's node, whose row is filled. */
  subject?: Node;
}>();
const emit = defineEmits<{ select: [id: string] }>();

/** A Line is one line of the list: a group's heading or a row, indented by its depth. */
type Line = { depth: number } & ({ group: Node } | { row: Node });

/** Events are listed up to this many until typing narrows them; any other catalog lists every row. */
const EVENTS_LISTED = 200;
const SEARCH_DEBOUNCE_MS = 200;
/** How far a line is indented for each group above it, in rem. */
const INDENT = 0.75;
/** VIRTUAL draws only the lines in view. */
const VIRTUAL = { estimateSize: 28 };

const router = useRouter();
const workspace = useWorkspaceStore();
const list = useTemplateRef("list");
const { data: catalogs } = useQuery(() => ({ ...catalogsQuery, enabled: workspace.isOpen() }));
/** The catalog the user picked. It is "" while the list follows the subject. */
const picked = shallowRef("");
const current = computed(
  () =>
    catalogs.value?.find((catalog) => catalog.label === picked.value) ??
    catalogs.value?.find((catalog) => subject?.type && catalog.types?.includes(subject.type)) ??
    catalogs.value?.[0],
);
const term = shallowRef("");
const search = refDebounced(term, SEARCH_DEBOUNCE_MS);
const isEvents = computed(() => !!current.value?.types?.includes(Type.TypeEvent));
const { data: sections, isPending } = useQuery(() => ({
  ...catalogQuery({
    types: current.value?.types ?? [],
    query: search.value.trim(),
    limit: isEvents.value ? EVENTS_LISTED : EVERY_ROW,
  }),
  enabled: workspace.isOpen() && !!current.value,
}));
const opened = shallowRef<ReadonlySet<string>>(new Set());
const lines = computed(() => flatten(sections.value ?? [], 0));
const hint = computed(() => {
  const { shown = 0, found = 0 } = sections.value?.[0] ?? {};
  return shown < found ? `${describeCount(shown)} of ${describeCount(found)} shown. Type to narrow them.` : "";
});

// The list follows the subject again from the next subject of another catalog.
watch(
  () => subject?.type,
  () => (picked.value = ""),
);
// The subject's groups open and the list scrolls to its row, once for each subject.
let revealed = "";
watch([sections, () => subject?.id], async ([listed, id]) => {
  const groups = id && id !== revealed ? findGroups(listed ?? [], id) : undefined;
  if (!id || !groups) return;
  revealed = id;
  opened.value = new Set([...opened.value, ...groups]);
  await nextTick();
  const position = lines.value.findIndex((line) => "row" in line && line.row.id === id);
  if (position >= 0) list.value?.virtualizer?.scrollToIndex(position, { align: "center" });
});

/** flatten returns the sections as the list's lines, a closed group's rows left out. A search opens every group. */
function flatten(listed: CatalogSection[], depth: number): Line[] {
  return listed.flatMap((section) => {
    const rows = (section.rows ?? []).map((row) => ({ row, depth: section.group.id ? depth + 1 : depth }));
    if (!section.group.id) return rows;
    const inside = isOpen(section.group) ? [...flatten(section.sections ?? [], depth + 1), ...rows] : [];
    return [{ group: section.group, depth }, ...inside];
  });
}

/** findGroups returns the ids of the groups a row stands under, or undefined where the sections do not list it. */
function findGroups(listed: CatalogSection[], id: string): string[] | undefined {
  for (const section of listed) {
    const inner = section.rows?.some((row) => row.id === id) ? [] : findGroups(section.sections ?? [], id);
    if (inner) return [section.group.id, ...inner];
  }
  return undefined;
}

function isOpen(group: Node): boolean {
  return !!search.value || opened.value.has(group.id);
}

function toggle(group: Node): void {
  const next = new Set(opened.value);
  if (!next.delete(group.id)) next.add(group.id);
  opened.value = next;
}

/** pickCatalog shows a catalog. A search belongs to one catalog, so picking another empties it. */
function pickCatalog(label: string): void {
  picked.value = label;
  term.value = "";
}

function indent(depth: number) {
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
        :title="labelOf(item.row)"
        @click="emit('select', item.row.id)"
      >
        <NodePicture :node="item.row" class="size-4.5" />
        <span class="truncate" :class="!item.row.title && 'font-mono'">{{ labelOf(item.row) }}</span>
        <code
          v-if="isEvents && item.row.title"
          class="ms-auto max-w-2/5 shrink-0 truncate font-mono text-11 text-dimmed"
        >
          {{ item.row.name }}
        </code>
        <OriginChip
          v-if="item.row.origin !== VANILLA"
          :origin="item.row.origin"
          dot
          :class="!(isEvents && item.row.title) && 'ms-auto'"
        />
      </button>
    </UScrollArea>
    <p v-else-if="search && !isPending" class="border-t border-default px-3 py-3 text-12 text-dimmed">
      Nothing matches.
    </p>
    <p v-if="hint" class="border-t border-default px-3 py-1.5 text-11 text-dimmed">{{ hint }}</p>
  </aside>
</template>
