/** The settings store: reads every setting for the active workspace, writes one, and puts the theme on `<html>`. */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { useColorMode } from "@vueuse/core";
import { watchEffect } from "vue";

import ck3Icon from "@assets/Icon_CK3.png";
import eu5Icon from "@assets/Icon_EUV.png";
import vic3Icon from "@assets/Icon_Vic3.png";
import * as app from "@services/app/service";
import { settingsQuery, stateQuery } from "../queries";
import { useWorkspaceStore } from "./workspace";

/** GAMES holds each game's icon, by Go's id. */
export const GAMES: readonly { id: string; icon: string }[] = [
  { id: "ck3", icon: ck3Icon },
  { id: "vic3", icon: vic3Icon },
  { id: "eu5", icon: eu5Icon },
];

/** THEME_FAMILIES holds the theme families. */
export const THEME_FAMILIES: readonly { id: string; label: string }[] = [
  { id: "pmt", label: "PMT" },
  { id: "catppuccin", label: "Catppuccin" },
  { id: "one", label: "One" },
  { id: "pierre", label: "Pierre Soft" },
  { id: "jetbrains", label: "JetBrains" },
  { id: "vscode", label: "VS Code Modern" },
  { id: "ck3", label: "Crusader Kings" },
  { id: "eu5", label: "Europa Universalis" },
  { id: "vic3", label: "Victoria" },
];

/** SIMPLE_HIDES holds what the simple interface hides, each by its id and the label the start page names it by. */
export const SIMPLE_HIDES: readonly { id: string; label: string }[] = [
  { id: "ide.minimap", label: "Minimap" },
  { id: "ide.predict", label: "Predicted text" },
  { id: "ide.signature", label: "Signature help" },
  { id: "git", label: "Source control" },
  { id: "entries", label: "Entries" },
  { id: "output", label: "Git output" },
  { id: "terminal", label: "Terminal" },
  { id: "hover.details", label: "Hover card details" },
];

/** gameIcon returns the URL of a game's icon, or "" if the app has none. */
export function gameIcon(id: string): string {
  return GAMES.find((row) => row.id === id)?.icon ?? "";
}

export const useSettingsStore = defineStore("settings", () => {
  const workspace = useWorkspaceStore();
  // The last workspace's values stand in while the next one's are read, so no setting reads as "" during a switch.
  const { data: values } = useQuery(() => ({
    ...settingsQuery(workspace.activeId()),
    placeholderData: (previous) => previous,
  }));
  const { data: state } = useQuery(stateQuery);
  const { mutate } = useMutation({
    mutation: (change: { key: string; value: string }) =>
      app.WriteSetting(workspace.activeId(), change.key, change.value),
  });
  const colorMode = useColorMode();

  /** value returns a setting's value, or "" for a key the catalog lacks. */
  const value = (key: string): string => values.value?.[key] ?? "";

  /** isHidden reports whether the simple interface is on and hides the part with the given id. */
  const isHidden = (id: string): boolean =>
    value("appearance.simple") === "true" && SIMPLE_HIDES.some((row) => row.id === id);

  /** isOn reports whether a switch is on. A setting the simple interface hides is off. */
  const isOn = (key: string): boolean => value(key) === "true" && !isHidden(key);

  /** isGuided reports whether Guidance for new modders is on. */
  const isGuided = (): boolean => isOn("guidance.newModders");

  /** write writes a setting. The config:changed event then refreshes the query. */
  const write = (key: string, text: string): void => mutate({ key, value: text });

  /** writeItem adds an item to a comma-separated setting, or removes it. */
  const writeItem = (key: string, item: string, listed: boolean): void => {
    const items = value(key)
      .split(",")
      .filter((other) => other && other !== item);
    write(key, (listed ? [...items, item] : items).join(","));
  };

  /** theme returns the theme in force: the family and whether it is dark. */
  const theme = (): { family: string; dark: boolean } => {
    const game = isOn("appearance.matchGame") ? GAMES.find((row) => row.id === state.value?.game?.id) : undefined;
    return { family: game?.id ?? (value("appearance.theme") || "pmt"), dark: isOn("appearance.dark") };
  };

  /** codeFont returns the Monaco options every code view uses. */
  const codeFont = (): {
    fontFamily: string;
    fontSize: number;
    lineHeight: number;
    padding: { top: number };
    lineNumbersMinChars: number;
  } => ({
    fontFamily: `"${value("ide.font").replaceAll('"', "")}", "Geist Mono", monospace`,
    fontSize: Number(value("ide.editorTextSize")),
    lineHeight: value("ide.lineHeight") === "roomy" ? 1.6 : 1.35,
    padding: { top: 6 },
    lineNumbersMinChars: 4,
  });

  /** toPixels returns a length given in rem as pixels at the app's scale. */
  const toPixels = (rem: number): number => (rem * 16 * (Number(value("appearance.scale")) || 100)) / 100;

  // Puts the theme, the text scale and the panel text size on the page once the settings are read.
  watchEffect(() => {
    if (!values.value) return;
    const { family, dark } = theme();
    const root = document.documentElement;
    root.dataset.theme = `${family}-${dark ? "dark" : "light"}`;
    colorMode.value = dark ? "dark" : "light";
    root.style.fontSize = `${toPixels(1)}px`;
    root.style.setProperty("--ide-ui-font", `${value("ide.panelTextSize")}px`);
  });

  return { value, isHidden, isOn, isGuided, write, writeItem, theme, codeFont, toPixels };
});
