/**
 * The hover card's glue. Monaco's own hover is off. When the pointer rests on a key or a value, the card is
 * requested from the editor service and shown at that position, and hidden again when the pointer moves away.
 */
import type { editor } from "monaco-editor";

/** Attaches the hover behavior to an editor and returns the function that detaches it. */
export function attachHover(codeEditor: editor.IStandaloneCodeEditor): () => void {
  throw new Error("stub", { cause: codeEditor });
}
