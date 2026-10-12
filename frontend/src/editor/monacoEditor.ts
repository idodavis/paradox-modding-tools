/**
 * The IDE's editors over Monaco (lib/code.ts). A file is one model, shared by every group that shows it: its text
 * is told to IdeService, whose columns are UTF-8 bytes (editorCore.ts converts), and its problems come back as
 * markers. The Paradox languages' providers answer from IdeService; a definition in another file opens its tab.
 * The hover card is the IDE's own (Monaco's hover draws only Markdown), shown from an editor's `hover`.
 */
import { shallowRef, watch } from "vue";
import { Events } from "@wailsio/runtime";
import { ReadText, WriteText } from "@services/fileservice";
import * as rpc from "@services/ideservice";
import type {
  CodeAction,
  Completion,
  Inspect,
  Item,
  Location,
  Range as ByteRange,
  TextEdit,
} from "@services/internal/lsp";
import type { FileText, WindowTabs } from "@services/models";
import { pathKey } from "../composables/useIdeRoots";
import { fileUri, labels, languageOf, models, monaco } from "../lib/monaco";
import { useEditorStore } from "../stores/editor";
import { tabOf } from "./tabs";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import { useCommandsStore } from "../stores/commands";
import { fileName } from "../lib/format";
import { useWindowsStore } from "../stores/windows";
import { watchConflicts } from "./conflicts";
import { editorRange, toBytes } from "./columns";

/**
 * A file's model and what the IDE keeps of it: its path as the tabs spell it, its byte order mark, the version last
 * saved, whether a tab holds it (the backend then has its text), and its text not yet told to the backend.
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
};

const files = new Map<string, File>();
// the paths the backend's places spell, by their models' address, for files opened from them
const spelled = new Map<string, string>();
let workspaceId = "";
let locked: (path: string) => boolean = () => false;

const editorStore = useEditorStore();
const windowsStore = useWindowsStore();
const commandsStore = useCommandsStore();
const settings = useSettingsStore();
// the providers answer for files only: a compare's sides are models of text alone (inmemory://model/3), which the
// backend would be asked to read as a file
const PARADOX = ["paradox", "paradox-loc", "paradox-gui"].map((language) => ({ language, scheme: "file" }));

/** The workspace the editors are of, and which of its files are read only; another workspace's files go. */
export function setWorkspace(id: string, readOnly: (path: string) => boolean): void {
  locked = readOnly;
  if (id === workspaceId) return;
  workspaceId = id;
  for (const model of monaco.editor.getModels()) if (model.uri.scheme === "file") model.dispose();
  files.clear();
  folders.clear();
  dotted.clear();
  dots.replaceSync("");
}

/** The path a model's address is of. */
function pathAt(uri: monaco.Uri): string {
  const key = uri.toString();
  return files.get(key)?.path ?? spelled.get(key) ?? uri.fsPath;
}
const pathOf = (model: monaco.editor.ITextModel) => pathAt(model.uri);

/** A backend range in the model's columns, 1-based; a file no model holds keeps its byte columns. */
function toRange(model: monaco.editor.ITextModel | null, range: ByteRange): monaco.Range {
  const { start, end } = editorRange(range, (n) =>
    model && n < model.getLineCount() ? model.getLineContent(n + 1) : undefined,
  );
  return new monaco.Range(start.line + 1, start.character + 1, end.line + 1, end.character + 1);
}

/** A position as the backend counts: a 0-based line and UTF-8 byte column. */
export function bytesAt(model: monaco.editor.ITextModel, at: monaco.IPosition): [line: number, col: number] {
  return [at.lineNumber - 1, toBytes(model.getLineContent(at.lineNumber), at.column - 1)];
}

/** A backend place as Monaco's, its path kept for the model it may open, and its origin for a peek's rows. */
function location(site: Location): monaco.languages.Location {
  const uri = fileUri(site.path);
  spelled.set(uri.toString(), site.path);
  marked(uri, site);
  return { uri, range: toRange(monaco.editor.getModel(uri), site.range) };
}

