<!--
  The Visual view's panel: a window's Widgets, States and Drawables tabs, or a coat's Layers, plus a Reference tab
  that draws a screenshot under the picture.
-->
<script setup lang="ts">
import type { Coat, Window } from "@services/draw/models";

defineProps<{ path: string; window: Window | null; coat: Coat | null }>();
</script>

<template>
  <div />
</template>
