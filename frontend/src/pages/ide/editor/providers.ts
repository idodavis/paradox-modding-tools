/**
 * Monaco's language providers over the editor service for the Paradox languages, one registration per feature, each
 * handing the service's answer to Monaco as it is, since Go answers in Monaco's ranges. The files of each definition
 * and reference found are kept, so a peek can show whose files they are.
 */
import * as monaco from "monaco-editor/editor/editor.api";
import type { languages } from "monaco-editor";
import { watch } from "vue";

import * as lsp from "@services/lsp/service";
import type { Completion, Edit, Fix, Site, Symbol } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import { fileUrl, loadQuery, placeQuery } from "../../../queries";
import { useEditorStore } from "../../../stores/editor";
import { useSettingsStore } from "../../../stores/settings";
import { useWorkspaceStore } from "../../../stores/workspace";
import { openModel, findModel } from "./models";

/** The id of the command a quick fix of the lightbulb runs. */
const FIX_COMMAND = "pmt.applyFix";
/** The setting that turns off each kind of inlay hint, by the kind Go gives it. A kind with none is always shown. */
const HINT_SETTINGS: Record<string, string> = { text: "ide.textHints" };
/** The widest a picture beside a completion that names an image file is drawn, in pixels. */
const PICTURE_WIDTH = 96;

/** SHOW_DETAILS is the editor action that opens the details beside the completion list while they are closed. */
export const SHOW_DETAILS: monaco.editor.IActionDescriptor = {
  id: "pmt.showSuggestionDetails",
  label: "Show the details beside the completion list",
  precondition: "suggestWidgetVisible && !suggestWidgetDetailsVisible",
  run: (codeEditor) => codeEditor.trigger("pmt", "toggleSuggestionDetails", null),
};

/** An ImageCompletion is a completion item that names an image file, kept with the file's full path. */
type ImageCompletion = languages.CompletionItem & { image?: string };

// the place of a file in each folder that definitions or references were found in, by the folder's address
const folders = new Map<string, Place>();
// the rule that colors the dot before each such file's name in a peek, by the file's path, and their sheet
const dots = new Map<string, string>();
const dotSheet = new CSSStyleSheet();

/**
 * Monaco's labels of files and folders. A peek names the folder of a definition or reference by its origin, as a list
 * of files does. Any other folder or file is named by its path.
 */
export const labelService = {
  getUriLabel: (uri: monaco.Uri): string => {
    const place = folders.get(uri.toString());
    if (place) return useWorkspaceStore().describeFolder(place);
    return uri.scheme === "file" ? uri.fsPath : uri.path;
  },
  getUriBasenameLabel: (uri: monaco.Uri): string => uri.path.slice(uri.path.lastIndexOf("/") + 1),
};

/**
 * Registers every provider for the given languages, which must be registered already, and the sheet of the dots in a
 * peek.
 */
export function registerProviders(languageIds: string[], legend: languages.SemanticTokensLegend): void {
  const selector = languageIds.map((language) => ({ language, scheme: "file" }));
  document.adoptedStyleSheets = [...document.adoptedStyleSheets, dotSheet];
  registerNavigation(selector);
  registerCompletion(selector);
  registerWriting(selector);
  registerDecoration(selector, legend);
}

/** Registers going to a definition and its references, renaming and the outline, and the opener of other files. */
function registerNavigation(selector: languages.LanguageFilter[]): void {
  monaco.languages.registerDefinitionProvider(selector, {
    provideDefinition: async (model, position) =>
      toLocations(await lsp.Definition(model.uri.fsPath, position.lineNumber, position.column)),
  });
  monaco.languages.registerReferenceProvider(selector, {
    provideReferences: async (model, position) =>
      toLocations(await lsp.References(model.uri.fsPath, position.lineNumber, position.column)),
  });
  monaco.languages.registerRenameProvider(selector, {
    resolveRenameLocation: async (model, position) => {
      const range = await lsp.PrepareRename(model.uri.fsPath, position.lineNumber, position.column);
      if (range) return { range, text: model.getValueInRange(range) };
      return { range: monaco.Range.fromPositions(position), text: "", rejectReason: "Nothing here can be renamed." };
    },
    provideRenameEdits: (model, position, name) =>
      lsp
        .Rename(model.uri.fsPath, position.lineNumber, position.column, name)
        .then((edits) => toWorkspaceEdit(edits ?? {}))
        .catch((error: unknown) => ({ edits: [], rejectReason: String(error) })),
  });
  monaco.languages.registerDocumentSymbolProvider(selector, {
    provideDocumentSymbols: async (model) => ((await lsp.ListSymbols(model.uri.fsPath)) ?? []).map(toSymbol),
  });
  // the editor has only the one model, so a definition in another file is shown in the IDE
  monaco.editor.registerEditorOpener({
    openCodeEditor: (_source, resource, selection) => {
      void show(resource, selection);
      return true;
    },
  });
}

