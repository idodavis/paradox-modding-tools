<!--
  A Monaco editor over one model. It reports the caret, attaches the hover card, and opens the graph on Ctrl Shift G.
-->
<script setup lang="ts">
import type { IPosition } from "monaco-editor";
import type { Model } from "./editor/models";

defineProps<{ model: Model; position?: IPosition }>();
</script>

<template>
  <div />
</template>
