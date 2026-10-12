<script setup lang="ts">
/**
 * A localization value as the game says it, quoted as the IDE's hover quotes it: a bar on its start in
 * the blue at half strength, on bg-3; bare, the text alone at its parent's size. Its codes take the loc
 * grammar's colors (`lib/codeView.ts`'s `locTokens`), its \n are line breaks, none kept at either end.
 */
import { computed } from "vue";
import { locTokens } from "../lib/codeView";

const { text, bare = false } = defineProps<{ text: string; bare?: boolean }>();

const value = computed(() => text.replace(/^(\\n)+|(\\n)+\s*$/g, ""));
// each token's \n a line break
const parts = computed(() =>
  locTokens(value.value).flatMap(({ content, color }) =>
    content
      .split(/(\\n)/)
      .filter(Boolean)
      .map((part) => ({ part, color, br: part === "\\n" })),
  ),
);
</script>

<template>
  <blockquote class="break-words" :class="{ 'border-s-3 border-(--blue)/50 bg-elevated px-2 py-1 text-13': !bare }">
    <template v-for="({ part, color, br }, i) in parts" :key="i">
      <br v-if="br" />
      <span v-else-if="color" class="font-mono text-12" :style="{ color }">{{ part }}</span>
      <template v-else>{{ part }}</template>
    </template>
  </blockquote>
</template>
