/** Monaco's setup, done once: the Paradox languages, their providers, the themes and the editor options. */
import { useEventBus } from "@vueuse/core";
import Color from "colorjs.io";
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/features/register.all";
import "monaco-editor/editor/contrib/semanticTokens/browser/viewportSemanticTokens";
import "monaco-editor/languages/definitions/markdown/register";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import { watch } from "vue";

import type { editor, languages } from "monaco-editor";
import { legendQuery, loadQuery, placeQuery } from "../../../queries";
import { useEditorStore } from "../../../stores/editor";
import { useSettingsStore } from "../../../stores/settings";
import { COMMAND_BUS } from "../../../stores/windows";
import { registerConflicts } from "./conflicts";
import { LOCALIZATION, SCRIPT } from "./grammars";
import { openModel, setupModels } from "./models";
import { labelService, registerProviders } from "./providers";

/** LANGUAGES lists each Paradox language with its label, grammar and word pattern. */
const LANGUAGES = [
  { id: "paradox-script", label: "Paradox Script", grammar: SCRIPT, word: /[\w.:@$-]+/ },
  { id: "paradox-gui", label: "Paradox GUI", grammar: SCRIPT, word: /[\w.:@$-]+/ },
  { id: "paradox-localization", label: "Paradox localization", grammar: LOCALIZATION, word: /[\w$.-]+/ },
  // a game's readme/info is prose around script examples: the script's colors, and Go's answers where an example
  // names something, though Go reports no problem in it
  { id: "paradox-readme", label: "Game readme/info", grammar: SCRIPT, word: /[\w.:@$-]+/ },
];
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

// Each token the grammars and the legend use, with the `--syn-<name>` theme variable that colors it.
const SYNTAX: Record<string, string> = {
  comment: "comment",
  string: "string",
  number: "number",
  literal: "number",
  choice: "number",
  operator: "operator",
  block: "field",
  field: "field",
  entry: "name",
  variable: "macro",
  scope: "command",
  keyword: "keyword",
  command: "command",
  definition: "name",
  reference: "name",
  link: "link",
  call: "command",
  parameter: "macro",
  constant: "macro",
};

const defined = new Set<string>();

/** setupMonaco sets Monaco up once, before the first editor is created. */
export function setupMonaco(): void {
  self.MonacoEnvironment = { getWorker: () => new EditorWorker() };
  // Monaco takes its services from the first editor created, so a throwaway editor passes in the app's own.
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
        labelService,
      },
    )
    .dispose();
  for (const { id, label, grammar, word } of LANGUAGES) {
    monaco.languages.register({ id, aliases: [label] });
    monaco.languages.setMonarchTokensProvider(id, grammar);
    monaco.languages.setLanguageConfiguration(id, { ...CONFIGURATION, wordPattern: word });
  }
  const commandBus = useEventBus(COMMAND_BUS);
  monaco.editor.registerCommand("pmt.palette", () => commandBus.emit("palette.open"));
  monaco.editor.registerCommand("pmt.files", () => commandBus.emit("editor.goToFile"));
  monaco.editor.registerCommand("pmt.back", () => useEditorStore().go(-1));
  monaco.editor.registerCommand("pmt.forward", () => useEditorStore().go(1));
  monaco.editor.addKeybindingRules([
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyK, command: "pmt.palette" },
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyP, command: "pmt.palette" },
    { keybinding: monaco.KeyCode.F1, command: "pmt.palette" },
    { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyP, command: "pmt.files" },
    { keybinding: monaco.KeyMod.Alt | monaco.KeyCode.LeftArrow, command: "pmt.back" },
    { keybinding: monaco.KeyMod.Alt | monaco.KeyCode.RightArrow, command: "pmt.forward" },
  ]);
  document.fonts.addEventListener("loadingdone", () => monaco.editor.remeasureFonts());
  registerConflicts();
  setupModels();
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

/** describeLanguage returns the name Monaco knows an editor language by, such as Paradox Script or Markdown. */
export function describeLanguage(id: string): string {
  return monaco.languages.getLanguages().find((language) => language.id === id)?.aliases?.[0] ?? "Plain text";
}

