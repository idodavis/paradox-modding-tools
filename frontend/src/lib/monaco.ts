/**
 * Monaco's setup, run when the module loads: its worker, the Paradox languages and Markdown, the services a peek
 * reaches files through, and the PMT theme on the page's tokens, one Monaco theme per page theme.
 */
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/features/register.all";
// Markdown, the one language of Monaco's own a mod has (its Workshop description, readme)
import "monaco-editor/languages/definitions/markdown/register";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import { LANGUAGES, PAIRS, TOKENS } from "./grammars";

export { monaco };

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

/** What the editor plugs in: a file's model, made when a peek or a definition reaches a file no editor has open,
 * and a folder's name in a peek's list and title, when the IDE knows whose it is (else its path). */
export const hooks: {
  loadModel: (uri: monaco.Uri) => Promise<monaco.editor.ITextModel | null>;
  folderLabel: (uri: monaco.Uri) => string | undefined;
} = {
  loadModel: async () => null,
  folderLabel: () => undefined,
};

// Monaco makes its services once, with the first editor: this one, so the files it reaches are read on demand
monaco.editor
  .create(
    document.createElement("div"),
    {},
    {
      textModelService: {
        async createModelReference(uri: monaco.Uri) {
          const model = monaco.editor.getModel(uri) ?? (await hooks.loadModel(uri));
          if (!model) throw new Error(`${uri.fsPath} could not be read`);
          return { object: { textEditorModel: model }, dispose() {} };
        },
      },
      labelService: {
        getUriLabel: (uri: monaco.Uri) => hooks.folderLabel(uri) ?? (uri.scheme === "file" ? uri.fsPath : uri.path),
        getUriBasenameLabel: (uri: monaco.Uri) => uri.path.slice(uri.path.lastIndexOf("/") + 1),
      },
    },
  )
  .dispose();

for (const [id, extensions, grammar] of LANGUAGES) {
  monaco.languages.register({ id, extensions });
  monaco.languages.setMonarchTokensProvider(id, grammar);
  monaco.languages.setLanguageConfiguration(id, {
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
    // a dotted id, a saved scope and a loc key's $ are one word
    wordPattern: id === "paradox-loc" ? /[\w$.-]+/ : /[\w.:@$-]+/,
  });
}

/** A file's model address; Monaco writes a path's slashes and its drive's letter one way, so Go's and the explorer's
 * spellings of a path are one model. */
export function fileUri(path: string): monaco.Uri {
  return monaco.Uri.file(path.replace(/[\\/]+$/, ""));
}

/** The language a file calls for: the engine's format for it (FileText.format) where the text came from Go, so the
 * editor and the engine never disagree; else its name's (read-only snippets): localization, GUI, script, plain text. */
export function languageOf(name: string, format?: string): string {
  const gui = name.toLowerCase().endsWith(".gui");
  if (format === "script") return gui ? "paradox-gui" : "paradox";
  if (format === "loc") return "paradox-loc";
  if (format) return name.toLowerCase().endsWith(".md") ? "markdown" : "plaintext";
  const extension = name.slice(name.lastIndexOf(".")).toLowerCase();
  return (
    LANGUAGES.find(([, extensions]) => extensions.includes(extension))?.[0] ??
    (extension === ".md" ? "markdown" : "plaintext")
  );
}

const definedThemes = new Set<string>();

/**
 * The theme on the page as Monaco's: the page's tokens read as colors (Monaco takes no CSS variables), one theme per
 * page theme, set again whenever the page's changes.
 */
function followTheme(): void {
  const page = document.documentElement.dataset.theme ?? "pmt-dark";
  const name = `pmt-${page}`;
  if (!definedThemes.has(name)) {
    definedThemes.add(name);
    const css = getComputedStyle(document.documentElement);
    const token = (key: string) => css.getPropertyValue(`--${key}`).trim();
    const [accent, green, red] = [token("accent"), token("green"), token("red")];
    // a theme may give its diffs a pair of their own (Pierre Soft takes Pierre's), else they are its green and red
    const [added, removed] = [token("diff-add") || green, token("diff-del") || red];
    const light = page.endsWith("-light");
    // where Monaco draws nothing: a token at no strength
    const clear = `${token("bg-0")}00`;
    monaco.editor.defineTheme(name, {
      base: light ? "vs" : "vs-dark",
      inherit: true,
      rules: [
        { token: "", foreground: token("fg-1") },
        ...Object.entries(TOKENS).map(([kind, { color, italic }]) => ({
          token: kind,
          foreground: token(color),
          fontStyle: italic ? "italic" : "",
        })),
      ],
      colors: {
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
        "editorBracketHighlight.foreground1": token("syn-kw"),
        "editorBracketHighlight.foreground2": token("syn-fn"),
        "editorBracketHighlight.foreground3": token("syn-num"),
        "editorBracketMatch.background": `${accent}26`,
        "editorBracketMatch.border": `${accent}80`,
        "editorError.foreground": red,
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
        "widget.shadow": `${token(light ? "fg-1" : "bg-0")}40`,
        "diffEditor.insertedTextBackground": `${added}33`,
        "diffEditor.removedTextBackground": `${removed}33`,
        "diffEditor.insertedLineBackground": `${added}1a`,
        "diffEditor.removedLineBackground": `${removed}1a`,
        "diffEditorGutter.insertedLineBackground": `${added}26`,
        "diffEditorGutter.removedLineBackground": `${removed}26`,
        "diffEditor.diagonalFill": token("line-1"),
        "diffEditor.unchangedRegionBackground": token("bg-1"),
        "diffEditor.unchangedRegionForeground": token("fg-3"),
      },
    });
  }
  monaco.editor.setTheme(name);
}

followTheme();
new MutationObserver(followTheme).observe(document.documentElement, {
  attributes: true,
  attributeFilter: ["data-theme"],
});
