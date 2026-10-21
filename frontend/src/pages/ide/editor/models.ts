/**
 * The models: one copy of each open file in this window, as a Monaco text model with its place, its encoding,
 * whether it is unsaved and its caret. Every group that shows the file shares it, and stores and components read
 * these as reactive views without keeping a copy. The problems SetText returns become the model's markers, and
 * fs:changed reloads a model the user has not edited.
 */
import type { editor } from "monaco-editor";

import type { Diagnostic, Edit } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import type { Encoding } from "@internal/files/models";
/** One open file in this window: its Monaco model, its place and its encoding, shared by every tab that shows it. */
export type Model = {
  uri: string;
  place: Place;
  model: editor.ITextModel;
  encoding: Encoding;
  unsaved: boolean;
  caret: { line: number; column: number };
};

/** Opens a file's model, loading its text if needed. Every caller gets the same model. */
export function openModel(place: Place): Promise<Model> {
  throw new Error("stub", { cause: place });
}

/** The open model of a file, if any. */
export function findModel(key: string): Model | null {
  throw new Error("stub", { cause: key });
}

/** Applies the editor service's edits to a model as one undoable edit. */
export function applyEdits(model: Model, edits: Edit[]): void {
  throw new Error("stub", { cause: [model, edits] });
}

/** Sends a model's text to Go after an edit, debounced, and sets the problems that come back as its markers. */
export function syncText(model: Model): Promise<Diagnostic[]> {
  throw new Error("stub", { cause: model });
}

/** Writes a model's text to disk in its encoding and clears unsaved. */
export function save(model: Model): Promise<void> {
  throw new Error("stub", { cause: model });
}

/** Closes a model, disposing it when no tab shows it. */
export function closeModel(key: string): void {
  throw new Error("stub", { cause: key });
}

/** The models with unsaved text. */
export function unsavedModels(): Model[] {
  throw new Error("stub");
}
