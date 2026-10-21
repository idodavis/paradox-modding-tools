<!--
  The findings of one section as a table. Each column sorts, a column with few values filters, and selecting a row
  opens it.
-->
<script setup lang="ts">
import type { Finding, Section } from "@services/health/models";

defineProps<{ findings: Finding[]; section: Section }>();
defineEmits<{ open: [finding: Finding] }>();
</script>

<template>
  <div />
</template>
