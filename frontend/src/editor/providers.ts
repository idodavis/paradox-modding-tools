/**
 * The Paradox languages' features over IdeService, registered once for `file:` models of the three languages (a
 * compare's text models are never asked of the backend, which would read them as files): completion and its
 * prediction, signature help, definition, references, rename, formatting, folding, quick fixes, inlay hints, the
 * colors the engine gives keys, values and names over the grammars' and the colors script writes; what opens a
 * definition in another file; and a game file read only in every editor.
 */
import * as rpc from "@services/ideservice";
import type { CodeAction, Completion, Inspect, Item, Token } from "@services/internal/lsp";
import { CATEGORIES } from "../lib/grammars";
import { monaco, themeChanged } from "../lib/monaco";
import { useEditorStore } from "../stores/editor";
import { useSettingsStore } from "../stores/settings";
import {
  applyFix,
  backendPathAt,
  bytesAt,
  flushed,
  isReadOnly,
  location,
  pathOf,
  sessionBuilt,
  toRange,
  versionOf,
  workspaceId,
} from "./models";

const PARADOX = ["paradox", "paradox-loc", "paradox-gui"].map((language) => ({ language, scheme: "file" }));
// script and GUI files, the ones that write color blocks and take the engine's colors
const SCRIPT = PARADOX.filter(({ language }) => language !== "paradox-loc");
// a script or GUI text model of a diff's, which takes the engine's colors where it is a version of a file (versionModel)
const VERSIONS = SCRIPT.map(({ language }) => ({ language, scheme: "inmemory" }));

/** Whether a card says something: a field besides its place (within, range) and the winner is set. */
export function cardSaysSomething(card: Inspect): boolean {
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
function completionDetails(card: Inspect): string {
  const scope = [card.scope?.this && `${card.scope.this} scope`, card.returns && `returns ${card.returns}`]
    .filter(Boolean)
    .join(", ");
  return [scope, card.name && `> ${card.name}`, card.prose, card.about].filter(Boolean).join("\n\n");
}

// the backend's completions at a place, asked once per edit and place; the list and the prediction share it
let lastCompletion: { key: string; list: Promise<Completion | null> } | undefined;
function complete(model: monaco.editor.ITextModel, position: monaco.IPosition): Promise<Completion | null> {
  const key = `${model.uri.toString()}:${model.getVersionId()}:${position.lineNumber}:${position.column}`;
  if (lastCompletion?.key !== key) {
    const [line, col] = bytesAt(model, position);
    lastCompletion = {
      key,
      list: flushed(model).then(() => rpc.Complete(workspaceId, pathOf(model), line, col)),
    };
  }
  return lastCompletion.list;
}

// each suggestion's file and item, which resolving it asks the backend about
const completionItems = new WeakMap<monaco.languages.CompletionItem, { path: string; item: Item }>();

// a snippet's $0 is the caret; any other $ is text
const snippet = (text: string) =>
  text
    .replace(/\\/g, "\\\\")
    .replace(/\$(?!0)/g, "\\$")
    .replace(/}/g, "\\}");

/** Ask the backend at a model's position, once its text has gone; the call's rejection reaches Monaco. */
async function asked<T>(
  model: monaco.editor.ITextModel,
  position: monaco.IPosition,
  call: (path: string, line: number, col: number) => Promise<T>,
): Promise<T> {
  await flushed(model);
  const [line, col] = bytesAt(model, position);
  return call(pathOf(model), line, col);
}

// the categories by their place in the legend Monaco is given; the engine's are four of them
const LEGEND = { tokenTypes: Object.keys(CATEGORIES), tokenModifiers: [] };

// the engine's colors are asked again for every open file once the session is built again, and at once when the theme
// changes, since Monaco clears a model's colors then and would wait before asking
const colorsChanged = new monaco.Emitter<void>();
sessionBuilt.event(() => colorsChanged.fire());
themeChanged.event(() => colorsChanged.fire());

/** The engine's tokens as Monaco takes them: per token its line and start, each counted from the token before, its
 * length and its category's place in the legend, in editor columns (toRange). */
function semanticTokens(model: monaco.editor.ITextModel, tokens: Token[]): Uint32Array {
  const data: number[] = [];
  let previousLine = 0;
  let previousColumn = 0;
  for (const token of tokens) {
    const start = { line: token.line, character: token.col };
    const range = toRange(model, { start, end: { ...start, character: token.col + token.length } });
    const column = range.startColumn - 1;
    data.push(
      token.line - previousLine,
      token.line === previousLine ? column - previousColumn : column,
      range.endColumn - range.startColumn,
      LEGEND.tokenTypes.indexOf(token.category),
      0,
    );
    previousLine = token.line;
    previousColumn = column;
  }
  return new Uint32Array(data);
}

