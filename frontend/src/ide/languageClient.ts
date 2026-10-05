/**
 * The editor client: registers the workbench's language providers and document sync over Go's
 * IdeService, turning editorCore's output into vscode types. The only module that knows the
 * workbench API; the backend's positions are UTF-8 byte columns, the editor's UTF-16.
 */
import * as vscode from "vscode";
import { useDebounceFn } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import * as ide from "@services/ideservice";
import type { Inspect, Item, Location, Range, Symbol as Defined } from "@services/internal/lsp";
import { editorRange, hoverMarkdown, toBytes, type CardLinks, type LineText } from "./editorCore";
import { normFs } from "./fsBridge";

const OPEN_EVENT_GRAPH = "pmt.openInEventGraph";
const LANGS = ["paradox", "paradox-gui", "paradox-loc", "paradox-info", "paradox-mod"];

function pathOf(doc: vscode.TextDocument): string {
  const path = doc.uri.fsPath || doc.uri.path;
  return /^\/[A-Za-z]:/.test(path) ? path.slice(1) : path;
}

/** The open document at a backend path, if any: the backend folds case, so match as it does. */
function openDoc(path: string): vscode.TextDocument | undefined {
  const key = normFs(path);
  return vscode.workspace.textDocuments.find((doc) => normFs(doc.uri.fsPath) === key);
}

// The workbench keys editors by exact URI, and the backend's paths come back case-folded on
// Windows; an open document's own URI keeps go-to-definition in the tab already open.
function uriOf(path: string): vscode.Uri {
  return openDoc(path)?.uri ?? vscode.Uri.file(path);
}

function linesOf(doc: vscode.TextDocument | undefined): LineText {
  return (line) => (doc && line >= 0 && line < doc.lineCount ? doc.lineAt(line).text : undefined);
}

function toRange(range: Range, lineText: LineText): vscode.Range {
  const { start, end } = editorRange(range, lineText);
  return new vscode.Range(start.line, start.character, end.line, end.character);
}

function toLocation(site: Location): vscode.Location {
  return new vscode.Location(uriOf(site.path), toRange(site.range, linesOf(openDoc(site.path))));
}

function symbolOf(defined: Defined): vscode.SymbolInformation {
  return new vscode.SymbolInformation(defined.name, vscode.SymbolKind.Object, defined.database, toLocation(defined.location));
}

