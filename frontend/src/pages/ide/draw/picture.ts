/**
 * The picture: what a holding and a property mean in CSS, in two tables with each row sourced to the wiki or
 * marked observed, and the DOM built from Go's widgets through them. The browser lays it out, and each widget's
 * rect is read back for the marks, the panel and the code link. Go measures nothing and the page computes no
 * layout.
 */
import type { Widget, Window } from "@services/draw/models";

/** How a holding lays its children out, as CSS. */
export const HOLDS: Readonly<Record<string, { css: Record<string, string>; source: string }>> = {};

/** What a property means, as CSS on the widget, with the source of the meaning. */
export const PROPERTIES: Readonly<
  Record<string, { apply: (value: string, style: CSSStyleDeclaration) => void; source: string }>
> = {};

/** One drawn widget: its element and its id. */
export type PictureNode = { id: number; element: HTMLElement; widget: Widget };

/** Builds the DOM of a window's widgets inside the floor element, leaving the hidden ones out. */
export function buildPicture(window: Window, hidden: ReadonlySet<number>, floor: HTMLElement): PictureNode[] {
  throw new Error("stub", { cause: [window, hidden, floor] });
}

/** Reads each drawn widget's rect relative to the floor, after layout. */
export function readRects(nodes: PictureNode[], floor: HTMLElement): Map<number, DOMRect> {
  throw new Error("stub", { cause: [nodes, floor] });
}
