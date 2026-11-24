<!-- The start page: the game's goods as tiles, its building groups and trees as cards, the mods' own, and what was opened last. -->
<script lang="ts">
import type { Node } from "@services/atlas/models";

/** A Recent is a subject opened before, with what the start page shows of it. */
export type Recent = Pick<Node, "id" | "name" | "title" | "what" | "type" | "display" | "picture" | "silhouette">;
</script>

<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { Type } from "@internal/lang/models";
import type { CatalogSection } from "@services/atlas/models";
import { describeCount, toSentence } from "../../lib/format";
import { catalogQuery, catalogsQuery, EVERY_ROW, ownRootsQuery } from "../../queries";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import NodePicture, { labelOf } from "./NodePicture.vue";
import PaintingBand from "./PaintingBand.vue";

defineProps<{ recent: Recent[] }>();
const emit = defineEmits<{ select: [id: string] }>();

/** The members a shelf's card shows as picture buttons. */
const CARD_ICONS = 4;
const ROW_CLASS = "flex h-7.5 w-full items-center gap-2.5 rounded-md px-2 text-start text-12 hover:bg-elevated";

const workspace = useWorkspaceStore();
const { data: catalogs } = useQuery(() => ({ ...catalogsQuery, enabled: workspace.isOpen() }));
const { data: own } = useQuery(() => ({ ...ownRootsQuery, enabled: workspace.isOpen() }));
const goods = useCatalog(Type.TypeGood);
const buildings = useCatalog(Type.TypeBuilding);
const technologies = useCatalog(Type.TypeTechnology);
/** A shelf for the buildings and one for the technologies, each a card for every widest group. */
const shelves = computed(() =>
  [buildings, technologies].flatMap(({ catalog, sections }) =>
    catalog.value && sections.value?.some((section) => section.group.id)
      ? { catalog: catalog.value, cards: sections.value.filter((section) => section.group.id) }
      : [],
  ),
);

/** useCatalog returns the catalog of a type and its every row under its groups. */
function useCatalog(type: Type) {
  const catalog = computed(() => catalogs.value?.find((listed) => listed.types?.includes(type)));
  const { data: sections } = useQuery(() => ({
    ...catalogQuery({ types: catalog.value?.types ?? [], query: "", limit: EVERY_ROW }),
    enabled: workspace.isOpen() && !!catalog.value,
  }));
  return { catalog, sections };
}

/** listRows returns every row of a section, those of the sections inside it first. */
function listRows(section: CatalogSection): Node[] {
  return [...(section.sections ?? []).flatMap(listRows), ...(section.rows ?? [])];
}
</script>

