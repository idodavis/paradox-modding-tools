<script setup lang="ts">
/**
 * A place in a file as `rel:line`, or the file alone, that opens the IDE there.
 */
import { useIdeStore } from "../../stores/ide";

const {
  at,
  line,
  file = false,
} = defineProps<{
  at: { path: string; rel: string };
  line: number;
  file?: boolean;
}>();
const ide = useIdeStore();
</script>

<template>
  <UButton
    :label="file ? at.rel : `${at.rel}:${line + 1}`"
    variant="link"
    size="xs"
    class="max-w-full min-w-0 p-0 font-mono"
    :ui="{ label: 'truncate' }"
    @click="ide.open(at.path, { line })"
  />
</template>
