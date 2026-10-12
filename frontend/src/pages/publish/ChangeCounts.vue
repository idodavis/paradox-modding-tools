<script setup lang="ts">
/** The lines a change adds and removes, "+38 −12", as Go counts them. */
defineProps<{ added: number; removed: number }>();
</script>

<template>
  <span class="ms-auto shrink-0 font-mono text-11">
    <span v-if="added" class="text-success">+{{ added }}</span>
    <span v-if="removed" class="ms-1 text-error">−{{ removed }}</span>
  </span>
</template>
