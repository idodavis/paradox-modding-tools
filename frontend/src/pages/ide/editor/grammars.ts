/**
 * The Monarch grammars. They color shapes only: comments, strings, numbers, dates, yes and no, braces, operators and
 * a key before `=`, so a file reads instantly. Meaning comes from the semantic tokens on top. No pattern here
 * mirrors a Go rule.
 */
import type { languages } from "monaco-editor";

/** The script grammar, for .txt, .gui, .asset, .font and .gfx. */
export const SCRIPT: languages.IMonarchLanguage = {
  brackets: [{ open: "{", close: "}", token: "delimiter.curly" }],
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/"/, "string", "@string"],
      // a key is whatever stands before an operator, and a block's key has a brace after it
      [/[\w.:@$|'-]+(?=\s*(?:[!=<>?]=|[=<>])\s*\{)/, "block"],
      [/[\w.:@$|'-]+(?=\s*(?:[!=<>?]=|[=<>]))/, "field"],
      [/\d{1,4}\.\d{1,2}\.\d{1,2}(?:\.\d{1,2})?(?![\w.])/, "literal"],
      [/-?\d+(?:\.\d+)?(?![\w.])/, "number"],
      [/(?:yes|no|none)(?![\w.:-])/, "literal"],
      [/[!=<>?]=|[=<>]/, "operator"],
      [/[{}]/, "@brackets"],
    ],
    string: [
      [/[^\\"]+/, "string"],
      [/\\./, "string"],
      [/"/, "string", "@pop"],
    ],
  },
};

/**
 * The localization grammar: the language line, a key with its version, and the quoted text with the markup inside:
 * `$variables$`, `[scopes]`, `#formats` and `@icons!`.
 */
export const LOCALIZATION: languages.IMonarchLanguage = {
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/^\s*l_\w+:/, "block"],
      [/^(\s*)([^\s:#"]+)(:\d*)/, ["", "entry", "number"]],
      [/"/, "string", "@text"],
    ],
    // the last quote on a line closes the text, so a text may quote
    text: [
      [/"(?=[^"]*$)/, "string", "@pop"],
      [/\$[^$\s]*\$/, "variable"],
      [/\[[^\]]*\]/, "scope"],
      [/#[!\w;]*/, "comment"],
      [/@\w+!/, "literal"],
      [/\\./, "string"],
      [/[^\\"$[#@]+/, "string"],
      [/./, "string"],
    ],
  },
};
