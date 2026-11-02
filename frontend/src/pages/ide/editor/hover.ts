/**
 * The hover card over an editor, in place of Monaco's own. When the pointer rests on a word, the card of the word and
 * the problems on it show above it until the pointer moves away, so the lines below stay free to hover and right-click.
 */
import { useDebounceFn } from "@vueuse/core";
import * as monaco from "monaco-editor/editor/editor.api";
import { shallowRef, type ShallowRef } from "vue";

import * as lsp from "@services/lsp/service";
import type { Card, Diagnostic } from "@services/lsp/models";
import type { editor } from "monaco-editor";
import { useSettingsStore } from "../../../stores/settings";
import type { Model } from "./models";

/** A Hover is the card on show: the facts of a word, the problems on it, and the rectangle of the word in the page. */
type Hover = { card: Card | null; problems: Diagnostic[]; anchor: DOMRect };

/**
 * attachHover shows a hover card over the word the pointer rests on in an editor of a file's model, with the model's
 * problems on the word. It returns the card on show, which is null while there is none, the functions that take the
 * card down and hold it while the pointer is on it, and the function that detaches the card from the editor.
 */
export function attachHover(
  codeEditor: editor.IStandaloneCodeEditor,
  model: Model,
): {
  hover: ShallowRef<Hover | null>;
  close: () => void;
  hold: (on: boolean) => void;
  detach: () => void;
} {
  const settings = useSettingsStore();
  const hover = shallowRef<Hover | null>(null);
  // the start of the word the pointer is on, and whether the pointer is on the card
  let target: monaco.IPosition | null = null;
  let onCard = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  /** close takes the card down at once and forgets the word. */
  const close = (): void => {
    clearTimeout(timer);
    target = null;
    onCard = false;
    hover.value = null;
  };
  /** closeLater takes the card down after a moment, unless the pointer is on it by then. */
  const closeLater = (): void => {
    clearTimeout(timer);
    timer = setTimeout(() => onCard || (hover.value = null), 300);
  };
  /** hold holds the card while the pointer is on it, and takes it down after a moment once the pointer leaves it. */
  const hold = (on: boolean): void => {
    onCard = on;
    if (!on) closeLater();
  };
  /**
   * findCard finds the card of a word and the problems on it and shows them, unless the pointer has moved on
   * meanwhile. A card the pointer is on is never replaced.
   */
  const findCard = useDebounceFn(async (word: editor.IWordAtPosition, lineNumber: number) => {
    const card = await lsp.Hover(model.place.path, lineNumber, word.startColumn);
    if (onCard || target?.lineNumber !== lineNumber || target.column !== word.startColumn) return;
    const span = card?.range ?? new monaco.Range(lineNumber, word.startColumn, lineNumber, word.endColumn);
    const problems = model.problems.filter((problem) => monaco.Range.areIntersectingOrTouching(problem.range, span));
    const start = codeEditor.getScrolledVisiblePosition({ lineNumber, column: word.startColumn });
    const end = codeEditor.getScrolledVisiblePosition({ lineNumber, column: word.endColumn });
    const box = codeEditor.getDomNode()?.getBoundingClientRect();
    if ((!card && !problems.length) || !start || !end || !box) return;
    const width = Math.max(end.left - start.left, 1);
    hover.value = {
      card,
      problems,
      anchor: new DOMRect(box.left + start.left, box.top + start.top, width, start.height),
    };
  }, 300);

  const subscriptions = [
    codeEditor.onMouseMove((event) => {
      const position = event.target.type === monaco.editor.MouseTargetType.CONTENT_TEXT ? event.target.position : null;
      const word = position && codeEditor.getModel()?.getWordAtPosition(position);
      if (position && word && target?.lineNumber === position.lineNumber && target.column === word.startColumn) {
        clearTimeout(timer);
        return;
      }
      target = position && word ? { lineNumber: position.lineNumber, column: word.startColumn } : null;
      closeLater();
      if (position && word && settings.isOn("ide.hover")) void findCard(word, position.lineNumber);
    }),
    codeEditor.onMouseDown(close),
    codeEditor.onContextMenu(close),
    codeEditor.onKeyDown(close),
    codeEditor.onDidScrollChange(close),
    codeEditor.onMouseLeave(closeLater),
  ];

  return {
    hover,
    close,
    hold,
    detach: () => {
      subscriptions.forEach((subscription) => subscription.dispose());
      close();
    },
  };
}
