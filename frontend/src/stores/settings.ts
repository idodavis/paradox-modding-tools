/**
 * Pinia store for the app-wide settings: the map Go keeps, typed accessors for its keys, and the theme
 * they put on the page.
 */
import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { useColorMode, usePreferredDark } from "@vueuse/core";
import { FILE_TREE_DENSITY_PRESETS, type FileTreeDensityKeyword } from "@pierre/trees";
import { GetSettings, SaveSettings } from "@services/appservice";
import { WORKSPACE_TOOL_NAMES, type WorkspaceToolName } from "../workspaceTools";
import { changed } from "../windows";
import { useWorkspaceStore } from "./workspace";

/** A theme family; its halves are `<id>-dark` and `<id>-light` in styles/themes.css. */
export type ThemeFamily = { id: "pmt" | "catppuccin" | "one" | "pierre" | "ck3" | "eu5" | "vic3"; label: string; note: string; game?: string };
type FamilyId = ThemeFamily["id"];
type Mode = "dark" | "light";
export type ThemeId = `${FamilyId}-${Mode}`;
/** Light or dark as the top bar sets it; system follows Windows. */
export type Appearance = Mode | "system";

const PMT: ThemeFamily = { id: "pmt", label: "PMT", note: "PMT's own" };

/** The seven families, in the order Appearance shows them; each comes dark and light. */
export const THEMES: readonly ThemeFamily[] = [
  PMT,
  { id: "catppuccin", label: "Catppuccin", note: "Mocha and Latte" },
  { id: "one", label: "One", note: "One Dark and One Light" },
  { id: "pierre", label: "Pierre Soft", note: "Pierre Dark Soft and Pierre Light Soft" },
  { id: "ck3", label: "Crusader Kings", note: "CK3 workspaces", game: "ck3" },
  { id: "eu5", label: "Europa Universalis", note: "EU5 workspaces", game: "eu5" },
  { id: "vic3", label: "Victoria", note: "Vic3 workspaces", game: "vic3" },
];
export const MODES: readonly Mode[] = ["dark", "light"];

/** The family with an id; anything else is PMT. */
function familyOf(id: string | undefined): ThemeFamily {
  return THEMES.find((t) => t.id === id) ?? PMT;
}

/** The IDE's switches by key, each with its default (the IdeSettings board). */
export const IDE_SWITCHES = {
  "ide.wordWrap": false,
  "ide.closeBrackets": true,
  "ide.minimap": true,
  "ide.indentGuides": true,
  "ide.bracketColors": true,
  "ide.whitespace": false,
  "ide.folding": true,
  "ide.hover": true,
  "ide.complete": true,
  "ide.predict": true,
  "ide.signature": true,
  "ide.stickyFolders": true,
  "ide.gameFiles": true,
  "ide.rootColors": true,
  "ide.followFile": true,
  "ui.wiki.follow": true,
} as const;
export type IdeSwitch = keyof typeof IDE_SWITCHES;

/** Interface scale steps, in percent. */
export const FONT_SCALES = [80, 90, 100, 110, 120, 130, 140, 150] as const;
const EDITOR_FONT = { min: 10, max: 24 } as const;
const IDE_UI_FONT = { min: 10, max: 20 } as const;

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

/** ui.visibleTools as stored (JSON); missing or invalid means every page, and none leaves the IDE. */
function parseVisibleTools(raw: string | undefined): WorkspaceToolName[] {
  let names: unknown = WORKSPACE_TOOL_NAMES;
  try {
    if (raw) names = JSON.parse(raw);
  } catch {
    /* every page */
  }
  const valid = Array.isArray(names)
    ? WORKSPACE_TOOL_NAMES.filter((name) => names.includes(name))
    : [...WORKSPACE_TOOL_NAMES];
  return valid.length ? valid : ["workspace-ide"];
}

