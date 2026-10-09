<script setup lang="ts">
/**
 * Find in a page (Patch Notes, the Steam compare, the Wiki pane): Ctrl F focuses it, Enter and Shift Enter step to
 * the next and previous hit, and it counts them as "N of M".
 */
import { onActivated, onDeactivated, shallowRef, useId, useTemplateRef } from "vue";

const { hit, hits, placeholder } = defineProps<{ hit: number; hits: number; placeholder: string }>();
const emit = defineEmits<{ step: [by: number] }>();
const query = defineModel<string>({ required: true });

const input = useTemplateRef("input");
const name = useId();
// shortcuts listen on the document, and a page is kept alive behind the others: only a shown one finds
const shown = shallowRef(true);
onActivated(() => (shown.value = true));
onDeactivated(() => (shown.value = false));
defineShortcuts({
  ctrl_f: { usingInput: true, handler: () => shown.value && input.value?.inputRef?.focus() },
  enter: { usingInput: name, handler: () => emit("step", 1) },
  shift_enter: { usingInput: name, handler: () => emit("step", -1) },
});
</script>

<template>
  <UInput
    ref="input"
    v-model="query"
    icon="i-lucide-search"
    :placeholder="placeholder"
    :name="name"
    :ui="{ base: query.trim() && 'pe-30', trailing: 'gap-0.5' }"
  >
    <template #trailing>
      <template v-if="query.trim()">
        <span class="pe-1 text-11 text-dimmed tabular-nums">{{ hits ? `${hit + 1} of ${hits}` : "No results" }}</span>
        <UButton
          icon="i-lucide-chevron-up"
          variant="ghost"
          size="xs"
          :disabled="!hits"
          aria-label="Previous match"
          @click="emit('step', -1)"
        />
        <UButton
          icon="i-lucide-chevron-down"
          variant="ghost"
          size="xs"
          :disabled="!hits"
          aria-label="Next match"
          @click="emit('step', 1)"
        />
      </template>
      <UKbd v-else value="Ctrl F" />
    </template>
  </UInput>
</template>
