<!-- Shows a message's parts, each fact in its own style. A fact named again shows as plain text. -->
<script setup lang="ts">
import { computed } from "vue";

import type { Part } from "@services/lsp/models";

const { parts = [] } = defineProps<{ parts?: readonly Part[] | null }>();

/** drawn holds each part with its style: plain for a fact shown before, and a path split into folder and file. */
const drawn = computed(() => {
  const keys = new Set<string>();
  return (parts ?? []).map((part) => {
    const key = `${part.style}:${part.text}`;
    const style = keys.has(key) ? "" : part.style;
    keys.add(key);
    const fileStart = style === "path" ? part.text.lastIndexOf("/") + 1 : 0;
    return { text: part.text, style, folder: part.text.slice(0, fileStart), file: part.text.slice(fileStart) };
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
