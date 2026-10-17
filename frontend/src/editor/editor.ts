/**
 * An editor over a file's model: its options from the settings, the hover card's timing and place, the caret
 * reported to the store, and the palette's keys inside Monaco. `installEditor` sets the editor library up once per
 * window: the providers, the conflicts, the files, the IDE's commands and the font measuring.
 */
import { shallowRef } from "vue";
import type { Router } from "vue-router";
import * as rpc from "@services/ideservice";
import type { Inspect } from "@services/internal/lsp";
import { monaco } from "../lib/monaco";
import { useCommandsStore } from "../stores/commands";
import { useEditorStore } from "../stores/editor";
import { useSettingsStore } from "../stores/settings";
import { installIdeCommands } from "./commands";
import { installConflicts } from "./conflicts";
import { bytesAt, flushed, installModels, pathOf, workspaceId } from "./models";
import { cardSaysSomething, categoryAt, installProviders } from "./providers";

let installed = false;

/** Set the editor library up, once per window: the page that shows the IDE calls it in its setup. */
export function installEditor(router: Router): void {
  if (installed) return;
  installed = true;
  installProviders();
  installConflicts();
  installModels();
  installIdeCommands(router);

  // the app's palette keys open the palette in an editor too, where Monaco would take them
  const palette = useCommandsStore();
  monaco.editor.registerCommand("pmt.palette", () => palette.openPalette());
  monaco.editor.registerCommand("pmt.files", () => palette.openPalette(""));
  monaco.editor.addKeybindingRules([
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyK, command: "pmt.palette" },
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyP, command: "pmt.palette" },
    { keybinding: monaco.KeyCode.F1, command: "pmt.palette" },
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyP, command: "pmt.files" },
  ]);

  // a web font measured before it loaded leaves the carets and squiggles short of its letters; the editor's own text
  // is what asks for Geist Mono, after Monaco was set up, so measure again whenever fonts finish loading
  document.fonts.addEventListener("loadingdone", () => monaco.editor.remeasureFonts());
}

/** What the IDE's settings set in its editors. */
export function editorOptions(): monaco.editor.IStandaloneEditorConstructionOptions {
  const settings = useSettingsStore();
  const on = settings.on;
  return {
    ...settings.codeFont,
    wordWrap: on("ide.wordWrap") ? "on" : "off",
    autoClosingBrackets: on("ide.closeBrackets") ? "languageDefined" : "never",
    autoClosingQuotes: on("ide.closeBrackets") ? "languageDefined" : "never",
    quickSuggestions: on("ide.complete") ? { other: true, comments: false, strings: true } : false,
    inlineSuggest: { enabled: on("ide.predict") },
    parameterHints: { enabled: on("ide.signature") },
    minimap: { enabled: on("ide.minimap") },
    folding: on("ide.folding"),
    guides: { indentation: on("ide.indentGuides"), bracketPairs: false },
    bracketPairColorization: { enabled: on("ide.bracketColors") },
    renderWhitespace: on("ide.whitespace") ? "all" : "selection",
  };
}

// the editor last typed in, which the palette's commands run in
let lastEditor: monaco.editor.IStandaloneCodeEditor | undefined;

/** Run one of Monaco's actions in the editor last typed in. */
export function runAction(id: string): void {
  lastEditor?.focus();
  lastEditor?.trigger("palette", id, null);
}

