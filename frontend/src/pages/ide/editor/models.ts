/**
 * The open files of this window, each a model around a Monaco text model that its tabs share. A model shows Go's
 * problems as markers and follows its file on disk; a change under unsaved text is kept beside it, for the user to keep
 * either text.
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
import { describeFinding, list, plural } from "../../../lib/format";
import { codesQuery, fileQuery, keys, loadQuery, placeQuery } from "../../../queries";
import ConfirmModal from "../../../ui/ConfirmModal.vue";
import { EDITOR_LANGUAGES } from "../../../ui/ScriptSnippet.vue";

/** A Model is one open file in this window, shared by every tab that shows it. */
export type Model = {
  uri: string;
  place: Place;
  model: monaco.editor.ITextModel;
  encoding: Encoding;
  unsaved: boolean;
  caret: { line: number; column: number };
  lines: { start: number; end: number } | null; // the lines selected, or null when nothing is
  problems: Diagnostic[];
  diskText: Text | null; // the disk's text after the file changed on disk under unsaved text; null while none did
};

/** SEVERITIES holds the Monaco severity of each level. Monaco has one severity for both critical and error. */
export const SEVERITIES: Record<string, monaco.MarkerSeverity> = {
  critical: monaco.MarkerSeverity.Error,
  error: monaco.MarkerSeverity.Error,
  warning: monaco.MarkerSeverity.Warning,
  info: monaco.MarkerSeverity.Info,
};

const models = shallowReactive(new Map<string, Model>());
/** The loads under way, by file key. */
const opening = new Map<string, Promise<Model>>();
/** The keys closed while their model was loading. */
const doomed = new Set<string>();
/** The version and text each model last agreed with on disk. */
const disk = new Map<string, { version: number; text: string }>();
/** The debounced send of each model's text to Go, by file key. */
const syncs = new Map<string, () => Promise<Diagnostic[]>>();
const confirm = useOverlay().create(ConfirmModal);

/**
 * setupModels keeps the models in step with the disk, the workspace and the settings. A change on disk under unsaved
 * text is kept as the model's disk text.
 */
export function setupModels(): void {
  Events.On("fs:changed", ({ data }) => {
    if (!data.deleted) void applyChange(data.path);
  });
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
 * syncText sends a model's text to Go once the edits pause, and returns the problems found. A call that a later call
 * replaces returns none.
 */
export async function syncText(model: Model): Promise<Diagnostic[]> {
  let debounced = syncs.get(model.place.key);
  if (!debounced) {
    debounced = useDebounceFn(() => sendText(model), 300);
    syncs.set(model.place.key, debounced);
  }
  return (await debounced()) ?? [];
}

/**
 * save writes a model's text to disk and marks it saved, unless it was edited meanwhile. It drops a disk text the
 * model kept. The other models are checked again.
 */
export async function save(model: Model): Promise<void> {
  const text = model.model.getValue();
  const version = model.model.getAlternativeVersionId();
  await workspace.Write(model.place.path, { text, encoding: model.encoding });
  disk.set(model.place.key, { version, text });
  model.unsaved = model.model.getAlternativeVersionId() !== version;
  model.diskText = null;
  await Promise.all([...models.values()].filter((other) => other !== model).map(diagnose));
}

/** closeModel closes a file's model and has Go drop its text. A model still loading closes when it lands. */
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
  const previous = disk.get(key);
  if (!old) return;
  const text = old.model.getValue();
  closeModel(key);
  const model = buildModel(place, {
    text,
    disk: previous?.text ?? text,
    encoding: old.encoding,
    unsaved: old.unsaved,
  });
  model.caret = old.caret;
  void (model.unsaved ? syncText(model) : diagnose(model));
}

/**
 * saveUnsaved asks whether to save the unsaved models before an action such as "Closing PMT" drops them. It returns
 * false if the user cancels.
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
 * buildModel makes a file's model and keeps it up to date. It takes the Monaco text model the file's address has, or
 * makes one in the file's language from the start text. An unsaved model matches no version of the disk.
 */
function buildModel(place: Place, start: { text: string; disk: string; encoding: Encoding; unsaved: boolean }): Model {
  const uri = monaco.Uri.file(place.path);
  const text =
    monaco.editor.getModel(uri) ?? monaco.editor.createModel(start.text, EDITOR_LANGUAGES[place.format], uri);
  const model = shallowReactive<Model>({
    uri: uri.toString(),
    place,
    model: text,
    encoding: start.encoding,
    unsaved: start.unsaved,
    caret: { line: 1, column: 1 },
    lines: null,
    problems: [],
    diskText: null,
  });
  disk.set(place.key, { version: start.unsaved ? -1 : text.getAlternativeVersionId(), text: start.disk });
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

/** sendText sends a model's text to Go and shows the problems found as its markers. */
async function sendText(model: Model): Promise<Diagnostic[]> {
  if (model.model.isDisposed()) return [];
  const diagnostics = (await lsp.SetText(model.place.path, model.model.getValue())) ?? [];
  await setMarkers(model, diagnostics);
  // the file's pictures are drawn again from the new text
  void useQueryCache().invalidateQueries({ key: keys.draw(model.place.path) });
  return diagnostics;
}

/** diagnose asks Go for a model's problems and shows them as its markers. */
async function diagnose(model: Model): Promise<void> {
  await setMarkers(model, (await lsp.Diagnose(model.place.path)) ?? []);
}

/** setMarkers keeps a model's problems and shows them as its markers. */
async function setMarkers(model: Model, diagnostics: Diagnostic[]): Promise<void> {
  const codes = (await loadQuery(codesQuery)) ?? [];
  if (model.model.isDisposed()) return;
  model.problems = diagnostics;
  const markers = diagnostics.map((diagnostic) => ({
    ...diagnostic.range,
    severity:
      SEVERITIES[codes.find((row) => row.id === diagnostic.code)?.level ?? "info"] ?? monaco.MarkerSeverity.Info,
    message: describeFinding(diagnostic, codes),
    code: diagnostic.code,
  }));
  monaco.editor.setModelMarkers(model.model, "pmt", markers);
}

/**
 * applyChange brings a change on disk into its file's model, at once if the model has no unsaved text. Otherwise the
 * model keeps the disk's text beside its own until the user keeps one; a disk back at the text the model last agreed
 * with drops it.
 */
async function applyChange(path: string): Promise<void> {
  const place = await loadQuery(placeQuery(path));
  const model = place && models.get(place.key);
  if (!model) return;
  const cache = useQueryCache();
  const { data: file } = await cache.fetch(cache.ensure(fileQuery(path)));
  const snapshot = disk.get(model.place.key);
  if (!file || !snapshot) return;
  // a write this window made reaches it as a change too
  if (file.text.text === snapshot.text) model.diskText = null;
  else if (model.unsaved) model.diskText = file.text;
  else setText(model, file.text);
}

/**
 * setText puts text from disk in a model as one undoable edit. The model then matches the disk, and drops a disk text
 * it kept.
 */
export function setText(model: Model, text: Text): void {
  applyEdits(model, [{ range: model.model.getFullModelRange(), text: text.text }]);
  model.encoding = text.encoding;
  disk.set(model.place.key, { version: model.model.getAlternativeVersionId(), text: text.text });
  model.unsaved = false;
  model.diskText = null;
}
