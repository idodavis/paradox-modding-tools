<script setup lang="ts">
/**
 * A surface (pmt.css .card): bg-2, a line-1 border and 8px corners; raised is bg-3.
 */
const { raised = false } = defineProps<{ raised?: boolean }>();
</script>

<template>
  <div class="rounded-lg border border-default" :class="raised ? 'bg-elevated' : 'bg-muted'">
    <slot />
  </div>
</template>
