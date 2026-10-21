<!-- A wiki article: its HTML with the article styles, a table of contents over its headings, and find. -->
<script setup lang="ts">
import type { Page } from "@services/wiki/models";

defineProps<{ page: Page }>();
</script>

<template>
  <article />
</template>
