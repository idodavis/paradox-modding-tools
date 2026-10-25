<!-- A link to a place in a file, showing its origin and path. Clicking it opens the file there. -->
<script setup lang="ts">
import type { Place } from "@services/workspace/models";
import { useEditorStore } from "../stores/editor";
import OriginChip from "./OriginChip.vue";

defineProps<{ place: Place; line?: number }>();

const editor = useEditorStore();
</script>

<template>
  <UButton
    :label="`${place.rel}${line ? `:${line}` : ''}`"
    variant="link"
    size="xs"
    class="max-w-full min-w-0 p-0 font-mono"
    :ui="{ label: 'truncate' }"
    @click="editor.show(place, line ? { lineNumber: line, column: 1 } : undefined)"
  >
    <template #leading><OriginChip :origin="place.origin" dot /></template>
  </UButton>
</template>
