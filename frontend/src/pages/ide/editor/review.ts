/**
 * The review of a file an agent edited: its baseline as an editor model, the
 * changes the diff editor finds against it, and Keep and Undo for each change.
 */
import { nextTick, shallowReactive, shallowRef, watch, type ShallowRef } from "vue";
import * as monaco from "monaco-editor/editor/editor.api";

import type { Edit } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import * as workspace from "@services/workspace/service";
import { loadQuery, reviewsQuery } from "../../../queries";
import { useEditorStore } from "../../../stores/editor";
import { confirmUndo } from "../../../stores/edits";
import { applyEdits, save, type Model } from "./models";

/**
 * A LineChange is one run of lines the diff editor found changed. An end line
 * of 0 means that side has no lines: the run is an insertion or a deletion,
 * and its start line is the line before it.
 */
export type LineChange = monaco.editor.ILineChange;

/**
 * A Toolbar is the Keep and Undo buttons over one change. The page draws the
 * buttons into its node.
 */
type Toolbar = monaco.editor.IOverlayWidget & {
  id: string;
  node: HTMLElement;
  change: LineChange;
  position: monaco.editor.IOverlayWidgetPosition | null;
};

/** A FileReview is one file under review in this window. */
type FileReview = {
  // original is Monaco's model of the baseline, in the line endings Go gave.
  original: monaco.editor.ITextModel;
  // changes are the last diff's changes, moved along with the edits since.
  changes: LineChange[];
  // isFresh is whether the changes match both texts now. An edit they cannot
  // follow makes it false until the diff is computed again.
  isFresh: boolean;
  // isDirty is whether the baseline holds edits that Go does not hold yet.
  isDirty: boolean;
  // undoVersions and redoVersions are the versions of the file's model that
  // undoing or redoing one Undo of a change gives. Those edits are not the
  // user's own.
  undoVersions: Set<number>;
  redoVersions: Set<number>;
  timer?: ReturnType<typeof setTimeout>;
  listeners: monaco.IDisposable[];
};

/**
 * SCHEME is the URI scheme of a baseline's model, so that no language feature
 * takes it for the file.
 */
const SCHEME = "pmt-baseline";

/**
 * WAIT is how long the user's edits must rest, in milliseconds, before the
 * baseline goes to Go.
 */
const WAIT = 500;

const reviews = shallowReactive(new Map<string, FileReview>());

/** findFileReview returns a file's review in this window, if it has one. */
export function findFileReview(model: Model): FileReview | undefined {
  return reviews.get(model.uri);
}

/**
 * attachReview shows a file's review in a diff editor: its baseline against
 * the file's model, and a toolbar over each change. It starts the file's
 * review if it has none. With revealFirst it moves the caret to the first
 * change once the diff is known. It returns the toolbars and a function that
 * takes them off the editor.
 */
export function attachReview(
  diff: monaco.editor.IStandaloneDiffEditor,
  model: Model,
  baseline: string,
  revealFirst: boolean,
): { toolbars: ShallowRef<Toolbar[]>; detach: () => void } {
  const review = startReview(model, baseline);
  const codeEditor = diff.getModifiedEditor();
  const toolbars = shallowRef<Toolbar[]>([]);
  const bars: Toolbar[] = [];
  let isRevealing = revealFirst;
  const layout = (): void => placeToolbars(codeEditor, bars);
  const release = model.model.onWillDispose(() => diff.setModel(null));
  diff.setModel({ original: review.original, modified: model.model });

  const subscriptions = [
    release,
    diff.onDidUpdateDiff(() => {
      review.changes = diff.getLineChanges() ?? [];
      review.isFresh = true;
      if (!isRevealing || !review.changes[0]) return;

      isRevealing = false;
      showChange(codeEditor, model, review.changes[0]);
    }),
    codeEditor.onDidScrollChange(layout),
    codeEditor.onDidLayoutChange(layout),
    codeEditor.onDidContentSizeChange(layout),
  ];
  const stop = watch(
    () => review.changes,
    (changes) => {
      toolbars.value = syncToolbars(codeEditor, bars, changes);
      void nextTick(layout);
    },
    { immediate: true },
  );

  return {
    toolbars,
    detach: () => {
      stop();
      subscriptions.forEach((subscription) => subscription.dispose());
      bars.forEach((bar) => codeEditor.removeOverlayWidget(bar));
    },
  };
}

/**
 * followBaseline puts a baseline Go gave into a file's review, unless the
 * review holds edits Go does not have yet or already has that text.
 */
export function followBaseline(model: Model, baseline: string): void {
  const review = findFileReview(model);
  if (!review || review.isDirty || review.original.getValue() === baseline) return;

  review.original.setValue(baseline);
  review.isFresh = false;
}

