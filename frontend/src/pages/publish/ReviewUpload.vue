<!-- The upload review: the files sent, the rules, the DLCs and tags, and the Publish button with its progress. -->
<script setup lang="ts">
import type { Comparison, Upload } from "@services/steam/models";

defineProps<{ modId: string; upload: Upload; comparison: Comparison }>();
defineEmits<{ publish: [] }>();
</script>

<template>
  <div />
</template>
