<!--
  A technology as a card: its picture and name, then what it unlocks or gives,
  in the primary color for the one asked about, the second for its chain and
  with a purple edge for an audience. Its parent gives it its place and size.
-->
<script setup lang="ts">
import { computed } from "vue";
import type { Technology } from "@services/graph/models";
import { describeCount, list } from "../../lib/format";
import { labelOf } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import SelectedTab from "./SelectedTab.vue";
import type { Mark, Shape } from "./techlayout";

const {
  technology,
  shape,
  mark = "",
  audience = "",
} = defineProps<{
  technology: Technology;
  /**
   * What the card shows: the icons of what it unlocks in a tree packed by
   * era, a line of words in a tree of an age, and its picture large beside
   * the words for "art".
   */
  shape: Shape | "art";
  mark?: Mark;
  /** The title of the audience the card is drawn for, "" for every country. */
  audience?: string;
}>();
const emit = defineEmits<{ ask: [id: string] }>();

/** UNLOCK_ICONS is how many of the unlocked definitions a card shows. */
const UNLOCK_ICONS = 4;
/** NAMED is how many unlocks or facts the card's line of words names. */
const NAMED = 2;

const unlocks = computed(() => technology.unlocks ?? []);

/**
 * summary is the card's line of words: what the technology unlocks, else what
 * it gives, the first few named and the rest counted.
 */
const summary = computed(() => {
  const facts = (technology.gives ?? []).map((fact) => `${fact.label} ${fact.text}`);
  const named = unlocks.value.length ? unlocks.value.map(labelOf) : facts;
  const more = named.length - NAMED;
  const kept = more > 0 ? [...named.slice(0, NAMED), `${describeCount(more)} more`] : named;

  return `${unlocks.value.length ? "Unlocks " : ""}${list(kept)}`;
});
</script>

<template>
  <button
    type="button"
    :data-mark="mark || undefined"
    :data-audience="audience || undefined"
    :title="audience && `For ${audience}`"
    class="flex cursor-pointer rounded-lg border border-accented bg-muted text-start data-audience:border-s-3 data-audience:border-s-(--purple) data-[mark=chain]:border-secondary data-[mark=subject]:border-primary data-[mark=subject]:bg-(--accent-soft) data-[mark=subject]:ring-1 data-[mark=subject]:ring-primary"
    :class="shape === 'art' ? 'items-center gap-2.5 p-0.5 pe-2.5' : 'flex-col justify-between px-3 py-2.5'"
    @click="emit('ask', technology.node.id)"
  >
    <SelectedTab v-if="mark === 'subject'" :inside="shape === 'art'" />
    <template v-if="shape === 'art'">
      <NodeIcon :node="technology.node" class="h-full w-30 rounded-[5px] object-cover!" />
      <span class="flex min-w-0 flex-col gap-0.5">
        <b class="truncate text-13 leading-4 font-medium text-highlighted">{{ labelOf(technology.node) }}</b>
        <small class="line-clamp-3 text-11 leading-3.5 text-muted">{{ summary }}</small>
      </span>
    </template>
    <template v-else>
      <span class="flex items-center gap-3">
        <NodeIcon :node="technology.node" class="size-10" />
        <b class="line-clamp-2 text-14 leading-4.5 font-medium text-highlighted">{{ labelOf(technology.node) }}</b>
      </span>
      <span v-if="shape === 'trees'" class="truncate text-12 text-muted">{{ summary }}</span>
      <span v-else-if="unlocks.length" class="flex items-center gap-1.5">
        <NodeIcon
          v-for="unlocked in unlocks.slice(0, UNLOCK_ICONS)"
          :key="unlocked.id"
          :node="unlocked"
          class="size-6"
        />
        <em v-if="unlocks.length > UNLOCK_ICONS" class="ms-0.5 text-12 text-muted not-italic tabular-nums">
          +{{ describeCount(unlocks.length - UNLOCK_ICONS) }}
        </em>
      </span>
      <span v-else class="text-12 text-dimmed">Unlocks nothing</span>
    </template>
  </button>
</template>