<template>
  <div class="@container h-full min-h-0 overflow-auto bg-(--bg-0)">
    <div class="flex flex-col gap-6 px-8 py-6">
      <header class="flex items-end gap-4">
        <div class="flex flex-col">
          <h1 class="text-20 font-semibold text-highlighted">{{ workspace.active()?.game?.name }}</h1>
          <p class="text-13 text-muted">
            Pick a good, building, technology or event on the left, or start from one of these.
          </p>
        </div>
        <span class="ms-auto flex items-center gap-1.5 text-12 text-dimmed">
          In the editor <UKbd value="Ctrl" /> <UKbd value="Shift" /> <UKbd value="G" /> opens the name under the cursor
          here
        </span>
      </header>
      <div class="grid items-start gap-8 @4xl:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        <section v-if="goods.catalog.value && goods.sections.value?.length" class="flex flex-col gap-3">
          <h2 class="flex items-baseline gap-2.5 text-16 font-semibold text-highlighted">
            {{ toSentence(goods.catalog.value.label) }}
            <small class="text-12 font-normal text-dimmed">{{ describeCount(goods.catalog.value.count) }}</small>
          </h2>
          <div v-for="group in goods.sections.value" :key="group.group.id" class="flex flex-col gap-1.5">
            <div v-if="group.group.id" class="flex h-6 items-center gap-2 text-12 font-semibold text-toned">
              {{ labelOf(group.group) }}
              <span class="font-normal text-dimmed">{{ describeCount(group.group.count) }}</span>
              <span class="h-px grow bg-(--line-1)" />
            </div>
            <div class="grid grid-cols-[repeat(auto-fill,minmax(5.75rem,1fr))] gap-1">
              <button
                v-for="row in listRows(group)"
                :key="row.id"
                type="button"
                :title="labelOf(row)"
                class="relative flex h-21 flex-col items-center gap-1.5 overflow-hidden rounded-lg bg-muted px-1 pt-1.5 text-center text-12 leading-3.5 text-highlighted hover:bg-elevated"
                @click="emit('select', row.id)"
              >
                <NodePicture :node="row" class="size-12" />
                <span class="line-clamp-2 wrap-anywhere" :class="!row.title && 'font-mono'">{{ labelOf(row) }}</span>
                <OriginChip v-if="row.origin !== VANILLA" :origin="row.origin" dot class="absolute end-1.5 top-1.5" />
              </button>
            </div>
          </div>
        </section>
        <div class="flex min-w-0 flex-col gap-6">
          <section v-for="shelf in shelves" :key="shelf.catalog.label" class="flex flex-col gap-3">
            <h2 class="flex items-baseline gap-2.5 text-16 font-semibold text-highlighted">
              {{ toSentence(shelf.catalog.label) }}
              <small class="text-12 font-normal text-dimmed">{{ describeCount(shelf.catalog.count) }}</small>
            </h2>
            <div class="grid grid-cols-[repeat(auto-fill,minmax(14rem,1fr))] gap-2">
              <PaintingBand
                v-for="card in shelf.cards"
                :key="card.group.id"
                :painting="card.group.painting"
                class="flex h-22.5 flex-col justify-between rounded-xl border border-accented bg-muted p-3"
              >
                <span class="flex items-baseline gap-2">
                  <b class="truncate text-14 font-semibold text-highlighted" :title="labelOf(card.group)">{{
                    labelOf(card.group)
                  }}</b>
                  <span class="ms-auto text-12 text-muted tabular-nums">{{ describeCount(card.group.count) }}</span>
                </span>
                <span class="flex items-center gap-1">
                  <button
                    v-for="row in listRows(card).slice(0, CARD_ICONS)"
                    :key="row.id"
                    type="button"
                    :title="labelOf(row)"
                    :aria-label="labelOf(row)"
                    class="relative flex size-7 items-center justify-center rounded-md border border-default bg-default/70 hover:bg-elevated"
                    @click="emit('select', row.id)"
                  >
                    <NodePicture :node="row" class="size-5.5" />
                    <OriginChip
                      v-if="row.origin !== VANILLA"
                      :origin="row.origin"
                      dot
                      class="absolute -end-0.5 -top-0.5"
                    />
                  </button>
                  <span v-if="card.group.count > CARD_ICONS" class="ms-1 text-12 text-muted tabular-nums">
                    +{{ describeCount(card.group.count - CARD_ICONS) }}
                  </span>
                </span>
              </PaintingBand>
            </div>
          </section>
          <div v-if="own?.length || recent.length" class="grid items-start gap-6 @2xl:grid-cols-2">
            <section v-if="own?.length" class="flex min-w-0 flex-col gap-1">
              <h2 class="px-2 pb-1.5 text-16 font-semibold text-highlighted">From your mods</h2>
              <div class="flex max-h-72 flex-col gap-1 overflow-y-auto">
                <button
                  v-for="row in own"
                  :key="row.id"
                  type="button"
                  :class="ROW_CLASS"
                  @click="emit('select', row.id)"
                >
                  <OriginChip :origin="row.origin" dot />
                  <span class="max-w-1/2 shrink-0 truncate font-mono text-12 text-highlighted">{{ row.name }}</span>
                  <span class="min-w-0 grow truncate text-12 text-dimmed">{{ row.title || row.what }}</span>
                  <UBadge
                    :label="row.copyOrigin ? 'replaces' : 'added'"
                    :color="row.copyOrigin ? 'warning' : 'success'"
                    variant="subtle"
                    size="sm"
                  />
                </button>
              </div>
            </section>
            <section v-if="recent.length" class="flex min-w-0 flex-col gap-1">
              <h2 class="px-2 pb-1.5 text-16 font-semibold text-highlighted">Recently opened</h2>
              <button
                v-for="row in recent"
                :key="row.id"
                type="button"
                :class="ROW_CLASS"
                @click="emit('select', row.id)"
              >
                <NodePicture :node="row" class="size-5.5" />
                <span class="min-w-0 grow truncate text-12 text-highlighted">{{ labelOf(row) }}</span>
                <span class="shrink-0 text-12 text-dimmed">{{ row.what }}</span>
              </button>
            </section>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
