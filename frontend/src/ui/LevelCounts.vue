<!-- The marks of a set of problems, one for each level with its count. With none, it says "No problems". -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import type { Tally } from "@services/health/models";

import { describeCount } from "../lib/format";
import { levelsQuery } from "../queries";

const { levels = undefined } = defineProps<{
  /** How many problems there are of each level id. Without counts it shows "No problems". */
  levels?: Tally["counts"];
}>();

const { data: rows } = useQuery(levelsQuery);
const marks = computed(() =>
  (rows.value ?? []).flatMap((row) => {
    const count = levels?.[row.id] ?? 0;
    return row.problem && count ? [{ ...row, count: describeCount(count) }] : [];
  }),
);
</script>

<template>
  <span class="flex shrink-0 items-center gap-2 text-11 tabular-nums">
    <span
      v-for="mark in marks"
      :key="mark.id"
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
