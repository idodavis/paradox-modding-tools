<!-- A link to a place in a file, showing its origin and path. Clicking it opens the file there. -->
<script setup lang="ts">
import type { Place } from "@services/workspace/models";

defineProps<{ place: Place; line?: number }>();
</script>

<template>
  <a />
</template>
