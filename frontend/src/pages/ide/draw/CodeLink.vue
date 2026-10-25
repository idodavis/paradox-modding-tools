<!--
  The code beside the picture. It marks the selected and hovered widgets' lines, or those of the nearest widget above
  them written in this file, shows the drawn size on each widget's first line, flags what the game's files did not
  give, outlines the widget under the pointer in the picture, and scrolls to a selection.
-->
<script setup lang="ts">
import { refDebounced } from "@vueuse/core";
import { editor as monacoEditor, MarkerSeverity, Range } from "monaco-editor";
import type { IDisposable, IPosition, IRange } from "monaco-editor";
import { computed, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch, watchEffect } from "vue";

import type { Widget } from "@services/draw/models";
import type { Place } from "@services/workspace/models";

import { useDrawStore } from "../../../stores/draw";
import type { Model } from "../editor/models";

const { model, blocks, boxes, hidden, missing, name } = defineProps<{
  model: Model;
  /** Every widget or layer of the drawable, with the place its block is written. */
  blocks: (Pick<Widget, "id" | "parent" | "range"> & { place: Place })[];
  /** The boxes each one covers in the picture. */
  boxes: Map<number, DOMRect[]>;
  /** The ones hidden, by the session or by the states. */
  hidden: ReadonlySet<number>;
  /** What the game's files did not give for each one. */
  missing: { id: number; text: string }[];
  /** The name of the drawable. */
  name: string;
}>();

const MISSING = "pmt.draw";

const draw = useDrawStore();
const mark = useTemplateRef("mark");
const file = computed(() => draw.file(model.place.path));

const own = computed(() => blocks.filter(({ place }) => place.key === model.place.key));
// one eye hides every widget whose block starts on its line
const firstLines = computed(() => Map.groupBy(own.value, ({ range }) => range.startLineNumber));
const dimmed = computed(() => {
  const all = new Set<number>();
  for (const { id, parent } of blocks) if (hidden.has(id) || (id !== 0 && all.has(parent))) all.add(id);
  return all;
});

/** The lines of a block, whole. */
const toLines = (range: IRange) => new Range(range.startLineNumber, 1, range.endLineNumber, 1);

/** The block of a widget written in this file, else of the nearest widget above it that is, or undefined for none. */
function findBlock(id: number | null): (typeof blocks)[number] | undefined {
  for (let at = id; at !== null && at >= 0; at = at === 0 ? null : (blocks[at]?.parent ?? null)) {
    if (blocks[at]?.place.key === model.place.key) return blocks[at];
  }
  return undefined;
}

/** The deepest widget whose block holds a position, or undefined for none. */
const findWidget = (position: IPosition) =>
  own.value.findLast(({ range }) => Range.containsPosition(range, position))?.id;

/** The widgets whose eye the pointer is on, or undefined when it is on no eye. */
const findEye = ({ type, position }: monacoEditor.IMouseTarget) =>
  type === monacoEditor.MouseTargetType.GUTTER_GLYPH_MARGIN
    ? firstLines.value.get(position?.lineNumber ?? 0)
    : undefined;

const selectedBlock = computed(() => findBlock(file.value.selected));
const marks = computed<monacoEditor.IModelDeltaDecoration[]>(() => [
  ...own.value.flatMap(({ id, range }) => [
    ...(id === selectedBlock.value?.id
      ? [{ range: toLines(range), options: { isWholeLine: true, className: "pmt-marked-widget" } }]
      : []),
    ...(dimmed.value.has(id) ? [{ range: toLines(range), options: { inlineClassName: "pmt-hidden-widget" } }] : []),
  ]),
  ...[...firstLines.value].flatMap(([line, onLine]) => {
    const copies = boxes.get(onLine[0].id) ?? [];
    const box = copies[0];
    const isOff = onLine.every(({ id }) => file.value.hidden.includes(id));
    const end = model.model.getLineMaxColumn(line);
    return [
      {
        range: new Range(line, 1, line, 1),
        options: {
          glyphMarginClassName: isOff ? "codicon codicon-eye-closed pmt-eye-off" : "codicon codicon-eye pmt-eye",
        },
      },
      ...(box
        ? [
            {
              range: new Range(line, end, line, end),
              options: {
                showIfCollapsed: true,
                after: {
                  content: `${Math.round(box.width)} × ${Math.round(box.height)}${copies.length > 1 ? `, ${copies.length} rows` : ""}`,
                  inlineClassName: box.width && box.height ? "pmt-size" : "pmt-size pmt-size-empty",
                },
              },
            },
          ]
        : []),
    ];
  }),
]);
const pointer = computed<monacoEditor.IModelDeltaDecoration[]>(() => {
  const hoveredBlock = findBlock(file.value.hovered);
  const block = hoveredBlock?.id === selectedBlock.value?.id ? undefined : hoveredBlock;
  return block
    ? [{ range: toLines(block.range), options: { isWholeLine: true, className: "pmt-hovered-widget" } }]
    : [];
});

