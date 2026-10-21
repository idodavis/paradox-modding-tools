<!-- One problem: its message, its severity and its place. -->
<script setup lang="ts">
import type { Code } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

defineProps<{
  code: Code;
  facts: Record<string, string>;
  place: Place;
  line: number;
}>();
defineEmits<{ open: [] }>();
</script>

<template>
  <div />
</template>