// a peek's file rows say whose they are: the origin's name and the folder in it for their folder, and a dot in the
// origin's color, which Monaco has no hook for, matched by the full path each row announces (themes.css draws it)
const workspace = useWorkspaceStore();
const folders = new Map<string, { origin: string; dir: string }>();
const dotted = new Set<string>();
const dots = new CSSStyleSheet();
document.adoptedStyleSheets = [...document.adoptedStyleSheets, dots];
function marked(uri: monaco.Uri, site: Location): void {
  const folder = uri.with({ path: uri.path.slice(0, uri.path.lastIndexOf("/")) });
  folders.set(folder.toString(), {
    origin: site.origin,
    dir: site.rel.slice(0, Math.max(0, site.rel.lastIndexOf("/"))),
  });
  if (dotted.has(uri.fsPath)) return;
  dotted.add(uri.fsPath);
  const path = uri.fsPath.replaceAll("\\", "\\\\").replaceAll('"', '\\"');
  dots.insertRule(
    `.monaco-list-row[aria-label$="full path ${path}"] { --origin: ${workspace.originColor(site.origin)}; }`,
  );
}
labels.folder = (uri) => {
  const known = folders.get(uri.toString());
  if (!known) return undefined;
  const name = workspace.originName(known.origin);
  return known.dir ? `${name} › ${known.dir}` : name;
};

/** A file's text as read, its encoding with it. */
type Read = Pick<FileText, "text" | "bom"> & Partial<Pick<FileText, "legacy" | "format">>;

/** A file's model, made the first time a tab or a peek reaches it, with the text read. */
function fileModel(path: string, { text, bom, legacy = false, format }: Read): File {
  const uri = fileUri(path);
  const key = uri.toString();
  const known = files.get(key);
  if (known) return known;
  const made = monaco.editor.createModel(text, languageOf(path, format), uri);
  // a file keeps its own indentation; one with none takes the settings'
  made.detectIndentation(settings.indentSpaces, settings.indentSize);
  const file: File = {
    path,
    model: made,
    bom,
    legacy,
    saved: made.getAlternativeVersionId(),
    open: false,
    pending: false,
    sent: Promise.resolve(),
  };
  files.set(key, file);
  made.onDidChangeContent(() => changed(file));
  // a file let go (its tab closed, or handed to another window) sends nothing more
  made.onWillDispose(() => {
    files.delete(key);
    clearTimeout(file.timer);
  });
  watchConflicts(made, path);
  return file;
}

// a peek or a definition reads the file it reaches
models.load = async (uri) => {
  const path = pathAt(uri);
  const read = await ReadText(path);
  return read.reject ? null : fileModel(path, read).model;
};

/** The model a tab shows, its text told to the backend: the one a group or a peek made, else one of the text read. */
export function openFile(path: string, read: Read): monaco.editor.ITextModel {
  const file = fileModel(path, read);
  if (file.open) return file.model;
  file.open = true;
  if (file.model.getAlternativeVersionId() !== file.saved) changed(file);
  else
    file.sent = rpc
      .DidChange(workspaceId, file.path, file.model.getValue())
      .catch(() => undefined)
      .then(() => refresh(file));
  return file.model;
}

function fileOf(model: monaco.editor.ITextModel): File | undefined {
  return files.get(model.uri.toString());
}

/**
 * An edit: the backend hears of it within 200 ms, and the tab is marked while it differs from the file saved; an
 * edit in a peek to a file no tab holds opens its tab behind the one shown.
 */
function changed(file: File): void {
  if (!file.open) {
    openFile(file.path, { text: "", bom: file.bom, legacy: file.legacy });
    return editorStore.addTabHidden(file.path);
  }
  file.pending = true;
  clearTimeout(file.timer);
  file.timer = window.setTimeout(() => void flush(file), 200);
  editorStore.setUnsaved(file.path, file.model.getAlternativeVersionId() !== file.saved);
}

/** Send the edited text to the backend, once, then read its problems again. */
function flush(file: File | undefined): Promise<unknown> {
  if (!file?.pending) return file?.sent ?? Promise.resolve();
  clearTimeout(file.timer);
  file.pending = false;
  const text = file.model.getValue();
  file.sent = rpc.DidChange(workspaceId, file.path, text).catch(() => undefined);
  void file.sent.then(() => refresh(file));
  return file.sent;
}

/** A model's text as the backend has it: sent first if it waits. */
function told(model: monaco.editor.ITextModel): Promise<unknown> {
  return flush(fileOf(model));
}