watchEffect(() => {
  const ranges = new Map(own.value.map(({ id, range }) => [id, range]));
  const markers = missing.flatMap(({ id, text }) => {
    const range = ranges.get(id);
    if (!range) return [];
    const line = range.startLineNumber;
    return [
      {
        severity: MarkerSeverity.Warning,
        message: text,
        startLineNumber: line,
        startColumn: range.startColumn,
        endLineNumber: line,
        endColumn: model.model.getLineMaxColumn(line),
      },
    ];
  });
  monacoEditor.setModelMarkers(model.model, MISSING, markers);
});

const findEditor = () =>
  monacoEditor
    .getEditors()
    .find((one) => one.getModel() === model.model && mark.value?.parentElement?.contains(one.getContainerDomNode())) ??
  null;
const codeEditor = shallowRef(findEditor());
const listeners: IDisposable[] = [
  monacoEditor.onDidCreateEditor((created) => {
    codeEditor.value = findEditor();
    listeners.push(created.onDidChangeModel(() => (codeEditor.value = findEditor())));
  }),
];
onMounted(() => (codeEditor.value = findEditor()));
onBeforeUnmount(() => {
  listeners.forEach((listener) => listener.dispose());
  monacoEditor.setModelMarkers(model.model, MISSING, []);
});

watch(
  codeEditor,
  (editor, _previous, onCleanup) => {
    if (!editor) return;
    editor.updateOptions({ glyphMargin: true });
    const markCollection = editor.createDecorationsCollection();
    const pointerCollection = editor.createDecorationsCollection();
    const stop = watchEffect(() => {
      markCollection.set(marks.value);
      pointerCollection.set(pointer.value);
    });
    const subscriptions = [
      editor.onMouseMove(({ target }) => {
        const onLine = findEye(target);
        draw.hover(
          model.place.path,
          onLine?.[0].id ?? (target.position ? (findWidget(target.position) ?? null) : null),
        );
      }),
      editor.onMouseLeave(() => draw.hover(model.place.path, null)),
      editor.onMouseDown(({ target }) => {
        const onLine = findEye(target) ?? [];
        const isOff = onLine.every(({ id }) => file.value.hidden.includes(id));
        for (const { id } of onLine) draw.hide(model.place.path, id, !isOff);
      }),
    ];
    onCleanup(() => {
      stop();
      subscriptions.forEach((subscription) => subscription.dispose());
      draw.hover(model.place.path, null);
      if (!editor.getModel()) return;
      markCollection.clear();
      pointerCollection.clear();
      editor.updateOptions({ glyphMargin: false });
    });
  },
  { immediate: true },
);

watch(selectedBlock, (block) => {
  const caret = codeEditor.value?.getPosition();
  if (block && !(caret && Range.containsPosition(block.range, caret))) {
    codeEditor.value?.revealLineInCenterIfOutsideViewport(block.range.startLineNumber);
  }
});

const caret = refDebounced(
  computed(() => ({ lineNumber: model.caret.line, column: model.caret.column })),
  300,
);
let isSwitching = false;

/** Selects the widget the caret is in, when it differs from the selected one. */
function selectCaret(): void {
  const id = findWidget(caret.value);
  if (id !== undefined && id !== file.value.selected) draw.select(model.place.path, id);
}

watch(caret, selectCaret);
watch(
  () => name,
  () => (isSwitching = true),
);
// the widget at the caret is selected once the blocks of a newly drawn drawable arrive
watch(own, () => {
  if (!isSwitching || !own.value.length) return;
  isSwitching = false;
  selectCaret();
});
</script>

<template>
  <span ref="mark" class="hidden" />
</template>
