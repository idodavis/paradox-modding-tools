<!--
  The Layers panel of a coat: the layers as a sortable list, each with its fields, with add and remove. Every change
  is an edit to the file.
-->
<script setup lang="ts">
import type { Coat } from "@services/draw/models";
import type { Model } from "../editor/models";

defineProps<{ model: Model; coat: Coat }>();
</script>

<template>
  <div />
</template>
