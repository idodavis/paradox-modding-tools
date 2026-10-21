/**
 * The draw store: what the Visual view remembers for the session, one record per file: the drawn drawable, the
 * selected and hovered widgets, the widgets hidden for the session, the picks, the panel's tab and the reference
 * image. What Go answers (the window, the states, the coat) is read through queries and never stored here.
 */
import { defineStore } from "pinia";
/** What the Visual view remembers about one file for the session. It is never written to disk. */
export type DrawFile = {
  drawable: string | null;
  selected: number | null;
  hovered: number | null;
  hidden: number[];
  picks: Record<string, string>;
  tab: "widgets" | "states" | "drawables" | "layers" | "reference";
  reference: string | null;
};

export const useDrawStore = defineStore("draw", () => {
  const file = (path: string): DrawFile => {
    throw new Error("stub", { cause: path });
  };
  const select = (path: string, widget: number | null): void => {
    throw new Error("stub", { cause: [path, widget] });
  };
  const hover = (path: string, widget: number | null): void => {
    throw new Error("stub", { cause: [path, widget] });
  };
  const hide = (path: string, widget: number, hidden: boolean): void => {
    throw new Error("stub", { cause: [path, widget, hidden] });
  };
  const pick = (path: string, group: string, option: string): void => {
    throw new Error("stub", { cause: [path, group, option] });
  };
  const draw = (path: string, drawable: string | null): void => {
    throw new Error("stub", { cause: [path, drawable] });
  };
  return { file, select, hover, hide, pick, draw };
});
