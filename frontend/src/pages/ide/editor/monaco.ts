/**
 * Monaco's setup, done once: the Paradox languages with their grammars, the providers over the editor service,
 * one theme per page theme made when that theme comes into force, with its token colors read from the theme's
 * `--syn-<category>` variables, and the editor options the settings give.
 */
import { useEventBus } from "@vueuse/core";
import Color from "colorjs.io";
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/features/register.all";
// the one editor feature register.all leaves out: asking a provider for a model's semantic tokens
import "monaco-editor/editor/contrib/semanticTokens/browser/documentSemanticTokens";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import { watch } from "vue";

import type { editor, languages } from "monaco-editor";
import { legendQuery, loadQuery, placeQuery } from "../../../queries";
import { useSettingsStore } from "../../../stores/settings";
import { registerConflicts } from "./conflicts";
import { LOCALIZATION, SCRIPT } from "./grammars";
import { openModel, setupModels } from "./models";
import { registerProviders } from "./providers";

// each language with its grammar and its characters of a word
const LANGUAGES = [
  { id: "paradox-script", grammar: SCRIPT, word: /[\w.:@$-]+/ },
  { id: "paradox-gui", grammar: SCRIPT, word: /[\w.:@$-]+/ },
  { id: "paradox-localization", grammar: LOCALIZATION, word: /[\w$.-]+/ },
];
// the pairs typed together, and what every language shares of its configuration
const PAIRS = [
  { open: "{", close: "}" },
  { open: "[", close: "]" },
  { open: "(", close: ")" },
  { open: '"', close: '"', notIn: ["string", "comment"] },
];
const CONFIGURATION: languages.LanguageConfiguration = {
  comments: { lineComment: "#" },
  brackets: [
    ["{", "}"],
    ["[", "]"],
    ["(", ")"],
  ],
  autoClosingPairs: PAIRS,
  surroundingPairs: PAIRS,
  colorizedBracketPairs: [
    ["{", "}"],
    ["[", "]"],
  ],
};

// each token the grammars and the legend name, with the `--syn-<token>` of the themes that colors it
const SYNTAX: Record<string, string> = {
  comment: "comment",
  string: "string",
  number: "number",
  literal: "literal",
  operator: "operator",
  block: "block",
  field: "field",
  entry: "entry",
  variable: "variable",
  scope: "scope",
  keyword: "operator",
  command: "field",
  definition: "entry",
  reference: "entry",
  link: "scope",
  call: "scope",
  parameter: "variable",
  constant: "variable",
};

const defined = new Set<string>();

/**
 * Sets Monaco up, once, before the first editor: its worker and the text model service, the languages, the palette's
 * keys inside an editor, the providers and the conflict links.
 */
export function setupMonaco(): void {
  self.MonacoEnvironment = { getWorker: () => new EditorWorker() };
  // Monaco takes its services from the first editor made, so a throwaway one carries the text model service
  monaco.editor
    .create(
      document.createElement("div"),
      {},
      {
        textModelService: {
          createModelReference: async (uri: monaco.Uri) => {
            const place = await loadQuery(placeQuery(uri.fsPath));
            if (!place) throw new Error(`${uri.fsPath} could not be read`);
            return { object: { textEditorModel: (await openModel(place)).model }, dispose: () => undefined };
          },
        },
      },
    )
    .dispose();
  for (const { id, grammar, word } of LANGUAGES) {
    monaco.languages.register({ id });
    monaco.languages.setMonarchTokensProvider(id, grammar);
    monaco.languages.setLanguageConfiguration(id, { ...CONFIGURATION, wordPattern: word });
  }
  const bus = useEventBus<string>("pmt.command");
  monaco.editor.registerCommand("pmt.palette", () => bus.emit("palette.open"));
  monaco.editor.registerCommand("pmt.files", () => bus.emit("editor.goToFile"));
  monaco.editor.addKeybindingRules([
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyK, command: "pmt.palette" },
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyP, command: "pmt.palette" },
    { keybinding: monaco.KeyCode.F1, command: "pmt.palette" },
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyP, command: "pmt.files" },
  ]);
  document.fonts.addEventListener("loadingdone", () => monaco.editor.remeasureFonts());
  registerConflicts();
  setupModels(useToast());
  const settings = useSettingsStore();
  watch(
    () => settings.theme(),
    ({ family, dark }) => applyTheme(family, dark),
    { immediate: true },
  );
  void loadQuery(legendQuery).then((legend) => {
    const ids = LANGUAGES.map(({ id }) => id);
    registerProviders(ids, { tokenTypes: legend?.tokenTypes ?? [], tokenModifiers: legend?.tokenModifiers ?? [] });
  });
}