/** The file's problems as markers and the Problems panel's rows. */
async function refresh(file: File): Promise<void> {
  if (file.model.isDisposed()) return;
  const version = file.model.getVersionId();
  const found = (await rpc.Diagnose(workspaceId, file.path).catch(() => null)) ?? [];
  if (file.model.isDisposed() || file.model.getVersionId() !== version) return;
  editorStore.problems.set(file.path, found);
  monaco.editor.setModelMarkers(
    file.model,
    "pmt",
    found.map((finding) => ({
      ...toRange(file.model, finding.range),
      severity: finding.severity === 1 ? monaco.MarkerSeverity.Error : monaco.MarkerSeverity.Warning,
      message: finding.message,
      code: finding.code,
    })),
  );
}

/** Write a file's text in the encoding it was read in (its byte order mark, Windows-1252), and tell the backend; a
 * game file is never written. */
export async function saveFile(path: string): Promise<void> {
  const file = [...files.values()].find((one) => pathKey(one.path) === pathKey(path));
  if (!file || locked(file.path)) return;
  const text = file.model.getValue();
  const version = file.model.getAlternativeVersionId();
  await WriteText(file.path, text, file.bom, file.legacy);
  await flush(file);
  file.saved = version;
  editorStore.setUnsaved(file.path, file.model.getAlternativeVersionId() !== version);
  void rpc.DidSave(workspaceId, file.path).then(() => refresh(file));
}

/** Every file with unsaved edits written. */
export async function saveAll(): Promise<void> {
  await Promise.all([...editorStore.unsaved].map(saveFile));
}

// a file no tab shows (its own or a diff) lets its model go, once the tabs' editors have let it go
watch(
  () => editorStore.openTabs.map((tab) => pathKey(tabOf(tab).file)),
  (open) => {
    for (const file of [...files.values()]) if (file.open && !open.includes(pathKey(file.path))) file.model.dispose();
  },
  { flush: "post" },
);

// a file changed on disk replaces a model with nothing unsaved, as one edit it can undo
Events.On("fs:changed", async ({ data }) => {
  const file = files.get(fileUri(data.path).toString());
  if (!file || data.deleted || file.model.getAlternativeVersionId() !== file.saved) return;
  const read = await ReadText(file.path);
  const text = read.text;
  if (read.reject || text === file.model.getValue() || file.model.isDisposed()) return;
  change(file.model, [{ range: file.model.getFullModelRange(), text }]);
  file.saved = file.model.getAlternativeVersionId();
  editorStore.setUnsaved(file.path, false);
});

/** A quick fix's edits, each file's in its model (its tab opened), left unsaved to read. */
export async function applyFix(action: CodeAction): Promise<void> {
  for (const [target, edits] of Object.entries(action.edit.changes ?? {})) {
    if (!edits?.length) continue;
    const read = files.has(fileUri(target).toString()) ? { text: "", bom: false } : await ReadText(target);
    if ("reject" in read && read.reject) continue;
    applyEdits(openFile(target, read), edits);
    editorStore.open(target);
  }
}

/** An open file's text in the editor, unsaved edits and all; undefined where no editor holds it. */
export function openText(path: string): string | undefined {
  return files.get(fileUri(path).toString())?.model.getValue();
}

/** An open file's text through an edit, as one change it can undo; a game file is never changed. */
export function editText(path: string, edit: (text: string) => string): void {
  const file = files.get(fileUri(path).toString());
  if (!file?.open || locked(file.path)) return;
  const text = edit(file.model.getValue());
  if (text !== file.model.getValue()) change(file.model, [{ range: file.model.getFullModelRange(), text }]);
}

/** Hand tabs to another window, each with its file's unsaved text; they go from here, the backend keeping them open.
 * Resolved once they are sent. */
export function handOver(to: string, tabs: string[], active = tabs[0] ?? ""): Promise<boolean> {
  const handed = tabs.map((tab) => {
    const file = files.get(fileUri(tabOf(tab).file).toString());
    const unsaved = !!file && file.model.getAlternativeVersionId() !== file.saved;
    return { tab, unsaved, text: unsaved && file ? file.model.getValue() : "" };
  });
  editorStore.dropTabs(tabs);
  return Events.Emit("window:tabs", { to, tabs: handed, active });
}

/** Tabs handed to this window, the last shown one shown, and one dragged here where it was dropped; a file's unsaved
 * text is an edit to the text saved, so its tab is marked and an undo goes back to the file. */
