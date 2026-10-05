/**
 * Read-only code, drawn by Pierre (`@pierre/diffs`): the Paradox grammars and the PMT theme, whose
 * colors are the page's tokens, so a theme or mode change needs no redraw.
 */
import {
  File,
  FileDiff,
  getSharedHighlighter,
  registerCustomLanguage,
  registerCustomTheme,
  type FileContents,
  type LanguageRegistration,
  type ThemeRegistration,
} from "@pierre/diffs";

/** The Paradox grammars' scopes and the token each is drawn in (styles/themes.css). */
export const PARADOX_SCOPES: {
  token: "fg-1" | "syn-kw" | "syn-str" | "syn-num" | "syn-fn" | "syn-ref" | "syn-com";
  italic?: boolean;
  scope: string[];
}[] = [
  { token: "syn-com", italic: true, scope: ["comment", "comment.line.number-sign.paradox"] },
  { token: "syn-com", scope: ["constant.other.formatting.paradox-loc"] },
  // operators and braces in the text's own color, as the boards draw them
  {
    token: "fg-1",
    scope: ["keyword.operator.comparison.paradox", "keyword.operator.arithmetic.paradox",
      "punctuation.section.block.begin.paradox", "punctuation.section.block.end.paradox",
      "punctuation.section.brackets.paradox"],
  },
  {
    token: "syn-kw",
    scope: ["keyword.control.paradox", "keyword.operator.iterator.paradox", "variable.other.property.paradox",
      "entity.name.type.paradox", "keyword.control.paradox-gui", "keyword.control.language.paradox-loc"],
  },
  {
    token: "syn-str",
    scope: ["string.quoted.double.paradox", "string.quoted.double.paradox-loc", "string.quoted.double.paradox-gui"],
  },
  {
    token: "syn-num",
    scope: ["constant.numeric.paradox", "constant.numeric.date.paradox", "constant.language.paradox", "constant.other.icon.paradox-loc"],
  },
  {
    token: "syn-fn",
    scope: ["entity.name.function.event-id.paradox", "support.function.databinding.paradox",
      "support.function.databinding.paradox-gui", "support.function.databinding.paradox-loc"],
  },
  {
    token: "syn-ref",
    scope: ["variable.other.saved-scope.paradox", "variable.other.constant.script.paradox",
      "variable.other.substitution.paradox", "variable.other.substitution.paradox-loc", "variable.parameter.macro.paradox",
      "variable.parameter.placeholder.paradox", "variable.language.scope.paradox"],
  },
];

/** The Paradox grammars, by the name each is registered and drawn by. */
const GRAMMARS: Record<string, () => Promise<{ default: object }>> = {
  paradox: () => import("../syntaxes/paradox.tmLanguage.json"),
  "paradox-loc": () => import("../syntaxes/paradox-loc.tmLanguage.json"),
  "paradox-gui": () => import("../syntaxes/paradox-gui.tmLanguage.json"),
};
// Pierre finds a grammar by its name, so each is loaded under the name it is registered by
for (const [name, load] of Object.entries(GRAMMARS)) {
  registerCustomLanguage(name, async () => ({ default: [{ ...(await load()).default, name } as LanguageRegistration] }));
}

/** The grammar a file's extension calls for: localization, GUI, else script. */
function grammarOf(name: string): string {
  const extension = name.slice(name.lastIndexOf(".")).toLowerCase();
  return extension === ".yml" ? "paradox-loc" : extension === ".gui" ? "paradox-gui" : "paradox";
}

/** A file to draw, in its grammar. */
export function codeFile(name: string, contents: string): FileContents {
  return { name, contents, lang: grammarOf(name) };
}

/** A token of code: its text and its color, none for the text's own. */
export type CodeToken = { content: string; color?: string };

