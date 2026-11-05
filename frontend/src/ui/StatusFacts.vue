<!-- A page's facts, shown in the status bar while the page is open. -->
<script lang="ts">
/** STATUS_ITEM holds the classes of an item of the status bar that does something when clicked. */
export const STATUS_ITEM =
  "-mx-1 flex h-6 items-center gap-1.25 rounded-sm px-1 hover:bg-accented hover:text-highlighted";
</script>

<script setup lang="ts">
import { useShown } from "../app/pages";

defineProps<{ facts: { label: string; value: string }[] }>();

// a kept-alive page stays mounted behind the others, so its facts leave with it
const visible = useShown();
</script>

<template>
  <Teleport v-if="visible" defer to="#status-start">
    <span v-for="fact in facts" :key="fact.label">
      {{ fact.label }} <span class="text-toned">{{ fact.value }}</span>
    </span>
  </Teleport>
</template>
