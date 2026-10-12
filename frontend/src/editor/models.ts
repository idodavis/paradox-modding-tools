/**
 * The files the editors hold: one Monaco model per file, shared by every group that shows it. A model's text is sent
 * to the backend within 200 ms (the backend counts UTF-8 bytes; columns.ts converts) and its problems come back as
 * markers; a save writes the encoding the file was read in. Every provider reads it, and it calls the editor store
 * for the tabs.
 */
import { watch } from "vue";
import { Events } from "@wailsio/runtime";
import { ReadText, WriteText } from "@services/fileservice";
import * as rpc from "@services/ideservice";
import type { CodeAction, Location, Range as ByteRange, TextEdit } from "@services/internal/lsp";
import { Log } from "@services/logservice";
import type { FileText } from "@services/models";
import { pathKey } from "../composables/useIdeRoots";
import { fileUri, hooks, languageOf, monaco } from "../lib/monaco";
import { useEditorStore } from "../stores/editor";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import { editorRange, toBytes } from "./columns";
import { watchConflicts } from "./conflicts";
import { tabOf } from "./tabs";

/** A file's text as it was read, its encoding with it. */
type Content = Pick<FileText, "text" | "bom"> & Partial<Pick<FileText, "legacy" | "format">>;

/**
 * A file's model and what the IDE keeps of it: its path as the tabs spell it, its byte order mark and legacy
 * encoding, the version last saved, whether a tab holds it (the backend then has its text), its text not yet sent
 * to the backend, the last call that sent it, and whether a failure of those calls was logged already.
 */
type File = {
  path: string;
  model: monaco.editor.ITextModel;
  bom: boolean;
  legacy: boolean;
  saved: number;
  open: boolean;
  pending: boolean;
  sent: Promise<unknown>;
  timer?: number;
  failed: boolean;
};

/** The workspace the editors are of. */
export let workspaceId = "";
let readOnlyRule: (path: string) => boolean = () => false;

// by a model's address
const files = new Map<string, File>();
// the paths the backend's places spell, for files opened from them
const backendPaths = new Map<string, string>();
// a peek's file rows say whose they are: the origin's name and the folder in it, and a dot in the origin's color,
// which Monaco has no hook for, matched by the full path each row announces (themes.css draws it)
const folders = new Map<string, { origin: string; dir: string }>();
const dotted = new Set<string>();
const dotSheet = new CSSStyleSheet();

/** Asked again when the session is built again: a date's era after it, where the mods declare a calendar. */
export const hintsChanged = new monaco.Emitter<void>();

/** The workspace the editors are of, and which of its files are read only; another workspace's files go. */
export function setWorkspace(id: string, isReadOnlyPath: (path: string) => boolean): void {
  readOnlyRule = isReadOnlyPath;
  if (id === workspaceId) return;
  workspaceId = id;
  for (const model of monaco.editor.getModels()) if (model.uri.scheme === "file") model.dispose();
  files.clear();
  folders.clear();
  dotted.clear();
  dotSheet.replaceSync("");
}

/** Whether a file is read only in the editors: a game's file is never written. */
export const isReadOnly = (path: string): boolean => readOnlyRule(path);

/** The path a model's address is of: the tab's spelling, else the backend's, else the address's own. */
export function pathAt(uri: monaco.Uri): string {
  const key = uri.toString();
  return files.get(key)?.path ?? backendPaths.get(key) ?? uri.fsPath;
}
export const pathOf = (model: monaco.editor.ITextModel): string => pathAt(model.uri);

/** The backend's name for a path, where a place named it before a model was made for it. */
export const backendPathAt = (uri: monaco.Uri): string | undefined => backendPaths.get(uri.toString());

/** A backend range in the model's columns, 1-based; a file no model holds keeps its byte columns. */
export function toRange(model: monaco.editor.ITextModel | null, range: ByteRange): monaco.Range {
  const { start, end } = editorRange(range, (line) =>
    model && line < model.getLineCount() ? model.getLineContent(line + 1) : undefined,
  );
  return new monaco.Range(start.line + 1, start.character + 1, end.line + 1, end.character + 1);
}

/** A position as the backend counts: a 0-based line and UTF-8 byte column. */
export function bytesAt(model: monaco.editor.ITextModel, position: monaco.IPosition): [line: number, col: number] {
  return [position.lineNumber - 1, toBytes(model.getLineContent(position.lineNumber), position.column - 1)];
}

/** A backend place as Monaco's, its path kept for the model it may open, and its folder marked for a peek's rows. */
export function location(site: Location): monaco.languages.Location {
  const uri = fileUri(site.path);
  backendPaths.set(uri.toString(), site.path);
  markFolder(uri, site);
  return { uri, range: toRange(monaco.editor.getModel(uri), site.range) };
}

