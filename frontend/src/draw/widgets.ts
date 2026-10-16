/**
 * Reading what is drawn: a type's icon, the hidden set closed over children, and what a window's states show: each
 * group of conditions drawn at one value, a column's pages after the first and the side windows closed until opened.
 */
import type { Condition, Group, List as DrawList } from "@services/internal/draw";
import { Holding } from "@services/internal/game";
import type { DrawElement, DrawFile } from "../stores/draw";

/** The icon of each type: what it shows, or how it holds its children; any other type is a plain box. A coat's layers
 * by what each is. */
const TYPE_ICONS: Record<string, string> = {
  coat: "i-lucide-shield",
  sub: "i-lucide-shield-half",
  pattern: "i-lucide-grid-2x2",
  emblem: "i-lucide-sticker",
  instance: "i-lucide-copy",
  window: "i-lucide-app-window",
  textbox: "i-lucide-type",
  ubertextbox: "i-lucide-type",
  editbox: "i-lucide-text-cursor-input",
  icon: "i-lucide-image",
  button: "i-lucide-rectangle-horizontal",
  checkbutton: "i-lucide-square-check",
  scrollbar: "i-lucide-grip-vertical",
  progressbar: "i-lucide-loader",
  scrollarea: "i-lucide-scroll-text",
  uberscrollarea: "i-lucide-scroll-text",
  scrollvbox: "i-lucide-scroll-text",
  hbox: "i-lucide-columns-3",
  uberhbox: "i-lucide-columns-3",
  vbox: "i-lucide-rows-3",
  ubervbox: "i-lucide-rows-3",
  flowcontainer: "i-lucide-wrap-text",
  fixedgridbox: "i-lucide-grid-3x3",
  dynamicgridbox: "i-lucide-grid-3x3",
};

/** A group's choices besides its values: every value drawn at once, or none. */
export const EVERY_STATE = "*every";
export const NO_STATE = "*none";

/** A type's icon. */
export const typeIcon = (type: string): string => TYPE_ICONS[type] ?? "i-lucide-square-dashed";

/** The elements a set hides with everything under them; parents are listed before their children. */
export function withDescendants(elements: DrawElement[], hidden: Set<number>): Set<number> {
  const all = new Set<number>();
  elements.forEach((element, index) => {
    if (hidden.has(index) || all.has(element.parent)) all.add(index);
  });
  return all;
}

/** The value a group is drawn at: the one chosen, else none where it is closed, else its first. */
export const stateOf = (file: DrawFile, group: Group): string =>
  file.states.get(group.call) ?? (file.closedGroups.has(group.call) ? NO_STATE : (group.values?.[0] ?? NO_STATE));

/** What a group can be drawn at: each of its values, every one where it has several, or none. */
export const choicesOf = (group: Group) => [
  ...(group.values ?? []).map((value) => ({ label: value, value })),
  ...((group.values?.length ?? 0) > 1 ? [{ label: "Every one", value: EVERY_STATE }] : []),
  { label: "None", value: NO_STATE },
];

/** The groups a condition reads, each once, in the order it reads them. */
export const groupsOf = (condition: Condition): number[] =>
  condition.operator ? [...new Set((condition.operands ?? []).flatMap(groupsOf))] : [condition.group];

/** Whether a condition is met where each group is drawn at its state: a fact where its group is drawn at its value
 * or at every value; the game's And, Or, Nor and Not over their conditions. */
function isMet(condition: Condition, states: string[]): boolean {
  const operands = condition.operands ?? [];
  switch (condition.operator) {
    case "and":
      return operands.every((one) => isMet(one, states));
    case "or":
      return operands.some((one) => isMet(one, states));
    case "nor":
      return !operands.some((one) => isMet(one, states));
    case "not":
      return !isMet(operands[0], states);
  }
  return states[condition.group] === EVERY_STATE || states[condition.group] === condition.value;
}

/** The elements a file hides, each by itself: the session's, and those shown on a condition its states do not meet. */
export function hiddenOf(list: DrawList, file: DrawFile): Set<number> {
  const states = (list.groups ?? []).map((group) => stateOf(file, group));
  return file.hidden.union(
    new Set(
      (list.elements ?? []).flatMap((element, index) =>
        element.condition && !isMet(element.condition, states) ? [index] : [],
      ),
    ),
  );
}

/** The groups a column shows one page of at a time, closed but for the first until opened: its children shown on a
 * condition, two or more, each on a group of its own. */
export function pageGroups(list: DrawList): Set<string> {
  const elements = list.elements ?? [];
  const pages = Map.groupBy(
    elements.flatMap(({ condition, role, parent }) =>
      condition && !role && elements[parent]?.holds === Holding.HoldsColumn ? [{ condition, parent }] : [],
    ),
    (page) => page.parent,
  );
  const closed = new Set<string>();
  for (const children of pages.values()) {
    const groups = children.map(({ condition }) => groupsOf(condition));
    const isOwn = groups.every((one) => one.length === 1) && new Set(groups.flat()).size === groups.length;
    if (children.length < 2 || !isOwn) continue;
    for (const [group] of groups.slice(1)) closed.add(list.groups?.[group]?.call ?? "");
  }
  return closed;
}

/** The groups that show only side widgets, closed until opened, as the game opens a tool or a side window on a
 * click: every widget a fact of the group shows lies wholly outside the window's rect, none inside it; a widget under
 * one that clips is the window's, wherever it lies. */
export function sideGroups(list: DrawList, rects: Map<number, DOMRect[]>): Set<string> {
  const elements = list.elements ?? [];
  const window = rects.get(0)?.[0];
  const inside = new Set<number>();
  const outside = new Set<number>();
  elements.forEach((element, index) => {
    const rect = rects.get(index)?.[0];
    if (!element.condition || !rect || !window) return;
    const isClipped = (element.ancestors ?? []).some((ancestor) => elements[ancestor]?.clips);
    const isOutside =
      !isClipped &&
      (rect.left >= window.right ||
        rect.top >= window.bottom ||
        rect.right <= window.left ||
        rect.bottom <= window.top);
    for (const group of groupsOf(element.condition)) (isOutside ? outside : inside).add(group);
  });
  return new Set([...outside.difference(inside)].map((group) => list.groups?.[group]?.call ?? ""));
}
