/**
 * The Paradox languages: their Monarch grammars, the file extensions each is registered for, their bracket and quote
 * pairs, and the categories code is colored by. A grammar colors by shape alone and lists no key; the engine's
 * tokens (editor/providers.ts, lsp.Tokens) color the keys, values and names only its walk knows, under the same
 * categories. Tables only; lib/monaco.ts registers them.
 */
import type { languages } from "monaco-editor/editor/editor.api";

/** The categories code is colored by, each in its page token `--syn-<category>` (styles/themes.css), and whether it
 * is slanted; code in none is the text's own color. The engine's (lsp.Category) are four of them. */
export const CATEGORIES: Record<string, { italic?: boolean }> = {
  entry: {},
  operator: {},
  block: {},
  field: {},
  scope: {},
  variable: {},
  string: {},
  number: {},
  literal: {},
  comment: { italic: true },
};

const ESCAPE: languages.IMonarchLanguageRule = [/\\./, "string"];

/** Paradox script and GUI, one syntax: a keyword pair opening a line, keys by what follows them, saved scopes and
 * variables by their prefix, parameters and constants, event ids, dates, numbers and booleans, quoted text. */
const SCRIPT: languages.IMonarchLanguage = {
  tokenPostfix: "",
  defaultToken: "",
  tokenizer: {
    root: [
      // a keyword pair opens a line: scripted_trigger x = {, type x = button {, template x {, blockoverride "x" {
      [/^(\s*)([A-Za-z_]\w*)(\s+)("[^"]*")(?=\s*[={])/, ["", "block", "", "string"]],
      [/^(\s*)([A-Za-z_]\w*)(\s+)([A-Za-z_][\w.-]*)(?=\s*[={])/, ["", "block", "", ""]],
      [/#.*$/, "comment"],
      [/\s+/, ""],
      [/"/, "string", "@string"],
      [/\$[\w.|-]+\$/, "variable"],
      [/@\[/, "", "@math"],
      [/@[A-Za-z_]\w*/, "variable"],
      [/\d{1,4}\.\d{1,2}\.\d{1,2}(?:\.\d{1,2})?(?![\w.])/, "literal"],
      [/[A-Za-z][\w-]*\.\d+(?![\w.])/, "entry"],
      [/-?\d+(?:\.\d+)?(?![\w.])/, "number"],
      [/(?:yes|no|none)(?![\w.:-])/, "literal"],
      [/(?:scope|event_target):[\w.:-]+/, "scope"],
      [/(?:var|local_var|global_var|flag):[\w.:-]+/, "variable"],
      [/([A-Za-z_][\w.:-]*)(\s*)([!=<>?]=|[=<>])(?=\s*\{)/, ["block", "", ""]],
      [/([A-Za-z_][\w.:-]*)(\s*)([!=<>?]=|[=<>])/, ["field", "", ""]],
      [/[A-Za-z_][\w.:-]*/, ""],
    ],
    string: [
      [/[^\\"$[]+/, "string"],
      ESCAPE,
      [/\$[\w.|-]+\$/, "variable"],
      [/\[[^\]]*\]/, "scope"],
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
const LOC: languages.IMonarchLanguage = {
  tokenPostfix: "",
  defaultToken: "",
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/^\s*l_[a-z_]+:\s*$/, "block"],
      [/^(\s*)([^\s:#"]+)(:\d*)/, ["", "entry", ""]],
      [/"/, "string", "@text"],
      [/[^"#]+/, ""],
    ],
    // the last quote on a line closes the text, so a text may quote
    text: [
      [/"(?=[^"]*$)/, "string", "@pop"],
      ESCAPE,
      [/\[[^\]]*\]/, "scope"],
      [/\$[\w.|-]+\$/, "variable"],
      [/#[!A-Za-z_;]*/, "comment"],
      [/@\w+!/, "literal"],
      [/[^\\[$#@"]+/, "string"],
      [/./, "string"],
    ],
  },
};

/** Each language's id, the file extensions it is registered for and its grammar. */
export const LANGUAGES: [id: string, extensions: string[], grammar: languages.IMonarchLanguage][] = [
  ["paradox", [".txt", ".mod", ".info", ".asset", ".editordata"], SCRIPT],
  ["paradox-loc", [".yml"], LOC],
  ["paradox-gui", [".gui"], SCRIPT],
];
/** The brackets and quotes typed in pairs and wrapped around a selection; a quote pairs outside strings and comments. */
export const PAIRS = [
  { open: "{", close: "}" },
  { open: "[", close: "]" },
  { open: "(", close: ")" },
  { open: '"', close: '"', notIn: ["string", "comment"] },
];
