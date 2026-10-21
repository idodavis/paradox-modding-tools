<!--
  The code beside the picture. It marks the selected and hovered widgets' lines, shows the drawn size on each widget's
  first line, outlines the widget under the pointer in the picture, and scrolls to a selection.
-->
<script setup lang="ts">
import type { Window } from "@services/draw/models";
import type { Model } from "../editor/models";

defineProps<{ model: Model; rects: Map<number, DOMRect>; window: Window }>();
</script>

<template>
  <div />
</template>
