/**
 * The settings store: every value through the settings query for the active workspace, written through one mutation,
 * and the theme put on `<html>` from the settings. This store keeps no copy of a value.
 * GAMES is the frontend's one table keyed by Go's game ids, holding each game's icon, since icons are the frontend's.
 */
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

/** GAMES holds the games as the frontend draws them, by Go's id: each game's icon. A game's theme family has its id. */
export const GAMES: readonly { id: string; icon: string }[] = [
  { id: "ck3", icon: ck3Icon },
  { id: "vic3", icon: vic3Icon },
  { id: "eu5", icon: eu5Icon },
];

/**
 * THEME_FAMILIES holds the theme families: pmt, catppuccin, one, pierre, and one per GAMES row. Each has the note the
 * Appearance page shows under its name.
 */
export const THEME_FAMILIES: readonly { id: string; label: string; note: string }[] = [
  { id: "pmt", label: "PMT", note: "PMT's own" },
  { id: "catppuccin", label: "Catppuccin", note: "Mocha and Latte" },
  { id: "one", label: "One", note: "One Dark and One Light" },
  { id: "pierre", label: "Pierre Soft", note: "Pierre Dark Soft and Pierre Light Soft" },
  { id: "ck3", label: "Crusader Kings", note: "CK3 workspaces" },
  { id: "eu5", label: "Europa Universalis", note: "EU5 workspaces" },
  { id: "vic3", label: "Victoria", note: "Vic3 workspaces" },
];

/**
 * SIMPLE_HIDES holds what the simple interface hides, each by its id and the label the start page names it by: the
 * settings `ide.minimap`, `ide.predict` and `ide.signature`, the sidebar views `git` and `entries`, the panel tabs
 * `output` and `terminal`, and `hover.details`, the hover card's facts beyond its title, blurb, problems and where its
 * name is defined.
 */
export const SIMPLE_HIDES: readonly { id: string; label: string }[] = [
  { id: "ide.minimap", label: "Minimap" },
  { id: "ide.predict", label: "Predicted text" },
  { id: "ide.signature", label: "Signature help" },
  { id: "git", label: "Source control" },
  { id: "entries", label: "Entries" },
  { id: "output", label: "Output" },
  { id: "terminal", label: "Terminal" },
  { id: "hover.details", label: "Hover card details" },
];

/** gameIcon returns the URL of a game's icon. It is "" for a game the app has no icon for. */
export function gameIcon(id: string): string {
  return GAMES.find((row) => row.id === id)?.icon ?? "";
}

export const useSettingsStore = defineStore("settings", () => {
  const workspace = useWorkspaceStore();
  // Go answers each setting for the active workspace: the IDE's are kept per workspace, the rest for the app. The last
  // workspace's values stand in while the next one's are read, so no setting reads as "" during a switch.
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

  /** value returns a setting's value, its default if never set. It is "" for a key the catalog lacks. */
  const value = (key: string): string => values.value?.[key] ?? "";

  /** isHidden reports whether the simple interface is on and hides the part with the given id. */
  const isHidden = (id: string): boolean =>
    value("appearance.simple") === "true" && SIMPLE_HIDES.some((row) => row.id === id);

  /** isOn reports whether a setting that is a switch is on. It is off for a setting the simple interface hides. */
  const isOn = (key: string): boolean => value(key) === "true" && !isHidden(key);

  /** isGuided reports whether Guidance for new modders is on: the parts of the app that only teach. */
  const isGuided = (): boolean => isOn("guidance.newModders");

  /** write writes a setting. The config:changed event then refreshes the query. */
  const write = (key: string, value: string): void => mutate({ key, value });

  /** writeItem puts an item in a setting that lists items with commas, or takes it out. A new item goes last, once. */
  const writeItem = (key: string, item: string, listed: boolean): void => {
    const items = value(key)
      .split(",")
      .filter((one) => one && one !== item);
    write(key, (listed ? [...items, item] : items).join(","));
  };

  /** theme returns the theme in force: the family and whether it is dark, from the settings. */
  const theme = (): { family: string; dark: boolean } => {
    const game = isOn("appearance.matchGame") ? GAMES.find((row) => row.id === state.value?.game?.id) : undefined;
    return { family: game?.id ?? (value("appearance.theme") || "pmt"), dark: isOn("appearance.dark") };
  };

  /**
   * codeFont returns how every code view sets its code, under Monaco's option keys: the font, text size and line
   * height, the padding above the first line in pixels, and how many digits the line numbers keep room for.
   */
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

  // the one place the theme and the scale reach the page; nothing is drawn over the page's own until they are read
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