/** An editor of a file's model in a host, the hover card's content and place in its `hover`. */
export function createEditor(host: HTMLElement, model: monaco.editor.ITextModel) {
  const settings = useSettingsStore();
  const editorStore = useEditorStore();
  const editor = monaco.editor.create(host, {
    model,
    ...editorOptions(),
    automaticLayout: true,
    contextmenu: false,
    hover: { enabled: "off" },
    fixedOverflowWidgets: true,
    wordBasedSuggestions: "off",
    suggest: { showIcons: false, preview: true },
    unicodeHighlight: { ambiguousCharacters: false, invisibleCharacters: false },
    scrollBeyondLastLine: false,
    padding: { top: 6 },
    lineNumbersMinChars: 4,
    glyphMargin: false,
    renderLineHighlight: "all",
  });
  const hover = shallowRef<{
    card: Inspect | null;
    problems: monaco.editor.IMarker[];
    anchor: DOMRect;
    category?: string;
  } | null>(null);
  let showTimer: number | undefined;
  let hideTimer: number | undefined;
  let hoveredPosition: monaco.IPosition | null = null;
  // the pointer is on the card: Monaco reads its first move there as leaving the editor, which must not close it
  let pointerOnCard = false;
  // the editor's menu is open: it opens at the pointer, whose first moves toward it are still on the word
  let menuOpen = false;

  /** Let the card go a moment after the pointer leaves its word, unless it is on the card by then. */
  function hideSoon(): void {
    clearTimeout(hideTimer);
    hideTimer = window.setTimeout(() => pointerOnCard || (hover.value = null), 300);
  }
  /** Keep the card open while the pointer is on it; let it go a moment after it leaves. */
  function holdHover(hold: boolean): void {
    pointerOnCard = hold;
    clearTimeout(showTimer);
    if (!hold) hideSoon();
  }

  async function showHover(position: monaco.IPosition): Promise<void> {
    const word = model.getWordAtPosition(position);
    const start =
      word && editor.getScrolledVisiblePosition({ lineNumber: position.lineNumber, column: word.startColumn });
    const end = word && editor.getScrolledVisiblePosition({ lineNumber: position.lineNumber, column: word.endColumn });
    if (!start || !end) return;
    await flushed(model);
    if (model.isDisposed()) return;
    const [line, col] = bytesAt(model, position);
    const card = await rpc.Hover(workspaceId, pathOf(model), line, col);
    if (hoveredPosition !== position) return;
    // a problem shows where the backend has no card for the word (plain text in a localization value)
    const problems = monaco.editor
      .getModelMarkers({ resource: model.uri })
      .filter((marker) => monaco.Range.containsPosition(marker, position));
    const box = host.getBoundingClientRect();
    const anchor = new DOMRect(
      box.left + start.left,
      box.top + start.top,
      Math.max(end.left - start.left, 1),
      start.height,
    );
    hover.value =
      (card && cardSaysSomething(card)) || problems.length
        ? { card, problems, anchor, category: categoryAt(model, position) }
        : null;
  }

  // the card shows when the pointer rests on a word for half a second, and goes when it leaves the word; a card
  // shown stays while the pointer is back on its word
  editor.onMouseMove((event) => {
    const position = event.target.type === monaco.editor.MouseTargetType.CONTENT_TEXT ? event.target.position : null;
    const word = position && model.getWordAtPosition(position);
    const sameWord =
      hoveredPosition &&
      word &&
      hoveredPosition.lineNumber === position.lineNumber &&
      model.getWordAtPosition(hoveredPosition)?.startColumn === word.startColumn;
    if (sameWord && hover.value) return clearTimeout(hideTimer);
    clearTimeout(showTimer);
    hoveredPosition = word ? position : null;
    hideSoon();
    const target = hoveredPosition;
    if (target && !menuOpen && settings.on("ide.hover"))
      showTimer = window.setTimeout(() => void showHover(target), 500);
  });
  /** Close the card and forget the word, so none shows until the pointer moves again. */
  function closeHover(): void {
    clearTimeout(showTimer);
    hoveredPosition = null;
    pointerOnCard = false;
    hover.value = null;
  }
  /** Close the card, and while the editor's menu is open show none. */
  function muteHover(mute: boolean): void {
    menuOpen = mute;
    closeHover();
  }
  // any press closes the card
  editor.onMouseDown(closeHover);
  editor.onKeyDown(closeHover);
  editor.onDidScrollChange(closeHover);
  editor.onMouseLeave(hideSoon);
  /** Tell the IDE where this editor's caret is. */
  function reportCaret(): void {
    const position = editor.getPosition();
    if (!position) return;
    const [, col] = bytesAt(model, position);
    editorStore.caret = { line: position.lineNumber - 1, character: position.column - 1, col };
  }
  editor.onDidFocusEditorText(() => {
    lastEditor = editor;
    reportCaret();
  });
  // a card about to show goes with its editor (its tab closed, or moved to another window)
  editor.onDidDispose(() => {
    if (lastEditor === editor) lastEditor = undefined;
    clearTimeout(showTimer);
    clearTimeout(hideTimer);
  });
  editor.onDidChangeCursorPosition(() => editor.hasTextFocus() && reportCaret());
  return { editor, hover, holdHover, muteHover, reportCaret };
}