/** applyTheme applies Monaco's theme for the given page theme, defining the theme first if it is new. */
function applyTheme(family: string, dark: boolean): void {
  const id = `pmt-${family}-${dark ? "dark" : "light"}`;
  if (!defined.has(id)) {
    defined.add(id);
    monaco.editor.defineTheme(id, makeTheme(`${family}-${dark ? "dark" : "light"}`, dark));
  }
  monaco.editor.setTheme(id);
}

/** editorOptions returns the Monaco editor options built from the settings. */
export function editorOptions(): editor.IStandaloneEditorConstructionOptions {
  const settings = useSettingsStore();
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
    glyphMargin: false,
    // Without this Monaco underlines problems only in files that can be typed in.
    renderValidationDecorations: "on",
    renderLineHighlight: "all",
    wordWrap: settings.isOn("ide.wordWrap") ? "on" : "off",
    autoClosingBrackets: settings.isOn("ide.closeBrackets") ? "languageDefined" : "never",
    autoClosingQuotes: settings.isOn("ide.closeBrackets") ? "languageDefined" : "never",
    quickSuggestions: settings.isOn("ide.complete") ? { other: true, comments: false, strings: true } : false,
    inlineSuggest: { enabled: settings.isOn("ide.predict") },
    parameterHints: { enabled: settings.isOn("ide.signature") },
    minimap: { enabled: settings.isOn("ide.minimap") },
    folding: settings.isOn("ide.folding"),
    guides: { indentation: settings.isOn("ide.indentGuides"), bracketPairs: false },
    bracketPairColorization: { enabled: settings.isOn("ide.bracketColors") },
    renderWhitespace: settings.isOn("ide.whitespace") ? "all" : "selection",
  };
}

/**
 * makeTheme makes a Monaco theme from the given page theme's tokens. Monaco takes no CSS variables, so it reads them
 * from a temporary element under that page theme.
 */
function makeTheme(pageTheme: string, dark: boolean): editor.IStandaloneThemeData {
  const probe = document.body.appendChild(document.createElement("div"));
  probe.dataset.theme = pageTheme;
  const style = getComputedStyle(probe);
  const token = (key: string): string => {
    const color = style.getPropertyValue(`--${key}`).trim();
    return !color || color.startsWith("#") ? color : new Color(color).to("srgb").toString({ format: "hex" });
  };
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

/** buildColors builds the editor colors of a Monaco theme from the page theme's tokens. */
function buildColors(token: (key: string) => string, dark: boolean): Record<string, string> {
  const accent = token("accent");
  const clear = `${token("bg-0")}00`;
  // A page theme may define diff-add and diff-del; otherwise diffs use its green and red.
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
    "editorBracketHighlight.foreground1": token("syn-command"),
    "editorBracketHighlight.foreground2": token("syn-name"),
    "editorBracketHighlight.foreground3": token("syn-number"),
    "editorBracketMatch.background": `${accent}26`,
    "editorBracketMatch.border": `${accent}80`,
    "editorError.foreground": token("red"),
    "editorWarning.foreground": token("orange"),
    "editorInfo.foreground": token("blue"),
    "editorInlayHint.foreground": token("fg-3"),
    "editorInlayHint.background": new Color(token("bg-0"))
      .set("oklch.l", (lightness: number) => lightness - 0.04)
      .to("srgb")
      .toString({ format: "hex" }),
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
    "diffEditor.insertedLineBackground": `${added}26`,
    "diffEditor.removedLineBackground": `${removed}26`,
    "diffEditor.insertedTextBackground": `${added}4d`,
    "diffEditor.removedTextBackground": `${removed}4d`,
    "diffEditorGutter.insertedLineBackground": `${added}40`,
    "diffEditorGutter.removedLineBackground": `${removed}40`,
    "diffEditorOverview.insertedForeground": `${added}cc`,
    "diffEditorOverview.removedForeground": `${removed}cc`,
    "diffEditor.border": token("line-2"),
    "diffEditor.diagonalFill": token("line-2"),
    "diffEditor.unchangedRegionBackground": token("bg-2"),
    "diffEditor.unchangedRegionForeground": token("fg-2"),
    "diffEditor.unchangedCodeBackground": `${token("fg-3")}1a`,
    "diffEditor.move.border": token("blue"),
    "diffEditor.moveActive.border": accent,
    // The change bars' marks in the ruler (changes.ts).
    "pmt.changeAdded": added,
    "pmt.changeChanged": token("blue"),
    "pmt.changeRemoved": removed,
  };
}
