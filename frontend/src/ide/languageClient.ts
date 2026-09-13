/**
 * Register workbench language providers that call Go IdeService.
 */
import * as vscode from "vscode";
import { useDebounceFn } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import {
  Diagnose,
  Hover,
  Complete,
  SignatureHelp,
  Definition,
  References,
  DocumentSymbols,
  WorkspaceSymbols,
  DidOpen,
  DidChange,
  DidClose,
  DidSave,
  Rename,
  FormatDocument,
  FoldingRanges,
  CodeActions,
  InlayHints,
} from "@services/ideservice";
import type {
  WorkspaceEdit as LspWorkspaceEdit,
  Location as LspLocation,
  HoverResult,
} from "@services/internal/lang/lsp/models";
import { originHexByOriginId } from "./rootDecorations";

const OPEN_EVENT_GRAPH = "pmt.openInEventGraph";

const LANGS = ["paradox", "paradox-gui", "paradox-loc", "paradox-info", "paradox-mod"];

function pathOf(doc: vscode.TextDocument): string {
  let p = doc.uri.fsPath || doc.uri.path;
  if (p.startsWith("/") && /^\/[A-Za-z]:/.test(p)) p = p.slice(1);
  return p;
}

/** Map editor position to UTF-8 byte column for the Go LSP bridge. */
function positionUtf8(doc: vscode.TextDocument, pos: vscode.Position): { line: number; character: number } {
  const lineText = doc.lineAt(pos.line).text;
  let utf16 = 0;
  let byteCol = 0;
  for (let i = 0; i < lineText.length && utf16 < pos.character; ) {
    const cp = lineText.codePointAt(i)!;
    utf16 += cp > 0xffff ? 2 : 1;
    byteCol += cp <= 0x7f ? 1 : cp <= 0x7ff ? 2 : cp <= 0xffff ? 3 : 4;
    i += cp > 0xffff ? 2 : 1;
  }
  return { line: pos.line, character: byteCol };
}

/** Map a UTF-8 byte column on a line to a UTF-16 code-unit column (Monaco). */
function columnUtf16(lineText: string, byteCol: number): number {
  let utf16 = 0;
  let byte = 0;
  for (let i = 0; i < lineText.length && byte < byteCol; ) {
    const cp = lineText.codePointAt(i)!;
    byte += cp <= 0x7f ? 1 : cp <= 0x7ff ? 2 : cp <= 0xffff ? 3 : 4;
    utf16 += cp > 0xffff ? 2 : 1;
    i += cp > 0xffff ? 2 : 1;
  }
  return utf16;
}

/**
 * Turn a path from the language service into a URI the workbench already knows.
 *
 * The workbench keys editors by exact URI string, and on Windows the backend
 * canonicalises paths to lower case for comparison — so a definition in the file
 * you are already looking at came back as `file:///c:/users/...` against the open
 * editor's `file:///C:/Users/...`. Two spellings, two resources, and go-to-
 * definition opened a second tab onto the same file.
 *
 * Matching against the open documents first means the URI handed back is
 * identical to the one the editor holds, so the workbench reuses that tab.
 */
function uriToVsCode(uri: string): vscode.Uri {
  const parsed = uri.startsWith("file:") ? vscode.Uri.parse(uri) : vscode.Uri.file(uri);
  const key = parsed.fsPath.replace(/\\/g, "/").toLowerCase();
  for (const doc of vscode.workspace.textDocuments) {
    if (doc.uri.scheme !== parsed.scheme) continue;
    if (doc.uri.fsPath.replace(/\\/g, "/").toLowerCase() === key) return doc.uri;
  }
  return parsed;
}

function rangeToVsCode(
  doc: vscode.TextDocument,
  r: {
    start: { line: number; character: number };
    end: { line: number; character: number };
  },
): vscode.Range {
  const startLine = safeLine(doc, r.start.line);
  const endLine = safeLine(doc, r.end.line);
  return new vscode.Range(
    r.start.line,
    columnUtf16(startLine, r.start.character),
    r.end.line,
    columnUtf16(endLine, r.end.character),
  );
}

