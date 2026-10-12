<script setup lang="ts">
/**
 * Health: the load order, dragged to preview another; six cards, three the order
 * changes and three fixed in the files, each a count; the chosen card's table, and the open row beside it.
 * A link opens a card and a row by name (`?card=&name=`).
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useSortable } from "@vueuse/integrations/useSortable";
import { CARDS, rowsOf, typeOf, useHealth, type Card, type Row } from "../../composables/useHealth";
import { countKinds } from "../../lib/format";
import { useLiveEnabled } from "../../composables/useLiveEnabled";
import { useWorkspaceStore } from "../../stores/workspace";
import HealthDetail from "./HealthDetail.vue";
import HealthTable from "./HealthTable.vue";
import OriginDot from "../../ui/OriginDot.vue";
import StatusFacts from "../../ui/StatusFacts.vue";

const { id, card: linkedCard = "", name = "" } = defineProps<{ id: string; card?: string; name?: string }>();

const router = useRouter();
const store = useWorkspaceStore();
const live = useLiveEnabled(() => id);
const { order, dirty, health, error, isPending, save, saving } = useHealth(() => id, live);

const GROUPS: { title: string; hint: string; cards: Card[] }[] = [
  { title: "Load order", hint: "Changes when you reorder mods", cards: ["contests", "overrides", "depends"] },
  { title: "Problems in the mods", hint: "Fixed in the files", cards: ["dangling", "loc", "script"] },
];
const isCard = (value: string): value is Card => !!value && value in CARDS;

// the card is the route's, so a link and the tiles pick it alike
const card = computed({
  get: (): Card => (isCard(linkedCard) ? linkedCard : "contests"),
  set: (next: Card) => void router.replace({ query: { card: next } }),
});
const rows = computed(() => rowsOf(health.value, card.value));
const open = shallowRef<Row | null>(null);
watch(card, () => (open.value = null));
// a linked name opens its row once the report holds it
watch([() => name, rows], () => {
  const linked =
    name && rows.value.find((row) => ("name" in row && row.name === name) || ("key" in row && row.key === name));
  if (linked) open.value = linked;
});

const counts = computed(() =>
  Object.fromEntries(GROUPS.flatMap((group) => group.cards).map((c) => [c, rowsOf(health.value, c).length])),
);
const total = computed(() => Object.values(counts.value).reduce((sum, count) => sum + count, 0));
/** A tile's note: the rule each contest goes by, how many of each type Localization Issues lists, else its own. */
function note(c: Card): string {
  if (c === "contests") {
    const fios = (health.value?.contests ?? []).filter((contest) => contest.firstWins).length;
    return `${counts.value.contests - fios} LIOS, ${fios} FIOS`;
  }
  if (c === "loc") {
    const types = countKinds(rowsOf(health.value, "loc").map((row) => ({ kind: typeOf(row).label })));
    return types.map(({ kind, count }) => `${count} ${kind.toLowerCase()}`).join(", ") || "Nothing to fix";
  }
  return CARDS[c].note ?? "";
}

/** A load-order chip's look, the game's and each mod's. */
const CHIP =
  "flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-default bg-muted ps-1.5 pe-2.5 text-12 font-medium text-muted whitespace-nowrap";
// the mods' chips, dragged to preview another load order; the game stays first
useSortable(useTemplateRef<HTMLElement>("chips"), order, { animation: 150, watchElement: true });
// the table's share of the split, in percent, either pane taking up to four fifths; the splitter keeps it
const PANES = [{ slot: "table", defaultSize: 65, minSize: 20, maxSize: 80 }, { slot: "detail" }];
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-4 overflow-hidden px-6 pt-4.5">
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" />

    <div class="flex items-center gap-2">
      <span class="me-1.5 shrink-0 text-12 font-medium whitespace-nowrap text-muted">Load order</span>
      <span :class="CHIP">
        <span class="w-3 text-center font-mono text-11 text-dimmed">0</span>
        <OriginDot :color="store.originColor(store.originVanilla)" small />
        {{ store.originName(store.originVanilla) }}
      </span>
      <ul ref="chips" class="flex min-w-0 items-center gap-2 overflow-x-auto">
        <li v-for="(mod, i) in order" :key="mod" class="flex shrink-0 cursor-grab items-center gap-2">
          <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
          <span :class="CHIP">
            <UIcon name="i-lucide-grip-vertical" class="size-3.5 text-dimmed" />
            <span class="w-3 text-center font-mono text-11 text-dimmed">{{ i + 1 }}</span>
            <OriginDot :color="store.originColor(mod)" small />
            {{ store.originName(mod) }}
          </span>
        </li>
      </ul>
      <span class="ms-2 shrink-0 text-12 whitespace-nowrap text-dimmed">Drag to preview another order.</span>
      <span class="grow" />
      <UButton label="Save as workspace order" class="shrink-0" :disabled="!dirty" :loading="saving" @click="save()" />
    </div>

    <div class="flex items-stretch gap-6">
      <template v-for="(group, g) in GROUPS" :key="group.title">
        <USeparator v-if="g" orientation="vertical" class="h-auto" />
        <div class="flex min-w-0 flex-1 flex-col gap-2">
          <div class="flex items-baseline gap-2.5 whitespace-nowrap">
            <span class="text-13 font-semibold">{{ group.title }}</span>
            <span class="text-12 text-dimmed">{{ group.hint }}</span>
          </div>
          <div class="grid grid-cols-3 gap-2.5">
            <button
              v-for="c in group.cards"
              :key="c"
              type="button"
              class="flex min-w-0 flex-col gap-1 rounded-lg border px-3.5 py-3 text-left"
              :class="card === c ? 'border-primary bg-elevated ring-2 ring-(--accent-soft)' : 'border-default bg-muted'"
              @click="card = c"
            >
              <span class="flex items-center gap-1.5 text-12 font-medium whitespace-nowrap text-muted">
                <OriginDot :color="CARDS[c].dot" :small="g === 0" />{{ CARDS[c].label }}
              </span>
              <span class="text-28 tabular-nums">{{ counts[c] }}</span>
              <span class="truncate text-11 leading-3.75 text-dimmed">{{ note(c) }}</span>
            </button>
          </div>
        </div>
      </template>
    </div>

    <USplitter
      :items="PANES"
      auto-save-id="pmt.health"
      class="min-h-0 grow overflow-hidden rounded-t-lg border border-b-0 border-default"
    >
      <template #table>
        <HealthTable
          :key="card"
          v-model="open"
          :card="card"
          :rows="rows"
          :sources="health?.sources ?? []"
          :loading="isPending"
          class="min-w-0 flex-1 border-e border-default"
        />
      </template>
      <template #detail>
        <HealthDetail :workspace-id="id" :row="open" :sources="health?.sources ?? []" :live="live" />
      </template>
    </USplitter>

    <StatusFacts>
      <span>{{ total }} findings across {{ Object.keys(counts).length }} checks</span>
      <span>{{ dirty ? "Preview order differs from the workspace" : "Preview order matches the workspace" }}</span>
    </StatusFacts>
  </div>
</template>
