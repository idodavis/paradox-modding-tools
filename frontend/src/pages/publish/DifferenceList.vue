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
    <div class="flex items-center gap-1.5 px-3 pt-2.5 pb-1 text-11 font-semibold text-dimmed uppercase">
      {{ group.label }}
      <span class="font-normal tabular-nums">{{ group.differences.length }}</span>
    </div>
    <button
      v-for="difference in group.differences"
      :key="toDifferenceId(difference)"
      type="button"
      class="flex h-7 w-full items-center gap-2 ps-3 pe-2.5 text-start text-12 hover:bg-accented"
      :class="{ 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]': toDifferenceId(difference) === lit }"
      :aria-current="toDifferenceId(difference) === lit"
      @click="emit('pick', toDifferenceId(difference))"
    >
      <b
        class="w-3.5 shrink-0 text-center font-mono text-11 font-semibold"
        :class="MARKS[difference.status]?.color"
        :title="MARKS[difference.status]?.count"
      >
        {{ MARKS[difference.status]?.letter }}
      </b>
      <span
        class="grow truncate text-highlighted"
        :class="{
          'font-mono text-11.5': difference.field === 'file',
          'font-medium': toDifferenceId(difference) === lit,
        }"
        :title="describeDifferenceLabel(difference)"
      >
        {{ describeDifferenceLabel(difference) }}
      </span>
      <span
        class="flex max-w-28 shrink-0 gap-1.5 overflow-hidden text-11 whitespace-nowrap text-dimmed tabular-nums"
        :class="{ 'font-mono': difference.field === 'version' }"
        :title="describeDifference(difference)"
      >
        <template v-if="difference.added?.length || difference.removed?.length">
          <span v-for="entry in difference.added" :key="`+${entry}`" class="text-success">+{{ entry }}</span>
          <span v-for="entry in difference.removed" :key="`−${entry}`" class="text-error">−{{ entry }}</span>
        </template>
        <template v-else-if="difference.gained || difference.lost">
          <span v-if="difference.gained" class="text-success">+{{ difference.gained }}</span>
          <span v-if="difference.lost" class="text-error">−{{ difference.lost }}</span>
        </template>
        <template v-else>{{ describeDifference(difference) }}</template>
      </span>
    </button>
  </template>
</template>
