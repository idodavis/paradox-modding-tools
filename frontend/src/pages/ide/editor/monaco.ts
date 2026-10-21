/**
 * Monaco's setup, done once: the Paradox languages with their grammars, the providers over the editor service,
 * one theme per page theme made when that theme comes into force, with its token colors read from the theme's
 * `--syn-<category>` variables, and the editor options the settings give.
 */
import type { editor } from "monaco-editor";

/** Registers the languages and the providers, once, before the first editor. */
export function setupMonaco(): void {
  throw new Error("stub");
}

/** Makes Monaco's theme for the page theme in force, if it does not exist yet, applies it and returns its name. */
export function applyTheme(family: string, dark: boolean): string {
  throw new Error("stub", { cause: [family, dark] });
}

/** The editor options from the settings: text size, indentation, the checks. */
export function editorOptions(): editor.IStandaloneEditorConstructionOptions {
  throw new Error("stub");
}
