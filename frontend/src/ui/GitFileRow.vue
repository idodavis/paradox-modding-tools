<!-- One changed file: its status, its path, and buttons to stage, unstage, discard and open. -->
<script setup lang="ts">
import type { Change } from "@services/git/models";

defineProps<{ change: Change }>();
defineEmits<{ open: []; stage: []; unstage: []; discard: [] }>();
</script>

<template>
  <div />
</template>
