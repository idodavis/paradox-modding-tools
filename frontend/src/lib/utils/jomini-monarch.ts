import type { languages } from "monaco-editor";

/** Paradox / Jomini script — Monarch grammar (replaces prior TextMate JSON). */
export const JOMINI_LANGUAGE_ID = "jomini" as const;

const keywords = [
  "if",
  "else",
  "for_each",
  "in",
  "true",
  "false",
  "yes",
  "no",
  "root",
  "prev",
  "from",
  "owner",
  "controller",
  "limit",
  "add",
  "multiply",
  "divide",
  "subtract",
  "round",
  "fixed_range",
  "ordered_in_list",
  "random",
  "random_list",
  "first_valid",
  "every",
  "any",
  "all",
  "scripted_triggers",
  "scripted_effects",
  "scripted_lists",
  "on_action",
  "namespace",
  "types",
  "gui_types",
  "gui",
  "list",
  "event",
  "character",
  "scope",
  "hidden_effect",
  "else_if",
  "trigger",
  "effect",
  "modifier",
  "value",
  "compare",
] as const;

const keywordAlternation = keywords
  .map((k) => k.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
  .join("|");

/** Block/object keys: `foo = {` / `blabla.0130 = {` but not reserved keywords. */
const blockAssignKey = new RegExp(
  `(?!\\b(?:${keywordAlternation})\\b)[a-zA-Z_][\\w.-]*(?=\\s*=\\s*\\{)`,
);

/** `scripted_triggers my_trigger = {` — name after certain headers. */
const namedBlockHeader = new RegExp(
  `\\b(scripted_triggers|scripted_effects|scripted_lists)\\s+([a-zA-Z_][\\w.-]*)`,
);

export const jominiMonarchLanguage: languages.IMonarchLanguage = {
  keywords: [...keywords],
  symbols: /[=><!~?:&|+\-*/^%]+/,
  escapes: /\\(?:[abfnrtv\\"']|x[0-9A-Fa-f]{2}|u[0-9A-Fa-f]{4})/,
  tokenizer: {
    root: [
      [/[ \t\r\n]+/, "white"],
      [/#.*$/, "comment"],
      [/\/\/.*$/, "comment"],
      [/\/\*/, "comment", "@comment"],
      [/@[a-zA-Z_][\w]*/, "variable.predefined"],
      [/\d+\.\d+/, "number.float"],
      [/0x[0-9a-fA-F]+/, "number.hex"],
      [/\d+/, "number"],
      [namedBlockHeader, ["keyword", "type"]],
      [blockAssignKey, "type"],
      [/[a-zA-Z_][\w.-]*/, { cases: { "@keywords": "keyword", "@default": "identifier" } }],
      [/[{}[\]()]/, "@brackets"],
      [/@symbols/, "operator"],
      [/[,:]/, "delimiter"],
      [/"/, "string.quote", "@string"],
    ],
    string: [
      [/[^\\"]+/, "string"],
      [/@escapes/, "string.escape"],
      [/\\./, "string.escape.invalid"],
      [/"/, "string.quote", "@pop"],
    ],
    comment: [
      [/[^/*]+/, "comment"],
      [/\*\//, "comment", "@pop"],
      [/[/*]/, "comment"],
    ],
  },
};

export const jominiLanguageConfiguration: languages.LanguageConfiguration = {
  comments: { lineComment: "#", blockComment: ["/*", "*/"] },
  brackets: [
    ["{", "}"],
    ["[", "]"],
    ["(", ")"],
  ],
  autoClosingPairs: [
    { open: "{", close: "}" },
    { open: "[", close: "]" },
    { open: "(", close: ")" },
    { open: '"', close: '"' },
  ],
  surroundingPairs: [
    { open: "{", close: "}" },
    { open: "[", close: "]" },
    { open: "(", close: ")" },
    { open: '"', close: '"' },
  ],
};
