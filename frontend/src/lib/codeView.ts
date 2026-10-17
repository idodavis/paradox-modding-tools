/**
 * What pages draw without an editor: read-only code as the page's own elements, colored by its tokens so a theme
 * change redraws nothing, and a compare of two texts. The compare's split or unified choice is kept here.
 */
import { useLocalStorage } from "@vueuse/core";
import { CATEGORIES } from "./grammars";
import { languageOf, monaco } from "./monaco";

/** Every compare's split or unified view, one choice kept on this computer, and the tabs that pick it. */
export const compareStyle = useLocalStorage<"split" | "unified">("pmt.compare.style", "split");
export const COMPARE_STYLES = [
  { label: "Split", value: "split" },
  { label: "Unified", value: "unified" },
];

/** A token of code: its text, and its color and slant as the page's tokens, none for the text's own. */
export type CodeToken = { content: string; color?: string; italic?: boolean };

/** Text's lines as tokens in its grammar (the file name's): what read-only code draws. */
export function codeLines(text: string, name: string): CodeToken[][] {
  const lines = text.split("\n");
  return monaco.editor.tokenize(text, languageOf(name)).map((tokens, i) => {
    const line = lines[i] ?? "";
    return tokens.map((token, j) => {
      const category = CATEGORIES[token.type];
      return {
        content: line.slice(token.offset, tokens[j + 1]?.offset),
        color: category && `var(--syn-${token.type})`,
        italic: category?.italic,
      };
    });
  });
}

/**
 * A localization value's tokens, read as a loc file writes it: its codes ($variables$, [data functions],
 * #formatting, @icons!) in their colors, its own text uncolored.
 */
export function locTokens(value: string): CodeToken[] {
  const prefix = ' key:0 "';
  const out: CodeToken[] = [];
  let offset = 0;
  for (const token of codeLines(`${prefix}${value}"`, "loc.yml")[0] ?? []) {
    const content = token.content.slice(
      Math.max(prefix.length - offset, 0),
      Math.max(prefix.length + value.length - offset, 0),
    );
    if (content) out.push({ ...token, content, color: token.color === "var(--syn-string)" ? undefined : token.color });
    offset += token.content.length;
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
  let models: monaco.editor.ITextModel[] = [];
  return {
    view,
    /** Show two texts, each in its file's grammar (its format, where the text came from Go). */
    show(
      original: { text: string; name: string; format?: string },
      modified: { text: string; name: string; format?: string },
    ): void {
      const next = [original, modified].map(({ text, name, format }) =>
        monaco.editor.createModel(text, languageOf(name, format)),
      );
      const [first, second] = next;
      if (first && second) view.setModel({ original: first, modified: second });
      models.forEach((model) => model.dispose());
      models = next;
    },
    dispose(): void {
      view.dispose();
      models.forEach((model) => model.dispose());
    },
  };
}
