/**
 * The workbench's user settings, written whole whenever one changes: the page's theme and the editor's text
 * size from the settings store, and the workspace's Hide binaries. What never changes is the workbench's
 * configurationDefaults (workbenchHost.ts).
 */
import { shallowRef, watch } from "vue";
import { updateUserConfiguration } from "@codingame/monaco-vscode-configuration-service-override";
import * as vscode from "vscode";
import { useSettingsStore } from "../stores/settings";

/** Hidden in the explorer and search while Hide binaries is on; images, .asset and .metadata stay. */
const BINARY_GLOBS = ["**/*.{dll,exe,pdb,hash,hex,bin,dat}", "**/.git"];

/** The mounted workspace's Hide binaries. */
export const hideBinaries = shallowRef(true);

/** The user settings as JSON; each glob is written on or off, as a user `{}` merges with the defaults. */
export function userSettings(): string {
  const settings = useSettingsStore();
  const excludes = Object.fromEntries(BINARY_GLOBS.map((glob) => [glob, hideBinaries.value]));
  return JSON.stringify({
    "workbench.colorTheme": settings.activeTheme,
    "editor.fontSize": settings.editorFontSize,
    "files.exclude": excludes,
    "search.exclude": excludes,
  });
}

/** Write the user settings now and on each change, and again if a picker swaps the theme. */
export function followSettings(): vscode.Disposable {
  const settings = useSettingsStore();
  const write = () => void updateUserConfiguration(userSettings());
  const stop = watch(() => [settings.activeTheme, settings.editorFontSize, hideBinaries.value], write, { immediate: true });
  const lock = vscode.workspace.onDidChangeConfiguration((e) => {
    if (!e.affectsConfiguration("workbench.colorTheme") && !e.affectsConfiguration("workbench.iconTheme")) return;
    const now = vscode.workspace.getConfiguration("workbench");
    if (now.get("colorTheme") !== settings.activeTheme || now.get("iconTheme") !== "pmt-icons") write();
  });
  return vscode.Disposable.from(lock, { dispose: stop });
}