// the engine's tokens of each file model and the model's version they were asked at, which the hover card's badge reads
const engineTokens = new WeakMap<monaco.editor.ITextModel, { version: number; tokens: Token[] }>();

/** A provider of the engine's colors, asking for a model's tokens by `tokensOf`; none leaves the grammar's alone. */
function colorProvider(
  tokensOf: (model: monaco.editor.ITextModel) => Promise<Token[] | null>,
): monaco.languages.DocumentSemanticTokensProvider {
  return {
    onDidChange: colorsChanged.event,
    getLegend: () => LEGEND,
    async provideDocumentSemanticTokens(model) {
      const tokens = await tokensOf(model);
      return tokens && { data: semanticTokens(model, tokens) };
    },
    releaseDocumentSemanticTokens() {},
  };
}

/** The category the code at a position is colored in: the engine's token there, else the grammar's. */
export function categoryAt(model: monaco.editor.ITextModel, position: monaco.IPosition): string | undefined {
  const [line, col] = bytesAt(model, position);
  const latest = engineTokens.get(model);
  const token =
    latest?.version === model.getVersionId()
      ? latest.tokens.find(
          (candidate) => candidate.line === line && candidate.col <= col && col < candidate.col + candidate.length,
        )
      : undefined;
  if (token) return token.category;
  const lineTokens = monaco.editor.tokenize(model.getLineContent(position.lineNumber), model.getLanguageId())[0] ?? [];
  const grammarToken = lineTokens.findLast((candidate) => candidate.offset < position.column);
  return grammarToken && grammarToken.type in CATEGORIES ? grammarToken.type : undefined;
}

