/**
 * Readonly monaco.editor.colorize for Paradox script and loc snippets.
 */
import * as monaco from "monaco-editor";
import { waitServicesReady } from "@codingame/monaco-vscode-api/lifecycle";

/** Language ids registered in paradoxLanguages.ts. */
export type ColorizeLang = "paradox" | "paradox-loc" | "paradox-gui";

/** Escape text when the tokenizer is not ready. */
function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

/**
 * Colorize one string once the workbench has started Monaco: a colorize call before that starts
 * it with services the workbench cannot replace. Falls back to escaped plain text.
 */
export async function colorizeText(
  text: string,
  language: ColorizeLang,
): Promise<string> {
  if (!text) return "";
  await waitServicesReady();
  try {
    return await monaco.editor.colorize(text, language, {});
  } catch {
    return escapeHtml(text);
  }
}

/** Colorize each line so a gutter can stay aligned. */
export async function colorizeLines(
  lines: string[],
  language: ColorizeLang,
): Promise<string[]> {
  return Promise.all(lines.map((line) => colorizeText(line || " ", language)));
}
