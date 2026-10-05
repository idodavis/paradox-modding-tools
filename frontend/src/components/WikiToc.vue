<script setup lang="ts">
/**
 * A wiki article's headings (the PatchNotes board's "On this page"): each jumps to its heading; the one
 * in view is marked in the accent. A top heading is set in the text's own color and weight, a subheading
 * set in and quieter, a deeper one smaller still. Its parent sets its width.
 */
import type { Section } from "@services/internal/wiki/models";

const { sections, active = "" } = defineProps<{ sections: Section[]; active?: string }>();
const emit = defineEmits<{ jump: [anchor: string] }>();

/** Each heading level's look, by its depth. */
const LEVELS: Record<number, string> = {
  1: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  2: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  3: "ps-5.5 text-12 text-dimmed",
  4: "ps-8 text-11 text-dimmed",
};
</script>

<template>
  <nav class="flex min-h-0 flex-col overflow-auto pt-4 ps-1 pe-2">
    <span class="px-2.5 pb-2 text-11 font-medium text-dimmed">On this page</span>
    <button
      v-for="(section, i) in sections"
      :key="`${section.anchor}-${i}`"
      type="button"
      class="shrink-0 truncate border-s-2 py-1.25 pe-2.5 text-start leading-4"
      :class="[
        active === section.anchor ? 'border-primary' : 'border-default',
        LEVELS[Math.min(section.tocLevel || 1, 4)],
        active === section.anchor && 'text-highlighted',
      ]"
      @click="emit('jump', section.anchor)"
    >
      {{ section.line }}
    </button>
  </nav>
</template>
