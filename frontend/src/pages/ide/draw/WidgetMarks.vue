<!-- The marks over the picture: the selected widget, the hovered one and the hidden ones, each from its drawn rect. -->
<script setup lang="ts">
defineProps<{
  rects: Map<number, DOMRect>;
  selected: number | null;
  hovered: number | null;
}>();
</script>

<template>
  <div />
</template>