/** closeReview ends a file's review here and disposes its baseline's model. */
export function closeReview(model: Model): void {
  const review = findFileReview(model);
  if (!review) return;

  clearTimeout(review.timer);
  review.listeners.forEach((listener) => listener.dispose());
  review.original.dispose();
  reviews.delete(model.uri);
}

/**
 * keepChange makes the baseline take one change's lines and sends Go the new
 * baseline. A change that is not in the file's review any more is left alone.
 */
export function keepChange(model: Model, change: LineChange): Promise<void> {
  return dropChange(model, change, async (review) => {
    const { original } = review;
    const lines = takeLines(model.model, change.modifiedStartLineNumber, change.modifiedEndLineNumber);
    review.isFresh = false;
    original.applyEdits([replaceLines(original, change.originalStartLineNumber, change.originalEndLineNumber, lines)]);
    await sendBaseline(model, review);
  });
}

/**
 * undoChange puts the baseline's lines back in the file, as one undoable edit.
 * It then saves the file, unless the file had unsaved text before. A change
 * that is not in the file's review any more is left alone.
 */
export function undoChange(model: Model, change: LineChange): Promise<void> {
  return dropChange(model, change, async (review) => {
    const wasUnsaved = model.unsaved;
    const lines = takeLines(review.original, change.originalStartLineNumber, change.originalEndLineNumber);
    const edit = replaceLines(model.model, change.modifiedStartLineNumber, change.modifiedEndLineNumber, lines);
    review.isFresh = false;
    review.undoVersions.add(model.model.getAlternativeVersionId());
    applyEdits(model, [edit]);
    review.redoVersions.add(model.model.getAlternativeVersionId());
    if (!wasUnsaved) await save(model);
  });
}

/** keepFile ends a file's review and leaves the file as the agent wrote it. */
export async function keepFile(model: Model): Promise<void> {
  await settle(model, true, () => workspace.KeepFile(model.place.path));
}

/**
 * undoFile asks first, and then puts a file back to how it was before the
 * agent's edits, or moves a file the agent created to the trash. It does
 * nothing if the user says no.
 */
export async function undoFile(model: Model): Promise<void> {
  if (await confirmUndo([model.place])) await settle(model, true, () => workspace.UndoFile(model.place.path));
}

/**
 * locateChange returns the index of the change the caret is in or comes
 * after, or -1 if the caret is above the first change.
 */
export function locateChange(model: Model, changes: LineChange[]): number {
  const lineCount = model.model.getLineCount();

  return changes.findLastIndex((change) => Math.min(startLine(change), lineCount) <= model.caret.line);
}

/**
 * goToChange moves the caret to the next change, or to the previous one for
 * -1, wrapping round at the ends. A file with no changes is left alone.
 */
export function goToChange(
  codeEditor: monaco.editor.IStandaloneCodeEditor | undefined,
  model: Model,
  step: 1 | -1,
): void {
  const changes = findFileReview(model)?.changes ?? [];
  const lineCount = model.model.getLineCount();
  const lines = changes.map((change) => Math.min(startLine(change), lineCount));
  const line = codeEditor?.getPosition()?.lineNumber ?? model.caret.line;
  const index = step > 0 ? lines.findIndex((other) => other > line) : lines.findLastIndex((other) => other < line);
  const change = changes[index < 0 ? (step > 0 ? 0 : changes.length - 1) : index];
  if (!codeEditor || !change) return;

  showChange(codeEditor, model, change);
  codeEditor.focus();
}

/**
 * dropChange takes one change out of a file's review and runs the way of
 * settling it. A change that is not in the file's review any more is left
 * alone.
 */
async function dropChange(model: Model, change: LineChange, run: (review: FileReview) => Promise<void>): Promise<void> {
  const review = findFileReview(model);
  if (!review?.changes.includes(change)) return;

  const isLast = review.changes.length === 1;
  review.changes = review.changes.filter((other) => other !== change);
  await settle(model, isLast, () => run(review));
}

/**
 * settle runs one way of settling a file's changes. When that settles the
 * file's last change, it then shows the next file under review.
 */
async function settle(model: Model, isLast: boolean, run: () => Promise<void>): Promise<void> {
  const next = isLast ? findNext(model.place, await loadQuery(reviewsQuery)) : undefined;
  await run();
  if (next) useEditorStore().show(next, undefined, "changes");
}

/**
 * findNext returns the file under review after the given one, wrapping round.
 * It returns undefined if no other file is under review.
 */
