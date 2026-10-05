<script setup lang="ts">
/**
 * Patch Center: wiki patch notes for the workspace game.
 */
import PatchNotesPane from "../components/patchCenter/PatchNotesPane.vue";

defineProps<{ id: string }>();
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <PatchNotesPane class="min-h-0 flex-1" />
  </div>
</template>