/** App-wide settings, and the theme and text sizes they set. */
export const useSettingsStore = defineStore("settings", () => {
  const values = ref<Record<string, string>>({});

  const fontScale = computed(() => clamp(Number(values.value["ui.fontScale"]) || 100, 80, 150));
  const editorFontSize = computed(() =>
    clamp(Number(values.value["ui.editorFontSize"]) || 14, EDITOR_FONT.min, EDITOR_FONT.max),
  );
  const ideUiFontSize = computed(() =>
    clamp(Number(values.value["ui.ideUiFontSize"]) || 13, IDE_UI_FONT.min, IDE_UI_FONT.max),
  );
  const visibleTools = computed(() => parseVisibleTools(values.value["ui.visibleTools"]));

  /** The family picked in Appearance. */
  const theme = computed(() => familyOf(values.value["_global.theme"]));
  const matchGameTheme = computed(() => on("ui.matchGameTheme", true));
  const appearance = computed<Appearance>(() => {
    const stored = values.value["ui.appearance"];
    return stored === "light" || stored === "system" ? stored : "dark";
  });

  const workspace = useWorkspaceStore();
  const systemDark = usePreferredDark();
  const mode = computed<Mode>(() =>
    appearance.value === "system" ? (systemDark.value ? "dark" : "light") : appearance.value,
  );
  /** The family on the page: the game's while the switch is on, else the pick. */
  const activeFamily = computed(() => {
    const gameId = workspace.activeWorkspace?.gameId;
    return (matchGameTheme.value && THEMES.find((t) => t.game && t.game === gameId)) || theme.value;
  });
  const activeTheme = computed<ThemeId>(() => `${activeFamily.value.id}-${mode.value}`);

  /** Load the settings from Go. */
  async function load(): Promise<void> {
    const loaded = await GetSettings();
    values.value = Object.fromEntries(
      Object.entries(loaded ?? {}).filter((entry): entry is [string, string] => entry[1] !== undefined),
    );
  }

  /** Persist the settings map. */
  function save(): Promise<void> {
    return SaveSettings(values.value).then(changed);
  }

  /** Set one key and persist. */
  function set(key: string, value: string): Promise<void> {
    values.value = { ...values.value, [key]: value };
    return save();
  }

  /** Keys back to their defaults: each written "", so a changed default is followed. */
  function reset(keys: readonly string[]): Promise<void> {
    values.value = { ...values.value, ...Object.fromEntries(keys.map((key) => [key, ""])) };
    return save();
  }

  /** Pick a family for every workspace, so matching the workspace's game turns off. */
  function pickTheme(id: FamilyId): Promise<void> {
    values.value = { ...values.value, "_global.theme": id, "ui.matchGameTheme": "off" };
    return save();
  }

  /** The top bar's button: the other mode, which stops following Windows. */
  function toggleMode(): Promise<void> {
    return set("ui.appearance", mode.value === "dark" ? "light" : "dark");
  }

  /** Follow Windows' light or dark, or keep the mode showing now. */
  function followSystem(on: boolean): Promise<void> {
    return set("ui.appearance", on ? "system" : mode.value);
  }

  /** Show or hide a page in the workspace's bar. */
  function showTool(name: WorkspaceToolName, on: boolean): Promise<void> {
    const others = visibleTools.value.filter((tool) => tool !== name);
    return set("ui.visibleTools", JSON.stringify(on ? [...others, name] : others));
  }

  /** A switch (an IDE switch, a check's ide.diag.<code>, matching the game's theme): the stored choice, else its
   * default, an IDE switch's from IDE_SWITCHES. */
  function on(key: string, fallback = (IDE_SWITCHES as Record<string, boolean>)[key] ?? false): boolean {
    const stored = values.value[key];
    return stored ? stored === "on" : fallback;
  }

  function setOn(key: string, on: boolean): Promise<void> {
    return set(key, on ? "on" : "off");
  }

  // a new line's indentation and Format document's (ideservice.go reads the same keys)
  const indentSpaces = computed(() => values.value["ide.indent"] === "spaces");
  const indentSize = computed(() => clamp(Number(values.value["ide.indentSize"]) || 4, 1, 8));
  const indentUnit = computed(() => (indentSpaces.value ? " ".repeat(indentSize.value) : "\t"));
  // the explorer's rows, one of @pierre/trees' own sizes; source control's rows and graph take its height
  const treeDensity = computed<FileTreeDensityKeyword>(() => {
    const stored = values.value["ide.density"];
    return stored === "default" || stored === "relaxed" ? stored : "compact";
  });
  const treeRow = computed(() => FILE_TREE_DENSITY_PRESETS[treeDensity.value].itemHeight);
  // the editors' and compares' code: the font named (one installed, else Geist Mono), its size and line height
  const editorFont = computed(() => values.value["ui.editorFont"]?.trim() || "Geist Mono");
  const roomy = computed(() => values.value["ide.lineHeight"] === "roomy");
  const codeFont = computed(() => ({
    fontFamily: [...new Set([editorFont.value.replaceAll('"', ""), "Geist Mono"])].map((font) => `"${font}"`).join(", ") + ", monospace",
    fontSize: editorFontSize.value,
    lineHeight: roomy.value ? 1.6 : 1.35,
  }));

  // the one place the theme reaches the page and Nuxt UI's color mode
  const colorMode = useColorMode();
  watch(
    activeTheme,
    (active) => {
      document.documentElement.dataset.theme = active;
      colorMode.value = mode.value;
    },
    { immediate: true },
  );
  watch(
    [fontScale, ideUiFontSize],
    ([scale, uiFont]) => {
      const root = document.documentElement;
      root.style.fontSize = `${(16 * scale) / 100}px`;
      root.style.setProperty("--ide-ui-font", `${uiFont}px`);
    },
    { immediate: true },
  );

  return {
    values,
    fontScale,
    editorFontSize,
    ideUiFontSize,
    visibleTools,
    theme,
    matchGameTheme,
    appearance,
    mode,
    activeFamily,
    activeTheme,
    load,
    set,
    pickTheme,
    toggleMode,
    followSystem,
    showTool,
    on,
    setOn,
    indentSpaces,
    indentSize,
    indentUnit,
    treeDensity,
    treeRow,
    editorFont,
    roomy,
    codeFont,
    reset,
  };
});
