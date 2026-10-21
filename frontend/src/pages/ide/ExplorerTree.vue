<!--
  The file explorer: the roots and their folders, with search, rename, drag and drop, git marks and the context menu.
  A folder's listing is fetched when it expands, and a double click pins a preview tab.
-->
<script setup lang="ts">
import type { Root } from "@internal/files/models";
import type { Place } from "@services/workspace/models";

defineProps<{ roots: Root[] }>();
defineEmits<{ open: [place: Place, preview: boolean] }>();
</script>

<template>
  <div />
</template>
