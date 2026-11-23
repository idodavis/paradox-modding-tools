<!--
  What unlocks a level or a production method: the technology with its era, or
  that it is there from the start. A double click on the technology asks for it.
-->
<script setup lang="ts">
import type { Node } from "@services/graph/models";
import { labelOf } from "./nodes";
import NodeIcon from "./NodeIcon.vue";

const {
  unlock = null,
  era = null,
  label = "",
} = defineProps<{
  unlock?: Node | null;
  era?: Node | null;
  /** The name of what it unlocks: an unlock called the same shows its era. */
  label?: string;
}>();
const emit = defineEmits<{ ask: [id: string] }>();
</script>

<template>
  <span
    v-if="unlock"
    class="inline-flex flex-wrap items-center gap-1.5 text-12 text-muted"
    @click.stop
    @dblclick.stop="emit('ask', unlock.id)"
  >
    <NodeIcon :node="unlock" class="size-4.5" />
    <span v-if="labelOf(unlock) !== label" class="font-medium text-highlighted">{{ labelOf(unlock) }}</span>
    <UBadge v-if="era" :label="labelOf(era)" color="neutral" variant="subtle" size="sm" />
  </span>
  <span v-else class="text-12 text-dimmed">Available from the start</span>
</template>