/** Attach PMT language intelligence to the workbench; the host names origins. Dispose to detach. */
export function registerLanguageClient(getWorkspaceId: () => string, origin: CardLinks["origin"]): vscode.Disposable {
  const subs: vscode.Disposable[] = [];
  const diagnostics = vscode.languages.createDiagnosticCollection("pmt");
  subs.push(diagnostics);
  const ours = (doc: vscode.TextDocument) => !!getWorkspaceId() && LANGS.includes(doc.languageId);
  const at = (doc: vscode.TextDocument, pos: vscode.Position) =>
    [getWorkspaceId(), pathOf(doc), pos.line, toBytes(doc.lineAt(pos.line).text, pos.character)] as const;
  const provide = (register: (lang: string) => vscode.Disposable) =>
    LANGS.forEach((lang) => subs.push(register(lang)));

  const refreshDiagnostics = async (doc: vscode.TextDocument) => {
    if (!ours(doc)) return;
    const found = (await ide.Diagnose(getWorkspaceId(), pathOf(doc)).catch(() => null)) ?? [];
    diagnostics.set(
      doc.uri,
      found.map((finding) => {
        const severity = finding.severity === 1 ? vscode.DiagnosticSeverity.Error : vscode.DiagnosticSeverity.Warning;
        const shown = new vscode.Diagnostic(toRange(finding.range, linesOf(doc)), finding.message, severity);
        // the code names the check a modder can turn off in Editor settings
        shown.source = "PMT";
        shown.code = finding.code;
        return shown;
      }),
    );
  };

  // edits reach the backend at most every 200 ms; a completion sends its own document first
  const dirty = new Map<string, vscode.TextDocument>();
  const flushChanges = useDebounceFn(() => {
    for (const doc of dirty.values()) {
      void ide.DidChange(getWorkspaceId(), pathOf(doc), doc.getText()).then(() => refreshDiagnostics(doc));
    }
    dirty.clear();
  }, 200);

  // a card as the workbench shows it, its sites linking to the editor
  const links: CardLinks = {
    origin,
    open: (site: Location) => {
      const selection = { startLineNumber: site.range.start.line + 1, startColumn: 1 };
      const args = [uriOf(site.path).toString(), { selection }];
      return `command:vscode.open?${encodeURIComponent(JSON.stringify(args))}`;
    },
  };
  const markdownOf = (card: Inspect) => {
    const md = new vscode.MarkdownString(hoverMarkdown(card, links), true);
    md.supportHtml = true;
    md.isTrusted = { enabledCommands: ["vscode.open", OPEN_EVENT_GRAPH] };
    return md;
  };

  provide((lang) =>
    vscode.languages.registerHoverProvider(lang, {
      async provideHover(doc, pos) {
        const card = await ide.Hover(...at(doc, pos));
        if (!card) return null;
        const md = markdownOf(card);
        if (card.graphed) {
          const args = encodeURIComponent(JSON.stringify([card.word]));
          md.appendMarkdown(`\n\n[Open in Event Graph](command:${OPEN_EVENT_GRAPH}?${args})`);
        }
        return new vscode.Hover(md, toRange(card.range, linesOf(doc)));
      },
    }),
  );

  // a mod's era beside the dates its script writes; the event below says when they went stale
  const inlayChanged = new vscode.EventEmitter<void>();
  subs.push(inlayChanged);
  provide((lang) =>
    vscode.languages.registerInlayHintsProvider(lang, {
      onDidChangeInlayHints: inlayChanged.event,
      async provideInlayHints(doc, visible) {
        const hints = (await ide.InlayHints(getWorkspaceId(), pathOf(doc))) ?? [];
        return hints
          .map((hint) => {
            const position = toRange({ start: hint.position, end: hint.position }, linesOf(doc)).start;
            const shown = new vscode.InlayHint(position, hint.label);
            shown.paddingLeft = true;
            return shown;
          })
          .filter((hint) => visible.contains(hint.position));
      },
    }),
  );

  // the backend's item for each completion shown, which ResolveCompletion takes back
  const items = new WeakMap<vscode.CompletionItem, { path: string; item: Item }>();
  provide((lang) =>
    vscode.languages.registerCompletionItemProvider(
      lang,
      {
        async provideCompletionItems(doc, pos, token) {
          dirty.delete(doc.uri.toString());
          await ide.DidChange(getWorkspaceId(), pathOf(doc), doc.getText());
          const list = await ide.Complete(...at(doc, pos));
          if (!list || token.isCancellationRequested) return undefined;
          const shown = (list.items ?? []).map((item, i) => {
            const entry = new vscode.CompletionItem(item.label, vscode.CompletionItemKind.Value);
            entry.detail = item.head;
            // the backend's rank first, then the order it wrote the ties in
            entry.sortText = `${item.rank}${String(i).padStart(4, "0")}`;
            entry.range = toRange(item.range, linesOf(doc));
            if (item.snippet) entry.insertText = new vscode.SnippetString(item.snippet);
            items.set(entry, { path: pathOf(doc), item });
            return entry;
          });
          return new vscode.CompletionList(shown, list.incomplete);
        },
        // the card is fetched for the item the reader is looking at, not for the whole list
        async resolveCompletionItem(entry) {
          const offered = items.get(entry);
          const card = offered && (await ide.ResolveCompletion(getWorkspaceId(), offered.path, offered.item));
          if (card) entry.documentation = markdownOf(card);
          return entry;
        },
      },
      ".",
      "[",
      ":",
    ),
  );
  provide((lang) =>
    vscode.languages.registerSignatureHelpProvider(
      lang,
      {
        async provideSignatureHelp(doc, pos) {
          const signature = await ide.SignatureHelp(...at(doc, pos));
          if (!signature?.label) return undefined;
          const info = new vscode.SignatureInformation(signature.label, signature.docs);
          info.parameters = (signature.parameters ?? []).map((name) => new vscode.ParameterInformation(name));
          return { signatures: [info], activeSignature: 0, activeParameter: 0 };
        },
      },
      "(",
      "{",
    ),
  );
  provide((lang) =>
    vscode.languages.registerDefinitionProvider(lang, {
      provideDefinition: async (doc, pos) => ((await ide.Definition(...at(doc, pos))) ?? []).map(toLocation),
    }),
  );
  provide((lang) =>
    vscode.languages.registerReferenceProvider(lang, {
      provideReferences: async (doc, pos) => ((await ide.References(...at(doc, pos))) ?? []).map(toLocation),
    }),
  );
  provide((lang) =>
    vscode.languages.registerDocumentSymbolProvider(lang, {
      provideDocumentSymbols: async (doc) =>
        ((await ide.DocumentSymbols(getWorkspaceId(), pathOf(doc))) ?? []).map(symbolOf),
    }),
  );
  provide((lang) =>
    vscode.languages.registerDocumentFormattingEditProvider(lang, {
      async provideDocumentFormattingEdits(doc) {
        const edits = (await ide.FormatDocument(getWorkspaceId(), pathOf(doc))) ?? [];
        return edits.map((edit) => new vscode.TextEdit(toRange(edit.range, linesOf(doc)), edit.newText));
      },
    }),
  );
  provide((lang) =>
    vscode.languages.registerFoldingRangeProvider(lang, {
      async provideFoldingRanges(doc) {
        const ranges = (await ide.FoldingRanges(getWorkspaceId(), pathOf(doc))) ?? [];
        const region = vscode.FoldingRangeKind.Region;
        return ranges.map((range) => new vscode.FoldingRange(range.startLine, range.endLine, region));
      },
    }),
  );
  provide((lang) =>
    vscode.languages.registerCodeActionsProvider(lang, {
      async provideCodeActions(doc, range) {
        const actions = (await ide.CodeActions(...at(doc, range.start))) ?? [];
        return actions.map((action) => {
          const fix = new vscode.CodeAction(action.title, vscode.CodeActionKind.QuickFix);
          fix.edit = new vscode.WorkspaceEdit();
          for (const [path, edits] of Object.entries(action.edit.changes ?? {})) {
            for (const edit of edits ?? []) {
              fix.edit.replace(uriOf(path), toRange(edit.range, linesOf(openDoc(path))), edit.newText);
            }
          }
          return fix;
        });
      },
    }),
  );
  subs.push(
    vscode.languages.registerWorkspaceSymbolProvider({
      provideWorkspaceSymbols: async (query) =>
        ((await ide.WorkspaceSymbols(getWorkspaceId(), query)) ?? []).map(symbolOf),
    }),
  );

  // document sync: the backend indexes an open buffer's text in place of the disk's
  const opened = (doc: vscode.TextDocument) => {
    if (!ours(doc)) return;
    void ide.DidChange(getWorkspaceId(), pathOf(doc), doc.getText()).then(() => refreshDiagnostics(doc));
  };
  subs.push(
    vscode.workspace.onDidOpenTextDocument(opened),
    vscode.workspace.onDidChangeTextDocument(({ document: doc }) => {
      if (!ours(doc)) return;
      dirty.set(doc.uri.toString(), doc);
      void flushChanges();
    }),
    vscode.workspace.onDidSaveTextDocument((doc) => {
      if (ours(doc)) void ide.DidSave(getWorkspaceId(), pathOf(doc)).then(() => refreshDiagnostics(doc));
    }),
    vscode.workspace.onDidCloseTextDocument((doc) => {
      if (ours(doc)) void ide.DidClose(getWorkspaceId(), pathOf(doc));
      diagnostics.delete(doc.uri);
    }),
  );
  vscode.workspace.textDocuments.forEach(opened);

  const stopCalendar = Events.On("lang:calendar-changed", () => inlayChanged.fire());
  const stopCache = Events.On("lang:cache-updated", () =>
    vscode.workspace.textDocuments.forEach((doc) => void refreshDiagnostics(doc)),
  );
  subs.push({
    dispose() {
      stopCalendar();
      stopCache();
    },
  });

  // commands.ts pulls in workbenchHost, which registers this module, so it is imported late
  subs.push(
    vscode.commands.registerCommand(OPEN_EVENT_GRAPH, (key?: string) => {
      const id = getWorkspaceId();
      if (id && key) void import("./commands").then(({ openEventGraph }) => openEventGraph(id, key));
    }),
  );
  return { dispose: () => subs.forEach((sub) => sub.dispose()) };
}