/** Registers completion, predicted text and signature help. */
function registerCompletion(selector: languages.LanguageFilter[]): void {
  const settings = useSettingsStore();
  monaco.languages.registerCompletionItemProvider(selector, {
    triggerCharacters: [".", "@", "$", ":", "["],
    provideCompletionItems: async (model, position) => {
      const { word, completions } = await resolveCompletions(model, position);
      const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn);
      const snippet = monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet;
      return {
        suggestions: completions.map((completion): ImageCompletion => ({
          label: completion.label,
          kind: monaco.languages.CompletionItemKind.Property,
          insertText: completion.insert,
          insertTextRules: completion.snippet ? snippet : undefined,
          detail: completion.detail,
          documentation: completion.blurb,
          // the completion the game writes most sorts first
          sortText: String(Number.MAX_SAFE_INTEGER - completion.rank).padStart(16, "0"),
          range,
          image: completion.image,
        })),
      };
    },
    // the picture of an image file is read when its item is the one on show, and shown in the details beside the list,
    // which open for the editor being typed in; Monaco opens them only once asked, and keeps them open
    resolveCompletionItem: async (item: ImageCompletion) => {
      const picture = item.image && settings.isOn("ide.completionPictures") ? await fetchPicture(item.image) : "";
      if (!picture) return item;
      item.documentation = { value: `<img src="${picture}">`, supportHtml: true };
      void monaco.editor
        .getEditors()
        .find((codeEditor) => codeEditor.hasTextFocus())
        ?.getAction(SHOW_DETAILS.id)
        ?.run();
      return item;
    },
  });
  monaco.languages.registerInlineCompletionsProvider(selector, {
    provideInlineCompletions: async (model, position) => {
      const { word, completions } = await resolveCompletions(model, position);
      const top = completions[0];
      if (!top || !word.word || word.endColumn > position.column) return { items: [] };
      const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, position.column);
      return { items: [{ insertText: top.label, range }] };
    },
    disposeInlineCompletions: () => undefined,
  });
  monaco.languages.registerSignatureHelpProvider(selector, {
    signatureHelpTriggerCharacters: [" ", "="],
    provideSignatureHelp: async (model, position) => {
      const signature = await lsp.Signature(model.uri.fsPath, position.lineNumber, position.column);
      if (!signature) return null;
      const parameters = (signature.parameters ?? []).map((label) => ({ label }));
      const value = {
        signatures: [{ label: signature.label, parameters }],
        activeSignature: 0,
        activeParameter: signature.active ?? 0,
      };
      return { value, dispose: () => undefined };
    },
  });
}

/** Registers formatting and quick fixes. */
function registerWriting(selector: languages.LanguageFilter[]): void {
  monaco.languages.registerDocumentFormattingEditProvider(selector, {
    provideDocumentFormattingEdits: async (model, options) =>
      (await lsp.Format(model.uri.fsPath, options.tabSize, options.insertSpaces)) ?? [],
  });
  monaco.languages.registerCodeActionProvider(selector, {
    provideCodeActions: async (model, _range, context) => {
      const none = { actions: [], dispose: () => undefined };
      if (!context.markers.length) return none;
      const place = await loadQuery(placeQuery(model.uri.fsPath));
      const open = place && findModel(place.key);
      const problems = (open?.problems ?? []).filter((problem) =>
        context.markers.some(
          (marker) =>
            marker.code === problem.code &&
            marker.startLineNumber === problem.range.startLineNumber &&
            marker.startColumn === problem.range.startColumn,
        ),
      );
      if (!problems.length) return none;
      const fixes = (await lsp.ListFixes(model.uri.fsPath, problems)) ?? [];
      const actions = fixes.map((fix) => ({
        title: fix.title,
        kind: "quickfix",
        command: { id: FIX_COMMAND, title: fix.title, arguments: [fix] },
      }));
      return { actions, dispose: () => undefined };
    },
  });
  // a fix runs as the menu's does, so that it can write a new file and show where it leaves the caret
  monaco.editor.registerCommand(FIX_COMMAND, (_accessor, fix: Fix) => useEditorStore().applyFix(fix));
}

