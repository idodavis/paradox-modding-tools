/**
 * Code, drawn by Monaco (`monaco-editor`): the Paradox languages and the PMT theme, for the IDE's editors, every
 * compare, and read-only code. Read-only code is Monaco's tokens drawn as the page's own elements, colored by the
 * page's tokens, so a theme change redraws nothing; an editor takes the theme on the page as Monaco's own colors.
 */
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/features/register.all";
// Markdown, the one language of Monaco's own a mod has (its Workshop description, readme)
import "monaco-editor/languages/definitions/markdown/register";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import { useLocalStorage } from "@vueuse/core";

export { monaco };

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

/** Every compare's split or unified view, one choice kept on this computer, and the tabs that pick it. */
export const compareStyle = useLocalStorage<"split" | "unified">("pmt.compare.style", "split");
export const COMPARE_STYLES = [
  { label: "Split", value: "split" },
  { label: "Unified", value: "unified" },
];

/** A file's model, made when a peek or a definition reaches a file no editor has open (ide/monacoEditor.ts). */
export const models: { load: (uri: monaco.Uri) => Promise<monaco.editor.ITextModel | null> } = {
  load: async () => null,
};
/** A folder's name in a peek's list and title, when the IDE knows whose it is (ide/monacoEditor.ts); else its path. */
export const labels: { folder: (uri: monaco.Uri) => string | undefined } = { folder: () => undefined };

// Monaco makes its services once, with the first editor: this one, so the files it reaches are read on demand
monaco.editor
  .create(
    document.createElement("div"),
    {},
    {
      textModelService: {
        async createModelReference(uri: monaco.Uri) {
          const model = monaco.editor.getModel(uri) ?? (await models.load(uri));
          if (!model) throw new Error(`${uri.fsPath} could not be read`);
          return { object: { textEditorModel: model }, dispose() {} };
        },
      },
      labelService: {
        getUriLabel: (uri: monaco.Uri) => labels.folder(uri) ?? (uri.scheme === "file" ? uri.fsPath : uri.path),
        getUriBasenameLabel: (uri: monaco.Uri) => uri.path.slice(uri.path.lastIndexOf("/") + 1),
      },
    },
  )
  .dispose();

/** A file's model address; Monaco writes a path's slashes and its drive's letter one way, so Go's and the explorer's
 * spellings of a path are one model. */
export function fileUri(path: string): monaco.Uri {
  return monaco.Uri.file(path.replace(/[\\/]+$/, ""));
}

/** Each kind of token and the page's token it is drawn in (styles/themes.css); none is the text's own color. */
const TOKENS: Record<string, { color: string; italic?: boolean }> = {
  comment: { color: "syn-com", italic: true },
  string: { color: "syn-str" },
  "string.format": { color: "syn-com" },
  "string.fn": { color: "syn-fn" },
  "string.ref": { color: "syn-ref" },
  "string.icon": { color: "syn-num" },
  keyword: { color: "syn-kw" },
  number: { color: "syn-num" },
  function: { color: "syn-fn" },
  variable: { color: "syn-ref" },
};

const ESCAPE: monaco.languages.IMonarchLanguageRule = [/\\./, "string"];

