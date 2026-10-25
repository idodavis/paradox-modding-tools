/**
 * The picture: what a holding and a property mean in CSS, in two tables with each row sourced to the wiki or
 * marked observed, and the DOM built from Go's widgets through them. The browser lays it out, and each widget's
 * rect is read back for the marks, the panel and the code link. Go measures nothing and the page computes no
 * layout.
 */
import { Markup, type Segment } from "@internal/loc/models";
import type { Choice, Font, Overlay, Property, Row, States, Widget, Window } from "@services/draw/models";

import { SAMPLE_COLORS, findPlaceholder } from "../../../lib/placeholders";
import { fileUrl } from "../../../queries";

type Apply = (value: string, style: CSSStyleDeclaration) => void;

/**
 * A Drawing is how a window is drawn: the widgets hidden, the values the states choose, and how many rows a list
 * draws that reads no database.
 */
type Drawing = { hidden: ReadonlySet<number>; choices: States["choices"]; rows: number };

/** A Scene is a window being built into the DOM, with the rows of each list that reads a database, by its item. */
type Scene = Drawing & { lists: Window["rows"]; children: Map<number, Drawn[]>; nodes: PictureNode[] };

/**
 * A Drawn is a widget as the picture draws it: its properties and its overlays' read out of its window's tables, with
 * the values the states choose in place of the ones written.
 */
type Drawn = Omit<Widget, "properties" | "overlays"> & {
  properties: Property[];
  overlays: (Omit<Overlay, "properties"> & { properties: Property[] })[];
};

/** A Sprite is one texture as its rows wrote it: its image, cut to one frame of a sheet, and how it lies in its box. */
type Sprite = { url: string; border: number[]; density: number; isTiled: boolean; fit: string };

/** A Mask is a sprite that keeps part of a layer, by its alpha or by its brightness. */
type Mask = [sprite: Sprite, mode: "alpha" | "luminance"];

const WIKI = "the wiki's Interface page";
const GAME_FILES = "the game's own GUI files";

/**
 * The built-in types that are as large as they write, none where they write nothing, whatever their children's size:
 * the plain widget, which EU5 builds as uberwidget. Every other type grows to its children where it writes no size.
 */
const WRITTEN_SIZES = ["widget", "uberwidget"];

/** The keys whose image file cuts a widget's picture to its shape, a coat of arms' or its own, which is no picture. */
const MASKS = ["coat_of_arms_mask", "mask"];

/** The prefixes of the custom properties a row writes for its widget's text or pictures, copied on without them. */
const TEXT = "--text-";
const PICTURE = "--picture-";

const SIDES = ["top", "right", "bottom", "left"];
const PLACES = ["start", "center", "end"] as const;

/** Each anchor term's place across and then down, as a fraction. */
const ANCHORS: Record<string, number>[] = [
  { left: 0, hcenter: 0.5, center: 0.5, right: 1 },
  { top: 0, vcenter: 0.5, center: 0.5, bottom: 1 },
];

/**
 * The wiki's five layout policies: whether each grows into free room, shrinks, or takes it before the others.
 * Expanding is preferred that takes free room first, so it shrinks as preferred does.
 */
const POLICIES: Record<string, { grow: string; shrink: string; expand: string }> = {
  fixed: { grow: "0", shrink: "0", expand: "0" },
  growing: { grow: "1", shrink: "0", expand: "0" },
  shrinking: { grow: "0", shrink: "1", expand: "0" },
  preferred: { grow: "1", shrink: "1", expand: "0" },
  expanding: { grow: "1", shrink: "1", expand: "1" },
};

/** The CSS of a grid's tracks on each axis: the flow that fills a set count of them, their count and their size. */
const GRID_AXES = {
  x: { flow: "row", template: "grid-template-columns", track: "grid-auto-columns" },
  y: { flow: "column", template: "grid-template-rows", track: "grid-auto-rows" },
};

/** The text cut with "…" where it is too long for its box. */
const ELIDED = { [`${TEXT}text-overflow`]: "ellipsis" };

/** How a texture lies in its widget by its fittype. Any other fittype fills the widget. */
const FITS: Record<string, { size: string; position: string }> = {
  centercrop: { size: "cover", position: "center" },
  start: { size: "contain", position: "left top" },
  center: { size: "contain", position: "center" },
  end: { size: "contain", position: "right bottom" },
};
const FILL = { size: "100% 100%", position: "0 0" };

/** The custom properties sent with an image's path, from which Go cuts one frame out of a sprite sheet. */
const CUTS = ["frame", "framesize", "framegrid", "density"];