export async function receive(handed: WindowTabs): Promise<void> {
  for (const { tab, unsaved, text } of handed.tabs ?? []) {
    editorStore.addTabHidden(tab);
    const path = tabOf(tab).file;
    if (!unsaved || files.has(fileUri(path).toString())) continue;
    const { model } = fileModel(path, await ReadText(path));
    change(model, [{ range: model.getFullModelRange(), text }]);
  }
  const group = editorStore.group(editorStore.focused);
  if (group?.tabs.includes(handed.active)) group.active = handed.active;
  for (const { tab } of handed.tabs ?? []) {
    const dropped = windowsStore.placing.get(tab);
    windowsStore.placing.delete(tab);
    if (dropped) editorStore.drop(tab, editorStore.focused, dropped.group, dropped.edge, dropped.nextTab);
  }
}

/** A tab into another window: handed on from here, asked of the window holding it, or opened there. */
export function sendTab(to: string, tab: string): Promise<unknown> {
  if (editorStore.openTabs.includes(tab)) return handOver(to, [tab]);
  const holder = [...windowsStore.otherWindows.values()].find((one) => one.tabs?.includes(tab));
  if (holder) return Events.Emit("window:give", { to: holder.name, tab, window: to });
  return Events.Emit("window:tabs", { to, tabs: [{ tab, unsaved: false, text: "" }], active: tab });
}

/** A tab in a new editor window of its own. */
export async function newWindowWith(tab: string): Promise<void> {
  const name = `editor:${Date.now().toString(36)}`;
  await windowsStore.openWindow(`/workspace/${workspaceId}`, name, fileName(tabOf(tab).file));
  await sendTab(name, tab);
}

/** A window whose explorer shows folders (the game's files, or one folder), given this window's tabs of files in
 * them. */
export async function openFilesWindow(name: string, title: string, folders: string[]): Promise<void> {
  await windowsStore.openWindow(`/workspace/${workspaceId}`, name, title);
  const keys = folders.map(pathKey);
  const tabs = editorStore.openTabs.filter((tab) => keys.some((key) => pathKey(tabOf(tab).file).startsWith(`${key}/`)));
  if (tabs.length) await handOver(name, tabs);
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

/** A card says something when a field besides its place (within, range) is set. */
export function known(card: Inspect): boolean {
  return Object.entries(card).some(
    ([key, value]) =>
      !["within", "range", "winner"].includes(key) &&
      value !== undefined &&
      value !== null &&
      value !== "" &&
      value !== false &&
      !(Array.isArray(value) && !value.length),
  );
}

/** A card as the completion list's details, under the item's kind (its detail): its scope, its text and prose. */
function details(card: Inspect): string {
  const scope = [card.scope?.this && `${card.scope.this} scope`, card.returns && `returns ${card.returns}`]
    .filter(Boolean)
    .join(", ");
  return [scope, card.name && `> ${card.name}`, card.prose, card.about].filter(Boolean).join("\n\n");
}

// the backend's completions at a place, asked once per edit and place; the list and the prediction share it
let listed: { key: string; list: Promise<Completion | null> } | undefined;
function complete(model: monaco.editor.ITextModel, at: monaco.IPosition): Promise<Completion | null> {
  const key = `${model.uri}:${model.getVersionId()}:${at.lineNumber}:${at.column}`;
  if (listed?.key !== key) {
    const [line, col] = bytesAt(model, at);
    listed = {
      key,
      list: told(model).then(() => rpc.Complete(workspaceId, pathOf(model), line, col).catch(() => null)),
    };
  }
  return listed.list;
}
const items = new WeakMap<monaco.languages.CompletionItem, { path: string; item: Item }>();
// a snippet's $0 is the caret; any other $ is text
const snippet = (text: string) =>
  text
    .replace(/\\/g, "\\\\")
    .replace(/\$(?!0)/g, "\\$")
    .replace(/}/g, "\\}");

monaco.languages.registerCompletionItemProvider(PARADOX, {
  triggerCharacters: [".", "@", "$", ":", "["],
  async provideCompletionItems(model, at) {
    const list = await complete(model, at);
    const path = pathOf(model);
    const suggestions = (list?.items ?? []).map((item) => {
      const suggestion: monaco.languages.CompletionItem = {
        label: item.label,
        detail: item.head,
        kind: monaco.languages.CompletionItemKind.Value,
        insertText: item.snippet ? snippet(item.snippet) : item.label,
        insertTextRules: item.snippet ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
        range: toRange(model, item.range),
        sortText: String(item.rank).padStart(6, "0"),
      };
      items.set(suggestion, { path, item });
      return suggestion;
    });
    return { suggestions, incomplete: list?.incomplete };
  },
  async resolveCompletionItem(suggestion) {
    const from = items.get(suggestion);
    const card = from && (await rpc.ResolveCompletion(workspaceId, from.path, from.item).catch(() => null));
    return card ? { ...suggestion, documentation: { value: details(card) } } : suggestion;
  },
});

