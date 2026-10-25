<!--
  Read-only code, drawn as the editor draws it: its colors, its font, its top padding and its background. Given the
  line the code begins at in its file, it numbers the lines in a gutter like the editor's.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { editor } from "monaco-editor/editor/editor.api";

import { useSettingsStore } from "../stores/settings";

const {
  text,
  language,
  line = 0,
} = defineProps<{
  text: string;
  language: string;
  /** The line of the file that the text begins at, counted from one. Without it, the snippet shows no line numbers. */
  line?: number;
}>();

const settings = useSettingsStore();
const html = shallowRef("");
// the editor's font and top padding; Monaco rounds the height of a line to whole pixels
const style = computed(() => {
  const { fontFamily, fontSize, lineHeight, padding } = settings.codeFont();
  return {
    fontFamily,
    fontSize: `${fontSize}px`,
    lineHeight: `${Math.round(fontSize * lineHeight)}px`,
    paddingTop: `${padding.top}px`,
  };
});
// the text without the line breaks at its end, which Monaco would draw as an empty last line
const code = computed(() => text.replace(/(\r\n|\r|\n)+$/, ""));
// the file's number of each line of the text, split as Monaco splits it
const lineNumbers = computed(() => (line ? code.value.split(/\r\n|\r|\n/).map((_text, index) => line + index) : []));
// the gutter: as wide as the editor's, or wider for a longer number, with Monaco's space before the code
const gutter = computed(() => ({
  width: `${Math.max(settings.codeFont().lineNumbersMinChars, String(lineNumbers.value.at(-1)).length)}ch`,
  marginInlineEnd: `${editor.EditorOptions.lineDecorationsWidth.defaultValue}px`,
}));

watch(
  [code, () => language, () => settings.theme()],
  async (_sources, _previous, onCleanup) => {
    let stale = false;
    onCleanup(() => (stale = true));
    const colored = await editor.colorize(code.value, language, {});
    if (!stale) html.value = colored;
  },
  { immediate: true, flush: "post" },
);
</script>

<template>
  <div class="flex bg-(--bg-0) text-(--fg-1)" :style="style">
    <div v-if="lineNumbers.length" class="shrink-0 text-right text-(--fg-3) tabular-nums select-none" :style="gutter">
      <div v-for="number in lineNumbers" :key="number">{{ number }}</div>
    </div>
    <!-- eslint-disable-next-line vue/no-v-html -- Monaco escapes the text it colors -->
    <div class="min-w-0 flex-1 overflow-x-auto whitespace-pre" v-html="html" />
  </div>
</template>
