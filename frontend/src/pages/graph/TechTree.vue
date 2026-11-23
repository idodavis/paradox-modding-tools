<!--
  A technology's tree under its bar, on a canvas that zooms and pans: the cards
  of each era, or of each tree in an age, with the wires of the chain of the
  technology asked about lit and the others faint or hidden. It can draw that
  chain alone.
-->
<script lang="ts">
/**
 * TechChange is a change to what the route says of a technology's tree. An
 * empty text puts a part back to what it is without.
 */
export type TechChange = { view?: string; direction?: string; wires?: string; potential?: string };
</script>

<script setup lang="ts">
import { computedAsync } from "@vueuse/core";
import { clamp } from "es-toolkit";
import { computed, onMounted, useTemplateRef, watch } from "vue";
import type { SelectMenuItem } from "@nuxt/ui";
import type { Audience, TreeGraph } from "@services/graph/models";
import { describeCount } from "../../lib/format";
import PictureViewport, { MARGIN } from "../../ui/PictureViewport.vue";
import { GROUND, labelOf } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import PaintingBand from "./PaintingBand.vue";
import TechCard from "./TechCard.vue";
import { chainOf, EMPTY, layoutChain, layoutTree, markOf, toStyle, type Wire } from "./techlayout";

const {
  tree,
  view = "",
  direction = "",
  wires = "",
  potential = "",
} = defineProps<{
  tree: TreeGraph;
  /** The route's view: "chain" for the chain alone, else the whole tree. */
  view?: string;
  /** The route's direction: "down" for top to bottom, else left to right. */
  direction?: string;
  /** The route's wires: "hidden" leaves out those outside the chain. */
  wires?: string;
  /** The id of the audience the route adds to the drawing, "" for none. */
  potential?: string;
}>();
const emit = defineEmits<{ ask: [id: string]; change: [change: TechChange] }>();

const WHOLE = "tree";
const VIEWS = [
  { label: "Whole tree", value: WHOLE },
  { label: "Only its chain", value: "chain" },
];
/** DIRECTIONS are the ways a tree reads, as the route names them. */
const DIRECTIONS = [
  { value: "", label: "Read left to right", icon: "i-lucide-arrow-right" },
  { value: "down", label: "Read top to bottom", icon: "i-lucide-arrow-down" },
];
/**
 * LOOKS is how a wire is drawn: the subject's in the primary color, the rest
 * of its chain in the second, any other faint. A higher rank draws on top.
 */
const LOOKS = {
  other: { color: "text-(--fg-3) opacity-45", width: 1.25, rank: 0 },
  chain: { color: "text-secondary", width: 2, rank: 1 },
  subject: { color: "text-primary", width: 3, rank: 2 },
};

const viewport = useTemplateRef("viewport");

const isChainOnly = computed(() => view === "chain");
const isHidingWires = computed(() => wires === "hidden");
const current = computed(() => tree.trees?.find((candidate) => candidate.id === tree.tree));
const subject = computed(() => tree.technologies?.find((technology) => technology.node.id === tree.subject)?.node);
const treeItems = computed(() =>
  (tree.trees ?? []).map((candidate) => ({
    label: candidate.title,
    value: candidate.id,
    badge: describeCount(candidate.count),
  })),
);
const audienceItems = computed(() =>
  (tree.audiences ?? []).map((row): SelectMenuItem =>
    isGroup(row) ? { type: "label", label: row.title } : { label: row.title, value: row.id, count: row.count },
  ),
);
const audienceTitles = computed(() => new Map((tree.audiences ?? []).map((row) => [row.id, row.title])));
const picked = computed({
  get: () => potential || undefined,
  set: (id?: string | null) => emit("change", { potential: id ?? "" }),
});

const chain = computed(() => chainOf(tree));
const layout = computedAsync(() => {
  const reading = direction === "down" ? "down" : "across";

  return isChainOnly.value ? layoutChain(tree, chain.value, reading) : layoutTree(tree, reading);
}, EMPTY);
const cards = computed(() =>
  (tree.technologies ?? []).flatMap((technology) => {
    const box = layout.value.cards[technology.node.id];

    return box ? [{ technology, box }] : [];
  }),
);
const shownWires = computed(() =>
  layout.value.wires
    .map((wire) => ({ wire, look: describeWire(wire) }))
    .filter(({ look }) => !isHidingWires.value || look !== "other")
    .sort((first, second) => LOOKS[first.look].rank - LOOKS[second.look].rank),
);
const hasAudience = computed(() => cards.value.some(({ technology }) => technology.audience));
const subjectBox = computed(() => layout.value.cards[tree.subject]);

watch(subjectBox, focus, { flush: "post" });
onMounted(focus);

/** isGroup reports whether a row heads others, as Religions heads them. */
function isGroup(row: Audience): boolean {
  return (tree.audiences ?? []).some((other) => other.group === row.id && other.id !== row.id);
}

function pickView(id: string | number): void {
  emit("change", { view: id === WHOLE ? "" : String(id) });
}

/** pickTree asks for the technology the chosen tree opens on. */
function pickTree(id: string | number): void {
  const chosen = tree.trees?.find((candidate) => candidate.id === id);
  if (chosen) emit("ask", chosen.first);
}

/** describeWire returns a wire's look by what it joins. */
function describeWire({ requirement, dependent }: Wire): keyof typeof LOOKS {
  if (requirement === tree.subject || dependent === tree.subject) return "subject";

  return chain.value.has(requirement) && chain.value.has(dependent) ? "chain" : "other";
}

/**
 * focus shows as much of the tree as the pane holds at full size, the
 * subject's card as near the middle as the tree's edges allow.
 */
