<!-- Shows read-only code as the editor draws it. It can number its lines and highlight a run of them. -->
<script lang="ts">
import type { Format } from "@internal/files/models";

/** EDITOR_LANGUAGES holds the editor language a file is colored in, by Go's format. */
export const EDITOR_LANGUAGES: Readonly<Record<Format, string>> = {
  "": "plaintext",
  script: "paradox-script",
  gui: "paradox-gui",
  asset: "paradox-script",
  font: "paradox-script",
  gfx: "paradox-script",
  localization: "paradox-localization",
  image: "plaintext",
  table: "plaintext",
  descriptor: "plaintext",
  shader: "plaintext",
  text: "plaintext",
  binary: "plaintext",
};
</script>

<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { editor } from "monaco-editor/editor/editor.api";

import { useSettingsStore } from "../stores/settings";

const {
  text,
  language,
  line = 0,
  focus = undefined,
} = defineProps<{
  text: string;
  language: string;
  /** The file line the text begins at, from 1. Without it, no line numbers show. */
  line?: number;
  /** The first and last file lines to highlight. */
  focus?: [number, number];
}>();

const settings = useSettingsStore();
const html = shallowRef("");
// the height of a line, which Monaco rounds to whole pixels
const lineHeight = computed(() => {
  const { fontSize, lineHeight: factor } = settings.codeFont();
  return Math.round(fontSize * factor);
});
// the editor's font and top padding
const style = computed(() => {
  const { fontFamily, fontSize, padding } = settings.codeFont();
  return { fontFamily, fontSize: `${fontSize}px`, lineHeight: `${lineHeight.value}px`, paddingTop: `${padding.top}px` };
});
// the text without its trailing line breaks, which Monaco would draw as an empty line
const code = computed(() => text.replace(/(\r\n|\r|\n)+$/, ""));
// the file's line number of each line of the text
const lineNumbers = computed(() => (line ? code.value.split(/\r\n|\r|\n/).map((_text, index) => line + index) : []));
// the gutter, as wide as the editor's or as the longest number
const gutter = computed(() => ({
  width: `${Math.max(settings.codeFont().lineNumbersMinChars, String(lineNumbers.value.at(-1)).length)}ch`,
  marginInlineEnd: `${editor.EditorOptions.lineDecorationsWidth.defaultValue}px`,
}));
// the band behind the highlighted lines
const band = computed(() => {
  const numbers = lineNumbers.value;
  if (!focus || !numbers.length) return undefined;
  const first = Math.max(focus[0], numbers[0] ?? 0);
  const last = Math.min(focus[1], numbers.at(-1) ?? 0);
  if (first > last) return undefined;
  return {
    top: `calc(${style.value.paddingTop} + ${(first - line) * lineHeight.value}px)`,
    height: `${(last - first + 1) * lineHeight.value}px`,
  };
});

// colors the text with the editor's colorizer, dropping a stale answer
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
  <div class="relative flex bg-(--bg-0) text-(--fg-1)" :style="style">
    <div
      v-if="band"
      class="pointer-events-none absolute inset-x-0 bg-(--bg-3) shadow-[inset_2px_0_0_var(--accent)]"
      :style="band"
    />
    <div
      v-if="lineNumbers.length"
      class="relative shrink-0 text-right text-(--fg-3) tabular-nums select-none"
      :style="gutter"
    >
      <div v-for="number in lineNumbers" :key="number">{{ number }}</div>
    </div>
    <!-- eslint-disable-next-line vue/no-v-html -- Monaco escapes the text it colors -->
    <div class="relative min-w-0 flex-1 overflow-x-auto whitespace-pre" v-html="html" />
  </div>
</template>
