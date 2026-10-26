<!-- The marks of a set of problems, one for each level with its count. With none, it says "No problems". -->
<script setup lang="ts">
import { computed } from "vue";

import { describeCount, LEVELS, PROBLEM_LEVELS } from "../lib/format";

const { levels } = defineProps<{
  /** How many problems there are of each level, by Go's level. */
  levels: Record<string, number>;
}>();

const marks = computed(() =>
  PROBLEM_LEVELS.flatMap((level) => {
    const row = LEVELS[level];
    const count = levels[level] ?? 0;
    return row && count ? [{ level, ...row, count: describeCount(count) }] : [];
  }),
);
</script>

<template>
  <span class="flex shrink-0 items-center gap-2 text-11 tabular-nums">
    <span
      v-for="mark in marks"
      :key="mark.level"
      :title="`${mark.label}: ${mark.count}`"
      class="flex items-center gap-0.5"
      :style="{ color: `var(--ui-${mark.color})` }"
    >
      <UIcon :name="mark.icon" class="size-3" />
      {{ mark.count }}
    </span>
    <span v-if="!marks.length" class="flex items-center gap-0.5" style="color: var(--ui-success)">
      <UIcon name="i-lucide-circle-check" class="size-3" />
      No problems
    </span>
  </span>
</template>
