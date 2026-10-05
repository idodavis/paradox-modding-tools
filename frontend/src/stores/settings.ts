/**
 * Pinia store for persisted app settings (UI scale, theme keys, misc).
 */
import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { GetSettings, SaveSettings } from "@services/appservice";
import { applyEditorFontSize } from "../ide/themeBridge";
import { WORKSPACE_TOOL_NAMES, type WorkspaceToolName } from "../workspaceTools";

/** Drop undefined values from a settings map. */
function normalizeSettings(
  input: Record<string, string | undefined> | null | undefined,
): Record<string, string> {
  return Object.fromEntries(
    Object.entries(input ?? {}).filter(
      (entry): entry is [string, string] => entry[1] !== undefined,
    ),
  );
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

const FONT_SCALE_MIN = 85;
const FONT_SCALE_MAX = 130;
const EDITOR_FONT_MIN = 10;
const EDITOR_FONT_MAX = 24;
const IDE_UI_FONT_MIN = 10;
const IDE_UI_FONT_MAX = 20;

/** Parse ui.visibleTools JSON; missing or invalid means all tools. */
function parseVisibleTools(raw: string | undefined): WorkspaceToolName[] {
  if (raw === undefined || raw === "") return [...WORKSPACE_TOOL_NAMES];
  try {
    const arr: unknown = JSON.parse(raw);
    if (!Array.isArray(arr)) return [...WORKSPACE_TOOL_NAMES];
    const names = arr.filter((n): n is string => typeof n === "string");
    return [...new Set(names)].filter((n): n is WorkspaceToolName =>
      (WORKSPACE_TOOL_NAMES as readonly string[]).includes(n),
    );
  } catch {
    return [...WORKSPACE_TOOL_NAMES];
  }
}

/** Apply UI font scale and workbench chrome font to the document. */
export function applyFontCss(fontScale: number, ideUiFontSize = 13): void {
  const root = document.documentElement;
  root.style.setProperty("--pmt-font-scale", String(fontScale / 100));
  root.style.setProperty("--pmt-ide-ui-font", `${ideUiFontSize}px`);
  root.style.fontSize = `${(16 * fontScale) / 100}px`;
}

/** App-wide settings including UI scale. */
export const useSettingsStore = defineStore("settings", () => {
  const values = ref<Record<string, string>>({});
  const loading = ref(false);
  const saving = ref(false);

  const fontScale = computed(() =>
    clamp(Number(values.value["ui.fontScale"]) || 100, FONT_SCALE_MIN, FONT_SCALE_MAX),
  );

  const editorFontSize = computed(() =>
    clamp(
      Number(values.value["ui.editorFontSize"]) || 14,
      EDITOR_FONT_MIN,
      EDITOR_FONT_MAX,
    ),
  );

  const visibleTools = computed(() => parseVisibleTools(values.value["ui.visibleTools"]));

  const ideUiFontSize = computed(() =>
    clamp(
      Number(values.value["ui.ideUiFontSize"]) || 13,
      IDE_UI_FONT_MIN,
      IDE_UI_FONT_MAX,
    ),
  );

  /** Load settings from the backend and apply fonts. */
  async function load(): Promise<void> {
    loading.value = true;
    try {
      values.value = normalizeSettings(await GetSettings());
    } finally {
      loading.value = false;
    }
  }

  /** Persist current settings map. */
  async function save(): Promise<void> {
    saving.value = true;
    try {
      await SaveSettings(values.value);
    } finally {
      saving.value = false;
    }
  }

  /** Set one key and optionally persist immediately. */
  async function set(key: string, value: string, persist = true): Promise<void> {
    values.value = { ...values.value, [key]: value };
    if (persist) await save();
  }

  /** Update font scale percent and persist. */
  async function setFontScale(percent: number): Promise<void> {
    await set(
      "ui.fontScale",
      String(clamp(percent, FONT_SCALE_MIN, FONT_SCALE_MAX)),
    );
  }

  /** Persist editor font size (px). */
  async function setEditorFontSize(px: number): Promise<void> {
    await set(
      "ui.editorFontSize",
      String(clamp(px, EDITOR_FONT_MIN, EDITOR_FONT_MAX)),
    );
  }

  /** Persist workbench UI font size (px). */
  async function setIdeUiFontSize(px: number): Promise<void> {
    await set(
      "ui.ideUiFontSize",
      String(clamp(px, IDE_UI_FONT_MIN, IDE_UI_FONT_MAX)),
    );
  }

  /** Persist which workspace tool pills are shown. Empty means IDE-only in the toolbar. */
  async function setVisibleTools(names: string[]): Promise<void> {
    const next = [...new Set(names)].filter(
      (n): n is WorkspaceToolName =>
        (WORKSPACE_TOOL_NAMES as readonly string[]).includes(n),
    );
    await set("ui.visibleTools", JSON.stringify(next));
  }

  watch(
    [fontScale, ideUiFontSize],
    ([scale, ui]) => applyFontCss(scale, ui),
    { immediate: true },
  );
  watch(editorFontSize, (px) => applyEditorFontSize(px), { immediate: true });

  return {
    values,
    loading,
    saving,
    fontScale,
    editorFontSize,
    ideUiFontSize,
    visibleTools,
    load,
    save,
    set,
    setFontScale,
    setEditorFontSize,
    setIdeUiFontSize,
    setVisibleTools,
    FONT_SCALE_MIN,
    FONT_SCALE_MAX,
    EDITOR_FONT_MIN,
    EDITOR_FONT_MAX,
    IDE_UI_FONT_MIN,
    IDE_UI_FONT_MAX,
  };
});
