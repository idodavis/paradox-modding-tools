/**
 * Active editor language, caret, and diagnostic counts for the PMT footer.
 */
import { ref, toValue, watch, type MaybeRefOrGetter } from "vue";
import * as vscode from "vscode";
import { isWorkbenchReady, showProblemsPanel, whenWorkbenchReady } from "../ide/workbenchHost";

const LANG_LABEL: Record<string, string> = {
  paradox: "Paradox Script",
  "paradox-gui": "Paradox GUI",
  "paradox-loc": "Paradox Localization",
  "paradox-info": "Paradox Info",
  "paradox-mod": "Paradox Mod Descriptor",
};

/** Subscribe to the workbench caret and Problems totals while `enabled`. */
export function useIdeStatus(enabled: MaybeRefOrGetter<boolean>) {
  const language = ref("");
  const line = ref(0);
  const column = ref(0);
  const errors = ref(0);
  const warnings = ref(0);

  let subs: vscode.Disposable[] = [];

  function clearSubs() {
    for (const d of subs) d.dispose();
    subs = [];
  }

  function refreshEditor() {
    const ed = vscode.window.activeTextEditor;
    if (!ed) {
      language.value = "";
      line.value = 0;
      column.value = 0;
      return;
    }
    const id = ed.document.languageId;
    language.value = LANG_LABEL[id] ?? id;
    line.value = ed.selection.active.line + 1;
    column.value = ed.selection.active.character + 1;
  }

  function refreshDiags() {
    let err = 0;
    let warn = 0;
    for (const [, diags] of vscode.languages.getDiagnostics()) {
      for (const d of diags) {
        switch (d.severity) {
          case vscode.DiagnosticSeverity.Error:
            err++;
            break;
          case vscode.DiagnosticSeverity.Warning:
            warn++;
            break;
          case vscode.DiagnosticSeverity.Information:
          case vscode.DiagnosticSeverity.Hint:
            break;
          default: {
            const _never: never = d.severity;
            void _never;
          }
        }
      }
    }
    errors.value = err;
    warnings.value = warn;
  }

  function bind() {
    clearSubs();
    refreshEditor();
    refreshDiags();
    subs.push(
      vscode.window.onDidChangeActiveTextEditor(() => refreshEditor()),
      vscode.window.onDidChangeTextEditorSelection(() => refreshEditor()),
      vscode.languages.onDidChangeDiagnostics(() => refreshDiags()),
    );
  }

  // the cleanup runs on a change and when the strip goes, so a boot that ends after either binds nothing
  watch(
    () => toValue(enabled),
    async (on, _was, onCleanup) => {
      let gone = false;
      onCleanup(() => {
        gone = true;
        clearSubs();
      });
      if (!on) return;
      await whenWorkbenchReady();
      if (!gone && isWorkbenchReady()) bind();
    },
    { immediate: true },
  );

  return {
    language,
    line,
    column,
    errors,
    warnings,
    openProblems: showProblemsPanel,
  };
}
