/**
 * The change bars: the lines of a file changed since its last commit, marked in an editor's gutter and right ruler.
 */
import { useDebounceFn } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";

import type { LineChange } from "@services/git/models";
import * as gitApi from "@services/git/service";
import { useGitStore } from "../../../stores/git";
import type { Model } from "./models";

// the gutter class (themes.css) and the ruler color (monaco.ts) of each kind of changed run
const KINDS = {
  added: { className: "pmt-change-added", color: "pmt.changeAdded" },
  changed: { className: "pmt-change-changed", color: "pmt.changeChanged" },
  removed: { className: "pmt-change-removed", color: "pmt.changeRemoved" },
};
// how many milliseconds the text must rest after an edit before the bars are asked for again
const WAIT = 400;

/**
 * markChanges draws the change bars of an editor. It refreshes them when the editor opens, when the text rests after
 * an edit, and when git changes. A file outside every mod's repository has none. It returns a function that stops the
 * refreshing and clears the bars.
 */
export function markChanges(codeEditor: monaco.editor.IStandaloneCodeEditor, model: Model): () => void {
  const bars = codeEditor.createDecorationsCollection();
  const mark = useDebounceFn(async () => {
    const isInRepository = !!useGitStore().findRoot(model.place);
    const changes = isInRepository
      ? await gitApi.DiffFile(model.place.path, model.model.getValue()).catch(() => [])
      : [];
    bars.set((changes ?? []).map(toDecoration));
  }, WAIT);
  void mark();
  const edits = model.model.onDidChangeContent(() => void mark());
  const stop = Events.On("git:changed", () => void mark());
  return () => {
    edits.dispose();
    stop();
    bars.clear();
  };
}

/** toDecoration returns the gutter bar and ruler mark for one run of changed lines. */
function toDecoration(change: LineChange): monaco.editor.IModelDeltaDecoration {
  const kind = KINDS[change.kind as keyof typeof KINDS];
  return {
    range: new monaco.Range(change.start, 1, change.end, 1),
    options: {
      linesDecorationsClassName: kind.className,
      overviewRuler: { color: { id: kind.color }, position: monaco.editor.OverviewRulerLane.Left },
    },
  };
}