function safeLine(doc: vscode.TextDocument, line: number): string {
  if (line < 0 || line >= doc.lineCount) return "";
  return doc.lineAt(line).text;
}

/** Wrap LSP documentation as trusted markdown. */
function withDoc(text: string): vscode.MarkdownString {
  const md = new vscode.MarkdownString(text, true);
  md.supportHtml = true;
  return md;
}

/**
 * Keep the line breaks an author wrote.
 *
 * Markdown folds a single newline into a space, so a four-line comment above a
 * definition arrived as one wrapped run and lost whatever structure the modder
 * put there. Two trailing spaces is markdown's hard break. Paragraph breaks are
 * left alone — a blank line already means what it looks like.
 */
function hardBreaks(text: string): string {
  return text
    .split("\n\n")
    .map((para) => para.split("\n").join("  \n"))
    .join("\n\n");
}

/** One Markdown card: header, hint/docs, values/body, usage. */
function hoverCardMarkdown(h: HoverResult): string {
  const parts = [hoverCardHeader(h)];
  if (h.hint) parts.push(h.owner ? `${h.hint} of ${h.owner}` : h.hint);
  if (h.docs) parts.push(hardBreaks(h.docs));
  // What the key accepts sits above the scope line: it is about the token
  // itself, where the scope is about where the token sits.
  if (h.accepts) parts.push(`*${h.accepts}*`);
  const scope = hoverScopeLine(h);
  if (scope) parts.push(scope);
  const quote = hoverValuesText(h) || h.body;
  if (quote) {
    parts.push(
      quote
        .trim()
        .split("\n")
        .map((l) => `> ${l}`)
        .join("\n>\n"),
    );
  }
  if (h.usage) parts.push("```\n" + h.usage.trim() + "\n```");
  return parts.join("\n\n");
}

/**
 * Scope context: which scope the cursor sits in, and where a scope link goes.
 *
 * "What scope am I in?" is the question the file cannot answer — the enclosing
 * blocks may be hundreds of lines up — and it has to be right before any effect
 * or trigger makes sense. Rendered as a trailing context line rather than a
 * heading, because it is orientation, not the token's own meaning.
 */
function hoverScopeLine(h: HoverResult): string {
  const code = (s: string) => "`" + s + "`";
  const inScope = h.scope ? `in a ${code(h.scope)} scope` : "";
  if (h.scopeOut) {
    const to = `moves to ${code(h.scopeOut)}`;
    return `*${inScope ? `${inScope} · ${to}` : to}*`;
  }
  return inScope ? `*${inScope}*` : "";
}

/** Kind + key; overlay label is a second span so hover CSS can pin it right. */
function hoverCardHeader(h: HoverResult): string {
  const kindMd = h.kind ? `**${h.kind}**` : "";
  const keyMd = h.key ? ` \`${h.key}\`` : "";
  const leftMd = `${kindMd}${keyMd}`.trim();
  if (!h.vanillaPath) return leftMd;
  const raw = originHexByOriginId(h.origin || "");
  const hex = /^#[0-9A-Fa-f]{6}$/.test(raw) ? raw : "#5B9A8B";
  const kind = h.kind ? `<b>${escHtml(h.kind)}</b>` : "";
  const key = h.key ? ` <code>${escHtml(h.key)}</code>` : "";
  return (
    `<span>${kind}${key}</span>` +
    `<span style="color:${hex};">Game Override</span>`
  );
}

/** Unique harvested values: `text ×count`, then leftover unique names. */
function hoverValuesText(h: HoverResult): string {
  const rows = h.values ?? [];
  if (rows.length === 0) return "";
  const lines = rows.map((v) => `${v.text} ×${v.count}`);
  if (h.more) lines.push(`+${h.more} more`);
  return lines.join("\n");
}

/** Location line from Go origin + rel (1-based line). */
function hoverSiteMarkdown(h: HoverResult): string {
  const primary = hoverSiteLine(h.origin || "", h.originName || "", h.rel ?? "", h.path ?? "", h.line, h.col);
  const vanilla = h.vanillaPath
    ? hoverSiteLine(
        "vanilla",
        h.vanillaOriginName || "",
        h.vanillaRel ?? "",
        h.vanillaPath,
        h.vanillaLine,
        h.vanillaCol,
      )
    : "";
  const parts = [primary, vanilla].filter(Boolean);
  return parts.join("<br>");
}

