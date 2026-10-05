/**
 * The workbench's color themes, a dark and a light one per family in stores/settings.ts, read from the
 * tokens of an element carrying that `data-theme` (styles/themes.css).
 */
import { registerExtension } from "@codingame/monaco-vscode-api/extensions";
import { ExtensionHostKind } from "@codingame/monaco-vscode-extensions-service-override";
import darkPlus from
  "@codingame/monaco-vscode-theme-defaults-default-extension/resources/dark_plus.json";
import lightPlus from
  "@codingame/monaco-vscode-theme-defaults-default-extension/resources/light_plus.json";
import darkVs from
  "@codingame/monaco-vscode-theme-defaults-default-extension/resources/dark_vs.json";
import lightVs from
  "@codingame/monaco-vscode-theme-defaults-default-extension/resources/light_vs.json";
import Color from "colorjs.io";
import { MODES, THEMES, type ThemeId } from "../stores/settings";
import { PARADOX_SCOPES } from "../lib/code";

/** Each family's two halves, as the workbench lists them. */
const HALVES = THEMES.flatMap((family) =>
  MODES.map((mode) => ({
    id: `${family.id}-${mode}` as const,
    label: `${family.label} ${mode === "dark" ? "Dark" : "Light"}`,
    dark: mode === "dark",
  })),
);

const TOKENS = [
  "bg-0", "bg-1", "bg-2", "bg-3", "bg-4", "line-1", "line-2", "fg-1", "fg-2", "fg-3",
  "accent", "accent-ink", "link", "red", "orange", "blue", "green",
  "syn-kw", "syn-str", "syn-num", "syn-fn", "syn-ref", "syn-com",
] as const;
type Tokens = Record<(typeof TOKENS)[number], string>;

/** The theme on the page. */
export function pageTheme(): ThemeId {
  return (document.documentElement.dataset.theme as ThemeId | undefined) ?? "pmt-dark";
}

/** A theme's tokens as hex, read from an element carrying its `data-theme`. */
function tokensOf(id: ThemeId): Tokens {
  const el = document.body.appendChild(document.createElement("div"));
  el.dataset.theme = id;
  const style = getComputedStyle(el);
  const tokens = Object.fromEntries(
    TOKENS.map((name) => [name, new Color(style.getPropertyValue(`--${name}`).trim()).toString({ format: "hex" })]),
  ) as Tokens;
  el.remove();
  return tokens;
}

/** A hex color at an alpha given as two hex digits. */
function a(hex: string, alpha: string): string {
  const c = new Color(hex);
  c.alpha = parseInt(alpha, 16) / 255;
  return c.to("srgb").toString({ format: "hex" });
}