function markFolder(uri: monaco.Uri, site: Location): void {
  const folder = uri.with({ path: uri.path.slice(0, uri.path.lastIndexOf("/")) });
  folders.set(folder.toString(), {
    origin: site.origin,
    dir: site.rel.slice(0, Math.max(0, site.rel.lastIndexOf("/"))),
  });
  if (dotted.has(uri.fsPath)) return;
  dotted.add(uri.fsPath);
  const path = uri.fsPath.replaceAll("\\", "\\\\").replaceAll('"', '\\"');
  const color = useWorkspaceStore().originColor(site.origin);
  dotSheet.insertRule(`.monaco-list-row[aria-label$="full path ${path}"] { --origin: ${color}; }`);
}

/** Await a call that keeps the backend's copy of a file: a failure is logged once, until a call succeeds. */
async function synced<T>(file: File, call: string, pending: Promise<T>): Promise<T | undefined> {
  try {
    const result = await pending;
    file.failed = false;
    return result;
  } catch (error) {
    if (!file.failed)
      void Log("error", `${call} of ${file.path} failed`, {
        error: error instanceof Error ? error.message : String(error),
      });
    file.failed = true;
    return undefined;
  }
}

/** A file's model, made the first time a tab or a peek reaches it, with the text read. */
function fileModel(path: string, { text, bom, legacy = false, format }: Content): File {
  const uri = fileUri(path);
  const key = uri.toString();
  const existing = files.get(key);
  if (existing) return existing;
  const model = monaco.editor.createModel(text, languageOf(path, format), uri);
  // a file keeps its own indentation; one with none takes the settings'
  const settings = useSettingsStore();
  model.detectIndentation(settings.indentSpaces, settings.indentSize);
  const file: File = {
    path,
    model,
    bom,
    legacy,
    saved: model.getAlternativeVersionId(),
    open: false,
    pending: false,
    sent: Promise.resolve(),
    failed: false,
  };
  files.set(key, file);
  model.onDidChangeContent(() => edited(file));
  // a file let go (its tab closed, or handed to another window) sends nothing more
  model.onWillDispose(() => {
    files.delete(key);
    clearTimeout(file.timer);
  });
  watchConflicts(model, path);
  return file;
}

const fileOf = (model: monaco.editor.ITextModel): File | undefined => files.get(model.uri.toString());

/** The model a tab shows, its text sent to the backend: the one a group or a peek made, else one of the text read. */
export function openFile(path: string, content: Content): monaco.editor.ITextModel {
  const file = fileModel(path, content);
  if (file.open) return file.model;
  file.open = true;
  if (file.model.getAlternativeVersionId() !== file.saved) edited(file);
  else send(file);
  return file.model;
}

/** Tell the backend a file's text, then read its problems again; `sent` is the call. */
function send(file: File): void {
  file.sent = synced(file, "DidChange", rpc.DidChange(workspaceId, file.path, file.model.getValue()));
  void file.sent.then(() => diagnose(file));
}

/**
 * An edit: the backend hears of it within 200 ms, and the tab is marked while it differs from the file saved; an
 * edit in a peek to a file no tab holds opens its tab behind the one shown.
 */
function edited(file: File): void {
  const editor = useEditorStore();
  if (!file.open) {
    openFile(file.path, { text: "", bom: file.bom, legacy: file.legacy });
    return editor.addTabHidden(file.path);
  }
  file.pending = true;
  clearTimeout(file.timer);
  file.timer = window.setTimeout(() => void flush(file), 200);
  editor.setUnsaved(file.path, file.model.getAlternativeVersionId() !== file.saved);
}

/** Send a file's edited text to the backend, once, then read its problems again. */
function flush(file: File | undefined): Promise<unknown> {
  if (!file?.pending) return file?.sent ?? Promise.resolve();
  clearTimeout(file.timer);
  file.pending = false;
  send(file);
  return file.sent;
}

/** A model's text as the backend has it: sent first if it waits. Every provider awaits it before asking. */
export function flushed(model: monaco.editor.ITextModel): Promise<unknown> {
  return flush(fileOf(model));
}

/** The file's problems as markers and the Problems panel's rows. */
async function diagnose(file: File): Promise<void> {
  if (file.model.isDisposed()) return;
  const version = file.model.getVersionId();
  const diagnostics = (await synced(file, "Diagnose", rpc.Diagnose(workspaceId, file.path))) ?? [];
  if (file.model.isDisposed() || file.model.getVersionId() !== version) return;
  useEditorStore().setProblems(file.path, diagnostics);
  monaco.editor.setModelMarkers(
    file.model,
    "pmt",
    diagnostics.map((one) => ({
      ...toRange(file.model, one.range),
      severity: one.severity === 1 ? monaco.MarkerSeverity.Error : monaco.MarkerSeverity.Warning,
      message: one.message,
      code: one.code,
    })),
  );
}

/** Write a file's text in the encoding it was read in (its byte order mark, Windows-1252), and send it to the backend; a
 * game file is never written. */
