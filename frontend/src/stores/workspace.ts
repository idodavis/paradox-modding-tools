/**
 * The workspace store: the active workspace id, kept in local storage, and the facts of the open workspace that
 * every page reads: whether it is open, and each origin's name and color.
 */
import { defineStore } from "pinia";

export const useWorkspaceStore = defineStore("workspace", () => {
  /** The active workspace's id, or "" when there is none. The router's guard writes it. */
  const activeId = (): string => {
    throw new Error("stub");
  };
  /** Whether the active workspace is open in Go, from the state query. */
  const isOpen = (): boolean => {
    throw new Error("stub");
  };
  /** An origin's name: the game's, or a mod's, from the state. */
  const originName = (origin: string): string => {
    throw new Error("stub", { cause: origin });
  };
  /** An origin's color: a slot `o1`…`o16` or a CSS color, from the state. */
  const originColor = (origin: string): string => {
    throw new Error("stub", { cause: origin });
  };
  /** Makes a workspace active and asks Go to open it. */
  const open = (id: string): void => {
    throw new Error("stub", { cause: id });
  };
  return { activeId, isOpen, originName, originColor, open };
});
