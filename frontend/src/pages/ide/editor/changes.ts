/**
 * The change bars: the lines of a file changed since its last commit, marked in an editor's gutter and in its ruler
 * on the right, as VS Code marks them.
 */
import { useDebounceFn } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api";

import type { LineChange } from "@services/git/models";
import * as gitApi from "@services/git/service";
import { useGitStore } from "../../../stores/git";
import type { Model } from "./models";

// how each kind of run is drawn: its class in the gutter (themes.css) and its color in the ruler (monaco.ts)
const KINDS = {
  added: { className: "pmt-change-added", color: "pmt.changeAdded" },
  changed: { className: "pmt-change-changed", color: "pmt.changeChanged" },
  removed: { className: "pmt-change-removed", color: "pmt.changeRemoved" },
};
// how long the text rests, in milliseconds, before its bars are asked for again
const WAIT = 400;

/**
 * markChanges keeps an editor's change bars: it asks for them when the editor opens, when the text has rested after a
 * change, and when git changes a repository. A file in no mod's repository has none. It returns a function that stops
 * them.
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

/** toDecoration returns how a run of changed lines is drawn: its bar beside the lines and its mark in the ruler. */
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