export async function saveFile(path: string): Promise<void> {
  const key = pathKey(path);
  const file = [...files.values()].find((one) => pathKey(one.path) === key);
  if (!file || isReadOnly(file.path)) return;
  const text = file.model.getValue();
  const version = file.model.getAlternativeVersionId();
  await WriteText(file.path, text, file.bom, file.legacy);
  await flush(file);
  file.saved = version;
  useEditorStore().setUnsaved(file.path, file.model.getAlternativeVersionId() !== version);
  void synced(file, "DidSave", rpc.DidSave(workspaceId, file.path)).then(() => diagnose(file));
}

/** The backend's edits (UTF-8 byte ranges) as one undoable change. */
function applyEdits(model: monaco.editor.ITextModel, edits: TextEdit[]): void {
  change(
    model,
    edits.map((edit) => ({ range: toRange(model, edit.range), text: edit.newText })),
  );
}

/** Edits made for the user, a step of their own that one undo takes back, apart from what was typed before. */
function change(model: monaco.editor.ITextModel, edits: monaco.editor.IIdentifiedSingleEditOperation[]): void {
  model.pushStackElement();
  model.pushEditOperations([], edits, () => null);
  model.pushStackElement();
}

/** A quick fix's edits, each file's in its model (its tab opened), left unsaved to read. */
export async function applyFix(action: CodeAction): Promise<void> {
  const editor = useEditorStore();
  for (const [target, edits] of Object.entries(action.edit.changes ?? {})) {
    if (!edits?.length) continue;
    const content = files.has(fileUri(target).toString()) ? { text: "", bom: false } : await ReadText(target);
    if ("reject" in content && content.reject) continue;
    applyEdits(openFile(target, content), edits);
    editor.open(target);
  }
}

/** An open file's text in the editor, unsaved edits and all; undefined where no editor holds it. */
export function openText(path: string): string | undefined {
  return files.get(fileUri(path).toString())?.model.getValue();
}

/** An open file's text through an edit, as one change it can undo; a game file is never changed. */
export function editText(path: string, edit: (text: string) => string): void {
  const file = files.get(fileUri(path).toString());
  if (!file?.open || isReadOnly(file.path)) return;
  const text = edit(file.model.getValue());
  if (text !== file.model.getValue()) change(file.model, [{ range: file.model.getFullModelRange(), text }]);
}

/** The text of a tab's file when it differs from the file saved; undefined when it does not, or no model holds it. */
export function unsavedText(tab: string): string | undefined {
  const file = files.get(fileUri(tabOf(tab).file).toString());
  return file && file.model.getAlternativeVersionId() !== file.saved ? file.model.getValue() : undefined;
}

/** A tab handed over with unsaved text: the file's model, then the text as an edit, so the tab is marked and an undo
 * goes back to the file. A file with a model here keeps it. */
export async function showText(path: string, text: string): Promise<void> {
  if (files.has(fileUri(path).toString())) return;
  const { model } = fileModel(path, await ReadText(path));
  change(model, [{ range: model.getFullModelRange(), text }]);
}

/** The session was built again (its inputs changed, a calendar among them): each open file's text is sent to it
 * again, its problems read again, and the era hints asked again. */
export function resendOpenFiles(): void {
  for (const file of files.values()) if (file.open) send(file);
  hintsChanged.fire();
}

/** Start keeping the files: the models a peek or a definition reaches are made on demand, a file no tab shows lets its
 * model go, and a file changed on disk replaces a model with nothing unsaved. */
export function installModels(): void {
  const editor = useEditorStore();
  document.adoptedStyleSheets = [...document.adoptedStyleSheets, dotSheet];

  hooks.loadModel = async (uri) => {
    const path = pathAt(uri);
    const text = await ReadText(path);
    return text.reject ? null : fileModel(path, text).model;
  };
  hooks.folderLabel = (uri) => {
    const folder = folders.get(uri.toString());
    if (!folder) return undefined;
    const name = useWorkspaceStore().originName(folder.origin);
    return folder.dir ? `${name} › ${folder.dir}` : name;
  };

  // a file no tab shows (its own or a diff) lets its model go, once the tabs' editors have let it go
  watch(
    () => editor.openTabs.map((tab) => pathKey(tabOf(tab).file)),
    (open) => {
      for (const file of [...files.values()]) if (file.open && !open.includes(pathKey(file.path))) file.model.dispose();
    },
    { flush: "post" },
  );

  // a file changed on disk replaces a model with nothing unsaved, as one edit it can undo
  Events.On("fs:changed", ({ data }) => void replaceFromDisk(data.path, data.deleted));
}

async function replaceFromDisk(path: string, deleted: boolean): Promise<void> {
  const file = files.get(fileUri(path).toString());
  if (!file || deleted || file.model.getAlternativeVersionId() !== file.saved) return;
  const disk = await ReadText(file.path);
  if (disk.reject || disk.text === file.model.getValue() || file.model.isDisposed()) return;
  change(file.model, [{ range: file.model.getFullModelRange(), text: disk.text }]);
  file.saved = file.model.getAlternativeVersionId();
  useEditorStore().setUnsaved(file.path, false);
}