/** Paradox script: keys before `=`, the flow keywords and iterators, events, dates, saved scopes, constants. */
const SCRIPT: monaco.languages.IMonarchLanguage = {
  tokenPostfix: "",
  defaultToken: "",
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/\s+/, ""],
      [/"/, "string", "@string"],
      [/\$[\w.|-]+\$/, "variable"],
      [/@\[/, "", "@math"],
      [/@[A-Za-z_]\w*/, "variable"],
      [/\d{1,4}\.\d{1,2}\.\d{1,2}(?:\.\d{1,2})?(?![\w.])/, "number"],
      [/[A-Za-z][\w-]*\.\d+(?![\w.])/, "function"],
      [/-?\d+(?:\.\d+)?(?![\w.])/, "number"],
      [/(?:yes|no|none)(?![\w.-])/, "number"],
      [/(?:if|else_if|else|limit|AND|OR|NOT|NOR|NAND)(?![\w.-])/, "keyword"],
      [/([A-Za-z_][\w.-]*)(\s*)(\??=)/, ["keyword", "", ""]],
      [/(?:scope|var|local_var|global_var|flag|event_target):[\w.-]+/, "variable"],
      [/(?:any|every|random|ordered)_[a-z0-9_]+(?![\w.-])/, "keyword"],
      [/[A-Za-z_][\w.-]*/, ""],
    ],
    string: [
      [/[^\\"$[]+/, "string"],
      ESCAPE,
      [/\$[\w.|-]+\$/, "string.ref"],
      [/\[[^\]]*\]/, "string.fn"],
      [/"/, "string", "@pop"],
      [/./, "string"],
    ],
    math: [
      [/\]/, "", "@pop"],
      [/-?\d+(?:\.\d+)?/, "number"],
      [/[A-Za-z_]\w*/, "variable"],
    ],
  },
};

