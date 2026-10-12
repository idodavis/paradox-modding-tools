/**
 * The command palette's state: whether it is open and what its search starts with.
 */
import { shallowRef } from "vue";
import { defineStore } from "pinia";

export const useCommandsStore = defineStore("commands", () => {
  const paletteOpen = shallowRef(false);
  /** What the search starts with: "> " lists the commands, "" the files. */
  const paletteStart = shallowRef<"> " | "">("> ");

  /** Open the palette on its commands, or on its files with an empty prefix. */
  function openPalette(prefix: "> " | "" = "> "): void {
    paletteStart.value = prefix;
    paletteOpen.value = true;
  }

  return { paletteOpen, paletteStart, openPalette };
});
