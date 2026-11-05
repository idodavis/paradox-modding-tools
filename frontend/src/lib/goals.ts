/** The goals a modder may want to make, which the Make a new mod page and the new mod dialog both offer. */
import type { LocationQueryRaw } from "vue-router";

import type { WorkspacePage } from "../app/pages";

/** A Goal is one thing a modder may want to make, with the mods it works on and the page it opens. */
export interface Goal {
  id: string;
  icon: string;
  label: string;
  blurb: string;
  hint: string;
  /** How many mods that already exist the goal works on. */
  existing: number;
  /** Whether the goal makes a new mod. The dialog offers only these, as it adds to a workspace that exists. */
  makes: boolean;
  opens: { name: WorkspacePage; query?: LocationQueryRaw };
}

/** GOALS lists what a modder may want to make, in the order they are shown. */
export const GOALS: readonly Goal[] = [
  {
    id: "change",
    icon: "i-lucide-pencil",
    label: "Change something in the game",
    blurb:
      "A trait's numbers, a building's cost, a rule. Your mod changes just that entry and leaves the rest to the game.",
    hint: "Find it in the game's files, then Override",
    existing: 0,
    makes: true,
    opens: { name: "workspace-ide" },
  },
  {
    id: "add",
    icon: "i-lucide-plus",
    label: "Add something new",
    blurb: "A trait, an event, a decision. They live in a mod of your own, apart from the game's files.",
    hint: "Name the mod, then New in the explorer",
    existing: 0,
    makes: true,
    opens: { name: "workspace-ide" },
  },
  {
    id: "translate",
    icon: "i-lucide-languages",
    label: "Translate a mod",
    blurb: "Write a mod's text in another language, as a small mod that loads after it.",
    hint: "Pick the mod and the language; Health lists the keys",
    existing: 1,
    makes: true,
    opens: { name: "health", query: { view: "mod" } },
  },
  {
    id: "together",
    icon: "i-lucide-layers",
    label: "Make two mods work together",
    blurb: "A small mod that loads after both and settles what they both change.",
    hint: "Pick the two mods; Health shows what they share",
    existing: 2,
    makes: true,
    opens: { name: "health", query: { view: "compatibility" } },
  },
  {
    id: "fix",
    icon: "i-lucide-activity",
    label: "Fix a mod I have",
    blurb: "See what the game's log and Health say about it, worst first, and work down the list.",
    hint: "Choose its folder; Health opens first",
    existing: 1,
    makes: false,
    opens: { name: "health" },
  },
  {
    id: "browse",
    icon: "i-lucide-book-open",
    label: "Look around the game's files",
    blurb: "A workspace of the game alone, to read how it does things. The files are read only, so nothing can break.",
    hint: "The IDE, with the game's files",
    existing: 0,
    makes: false,
    opens: { name: "workspace-ide" },
  },
];

/**
 * isPicked reports whether a goal has what it needs from the mods picked for it: each one picked, none picked twice,
 * and a language if the goal is a translation.
 */
export function isPicked(goal: Goal, picked: string[], language: string | undefined): boolean {
  return picked.every(Boolean) && new Set(picked).size === picked.length && (goal.id !== "translate" || !!language);
}