/** Trusted hover link that opens Event Graph on this event id. */
function hoverEventGraphLink(key: string): string {
  const args = encodeURIComponent(JSON.stringify([key]));
  return `[Open in Event Graph](command:${OPEN_EVENT_GRAPH}?${args})`;
}

/** Colored origin + open-file link for one hover site. */
function hoverSiteLine(
  origin: string,
  originName: string,
  rel: string,
  path: string,
  line?: number,
  col?: number,
): string {
  const label = escHtml(originName);
  if (!label && !rel && !path) return "";
  const key = origin || "vanilla";
  const raw = originHexByOriginId(key);
  const hex = /^#[0-9A-Fa-f]{6}$/.test(raw) ? raw : "#5B9A8B";
  const icon = key === "vanilla" ? "$(root-folder)" : "$(package)";
  const name = label || escHtml(key === "vanilla" ? "Game" : key);
  const site = `<span style="color:${hex};">${icon} ${name}</span>`;
  const pathBit = hoverOpenLink(rel, path, line, col);
  return pathBit ? `${site} · ${pathBit}` : site;
}

/** Markdown command link that opens the def file. */
function hoverOpenLink(rel: string, path: string, line?: number, col?: number): string {
  const text = escHtml(rel || path);
  if (!text) return "";
  if (!path) return text;
  const target = vscode.Uri.file(path).toString();
  const args: unknown[] = col
    ? [
        target,
        {
          selection: {
            startLineNumber: (line ?? 0) + 1,
            startColumn: (col ?? 0) + 1,
          },
        },
      ]
    : [target];
  return `[${rel || text}](command:vscode.open?${encodeURIComponent(JSON.stringify(args))})`;
}

function escHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function applyWorkspaceEdit(doc: vscode.TextDocument | undefined, edit: LspWorkspaceEdit): vscode.WorkspaceEdit {
  const we = new vscode.WorkspaceEdit();
  for (const uri of edit.create ?? []) {
    we.createFile(uriToVsCode(uri), { ignoreIfExists: true });
  }
  for (const [uri, edits] of Object.entries(edit.changes ?? {})) {
    const vsUri = uriToVsCode(uri);
    const sameDoc = doc && vsUri.fsPath === doc.uri.fsPath ? doc : undefined;
    for (const e of edits ?? []) {
      const range = sameDoc
        ? rangeToVsCode(sameDoc, e.range)
        : new vscode.Range(e.range.start.line, e.range.start.character, e.range.end.line, e.range.end.character);
      we.replace(vsUri, range, e.newText);
    }
  }
  return we;
}

/** Map a Go UTF-8 range onto a VS Code range (same-file converts columns). */
function goRangeToVsCode(
  r: { start: { line: number; character: number }; end: { line: number; character: number } },
  fallback?: vscode.TextDocument,
  same?: boolean,
): vscode.Range {
  if (same && fallback) return rangeToVsCode(fallback, r);
  return new vscode.Range(r.start.line, r.start.character, r.end.line, r.end.character);
}

/** Map Go locations onto VS Code Locations (references / symbols). */
function locationsFromGo(locs: LspLocation[], fallback?: vscode.TextDocument): vscode.Location[] {
  const out: vscode.Location[] = [];
  for (const l of locs) {
    const uri = uriToVsCode(l.uri);
    const same =
      fallback &&
      uri.fsPath.replace(/\\/g, "/").toLowerCase() === fallback.uri.fsPath.replace(/\\/g, "/").toLowerCase();
    out.push(new vscode.Location(uri, goRangeToVsCode(l.range, fallback, same)));
  }
  return out;
}

