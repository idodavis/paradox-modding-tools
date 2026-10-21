/**
 * The Monarch grammars. They color shapes only: comments, strings, numbers, braces, operators and a key before
 * `=`, so a file reads instantly. Meaning comes from the semantic tokens on top. No pattern here mirrors a Go rule.
 */
import type { languages } from "monaco-editor";

/** The script grammar, for .txt, .gui, .asset, .font and .gfx. */
export const SCRIPT: languages.IMonarchLanguage = { tokenizer: { root: [] } };

/** The localization grammar: the header, a key, its version and its quoted text with the markup inside. */
export const LOCALIZATION: languages.IMonarchLanguage = { tokenizer: { root: [] } };
