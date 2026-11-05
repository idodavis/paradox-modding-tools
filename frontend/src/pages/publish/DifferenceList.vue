<!-- The differences the next upload makes on Steam, in groups of one row each. Pressing a row picks it. -->
<script setup lang="ts">
import { describeDifference, describeDifferenceLabel, MARKS, toDifferenceId } from "../../lib/format";
import type { Difference } from "@services/steam/models";

defineProps<{
  /** The groups to list, each under its label. */
  groups: { label: string; differences: Difference[] }[];
  /** The id of the lit difference, or "" for none. */
  lit: string;
}>();
const emit = defineEmits<{ pick: [id: string] }>();
</script>

<template>
  <template v-for="group in groups" :key="group.label">
    <div class="px-3 pt-2 pb-0.5 text-11 font-semibold text-dimmed">{{ group.label }}</div>
    <button
      v-for="difference in group.differences"
      :key="toDifferenceId(difference)"
      type="button"
      class="flex h-7 w-full items-center gap-2 ps-3 pe-2.5 text-start text-12 hover:bg-accented"
      :class="{ 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]': toDifferenceId(difference) === lit }"
      @click="emit('pick', toDifferenceId(difference))"
    >
      <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="MARKS[difference.status]?.color">
        {{ MARKS[difference.status]?.mark }}
      </b>
      <span
        class="grow truncate text-highlighted"
        :class="{ 'font-mono text-11.5': difference.field === 'file' }"
        :title="describeDifferenceLabel(difference)"
      >
        {{ describeDifferenceLabel(difference) }}
      </span>
      <span
        class="max-w-24 shrink-0 truncate text-11 text-dimmed"
        :class="{ 'font-mono': difference.field === 'version' }"
        :title="describeDifference(difference)"
      >
        {{ describeDifference(difference) }}
      </span>
    </button>
  </template>
</template>