/** Map Go locations onto LocationLinks so Peek can show the assignment block. */
function locationLinksFromGo(locs: LspLocation[], fallback?: vscode.TextDocument): vscode.LocationLink[] {
  const out: vscode.LocationLink[] = [];
  for (const l of locs) {
    const uri = uriToVsCode(l.uri);
    const same =
      fallback &&
      uri.fsPath.replace(/\\/g, "/").toLowerCase() === fallback.uri.fsPath.replace(/\\/g, "/").toLowerCase();
    const sel = goRangeToVsCode(l.range, fallback, same);
    out.push({
      targetUri: uri,
      targetRange: l.targetRange ? goRangeToVsCode(l.targetRange, fallback, same) : sel,
      targetSelectionRange: sel,
    });
  }
  return out;
}

/** Register the same provider factory for every PMT language id. */
function registerLangProviders(subs: vscode.Disposable[], factory: (lang: string) => vscode.Disposable): void {
  for (const lang of LANGS) subs.push(factory(lang));
}

/** Attach PMT language intelligence to the workbench (idempotent). */
export function registerLanguageClient(getWorkspaceId: () => string): vscode.Disposable {
  const subs: vscode.Disposable[] = [];
  const diag = vscode.languages.createDiagnosticCollection("pmt");
  subs.push(diag);

  const dirty = new Map<string, vscode.TextDocument>();
  const flushChanges = useDebounceFn(() => {
    const id = getWorkspaceId();
    const docs = [...dirty.values()];
    dirty.clear();
    if (!id) return;
    for (const doc of docs) {
      void DidChange(id, pathOf(doc), doc.getText()).then(() => refreshDiags(doc));
    }
  }, 200);
  const syncOpen = (doc: vscode.TextDocument) => {
    const id = getWorkspaceId();
    if (!id || !LANGS.includes(doc.languageId)) return;
    void DidOpen(id, pathOf(doc), doc.getText());
  };

  const refreshDiags = async (doc: vscode.TextDocument) => {
    const id = getWorkspaceId();
    if (!id || !LANGS.includes(doc.languageId)) return;
    try {
      const list = (await Diagnose(id, pathOf(doc))) ?? [];
      diag.set(
        doc.uri,
        list.map((d) => {
          const sev = d.severity === 1 ? vscode.DiagnosticSeverity.Error : vscode.DiagnosticSeverity.Warning;
          const v = new vscode.Diagnostic(rangeToVsCode(doc, d.range), d.message, sev);
          // The code was computed, registered and made toggleable, and then not
          // shown: the Problems panel named no check, so a modder who wanted a
          // finding to stop had nothing to look up in Editor settings. Source
          // marks the finding as PMT's rather than the workbench's own.
          v.source = "PMT";
          if (d.code) v.code = d.code;
          return v;
        }),
      );
    } catch {
      /* ignore */
    }
  };

  const onChange = (doc: vscode.TextDocument) => {
    const id = getWorkspaceId();
    if (!id || !LANGS.includes(doc.languageId)) return;
    dirty.set(doc.uri.toString(), doc);
    void flushChanges();
  };

  registerLangProviders(subs, (lang) =>
    vscode.languages.registerHoverProvider(lang, {
      async provideHover(doc, pos) {
        const id = getWorkspaceId();
        if (!id) return null;
        const p = positionUtf8(doc, pos);
        const h = await Hover(id, pathOf(doc), p.line, p.character);
        if (!h?.kind && !h?.key) return null;
        const md = new vscode.MarkdownString("", true);
        md.supportHtml = true;
        md.supportThemeIcons = true;
        md.isTrusted = { enabledCommands: ["vscode.open", OPEN_EVENT_GRAPH] };
        md.appendMarkdown(hoverCardMarkdown(h));
        const site = hoverSiteMarkdown(h);
        if (site) md.appendMarkdown(`\n\n---\n\n${site}`);
        if (h.kind === "event" && h.key) {
          md.appendMarkdown(`\n\n${hoverEventGraphLink(h.key)}`);
        }
        return new vscode.Hover(md);
      },
    }),
  );
  // Fired when a mod's calendar changes. VS Code caches inlay hints per document
  // and re-requests them only when the provider says they are stale, so without
  // this a modder sets an era and sees nothing until they edit the file.
  const inlayChanged = new vscode.EventEmitter<void>();
  subs.push(inlayChanged);

  // A mod's own era, drawn beside the dates its script is written in. Silent
  // for every mod that declares no calendar, which the backend decides before
  // it parses anything -- so this costs nothing in an ordinary workspace.
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerInlayHintsProvider(lang, {
      onDidChangeInlayHints: inlayChanged.event,
      async provideInlayHints(doc, range) {
        const id = getWorkspaceId();
        if (!id) return [];
        const hints = (await InlayHints(id, pathOf(doc))) ?? [];
        return hints
          .map((h) => {
            const r = rangeToVsCode(doc, h.range);
            const hint = new vscode.InlayHint(r.end, h.label);
            hint.tooltip = h.tooltip;
            hint.paddingLeft = true;
            return hint;
          })
          // The editor asks for the visible span; returning the whole file
          // would draw hints it then has to throw away on every scroll.
          .filter((h) => range.contains(h.position));
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerCompletionItemProvider(
      lang,
      {
        async provideCompletionItems(doc, pos, token) {
          const id = getWorkspaceId();
          if (!id || token?.isCancellationRequested) return undefined;
          dirty.delete(doc.uri.toString());
          await DidChange(id, pathOf(doc), doc.getText());
          if (token?.isCancellationRequested) return undefined;
          const p = positionUtf8(doc, pos);
          const items = (await Complete(id, pathOf(doc), p.line, p.character)) ?? [];
          if (token?.isCancellationRequested) return undefined;
          return items.map((it, i) => {
            const kind = it.kind === "property" ? vscode.CompletionItemKind.Property : vscode.CompletionItemKind.Value;
            const c = new vscode.CompletionItem(it.label, kind);
            c.detail = it.detail;
            c.sortText = String(i).padStart(4, "0");
            if (it.documentation) {
              c.documentation = withDoc(it.documentation);
            }
            if (it.insertText) {
              c.insertText = it.insertText.includes("$") ? new vscode.SnippetString(it.insertText) : it.insertText;
            }
            if (it.range) {
              c.range = rangeToVsCode(doc, it.range);
            }
            return c;
          });
        },
      },
      ".",
      "[",
      ":",
      "{",
    ),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerSignatureHelpProvider(
      lang,
      {
        async provideSignatureHelp(doc, pos, token) {
          const id = getWorkspaceId();
          if (!id || token?.isCancellationRequested) return undefined;
          const p = positionUtf8(doc, pos);
          const h = await SignatureHelp(id, pathOf(doc), p.line, p.character);
          if (!h?.label || token?.isCancellationRequested) return undefined;
          const info = new vscode.SignatureInformation(h.label, h.documentation);
          for (const pName of h.parameters ?? []) {
            info.parameters.push(new vscode.ParameterInformation(pName));
          }
          info.activeParameter = h.activeParameter ?? 0;
          return {
            signatures: [info],
            activeSignature: 0,
            activeParameter: h.activeParameter ?? 0,
          };
        },
      },
      "(",
      "{",
    ),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerDefinitionProvider(lang, {
      async provideDefinition(doc, pos) {
        const id = getWorkspaceId();
        if (!id) return [];
        const p = positionUtf8(doc, pos);
        const locs = (await Definition(id, pathOf(doc), p.line, p.character)) ?? [];
        return locationLinksFromGo(locs, doc);
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerReferenceProvider(lang, {
      async provideReferences(doc, pos) {
        const id = getWorkspaceId();
        if (!id) return [];
        const p = positionUtf8(doc, pos);
        const locs = (await References(id, pathOf(doc), p.line, p.character)) ?? [];
        return locationsFromGo(locs, doc);
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerDocumentSymbolProvider(lang, {
      async provideDocumentSymbols(doc) {
        const id = getWorkspaceId();
        if (!id) return [];
        const syms = (await DocumentSymbols(id, pathOf(doc))) ?? [];
        return syms.map(
          (s) =>
            new vscode.SymbolInformation(
              s.name,
              vscode.SymbolKind.Object,
              s.containerName ?? "",
              new vscode.Location(uriToVsCode(s.location.uri), rangeToVsCode(doc, s.location.range)),
            ),
        );
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerRenameProvider(lang, {
      async provideRenameEdits(doc, pos, newName) {
        const id = getWorkspaceId();
        if (!id) return null;
        const p = positionUtf8(doc, pos);
        const edit = await Rename(id, pathOf(doc), p.line, p.character, newName);
        if (!edit?.changes) return null;
        return applyWorkspaceEdit(doc, edit);
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerDocumentFormattingEditProvider(lang, {
      async provideDocumentFormattingEdits(doc) {
        const id = getWorkspaceId();
        if (!id) return [];
        const edits = (await FormatDocument(id, pathOf(doc))) ?? [];
        return edits.map((e) => new vscode.TextEdit(rangeToVsCode(doc, e.range), e.newText));
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerFoldingRangeProvider(lang, {
      async provideFoldingRanges(doc) {
        const id = getWorkspaceId();
        if (!id) return [];
        const ranges = (await FoldingRanges(id, pathOf(doc))) ?? [];
        return ranges.map((r) => new vscode.FoldingRange(r.startLine, r.endLine, vscode.FoldingRangeKind.Region));
      },
    }),
  );
  registerLangProviders(subs, (lang) =>
    vscode.languages.registerCodeActionsProvider(lang, {
      async provideCodeActions(doc) {
        const id = getWorkspaceId();
        if (!id) return [];
        const actions = (await CodeActions(id, pathOf(doc))) ?? [];
        return actions.map((a) => {
          const ca = new vscode.CodeAction(a.title, vscode.CodeActionKind.QuickFix);
          if (a.edit) ca.edit = applyWorkspaceEdit(doc, a.edit);
          return ca;
        });
      },
    }),
  );

  subs.push(
    vscode.languages.registerWorkspaceSymbolProvider({
      async provideWorkspaceSymbols(query) {
        const id = getWorkspaceId();
        if (!id) return [];
        const syms = (await WorkspaceSymbols(id, query)) ?? [];
        const out: vscode.SymbolInformation[] = [];
        for (const s of syms) {
          const r = s.location.range;
          out.push(
            new vscode.SymbolInformation(
              s.name,
              vscode.SymbolKind.Object,
              s.containerName ?? "",
              new vscode.Location(
                uriToVsCode(s.location.uri),
                new vscode.Range(r.start.line, r.start.character, r.end.line, r.end.character),
              ),
            ),
          );
        }
        return out;
      },
    }),
  );

  subs.push(
    vscode.workspace.onDidOpenTextDocument((d) => {
      syncOpen(d);
      void refreshDiags(d);
    }),
  );
  subs.push(vscode.workspace.onDidChangeTextDocument((e) => onChange(e.document)));
  subs.push(
    vscode.workspace.onDidSaveTextDocument((d) => {
      const id = getWorkspaceId();
      if (id && LANGS.includes(d.languageId)) {
        void DidSave(id, pathOf(d)).then(() => refreshDiags(d));
        return;
      }
      void refreshDiags(d);
    }),
  );
  subs.push(
    vscode.workspace.onDidCloseTextDocument((d) => {
      const id = getWorkspaceId();
      if (id && LANGS.includes(d.languageId)) {
        void DidClose(id, pathOf(d));
      }
      diag.delete(d.uri);
    }),
  );

  for (const doc of vscode.workspace.textDocuments) {
    syncOpen(doc);
    void refreshDiags(doc);
  }

  Events.On("lang:calendar-changed", () => {
    inlayChanged.fire();
  });

  Events.On("lang:cache-updated", () => {
    for (const doc of vscode.workspace.textDocuments) {
      void refreshDiags(doc);
    }
  });

  // Deferred import: commands.ts pulls workbenchHost, which registers this module.
  subs.push(
    vscode.commands.registerCommand(OPEN_EVENT_GRAPH, (key?: string) => {
      const id = getWorkspaceId();
      if (!id || typeof key !== "string" || !key) return;
      void import("./commands").then(({ openEventGraph }) => {
        void openEventGraph(id, key);
      });
    }),
  );

  return {
    dispose() {
      for (const s of subs) s.dispose();
    },
  };
}
