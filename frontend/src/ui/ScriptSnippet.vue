<!--
  Shows read-only code in the colors Go gives it. It can number its lines and
  highlight a run of them.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { editor } from "monaco-editor/editor/editor.api";

import { Format } from "@internal/files/models";
import { SYNTAX } from "../lib/syntax";
import { legendQuery, textColorsQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";

/**
 * A Piece is a run of a line's text with the syntax color it is drawn in,
 * none for plain text.
 */
type Piece = { text: string; color?: string };

const {
  text,
  path = "",
  format = Format.FormatScript,
  line = 0,
  focus = undefined,
  inline = false,
} = defineProps<{
  text: string;
  /**
   * The file the text is from, which Go reads it as. Without it, the text is
   * read by its format alone.
   */
  path?: string;
  /** The format of a text from no file. */
  format?: Format;
  /**
   * The file line the text begins at, from 1. Without it, no line numbers
   * show.
   */
  line?: number;
  /** The first and last file lines to highlight. */
  focus?: [number, number];
  /**
   * Whether it shows as a piece of a line, in the line's text size, with no
   * gutter, band or padding.
   */
  inline?: boolean;
}>();

const settings = useSettingsStore();
// Monaco rounds the height of a line to whole pixels.
const lineHeight = computed(() => {
  const { fontSize, lineHeight: factor } = settings.codeFont();
  return Math.round(fontSize * factor);
});
const style = computed(() => {
  const { fontFamily, fontSize, padding } = settings.codeFont();
  return { fontFamily, fontSize: `${fontSize}px`, lineHeight: `${lineHeight.value}px`, paddingTop: `${padding.top}px` };
});
// Trailing line breaks are dropped, since they would draw as an empty line.
const source = computed(() => text.replace(/[\r\n]+$/, "").split(/\r\n|\r|\n/));
// The indent every line shares is not drawn, so nested code starts at the
// left. Go gets the lines with it, to match them with the file.
const indent = computed(() => {
  const indents = source.value.filter((row) => row.trim()).map((row) => /^[\t ]*/.exec(row)?.[0] ?? "");
  const shared = indents.reduce((common, own) => {
    let end = 0;
    while (end < common.length && common[end] === own[end]) end++;
    return common.slice(0, end);
  }, indents[0] ?? "");
  return shared.length;
});
const lines = computed(() => source.value.map((row) => row.slice(indent.value)));
const lineNumbers = computed(() => (line ? lines.value.map((_text, index) => line + index) : []));
const gutter = computed(() => ({
  width: `${Math.max(settings.codeFont().lineNumbersMinChars, String(lineNumbers.value.at(-1)).length)}ch`,
  marginInlineEnd: `${editor.EditorOptions.lineDecorationsWidth.defaultValue}px`,
}));
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

// The text is drawn once, in its colors. Until Go answers it keeps its size
// and shows nothing, and a text drawn before is drawn again at once. With no
// workspace open Go gives no colors and the text is drawn plain.
const { data: legend, status: legendStatus } = useQuery(legendQuery);
const { data: tokens, status } = useQuery(() => textColorsQuery({ path, format, text: source.value.join("\n"), line }));
const painted = computed(() => status.value !== "pending" && legendStatus.value !== "pending");
const rows = computed<Piece[][]>(() =>
  tokens.value && legend.value
    ? colorLines(source.value, tokens.value, legend.value.tokenTypes ?? []).map(dedent)
    : lines.value.map((row) => [{ text: row }]),
);

/**
 * dedent drops the shared indent from the start of a line's first piece. No
 * token covers the indent.
 */
function dedent(pieces: Piece[]): Piece[] {
  const [first, ...rest] = pieces;
  return first && !first.color ? [{ text: first.text.slice(indent.value) }, ...rest] : pieces;
}

/**
 * colorLines cuts each line into pieces by Go's tokens, which come five
 * numbers each: the lines since the last token, the columns since it (or
 * since the line's start on a new line), the length, the type's place in the
 * legend, and the modifiers.
 */
function colorLines(source: string[], tokens: number[], types: string[]): Piece[][] {
  const colored: Piece[][] = source.map(() => []);
  const ends = source.map(() => 0);
  let row = 0;
  let column = 0;
  for (let at = 0; at + 4 < tokens.length; at += 5) {
    const [lineDelta = 0, columnDelta = 0, length = 0, type = 0] = tokens.slice(at, at + 4);
    row += lineDelta;
    column = lineDelta ? columnDelta : column + columnDelta;
    const content = source[row];
    const pieces = colored[row];
    if (content === undefined || !pieces) break;
    const end = ends[row] ?? 0;
    if (column > end) pieces.push({ text: content.slice(end, column) });
    pieces.push({ text: content.slice(column, column + length), color: SYNTAX[types[type] ?? ""] });
    ends[row] = column + length;
  }
  return colored.map((pieces, index) => {
    const content = source[index] ?? "";
    const end = ends[index] ?? 0;
    return end < content.length ? [...pieces, { text: content.slice(end) }] : pieces;
  });
}

/**
 * paint returns the color a piece is drawn in and the weight the theme gives
 * that color. Plain text takes neither.
 */
function paint(piece: Piece): Record<string, string> | undefined {
  if (!piece.color) return undefined;
  return { color: `var(--syn-${piece.color})`, fontWeight: `var(--syn-${piece.color}-weight, inherit)` };
}
</script>

<template>
  <span
    v-if="inline"
    class="whitespace-pre-wrap"
    :class="{ invisible: !painted }"
    :style="{ fontFamily: style.fontFamily }"
  >
    <template v-for="(pieces, row) in rows" :key="row">
      <span v-for="(piece, at) in pieces" :key="at" :style="paint(piece)">{{ piece.text }}</span>
    </template>
  </span>
  <div v-else class="relative flex bg-(--bg-0) text-(--fg-1)" :style="style">
    <div
      v-if="band"
      class="pointer-events-none absolute inset-x-0 bg-(--bg-3) shadow-[inset_2px_0_0_var(--accent)]"
      :style="band"
    />
    <div
      v-if="lineNumbers.length"
      class="relative box-content shrink-0 ps-3 text-right text-(--fg-3) tabular-nums select-none"
      :style="gutter"
    >
      <div v-for="number in lineNumbers" :key="number">{{ number }}</div>
    </div>
    <div class="relative min-w-0 flex-1 overflow-x-auto whitespace-pre" :class="{ invisible: !painted }">
      <div v-for="(pieces, row) in rows" :key="row" :style="{ minHeight: `${lineHeight}px` }">
        <span v-for="(piece, at) in pieces" :key="at" :style="paint(piece)">{{ piece.text }}</span>
      </div>
    </div>
  </div>
</template>