/** One line of a file's code as the theme's tokens, for code written inline (a property's value). */
export async function codeTokens(text: string, name: string): Promise<CodeToken[]> {
  const lang = grammarOf(name);
  const highlighter = await getSharedHighlighter({ themes: ["pmt"], langs: [lang] });
  return highlighter.codeToTokensBase(text, { lang, theme: "pmt" })[0] ?? [];
}

/**
 * A localization value's tokens, read as a loc file writes it: its codes ($variables$, [data functions],
 * #formatting, @icons!) in their colors, its own text uncolored.
 */
export async function locTokens(value: string): Promise<CodeToken[]> {
  const before = ' key:0 "';
  const end = before.length + value.length;
  const out: CodeToken[] = [];
  let at = 0;
  for (const token of await codeTokens(`${before}${value}"`, "loc.yml")) {
    const content = token.content.slice(Math.max(before.length - at, 0), Math.max(end - at, 0));
    if (content) out.push({ content, color: token.color === "var(--syn-str)" ? undefined : token.color });
    at += token.content.length;
  }
  return out;
}

const THEME: ThemeRegistration = {
  name: "pmt",
  colors: { "editor.foreground": "var(--fg-1)", "editor.background": "var(--bg-0)" },
  tokenColors: PARADOX_SCOPES.map(({ token, italic, scope }) => ({
    scope,
    settings: { foreground: `var(--${token})`, fontStyle: italic ? "italic" : "" },
  })),
};
registerCustomTheme("pmt", () => Promise.resolve(THEME));

/**
 * The boards' `.code`: Geist Mono 12.5 / 20; numbers in fg-3, right-aligned in a 48px column whose
 * last 16px are space; the container sets the padding around the rows. A long line scrolls, with the
 * page's thin scrollbar (styles/themes.css), which does not reach into Pierre's shadow root.
 */
const CODE_CSS = `:host {
  --diffs-font-family: var(--font-mono); --diffs-font-size: 12.5px; --diffs-line-height: 20px;
  --diffs-fg-number-override: var(--fg-3); --diffs-selection-base: var(--accent);
  --diffs-addition-color-override: var(--green); --diffs-deletion-color-override: var(--red);
  --diffs-modified-color-override: var(--blue);
  --diffs-gap-block: 0px; --diffs-gap-style: none; --diffs-overflow-override: auto;
}
[data-code] { scrollbar-width: thin; scrollbar-color: transparent transparent; }
[data-code]:hover { scrollbar-color: var(--line-2) transparent; }
[data-gutter] [data-column-number] { padding: 0 16px 0 0; }
[data-line-number-content] { min-width: 32px; }
[data-line] { padding-inline: 0 16px; }
[data-disable-line-numbers] [data-column-number] { padding: 0; }
[data-disable-line-numbers] [data-line] { padding-inline-start: 12px; }`;

/** A view that draws read-only code; numbered when asked. */
export function codeView(numbers: boolean): File {
  return new File({
    theme: "pmt",
    disableFileHeader: true,
    disableLineNumbers: !numbers,
    overflow: "scroll",
    unsafeCSS: CODE_CSS,
  });
}

/**
 * Draw text from a file (its name picks the grammar) into a host with a view, in Pierre's own element
 * (which carries its styles). `from` is the 0-based file line the text starts at: the text is drawn as
 * that window of its file, so its rows carry the file's line numbers.
 */
export function drawCode(view: File, host: HTMLElement, text: string, name: string, from = 0): void {
  const rows = text.split("\n").length;
  view.render({
    file: codeFile(name, "\n".repeat(from) + text),
    containerWrapper: host,
    renderRange: { startingLine: from, totalLines: rows, bufferBefore: 0, bufferAfter: 0 },
  });
}

/**
 * A view that draws two files side by side, what differs marked and nothing folded, so any line of either
 * can be shown; each file is given as `codeFile` makes it.
 */
export function diffView(): FileDiff {
  return new FileDiff({
    theme: "pmt",
    diffStyle: "split",
    expandUnchanged: true,
    disableFileHeader: true,
    overflow: "scroll",
    unsafeCSS: CODE_CSS,
  });
}
