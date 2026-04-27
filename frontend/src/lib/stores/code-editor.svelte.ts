import "@utils/monaco-environment";
import loader from "@monaco-editor/loader";
import * as monaco from "monaco-editor";
import { writable } from "svelte/store";
import {
  JOMINI_LANGUAGE_ID,
  jominiLanguageConfiguration,
  jominiMonarchLanguage,
} from "@utils/jomini-monarch";

type Monaco = typeof monaco;

const BUILTIN_THEMES = [
  { id: "vs-dark", label: "VS Dark" },
  { id: "vs", label: "VS Light" },
  { id: "hc-black", label: "High Contrast Dark" },
  { id: "hc-light", label: "High Contrast Light" },
] as const;

export const THEME_OPTIONS = [
  ...BUILTIN_THEMES,
] as const;

export type CodeTheme = (typeof THEME_OPTIONS)[number]["id"];

export const LANGUAGE_OPTIONS = [
  { id: "jomini", label: "Jomini (Paradox)" },
  { id: "plaintext", label: "Plain Text" },
  { id: "json", label: "JSON" },
  { id: "javascript", label: "JavaScript" },
  { id: "python", label: "Python" },
  { id: "go", label: "Go" },
  { id: "rust", label: "Rust" },
] as const;

export type CodeLanguage = (typeof LANGUAGE_OPTIONS)[number]["id"];

export const codeTheme = writable<CodeTheme>("vs-dark");
export const codeLanguage = writable<CodeLanguage>("jomini");

const baseEditorOptions = {
  automaticLayout: true,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  fontSize: 13,
  tabSize: 2,
  wordWrap: "off" as const,
  overviewRulerBorder: false,
  renderLineHighlight: "all" as const,
};

let monacoReady: Promise<Monaco> | null = null;
let monacoConfigured = false;

function registerJomini(api: Monaco) {
  const id = JOMINI_LANGUAGE_ID;
  if (!api.languages.getLanguages().some((language) => language.id === id)) {
    api.languages.register({ id });
  }

  api.languages.setMonarchTokensProvider(id, jominiMonarchLanguage);
  api.languages.setLanguageConfiguration(id, jominiLanguageConfiguration);
}

function configureMonaco(api: Monaco) {
  if (monacoConfigured) return;
  monacoConfigured = true;

  registerJomini(api);
}

export async function getMonaco() {
  if (!monacoReady) {
    loader.config({ monaco });
    monacoReady = loader.init().then((api) => {
      configureMonaco(api);
      return api;
    });
  }

  return monacoReady;
}

function lineNumbers(firstLineNumber = 1) {
  return firstLineNumber === 1 ? "on" : (lineNumber: number) => String(lineNumber + firstLineNumber - 1);
}

function setModelValue(model: monaco.editor.ITextModel | null, value: string | undefined) {
  const next = value ?? "";
  if (model && model.getValue() !== next) {
    model.setValue(next);
  }
}

export type MonacoEditorParams = {
  value: string;
  language: CodeLanguage;
  theme: CodeTheme;
  readOnly?: boolean;
  firstLineNumber?: number;
  placeholder?: string;
  onChange?: (value: string) => void;
};

export type MonacoDiffParams = {
  original: string;
  modified: string;
  language: CodeLanguage;
  theme: CodeTheme;
  renderSideBySide?: boolean;
  firstLineNumber?: number;
  originalFirstLine?: number;
  modifiedFirstLine?: number;
};

export function monacoEditor(node: HTMLDivElement, params: MonacoEditorParams) {
  let current = params;
  let editor: monaco.editor.IStandaloneCodeEditor | null = null;
  let model: monaco.editor.ITextModel | null = null;
  let applyingExternalValue = false;
  let disposed = false;
  let changeListener: monaco.IDisposable | null = null;

  const sync = async () => {
    const api = await getMonaco();
    if (disposed) return;

    if (!editor) {
      model = api.editor.createModel(current.value ?? "", current.language);
      editor = api.editor.create(node, {
        ...baseEditorOptions,
        model,
        readOnly: current.readOnly ?? true,
        lineNumbers: lineNumbers(current.firstLineNumber),
        placeholder: current.placeholder,
      });

      changeListener = editor.onDidChangeModelContent(() => {
        if (applyingExternalValue || current.readOnly || !current.onChange || !editor) return;
        current.onChange(editor.getValue());
      });
    }

    api.editor.setTheme(current.theme);

    if (model) {
      api.editor.setModelLanguage(model, current.language);
      applyingExternalValue = true;
      setModelValue(model, current.value);
      applyingExternalValue = false;
    }

    editor.updateOptions({
      readOnly: current.readOnly ?? true,
      lineNumbers: lineNumbers(current.firstLineNumber),
      placeholder: current.placeholder,
    });
  };

  void sync();

  return {
    update(next: MonacoEditorParams) {
      current = next;
      void sync();
    },
    destroy() {
      disposed = true;
      changeListener?.dispose();
      editor?.dispose();
      model?.dispose();
    },
  };
}

export function monacoDiff(node: HTMLDivElement, params: MonacoDiffParams) {
  let current = params;
  let diffEditor: monaco.editor.IStandaloneDiffEditor | null = null;
  let originalModel: monaco.editor.ITextModel | null = null;
  let modifiedModel: monaco.editor.ITextModel | null = null;
  let disposed = false;

  const sync = async () => {
    const api = await getMonaco();
    if (disposed) return;

    if (!diffEditor) {
      originalModel = api.editor.createModel(current.original ?? "", current.language);
      modifiedModel = api.editor.createModel(current.modified ?? "", current.language);
      diffEditor = api.editor.createDiffEditor(node, {
        ...baseEditorOptions,
        readOnly: true,
        originalEditable: false,
        renderSideBySide: current.renderSideBySide ?? true,
      });
      diffEditor.setModel({ original: originalModel, modified: modifiedModel });
    }

    api.editor.setTheme(current.theme);

    if (originalModel && modifiedModel) {
      api.editor.setModelLanguage(originalModel, current.language);
      api.editor.setModelLanguage(modifiedModel, current.language);
      setModelValue(originalModel, current.original);
      setModelValue(modifiedModel, current.modified);
    }

    diffEditor.updateOptions({
      readOnly: true,
      originalEditable: false,
      renderSideBySide: current.renderSideBySide ?? true,
    });

    diffEditor.getOriginalEditor().updateOptions({
      lineNumbers: lineNumbers(current.originalFirstLine ?? current.firstLineNumber ?? 1),
    });
    diffEditor.getModifiedEditor().updateOptions({
      lineNumbers: lineNumbers(current.modifiedFirstLine ?? current.firstLineNumber ?? 1),
    });
  };

  void sync();

  return {
    update(next: MonacoDiffParams) {
      current = next;
      void sync();
    },
    destroy() {
      disposed = true;
      diffEditor?.dispose();
      originalModel?.dispose();
      modifiedModel?.dispose();
    },
  };
}
