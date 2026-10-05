<script setup lang="ts">
/**
 * A page's facts in the status bar, at its start or before Help at its end. They are there while the page
 * is shown: a kept-alive page's leave with it.
 */
import { onActivated, onDeactivated, shallowRef } from "vue";

const { end = false } = defineProps<{ end?: boolean }>();
const shown = shallowRef(true);
onActivated(() => (shown.value = true));
onDeactivated(() => (shown.value = false));
</script>

<template>
  <Teleport v-if="shown" defer :to="end ? '#status-end' : '#status-start'">
    <slot />
  </Teleport>
</template>