// predicted text: the rest of the list's first item, at the end of the word typed so far
monaco.languages.registerInlineCompletionsProvider(PARADOX, {
  async provideInlineCompletions(model, at) {
    if (!settings.on("ide.predict") || locked(pathOf(model))) return { items: [] };
    const top = (await complete(model, at))?.items?.[0];
    const row = model.getLineContent(at.lineNumber);
    const start = top && toRange(model, top.range);
    const typed = start?.startLineNumber === at.lineNumber ? row.slice(start.startColumn - 1, at.column - 1) : "";
    if (
      !top ||
      !start ||
      !typed ||
      /[\w.:@$]/.test(row[at.column - 1] ?? "") ||
      !top.label.toLowerCase().startsWith(typed.toLowerCase())
    )
      return { items: [] };
    return {
      items: [
        { insertText: top.label, range: new monaco.Range(at.lineNumber, start.startColumn, at.lineNumber, at.column) },
      ],
    };
  },
  disposeInlineCompletions() {},
});

monaco.languages.registerSignatureHelpProvider(PARADOX, {
  signatureHelpTriggerCharacters: ["{", "("],
  async provideSignatureHelp(model, at) {
    await told(model);
    const [line, col] = bytesAt(model, at);
    const signature = await rpc.SignatureHelp(workspaceId, pathOf(model), line, col).catch(() => null);
    if (!signature?.label) return null;
    const parameters = (signature.parameters ?? []).map((label) => ({ label }));
    return {
      value: {
        signatures: [{ label: signature.label, parameters, documentation: signature.docs }],
        activeSignature: 0,
        activeParameter: 0,
      },
      dispose() {},
    };
  },
});

monaco.languages.registerDefinitionProvider(PARADOX, {
  async provideDefinition(model, at) {
    await told(model);
    const [line, col] = bytesAt(model, at);
    return ((await rpc.Definition(workspaceId, pathOf(model), line, col).catch(() => null)) ?? []).map(location);
  },
});

monaco.languages.registerReferenceProvider(PARADOX, {
  async provideReferences(model, at) {
    await told(model);
    const [line, col] = bytesAt(model, at);
    return ((await rpc.References(workspaceId, pathOf(model), line, col).catch(() => null)) ?? []).map(location);
  },
});

// F2: the name renamed everywhere the mods write it, through the quick fixes' edits (each file's tab opened,
// unsaved), or the reason it cannot be in Monaco's box
monaco.languages.registerRenameProvider(PARADOX, {
  async provideRenameEdits(model, at, name) {
    await told(model);
    const [line, col] = bytesAt(model, at);
    try {
      const edit = await rpc.Rename(workspaceId, pathOf(model), line, col, name);
      if (edit) await applyFix({ title: `Rename to ${name}`, edit });
      return { edits: [] };
    } catch (error) {
      return { edits: [], rejectReason: error instanceof Error ? error.message : String(error) };
    }
  },
});

monaco.languages.registerDocumentFormattingEditProvider(PARADOX, {
  async provideDocumentFormattingEdits(model) {
    await told(model);
    const edits = (await rpc.FormatDocument(workspaceId, pathOf(model)).catch(() => null)) ?? [];
    return edits.map((edit) => ({ range: toRange(model, edit.range), text: edit.newText }));
  },
});

// a block folds from its opening line to the line before its closing brace, which stays in view
monaco.languages.registerFoldingRangeProvider(PARADOX, {
  async provideFoldingRanges(model) {
    await told(model);
    const ranges = (await rpc.FoldingRanges(workspaceId, pathOf(model)).catch(() => null)) ?? [];
    return ranges.map((range) => ({ start: range.startLine + 1, end: range.endLine }));
  },
});

monaco.languages.registerCodeActionProvider(PARADOX, {
  async provideCodeActions(model, range) {
    await told(model);
    const [line, col] = bytesAt(model, range.getStartPosition());
    const found = (await rpc.CodeActions(workspaceId, pathOf(model), line, col).catch(() => null)) ?? [];
    const actions = found.map((action) => ({
      title: action.title,
      kind: "quickfix",
      command: { id: "pmt.applyFix", title: action.title, arguments: [action] },
    }));
    return { actions, dispose() {} };
  },
});
monaco.editor.registerCommand("pmt.applyFix", (_, action: CodeAction) => void applyFix(action));

