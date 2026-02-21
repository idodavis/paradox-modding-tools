/**
 * monaco.svelte.ts — single source of truth for all Monaco editor concerns.
 *
 * Exports:
 *   Types/constants  – MonacoApi, MonacoEditor, MonacoModel, CODE_THEMES, CODE_LANGUAGES …
 *   Stores           – codeTheme, codeLanguage
 *   Lifecycle ctx    – EditorCtx, DiffCtx (reactive classes, instantiate in component script)
 *   Helpers          – setLang, disposeMany, wireSyncedScroll
 *   Reactive reads   – monacoActive.lang / monacoActive.theme ($state object, mirrors stores)
 */

import { init } from "modern-monaco";
import { writable } from "svelte/store";
import { get } from "svelte/store";

// ─── Types ────────────────────────────────────────────────────────────────────

export type MonacoApi = Awaited<ReturnType<typeof init>>;
export type MonacoEditor = ReturnType<MonacoApi["editor"]["create"]>;
export type MonacoModel = ReturnType<MonacoApi["editor"]["createModel"]>;
export type MonacoEditorOptions = Parameters<MonacoApi["editor"]["create"]>[1];
export type MonacoDiffEditorOptions = Parameters<
  MonacoApi["editor"]["createDiffEditor"]
>[1];

// ─── Constants ────────────────────────────────────────────────────────────────

export const CODE_THEMES = [
  "one-dark-pro",
  "one-light",
  "ayu-dark",
  "ayu-light",
  "github-dark-default",
  "github-light-default",
  "material-theme-darker",
  "material-theme-lighter",
  "material-theme-palenight",
  "tokyo-night",
  "catppuccin-latte",
];

export const CODE_LANGUAGES = [
  "hcl",
  "plaintext",
  "json",
  "yaml",
  "typescript",
  "javascript",
  "html",
  "css",
  "markdown",
  "xml",
  "shellscript",
  "python",
  "cpp",
  "go",
  "rust",
  "java",
];

// ─── Stores ───────────────────────────────────────────────────────────────────

type CodeTheme = (typeof CODE_THEMES)[number];
type CodeLanguage = (typeof CODE_LANGUAGES)[number];

export const codeTheme = writable<CodeTheme>("one-dark-pro");
export const codeLanguage = writable<CodeLanguage>("hcl");

// Module-level $state mirrors — usable in .svelte.ts class $effects (no $ syntax needed).
export const monacoActive = $state({
  theme: get(codeTheme),
  lang: get(codeLanguage),
});
codeTheme.subscribe((v) => (monacoActive.theme = v));
codeLanguage.subscribe((v) => (monacoActive.lang = v));

// ─── Monaco singleton ─────────────────────────────────────────────────────────

let monacoPromise: Promise<MonacoApi> | null = null;

export function getMonaco(): Promise<MonacoApi> {
  if (!monacoPromise) {
    monacoPromise = init({
      defaultTheme: "one-dark-pro",
      themes: CODE_THEMES,
      langs: CODE_LANGUAGES,
    }).catch(() =>
      init({
        defaultTheme: "one-dark-pro",
        themes: CODE_THEMES,
        langs: CODE_LANGUAGES,
      }),
    );
  }
  return monacoPromise;
}

export function loadMonaco(onReady: (monaco: MonacoApi) => void): () => void {
  let active = true;
  void getMonaco().then((m) => {
    if (active) onReady(m);
  });
  return () => {
    active = false;
  };
}

// ─── Default options ──────────────────────────────────────────────────────────

export function getBaseEditorOptions(): MonacoEditorOptions {
  return {
    automaticLayout: true,
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    wordWrap: "off",
    glyphMargin: false,
    folding: true,
    lineNumbersMinChars: 3,
    lineDecorationsWidth: 8,
    padding: { top: 8, bottom: 8 },
  };
}

