<!--
  The Graph's page with no subject: the game's goods, building groups and
  technologies as art to start from, the mods' own definitions and the
  subjects opened last.
-->
<script setup lang="ts">
import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import { Type } from "@internal/lang/models";
import type { Catalog, Node } from "@services/graph/models";
import { describeCount, toSentence } from "../../lib/format";
import { graphCatalogQuery, graphCatalogsQuery, ownRootsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import { arrangeCatalog, listRows } from "./catalog";
import NodeIcon from "./NodeIcon.vue";
import PaintingBand from "./PaintingBand.vue";
import { labelOf, whatOf, type Recent } from "./nodes";

const { recent } = defineProps<{ recent: Recent[] }>();
const emit = defineEmits<{ select: [id: string] }>();

/** Card is a group, or a whole catalog, with its first rows. */
type Card = { id: string; title: string; painting?: string; count: number; rows: Node[] };

/** CARD_ICONS is how many rows a card shows the icon of. */
const CARD_ICONS = 4;
const ROW_CLASS = "flex h-7.5 w-full items-center gap-2.5 rounded-md px-2 text-start text-12 hover:bg-elevated";

const workspace = useWorkspaceStore();
const { data: catalogs } = useQuery(() => ({ ...graphCatalogsQuery, enabled: workspace.isOpen() }));
const { data: own } = useQuery(() => ({ ...ownRootsQuery, enabled: workspace.isOpen() }));
const goods = computed(() => findCatalog(Type.TypeGood));
const buildings = computed(() => findCatalog(Type.TypeBuilding));
const technologies = computed(() => findCatalog(Type.TypeTechnology));
const goodRows = useRows(goods);
const buildingRows = useRows(buildings);
const technologyRows = useRows(technologies);

const goodGroups = computed(() => {
  const { sections, loose } = arrangeCatalog(goods.value?.groups ?? [], goodRows.value ?? []);
  return [
    ...sections.map((section) => ({ title: labelOf(section.group), rows: listRows(section) })),
    ...(loose.length ? [{ title: "", rows: loose }] : []),
  ];
});
const buildingCards = computed(() => makeCards(buildings.value, buildingRows.value));
const technologyCards = computed(() => makeCards(technologies.value, technologyRows.value));
const shelves = computed(() =>
  [
    { catalog: buildings.value, cards: buildingCards.value },
    { catalog: technologies.value, cards: technologyCards.value },
  ].flatMap(({ catalog, cards }) => (catalog && cards.length ? [{ catalog, cards }] : [])),
);

/** makeCards returns a card for each of the widest groups of a catalog. */
function makeCards(catalog: Catalog | undefined, rows: Node[] | null | undefined): Card[] {
  return arrangeCatalog(catalog?.groups ?? [], rows ?? []).sections.map((section) => ({
    id: section.group.id,
    title: labelOf(section.group),
    painting: section.group.painting,
    count: section.group.count,
    rows: listRows(section).slice(0, CARD_ICONS),
  }));
}

function findCatalog(type: Type): Catalog | undefined {
  return catalogs.value?.find((catalog) => catalog.types?.[0] === type);
}

/** useRows lists every row of a catalog. */
function useRows(catalog: MaybeRefOrGetter<Catalog | undefined>) {
  const { data } = useQuery(() => ({
    ...graphCatalogQuery({
      types: toValue(catalog)?.types ?? [],
      query: "",
      limit: toValue(catalog)?.count ?? 0,
    }),
    enabled: workspace.isOpen() && !!toValue(catalog),
  }));
  return data;
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
        <section v-if="goods && goodGroups.length" class="flex flex-col gap-3">
          <h2 class="flex items-baseline gap-2.5 text-16 font-semibold text-highlighted">
            {{ toSentence(goods.label) }}
            <small class="text-12 font-normal text-dimmed">{{ describeCount(goods.count) }}</small>
          </h2>
          <div v-for="group in goodGroups" :key="group.title" class="flex flex-col gap-1.5">
            <div v-if="group.title" class="flex h-6 items-center gap-2 text-12 font-semibold text-toned">
              {{ group.title }}
              <span class="font-normal text-dimmed">{{ describeCount(group.rows.length) }}</span>
              <span class="h-px grow bg-(--line-1)" />
            </div>
            <div class="grid grid-cols-[repeat(auto-fill,minmax(5.75rem,1fr))] gap-1">
              <button
                v-for="row in group.rows"
                :key="row.id"
                type="button"
                class="flex h-21 flex-col items-center gap-1.5 rounded-lg bg-muted px-1 pt-1.5 text-center text-12 leading-3.5 text-highlighted hover:bg-elevated"
                @click="emit('select', row.id)"
              >
                <NodeIcon :node="row" class="size-12" />
                <span>{{ labelOf(row) }}</span>
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
                :key="card.id"
                :painting="card.painting"
                class="flex h-22.5 flex-col justify-between rounded-xl border border-accented bg-muted p-3"
              >
                <span class="flex items-baseline gap-2">
                  <b class="truncate text-14 font-semibold text-highlighted">{{ card.title }}</b>
                  <span class="ms-auto text-12 text-muted tabular-nums">{{ describeCount(card.count) }}</span>
                </span>
                <span class="flex items-center gap-1">
                  <button
                    v-for="row in card.rows"
                    :key="row.id"
                    type="button"
                    :title="labelOf(row)"
                    :aria-label="labelOf(row)"
                    class="flex size-7 items-center justify-center rounded-md border border-default bg-default/70 hover:bg-elevated"
                    @click="emit('select', row.id)"
                  >
                    <NodeIcon :node="row" class="size-5.5" />
                  </button>
                  <span v-if="card.count > CARD_ICONS" class="ms-1 text-12 text-muted tabular-nums">
                    +{{ describeCount(card.count - CARD_ICONS) }}
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
                  <OriginChip :origin="row.site.place.origin" dot />
                  <span class="font-mono text-12 text-highlighted">{{ row.name }}</span>
                  <span class="min-w-0 grow truncate text-12 text-dimmed">{{ row.title || whatOf(row) }}</span>
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
                <NodeIcon :node="row" class="size-5.5" />
                <span class="min-w-0 grow truncate text-12 text-highlighted">{{ row.title || row.name }}</span>
                <span class="text-12 text-dimmed">{{ whatOf(row) }}</span>
              </button>
            </section>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
