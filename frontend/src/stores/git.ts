/**
 * The git store: the selected repository, which defaults to the active file's mod or else the first mod that is a
 * repository, and the output lines git printed. The repository's state itself comes from the git queries.
 */
import { defineStore } from "pinia";

export const useGitStore = defineStore("git", () => {
  /** The selected repository's folder, or "" when none is selected. */
  const repository = (): string => {
    throw new Error("stub");
  };
  const select = (dir: string): void => {
    throw new Error("stub", { cause: dir });
  };
  /** The lines git printed, newest last, from git:output. */
  const output = (): readonly string[] => {
    throw new Error("stub");
  };
  return { repository, select, output };
});
