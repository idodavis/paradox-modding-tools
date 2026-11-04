/**
 * The open files of this window. A Model wraps a Monaco text model that every tab showing the file shares. It shows
 * Go's problems as markers and follows its file on disk. A disk change under unsaved text is kept beside the model's
 * text until the user picks one.
 */
import { useQueryCache } from "@pinia/colada";
import { useDebounceFn } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";
import { shallowReactive } from "vue";

import type { Diagnostic, Edit } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import type { Encoding, Text } from "@internal/files/models";
import * as lsp from "@services/lsp/service";
import * as workspace from "@services/workspace/service";
import { confirm } from "../../../lib/feedback";
import { list, plural } from "../../../lib/format";
import { fileQuery, keys, loadQuery, placeQuery } from "../../../queries";
import { EDITOR_LANGUAGES } from "../../../ui/ScriptSnippet.vue";

/** A Model is one open file in this window, shared by every tab that shows it. */
export type Model = {
  uri: string;
  place: Place;
  model: monaco.editor.ITextModel;
  encoding: Encoding;
  unsaved: boolean;
  caret: { line: number; column: number };
  lines: { start: number; end: number } | null; // the selected lines; null when nothing is selected
  problems: Diagnostic[];
  diskText: Text | null; // the new disk text when the file changed on disk under unsaved text; null otherwise
  disk: { version: number; text: string }; // the version and text last loaded from or saved to disk
  sync: () => Promise<Diagnostic[]>; // sends the text to Go once edits pause
};

/** SEVERITIES holds the Monaco severity of each level. Monaco has one severity for both critical and error. */
const SEVERITIES: Record<string, monaco.MarkerSeverity> = {
  critical: monaco.MarkerSeverity.Error,
  error: monaco.MarkerSeverity.Error,
  warning: monaco.MarkerSeverity.Warning,
  info: monaco.MarkerSeverity.Info,
};

const models = shallowReactive(new Map<string, Model>());
/** opening holds the loads under way, by file key. */
const opening = new Map<string, Promise<Model>>();
/** doomed holds the keys closed while their model was loading. */
const doomed = new Set<string>();

/** setupModels keeps the models in step with the workspace and the settings. */
export function setupModels(): void {
  Events.On("workspace:opened", () => {
    models.forEach((model) => void (model.unsaved ? syncText(model) : diagnose(model)));
  });
  // a check turned on or off changes every open file's problems
  Events.On("config:changed", ({ data }) => {
    if (!data || data === "settings") models.forEach((model) => void diagnose(model));
  });
}

/** openModel returns a file's model, loading its text if needed. A file has one model. */
export function openModel(place: Place): Promise<Model> {
  const model = models.get(place.key);
  if (model) return Promise.resolve(model);
  doomed.delete(place.key);
  let pending = opening.get(place.key);
  if (!pending) {
    pending = load(place).finally(() => opening.delete(place.key));
    opening.set(place.key, pending);
  }
  return pending;
}

/** findModel returns a file's open model, or null if it has none. */
export function findModel(key: string): Model | null {
  return models.get(key) ?? null;
}

/** applyEdits applies edits to a model as one undoable step. An empty list does nothing. */
export function applyEdits(model: Model, edits: Edit[]): void {
  if (!edits.length) return;
  model.model.pushStackElement();
  model.model.pushEditOperations(
    [],
    edits.map(({ range, text }) => ({ range, text })),
    () => null,
  );
  model.model.pushStackElement();
}

/**
 * syncText sends a model's text to Go once edits pause and returns its problems. A call that a later call replaces
 * returns none.
 */
export async function syncText(model: Model): Promise<Diagnostic[]> {
  return (await model.sync()) ?? [];
}

/**
 * save writes a model's text to disk. The model stays unsaved if it was edited during the write. It clears any kept
 * disk text, then checks the other models again.
 */
export async function save(model: Model): Promise<void> {
  const text = model.model.getValue();
  const version = model.model.getAlternativeVersionId();
  await workspace.Write(model.place.path, { text, encoding: model.encoding });
  model.disk = { version, text };
  model.unsaved = model.model.getAlternativeVersionId() !== version;
  model.diskText = null;
  await Promise.all([...models.values()].filter((other) => other !== model).map(diagnose));
}

/** closeModel closes a file's model and has Go drop its text. A model still loading closes once it has loaded. */
export function closeModel(key: string): void {
  const model = models.get(key);
  if (!model) {
    if (opening.has(key)) doomed.add(key);
    return;
  }
  model.model.dispose();
  void lsp.CloseFile(model.place.path);
}

/**
 * renameModel moves a file's model to the file's new place. It keeps its text, caret and unsaved state in a new
 * Monaco model.
 */
export function renameModel(key: string, place: Place): void {
  const old = models.get(key);
  if (!old) return;
  const text = old.model.getValue();
  closeModel(key);
  const model = buildModel(place, {
    text,
    disk: old.disk.text,
    encoding: old.encoding,
    unsaved: old.unsaved,
  });
  model.caret = old.caret;
  void (model.unsaved ? syncText(model) : diagnose(model));
}

/**
 * saveUnsaved asks whether to save the unsaved models before the given action, such as "Closing PMT", drops them. It
 * returns false if the user cancels.
 */
