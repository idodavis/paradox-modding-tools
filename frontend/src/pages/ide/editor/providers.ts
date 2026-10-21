/**
 * Monaco's language providers over the editor service, registered once for the Paradox languages: one
 * registration per feature, each calling the service with the model's path and Monaco's own position and handing
 * the answer to Monaco as it is, since Go answers in Monaco's ranges. Diagnostics are not a provider: they come
 * back from SetText and become the model's markers. The hover card is not a provider either, since the app draws
 * it.
 */
import type { languages } from "monaco-editor";

/** Registers every provider for the given languages. Called once, after the languages themselves are registered. */
export function registerProviders(languageIds: string[], legend: languages.SemanticTokensLegend): void {
  throw new Error("stub", { cause: [languageIds, legend] });
}