function focus(): void {
  const box = subjectBox.value;
  const pane = viewport.value?.pane;
  if (!box || !pane) return;

  const width = Math.min(layout.value.width, pane.clientWidth - MARGIN);
  const height = Math.min(layout.value.height, pane.clientHeight - MARGIN);
  const x = clamp(box.x + (box.width - width) / 2, 0, layout.value.width - width);
  const y = clamp(box.y + (box.height - height) / 2, 0, layout.value.height - height);
  viewport.value?.fit(new DOMRect(x, y, width, height));
}
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-col">
    <div class="flex h-12 shrink-0 items-center gap-3 border-b border-default bg-muted px-3">
      <b class="text-14 text-highlighted">{{ current?.title }}</b>
      <UTabs
        :model-value="view || WHOLE"
        :items="VIEWS"
        :content="false"
        size="xs"
        variant="pill"
        color="neutral"
        @update:model-value="pickView"
      />
      <span v-if="subject" class="flex items-center gap-2 text-12 text-muted">
        <NodeIcon :node="subject" class="size-6" />
        <b class="text-13 font-medium text-highlighted">{{ labelOf(subject) }}</b>
        needs {{ describeCount(tree.needs?.length ?? 0) }}, leads to {{ describeCount(tree.sharedLeads) }}
      </span>
      <span class="grow" />
      <label v-if="audienceItems.length" class="flex items-center gap-2 text-12 text-dimmed">
        Also show advances of
        <USelectMenu
          v-model="picked"
          :items="audienceItems"
          value-key="value"
          clear
          placeholder="Pick a country, religion or culture"
          size="xs"
          class="w-64"
        >
          <template #item-trailing="{ item }">
            <span v-if="typeof item === 'object' && item && 'count' in item" class="text-12 text-dimmed tabular-nums">
              {{ describeCount(item.count) }}
            </span>
          </template>
        </USelectMenu>
      </label>
      <UTooltip
        v-if="!isChainOnly"
        :text="isHidingWires ? 'Show the wires outside its chain' : 'Hide the wires outside its chain'"
      >
        <UButton
          :icon="isHidingWires ? 'i-lucide-eye-off' : 'i-lucide-eye'"
          color="neutral"
          variant="ghost"
          size="sm"
          square
          aria-label="The wires outside its chain"
          @click="emit('change', { wires: isHidingWires ? '' : 'hidden' })"
        />
      </UTooltip>
      <UFieldGroup size="sm">
        <UTooltip v-for="option in DIRECTIONS" :key="option.value" :text="option.label">
          <UButton
            :icon="option.icon"
            color="neutral"
            :variant="direction === option.value ? 'soft' : 'ghost'"
            square
            :aria-label="option.label"
            @click="emit('change', { direction: option.value })"
          />
        </UTooltip>
      </UFieldGroup>
    </div>
    <PaintingBand
      :painting="current?.painting"
      class="flex h-18 shrink-0 items-center gap-3 border-b border-default px-3"
    >
      <UTabs
        :model-value="tree.tree"
        :items="treeItems"
        :content="false"
        variant="pill"
        color="neutral"
        class="min-w-0"
        :ui="{ list: 'overflow-x-auto' }"
        @update:model-value="pickTree"
      />
      <span class="grow" />
      <span v-if="current && cards.length < current.count && !isChainOnly" class="text-12 whitespace-nowrap text-muted">
        {{ describeCount(cards.length) }} of {{ describeCount(current.count) }} drawn
      </span>
    </PaintingBand>
    <PictureViewport ref="viewport" class="min-h-0 flex-1" :ground="GROUND">
      <div class="relative" :style="{ width: `${layout.width}px`, height: `${layout.height}px` }">
        <div
          v-for="block in layout.blocks"
          :key="block.id"
          class="absolute border-s border-t border-default"
          :style="toStyle(block)"
        >
          <h3 v-if="block.title" class="flex items-baseline gap-2 px-3.5 pt-2.5 text-13 font-semibold text-highlighted">
            {{ block.title }}
            <small class="font-normal text-dimmed tabular-nums">{{ describeCount(block.count) }}</small>
          </h3>
        </div>
        <svg class="pointer-events-none absolute inset-0 size-full overflow-visible" aria-hidden="true">
          <g
            v-for="{ wire, look } in shownWires"
            :key="`${wire.requirement}>${wire.dependent}`"
            :class="LOOKS[look].color"
          >
            <path
              :d="wire.path"
              class="fill-none stroke-current [stroke-linecap:round]"
              :stroke-width="LOOKS[look].width"
            />
            <path :d="wire.head" class="fill-current stroke-none" />
          </g>
        </svg>
        <TechCard
          v-for="{ technology, box } in cards"
          :key="technology.node.id"
          class="absolute"
          :style="toStyle(box)"
          :technology="technology"
          :shape="layout.shape"
          :mark="markOf(tree, chain, technology.node.id)"
          :audience="audienceTitles.get(technology.audience ?? '')"
          @ask="emit('ask', $event)"
        />
      </div>
      <template #toolbar>
        <span class="flex items-center gap-4">
          <span class="flex items-center gap-1.5">
            <i class="h-3 w-5 rounded-xs border border-primary bg-(--accent-soft)" />Selected
          </span>
          <span class="flex items-center gap-1.5">
            <i class="h-3 w-5 rounded-xs border border-secondary bg-muted" />Its chain
          </span>
          <span v-if="hasAudience" class="flex items-center gap-1.5">
            <i class="h-3 w-5 rounded-xs border border-s-3 border-accented border-s-(--purple) bg-muted" />For an
            audience
          </span>
        </span>
      </template>
    </PictureViewport>
  </div>
</template>
