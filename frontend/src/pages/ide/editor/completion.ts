/**
 * Tab completion in the editor: tells the completion agent about the open
 * files and shows its next edit suggestions as Monaco's inline completions.
 */
import type * as nes from "@agentclientprotocol/sdk/experimental/nes";
import type { languages } from "monaco-editor";
import * as monaco from "monaco-editor/editor/editor.api";
import { watch } from "vue";

import { isAuthRequired, toUri } from "../../../stores/agent";
import { useCompletionStore, type Connection } from "../../../stores/completion";

/** An Item is an inline completion from one suggestion, which keeps its id. */
type Item = languages.InlineCompletion & { id: string };

/** GROUP is the group of this provider's completions. PMT's own give way. */
export const GROUP = "copilot";

/** SEVERITIES holds the protocol's severity of each Monaco marker severity. */
const SEVERITIES: Record<number, nes.NesDiagnosticSeverity> = {
  [monaco.MarkerSeverity.Error]: "error",
  [monaco.MarkerSeverity.Warning]: "warning",
  [monaco.MarkerSeverity.Info]: "information",
  [monaco.MarkerSeverity.Hint]: "hint",
};

/**
 * registerSuggestions keeps the completion agent told of the open files and
 * registers its suggestions for the given languages, which must already be
 * registered.
 */
export function registerSuggestions(selector: languages.LanguageFilter[]): void {
  const completion = useCompletionStore();

  // Synchronous, so no change to a file reaches the agent before its opening.
  watch(
    () => completion.connection,
    (connection) => {
      if (connection)
        monaco.editor
          .getModels()
          .filter(isFile)
          .forEach((model) => openDocument(connection, model));
    },
    { immediate: true, flush: "sync" },
  );

  monaco.editor.getModels().filter(isFile).forEach(follow);
  monaco.editor.onDidCreateModel((model) => isFile(model) && follow(model));
  monaco.editor.onWillDisposeModel((model) => {
    const connection = completion.connection;
    if (connection && isFile(model) && isWanted(connection, "didClose"))
      void connection.agent.notify("document/didClose", {
        sessionId: connection.sessionId,
        uri: toUri(model.uri.fsPath),
      });
  });
  monaco.editor.onDidCreateEditor((editor) => editor.onDidFocusEditorText(() => focusDocument(editor)));

  const provider: languages.InlineCompletionsProvider<languages.InlineCompletions<Item>> = {
    groupId: GROUP,
    provideInlineCompletions: suggest,
    handleEndOfLifetime: (_completions, item, reason) => {
      const connection = completion.connection;
      if (!connection) return;

      const params = { sessionId: connection.sessionId, id: item.id };
      if (reason.kind === monaco.languages.InlineCompletionEndOfLifeReasonKind.Accepted)
        void connection.agent.notify("nes/accept", params);
      else if (reason.kind === monaco.languages.InlineCompletionEndOfLifeReasonKind.Rejected)
        void connection.agent.notify("nes/reject", { ...params, reason: "rejected" });
    },
    disposeInlineCompletions: () => undefined,
  };
  monaco.languages.registerInlineCompletionsProvider(selector, provider);
}

/** isFile reports whether a model shows a file on disk. */
function isFile(model: monaco.editor.ITextModel): boolean {
  return model.uri.scheme === "file";
}

/** isWanted reports whether the agent asked to be told of a document event. */
function isWanted(connection: Connection, event: keyof nes.NesDocumentEventCapabilities): boolean {
  return !!connection.capabilities.events?.document?.[event];
}

/** follow tells the store a file is open and the agent of its changes. */
function follow(model: monaco.editor.ITextModel): void {
  const completion = useCompletionStore();
  completion.openFile(model.uri.fsPath);
  if (completion.connection) openDocument(completion.connection, model);

  model.onDidChangeContent((event) => {
    const connection = completion.connection;
    const sync = connection?.capabilities.events?.document?.didChange;
    if (!connection || !sync) return;

    // Monaco lists the edits of a change last to first, so they apply in order.
    const contentChanges =
      sync.syncKind === "full"
        ? [{ text: model.getValue() }]
        : event.changes.map((change) => ({ range: toRange(change.range), text: change.text }));
    void connection.agent.notify("document/didChange", {
      sessionId: connection.sessionId,
      uri: toUri(model.uri.fsPath),
      version: model.getVersionId(),
      contentChanges,
    });
  });
}

/** openDocument tells the agent a file is open, with its text. */
function openDocument(connection: Connection, model: monaco.editor.ITextModel): void {
  if (!isWanted(connection, "didOpen")) return;

  void connection.agent.notify("document/didOpen", {
    sessionId: connection.sessionId,
    uri: toUri(model.uri.fsPath),
    languageId: model.getLanguageId(),
    version: model.getVersionId(),
    text: model.getValue(),
  });
}

/** focusDocument tells the agent which file the user is in, and where. */
function focusDocument(editor: monaco.editor.ICodeEditor): void {
  const connection = useCompletionStore().connection;
  const model = editor.getModel();
  const position = editor.getPosition();
  const visible = editor.getVisibleRanges()[0];
  if (!connection || !model || !isFile(model) || !position || !visible || !isWanted(connection, "didFocus")) return;

  void connection.agent.notify("document/didFocus", {
    sessionId: connection.sessionId,
    uri: toUri(model.uri.fsPath),
    version: model.getVersionId(),
    position: toPosition(position),
    visibleRange: toRange(visible),
  });
}

/**
 * suggest asks the agent for the next edits at a position and returns them as
 * inline completions. It returns nothing at once while the connection is not
 * ready, so typing never waits for it. If the agent refuses for a sign-in, it
 * returns nothing and the completion waits for the user to sign in.
 */
