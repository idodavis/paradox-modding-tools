<script setup lang="ts">
/**
 * A page's facts in the status bar, at its start or before Help at its end. They are there while the page
 * is shown: a kept-alive page's leave with it.
 */
import { onActivated, onDeactivated, shallowRef } from "vue";

const { end = false } = defineProps<{ end?: boolean }>();
const visible = shallowRef(true);
onActivated(() => (visible.value = true));
onDeactivated(() => (visible.value = false));
</script>

<template>
  <Teleport v-if="visible" defer :to="end ? '#status-end' : '#status-start'">
    <slot />
  </Teleport>
</template>