/** The workbench keys VS Code reads, each from the token with the same role on the boards. */
function workbenchColors(t: Tokens): Record<string, string> {
  return {
    foreground: t["fg-1"], descriptionForeground: t["fg-3"], "icon.foreground": t["fg-2"],
    errorForeground: t.red, focusBorder: a(t.accent, "80"), "widget.border": t["line-2"],
    "editor.background": t["bg-0"], "editor.foreground": t["fg-1"],
    "editor.lineHighlightBackground": t["bg-3"], "editor.lineHighlightBorder": a(t["bg-3"], "00"),
    "editor.selectionBackground": a(t.accent, "40"), "editor.inactiveSelectionBackground": a(t.accent, "20"),
    "editor.selectionHighlightBackground": a(t.accent, "26"),
    "editorLineNumber.foreground": t["fg-3"], "editorLineNumber.activeForeground": t["fg-1"],
    "editorCursor.foreground": t.accent,
    "editorWidget.background": t["bg-3"], "editorWidget.foreground": t["fg-1"], "editorWidget.border": t["line-2"],
    "editorHoverWidget.background": t["bg-3"], "editorHoverWidget.border": t["line-2"],
    "editorSuggestWidget.background": t["bg-3"], "editorSuggestWidget.border": t["line-2"],
    "editorSuggestWidget.selectedBackground": a(t.accent, "26"),
    "editorGroup.border": t["line-1"], "editorGroupHeader.tabsBackground": t["bg-2"],
    "editorGroupHeader.tabsBorder": t["line-1"],
    "tab.activeBackground": t["bg-0"], "tab.activeForeground": t["fg-1"], "tab.activeBorderTop": t.accent,
    "tab.inactiveBackground": t["bg-2"], "tab.inactiveForeground": t["fg-3"], "tab.border": t["line-1"],
    "tab.hoverBackground": t["bg-3"],
    "sideBar.background": t["bg-2"], "sideBar.foreground": t["fg-2"], "sideBar.border": t["line-1"],
    "sideBarTitle.foreground": t["fg-2"], "sideBarSectionHeader.background": t["bg-2"],
    "sideBarSectionHeader.foreground": t["fg-2"], "sideBarSectionHeader.border": t["line-1"],
    "list.activeSelectionBackground": t["bg-4"], "list.activeSelectionForeground": t["fg-1"],
    "list.inactiveSelectionBackground": t["bg-4"], "list.inactiveSelectionForeground": t["fg-1"],
    "list.hoverBackground": t["bg-3"], "list.focusBackground": t["bg-4"], "list.focusOutline": a(t.accent, "73"),
    "list.highlightForeground": t.accent, "tree.indentGuidesStroke": t["line-2"],
    "activityBar.background": t["bg-2"], "activityBar.foreground": t["fg-1"],
    "activityBar.inactiveForeground": t["fg-3"], "activityBar.border": t["line-1"],
    "activityBar.activeBorder": t.accent, "activityBarBadge.background": t.accent,
    "activityBarBadge.foreground": t["accent-ink"],
    "statusBar.background": t["bg-2"], "statusBar.foreground": t["fg-3"], "statusBar.border": t["line-1"],
    "statusBar.noFolderBackground": t["bg-2"], "statusBarItem.remoteBackground": t.link,
    "statusBarItem.remoteForeground": t["bg-1"],
    "panel.background": t["bg-2"], "panel.border": t["line-1"], "panelTitle.activeBorder": t.accent,
    "panelTitle.activeForeground": t["fg-1"], "panelTitle.inactiveForeground": t["fg-3"],
    "titleBar.activeBackground": t["bg-2"], "titleBar.activeForeground": t["fg-2"],
    "titleBar.inactiveBackground": t["bg-2"], "titleBar.inactiveForeground": t["fg-3"], "titleBar.border": t["line-1"],
    "input.background": t["bg-0"], "input.foreground": t["fg-1"], "input.border": t["line-2"],
    "input.placeholderForeground": t["fg-3"], "inputOption.activeBorder": t.accent,
    "inputOption.activeBackground": a(t.accent, "26"),
    "dropdown.background": t["bg-3"], "dropdown.foreground": t["fg-1"], "dropdown.border": t["line-2"],
    "dropdown.listBackground": t["bg-3"],
    "button.background": t.accent, "button.foreground": t["accent-ink"], "button.hoverBackground": a(t.accent, "d9"),
    "button.secondaryBackground": t["bg-3"], "button.secondaryForeground": t["fg-1"],
    "badge.background": t["bg-4"], "badge.foreground": t["fg-2"], "progressBar.background": t.accent,
    "scrollbarSlider.background": t["line-2"], "scrollbarSlider.hoverBackground": a(t["fg-3"], "80"),
    "scrollbarSlider.activeBackground": t["fg-3"],
    "listFilterWidget.background": t["bg-3"], "listFilterWidget.outline": a(t.accent, "80"),
    "searchEditor.findMatchBackground": a(t.accent, "33"), "search.resultsInfoForeground": t["fg-3"],
    "minimap.background": t["bg-0"], "peekViewEditor.background": t["bg-0"], "peekViewResult.background": t["bg-2"],
    "peekViewTitle.background": t["bg-3"], "quickInput.background": t["bg-3"], "quickInput.foreground": t["fg-1"],
    "menu.background": t["bg-3"], "menu.foreground": t["fg-1"], "menu.border": t["line-2"],
    "menu.selectionBackground": a(t.accent, "26"), "menu.selectionForeground": t["fg-1"],
    "menu.separatorBackground": t["line-1"],
    "notificationCenterHeader.background": t["bg-3"], "notifications.background": t["bg-3"],
    "notifications.foreground": t["fg-1"], "notifications.border": t["line-2"],
    "editorGutter.addedBackground": t.green, "editorGutter.deletedBackground": t.red,
    "editorGutter.modifiedBackground": t.blue, "editorInfo.foreground": t.blue,
    "editorWarning.foreground": t.orange, "editorError.foreground": t.red,
    "diffEditor.insertedTextBackground": a(t.green, "29"), "diffEditor.removedTextBackground": a(t.red, "29"),
    "textLink.foreground": t.link, "textLink.activeForeground": t.accent,
  };
}

/** The Paradox grammars' scopes in the boards' six syntax colors. */
function paradoxTokenColors(t: Tokens): unknown[] {
  return PARADOX_SCOPES.map(({ token, italic, scope }) => ({
    scope,
    settings: { foreground: t[token], ...(italic && { fontStyle: "italic" }) },
  }));
}

/** One theme's document: its colors, VS Code's own token colors, and the Paradox scopes. */
function themeDocument(theme: (typeof HALVES)[number]): Record<string, unknown> {
  const tokens = tokensOf(theme.id);
  const base: { tokenColors?: unknown[] }[] = theme.dark ? [darkVs, darkPlus] : [lightVs, lightPlus];
  return {
    $schema: "vscode://schemas/color-theme",
    name: theme.id,
    type: theme.dark ? "dark" : "light",
    colors: workbenchColors(tokens),
    tokenColors: [...base.flatMap((doc) => doc.tokenColors ?? []), ...paradoxTokenColors(tokens)],
    semanticHighlighting: false,
  };
}

/** Contribute one color theme per theme. */
export async function registerPmtColorThemes(): Promise<void> {
  const { registerFileUrl, whenReady } = registerExtension(
    {
      name: "pmt-color-themes",
      publisher: "pmt",
      version: "1.0.0",
      engines: { vscode: "*" },
      contributes: {
        themes: HALVES.map((theme) => ({
          id: theme.id,
          label: theme.label,
          uiTheme: theme.dark ? "vs-dark" : "vs",
          path: `./themes/${theme.id}-color-theme.json`,
        })),
      },
    },
    ExtensionHostKind.LocalProcess,
  );
  for (const theme of HALVES) {
    registerFileUrl(
      `./themes/${theme.id}-color-theme.json`,
      `data:application/json,${encodeURIComponent(JSON.stringify(themeDocument(theme)))}`,
    );
  }
  await whenReady();
}