/** Register the languages' providers, once per window. */
export function installProviders(): void {
  const settings = useSettingsStore();
  const editor = useEditorStore();

  monaco.languages.registerCompletionItemProvider(PARADOX, {
    triggerCharacters: [".", "@", "$", ":", "["],
    async provideCompletionItems(model, position) {
      const list = await complete(model, position);
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
        completionItems.set(suggestion, { path, item });
        return suggestion;
      });
      return { suggestions, incomplete: list?.incomplete };
    },
    async resolveCompletionItem(suggestion) {
      const entry = completionItems.get(suggestion);
      const card = entry && (await rpc.ResolveCompletion(workspaceId, entry.path, entry.item));
      return card ? { ...suggestion, documentation: { value: completionDetails(card) } } : suggestion;
    },
  });

  // predicted text: the rest of the list's first item, at the end of the word typed so far
  monaco.languages.registerInlineCompletionsProvider(PARADOX, {
    async provideInlineCompletions(model, position) {
      if (!settings.on("ide.predict") || isReadOnly(pathOf(model))) return { items: [] };
      const top = (await complete(model, position))?.items?.[0];
      const row = model.getLineContent(position.lineNumber);
      const start = top && toRange(model, top.range);
      const typed =
        start?.startLineNumber === position.lineNumber ? row.slice(start.startColumn - 1, position.column - 1) : "";
      if (
        !top ||
        !start ||
        !typed ||
        /[\w.:@$]/.test(row[position.column - 1] ?? "") ||
        !top.label.toLowerCase().startsWith(typed.toLowerCase())
      )
        return { items: [] };
      const range = new monaco.Range(position.lineNumber, start.startColumn, position.lineNumber, position.column);
      return { items: [{ insertText: top.label, range }] };
    },
    disposeInlineCompletions() {},
  });

  monaco.languages.registerSignatureHelpProvider(PARADOX, {
    signatureHelpTriggerCharacters: ["{", "("],
    async provideSignatureHelp(model, position) {
      const signature = await asked(model, position, (path, line, col) =>
        rpc.SignatureHelp(workspaceId, path, line, col),
      );
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
    async provideDefinition(model, position) {
      const sites = await asked(model, position, (path, line, col) => rpc.Definition(workspaceId, path, line, col));
      return (sites ?? []).map(location);
    },
  });

  monaco.languages.registerReferenceProvider(PARADOX, {
    async provideReferences(model, position) {
      const sites = await asked(model, position, (path, line, col) => rpc.References(workspaceId, path, line, col));
      return (sites ?? []).map(location);
    },
  });

  // F2: the name renamed everywhere the mods write it, through the quick fixes' edits (each file's tab opened,
  // unsaved), or the reason it cannot be in Monaco's box
  monaco.languages.registerRenameProvider(PARADOX, {
    async provideRenameEdits(model, position, name) {
      try {
        const edit = await asked(model, position, (path, line, col) => rpc.Rename(workspaceId, path, line, col, name));
        if (edit) await applyFix({ title: `Rename to ${name}`, edit });
        return { edits: [] };
      } catch (error) {
        return { edits: [], rejectReason: error instanceof Error ? error.message : String(error) };
      }
    },
  });

  monaco.languages.registerDocumentFormattingEditProvider(PARADOX, {
    async provideDocumentFormattingEdits(model) {
      await flushed(model);
      const edits = (await rpc.FormatDocument(workspaceId, pathOf(model))) ?? [];
      return edits.map((edit) => ({ range: toRange(model, edit.range), text: edit.newText }));
    },
  });

  // a block folds from its opening line to the line before its closing brace, which stays in view
  monaco.languages.registerFoldingRangeProvider(PARADOX, {
    async provideFoldingRanges(model) {
      await flushed(model);
      const ranges = (await rpc.FoldingRanges(workspaceId, pathOf(model))) ?? [];
      return ranges.map((range) => ({ start: range.startLine + 1, end: range.endLine }));
    },
  });

  monaco.languages.registerCodeActionProvider(PARADOX, {
    async provideCodeActions(model, range) {
      const fixes = await asked(model, range.getStartPosition(), (path, line, col) =>
        rpc.CodeActions(workspaceId, path, line, col),
      );
      const actions = (fixes ?? []).map((action) => ({
        title: action.title,
        kind: "quickfix",
        command: { id: "pmt.applyFix", title: action.title, arguments: [action] },
      }));
      return { actions, dispose() {} };
    },
  });
  monaco.editor.registerCommand("pmt.applyFix", (_, action: CodeAction) => void applyFix(action));

  monaco.languages.registerInlayHintsProvider(PARADOX, {
    onDidChangeInlayHints: sessionBuilt.event,
    async provideInlayHints(model) {
      await flushed(model);
      const hints = (await rpc.InlayHints(workspaceId, pathOf(model))) ?? [];
      return {
        hints: hints.map((hint) => {
          const range = toRange(model, { start: hint.position, end: hint.position });
          return { label: hint.label, position: { lineNumber: range.startLineNumber, column: range.startColumn } };
        }),
        dispose() {},
      };
    },
  });

  // the colors a file writes, a swatch before each, and a color picked written back in its block's own form
  monaco.languages.registerColorProvider(SCRIPT, {
    async provideDocumentColors(model) {
      await flushed(model);
      const swatches = (await rpc.Colors(workspaceId, pathOf(model))) ?? [];
      return swatches.map(({ range, color }) => ({
        range: toRange(model, range),
        color: { red: color.r, green: color.g, blue: color.b, alpha: color.a },
      }));
    },
    async provideColorPresentations(model, { color, range }) {
      const block = model.getValueInRange(range);
      const text = await rpc.Recolor(block, { r: color.red, g: color.green, b: color.blue, a: color.alpha });
      return [{ label: text, textEdit: { range, text } }];
    },
  });

  // the engine's colors of a script or GUI file over its grammar's: what only the walk tells (lsp.Tokens)
  monaco.languages.registerDocumentSemanticTokensProvider(
    SCRIPT,
    colorProvider(async (model) => {
      await flushed(model);
      const version = model.getVersionId();
      const tokens = (await rpc.Tokens(workspaceId, pathOf(model))) ?? [];
      engineTokens.set(model, { version, tokens });
      return tokens;
    }),
  );
  // and of a version of one a diff shows, its text sent whole (lsp.TextTokens); another text model keeps the grammar's
  monaco.languages.registerDocumentSemanticTokensProvider(
    VERSIONS,
    colorProvider(async (model) => {
      const path = versionOf(model);
      return path ? ((await rpc.TextTokens(workspaceId, path, model.getValue())) ?? []) : null;
    }),
  );

  // a definition in another file opens its tab there
  monaco.editor.registerEditorOpener({
    openCodeEditor(_, resource, selection) {
      const target = monaco.editor.getModel(resource);
      const position =
        selection &&
        ("startLineNumber" in selection
          ? { lineNumber: selection.startLineNumber, column: selection.startColumn }
          : selection);
      const [line, col] = position && target ? bytesAt(target, position) : [0, 0];
      editor.open(backendPathAt(resource) ?? resource.fsPath, { line, col });
      return true;
    },
  });

  // a game file is read only in every editor that shows it, a peek's too
  monaco.editor.onDidCreateEditor((created) => {
    const lock = () => {
      const model = created.getModel();
      if (model && !created.getOption(monaco.editor.EditorOption.inDiffEditor))
        created.updateOptions({ readOnly: isReadOnly(pathOf(model)) });
    };
    lock();
    created.onDidChangeModel(lock);
  });
}
