/**
 * The Monarch grammars. They color what needs no reading: comments, strings,
 * operators and brackets. Go colors every key, value and piece of markup.
 */
import type { languages } from "monaco-editor";

/** The script grammar, for .txt, .gui, .asset, .font and .gfx. */
export const SCRIPT: languages.IMonarchLanguage = {
  brackets: [{ open: "{", close: "}", token: "delimiter.curly" }],
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/"/, "string", "@string"],
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
 * The localization grammar: the language line, a key with its version, and
 * the quoted text.
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
      [/\\./, "string"],
      [/[^\\"]+/, "string"],
      [/./, "string"],
    ],
  },
};
