/**
 * The models: one copy of each open file in this window, as a Monaco text model with its place, its encoding,
 * whether it is unsaved and its caret. Every group that shows the file shares it, and stores and components read
 * these as reactive views without keeping a copy. The problems SetText returns become the model's markers, and
 * fs:changed reloads a model the user has not edited.
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
import { diagnosticMessage, list, plural } from "../../../lib/format";
import { codesQuery, fileQuery, keys, loadQuery, placeQuery } from "../../../queries";
import ConfirmModal from "../../../ui/ConfirmModal.vue";
import { VIEWS } from "./tabs";

/** One open file in this window: its Monaco model, its place and its encoding, shared by every tab that shows it. */
export type Model = {
  uri: string;
  place: Place;
  model: monaco.editor.ITextModel;
  encoding: Encoding;
  unsaved: boolean;
  caret: { line: number; column: number };
  problems: Diagnostic[];
};

// a code's severity as Go counts it (1 error, 2 warning, 3 information, 4 hint) and as Monaco does
const SEVERITIES: Record<number, monaco.MarkerSeverity> = {
  1: monaco.MarkerSeverity.Error,
  2: monaco.MarkerSeverity.Warning,
  3: monaco.MarkerSeverity.Info,
  4: monaco.MarkerSeverity.Hint,
};

const models = shallowReactive(new Map<string, Model>());
const opening = new Map<string, Promise<Model>>();
// the keys closed while their model was loading
const doomed = new Set<string>();
// what each model last agreed with on disk: the version of its text and the text itself
const disk = new Map<string, { version: number; text: string }>();
const syncs = new Map<string, () => Promise<Diagnostic[]>>();
const confirm = useOverlay().create(ConfirmModal);

/**
 * Starts following the disk, the workspace and the settings. A file changed on disk is brought into its model, with
 * the given toast asking first when the model has unsaved text. A workspace opened again is sent the texts again, and
 * a change of the settings checks every model again.
 */
export function setupModels(toast: ReturnType<typeof useToast>): void {
  Events.On("fs:changed", ({ data }) => {
    if (!data.deleted) void applyChange(data.path, toast);
  });
  Events.On("workspace:opened", () => {
    models.forEach((model) => void (model.unsaved ? syncText(model) : diagnose(model)));
  });
  // a check turned on or off changes every open file's problems
  Events.On("config:changed", ({ data }) => {
    if (!data || data === "settings") models.forEach((model) => void diagnose(model));
  });
}

/** Opens a file's model, loading its text if needed. Every caller gets the same model. */
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

/** The open model of a file, if any. */
export function findModel(key: string): Model | null {
  return models.get(key) ?? null;
}

/** Applies the editor service's edits to a model as one undoable edit. */
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
 * Sends a model's text to Go after an edit, debounced, and sets the problems that come back as its markers. A call
 * that a later one replaces answers with no problems.
 */
export async function syncText(model: Model): Promise<Diagnostic[]> {
  let debounced = syncs.get(model.place.key);
  if (!debounced) {
    debounced = useDebounceFn(() => sendText(model), 300);
    syncs.set(model.place.key, debounced);
  }
  return (await debounced()) ?? [];
}

/** Writes a model's text to disk in its encoding and clears unsaved. The other open models are judged again. */
export async function save(model: Model): Promise<void> {
  const text = model.model.getValue();
  const version = model.model.getAlternativeVersionId();
  await workspace.Write(model.place.path, { text, encoding: model.encoding });
  disk.set(model.place.key, { version, text });
  model.unsaved = model.model.getAlternativeVersionId() !== version;
  await Promise.all([...models.values()].filter((other) => other !== model).map(diagnose));
}

/**
 * Closes a model and has Go drop the file's text. A model still loading is closed when it lands. A file with no
 * model changes nothing.
 */
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
 * Moves a model to the place its file was renamed to. It keeps its text, its caret and whether it is unsaved, but it
 * is a new Monaco model, since a model's address cannot change. A file with no model changes nothing.
 */
export function renameModel(key: string, place: Place): void {
  const old = models.get(key);
  const previous = disk.get(key);
  if (!old) return;
  const text = old.model.getValue();
  closeModel(key);
  const uri = monaco.Uri.file(place.path);
  const next = monaco.editor.getModel(uri) ?? monaco.editor.createModel(text, VIEWS[place.format].language, uri);
  const version = old.unsaved ? 0 : next.getAlternativeVersionId();
  const model = buildModel(place, next, old.encoding, { version, text: previous?.text ?? text });
  Object.assign(model, { unsaved: old.unsaved, caret: old.caret });
  void (model.unsaved ? syncText(model) : diagnose(model));
}

/**
 * Asks whether to save the unsaved models before something drops them, named by the given action such as "Closing
 * PMT". Save writes them all and Don't save leaves them, and both return true. Cancel returns false. It returns true
 * at once when no model is unsaved.
 */
