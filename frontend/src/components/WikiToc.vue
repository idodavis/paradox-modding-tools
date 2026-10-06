<script setup lang="ts">
/**
 * A wiki article's headings (the PatchNotes board's "On this page"): each jumps to its heading; the one
 * in view is marked in the accent. A top heading is set in the text's own color and weight, a subheading
 * set in and quieter, a deeper one smaller still. Its parent sets its width; hidden, it is a strip with the
 * button that shows it again.
 */
import type { Section } from "@services/internal/wiki/models";

const { sections, active = "" } = defineProps<{ sections: Section[]; active?: string }>();
const emit = defineEmits<{ jump: [anchor: string] }>();
const collapsed = defineModel<boolean>("collapsed", { default: false });

/** Each heading level's look, by its depth. */
const LEVELS: Record<number, string> = {
  1: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  2: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  3: "ps-5.5 text-12 text-dimmed",
  4: "ps-8 text-11 text-dimmed",
};
</script>

<template>
  <nav v-if="collapsed" class="flex flex-col items-center px-1 pt-3">
    <UTooltip text="Show the contents">
      <UButton icon="i-lucide-panel-left-open" variant="ghost" size="xs" square aria-label="Show the contents" @click="collapsed = false" />
    </UTooltip>
  </nav>
  <nav v-else class="flex min-h-0 flex-col overflow-auto pt-3 ps-1 pe-2">
    <div class="flex items-center ps-2.5 pb-1">
      <span class="grow text-11 font-medium text-dimmed">On this page</span>
      <UTooltip text="Hide the contents">
        <UButton icon="i-lucide-panel-left-close" variant="ghost" size="xs" square aria-label="Hide the contents" @click="collapsed = true" />
      </UTooltip>
    </div>
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
