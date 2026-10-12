<script setup lang="ts">
/**
 * A place in a file as `rel:line`, or the file alone, that opens the IDE there.
 */
import { useEditorStore } from "../stores/editor";

const {
  at,
  line,
  file = false,
} = defineProps<{
  at: { path: string; rel: string };
  line: number;
  file?: boolean;
}>();
const editor = useEditorStore();
</script>

<template>
  <UButton
    :label="file ? at.rel : `${at.rel}:${line + 1}`"
    variant="link"
    size="xs"
    class="max-w-full min-w-0 p-0 font-mono"
    :ui="{ label: 'truncate' }"
    @click="editor.open(at.path, { line })"
  />
</template>