/** Registers folding, inlay hints, colors and the semantic tokens. */
function registerDecoration(selector: languages.LanguageFilter[], legend: languages.SemanticTokensLegend): void {
  monaco.languages.registerFoldingRangeProvider(selector, {
    provideFoldingRanges: async (model) =>
      ((await lsp.ListFolds(model.uri.fsPath)) ?? []).map(({ start, end }) => ({ start, end })),
  });
  const settings = useSettingsStore();
  const hintsChanged = new monaco.Emitter<void>();
  watch(
    () => Object.values(HINT_SETTINGS).map((key) => settings.isOn(key)),
    () => hintsChanged.fire(),
  );
  monaco.languages.registerInlayHintsProvider(selector, {
    onDidChangeInlayHints: hintsChanged.event,
    provideInlayHints: async (model) => ({
      // Monaco's own padding is a few pixels, so an em space sets the hint further apart from the code
      hints: ((await lsp.ListHints(model.uri.fsPath)) ?? [])
        .filter((hint) => {
          const key = HINT_SETTINGS[hint.kind];
          return !key || settings.isOn(key);
        })
        .map((hint) => ({
          label: `\u{2003}${hint.text}`,
          paddingLeft: true,
          position: { lineNumber: hint.line, column: hint.column },
        })),
      dispose: () => undefined,
    }),
  });
  monaco.languages.registerColorProvider(selector, {
    provideDocumentColors: async (model) =>
      ((await lsp.ListColorSites(model.uri.fsPath)) ?? []).map(({ range, color }) => ({
        range,
        color: { red: color.r, green: color.g, blue: color.b, alpha: color.a },
      })),
    provideColorPresentations: async (model, { range, color }) => [
      {
        label: await lsp.PresentColor(model.uri.fsPath, range, {
          r: color.red,
          g: color.green,
          b: color.blue,
          a: color.alpha,
        }),
      },
    ],
  });
  monaco.languages.registerDocumentSemanticTokensProvider(selector, {
    getLegend: () => legend,
    provideDocumentSemanticTokens: async (model) => ({
      data: Uint32Array.from((await lsp.Tokens(model.uri.fsPath)) ?? []),
    }),
    releaseDocumentSemanticTokens: () => undefined,
  });
}

/** Resolves the completions at a position and the word typed so far, which the completions go on from. */
async function resolveCompletions(
  model: monaco.editor.ITextModel,
  position: monaco.Position,
): Promise<{ word: monaco.editor.IWordAtPosition; completions: Completion[] }> {
  const word = model.getWordUntilPosition(position);
  const completions = await lsp.Complete(model.uri.fsPath, position.lineNumber, position.column);
  return { word, completions: completions ?? [] };
}

/**
 * Fetches the picture of an image file as a data URL, which Monaco's markdown shows where it refuses a relative one.
 * The picture is drawn at most PICTURE_WIDTH wide, a smaller one at its own size, so the URL stays under the 100,000
 * characters Monaco's markdown takes. It is "" if the file cannot be read.
 */
async function fetchPicture(path: string): Promise<string> {
  const response = await fetch(fileUrl(path));
  const bitmap = response.ok ? await createImageBitmap(await response.blob()).catch(() => null) : null;
  if (!bitmap) return "";
  const scale = Math.min(1, PICTURE_WIDTH / bitmap.width);
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(bitmap.width * scale);
  canvas.height = Math.max(1, Math.round(bitmap.height * scale));
  canvas.getContext("2d")?.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  return canvas.toDataURL();
}

/** Shows the file at a resource in the IDE, at the given selection. */
async function show(resource: monaco.Uri, selection?: monaco.IRange | monaco.IPosition): Promise<void> {
  const place = await loadQuery(placeQuery(resource.fsPath));
  if (!place) return;
  const position =
    selection && "startLineNumber" in selection
      ? { lineNumber: selection.startLineNumber, column: selection.startColumn }
      : selection;
  useEditorStore().show(place, position);
}

/** Turns Go's sites into Monaco's locations, and keeps each file's place for a peek of them. */
function toLocations(sites: Site[] | null): languages.Location[] {
  const found = (sites ?? []).map(({ place, range }) => ({ uri: markPlace(place), range }));
  dotSheet.replaceSync([...dots.values()].join("\n"));
  return found;
}

/**
 * Keeps the place of a file a peek may list, and returns the file's address. The peek names the file's folder by its
 * place, and the dot before the file's name takes its origin's color. Monaco gives a peek's rows no hook of their own,
 * so the dot's rule finds the row by the full path its label ends with.
 */
function markPlace(place: Place): monaco.Uri {
  const uri = monaco.Uri.file(place.path);
  folders.set(monaco.Uri.joinPath(uri, "..").toString(), place);
  const label = JSON.stringify(`full path ${uri.fsPath}`);
  const color = useWorkspaceStore().originColor(place.origin);
  dots.set(uri.fsPath, `.monaco-list-row[aria-label$=${label}] { --origin: ${color}; }`);
  return uri;
}

/** Turns Go's edits by path into a workspace edit. The files they change are opened first, so Monaco can edit them. */
async function toWorkspaceEdit(byPath: Record<string, Edit[] | null | undefined>): Promise<languages.WorkspaceEdit> {
  const entries = Object.entries(byPath);
  const places = await Promise.all(entries.map(([path]) => loadQuery(placeQuery(path))));
  await Promise.all(places.flatMap((place) => (place ? [openModel(place)] : [])));
  return {
    edits: entries.flatMap(([path, edits]) =>
      (edits ?? []).map(({ range, text }) => ({
        resource: monaco.Uri.file(path),
        versionId: undefined,
        textEdit: { range, text },
      })),
    ),
  };
}

/** Turns one of Go's symbols and the ones inside it into the outline's. */
function toSymbol(symbol: Symbol): languages.DocumentSymbol {
  return {
    name: symbol.name,
    detail: symbol.database,
    kind: monaco.languages.SymbolKind.Object,
    tags: [],
    range: symbol.range,
    selectionRange: symbol.nameRange,
    children: (symbol.children ?? []).map(toSymbol),
  };
}
