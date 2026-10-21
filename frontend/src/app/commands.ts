/**
 * The command table. Every command of the app is one row here, in the menu item shape, with the area it belongs
 * to and when it applies. The palette, the context menus, the dropdown menus, the toolbar buttons and the
 * shortcuts all read these rows; a command exists nowhere else. Undo and redo are two rows over the model's own
 * history, and they stay out of the way of a focused editor.
 */
import type { CommandPaletteItem } from "@nuxt/ui";

/** One command: a menu item whose id names its area, such as `editor.save`, and whether it applies now. */
export type Command = CommandPaletteItem & {
  /** `<area>.<verb>`: editor.save, file.rename, graph.open, window.popOut … */
  id: string;
  /** Whether the command applies now. One that does not is left out of the palette and its shortcut is off. */
  when: () => boolean;
};

/** Every command of the app. */
export const COMMANDS: readonly Command[] = [];

/** Registers every command's keyboard shortcut. A command's `when` is checked at each press. */
export function registerShortcuts(): void {
  throw new Error("stub");
}

/** The palette's groups: the pages, the workspaces, the settings' switches and the commands that apply now. */
export function paletteGroups(): { id: string; label: string; items: Command[] }[] {
  throw new Error("stub");
}
