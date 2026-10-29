<!-- A sentence about a finding in another view, after that finding's level mark. It opens that finding. -->
<script setup lang="ts">
import { computed } from "vue";

import { LEVELS, type Part } from "../../lib/format";
import FactText from "../../ui/FactText.vue";

const { level, parts } = defineProps<{
  /** The other finding's level. */
  level: string;
  parts: readonly Part[];
}>();
const emit = defineEmits<{ open: [] }>();

const mark = computed(() => LEVELS[level]);
</script>

<template>
  <button
    type="button"
    class="flex items-start gap-2.5 text-start text-13 leading-5.25 text-toned hover:text-highlighted"
    @click="emit('open')"
  >
    <UIcon
      :name="mark?.icon ?? 'i-lucide-info'"
      class="mt-0.75 size-3.5 shrink-0"
      :style="{ color: `var(--ui-${mark?.color})` }"
    />
    <span class="grow"><FactText :parts="parts" /></span>
    <UIcon name="i-lucide-chevron-right" class="mt-0.75 size-3.5 shrink-0 text-dimmed" />
  </button>
</template>
