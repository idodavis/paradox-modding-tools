<script setup lang="ts">
/**
 * Read-only code, as Pierre draws it: the boards' `.code`, its rows numbered by the file's lines; inline, one
 * line in the code's colors (a property's value), plain until its grammar loads.
 */
import { onBeforeUnmount, useTemplateRef, watchEffect } from "vue";
import { computedAsync } from "@vueuse/core";
import { codeTokens, codeView, drawCode } from "../lib/code";

const { text, name, fromLine = 0, inline = false } = defineProps<{
  text: string;
  /** The file's name or path, whose extension picks the grammar. */
  name: string;
  /** The 0-based file line `text` starts at. */
  fromLine?: number;
  inline?: boolean;
}>();

const host = useTemplateRef("host");
const view = inline ? undefined : codeView(true);
watchEffect(() => {
  if (view && host.value) drawCode(view, host.value, text, name, fromLine);
});
onBeforeUnmount(() => view?.cleanUp());
const tokens = computedAsync(() => (inline ? codeTokens(text, name) : null), null);
</script>

<template>
  <span v-if="inline" class="font-mono">
    <template v-if="tokens">
      <span v-for="(token, i) in tokens" :key="i" :style="{ color: token.color }">{{ token.content }}</span>
    </template>
    <template v-else>{{ text }}</template>
  </span>
  <div v-else ref="host" />
</template>
