<!--
  A link to a place in a file, showing its path and line. Clicking it opens the file there. It leads with the origin's
  dot unless the leading slot says otherwise.
-->
<script setup lang="ts">
import type { Place } from "@services/workspace/models";
import { useEditorStore } from "../stores/editor";
import OriginChip from "./OriginChip.vue";

defineProps<{ place: Place; line?: number; column?: number }>();

const editor = useEditorStore();
</script>

<template>
  <UButton
    :label="`${place.rel}${line ? `:${line}` : ''}`"
    variant="link"
    size="xs"
    class="max-w-full min-w-0 p-0 font-mono"
    :ui="{ label: 'truncate' }"
    @click="editor.show(place, line ? { lineNumber: line, column: column ?? 1 } : undefined)"
  >
    <template #leading>
      <slot name="leading"><OriginChip :origin="place.origin" dot /></slot>
    </template>
  </UButton>
</template>
