/**
 * The Paradox languages: their Monarch grammars, the file extensions each is registered for, their bracket and quote
 * pairs, and the page token each kind of token is drawn in. Tables only; lib/monaco.ts registers them.
 */
import type { languages } from "monaco-editor/editor/editor.api";

/** Each kind of token and the page's token it is drawn in (styles/themes.css); none is the text's own color. */
export const TOKENS: Record<string, { color: string; italic?: boolean }> = {
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

const ESCAPE: languages.IMonarchLanguageRule = [/\\./, "string"];

/** Paradox script: keys before `=`, the flow keywords and iterators, events, dates, saved scopes, constants. */
const SCRIPT: languages.IMonarchLanguage = {
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
const LOC: languages.IMonarchLanguage = {
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
const GUI: languages.IMonarchLanguage = {
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

/** Each language's id, the file extensions it is registered for and its grammar. */
export const LANGUAGES: [id: string, extensions: string[], grammar: languages.IMonarchLanguage][] = [
  ["paradox", [".txt", ".mod", ".info", ".asset", ".editordata"], SCRIPT],
  ["paradox-loc", [".yml"], LOC],
  ["paradox-gui", [".gui"], GUI],
];
/** The brackets and quotes typed in pairs and wrapped around a selection; a quote pairs outside strings and comments. */
export const PAIRS = [
  { open: "{", close: "}" },
  { open: "[", close: "]" },
  { open: "(", close: ")" },
  { open: '"', close: '"', notIn: ["string", "comment"] },
];
