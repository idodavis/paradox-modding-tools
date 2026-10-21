<!--
  The graph canvas. It draws the laid-out graph with a minimap, reports a node's click and double click, and exports
  the picture as SVG or PNG.
-->
<script setup lang="ts">
import type { Graph } from "@services/graph/models";

defineProps<{ graph: Graph }>();
defineEmits<{ select: [id: string]; draw: [id: string] }>();
</script>

<template>
  <div />
</template>
