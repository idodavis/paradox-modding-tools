<script setup lang="ts">
/**
 * The code side of a file's Visual view, in one editor of the file while the picture is beside it: the picture's marks
 * on the code that writes it, the caret selecting what it is in, and the pointer on an element's code outlining it in
 * the picture. It reads what the code says from the editor's own text and what the picture drew from the file's record,
 * so it asks Go nothing of its own.
 */
import { computed, onScopeDispose, watch, watchEffect } from "vue";
import { refDebounced } from "@vueuse/core";
import type { Element as DrawElement } from "@services/internal/draw";
import type { Position, Range } from "@services/internal/lsp";
import { pathKey } from "../composables/useIdeRoots";
import { bytesAt, toRange } from "../editor/models";
import { monaco } from "../lib/monaco";
import { useDraw, useDrawStore } from "../stores/draw";
import { useEditorStore } from "../stores/editor";
import { hiddenOf, withDescendants } from "./widgets";

const { codeEditor, model, workspaceId, path } = defineProps<{
  codeEditor: monaco.editor.IStandaloneCodeEditor;
  model: monaco.editor.ITextModel;
  workspaceId: string;
  path: string;
}>();

// the owner of the markers that say what the game's files did not give
const MISSING = "pmt.draw";
const GLYPHS = monaco.editor.MouseTargetType.GUTTER_GLYPH_MARGIN;

const store = useDrawStore();
const editor = useEditorStore();
const { file, list, elements } = useDraw(
  () => workspaceId,
  () => path,
);

/** Whether a position comes at or before another, by line and byte column. */
const isAtOrBefore = (first: Position, second: Position) =>
  first.line < second.line || (first.line === second.line && first.character <= second.character);

/** A block's lines, whole, as the editor counts them. */
const linesOf = (range: Range) => new monaco.Range(range.start.line + 1, 1, range.end.line + 1, 1);

/** Text after a line's end, as the editor counts lines; an empty range shows it only when told to. */
function lineEnd(line: number, content: string, inlineClassName: string): monaco.editor.IModelDeltaDecoration {
  const end = model.getLineMaxColumn(line);
  return {
    range: new monaco.Range(line, end, line, end),
    options: { showIfCollapsed: true, after: { content, inlineClassName } },
  };
}

/** Where an element is written, as text, to tell elements apart that are written in the same place. */
const spanKey = (element?: DrawElement) => JSON.stringify(element?.location);

// the blocks this file writes elements as, in the list's order, each with its element
const blocks = computed(() =>
  elements.value.flatMap(({ location }, element) =>
    pathKey(location.path) === pathKey(path) ? [{ element, range: location.range }] : [],
  ),
);

/** The deepest element whose block holds a line and byte column of this file: the last the list names, which lists
 * parents first; -1 for none. */
function elementAt(line: number, col: number): number {
  const position = { line, character: col };
  return (
    blocks.value.findLast(({ range }) => isAtOrBefore(range.start, position) && isAtOrBefore(position, range.end))
      ?.element ?? -1
  );
}

// the elements whose blocks start on each line: the line's eye hides and shows them together, its size is the first's
const firstLines = computed(() => {
  const lines = new Map<number, number[]>();
  for (const { element, range } of blocks.value)
    lines.set(range.start.line, [...(lines.get(range.start.line) ?? []), element]);
  return lines;
});

// what the caret rests on, a moment after it stops: the element whose block holds it, the key its line writes and the
// token under it, which the picture's guides read. The caret selects the element, unless the view put the caret
// there, or the element is written in the same place as the selected one (a template used twice)
const caret = refDebounced(
  computed(() => (pathKey(editor.activeFile) === pathKey(path) ? editor.caret : null)),
  300,
);
watch(caret, (position) => {
  if (!position) {
    file.value.caret = null;
    return;
  }
  const element = elementAt(position.line, position.col);
  const key = /^\s*([\w.]+)\s*=/.exec(model.getLineContent(position.line + 1))?.[1] ?? "";
  const cursor = codeEditor.getPosition();
  const token = (cursor && model.getWordAtPosition(cursor)?.word) || "";
  file.value.caret = { element, key, token };
  const target = file.value.caretTarget;
  if (target && target.line === position.line && target.col === position.col) return;
  if (element >= 0 && spanKey(elements.value[element]) !== spanKey(elements.value[file.value.selection]))
    file.value.selection = element;
});

