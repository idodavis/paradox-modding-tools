<script setup lang="ts">
/**
 * Where a mod's Workshop description comes from, in the words Publish and Settings both use; the slot
 * holds the way to change it.
 */
import { computed } from "vue";
import type { ListingSummary } from "@services/models";

const { summary } = defineProps<{ summary: ListingSummary }>();

const words = computed(() => {
  const file = summary.descRel;
  const descriptions: Record<string, string> = {
    markdown: `From ${file}, converted to BBCode when you upload. Steam's text is replaced.`,
    bbcode: `From ${file}, uploaded as written. Steam's text is replaced.`,
  };
  const missing = file && !summary.descExists ? ` ${file} is not in the mod yet.` : "";
  return (
    (descriptions[summary.descSource] ??
      "Steam keeps its own text: uploads leave it as it is. Edit it on the Workshop page.") + missing
  );
});
</script>

<template>
  <p class="text-12 text-dimmed">
    {{ words }}
    <slot />
  </p>
</template>