// a date's era after it, where the mods declare a calendar; asked again when the session is built again
const hintsChanged = new monaco.Emitter<void>();
monaco.languages.registerInlayHintsProvider(PARADOX, {
  onDidChangeInlayHints: hintsChanged.event,
  async provideInlayHints(model) {
    await told(model);
    const hints = (await rpc.InlayHints(workspaceId, pathOf(model)).catch(() => null)) ?? [];
    return {
      hints: hints.map((hint) => {
        const at = toRange(model, { start: hint.position, end: hint.position });
        return { label: hint.label, position: { lineNumber: at.startLineNumber, column: at.startColumn } };
      }),
      dispose() {},
    };
  },
});

/** The session was built again (its inputs changed, a calendar among them): each open file's text is told to it
 * again, its problems read again, and the era hints asked again. */
export function rebuilt(): void {
  for (const file of files.values()) {
    if (file.open)
      file.sent = rpc
        .DidChange(workspaceId, file.path, file.model.getValue())
        .catch(() => undefined)
        .then(() => refresh(file));
  }
  hintsChanged.fire();
}

// a definition in another file opens its tab there
monaco.editor.registerEditorOpener({
  openCodeEditor(_, resource, at) {
    const target = monaco.editor.getModel(resource);
    const place = at && ("startLineNumber" in at ? { lineNumber: at.startLineNumber, column: at.startColumn } : at);
    const [line, col] = place && target ? bytesAt(target, place) : [0, 0];
    editorStore.open(spelled.get(resource.toString()) ?? resource.fsPath, { line, col });
    return true;
  },
});

// a game file is read only in every editor that shows it, a peek's too
monaco.editor.onDidCreateEditor((editor) => {
  const lock = () => {
    const shown = editor.getModel();
    if (shown && !editor.getOption(monaco.editor.EditorOption.inDiffEditor))
      editor.updateOptions({ readOnly: locked(pathOf(shown)) });
  };
  lock();
  editor.onDidChangeModel(lock);
});

// the app's palette keys open the palette in an editor too, where Monaco would take them
monaco.editor.registerCommand("pmt.palette", () => commandsStore.openPalette());
monaco.editor.registerCommand("pmt.files", () => commandsStore.openPalette(""));
monaco.editor.addKeybindingRules([
  { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyK, command: "pmt.palette" },
  { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.KeyP, command: "pmt.palette" },
  { keybinding: monaco.KeyCode.F1, command: "pmt.palette" },
  { keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyP, command: "pmt.files" },
]);

/** What the IDE's settings set in its editors. */
export function editorOptions(): monaco.editor.IStandaloneEditorConstructionOptions {
  const on = settings.on;
  return {
    ...settings.codeFont,
    wordWrap: on("ide.wordWrap") ? "on" : "off",
    autoClosingBrackets: on("ide.closeBrackets") ? "languageDefined" : "never",
    autoClosingQuotes: on("ide.closeBrackets") ? "languageDefined" : "never",
    quickSuggestions: on("ide.complete") ? { other: true, comments: false, strings: true } : false,
    inlineSuggest: { enabled: on("ide.predict") },
    parameterHints: { enabled: on("ide.signature") },
    minimap: { enabled: on("ide.minimap") },
    folding: on("ide.folding"),
    guides: { indentation: on("ide.indentGuides"), bracketPairs: false },
    bracketPairColorization: { enabled: on("ide.bracketColors") },
    renderWhitespace: on("ide.whitespace") ? "all" : "selection",
  };
}

// the editor last typed in, which the palette's commands run in
let last: monaco.editor.IStandaloneCodeEditor | undefined;

/** Run one of Monaco's actions in the editor last typed in. */
export function runAction(id: string): void {
  last?.focus();
  last?.trigger("palette", id, null);
}

