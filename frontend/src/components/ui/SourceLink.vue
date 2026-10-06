<script setup lang="ts">
/**
 * A place in a file as `rel:line`, or the file alone, that opens the IDE there.
 */
import { useOpenInIde } from "../../composables/useOpenInIde";

const { workspaceId, at, line, file = false } = defineProps<{
  workspaceId: string;
  at: { path: string; rel: string };
  line: number;
  file?: boolean;
}>();
const { openInIde } = useOpenInIde();
</script>

<template>
  <UButton :label="file ? at.rel : `${at.rel}:${line + 1}`" variant="link" size="xs" class="max-w-full min-w-0 p-0 font-mono"
    :ui="{ label: 'truncate' }" @click="openInIde(workspaceId, at.path, line + 1)" />
</template>