export async function saveUnsaved(action: string): Promise<boolean> {
  const unsaved = unsavedModels();
  if (!unsaved.length) return true;
  const names = unsaved.slice(0, 4).map((model) => model.place.rel);
  if (unsaved.length > 4) names.push(`${unsaved.length - 4} more`);
  const choice = await confirm.open({
    title: `Save ${plural(unsaved.length, "file")}?`,
    body: `${list(names)} ${unsaved.length === 1 ? "has" : "have"} changes not saved. ${action} without saving drops them.`,
    confirm: "Save",
    alternative: "Don't save",
  });
  if (choice === true) await Promise.all(unsaved.map(save));
  return choice !== false;
}

/** unsavedModels lists the models with unsaved text. */
export function unsavedModels(): Model[] {
  return [...models.values()].filter((model) => model.unsaved);
}

/** load reads a file and makes its model. A draft Go still holds comes back as unsaved text. */
async function load(place: Place): Promise<Model> {
  const cache = useQueryCache();
  const { data: file } = await cache.fetch(cache.ensure(fileQuery(place.path)));
  if (!file) throw new Error(`${place.path} could not be read`);
  const model = buildModel(place, {
    text: file.draft || file.text.text,
    disk: file.text.text,
    encoding: file.text.encoding,
    unsaved: !!file.draft,
  });
  if (doomed.delete(place.key)) closeModel(place.key);
  else void diagnose(model);
  return model;
}

/**
 * buildModel makes a file's Model. It reuses the Monaco text model already at the file's URI, else creates one from
 * the start text in the file's language. An unsaved start matches no disk version.
 */
function buildModel(place: Place, start: { text: string; disk: string; encoding: Encoding; unsaved: boolean }): Model {
  const uri = monaco.Uri.file(place.path);
  const textModel =
    monaco.editor.getModel(uri) ?? monaco.editor.createModel(start.text, EDITOR_LANGUAGES[place.format], uri);
  const model: Model = shallowReactive<Model>({
    uri: uri.toString(),
    place,
    model: textModel,
    encoding: start.encoding,
    unsaved: start.unsaved,
    caret: { line: 1, column: 1 },
    lines: null,
    problems: [],
    diskText: null,
    disk: { version: start.unsaved ? -1 : textModel.getAlternativeVersionId(), text: start.disk },
    sync: useDebounceFn((): Promise<Diagnostic[]> => sendText(model), 300),
  });
  textModel.onDidChangeContent(() => {
    // a file no change is written to is never unsaved
    model.unsaved = !place.readOnly && textModel.getAlternativeVersionId() !== model.disk.version;
    void syncText(model);
  });
  textModel.onWillDispose(() => models.delete(place.key));
  models.set(place.key, model);
  return model;
}

/** sendText sends a model's text to Go and shows the problems found as its markers. */
async function sendText(model: Model): Promise<Diagnostic[]> {
  if (model.model.isDisposed()) return [];
  const diagnostics = (await lsp.SetText(model.place.path, model.model.getValue())) ?? [];
  setMarkers(model, diagnostics);
  // the file's drawn pictures are fetched again from the new text
  void useQueryCache().invalidateQueries({ key: keys.draw(model.place.path) });
  return diagnostics;
}

/** diagnose asks Go for a model's problems and shows them as its markers. */
async function diagnose(model: Model): Promise<void> {
  setMarkers(model, (await lsp.Diagnose(model.place.path)) ?? []);
}

/** setMarkers keeps a model's problems and shows them as its markers. A disposed model is left alone. */
function setMarkers(model: Model, diagnostics: Diagnostic[]): void {
  if (model.model.isDisposed()) return;
  model.problems = diagnostics;
  const markers = diagnostics.map((diagnostic) => ({
    ...diagnostic.range,
    severity: SEVERITIES[diagnostic.level] ?? monaco.MarkerSeverity.Info,
    message: diagnostic.message,
    code: diagnostic.code,
  }));
  monaco.editor.setModelMarkers(model.model, "pmt", markers);
}

/**
 * applyChange applies a change on disk to its file's model. A model with no unsaved text takes the disk text at once.
 * Otherwise it keeps the disk text in diskText until the user picks one. A disk text equal to the last loaded or saved
 * text clears diskText.
 */
export async function applyChange(path: string): Promise<void> {
  const place = await loadQuery(placeQuery(path));
  const model = place && models.get(place.key);
  if (!model) return;
  const cache = useQueryCache();
  const { data: file } = await cache.fetch(cache.ensure(fileQuery(path)));
  if (!file) return;
  // a write this window made reaches it as a change too
  if (file.text.text === model.disk.text) model.diskText = null;
  else if (model.unsaved) model.diskText = file.text;
  else setText(model, file.text);
}

/** setText replaces a model's text with text from disk, as one undoable edit. The model then counts as saved. */
export function setText(model: Model, text: Text): void {
  applyEdits(model, [{ range: model.model.getFullModelRange(), text: text.text }]);
  model.encoding = text.encoding;
  model.disk = { version: model.model.getAlternativeVersionId(), text: text.text };
  model.unsaved = false;
  model.diskText = null;
}
