<!--
  A find bar over a list of texts: the term, the count, the current hit, next, previous and close. It tells its holder
  which text is current. A bar its holder opens on a key takes the keys at once.
-->
<script setup lang="ts">
import { computed, useId, useTemplateRef, watch } from "vue";
import { useCycleList } from "@vueuse/core";

import { matchQuery } from "../lib/format";

const term = defineModel<string>("term", { default: "" });
const { texts, autofocus = false } = defineProps<{ texts: readonly string[]; autofocus?: boolean }>();
const emit = defineEmits<{ hit: [index: number]; close: [] }>();

const input = useTemplateRef("input");
const id = useId();

// the indexes of the texts that hold the term, in order
const hits = computed(() =>
  term.value.trim() ? texts.flatMap((text, index) => (matchQuery(term.value, text) ? [index] : [])) : [],
);
const { state, index, go, next, prev } = useCycleList(hits);

watch(hits, (indexes) => {
  go(0);
  if (indexes.length) emit("hit", indexes[0]);
});

/** step moves to the next hit, or the previous one for a negative step, and tells the holder. */
function step(by: number): void {
  if (!hits.value.length) return;
  if (by > 0) next();
  else prev();
  emit("hit", state.value);
}

// Ctrl F finds here unless an editor holds the focus, which keeps its own find
defineShortcuts({
  ctrl_f: {
    usingInput: true,
    handler: () => {
      if (!document.activeElement?.closest(".monaco-editor")) input.value?.inputRef?.focus();
    },
  },
  enter: { usingInput: id, handler: () => step(1) },
  shift_enter: { usingInput: id, handler: () => step(-1) },
  escape: { usingInput: id, handler: () => emit("close") },
});
</script>

<template>
  <UInput
    ref="input"
    v-model="term"
    icon="i-lucide-search"
    placeholder="Find"
    :name="id"
    :autofocus="autofocus"
    :ui="{ base: term.trim() && 'pe-36', trailing: 'gap-0.5' }"
  >
    <template #trailing>
      <template v-if="term.trim()">
        <span class="pe-1 text-11 text-dimmed tabular-nums">{{
          hits.length ? `${index + 1} of ${hits.length}` : "No results"
        }}</span>
        <UButton
          icon="i-lucide-chevron-up"
          variant="ghost"
          size="xs"
          :disabled="!hits.length"
          aria-label="Previous match"
          @click="step(-1)"
        />
        <UButton
          icon="i-lucide-chevron-down"
          variant="ghost"
          size="xs"
          :disabled="!hits.length"
          aria-label="Next match"
          @click="step(1)"
        />
        <UButton icon="i-lucide-x" variant="ghost" size="xs" aria-label="Close the find" @click="emit('close')" />
      </template>
      <UKbd v-else value="Ctrl F" />
    </template>
  </UInput>
</template>