/** The icon of a widget's type: what it shows, or how it holds its children. Any other type is a plain box. */
export const TYPE_ICONS: Record<string, string> = {
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

/** How a holding lays its children out, as CSS. */
export const HOLDS: Readonly<Record<string, { css: Record<string, string>; source: string }>> = {
  row: {
    css: { display: "flex", "flex-direction": "row", "align-items": "center", "justify-content": "space-evenly" },
    source: `${WIKI}: a box centers its children and spreads the room nobody takes evenly around them`,
  },
  column: {
    css: { display: "flex", "flex-direction": "column", "align-items": "center", "justify-content": "space-evenly" },
    source: `${WIKI}: a box centers its children and spreads the room nobody takes evenly around them`,
  },
  flow: { css: { display: "flex", "align-items": "start" }, source: `${WIKI}: a flow resizes to fit its children` },
  grid: {
    css: { display: "grid", "justify-content": "start", "align-content": "start" },
    source: WIKI,
  },
  scroll: {
    css: { display: "grid", overflow: "hidden", contain: "size", "justify-items": "start", "align-items": "start" },
    source: WIKI,
  },
  cell: {
    css: { display: "grid", "grid-template-columns": "100%", "grid-template-rows": "100%" },
    source:
      "observed: a child anchored to a cell's edge sits at that edge even when larger than the cell, so the one " +
      "track is the cell itself; a cell with no size still grows to its children",
  },
};

/** What a property means, as CSS on the widget, with the source of the meaning. */
export const PROPERTIES: Readonly<
  Record<string, { apply: (value: string, style: CSSStyleDeclaration) => void; source: string }>
> = {
  size: makeWikiRow(makeLength("width", "height")),
  minimumsize: makeWikiRow(makeLength("min-width", "min-height")),
  maximumsize: makeWikiRow(makeLength("max-width", "max-height")),
  min_width: makeWikiRow(makeLength("min-width")),
  min_height: makeWikiRow(makeLength("min-height")),
  max_width: makeWikiRow(makeLength("max-width")),
  max_height: makeWikiRow(makeLength("max-height")),
  margin: makeWikiRow(makeSides("--margin")),
  margin_left: makeWikiRow(makeSide("--margin", "left")),
  margin_right: makeWikiRow(makeSide("--margin", "right")),
  margin_top: makeWikiRow(makeSide("--margin", "top")),
  margin_bottom: makeWikiRow(makeSide("--margin", "bottom")),
  spacing: makeWikiRow(makeLength("gap")),
  alpha: makeWikiRow((value, style) => {
    if (Number.isFinite(Number(value))) style.setProperty("opacity", value);
  }),
  parentanchor: makeWikiRow(makeAnchor("--parent")),
  widgetanchor: makeWikiRow(makeAnchor("--own")),
  position: makeWikiRow(makeOffset(["x", "y"])),
  position_x: makeWikiRow(makeOffset(["x"])),
  position_y: makeWikiRow(makeOffset(["y"])),
  layoutpolicy_horizontal: makeWikiRow(makePolicy("x")),
  layoutpolicy_vertical: makeWikiRow(makePolicy("y")),
  layoutstretchfactor_horizontal: makeWikiRow(makeStretch("x")),
  layoutstretchfactor_vertical: makeWikiRow(makeStretch("y")),
  ignore_layout: makeWikiRow(makeChoice({ yes: { position: "absolute", "--ignored": "1" } })),
  set_parent_size_to_minimum: makeWikiRow(makeChoice({ yes: { "--fit-x": "1", "--fit-y": "1" } })),
  set_parent_dimension_to_minimum: makeWikiRow(makeChoice({ width: { "--fit-x": "1" }, height: { "--fit-y": "1" } })),
  resizeparent: makeWikiRow(makeChoice({ yes: { "--fit-x": "1", "--fit-y": "1" } })),
  righttoleft: makeWikiRow(makeChoice({ yes: { "flex-direction": "row-reverse" } })),
  bottomtotop: makeWikiRow(makeChoice({ yes: { "flex-direction": "column-reverse" } })),
  direction: makeWikiRow(makeChoice({ vertical: { "flex-direction": "column" } })),
  // a column written as a share is a share of the grid's holder, so the grid spans its holder: EU5's building_view
  addcolumn: makeWikiRow((value, style) => {
    makeLength("grid-auto-columns")(value, style);
    if (value.trim().endsWith("%") && !style.getPropertyValue("width")) style.setProperty("width", "100%");
  }),
  addrow: makeWikiRow(makeLength("grid-auto-rows")),
  datamodel_wrap: makeWikiRow(makeVariable("--wrap")),
  maxhorizontalslots: makeWikiRow(makeVariable("--slots-x")),
  maxverticalslots: makeWikiRow(makeVariable("--slots-y")),
  flipdirection: {
    apply: makeChoice({ yes: { "--flip": "1" } }),
    source:
      "observed: a grid fills down a column, and across a row when flipped, as CK3's character window draws its traits",
  },
  setitemsizefromcell: makeWikiRow(makeChoice({ yes: { "--cell-items": "1" } })),
  autoresizescrollarea: makeWikiRow(makeChoice({ yes: { contain: "none" } })),
  align: makeWikiRow(makeAnchor("--align")),
  autoresize: makeWikiRow(makeChoice({ yes: { width: "max-content", height: "max-content" } })),
  multiline: makeWikiRow(makeChoice({ yes: { [`${TEXT}white-space`]: "pre-wrap" } })),
  elide: makeWikiRow(makeChoice({ left: ELIDED, right: ELIDED })),
  font: {
    apply: (value, style) => style.setProperty(`${TEXT}font-family`, `"${value}", sans-serif`),
    source: GAME_FILES,
  },
  fontsize: { apply: makeLength(`${TEXT}font-size`), source: GAME_FILES },
  fontsize_min: {
    apply: makeLength("--least-font-size"),
    source: "observed: the game shows whole a label that fits only below its written size, such as Vic3's Unlocked",
  },
  fontcolor: { apply: makeColor(`${TEXT}color`), source: GAME_FILES },
  gfxtype: {
    apply: makeChoice({ linegfx: { "--line": "1" } }),
    source: "observed: a line draws its texture along its points, which only the running game has",
  },
  fittype: makeWikiRow(makeVariable("--fit")),
  blend_mode: makeWikiRow(
    makeChoice({
      normal: { [`${PICTURE}mix-blend-mode`]: "normal" },
      add: { [`${PICTURE}mix-blend-mode`]: "plus-lighter" },
      multiply: { [`${PICTURE}mix-blend-mode`]: "multiply" },
      overlay: { [`${PICTURE}mix-blend-mode`]: "overlay" },
      colordodge: { [`${PICTURE}mix-blend-mode`]: "color-dodge" },
      darken: { [`${PICTURE}mix-blend-mode`]: "darken" },
      lighten: { [`${PICTURE}mix-blend-mode`]: "lighten" },
      // these two draw nothing of their own: alphamultiply keeps the widget's texture where it is opaque, and mask
      // keeps each overlay after it where it is bright
      alphamultiply: { "--mask": "alpha" },
      mask: { "--mask": "luminance" },
    }),
  ),
  mirror: makeWikiRow(
    makeChoice({
      horizontal: { [`${PICTURE}transform`]: "scaleX(-1)" },
      vertical: { [`${PICTURE}transform`]: "scaleY(-1)" },
      both: { [`${PICTURE}transform`]: "scale(-1, -1)" },
    }),
  ),
  spritetype: makeWikiRow(makeChoice({ corneredtiled: { "--tiled": "1" } })),
  color: makeWikiRow(makeColor("--tint", "--tint-alpha")),
  tintcolor: makeWikiRow(makeColor("--tint", "--tint-alpha")),
  texture_density: makeWikiRow(makeVariable("--density")),
  framesize: makeWikiRow(makeVariable("--framesize")),
  frame_grid: makeWikiRow(makeVariable("--framegrid")),
  upframe: makeWikiRow(makeVariable("--frame")),
  frame: makeWikiRow(makeVariable("--frame")),
  spriteborder: makeWikiRow(makeSides("--border")),
  spriteborder_left: makeWikiRow(makeSide("--border", "left")),
  spriteborder_right: makeWikiRow(makeSide("--border", "right")),
  spriteborder_top: makeWikiRow(makeSide("--border", "top")),
  spriteborder_bottom: makeWikiRow(makeSide("--border", "bottom")),
};

/** One drawn widget: its element and its id. */
export type PictureNode = { id: number; element: HTMLElement };

/** The font faces the page has, by the font's name and style, with the file each is read from. */
const faces = new Map<string, { path: string; face: FontFace }>();

/**
 * Builds the DOM of a window's widgets inside the floor element as the drawing says, leaving the hidden ones out. The
 * window is laid out in its holders, each a box of the holder's own properties, the outermost on the floor.
 */
export function buildPicture(window: Window, drawing: Drawing, floor: HTMLElement): PictureNode[] {
  const widgets = (window.widgets ?? []).map((widget) =>
    applyChoices(toDrawn(window, widget), drawing.choices?.[`${widget.id}`] ?? []),
  );
  loadFonts(window.fonts ?? []);
  const children = Map.groupBy(
    widgets.filter((widget) => widget.id !== 0),
    (widget) => widget.parent,
  );
  const scene: Scene = { ...drawing, lists: window.rows, children, nodes: [] };
  floor.replaceChildren();
  floor.dataset.holds = "cell";
  applyCss(floor.style, {
    ...HOLDS.cell.css,
    [`${TEXT}font-family`]: "",
    [`${TEXT}font-size`]: "",
    "font-family": "",
    "font-size": "14px",
  });
  // a text with no font or size of its own is drawn in those of the game's own type of a text, through their rows
  if (window.textFont) PROPERTIES.font.apply(window.textFont, floor.style);
  if (window.textSize) PROPERTIES.fontsize.apply(window.textSize, floor.style);
  copyVariables(floor.style, TEXT, floor.style);
  let holding = floor;
  for (const holder of window.holders ?? []) {
    const box = document.createElement("div");
    box.dataset.holds = "cell";
    applyStyle(box, toDrawn(window, holder), null);
    holding.append(box);
    applyLayout(holding, [box]);
    holding = box;
  }
  const root = widgets[0] ? buildWidget(scene, widgets[0], null, null) : null;
  if (root) {
    holding.append(root);
    applyLayout(holding, [root]);
  }
  return scene.nodes;
}

/** Reads the rects of each drawn widget relative to the floor, after layout: one for each copy of an item row. */
export function readRects(nodes: PictureNode[], floor: HTMLElement): Map<number, DOMRect[]> {
  const origin = floor.getBoundingClientRect();
  const scale = floor.offsetWidth ? origin.width / floor.offsetWidth : 1;
  const rects = new Map<number, DOMRect[]>();
  for (const { id, element } of nodes) {
    const box = element.getBoundingClientRect();
    const rect = new DOMRect(
      (box.left - origin.left) / scale,
      (box.top - origin.top) / scale,
      box.width / scale,
      box.height / scale,
    );
    rects.set(id, [...(rects.get(id) ?? []), rect]);
  }
  return rects;
}

/**
 * Moves the drawn window to the screen's edge it lies past, where all of it lies off the floor, so that it can be seen.
 * The game places such a window by code, next to what opened it. A window partly on the floor stays where its
 * properties put it, cut by the floor's edge. It returns whether the window was moved.
 */
export function placeOnScreen(nodes: PictureNode[], floor: HTMLElement): boolean {
  const root = nodes[0];
  if (!root) return false;
  root.element.style.removeProperty("transform");
  const [box] = readRects([root], floor).get(root.id) ?? [];
  if (!box) return false;
  const [width, height] = [floor.offsetWidth, floor.offsetHeight];
  const across = box.left >= width ? width - box.right : box.right <= 0 ? -box.left : 0;
  const down = box.top >= height ? height - box.bottom : box.bottom <= 0 ? -box.top : 0;
  if (!across && !down) return false;
  root.element.style.setProperty("transform", `translate(${across}px, ${down}px)`);
  return true;
}

/**
 * Fits each text that its box cuts. Its samples keep the words that fit at the written size, a word of each sample at
 * least, so a box that holds one line gets a short sample. A text still cut then shrinks where its widget writes a
 * least font size, as the game shrinks it, as far as it needs and never below that size. Each text first gets back its
 * written size and its whole samples, so the picture can be fitted again once its fonts and images load.
 */
export function fitTexts(nodes: PictureNode[]): void {
  const spans = nodes.flatMap(({ element }) => element.querySelector<HTMLElement>(":scope > span") ?? []);
  for (const span of spans) restoreText(span);
  trimSamples(spans.filter((span) => listSamples(span).length && isCut(span)));
  shrinkTexts(spans.filter(isCut));
}

/**
 * Fits each placeholder into the part of its widget's box that lies inside the widget's holder, the box the game
 * draws the picture in, so its whole shape shows where the holder is. A widget wholly outside its holder draws none.
 */
export function fitPlaceholders(nodes: PictureNode[]): void {
  const fits = nodes.flatMap(({ element }) => {
    const placeholder = element.querySelector<HTMLElement>(":scope > [data-placeholder]");
    if (!placeholder || !element.parentElement || !element.offsetWidth) return [];
    const [own, holder] = [element.getBoundingClientRect(), element.parentElement.getBoundingClientRect()];
    const scale = own.width / element.offsetWidth;
    const sides = [holder.top - own.top, own.right - holder.right, own.bottom - holder.bottom, holder.left - own.left];
    return [{ placeholder, inset: sides.map((side) => `${Math.max(0, side / scale)}px`).join(" ") }];
  });
  for (const { placeholder, inset } of fits) placeholder.style.setProperty("inset", inset);
}

/** Lists the properties at the given positions among a window's properties: those of one of its widgets or overlays. */
export function listProperties(window: Window, positions: number[] | null): Property[] {
  return (positions ?? []).map((position) => (window.properties ?? [])[position]);
}

/** Applies the properties a widget writes to a style, each through its row, in the order of the table. */
export function applyProperties(properties: Property[] | null, style: CSSStyleDeclaration): void {
  const values = new Map((properties ?? []).map((property) => [property.key.toLowerCase(), property.value]));
  for (const [key, row] of Object.entries(PROPERTIES)) {
    const value = values.get(key);
    if (value !== undefined) row.apply(value, style);
  }
}

/**
 * Gives the page each style of the given fonts as a font face named by its font, read from its file. A face of the same
 * font and style read from another file, another game's, is replaced.
 */
function loadFonts(fonts: Font[]): void {
  for (const { name, style, path } of fonts) {
    const key = `${name}|${style}`;
    const current = faces.get(key);
    if (current?.path === path) continue;
    if (current) document.fonts.delete(current.face);
    const styles = style.toLowerCase().split("|");
    const face = new FontFace(name, `url("${fileUrl(path)}")`, {
      weight: styles.includes("bold") ? "700" : "400",
      style: styles.includes("italic") ? "italic" : "normal",
    });
    faces.set(key, { path, face });
    document.fonts.add(face);
    void face.load();
  }
}

/**
 * Builds the element of a widget with its pictures, text and children, or null where a hidden one takes no room. In a
 * row of a list that reads a database, the widget takes the values the row's definition gives it.
 */
function buildWidget(scene: Scene, widget: Drawn, holder: Drawn | null, row: Row | null): HTMLElement | null {
  widget = applyChoices(widget, row?.choices?.[`${widget.id}`] ?? []);
  const isHidden = scene.hidden.has(widget.id);
  // a hidden window takes no room, and a hidden child takes none where its holder drops hidden children
  if (isHidden && (!holder || holder.row.dropsHidden)) return null;
  const element = document.createElement("div");
  element.dataset.element = String(widget.id);
  element.dataset.holds = widget.row.holding || "cell";
  scene.nodes.push({ id: widget.id, element });
  applyStyle(element, widget, holder);
  if (isHidden) element.style.setProperty("visibility", "hidden");
  const kids = scene.children.get(widget.id) ?? [];
  const build = (role: string) =>
    kids
      .filter((kid) => kid.role === role && !(role === "chrome" && isBarOff(widget, kid)))
      .flatMap((kid) => listRows(scene, kid, row).map((kidRow) => buildWidget(scene, kid, widget, kidRow)))
      .filter((child) => child !== null);
  const skins = build("skin");
  const others = [...build(""), ...build("label"), ...build("chrome")];
  element.append(...skins, ...buildPictures(widget, element), ...others);
  applyLayout(element, [...skins, ...others]);
  return element;
}

/**
 * Lists the rows a child of a widget in the given row is drawn in: an item of a list that reads a database once for
 * each of its definitions, any other item the sample number of times in that row, and any other child once in it.
 */
function listRows(scene: Scene, kid: Drawn, row: Row | null): (Row | null)[] {
  if (!kid.item) return [row];
  return scene.lists?.[`${kid.id}`] ?? Array.from({ length: scene.rows }, () => row);
}

/** Reads a widget's properties and its overlays' out of its window's tables, as the picture draws them. */
function toDrawn(window: Window, widget: Widget): Drawn {
  const overlays = (widget.overlays ?? []).map((position) => (window.overlays ?? [])[position]);
  return {
    ...widget,
    properties: listProperties(window, widget.properties),
    overlays: overlays.map((overlay) => ({ ...overlay, properties: listProperties(window, overlay.properties) })),
  };
}

/**
 * Gives a widget the values the states choose for its properties, each in place of the choice its property writes,
 * with the texts, image files and colors they lead to. A text chosen takes the place of the text with its samples, a
 * color chosen takes the place of the first definition's, and an image file chosen for its picture takes the place of
 * its placeholder.
 */
function applyChoices(widget: Drawn, choices: Choice[]): Drawn {
  if (!choices.length) return widget;
  const values = new Map(choices.map((choice) => [choice.key, choice.value]));
  const toRecord = <Field extends "text" | "texture" | "color">(field: Field) =>
    Object.fromEntries(choices.flatMap((choice) => (choice[field] ? [[choice.key, choice[field]] as const] : [])));
  const textures = toRecord("texture");
  return {
    ...widget,
    properties: widget.properties.map((property) => ({
      ...property,
      value: values.get(property.key) ?? property.value,
    })),
    texts: { ...widget.texts, ...toRecord("text") },
    textures: { ...widget.textures, ...textures },
    colors: { ...widget.colors, ...toRecord("color") },
    placeholder: Object.keys(textures).some((key) => !MASKS.includes(key)) ? "" : widget.placeholder,
  };
}

/** Applies a widget's CSS to its element: its holding, each property through its row, and what its role makes of it. */
function applyStyle(element: HTMLElement, widget: Drawn, holder: Drawn | null): void {
  const { style } = element;
  applyCss(style, { position: "relative", "box-sizing": "border-box", "min-width": "0", "min-height": "0" });
  applyCss(style, HOLDS[widget.row.holding || "cell"].css);
  // custom properties are inherited, so a widget with no margin of its own starts from none
  for (const side of SIDES) style.setProperty(`--margin-${side}`, "0px");
  applyProperties(widget.properties, style);
  // a color Go found for a binding goes through the binding's own row, where the binding itself set nothing
  for (const [key, color] of Object.entries(widget.colors ?? {})) {
    if (color) PROPERTIES[key.toLowerCase()]?.apply(color, style);
  }
  const margins = SIDES.map((side) => `var(--margin-${side}, 0px)`).join(" ");
  element.dataset.free = String(["skin", "chrome"].includes(widget.role) || !!widget.row.fills);
  // a scroll area's content is sized by the area to what it holds, whatever its type; a size written as a binding is
  // the running game's, which the page cannot know, so that widget grows to its children
  const isScrolled = holder?.row.holding === "scroll";
  const isBound = findValue(widget, "size").includes("[");
  if (WRITTEN_SIZES.includes(widget.builtin) && element.dataset.free !== "true" && !isScrolled && !isBound) {
    const unsized = ["width", "height"].filter((length) => !style.getPropertyValue(length));
    for (const length of unsized) style.setProperty(length, "0px");
    element.dataset.unsized = unsized.join(" ");
  }
  if (widget.role === "skin") {
    applyCss(style, { position: "absolute", inset: margins, padding: "0", width: "auto", height: "auto" });
  } else {
    style.setProperty("padding", margins);
  }
  if (widget.role === "chrome") {
    const edges = {
      horizontal: { left: "0", right: "0", bottom: "0", width: "auto" },
      vertical: { top: "0", bottom: "0", right: "0", height: "auto" },
    };
    applyCss(style, { position: "absolute", translate: "0 0", ...edges[findAxis(widget)] });
  }
  if (widget.row.clips) style.setProperty("overflow", "hidden");
  if (widget.row.fills) applyCss(style, { position: "absolute", inset: "0", width: "auto", height: "auto" });
  // a scroll area's content fills it at least; its skins and scroll bars keep their own boxes
  if (isScrolled && element.dataset.free !== "true") {
    applyCss(style, { "min-width": "100%", "min-height": "100%" });
  }
}

/**
 * Applies the layout of children in their holder, by what their properties wrote: anchors, policies and fits. A child
 * that sizes its holder to its minimum gives it the size of what it holds, which its own maximum does not cut: EU5's
 * basic_dialog caps its box at 300 high, and the agenda inside it stands 885 high and whole in the game.
 */
function applyLayout(holder: HTMLElement, children: HTMLElement[]): void {
  const holding = holder.dataset.holds;
  for (const child of children) {
    if (findVariable(child, "--fit-x")) {
      holder.style.setProperty("width", "fit-content");
      child.style.removeProperty("max-width");
    }
    if (findVariable(child, "--fit-y")) {
      holder.style.setProperty("height", "fit-content");
      child.style.removeProperty("max-height");
    }
    if (holding === "flow") child.style.setProperty("flex-shrink", "0");
    if (holding === "cell") applyCell(child);
    // a grid that sizes its items from its cells gives a plain item the cell where the item writes no size
    if (holding === "grid" && findVariable(holder, "--cell-items")) {
      const unsized = child.dataset.unsized?.split(" ").filter(Boolean) ?? [];
      for (const length of unsized) child.style.setProperty(length, "100%");
    }
  }
  if (holding === "row" || holding === "column") applyBox(children, holding === "row" ? "x" : "y");
  if (holding === "grid") applyGrid(holder);
}

/**
 * Applies how a grid fills its cells. It fills down a column and goes on in the next, or across a row when it is
 * flipped. A line holds the grid's wrap of cells, or its most slots along the fill where those are fewer. A grid that
 * writes neither, but writes its most slots the other way, fills that many lines. Any other grid fills one line.
 */
function applyGrid(grid: HTMLElement): void {
  const [along, across] = findVariable(grid, "--flip") === "1" ? (["x", "y"] as const) : (["y", "x"] as const);
  const counts = [findNumber(grid, "--wrap", 0), findNumber(grid, `--slots-${along}`, 0)].filter((count) => count > 0);
  // a line of a set length is that many tracks on its own axis, and a set count of lines that many on the other;
  // one line is the other axis with no count written, which CSS fills as one track
  const [axis, count] = counts.length
    ? [along, Math.min(...counts)]
    : [across, findNumber(grid, `--slots-${across}`, 0)];
  const { style } = grid;
  const { flow, template, track } = GRID_AXES[axis];
  style.setProperty("grid-auto-flow", flow);
  if (count > 0) style.setProperty(template, `repeat(${count}, ${style.getPropertyValue(track) || "max-content"})`);
}

/** Applies the place of a child of a cell: a grid item at its holder's anchor, shifted to its own, or over the cell. */
function applyCell(child: HTMLElement): void {
  const { style } = child;
  if (child.dataset.free !== "true") {
    const [parentX, parentY] = [findNumber(child, "--parent-x", 0), findNumber(child, "--parent-y", 0)];
    const [ownX, ownY] = [findNumber(child, "--own-x", parentX), findNumber(child, "--own-y", parentY)];
    const [offsetX, offsetY] = [findVariable(child, "--offset-x") || "0px", findVariable(child, "--offset-y") || "0px"];
    applyCss(
      style,
      findVariable(child, "--ignored")
        ? {
            position: "absolute",
            left: `calc(${parentX * 100}% + ${offsetX})`,
            top: `calc(${parentY * 100}% + ${offsetY})`,
            translate: `${-ownX * 100}% ${-ownY * 100}%`,
          }
        : {
            "grid-area": "1 / 1",
            "justify-self": toPlace(parentX),
            "align-self": toPlace(parentY),
            translate: `calc(${(parentX - ownX) * 100}% + ${offsetX}) calc(${(parentY - ownY) * 100}% + ${offsetY})`,
          },
    );
  }
  // a widget that grows fills the cell on each axis where it writes no length, so a window keeps its written size
  for (const [axis, length] of [
    ["x", "width"],
    ["y", "height"],
  ]) {
    const isWritten = /\d/.test(style.getPropertyValue(length)) && !child.dataset.unsized?.includes(length);
    if (findVariable(child, `--grow-${axis}`) === "1" && !isWritten) style.setProperty(length, "100%");
  }
  const isBox = child.dataset.holds === "row" || child.dataset.holds === "column";
  if (isBox && !findVariable(child, "--parent-x") && !findVariable(child, "--parent-y")) {
    applyCss(style, { width: "100%", height: "100%" });
  }
}

/** Applies the place of children of a box: along its axis by their policies and across it by their anchors. */
function applyBox(children: HTMLElement[], along: "x" | "y"): void {
  const across = along === "x" ? "y" : "x";
  const isExpanding = (child: HTMLElement) => findVariable(child, `--expand-${along}`) === "1";
  const isShared = children.some(isExpanding);
  for (const child of children) {
    const { style } = child;
    const isGrowing = findVariable(child, `--grow-${along}`) === "1";
    const grows = isExpanding(child) || (isGrowing && !isShared);
    style.setProperty("flex-grow", String(findNumber(child, `--stretch-${along}`, 0) || (grows ? 1 : 0)));
    style.setProperty("flex-shrink", findVariable(child, `--shrink-${along}`) === "1" ? "1" : "0");
    // a widget that grows across, or a plain widget that writes no breadth, takes the box's breadth
    const breadth = across === "x" ? "width" : "height";
    if (findVariable(child, `--grow-${across}`) === "1" || child.dataset.unsized?.includes(breadth)) {
      applyCss(style, { "align-self": "stretch", [breadth]: "auto" });
    } else if (child.dataset.free !== "true" && findVariable(child, `--parent-${across}`)) {
      style.setProperty("align-self", toPlace(findNumber(child, `--parent-${across}`, 0)));
    }
  }
}

/**
 * Builds a widget's textures, the placeholder Go names for a picture of it that has no file, over them, and its text.
 * A portrait's silhouette sits over its transparent texture this way. A mask, a coat of arms' or the widget's own, is
 * no texture of its own: it cuts the placeholder to its shape. A line draws no picture.
 */
function buildPictures(widget: Drawn, element: HTMLElement): HTMLElement[] {
  const isLine = findVariable(element, "--line") !== "";
  const textures = Object.entries(widget.textures ?? {});
  const mask = textures.find(([key]) => MASKS.includes(key))?.[1] ?? "";
  const [path] = isLine ? [] : textures.flatMap(([key, file]) => (MASKS.includes(key) ? [] : [file]));
  const pictures: HTMLElement[] = [];
  if (path) {
    const own = makeSprite(path, element.style);
    pictures.push(buildTextures(own, widget.overlays, element.style));
    // the wiki: a widget with a texture and no size is the texture's size
    if (!findVariable(element, "width") && !findVariable(element, "height")) pictures.push(buildSizer(own));
  }
  if (widget.placeholder && !isLine) pictures.push(buildPlaceholder(widget.placeholder, mask));
  const text = buildText(widget, element);
  return text ? [...pictures, text] : pictures;
}

/** Makes the sprite of an image file from the custom properties that rows wrote to a style. */
function makeSprite(path: string, style: CSSStyleDeclaration): Sprite {
  const query = new URLSearchParams();
  for (const key of CUTS) {
    const value = style.getPropertyValue(`--${key}`);
    if (value) query.set(key, value);
  }
  return {
    url: `${fileUrl(path)}&${query}`,
    border: SIDES.map((side) => parseFloat(style.getPropertyValue(`--border-${side}`)) || 0),
    density: Number(style.getPropertyValue("--density")) || 1,
    isTiled: style.getPropertyValue("--tiled") === "1",
    fit: style.getPropertyValue("--fit").toLowerCase(),
  };
}

/**
 * Builds a widget's textures as one group over the widget: its own texture, then each overlay in the order the game
 * lays them on. The group draws in the own texture's blend, kept where every alphamultiply overlay is opaque. Each
 * other overlay blends with the own texture alone, kept to its shape and to where the last mask overlay before it is
 * bright. An overlay written visible = no is left out.
 */
function buildTextures(own: Sprite, overlays: Drawn["overlays"], style: CSSStyleDeclaration): HTMLElement {
  const group = document.createElement("div");
  const alphas: Mask[] = [];
  let shade: Mask | undefined;
  const layers = [buildLayer(own, style, 1, [])];
  for (const overlay of overlays) {
    if (!overlay.path || findValue(overlay, "visible").toLowerCase() === "no") continue;
    const scratch = document.createElement("div").style;
    // an overlay that writes no blend adds, the shader's first mode: measured on EU5's building_view, its minus and
    // plus icons and its production method header, against the game
    if (!findValue(overlay, "blend_mode")) PROPERTIES.blend_mode.apply("add", scratch);
    applyProperties(overlay.properties, scratch);
    const sprite = makeSprite(overlay.path, scratch);
    const mask = scratch.getPropertyValue("--mask");
    if (mask === "alpha") alphas.push([sprite, "alpha"]);
    else if (mask === "luminance") shade = [sprite, "luminance"];
    else {
      const masks: Mask[] = shade ? [[own, "alpha"], shade] : [[own, "alpha"]];
      layers.push(buildLayer(sprite, scratch, Number(scratch.opacity || 1), masks));
    }
  }
  applyCss(group.style, {
    position: "absolute",
    inset: "0",
    isolation: "isolate",
    "mix-blend-mode": style.getPropertyValue(`${PICTURE}mix-blend-mode`) || "normal",
    ...toMask(alphas),
  });
  group.append(...layers);
  return group;
}

/**
 * Builds one texture as a layer over its widget, at the given alpha and kept to the given masks. A tint is multiplied
 * over the whole image and the layer is then kept to the image's shape, as the game's shader multiplies each pixel by
 * the tint's color. The tint's alpha scales the layer.
 */
function buildLayer(sprite: Sprite, style: CSSStyleDeclaration, alpha: number, masks: Mask[]): HTMLElement {
  const layer = document.createElement("div");
  copyVariables(style, PICTURE, layer.style);
  const tint = style.getPropertyValue("--tint");
  const opacity = alpha * Number(style.getPropertyValue("--tint-alpha") || 1);
  applyCss(layer.style, { position: "absolute", inset: "0", opacity: String(opacity), ...toPaint(sprite) });
  applyCss(layer.style, toMask(tint ? [...masks, [sprite, "alpha"]] : masks));
  if (tint) {
    const cover = document.createElement("div");
    applyCss(cover.style, { position: "absolute", inset: "0", "background-color": tint, "mix-blend-mode": "multiply" });
    layer.style.setProperty("isolation", "isolate");
    layer.append(cover);
  }
  return layer;
}

/** Builds the hidden image that gives a widget with no size of its own the size of its texture on the screen. */
function buildSizer(sprite: Sprite): HTMLElement {
  const image = document.createElement("img");
  image.src = sprite.url;
  image.alt = "";
  applyCss(image.style, {
    "grid-area": "1 / 1",
    display: "block",
    "max-width": "none",
    visibility: "hidden",
    zoom: String(1 / sprite.density),
  });
  return image;
}

/** Converts a sprite to the CSS that paints it over its box: by its nine slices where it has a border. */
function toPaint(sprite: Sprite): Record<string, string> {
  if (isSliced(sprite)) return { "border-style": "solid", "border-width": "0", "border-image": toSlices(sprite) };
  const placement = toPlacement(sprite);
  return {
    "background-image": placement.image,
    "background-size": placement.size,
    "background-position": placement.position,
    "background-repeat": placement.repeat,
  };
}

/**
 * Converts masks to the CSS that keeps what every one of them keeps, each by its alpha or its brightness. A sprite with
 * a border masks by its nine slices, and the browser takes only one such.
 */
function toMask(masks: Mask[]): Record<string, string> {
  const sliced = masks.find(([sprite]) => isSliced(sprite));
  const layers = masks.filter((mask) => mask !== sliced).map(([sprite, mode]) => ({ ...toPlacement(sprite), mode }));
  const css: Record<string, string> = sliced ? { "-webkit-mask-box-image": toSlices(sliced[0]) } : {};
  if (!layers.length) return css;
  return {
    ...css,
    "mask-image": layers.map((layer) => layer.image).join(", "),
    "mask-mode": layers.map((layer) => layer.mode).join(", "),
    "mask-size": layers.map((layer) => layer.size).join(", "),
    "mask-position": layers.map((layer) => layer.position).join(", "),
    "mask-repeat": layers.map((layer) => layer.repeat).join(", "),
    "mask-composite": "intersect",
  };
}

/** Converts a sprite to how its image lies in its box: tiled at its size on the screen, else by its fit. */
function toPlacement(sprite: Sprite): { image: string; size: string; position: string; repeat: string } {
  if (sprite.isTiled) {
    return {
      image: `image-set(url("${sprite.url}") ${sprite.density}x)`,
      size: "auto",
      position: "0 0",
      repeat: "repeat",
    };
  }
  return { image: `url("${sprite.url}")`, ...(FITS[sprite.fit] ?? FILL), repeat: "no-repeat" };
}

/**
 * Converts a sprite's border to its nine slices, as the game's shader cuts them. Each side is cut from the image at
 * its border, in the image's pixels, and drawn at that border over the density, as the whole texture is. The middle
 * is tiled or stretched.
 */
function toSlices(sprite: Sprite): string {
  // measured: EU5's paper_card.dds is 400x212 with a border of 100 at density 2, which only fits in image pixels
  const widths = sprite.border.map((side) => `${side / sprite.density}px`).join(" ");
  const middle = sprite.isTiled ? "repeat" : "stretch";
  return `url("${sprite.url}") ${sprite.border.join(" ")} fill / ${widths} / 0 ${middle}`;
}

/** Whether a sprite has a border, which cuts it into nine slices. */
function isSliced(sprite: Sprite): boolean {
  return sprite.border.some(Boolean);
}

/**
 * Builds the placeholder of the given shape over a widget whose picture has no file. Its file says how it lies there,
 * and a mask's image file, where the widget has one, cuts it to the mask's outline.
 */
function buildPlaceholder(shape: string, mask: string): HTMLElement {
  const holder = document.createElement("div");
  holder.dataset.placeholder = shape;
  applyCss(holder.style, { position: "absolute", inset: "0", ...findPlaceholder(shape, mask && fileUrl(mask)) });
  return holder;
}

/** Builds a widget's texts as a span in its cell, placed by its align values, or null where it has none. */
function buildText(widget: Drawn, element: HTMLElement): HTMLElement | null {
  const texts = Object.values(widget.texts ?? {}).flatMap((text) => (text?.length ? [text] : []));
  if (!texts.length) return null;
  const start = widget.row.centersText ? 0.5 : 0;
  const across = findNumber(element, "--align-x", start);
  const down = findNumber(element, "--align-y", start);
  const span = document.createElement("span");
  span.append(...texts.flatMap((text, position) => [position ? " " : "", ...text.map(buildSegment)]));
  applyCss(span.style, {
    position: "relative",
    "grid-area": "1 / 1",
    "min-width": "0",
    "line-height": "normal",
    "white-space": "pre",
    "place-self": `${toPlace(down)} ${toPlace(across)}`,
    "text-align": ["left", "center", "right"][Math.round(across * 2)] ?? "left",
    // a text is cut at its box, to the whole lines the box holds and at least one; a box that grows with its text
    // has no size of its own to cut at
    "max-width": "100%",
    "max-height": "max(1lh, round(down, 100%, 1lh))",
    overflow: "hidden",
  });
  copyVariables(element.style, TEXT, span.style);
  return span;
}

/** Builds one piece of a text: plain text as it is, and the sample text of a data call in the color of its kind. */
function buildSegment(segment: Segment): Node | string {
  if (segment.markup !== Markup.MarkupCall) return segment.text;
  const sample = document.createElement("span");
  sample.textContent = segment.text;
  sample.dataset.sample = segment.text;
  sample.style.setProperty("color", SAMPLE_COLORS[segment.kind]);
  return sample;
}

/** Gives a text back its written font size and the whole of each sample in it. */
function restoreText(span: HTMLElement): void {
  span.dataset.size ??= span.style.fontSize;
  span.style.fontSize = span.dataset.size;
  for (const sample of listSamples(span)) sample.textContent = sample.dataset.sample ?? "";
}

/** Lists the samples in a text, each the sample text of a data call. */
function listSamples(span: HTMLElement): HTMLElement[] {
  return [...span.querySelectorAll<HTMLElement>("[data-sample]")];
}

/** Whether a text's box cuts part of it. */
function isCut(span: HTMLElement): boolean {
  return span.scrollWidth > span.clientWidth || span.scrollHeight > span.clientHeight;
}

/**
 * Cuts the samples of the given texts to the words that fit their boxes, a word of each sample at least. The first
 * count comes from the share of the text the box shows, and a word goes after it while the box still cuts the text.
 */
function trimSamples(spans: HTMLElement[]): void {
  let texts = spans.map((span) => {
    const words = listSamples(span).reduce((sum, sample) => sum + (sample.dataset.sample ?? "").split(" ").length, 0);
    return { span, count: Math.floor(words * findShare(span, 1)) };
  });
  while (texts.length) {
    for (const { span, count } of texts) keepWords(span, count);
    texts = texts.filter(({ span, count }) => count > listSamples(span).length && isCut(span));
    for (const text of texts) text.count -= 1;
  }
}

/**
 * Finds the share of a text at which all of it would show in its box: 1 for a text shown whole. The share of its
 * height counts at the given power: once for the words that fit, and as a root for its font size, since a wrapped
 * text grows longer and taller with its size.
 */
function findShare(span: HTMLElement, power: number): number {
  return Math.min(1, span.clientWidth / span.scrollWidth, (span.clientHeight / span.scrollHeight) ** power);
}

/** Keeps the given number of the sample words of a text, in order, and a word of each sample at least. */
function keepWords(span: HTMLElement, count: number): void {
  const samples = listSamples(span);
  let left = count;
  samples.forEach((sample, position) => {
    const words = (sample.dataset.sample ?? "").split(" ");
    const kept = Math.max(1, Math.min(words.length, left - (samples.length - position - 1)));
    sample.textContent = words.slice(0, kept).join(" ");
    left -= kept;
  });
}

/**
 * Shrinks the given texts where their widgets write a least font size, as far as each needs to fit and never below
 * that size. The first size comes from the share of the text the box shows, then a step lower where it still does
 * not fit.
 */
function shrinkTexts(spans: HTMLElement[]): void {
  const texts = spans.flatMap((span) => {
    const least = span.parentElement ? parseFloat(findVariable(span.parentElement, "--least-font-size")) : 0;
    if (!least) return [];
    const size = Math.floor(parseFloat(getComputedStyle(span).fontSize) * findShare(span, 0.5));
    return [{ span, least, size: Math.max(least, size) }];
  });
  for (const { span, size } of texts) span.style.fontSize = `${size}px`;
  for (const { span, size } of texts.filter((text) => text.size > text.least && isCut(text.span))) {
    span.style.fontSize = `${size - 1}px`;
  }
}

/** Finds the axis a scroll bar lies along from the slot it fills. */
function findAxis(bar: Drawn): "horizontal" | "vertical" {
  return bar.slot.endsWith("horizontal") ? "horizontal" : "vertical";
}

/** Whether the policy of a widget turns off the scroll bar along its axis. */
function isBarOff(holder: Drawn, bar: Drawn): boolean {
  return findValue(holder, `scrollbarpolicy_${findAxis(bar)}`).toLowerCase() === "always_off";
}

/** Makes a row that sets lengths: the parts of the value in order for the given properties, the first if short. */
function makeLength(...properties: string[]): Apply {
  return (value, style) => {
    const parts = splitValue(value);
    properties.forEach((property, position) => {
      const length = toLength(parts[position] ?? parts[0]);
      if (length) style.setProperty(property, length);
    });
  };
}

/** Makes a row that applies the CSS of the choice the value is, and nothing for any other value. */
function makeChoice(choices: Record<string, Record<string, string>>): Apply {
  return (value, style) => applyCss(style, choices[value.trim().toLowerCase()] ?? {});
}

/** Makes a row that writes the four sides of a prefix from an across and a down amount. */
function makeSides(prefix: string): Apply {
  return (value, style) => {
    const [across, down = across] = splitValue(value);
    for (const side of ["left", "right"]) makeSide(prefix, side)(across, style);
    for (const side of ["top", "bottom"]) makeSide(prefix, side)(down, style);
  };
}

/** Makes a row that writes one side of a prefix in pixels. A margin lets it be negative. */
function makeSide(prefix: string, side: string): Apply {
  return (value, style) => style.setProperty(`${prefix}-${side}`, `${Number(splitValue(value)[0]) || 0}px`);
}

/** Makes a row that writes the fractions its anchor terms name, across and down, as two custom properties. */
function makeAnchor(prefix: string): Apply {
  return (value, style) => {
    const terms = value.toLowerCase().split(/[|\s]+/);
    ANCHORS.forEach((anchors, axis) => {
      const fraction = terms.reduce<number | undefined>((latest, term) => anchors[term] ?? latest, undefined);
      if (fraction !== undefined) style.setProperty(`${prefix}-${axis ? "y" : "x"}`, String(fraction));
    });
  };
}

/** Makes a row that writes the parts of a position as the offsets on the given axes, in pixels or percent. */
function makeOffset(axes: string[]): Apply {
  return (value, style) => {
    const parts = splitValue(value);
    axes.forEach((axis, position) => {
      const part = parts[position] ?? "0";
      style.setProperty(`--offset-${axis}`, part.endsWith("%") ? part : `${Number(part) || 0}px`);
    });
  };
}

/** Makes a row that writes a layout policy as three custom properties on an axis. */
function makePolicy(axis: string): Apply {
  return (value, style) => {
    const policy = POLICIES[value.trim().toLowerCase()] ?? POLICIES.fixed;
    for (const [property, setting] of Object.entries(policy)) style.setProperty(`--${property}-${axis}`, setting);
  };
}

/** Makes a row that writes a stretch factor on an axis. */
function makeStretch(axis: string): Apply {
  return (value, style) => style.setProperty(`--stretch-${axis}`, String(Number(value) || 0));
}

/**
 * Makes a row that sets a color written as red, green, blue and alpha, each out of one, or a CSS color that Go found
 * for a binding. Given a second property, a written color is set opaque and its alpha goes to that property.
 */
function makeColor(property: string, alphaProperty = ""): Apply {
  return (value, style) => {
    const [red, green, blue, alpha = 1] = splitValue(value).map(Number);
    if (![red, green, blue, alpha].every(Number.isFinite)) {
      if (CSS.supports("color", value)) style.setProperty(property, value);
      return;
    }
    style.setProperty(property, `color(srgb ${red} ${green} ${blue} / ${alphaProperty ? 1 : alpha})`);
    if (alphaProperty) style.setProperty(alphaProperty, String(alpha));
  };
}

/** Makes a row that writes the parts of its value, separated by spaces, as a custom property. */
function makeVariable(property: string): Apply {
  return (value, style) => style.setProperty(property, splitValue(value).join(" "));
}

/** Makes a table row whose source is the wiki. */
function makeWikiRow(apply: Apply): { apply: Apply; source: string } {
  return { apply, source: WIKI };
}

/** Applies CSS properties to a style. */
function applyCss(style: CSSStyleDeclaration, css: Record<string, string>): void {
  for (const [property, setting] of Object.entries(css)) style.setProperty(property, setting);
}

/** Splits a value into its parts, a block's braces dropped. */
function splitValue(value: string): string[] {
  return value.replace(/[{}]/g, " ").trim().split(/\s+/);
}

/** Converts text to a CSS length: a number in pixels, a percent as it is, and undefined for -1, 0 or a binding. */
function toLength(text: string | undefined): string | undefined {
  if (!text) return undefined;
  if (text.endsWith("%")) return text;
  const number = Number(text);
  return Number.isFinite(number) && number > 0 ? `${number}px` : undefined;
}

/** Converts a fraction of free room to the place a grid or a box aligns at. */
function toPlace(fraction: number): string {
  return PLACES[Math.round(fraction * 2)] ?? "start";
}

/** Finds the value a widget or an overlay writes for a key, or "" where it writes none. */
function findValue(holder: Pick<Drawn, "properties">, key: string): string {
  return holder.properties.findLast((property) => property.key.toLowerCase() === key)?.value ?? "";
}

/** Finds a custom property an element's rows wrote, or "" where none did. */
function findVariable(element: HTMLElement, property: string): string {
  return element.style.getPropertyValue(property);
}

/** Finds a custom property as a number, or the given number where none was written. */
function findNumber(element: HTMLElement, property: string, fallback: number): number {
  const text = findVariable(element, property);
  return text ? Number(text) : fallback;
}

/** Copies the custom properties of a style that start with a prefix to another style, without the prefix. */
function copyVariables(source: CSSStyleDeclaration, prefix: string, target: CSSStyleDeclaration): void {
  for (const property of Array.from(source)) {
    if (property.startsWith(prefix))
      target.setProperty(property.slice(prefix.length), source.getPropertyValue(property));
  }
}