/** Localization: the language line, each key and its quoted text with the text's codes. */
const LOC: monaco.languages.IMonarchLanguage = {
  tokenPostfix: "",
  defaultToken: "",
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/^\s*l_[a-z_]+:\s*$/, "keyword"],
      [/"/, "string", "@text"],
      [/[^"#]+/, ""],
    ],
    // the last quote on a line closes the text, so a text may quote
    text: [
      [/"(?=[^"]*$)/, "string", "@pop"],
      ESCAPE,
      [/\[[^\]]*\]/, "string.fn"],
      [/\$[\w.|-]+\$/, "string.ref"],
      [/#[!A-Za-z_;]*/, "string.format"],
      [/@\w+!/, "string.icon"],
      [/[^\\[$#@"]+/, "string"],
      [/./, "string"],
    ],
  },
};

/** GUI: its widget words, quoted text and its data calls. */
const GUI: monaco.languages.IMonarchLanguage = {
  tokenPostfix: "",
  defaultToken: "",
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/\s+/, ""],
      [/"/, "string", "@string"],
      [
        /(?:types|type|template|widget|button|hbox|vbox|container|scrollarea|flowcontainer|fixedgridbox|overlappingcontainer|background|icon|datamodel|datacontext|visible|size|position|margin|using|blockoverride|onmousehierarchyenter|text)(?![\w.])/,
        "keyword",
      ],
      [/[A-Za-z_][\w.]*/, ""],
    ],
    string: [
      [/[^"[]+/, "string"],
      [/\[[^\]]*\]/, "string.fn"],
      [/"/, "string", "@pop"],
      [/./, "string"],
    ],
  },
};

const LANGUAGES: [id: string, extensions: string[], grammar: monaco.languages.IMonarchLanguage][] = [
  ["paradox", [".txt", ".mod", ".info", ".asset", ".editordata"], SCRIPT],
  ["paradox-loc", [".yml"], LOC],
  ["paradox-gui", [".gui"], GUI],
];
const PAIRS = [
  { open: "{", close: "}" },
  { open: "[", close: "]" },
  { open: "(", close: ")" },
  { open: '"', close: '"', notIn: ["string", "comment"] },
];
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

/**
 * The theme on the page as Monaco's: the page's tokens read as colors (Monaco takes no CSS variables), one theme per
 * page theme, set again whenever the page's changes.
 */
function followTheme(): void {
  const page = document.documentElement.dataset.theme ?? "pmt-dark";
  const name = `pmt-${page}`;
  if (!defined.has(name)) {
    defined.add(name);
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
const defined = new Set<string>();
followTheme();
new MutationObserver(followTheme).observe(document.documentElement, {
  attributes: true,
  attributeFilter: ["data-theme"],
});
// a web font measured before it loaded leaves the carets and squiggles short of its letters; the editor's own text is
// what asks for Geist Mono, after this module ran, so measure again whenever fonts finish loading
document.fonts.addEventListener("loadingdone", () => monaco.editor.remeasureFonts());

/** A token of code: its text, and its color and slant as the page's tokens, none for the text's own. */
export type CodeToken = { content: string; color?: string; italic?: boolean };

/** Text's lines as tokens in its grammar (the file name's): what read-only code draws. */
export function codeLines(text: string, name: string): CodeToken[][] {
  const lines = text.split("\n");
  return monaco.editor.tokenize(text, languageOf(name)).map((tokens, i) => {
    const line = lines[i] ?? "";
    return tokens.map((token, j) => {
      const style = TOKENS[token.type] ?? TOKENS[token.type.slice(0, token.type.lastIndexOf("."))];
      return {
        content: line.slice(token.offset, tokens[j + 1]?.offset),
        color: style && `var(--${style.color})`,
        italic: style?.italic,
      };
    });
  });
}

/**
 * A localization value's tokens, read as a loc file writes it: its codes ($variables$, [data functions],
 * #formatting, @icons!) in their colors, its own text uncolored.
 */
export function locTokens(value: string): CodeToken[] {
  const before = ' key:0 "';
  const out: CodeToken[] = [];
  let at = 0;
  for (const token of codeLines(`${before}${value}"`, "loc.yml")[0] ?? []) {
    const content = token.content.slice(
      Math.max(before.length - at, 0),
      Math.max(before.length + value.length - at, 0),
    );
    if (content) out.push({ ...token, content, color: token.color === "var(--syn-str)" ? undefined : token.color });
    at += token.content.length;
  }
  return out;
}

/** Read-only code into an element: its lines' tokens as spans, each line ending in a break. */
export function drawCode(host: HTMLElement, text: string, name: string): void {
  host.replaceChildren(
    ...codeLines(text, name).flatMap((line) => [
      ...line.map(({ content, color, italic }) => {
        const span = document.createElement("span");
        span.textContent = content;
        if (color) span.style.color = color;
        if (italic) span.style.fontStyle = "italic";
        return span;
      }),
      document.createTextNode("\n"),
    ]),
  );
}

/** What a code view draws its text in: the IDE's editor font. */
export type CodeFont = { fontFamily: string; fontSize: number; lineHeight: number };

/**
 * Two texts compared, what differs marked, side by side or stacked; unchanged lines folded into bars that open,
 * unless every line is asked for. Read only; `dispose` lets it and its texts go.
 */
export function diffView(host: HTMLElement, font: CodeFont, side: boolean, all: boolean) {
  const view = monaco.editor.createDiffEditor(host, {
    ...font,
    readOnly: true,
    automaticLayout: true,
    renderSideBySide: side,
    // a change of whitespace alone is a change of the file, as the line counts say
    ignoreTrimWhitespace: false,
    useInlineViewWhenSpaceIsLimited: false,
    hideUnchangedRegions: { enabled: !all },
    renderOverviewRuler: false,
    renderMarginRevertIcon: false,
    minimap: { enabled: false },
    glyphMargin: false,
    folding: false,
    scrollBeyondLastLine: false,
    stickyScroll: { enabled: false },
    contextmenu: false,
  });
  let shown: monaco.editor.ITextModel[] = [];
  return {
    view,
    /** Show two texts, each in its file's grammar (its format, where the text came from Go). */
    show(
      before: { text: string; name: string; format?: string },
      after: { text: string; name: string; format?: string },
    ): void {
      const next = [before, after].map(({ text, name, format }) =>
        monaco.editor.createModel(text, languageOf(name, format)),
      );
      view.setModel({ original: next[0]!, modified: next[1]! });
      shown.forEach((model) => model.dispose());
      shown = next;
    },
    dispose(): void {
      view.dispose();
      shown.forEach((model) => model.dispose());
    },
  };
}
