/**
 * The settings store: every value through the settings query for the active workspace, written through one mutation,
 * and the theme put on `<html>` from the settings and the system's preference. This store keeps no copy of a value.
 * GAMES is the frontend's one table keyed by Go's game ids, holding each game's icon and theme family, since icons
 * and colors are the frontend's.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { useColorMode, usePreferredDark } from "@vueuse/core";
import { watchEffect } from "vue";

import ck3Icon from "@assets/Icon_CK3.png";
import eu5Icon from "@assets/Icon_EUV.png";
import vic3Icon from "@assets/Icon_Vic3.png";
import * as app from "@services/app/service";
import { settingsQuery, stateQuery } from "../queries";
import { useWorkspaceStore } from "./workspace";

/** The games as the frontend draws them, by Go's id: each game's icon and the theme family named after it. */
export const GAMES: readonly { id: string; icon: string; family: string }[] = [
  { id: "ck3", icon: ck3Icon, family: "ck3" },
  { id: "vic3", icon: vic3Icon, family: "vic3" },
  { id: "eu5", icon: eu5Icon, family: "eu5" },
];

/** The theme families: pmt, catppuccin, one, pierre, and one per GAMES row. */
export const THEME_FAMILIES: readonly { id: string; label: string }[] = [
  { id: "pmt", label: "PMT" },
  { id: "catppuccin", label: "Catppuccin" },
  { id: "one", label: "One" },
  { id: "pierre", label: "Pierre Soft" },
  { id: "ck3", label: "Crusader Kings" },
  { id: "eu5", label: "Europa Universalis" },
  { id: "vic3", label: "Victoria" },
];

/** A game's icon: the URL of its image. It is "" for a game the app has no icon for. */
export function gameIcon(id: string): string {
  return GAMES.find((row) => row.id === id)?.icon ?? "";
}

export const useSettingsStore = defineStore("settings", () => {
  const workspace = useWorkspaceStore();
  // Go answers each setting for the active workspace: the IDE's are kept per workspace, the rest for the app
  const { data: values } = useQuery(() => settingsQuery(workspace.activeId()));
  const { data: state } = useQuery(stateQuery);
  const { mutate } = useMutation({
    mutation: (change: { key: string; value: string }) =>
      app.WriteSetting(workspace.activeId(), change.key, change.value),
  });
  const systemDark = usePreferredDark();
  const colorMode = useColorMode();

  /** A setting's value, which is its default when it was never set. It is "" for a key the catalog lacks. */
  const value = (key: string): string => values.value?.[key] ?? "";

  /** Writes a setting. The config:changed event then refreshes the query. */
  const write = (key: string, value: string): void => mutate({ key, value });

  /** The theme in force: the family and whether dark, from the settings and the system. */
  const theme = (): { family: string; dark: boolean } => {
    const game =
      value("appearance.matchGame") === "true" ? GAMES.find((row) => row.id === state.value?.game?.id) : undefined;
    return {
      family: game?.family ?? (value("appearance.theme") || "pmt"),
      dark: value("appearance.followSystem") === "true" ? systemDark.value : value("appearance.dark") === "true",
    };
  };

  /** The editor's font, text size and line height, as every code view draws its text. */
  const codeFont = (): { fontFamily: string; fontSize: number; lineHeight: number } => ({
    fontFamily: `"${value("ide.font").replaceAll('"', "")}", "Geist Mono", monospace`,
    fontSize: Number(value("ide.editorTextSize")),
    lineHeight: value("ide.lineHeight") === "roomy" ? 1.6 : 1.35,
  });

  // the one place the theme and the scale reach the page; nothing is drawn over the page's own until they are read
  watchEffect(() => {
    if (!values.value) return;
    const { family, dark } = theme();
    const root = document.documentElement;
    root.dataset.theme = `${family}-${dark ? "dark" : "light"}`;
    colorMode.value = dark ? "dark" : "light";
    root.style.fontSize = `${(16 * (Number(value("appearance.scale")) || 100)) / 100}px`;
    root.style.setProperty("--ide-ui-font", `${value("ide.panelTextSize")}px`);
  });

  return { value, write, theme, codeFont };
});
