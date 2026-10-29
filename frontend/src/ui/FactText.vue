<!--
  Shows a message's parts, each fact in its style: a mod's name and a count in bold, code in the code font, and a
  path with its file brighter than its folder. A fact the message names again shows as plain text.
-->
<script setup lang="ts">
import { computed } from "vue";

import type { Part } from "../lib/format";

const { parts } = defineProps<{ parts: readonly Part[] }>();

/** Each part as drawn, a fact drawn before as plain text, with a path's folder and file. */
const drawn = computed(() => {
  const keys = new Set<string>();
  return parts.map((part) => {
    const key = `${part.style}:${part.text}`;
    const style = keys.has(key) ? "" : part.style;
    keys.add(key);
    const slash = style === "path" ? part.text.lastIndexOf("/") + 1 : 0;
    return { text: part.text, style, folder: part.text.slice(0, slash), file: part.text.slice(slash) };
  });
});
</script>

<template>
  <template v-for="(part, index) in drawn" :key="index">
    <b v-if="part.style === 'mod' || part.style === 'count'" class="font-semibold text-highlighted tabular-nums">{{
      part.text
    }}</b>
    <code v-else-if="part.style === 'code'" class="font-mono text-[0.92em] wrap-anywhere text-highlighted">{{
      part.text
    }}</code>
    <span v-else-if="part.style === 'path'" class="font-mono text-[0.92em] wrap-anywhere text-dimmed"
      >{{ part.folder }}<span class="font-medium text-highlighted">{{ part.file }}</span></span
    >
    <template v-else>{{ part.text }}</template>
  </template>
</template>