/** Makes Monaco's theme for the page theme in force, if it does not exist yet, applies it and returns its id. */
export function applyTheme(family: string, dark: boolean): string {
  const id = `pmt-${family}-${dark ? "dark" : "light"}`;
  if (!defined.has(id)) {
    defined.add(id);
    monaco.editor.defineTheme(id, makeTheme(`${family}-${dark ? "dark" : "light"}`, dark));
  }
  monaco.editor.setTheme(id);
  return id;
}

/** The editor options from the settings: text size, indentation, the checks. */
export function editorOptions(): editor.IStandaloneEditorConstructionOptions {
  const settings = useSettingsStore();
  const on = (key: string) => settings.value(key) === "true";
  return {
    ...settings.codeFont(),
    tabSize: Number(settings.value("ide.indentation")),
    insertSpaces: settings.value("ide.indentWith") === "spaces",
    "semanticHighlighting.enabled": true,
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

/**
 * Makes a Monaco theme from the tokens of a page theme: the colors of the syntax and the editor's own. Monaco takes
 * no CSS variables, so the tokens are read from an element of that page theme.
 */
function makeTheme(pageTheme: string, dark: boolean): editor.IStandaloneThemeData {
  const probe = document.body.appendChild(document.createElement("div"));
  probe.dataset.theme = pageTheme;
  const style = getComputedStyle(probe);
  const token = (key: string): string => toHex(style.getPropertyValue(`--${key}`).trim());
  const rules = Object.entries(SYNTAX).map(([type, syntax]) => ({
    token: type,
    foreground: token(`syn-${syntax}`),
    fontStyle: type === "comment" ? "italic" : "",
  }));
  const theme: editor.IStandaloneThemeData = {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [{ token: "", foreground: token("fg-1") }, ...rules],
    colors: buildColors(token, dark),
  };
  probe.remove();
  return theme;
}

/** A CSS color as #rrggbb, whatever form the token is written in. It is "" for a token that is not set. */
function toHex(color: string): string {
  if (!color || color.startsWith("#")) return color;
  return new Color(color).to("srgb").toString({ format: "hex" });
}

/** The editor's colors in a theme, from the page's tokens. */
function buildColors(token: (key: string) => string, dark: boolean): Record<string, string> {
  const accent = token("accent");
  const clear = `${token("bg-0")}00`;
  // a theme may give its diffs a pair of their own, else they are its green and red
  const added = token("diff-add") || token("green");
  const removed = token("diff-del") || token("red");
  return {
    "editor.background": token("bg-0"),
    "editor.foreground": token("fg-1"),
    "editorGutter.background": token("bg-0"),
    "editorLineNumber.foreground": token("fg-3"),
    "editorLineNumber.activeForeground": token("fg-1"),
    "editorCursor.foreground": accent,
    "editor.lineHighlightBackground": token("bg-3"),
    "editor.lineHighlightBorder": clear,
    "editor.selectionBackground": `${accent}40`,
    "editor.inactiveSelectionBackground": `${accent}26`,
    "editor.selectionHighlightBackground": `${accent}1f`,
    "editor.wordHighlightBackground": `${token("fg-3")}33`,
    "editor.findMatchBackground": `${accent}66`,
    "editor.findMatchHighlightBackground": `${accent}33`,
    "editorIndentGuide.background1": token("line-1"),
    "editorIndentGuide.activeBackground1": token("line-2"),
    "editorWhitespace.foreground": token("line-2"),
    "editorBracketHighlight.foreground1": token("syn-block"),
    "editorBracketHighlight.foreground2": token("syn-entry"),
    "editorBracketHighlight.foreground3": token("syn-number"),
    "editorBracketMatch.background": `${accent}26`,
    "editorBracketMatch.border": `${accent}80`,
    "editorError.foreground": token("red"),
    "editorWarning.foreground": token("orange"),
    "editorInfo.foreground": token("blue"),
    "editorInlayHint.foreground": token("fg-3"),
    "editorInlayHint.background": clear,
    "editorGhostText.foreground": token("fg-3"),
    "editorLink.activeForeground": token("link"),
    "editorLightBulb.foreground": token("orange"),
    "editorOverviewRuler.border": clear,
    "editorStickyScroll.background": token("bg-0"),
    "editorStickyScrollHover.background": token("bg-3"),
    "editorWidget.background": token("bg-3"),
    "editorWidget.foreground": token("fg-1"),
    "editorWidget.border": token("line-2"),
    "editorSuggestWidget.background": token("bg-3"),
    "editorSuggestWidget.border": token("line-2"),
    "editorSuggestWidget.selectedBackground": `${accent}26`,
    "editorSuggestWidget.highlightForeground": accent,
    "editorSuggestWidget.focusHighlightForeground": accent,
    "peekView.border": accent,
    "peekViewTitle.background": token("bg-2"),
    "peekViewTitleLabel.foreground": token("fg-1"),
    "peekViewTitleDescription.foreground": token("fg-3"),
    "peekViewResult.background": token("bg-1"),
    "peekViewResult.fileForeground": token("fg-1"),
    "peekViewResult.lineForeground": token("fg-2"),
    "peekViewResult.selectionBackground": `${accent}26`,
    "peekViewResult.selectionForeground": token("fg-1"),
    "peekViewResult.matchHighlightBackground": `${accent}40`,
    "peekViewEditor.background": token("bg-0"),
    "peekViewEditorGutter.background": token("bg-0"),
    "peekViewEditor.matchHighlightBackground": `${accent}40`,
    "input.background": token("bg-0"),
    "input.border": token("line-2"),
    "input.foreground": token("fg-1"),
    "inputOption.activeBorder": accent,
    "inputOption.activeBackground": `${accent}26`,
    focusBorder: accent,
    "list.hoverBackground": token("bg-4"),
    "list.activeSelectionBackground": `${accent}26`,
    "list.activeSelectionForeground": token("fg-1"),
    "list.focusBackground": `${accent}26`,
    "list.highlightForeground": accent,
    "scrollbarSlider.background": `${token("line-2")}99`,
    "scrollbarSlider.hoverBackground": token("line-2"),
    "scrollbarSlider.activeBackground": token("fg-3"),
    "minimap.background": token("bg-0"),
    "widget.shadow": `${token(dark ? "bg-0" : "fg-1")}40`,
    "diffEditor.insertedTextBackground": `${added}33`,
    "diffEditor.removedTextBackground": `${removed}33`,
    "diffEditor.insertedLineBackground": `${added}1a`,
    "diffEditor.removedLineBackground": `${removed}1a`,
    "diffEditorGutter.insertedLineBackground": `${added}26`,
    "diffEditorGutter.removedLineBackground": `${removed}26`,
    "diffEditor.diagonalFill": token("line-1"),
    "diffEditor.unchangedRegionBackground": token("bg-1"),
    "diffEditor.unchangedRegionForeground": token("fg-3"),
  };
}
