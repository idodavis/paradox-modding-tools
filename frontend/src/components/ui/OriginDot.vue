<script setup lang="ts">
/**
 * An origin's color as a dot (pmt.css .dot): 8px, or 6px small.
 */
const { color, small = false } = defineProps<{ color: string; small?: boolean }>();
</script>

<template>
  <span class="shrink-0 rounded-full" :class="small ? 'size-1.5' : 'size-2'" :style="{ background: color }" />
</template>
