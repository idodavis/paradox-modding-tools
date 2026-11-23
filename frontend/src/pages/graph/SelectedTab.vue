<!--
  The small "Selected" tab on the one selected card: above its top edge, or
  inside its top corner on a row of a table.
-->
<script setup lang="ts">
const { inside = false } = defineProps<{ inside?: boolean }>();
</script>

<template>
  <span
    class="absolute z-10 inline-flex h-4.5 items-center bg-primary px-2 text-11 leading-none font-semibold text-(--accent-ink)"
    :class="inside ? 'start-1 top-0 rounded-br-md' : 'end-3 -top-4.5 rounded-t-md'"
  >
    Selected
  </span>
</template>
