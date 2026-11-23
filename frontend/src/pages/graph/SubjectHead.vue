<!--
  The head of a subject's view, in the second color: its art, its name and its
  group over a painting, then facts in columns. The default slot holds what
  else it says, the aside slot a control at its end.
-->
<script setup lang="ts">
import { computed } from "vue";
import type { Fact, Node } from "@services/graph/models";
import { toSentence } from "../../lib/format";
import { labelOf } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import PaintingBand from "./PaintingBand.vue";

const {
  node,
  group = undefined,
  facts = [],
} = defineProps<{
  node: Node;
  /** The group the node is listed under, left out where it is in none. */
  group?: Node;
  /** The facts shown in columns at the end, as what it costs. */
  facts?: Pick<Fact, "label" | "text" | "when">[];
}>();

/** The group, where it is one and is not called the same as the node. */
const listed = computed(() => (group?.id && labelOf(group) !== labelOf(node) ? group : undefined));
</script>

<template>
  <PaintingBand
    :painting="node.painting || group?.painting"
    class="flex min-h-22 shrink-0 items-center gap-4 rounded-xl border border-secondary bg-(--link-soft) px-4 py-3 ring-1 ring-secondary"
  >
    <NodeIcon :node="node" class="size-14" />
    <span class="flex min-w-0 flex-col items-start gap-1">
      <h1 class="text-20 leading-7 text-highlighted">{{ labelOf(node) }}</h1>
      <span class="flex flex-wrap items-center gap-2 text-12 text-muted">
        <UBadge v-if="listed" :label="labelOf(listed)" color="neutral" variant="subtle" size="sm" />
        <slot />
      </span>
    </span>
    <span class="grow" />
    <span
      v-for="fact in facts"
      :key="`${fact.label}|${fact.text}`"
      class="flex flex-col gap-0.5 border-s border-default px-6 last:pe-2"
    >
      <small class="text-11.5 text-dimmed">{{ toSentence(fact.label) }}</small>
      <b class="text-14 font-semibold whitespace-nowrap text-highlighted tabular-nums">
        {{ fact.text }}
        <small v-if="fact.when" class="ms-1 text-11.5 font-normal text-dimmed">{{ fact.when }}</small>
      </b>
    </span>
    <slot name="aside" />
  </PaintingBand>
</template>
