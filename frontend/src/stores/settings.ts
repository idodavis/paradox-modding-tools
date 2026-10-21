/**
 * The settings store: every value through the settings query, written through one mutation, and the theme put on
 * `<html>` from the settings and the system's preference. The catalog is Go's; this store keeps no copy of a
 * value. GAMES is the frontend's one table keyed by Go's game ids, holding each game's icon and theme family,
 * since icons and colors are the frontend's.
 */
import { defineStore } from "pinia";

/** The games as the frontend draws them, by Go's id: each game's icon and the theme family named after it. */
export const GAMES: readonly { id: string; icon: string; family: string }[] = [];

/** A game's icon name, used wherever a game is shown. */
export function gameIcon(id: string): string {
  throw new Error("stub", { cause: id });
}

/** The theme families: pmt, catppuccin, one, pierre, and one per GAMES row. */
export const THEME_FAMILIES: readonly { id: string; label: string }[] = [];

export const useSettingsStore = defineStore("settings", () => {
  /** A setting's value, or the catalog's default when it was never set. */
  const value = (key: string): string => {
    throw new Error("stub", { cause: key });
  };
  /** Writes a setting. The config:changed event then refreshes the query. */
  const write = (key: string, value: string): void => {
    throw new Error("stub", { cause: [key, value] });
  };
  /** The theme in force: the family and whether dark, from the settings and the system. */
  const theme = (): { family: string; dark: boolean } => {
    throw new Error("stub");
  };
  return { value, write, theme };
});
