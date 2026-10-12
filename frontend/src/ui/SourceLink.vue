<script setup lang="ts">
/**
 * A place in a file as `rel:line`, or the file alone, that opens the IDE there.
 */
import { useEditorStore } from "../stores/editor";

const {
  place,
  line,
  file = false,
} = defineProps<{
  place: { path: string; rel: string };
  line: number;
  file?: boolean;
}>();
const editor = useEditorStore();
</script>

<template>
  <UButton
    :label="file ? place.rel : `${place.rel}:${line + 1}`"
    variant="link"
    size="xs"
    class="max-w-full min-w-0 p-0 font-mono"
    :ui="{ label: 'truncate' }"
    @click="editor.open(place.path, { line })"
  />
</template>