/** An editor of a file's model in a host, the hover card's content and place in its `hover`. */
export function createEditor(host: HTMLElement, shown: monaco.editor.ITextModel) {
  const editor = monaco.editor.create(host, {
    model: shown,
    ...editorOptions(),
    automaticLayout: true,
    contextmenu: false,
    hover: { enabled: "off" },
    fixedOverflowWidgets: true,
    wordBasedSuggestions: "off",
    suggest: { showIcons: false, preview: true },
    unicodeHighlight: { ambiguousCharacters: false, invisibleCharacters: false },
    scrollBeyondLastLine: false,
    padding: { top: 6 },
    lineNumbersMinChars: 4,
    glyphMargin: false,
    renderLineHighlight: "all",
  });
  const hover = shallowRef<{ card: Inspect | null; problems: monaco.editor.IMarker[]; anchor: DOMRect } | null>(null);
  let showing: number | undefined;
  let hiding: number | undefined;
  let hovered: monaco.IPosition | null = null;
  // the pointer is on the card: Monaco reads its first move there as leaving the editor, which must not close it
  let held = false;
  // the editor's menu is open: it opens at the pointer, whose first moves toward it are still on the word
  let muted = false;

  /** Let the card go a moment after the pointer leaves its word, unless it is on the card by then. */
  function letGo(): void {
    clearTimeout(hiding);
    hiding = window.setTimeout(() => held || (hover.value = null), 300);
  }
  /** Keep the card open while the pointer is on it; let it go a moment after it leaves. */
  function holdHover(hold: boolean): void {
    held = hold;
    clearTimeout(showing);
    if (!hold) letGo();
  }

  async function showHover(at: monaco.IPosition): Promise<void> {
    const word = shown.getWordAtPosition(at);
    const start = word && editor.getScrolledVisiblePosition({ lineNumber: at.lineNumber, column: word.startColumn });
    const end = word && editor.getScrolledVisiblePosition({ lineNumber: at.lineNumber, column: word.endColumn });
    if (!start || !end) return;
    await told(shown);
    if (shown.isDisposed()) return;
    const [line, col] = bytesAt(shown, at);
    const card = await rpc.Hover(workspaceId, pathOf(shown), line, col).catch(() => null);
    if (hovered !== at) return;
    // a problem shows where the backend has no card for the word (plain text in a localization value)
    const problems = monaco.editor
      .getModelMarkers({ resource: shown.uri })
      .filter((marker) => monaco.Range.containsPosition(marker, at));
    const box = host.getBoundingClientRect();
    const anchor = new DOMRect(
      box.left + start.left,
      box.top + start.top,
      Math.max(end.left - start.left, 1),
      start.height,
    );
    hover.value = (card && known(card)) || problems.length ? { card, problems, anchor } : null;
  }

  // the card shows when the pointer rests on a word for half a second, and goes when it leaves the word; a card
  // shown stays while the pointer is back on its word
  editor.onMouseMove((e) => {
    const at = e.target.type === monaco.editor.MouseTargetType.CONTENT_TEXT ? e.target.position : null;
    const word = at && shown.getWordAtPosition(at);
    const same =
      hovered &&
      word &&
      hovered.lineNumber === at.lineNumber &&
      shown.getWordAtPosition(hovered)?.startColumn === word.startColumn;
    if (same && hover.value) return clearTimeout(hiding);
    clearTimeout(showing);
    hovered = word ? at : null;
    letGo();
    const target = hovered;
    if (target && !muted && settings.on("ide.hover")) showing = window.setTimeout(() => void showHover(target), 500);
  });
  /** Close the card and forget the word, so none shows until the pointer moves again. */
  function closeHover(): void {
    clearTimeout(showing);
    hovered = null;
    held = false;
    hover.value = null;
  }
  /** Close the card, and while muted (the editor's menu is open) show none. */
  function muteHover(mute: boolean): void {
    muted = mute;
    closeHover();
  }
  // any press closes the card
  editor.onMouseDown(closeHover);
  editor.onKeyDown(closeHover);
  editor.onDidScrollChange(closeHover);
  editor.onMouseLeave(letGo);
  /** Tell the IDE where this editor's caret is. */
  function report(): void {
    const position = editor.getPosition();
    if (!position) return;
    const [, col] = bytesAt(shown, position);
    editorStore.caret = { line: position.lineNumber - 1, character: position.column - 1, col };
  }
  editor.onDidFocusEditorText(() => {
    last = editor;
    report();
  });
  // a card about to show goes with its editor (its tab closed, or moved to another window)
  editor.onDidDispose(() => {
    if (last === editor) last = undefined;
    clearTimeout(showing);
    clearTimeout(hiding);
  });
  editor.onDidChangeCursorPosition(() => editor.hasTextFocus() && report());
  return { editor, hover, holdHover, muteHover, report };
}
