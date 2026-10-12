/**
 * The command palette's state: whether it is open, what its search starts with, and the IDE's commands, which the
 * palette lists in its Commands group.
 */
import { shallowRef } from "vue";
import { defineStore } from "pinia";
import type { CommandPaletteItem } from "@nuxt/ui";

/** A palette row: Nuxt UI's, with the words it is found by, the scope a switch is in, its state and what it opens. */
export type CommandItem = CommandPaletteItem & { words?: string; scope?: string; on?: boolean; opens?: string };

export const useCommandsStore = defineStore("commands", () => {
  const paletteOpen = shallowRef(false);
  /** What the search starts with: "> " lists the commands, "" the files. */
  const paletteStart = shallowRef<"> " | "">("> ");
  /** The IDE's commands as a function of what it shows, set once the editor has installed in this window. */
  const ideCommands = shallowRef<() => CommandItem[]>(() => []);

  /** Open the palette on its commands, or on its files with an empty prefix. */
  function openPalette(prefix: "> " | "" = "> "): void {
    paletteStart.value = prefix;
    paletteOpen.value = true;
  }

  return { paletteOpen, paletteStart, ideCommands, openPalette };
});