export async function saveUnsaved(going: string): Promise<boolean> {
  const unsaved = unsavedModels();
  if (!unsaved.length) return true;
  const names = unsaved.slice(0, 4).map((model) => model.place.rel);
  if (unsaved.length > 4) names.push(`${unsaved.length - 4} more`);
  const choice = await confirm.open({
    title: `Save ${plural(unsaved.length, "file")}?`,
    body: `${list(names)} ${unsaved.length === 1 ? "has" : "have"} changes not saved. ${going} without saving drops them.`,
    confirm: "Save",
    alternative: "Don't save",
  });
  if (choice === true) await Promise.all(unsaved.map(save));
  return choice !== false;
}

/** The models with unsaved text. */
export function unsavedModels(): Model[] {
  return [...models.values()].filter((model) => model.unsaved);
}

/**
 * Reads a file and makes its model, shared from then on. A draft Go still holds for the file, such as edits a reload
 * dropped from the page, comes back as the model's text, unsaved. A model closed while it loads is closed when it lands.
 */
async function load(place: Place): Promise<Model> {
  const cache = useQueryCache();
  const { data: file } = await cache.fetch(cache.ensure(fileQuery(place.path)));
  if (!file) throw new Error(`${place.path} could not be read`);
  const uri = monaco.Uri.file(place.path);
  const text =
    monaco.editor.getModel(uri) ??
    monaco.editor.createModel(file.draft || file.text.text, VIEWS[place.format].language, uri);
  const model = buildModel(place, text, file.text.encoding, {
    version: file.draft ? -1 : text.getAlternativeVersionId(),
    text: file.text.text,
  });
  model.unsaved = !!file.draft;
  if (doomed.delete(place.key)) closeModel(place.key);
  else void diagnose(model);
  return model;
}

/** Makes the model of a file around a Monaco text model and the disk's version of it, and keeps it up to date. */
function buildModel(
  place: Place,
  text: monaco.editor.ITextModel,
  encoding: Encoding,
  snapshot: { version: number; text: string },
): Model {
  const model = shallowReactive<Model>({
    uri: text.uri.toString(),
    place,
    model: text,
    encoding,
    unsaved: false,
    caret: { line: 1, column: 1 },
    problems: [],
  });
  disk.set(place.key, snapshot);
  text.onDidChangeContent(() => {
    model.unsaved = text.getAlternativeVersionId() !== disk.get(place.key)?.version;
    void syncText(model);
  });
  text.onWillDispose(() => {
    models.delete(place.key);
    disk.delete(place.key);
    syncs.delete(place.key);
  });
  models.set(place.key, model);
  return model;
}

/** Sends the model's text to Go and sets the problems that come back as its markers. */
async function sendText(model: Model): Promise<Diagnostic[]> {
  if (model.model.isDisposed()) return [];
  const diagnostics = (await lsp.SetText(model.place.path, model.model.getValue())) ?? [];
  await setMarkers(model, diagnostics);
  // the pictures drawn from the file are drawn again from the new text
  void useQueryCache().invalidateQueries({ key: keys.draw(model.place.path) });
  return diagnostics;
}

/** Asks Go for a file's problems as it is now and sets them as the model's markers. */
async function diagnose(model: Model): Promise<void> {
  await setMarkers(model, (await lsp.Diagnose(model.place.path)) ?? []);
}

/** Keeps the problems found in a model and sets them as its markers, each worded from its code's template. */
async function setMarkers(model: Model, diagnostics: Diagnostic[]): Promise<void> {
  const codes = (await loadQuery(codesQuery)) ?? [];
  if (model.model.isDisposed()) return;
  model.problems = diagnostics;
  const markers = diagnostics.map((diagnostic) => {
    const code = codes.find((row) => row.id === diagnostic.code);
    return {
      ...diagnostic.range,
      severity: SEVERITIES[code?.severity ?? 3] ?? monaco.MarkerSeverity.Info,
      message: code ? diagnosticMessage(code.message, diagnostic.facts ?? {}) : diagnostic.code,
      code: diagnostic.code,
    };
  });
  monaco.editor.setModelMarkers(model.model, "pmt", markers);
}

/** Brings a file changed on disk into its model: at once if the user has not edited it, else after they choose. */
async function applyChange(path: string, toast: ReturnType<typeof useToast>): Promise<void> {
  const place = await loadQuery(placeQuery(path));
  const model = place && models.get(place.key);
  if (!model) return;
  const cache = useQueryCache();
  const { data: file } = await cache.fetch(cache.ensure(fileQuery(path)));
  const snapshot = disk.get(model.place.key);
  // a write this window made reaches it as a change too
  if (!file || !snapshot || file.text.text === snapshot.text) return;
  if (!model.unsaved) {
    setText(model, file.text);
    return;
  }
  toast.add({
    title: `${model.place.name} changed on disk`,
    description: "Reload it and lose your edits, or keep them.",
    color: "warning",
    duration: 0,
    actions: [
      { label: "Reload", onClick: () => setText(model, file.text) },
      { label: "Keep", onClick: () => disk.set(model.place.key, { version: snapshot.version, text: file.text.text }) },
    ],
  });
}

/** Puts a text read from disk in a model as one undoable edit, and takes it as what the model agrees with. */
function setText(model: Model, text: Text): void {
  applyEdits(model, [{ range: model.model.getFullModelRange(), text: text.text }]);
  model.encoding = text.encoding;
  disk.set(model.place.key, { version: model.model.getAlternativeVersionId(), text: text.text });
  model.unsaved = false;
}