// the blocks' marks: the selected one's lines, the hidden ones' text dimmed, each first line's eye and drawn size
const marks = codeEditor.createDecorationsCollection();
watchEffect(() => {
  const { selection, hidden: sessionHidden, rects } = file.value;
  const hidden = withDescendants(elements.value, list.value ? hiddenOf(list.value, file.value) : new Set<number>());
  marks.set([
    ...blocks.value.flatMap(({ element, range }) => [
      ...(element === selection
        ? [{ range: linesOf(range), options: { isWholeLine: true, className: "pmt-marked-widget" } }]
        : []),
      ...(hidden.has(element) ? [{ range: linesOf(range), options: { inlineClassName: "pmt-hidden-widget" } }] : []),
    ]),
    ...[...firstLines.value].flatMap(([line, lineElements]) => {
      const isOff = lineElements.every((element) => sessionHidden.has(element));
      const copies = rects.get(lineElements[0] ?? -1) ?? [];
      const box = copies[0];
      const size =
        box &&
        `${Math.round(box.width)} × ${Math.round(box.height)}${copies.length > 1 ? `, ${copies.length} rows` : ""}`;
      return [
        {
          range: new monaco.Range(line + 1, 1, line + 1, 1),
          options: {
            glyphMarginClassName: isOff ? "codicon codicon-eye-closed pmt-eye-off" : "codicon codicon-eye pmt-eye",
          },
        },
        ...(box && size
          ? [lineEnd(line + 1, size, box.width && box.height ? "pmt-size" : "pmt-size pmt-size-empty")]
          : []),
      ];
    }),
  ]);
});

// what follows the pointer: the hovered widget's lines unless they are the selected one's
const pointer = codeEditor.createDecorationsCollection();
watchEffect(() => {
  const { hover, selection } = file.value;
  const block = hover === selection ? undefined : blocks.value.find(({ element }) => element === hover);
  pointer.set(
    block ? [{ range: linesOf(block.range), options: { isWholeLine: true, className: "pmt-hovered-widget" } }] : [],
  );
});

// what the game's files did not give, squiggled on the first line of the block of the element that asked for it
watchEffect(() => {
  const starts = new Map(blocks.value.map(({ element, range }) => [element, range.start]));
  const markers = (list.value?.missing ?? []).flatMap(({ text, element }) => {
    const start = starts.get(element);
    if (!start) return [];
    const { startLineNumber, startColumn } = toRange(model, { start, end: start });
    const endColumn = model.getLineMaxColumn(startLineNumber);
    const severity = monaco.MarkerSeverity.Warning;
    return [{ severity, message: text, startLineNumber, startColumn, endLineNumber: startLineNumber, endColumn }];
  });
  monaco.editor.setModelMarkers(model, MISSING, markers);
});

/** Makes an element the picture's hovered one, from this code. */
const hover = (element: number) => (file.value.hover = element);

// the pointer on a widget's code, or on its eye, is the picture's hovered widget; a click on an eye hides or shows the
// widgets whose blocks start on its line
codeEditor.updateOptions({ glyphMargin: true });
const listeners = [
  codeEditor.onMouseMove(({ target: { type, position } }) => {
    if (!position) hover(-1);
    else
      hover(
        type === GLYPHS
          ? (firstLines.value.get(position.lineNumber - 1)?.[0] ?? -1)
          : elementAt(...bytesAt(model, position)),
      );
  }),
  codeEditor.onMouseLeave(() => hover(-1)),
  codeEditor.onMouseDown(({ target: { type, position } }) => {
    const lineElements = type === GLYPHS && position ? firstLines.value.get(position.lineNumber - 1) : undefined;
    if (lineElements) store.toggleHidden(path, lineElements);
  }),
];

onScopeDispose(() => {
  for (const listener of listeners) listener.dispose();
  hover(-1);
  file.value.caret = null;
  monaco.editor.setModelMarkers(model, MISSING, []);
  // an editor closed first took its marks with it
  if (!codeEditor.getModel()) return;
  marks.clear();
  pointer.clear();
  codeEditor.updateOptions({ glyphMargin: false });
});
</script>

<template>
  <span class="hidden" />
</template>
