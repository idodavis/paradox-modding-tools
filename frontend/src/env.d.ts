/// <reference types="vite/client" />

// The modules Monaco colors a model with. Its types leave them out.
declare module "monaco-editor/editor/common/services/semanticTokensProviderStyling" {
  export function toMultilineTokens2(tokens: { data: Uint32Array }, styling: unknown, languageId: string): unknown[];
}

declare module "monaco-editor/editor/common/services/semanticTokensStyling" {
  export const ISemanticTokensStylingService: unknown;
}

declare module "monaco-editor/editor/standalone/browser/standaloneServices" {
  export const StandaloneServices: {
    get(id: unknown): { getStyling(provider: { getLegend(): unknown }): unknown };
  };
}
