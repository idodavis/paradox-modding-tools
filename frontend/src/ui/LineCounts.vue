<!-- The lines a change adds and removes, as +3 −1 in the diff's green and red. -->
<script setup lang="ts">
const { added, removed } = defineProps<{ added: number; removed: number }>();
</script>

<template>
  <span class="flex shrink-0 gap-1 font-mono text-11 tabular-nums">
    <span class="text-success">+{{ added }}</span>
    <span class="text-error">−{{ removed }}</span>
  </span>
</template>