function findNext(place: Place, places: Place[] | null | undefined): Place | undefined {
  const reviewed = places ?? [];
  const isOther = (candidate: Place): boolean => candidate.key !== place.key;
  const index = reviewed.findIndex((candidate) => !isOther(candidate));

  return reviewed.slice(index + 1).find(isOther) ?? reviewed.find(isOther);
}

/**
 * startReview returns a file's review, starting it from the baseline if the
 * file has none. The review ends with the file's model.
 */
function startReview(model: Model, baseline: string): FileReview {
  const existing = findFileReview(model);
  if (existing) return existing;

  const review = shallowReactive<FileReview>({
    original: createOriginal(model, baseline),
    changes: [],
    isFresh: false,
    isDirty: false,
    undoVersions: new Set(),
    redoVersions: new Set(),
    listeners: [
      model.model.onDidChangeContent((event) => mirror(model, review, event)),
      model.model.onWillDispose(() => queueMicrotask(() => closeReview(model))),
    ],
  });
  reviews.set(model.uri, review);

  return review;
}

/**
 * createOriginal makes the editor model of a baseline, in the file's language,
 * in the baseline's line endings. A baseline with no line break takes the
 * file's.
 */
function createOriginal(model: Model, baseline: string): monaco.editor.ITextModel {
  const uri = monaco.Uri.file(model.place.path).with({ scheme: SCHEME });
  monaco.editor.getModel(uri)?.dispose();

  const original = monaco.editor.createModel(baseline, model.place.language, uri);
  let ending = model.model.getEOL();
  if (baseline.includes("\r\n")) ending = "\r\n";
  else if (baseline.includes("\n")) ending = "\n";
  original.setEOL(ending === "\r\n" ? monaco.editor.EndOfLineSequence.CRLF : monaco.editor.EndOfLineSequence.LF);

  return original;
}

/**
 * mirror makes an edit of the user's in the baseline too, where it lies wholly
 * outside every change, so that only the agent's changes show. An edit inside
 * a change joins that change. An edit it cannot follow leaves the changes
 * unknown until the diff is computed again. A change of the model from disk,
 * and the undoing or redoing of an Undo of a change, are not the user's.
 */
function mirror(model: Model, review: FileReview, event: monaco.editor.IModelContentChangedEvent): void {
  const [edit] = event.changes;
  const version = model.model.getAlternativeVersionId();
  const isOwn = event.isUndoing
    ? review.undoVersions.has(version)
    : event.isRedoing && review.redoVersions.has(version);
  if (model.isDiskEdit || isOwn || event.isFlush || event.changes.length !== 1) review.isFresh = false;
  if (!review.isFresh || !edit) return;

  const first = edit.range.startLineNumber;
  const last = edit.range.endLineNumber;
  if (review.changes.some((change) => touches(change, first, last))) {
    review.isFresh = false;

    return;
  }

  const offset = review.changes
    .filter((change) => endLine(change) < first)
    .reduce((sum, change) => sum + extraLines(change), 0);
  const range = new monaco.Range(first + offset, edit.range.startColumn, last + offset, edit.range.endColumn);
  review.original.applyEdits([{ range, text: edit.text }]);

  const delta = edit.text.split(/\r\n|\r|\n/).length - (last - first + 1);
  review.changes = review.changes.map((change) => (startLine(change) > last ? shiftChange(change, delta) : change));
  review.isDirty = true;
  clearTimeout(review.timer);
  review.timer = setTimeout(() => void sendBaseline(model, review), WAIT);
}

/** sendBaseline sends Go the baseline's text now. */
async function sendBaseline(model: Model, review: FileReview): Promise<void> {
  clearTimeout(review.timer);

  const text = review.original.getValue();
  await workspace.SetBaseline(model.place.path, text);
  if (!review.original.isDisposed()) review.isDirty = review.original.getValue() !== text;
}

/**
 * startLine returns the first line of the file that belongs to a change: for a
 * deletion, the line after the place where lines were.
 */
function startLine(change: LineChange): number {
  return change.modifiedStartLineNumber + (change.modifiedEndLineNumber ? 0 : 1);
}

/**
 * endLine returns the last line of the file that belongs to a change: for a
 * deletion, the line before the place where lines were.
 */
function endLine(change: LineChange): number {
  return change.modifiedEndLineNumber || change.modifiedStartLineNumber;
}

/** extraLines returns how many more lines a change has in the baseline. */
function extraLines(change: LineChange): number {
  const size = (first: number, last: number): number => (last ? last - first + 1 : 0);

  return (
    size(change.originalStartLineNumber, change.originalEndLineNumber) -
    size(change.modifiedStartLineNumber, change.modifiedEndLineNumber)
  );
}

/**
 * touches reports whether an edit of the lines first to last reaches a change:
 * one of its lines, or, for a deletion, both lines round the place it was.
 */
