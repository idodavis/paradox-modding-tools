/**
 * The hover card over an editor, in place of Monaco's own. When the pointer rests on a word or on a step of a data
 * call, its card and the problems on it show above it until the pointer moves away, so the lines below stay free to
 * hover and right-click. Any card the pointer can move onto is kept the same way.
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
  const { card: hover, close: closeCard, closeLater, keep, hold, isHeld } = useHeldCard<Hover>();
  // the position of the pointer a card is asked for, and what the answer on show is about
  let target: monaco.IPosition | null = null;
  let shown: monaco.IRange | null = null;

  /** close takes the card down at once and forgets where the pointer was. */
  const close = (): void => {
    closeCard();
    target = null;
    shown = null;
  };
  /**
   * findCard finds the card at the pointer in a word and the problems on it and shows them, unless the pointer has
   * moved on meanwhile. The card is about one step of a data call or one name, and sits over it. A card the pointer is
   * on is never replaced.
   */
  const findCard = useDebounceFn(async (word: editor.IWordAtPosition, pointer: monaco.IPosition) => {
    const card = await lsp.Hover(model.place.path, pointer.lineNumber, pointer.column);
    if (isHeld() || target?.lineNumber !== pointer.lineNumber || target.column !== pointer.column) return;
    const { lineNumber } = pointer;
    const span = card?.range ?? new monaco.Range(lineNumber, word.startColumn, lineNumber, word.endColumn);
    shown = span;
    const problems = model.problems.filter((problem) => monaco.Range.areIntersectingOrTouching(problem.range, span));
    const start = codeEditor.getScrolledVisiblePosition({ lineNumber: span.startLineNumber, column: span.startColumn });
    const end = codeEditor.getScrolledVisiblePosition({ lineNumber: span.endLineNumber, column: span.endColumn });
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
      if (position && word && shown && monaco.Range.containsPosition(shown, position)) {
        keep();
        return;
      }
      target = position && word ? position : null;
      shown = null;
      closeLater();
      if (position && word && settings.isOn("ide.hover")) void findCard(word, position);
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

/**
 * useHeldCard keeps a card that the pointer can move onto. It returns the card on show, which is null while there is
 * none, and the functions that take it down at once, take it down after a moment unless the pointer is on it by then,
 * keep it up when the pointer comes back to what it is about, hold it while the pointer is on it, and tell whether the
 * pointer is on it.
 */
export function useHeldCard<T>(): {
  card: ShallowRef<T | null>;
  close: () => void;
  closeLater: () => void;
  keep: () => void;
  hold: (on: boolean) => void;
  isHeld: () => boolean;
} {
  const card = shallowRef<T | null>(null);
  let onCard = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const close = (): void => {
    clearTimeout(timer);
    onCard = false;
    card.value = null;
  };
  const closeLater = (): void => {
    clearTimeout(timer);
    timer = setTimeout(() => onCard || (card.value = null), 300);
  };
  const hold = (on: boolean): void => {
    onCard = on;
    if (!on) closeLater();
  };
  return { card, close, closeLater, keep: () => clearTimeout(timer), hold, isHeld: () => onCard };
}
