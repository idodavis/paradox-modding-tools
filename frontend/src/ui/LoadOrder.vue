<!-- The load order as a row of mod chips, reordered by dragging where editing is allowed. -->
<script setup lang="ts">
import type { Mod } from "@services/workspace/models";

defineModel<string[]>("order", { required: true });
defineProps<{ mods: Mod[]; editable: boolean }>();
</script>

<template>
  <div />
</template>