async function suggest(
  model: monaco.editor.ITextModel,
  position: monaco.Position,
  context: languages.InlineCompletionContext,
  token: monaco.CancellationToken,
): Promise<languages.InlineCompletions<Item> | undefined> {
  const completion = useCompletionStore();
  const connection = completion.connection;
  if (!connection || !isFile(model)) return undefined;

  const response = await connection.agent
    .request("nes/suggest", {
      sessionId: connection.sessionId,
      uri: toUri(model.uri.fsPath),
      version: model.getVersionId(),
      position: toPosition(position),
      triggerKind:
        context.triggerKind === monaco.languages.InlineCompletionTriggerKind.Explicit ? "manual" : "automatic",
      context: connection.capabilities.context?.diagnostics ? { diagnostics: listDiagnostics(model) } : undefined,
    })
    .catch((cause: unknown) => {
      if (!isAuthRequired(cause)) throw cause;

      completion.requireSignIn(connection);
      return null;
    });
  if (!response || token.isCancellationRequested) return undefined;

  const items = response.suggestions
    .flatMap((suggestion) => toItem(model, position, suggestion) ?? [])
    .filter((item) => (item.isInlineEdit ? context.includeInlineEdits : context.includeInlineCompletions));

  return { items };
}

/**
 * toItem turns a suggestion into an inline completion. An edit at the caret is
 * ghost text, an edit elsewhere in the file is an inline edit that shows while
 * the caret stays on its line, and a jump is a jump. It returns null for a
 * suggestion about another file and for a rename or a search and replace,
 * which PMT does not show.
 */
function toItem(
  model: monaco.editor.ITextModel,
  position: monaco.Position,
  suggestion: nes.NesSuggestion,
): Item | null {
  if (suggestion.kind !== "edit" && suggestion.kind !== "jump") return null;

  // Monaco's URIs read a drive letter's case and an encoded colon alike.
  if (monaco.Uri.parse(suggestion.uri).toString() !== model.uri.toString()) return null;

  if (suggestion.kind === "jump")
    return {
      id: suggestion.id,
      insertText: undefined,
      isInlineEdit: true,
      jumpToPosition: fromPosition(suggestion.position),
    };

  if (!suggestion.edits.length) return null;

  const { range, text } = mergeEdits(model, suggestion.edits);
  if (isAtCaret(model, position, range, text)) return { id: suggestion.id, insertText: text, range };

  const line = position.lineNumber;
  const showRange = new monaco.Range(line, 1, line, model.getLineMaxColumn(line));

  return { id: suggestion.id, insertText: text, range, isInlineEdit: true, showRange };
}

/**
 * mergeEdits returns the one edit that does what the given edits do together:
 * it replaces the text from the first edit's start to the last edit's end.
 */
function mergeEdits(model: monaco.editor.ITextModel, edits: nes.NesTextEdit[]): { range: monaco.Range; text: string } {
  const sorted = edits
    .map((edit) => ({ range: fromRange(edit.range), text: edit.newText }))
    .sort((first, second) => monaco.Range.compareRangesUsingStarts(first.range, second.range));
  const range = sorted.map((edit) => edit.range).reduce((whole, other) => whole.plusRange(other));

  let text = "";
  let end = range.getStartPosition();
  for (const edit of sorted) {
    text += model.getValueInRange(monaco.Range.fromPositions(end, edit.range.getStartPosition())) + edit.text;
    end = edit.range.getEndPosition();
  }

  return { range, text };
}

/**
 * isAtCaret reports whether an edit is ghost text. Monaco shows ghost text for
 * an edit on the caret's line that surrounds the caret and keeps the text it
 * replaces as its start. If the edit has line breaks, it must end its line.
 */
function isAtCaret(
  model: monaco.editor.ITextModel,
  position: monaco.Position,
  range: monaco.Range,
  text: string,
): boolean {
  const line = position.lineNumber;

  return (
    range.startLineNumber === line &&
    range.endLineNumber === line &&
    range.containsPosition(position) &&
    text.startsWith(model.getValueInRange(range)) &&
    (!text.includes("\n") || range.endColumn === model.getLineMaxColumn(line))
  );
}

/** listDiagnostics returns a model's markers as the protocol's diagnostics. */
function listDiagnostics(model: monaco.editor.ITextModel): nes.NesDiagnostic[] {
  return monaco.editor.getModelMarkers({ resource: model.uri }).map((marker) => ({
    uri: toUri(model.uri.fsPath),
    range: toRange(marker),
    severity: SEVERITIES[marker.severity] ?? "information",
    message: marker.message,
  }));
}

/** toPosition returns a Monaco position as the protocol's, from 0. */
function toPosition({ lineNumber, column }: monaco.IPosition): nes.Position {
  return { line: lineNumber - 1, character: column - 1 };
}

/** fromPosition returns the protocol's position as Monaco's. */
function fromPosition({ line, character }: nes.Position): monaco.IPosition {
  return { lineNumber: line + 1, column: character + 1 };
}

/** toRange returns a Monaco range as the protocol's. */
function toRange(range: monaco.IRange): nes.Range {
  return {
    start: toPosition({ lineNumber: range.startLineNumber, column: range.startColumn }),
    end: toPosition({ lineNumber: range.endLineNumber, column: range.endColumn }),
  };
}

/** fromRange returns the protocol's range as Monaco's. */
function fromRange({ start, end }: nes.Range): monaco.Range {
  return monaco.Range.fromPositions(fromPosition(start), fromPosition(end));
}
