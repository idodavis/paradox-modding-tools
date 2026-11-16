/**
 * The colors of code: each token type with the theme variable for it, and
 * Go's colors set on an editor's text.
 */
import { toMultilineTokens2 } from "monaco-editor/editor/common/services/semanticTokensProviderStyling";
import { ISemanticTokensStylingService } from "monaco-editor/editor/common/services/semanticTokensStyling";
import type { editor, languages } from "monaco-editor/editor/editor.api";
import { StandaloneServices } from "monaco-editor/editor/standalone/browser/standaloneServices";

/**
 * SYNTAX holds, by the token type Go or an editor grammar gives, the name of
 * the `--syn-<name>` theme variable that colors it.
 */
export const SYNTAX: Readonly<Record<string, string>> = {
  comment: "comment",
  string: "string",
  number: "number",
  choice: "number",
  icon: "number",
  operator: "operator",
  format: "comment",
  block: "field",
  field: "field",
  entry: "name",
  keyword: "keyword",
  command: "command",
  call: "call",
  definition: "name",
  reference: "reference",
  link: "link",
  parameter: "macro",
  constant: "macro",
  // Monaco's own names for the parts of a JSON file. A value takes the
  // reference color, as a .mod descriptor's does.
  "string.key.json": "field",
  "string.value.json": "reference",
  "number.json": "reference",
  "keyword.json": "reference",
};

/** Colors is Go's colors of a text, with the legend they are written in. */
export type Colors = { legend: languages.SemanticTokensLegend; data: number[] };

/**
 * paintModel sets Go's colors on a text model. Monaco asks a provider for
 * colors only a moment after an editor shows a model, so a model painted
 * first is drawn in its colors. complete says the colors are the whole
 * text's: Monaco then asks no provider for any part of it.
 */
export function paintModel(textModel: editor.ITextModel, colors: Colors, complete: boolean): void {
  if (textModel.isDisposed()) return;
  const provider = { getLegend: () => colors.legend };
  const styling = StandaloneServices.get(ISemanticTokensStylingService).getStyling(provider);
  const tokens = toMultilineTokens2({ data: Uint32Array.from(colors.data) }, styling, textModel.getLanguageId());
  // Monaco's types leave a model's own coloring out.
  const painted = textModel as unknown as {
    tokenization: { setSemanticTokens(tokens: unknown[], all: boolean): void };
  };
  painted.tokenization.setSemanticTokens(tokens, complete);
}
