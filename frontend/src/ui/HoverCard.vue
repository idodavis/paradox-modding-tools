<!-- The hover card: the card's facts laid out in sections. A site in it opens that place. -->
<script setup lang="ts">
import type { Card } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

defineProps<{ card: Card }>();
defineEmits<{ open: [place: Place, line: number] }>();
</script>

<template>
  <div />
</template>