export function getBaseDiffEditorOptions(): MonacoDiffEditorOptions {
  return {
    originalEditable: false,
    readOnly: true,
    automaticLayout: true,
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    padding: { top: 16, bottom: 16 },
  };
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

export function setLang(
  monaco: MonacoApi,
  lang: string,
  ...models: Array<MonacoModel | null>
) {
  for (const m of models)
    if (m) (monaco as any).editor.setModelLanguage(m, lang);
}

export function disposeMany(
  ...items: Array<{ dispose?: () => void } | null | undefined>
) {
  for (const item of items) item?.dispose?.();
}

export function wireSyncedScroll(
  editors: MonacoEditor[],
  onAnyScroll?: () => void,
): () => void {
  let syncing = false;
  const ds = editors.map((src) =>
    src.onDidScrollChange(() => {
      if (syncing) {
        onAnyScroll?.();
        return;
      }
      syncing = true;
      const top = src.getScrollTop();
      const left = src.getScrollLeft();
      for (const t of editors) {
        if (t === src) continue;
        t.setScrollTop(top);
        t.setScrollLeft(left);
      }
      syncing = false;
      onAnyScroll?.();
    }),
  );
  return () => disposeMany(...ds);
}

function upsertModel(
  monaco: MonacoApi,
  current: MonacoModel | null,
  value: string,
  lang: string,
): MonacoModel {
  if (!current) return monaco.editor.createModel(value, lang);
  if (current.getLanguageId() !== lang) {
    current.dispose();
    return monaco.editor.createModel(value, lang);
  }
  if (current.getValue() !== value) current.setValue(value);
  return current;
}

// ─── EditorCtx ───────────────────────────────────────────────────────────────

export class EditorCtx {
  monaco = $state<MonacoApi | null>(null);
  editor = $state<MonacoEditor | null>(null);
  model = $state<MonacoModel | null>(null);
  host = $state<HTMLElement | null>(null);

  private stopLoad: () => void;
  private ownedModel = false;
  private _value = $state("");
  private _lang = $state<string | undefined>(undefined);
  private extraOptions: any;
  readonly readOnly: boolean;

  constructor(opts: { readOnly?: boolean; options?: any } = {}) {
    this.readOnly = opts.readOnly ?? true;
    this.extraOptions = opts.options ?? {};
    this.stopLoad = loadMonaco((m) => (this.monaco = m));

    $effect(() => {
      if (!this.monaco || !this.host || this.editor) return;
      this.editor = this.monaco.editor.create(this.host, {
        ...getBaseEditorOptions(),
        ...this.extraOptions,
        readOnly: this.readOnly,
        model: this.model ?? undefined,
      } as any);
    });

    $effect(() => {
      if (this.editor) this.editor.setModel((this.model ?? null) as any);
    });

    $effect(() => {
      if (!this.monaco || !this._value) return;
      const l = this._lang ?? monacoActive.lang;
      this.ownedModel = true;
      this.model = upsertModel(this.monaco, this.model, this._value, l);
    });

    $effect(() => {
      if (this.monaco) this.monaco.editor.setTheme(monacoActive.theme);
    });
  }

  upsert(value: string, lang?: string) {
    this._value = value;
    this._lang = lang;
  }

  dispose() {
    this.stopLoad();
    if (this.ownedModel) this.model?.dispose();
    this.editor?.dispose();
  }
}

// ─── DiffCtx ─────────────────────────────────────────────────────────────────

export class DiffCtx {
  monaco = $state<MonacoApi | null>(null);
  diffEditor = $state<any>(null);
  originalModel = $state<MonacoModel | null>(null);
  modifiedModel = $state<MonacoModel | null>(null);
  host = $state<HTMLElement | null>(null);
  renderSideBySide = $state(true);

  private stopLoad: () => void;
  private _orig = $state("");
  private _mod = $state("");
  private _lang = $state<string | undefined>(undefined);
  private extraOptions: any;

  constructor(opts: { renderSideBySide?: boolean; options?: any } = {}) {
    this.renderSideBySide = opts.renderSideBySide ?? true;
    this.extraOptions = opts.options ?? {};
    this.stopLoad = loadMonaco((m) => (this.monaco = m));

    $effect(() => {
      if (!this.monaco || !this.host || this.diffEditor) return;
      this.diffEditor = this.monaco.editor.createDiffEditor(this.host, {
        ...getBaseDiffEditorOptions(),
        renderSideBySide: this.renderSideBySide,
        ...this.extraOptions,
      } as any);
    });

    $effect(() => {
      if (this.diffEditor)
        this.diffEditor.updateOptions({
          renderSideBySide: this.renderSideBySide,
        });
    });

    $effect(() => {
      if (!this.monaco || !this._orig) return;
      const l = this._lang ?? monacoActive.lang;
      this.originalModel = upsertModel(
        this.monaco,
        this.originalModel,
        this._orig,
        l,
      );
      this.modifiedModel = upsertModel(
        this.monaco,
        this.modifiedModel,
        this._mod,
        l,
      );
    });

    $effect(() => {
      if (!this.diffEditor || !this.originalModel || !this.modifiedModel)
        return;
      this.diffEditor.setModel({
        original: this.originalModel,
        modified: this.modifiedModel,
      });
    });

    $effect(() => {
      if (this.monaco) this.monaco.editor.setTheme(monacoActive.theme);
    });
  }

  setContent(original: string, modified: string, lang?: string) {
    this._orig = original;
    this._mod = modified;
    this._lang = lang;
  }

  dispose() {
    this.stopLoad();
    this.originalModel?.dispose();
    this.modifiedModel?.dispose();
    this.diffEditor?.dispose();
  }
}