function touches(change: LineChange, first: number, last: number): boolean {
  if (!change.modifiedEndLineNumber) return first < startLine(change) && startLine(change) <= last;

  return first <= change.modifiedEndLineNumber && change.modifiedStartLineNumber <= last;
}

/** shiftChange returns a change moved down by some lines on both sides. */
function shiftChange(change: LineChange, delta: number): LineChange {
  return {
    originalStartLineNumber: change.originalStartLineNumber + delta,
    originalEndLineNumber: change.originalEndLineNumber && change.originalEndLineNumber + delta,
    modifiedStartLineNumber: change.modifiedStartLineNumber + delta,
    modifiedEndLineNumber: change.modifiedEndLineNumber && change.modifiedEndLineNumber + delta,
    charChanges: undefined,
  };
}

/** takeLines returns a model's lines first to last, or none for a last of 0. */
function takeLines(textModel: monaco.editor.ITextModel, first: number, last: number): string[] {
  return last ? textModel.getLinesContent().slice(first - 1, last) : [];
}

/**
 * replaceLines returns the edit that puts the given lines where a change has
 * lines in a model: from first to last, or, for a last line of 0, after the
 * line first. No lines delete what is there.
 */
function replaceLines(textModel: monaco.editor.ITextModel, first: number, last: number, lines: string[]): Edit {
  const count = textModel.getLineCount();
  const ending = textModel.getEOL();
  const text = lines.join(ending);
  const width = (line: number): number => textModel.getLineMaxColumn(line);
  if (!last && first < count) return { range: new monaco.Range(first + 1, 1, first + 1, 1), text: text + ending };
  if (!last) return { range: new monaco.Range(count, width(count), count, width(count)), text: ending + text };
  if (lines.length) return { range: new monaco.Range(first, 1, last, width(last)), text };
  if (last < count) return { range: new monaco.Range(first, 1, last + 1, 1), text: "" };
  if (first > 1) return { range: new monaco.Range(first - 1, width(first - 1), last, width(last)), text: "" };

  return { range: new monaco.Range(1, 1, last, width(last)), text: "" };
}

/** showChange moves the caret to a change and scrolls it into view. */
function showChange(codeEditor: monaco.editor.IStandaloneCodeEditor, model: Model, change: LineChange): void {
  const position = { lineNumber: Math.min(startLine(change), model.model.getLineCount()), column: 1 };
  codeEditor.setPosition(position);
  codeEditor.revealPositionInCenterIfOutsideViewport(position);
}

/**
 * syncToolbars gives each change a toolbar and takes the toolbars of changes
 * that are gone off the editor. It reuses the toolbars the editor has already.
 */
function syncToolbars(
  codeEditor: monaco.editor.IStandaloneCodeEditor,
  bars: Toolbar[],
  changes: LineChange[],
): Toolbar[] {
  bars.splice(changes.length).forEach((bar) => codeEditor.removeOverlayWidget(bar));
  changes.forEach((change, index) => {
    const bar = bars[index];
    if (bar) {
      bar.change = change;

      return;
    }

    const id = `pmt.review.${index}`;
    const node = document.createElement("div");
    const toolbar: Toolbar = shallowReactive({
      id,
      node,
      change,
      position: null,
      getId: () => id,
      getDomNode: () => node,
      getPosition: () => toolbar.position,
    });
    codeEditor.addOverlayWidget(toolbar);
    bars.push(toolbar);
  });

  return [...bars];
}

/**
 * placeToolbars puts each toolbar at the top of its change, at the right of
 * the editor, as VS Code does.
 */
function placeToolbars(codeEditor: monaco.editor.IStandaloneCodeEditor, bars: Toolbar[]): void {
  const { contentLeft, contentWidth, verticalScrollbarWidth } = codeEditor.getLayoutInfo();
  for (const bar of bars) {
    const top = measureTop(codeEditor, bar.change) - codeEditor.getScrollTop();
    const left = contentLeft + contentWidth - 2 * verticalScrollbarWidth - bar.node.offsetWidth;
    bar.position = { preference: { top, left } };
    codeEditor.layoutOverlayWidget(bar);
  }
}

/**
 * measureTop returns where a change starts in the editor, counting the deleted
 * lines the diff shows above it. A deletion starts under the line before it.
 */
function measureTop(codeEditor: monaco.editor.IStandaloneCodeEditor, change: LineChange): number {
  if (change.modifiedEndLineNumber) return codeEditor.getTopForLineNumber(change.modifiedStartLineNumber, true);

  return change.modifiedStartLineNumber ? codeEditor.getBottomForLineNumber(change.modifiedStartLineNumber) : 0;
}
