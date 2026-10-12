/**
 * The command palette's state: whether it is open and what its search starts with, and the commands the pages
 * register, which the palette lists in its Commands group.
 */
import { computed, shallowRef } from "vue";
import { defineStore } from "pinia";
import type { CommandPaletteItem } from "@nuxt/ui";

/** A palette row: Nuxt UI's, with the words it is found by, the scope a switch is in, its state and what it opens. */
export type CommandItem = CommandPaletteItem & { words?: string; scope?: string; on?: boolean; opens?: string };

export const useCommandsStore = defineStore("commands", () => {
  const paletteOpen = shallowRef(false);
  /** What the search starts with: "> " lists the commands, "" the files. */
  const paletteStart = shallowRef<"> " | "">("> ");
  // each page's commands, as a function of what it shows, by the name it registered under
  const sources = shallowRef(new Map<string, () => CommandItem[]>());

  /** The commands every source lists now. */
  const commands = computed(() => [...sources.value.values()].flatMap((items) => items()));

  /** Open the palette on its commands, or on its files with an empty prefix. */
  function openPalette(prefix: "> " | "" = "> "): void {
    paletteStart.value = prefix;
    paletteOpen.value = true;
  }

  /** Register a page's commands under a name; they are listed for as long as the page lives. */
  function register(name: string, items: () => CommandItem[]): void {
    sources.value = new Map(sources.value).set(name, items);
  }

  return { paletteOpen, paletteStart, commands, openPalette, register };
});
