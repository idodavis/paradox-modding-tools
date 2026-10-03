<script setup lang="ts">
/**
 * Left Contents rail for Guide and Patch Notes.
 */
import { useLocalStorage } from "@vueuse/core";
import type { Section } from "@services/internal/wiki/models";

const props = defineProps<{
  sections: Section[];
  active?: string;
}>();

const emit = defineEmits<{
  jump: [anchor: string];
}>();

const open = useLocalStorage("pmt.wiki.toc", true);

function isGroup(s: Section): boolean {
  return (s.tocLevel || 1) <= 2;
}

function itemPad(s: Section): string {
  const n = Math.max(0, (s.tocLevel || 1) - 3);
  return `${0.5 + n * 0.5}rem`;
}
</script>

<template>
  <div v-if="!open" class="flex w-8 shrink-0 flex-col items-center border-e border-default py-1.5">
    <UTooltip text="Show contents">
      <UButton
        icon="i-lucide-panel-left"
        color="neutral"
        variant="ghost"
        size="xs"
        @click="open = true"
      />
    </UTooltip>
  </div>
  <nav v-else class="flex w-40 min-h-0 shrink-0 flex-col overflow-auto border-e border-default px-2 py-2">
    <div class="mb-1 flex items-center gap-1">
      <div class="min-w-0 flex-1 text-xs font-semibold uppercase tracking-wide text-muted">
        Contents
      </div>
      <UTooltip text="Hide contents">
        <UButton
          icon="i-lucide-panel-left-close"
          color="neutral"
          variant="ghost"
          size="xs"
          @click="open = false"
        />
      </UTooltip>
    </div>
    <template v-for="(s, i) in props.sections" :key="`${s.anchor}-${i}`">
      <button
        v-if="isGroup(s)"
        type="button"
        class="w-full truncate text-left text-xs font-semibold tracking-wide"
        :class="[
          i > 0 ? 'mt-2' : 'mt-1',
          props.active === s.anchor ? 'text-primary' : 'text-highlighted',
        ]"
        @click="emit('jump', s.anchor)"
      >
        {{ s.line }}
      </button>
      <UButton
        v-else
        :label="s.line"
        color="neutral"
        variant="ghost"
        size="xs"
        block
        class="justify-start truncate text-left"
        :class="props.active === s.anchor ? 'bg-primary/10 rounded-md' : ''"
        :ui="{ label: props.active === s.anchor ? 'text-primary' : 'text-muted' }"
        :style="{ paddingLeft: itemPad(s) }"
        @click="emit('jump', s.anchor)"
      />
    </template>
  </nav>
</template>
