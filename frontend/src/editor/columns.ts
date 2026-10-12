/**
 * The editor's reading of the backend's places: columns between the backend's UTF-8 bytes and the editor's
 * UTF-16 units, the one place either is converted (AGENTS.md).
 */
import type { Position, Range } from "@services/internal/lsp";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/** A line's text by its number; undefined where the file is not open. */
export type LineText = (line: number) => string | undefined;

/** The UTF-8 byte column of a UTF-16 column on a line: what the backend counts. */
export function toBytes(text: string, column: number): number {
  return encoder.encode(text.slice(0, column)).length;
}

/** A backend range in editor columns; a line whose text is unknown keeps its byte columns. */
export function editorRange(range: Range, lineText: LineText): Range {
  const column = (at: Position): Position => {
    const text = lineText(at.line);
    if (text === undefined) return at;
    return { line: at.line, character: decoder.decode(encoder.encode(text).subarray(0, at.character)).length };
  };
  return { start: column(range.start), end: column(range.end) };
}
