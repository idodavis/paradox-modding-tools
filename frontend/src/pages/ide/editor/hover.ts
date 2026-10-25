/**
 * The hover card over an editor. Monaco's own hover is off. When the pointer rests on a word, the card of the word and
 * the problems on it show above it, as VS Code's do, until the pointer moves away. The lines below stay free to hover
 * and right-click.
 */
import UPopover from "@nuxt/ui/components/Popover.vue";
import { useDebounceFn } from "@vueuse/core";
import * as monaco from "monaco-editor/editor/editor.api";
import {
  getCurrentInstance,
  h,
  render,
  shallowRef,
  type AppContext,
  type FunctionalComponent,
  type ShallowRef,
} from "vue";

import * as lsp from "@services/lsp/service";
import type { Card } from "@services/lsp/models";
import type { editor } from "monaco-editor";
import HoverCard from "../../../ui/HoverCard.vue";
import { useSettingsStore } from "../../../stores/settings";

/** A card on show: the facts of a word, the problems on it, and the rectangle of the word in the page. */
type Hover = { card: Card | null; problems: editor.IMarker[]; anchor: DOMRect };

/** Shows a hover card over the word the pointer rests on in an editor. It returns the function that detaches it. */
export function attachHover(codeEditor: editor.IStandaloneCodeEditor): () => void {
  const settings = useSettingsStore();
  const hover = shallowRef<Hover | null>(null);
  // the start of the word the pointer is on, and whether the pointer is on the card
  let target: monaco.IPosition | null = null;
  let onCard = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  /** Takes the card down at once and forgets the word. */
  const closeCard = (): void => {
    clearTimeout(timer);
    target = null;
    onCard = false;
    hover.value = null;
  };
  /** Takes the card down after a moment, unless the pointer is on it by then. */
  const closeCardLater = (): void => {
    clearTimeout(timer);
    timer = setTimeout(() => onCard || (hover.value = null), 300);
  };
  /** Finds the card of a word and the problems on it and shows them, unless the pointer has moved on meanwhile. */
  const findCard = useDebounceFn(async (word: editor.IWordAtPosition, lineNumber: number) => {
    const model = codeEditor.getModel();
    const card = model && (await lsp.Hover(model.uri.fsPath, lineNumber, word.startColumn));
    if (!model || target?.lineNumber !== lineNumber || target.column !== word.startColumn) return;
    const span = card?.range ?? new monaco.Range(lineNumber, word.startColumn, lineNumber, word.endColumn);
    const problems = monaco.editor
      .getModelMarkers({ resource: model.uri })
      .filter((marker) => monaco.Range.areIntersectingOrTouching(marker, span));
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
      closeCardLater();
      if (position && word && settings.value("ide.hover") === "true") void findCard(word, position.lineNumber);
    }),
    codeEditor.onMouseDown(closeCard),
    codeEditor.onContextMenu(closeCard),
    codeEditor.onKeyDown(closeCard),
    codeEditor.onDidScrollChange(closeCard),
    codeEditor.onMouseLeave(closeCardLater),
  ];
  const detachCard = attachCard(
    hover,
    {
      close: closeCard,
      hold: (on) => {
        onCard = on;
        if (!on) closeCardLater();
      },
    },
    getCurrentInstance()?.appContext,
  );

  return () => {
    subscriptions.forEach((subscription) => subscription.dispose());
    closeCard();
    detachCard();
  };
}

/**
 * Draws the card in a popover against the word's rectangle, in the given app. It returns the function that takes the
 * popover away.
 */
function attachCard(
  hover: ShallowRef<Hover | null>,
  pointer: { close: () => void; hold: (on: boolean) => void },
  appContext?: AppContext,
): () => void {
  const reference = { getBoundingClientRect: () => hover.value?.anchor ?? new DOMRect() };
  const Popover: FunctionalComponent = () =>
    h(
      UPopover,
      {
        open: !!hover.value,
        reference,
        content: {
          side: "top",
          align: "start",
          onOpenAutoFocus: (event: Event) => event.preventDefault(),
          onCloseAutoFocus: (event: Event) => event.preventDefault(),
        },
        "onUpdate:open": (open: boolean) => open || pointer.close(),
      },
      {
        // never taller than the room beside the word: a long card scrolls inside itself
        content: () =>
          hover.value &&
          h(
            "div",
            {
              class: "max-h-(--reka-popover-content-available-height) overflow-y-auto",
              onMouseenter: () => pointer.hold(true),
              onMouseleave: () => pointer.hold(false),
            },
            h(HoverCard, { card: hover.value.card, problems: hover.value.problems }),
          ),
      },
    );
  const host = document.createElement("div");
  const vnode = h(Popover);
  vnode.appContext = appContext ?? null;
  render(vnode, host);
  return () => render(null, host);
}
