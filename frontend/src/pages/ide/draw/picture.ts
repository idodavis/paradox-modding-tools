/**
 * The picture: what a holding and a property mean in CSS, in two tables with each row sourced to the wiki or
 * marked observed, and the DOM built from Go's widgets through them. The browser lays it out, and each widget's
 * rect is read back for the marks, the panel and the code link. Go measures nothing and the page computes no
 * layout.
 */
import type { Choice, Font, Overlay, Property, States, Widget, Window } from "@services/draw/models";

import { findPlaceholder } from "../../../lib/placeholders";
import { fileUrl } from "../../../queries";

type Apply = (value: string, style: CSSStyleDeclaration) => void;

/** A Drawing is how a window is drawn: the widgets hidden, the values the states choose, and the rows of each list. */
type Drawing = { hidden: ReadonlySet<number>; choices: States["choices"]; rows: number };

/** A Scene is a window being built into the DOM. */
type Scene = Drawing & { children: Map<number, Widget[]>; nodes: PictureNode[] };

/** A Sprite is one texture as its rows wrote it: its image, cut to one frame of a sheet, and how it lies in its box. */
type Sprite = { url: string; border: number[]; density: number; isTiled: boolean; fit: string };

/** A Mask is a sprite that keeps part of a layer, by its alpha or by its brightness. */
type Mask = [sprite: Sprite, mode: "alpha" | "luminance"];

const WIKI = "the wiki's Interface page";
const GAME_FILES = "the game's own GUI files";

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

/** The wiki's five layout policies: whether each grows into free room, shrinks, or takes it before the others. */
const POLICIES: Record<string, { grow: string; shrink: string; expand: string }> = {
  fixed: { grow: "0", shrink: "0", expand: "0" },
  growing: { grow: "1", shrink: "0", expand: "0" },
  shrinking: { grow: "0", shrink: "1", expand: "0" },
  preferred: { grow: "1", shrink: "1", expand: "0" },
  expanding: { grow: "1", shrink: "0", expand: "1" },
};

/** The text cut with "…" where it is too long. */
const ELIDED = {
  [`${TEXT}overflow`]: "hidden",
  [`${TEXT}text-overflow`]: "ellipsis",
  [`${TEXT}max-width`]: "100%",
};

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

/** The glyph that stands for a picture no file leads to and no placeholder of the game's stands for. */
const GLYPH = `<svg viewBox="0 0 24 24" width="48" height="48" fill="none" stroke="currentColor" stroke-width="2"
  stroke-linecap="round" stroke-linejoin="round" style="max-width:100%;max-height:100%"><rect width="18" height="18"
  x="3" y="3" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/></svg>`;

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
    css: { display: "grid", "grid-auto-flow": "row", "justify-content": "start", "align-content": "start" },
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
  addcolumn: makeWikiRow(makeLength("grid-auto-columns")),
  addrow: makeWikiRow(makeLength("grid-auto-rows")),
  datamodel_wrap: makeWikiRow(makeChoice({ yes: { "flex-wrap": "wrap" } })),
  maxhorizontalslots: makeWikiRow(makeCount("grid-template-columns")),
  maxverticalslots: makeWikiRow(makeCount("grid-template-rows")),
  flipdirection: makeWikiRow(makeChoice({ yes: { "grid-auto-flow": "column" } })),
  autoresizescrollarea: makeWikiRow(makeChoice({ yes: { overflow: "visible", contain: "none" } })),
  align: makeWikiRow(makeAnchor("--align")),
  autoresize: makeWikiRow(makeChoice({ yes: { width: "max-content", height: "max-content" } })),
  multiline: makeWikiRow(makeChoice({ yes: { [`${TEXT}white-space`]: "pre-wrap" } })),
  elide: makeWikiRow(makeChoice({ left: ELIDED, right: ELIDED })),
  font: {
    apply: (value, style) => style.setProperty(`${TEXT}font-family`, `"${value}", sans-serif`),
    source: GAME_FILES,
  },
  fontsize: { apply: makeLength(`${TEXT}font-size`), source: GAME_FILES },
  fontcolor: { apply: makeColor(`${TEXT}color`), source: GAME_FILES },
  texture: { apply: (_value, style) => style.setProperty("--pictured", "1"), source: WIKI },
  icon: { apply: (_value, style) => style.setProperty("--pictured", "1"), source: WIKI },
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
  const widgets = (window.widgets ?? []).map((widget) => applyChoices(widget, drawing.choices?.[`${widget.id}`] ?? []));
  loadFonts(window.fonts ?? []);
  const children = Map.groupBy(
    widgets.filter((widget) => widget.id !== 0),
    (widget) => widget.parent,
  );
  const scene: Scene = { ...drawing, children, nodes: [] };
  floor.replaceChildren();
  floor.dataset.holds = "cell";
  applyCss(floor.style, HOLDS.cell.css);
  let holding = floor;
  for (const holder of window.holders ?? []) {
    const box = document.createElement("div");
    box.dataset.holds = "cell";
    applyStyle(box, holder, null);
    holding.append(box);
    applyLayout(holding, [box]);
    holding = box;
  }
  const root = widgets[0] ? buildWidget(scene, widgets[0], null) : null;
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

/** Builds the element of a widget with its pictures, text and children, or null where a hidden one takes no room. */
function buildWidget(scene: Scene, widget: Widget, holder: Widget | null): HTMLElement | null {
  const isHidden = scene.hidden.has(widget.id);
  if (isHidden && (holder?.row.DropsHidden ?? true)) return null;
  const element = document.createElement("div");
  element.dataset.element = String(widget.id);
  element.dataset.holds = widget.row.Holding || "cell";
  scene.nodes.push({ id: widget.id, element });
  applyStyle(element, widget, holder);
  if (isHidden) element.style.setProperty("visibility", "hidden");
  const kids = scene.children.get(widget.id) ?? [];
  const build = (role: string) =>
    kids
      .filter((kid) => kid.role === role && !(role === "chrome" && isBarOff(widget, kid)))
      .flatMap((kid) => Array.from({ length: kid.item ? scene.rows : 1 }, () => buildWidget(scene, kid, widget)))
      .filter((child) => child !== null);
  const skins = build("skin");
  const others = [...build(""), ...build("label"), ...build("chrome")];
  element.append(...skins, ...buildPictures(widget, element), ...others);
  applyLayout(element, [...skins, ...others]);
  return element;
}

/**
 * Gives a widget the values the states choose for its properties, each in place of the choice its property writes,
 * with the texts and image files they lead to.
 */
function applyChoices(widget: Widget, choices: Choice[]): Widget {
  if (!choices.length) return widget;
  const values = new Map(choices.map((choice) => [choice.key, choice.value]));
  const toRecord = (field: "text" | "texture") =>
    Object.fromEntries(choices.flatMap((choice) => (choice[field] ? [[choice.key, choice[field]]] : [])));
  return {
    ...widget,
    properties: (widget.properties ?? []).map((property) => ({
      ...property,
      value: values.get(property.key) ?? property.value,
    })),
    texts: { ...widget.texts, ...toRecord("text") },
    textures: { ...widget.textures, ...toRecord("texture") },
  };
}

/** Applies a widget's CSS to its element: its holding, each property through its row, and what its role makes of it. */
function applyStyle(element: HTMLElement, widget: Widget, holder: Widget | null): void {
  const { style } = element;
  applyCss(style, { position: "relative", "box-sizing": "border-box", "min-width": "0", "min-height": "0" });
  applyCss(style, HOLDS[widget.row.Holding || "cell"].css);
  // custom properties are inherited, so a widget with no margin of its own starts from none
  for (const side of SIDES) style.setProperty(`--margin-${side}`, "0px");
  applyProperties(widget.properties, style);
  const margins = SIDES.map((side) => `var(--margin-${side}, 0px)`).join(" ");
  element.dataset.free = String(["skin", "chrome"].includes(widget.role) || widget.row.Fills);
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
  if (widget.row.Clips) style.setProperty("overflow", "hidden");
  if (widget.row.Fills) applyCss(style, { position: "absolute", inset: "0", width: "auto", height: "auto" });
  if (holder?.row.Holding === "scroll") applyCss(style, { "min-width": "100%", "min-height": "100%" });
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
  }
  if (holding === "row" || holding === "column") applyBox(children, holding === "row" ? "x" : "y");
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
  if (findVariable(child, "--grow-x") === "1") style.setProperty("width", "100%");
  if (findVariable(child, "--grow-y") === "1") style.setProperty("height", "100%");
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
    if (findVariable(child, `--grow-${across}`) === "1") {
      applyCss(style, { "align-self": "stretch", [across === "x" ? "width" : "height"]: "auto" });
    } else if (child.dataset.free !== "true" && findVariable(child, `--parent-${across}`)) {
      style.setProperty("align-self", toPlace(findNumber(child, `--parent-${across}`, 0)));
    }
  }
}

/**
 * Builds a widget's textures, its placeholder and its text. A picture that no file leads to draws the game's
 * placeholder for it where Go names one, else the glyph; a background, which only dresses its holder, draws nothing
 * then, so no glyph sits over what the window holds. A line draws no picture.
 */
function buildPictures(widget: Widget, element: HTMLElement): HTMLElement[] {
  const isLine = findVariable(element, "--line") !== "";
  const [path] = isLine ? [] : Object.values(widget.textures ?? {});
  const pictures: HTMLElement[] = [];
  if (path) {
    const own = makeSprite(path, element.style);
    pictures.push(buildTextures(own, widget.overlays ?? [], element.style));
    // the wiki: a widget with a texture and no size is the texture's size
    if (!findVariable(element, "width") && !findVariable(element, "height")) pictures.push(buildSizer(own));
  } else if (findVariable(element, "--pictured") && !widget.placeholder && !isLine && widget.role !== "skin") {
    pictures.push(buildGlyph());
  }
  if (widget.placeholder) pictures.push(buildPlaceholder(widget.placeholder));
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
function buildTextures(own: Sprite, overlays: Overlay[], style: CSSStyleDeclaration): HTMLElement {
  const group = document.createElement("div");
  const alphas: Mask[] = [];
  let shade: Mask | undefined;
  const layers = [buildLayer(own, style, 1, [])];
  for (const overlay of overlays) {
    if (!overlay.path || findValue(overlay, "visible").toLowerCase() === "no") continue;
    const scratch = document.createElement("div").style;
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
 * Converts a sprite's border to its nine slices, as the game's shader cuts them. Each side is drawn at its width on
 * the screen and cut from the image at that width times the density. The middle is tiled or stretched.
 */
function toSlices(sprite: Sprite): string {
  const cut = sprite.border.map((side) => Math.round(side * sprite.density)).join(" ");
  const middle = sprite.isTiled ? "repeat" : "stretch";
  return `url("${sprite.url}") ${cut} fill / ${sprite.border.join("px ")}px / 0 ${middle}`;
}

/** Whether a sprite has a border, which cuts it into nine slices. */
function isSliced(sprite: Sprite): boolean {
  return sprite.border.some(Boolean);
}

/** Builds the glyph over a widget that stands for a picture the game supplies through a binding, or no file has. */
function buildGlyph(): HTMLElement {
  const holder = document.createElement("div");
  holder.style.cssText = "position: absolute; inset: 0; display: grid; place-items: center; color: var(--link)";
  holder.innerHTML = GLYPH;
  return holder;
}

/**
 * Builds the placeholder in the given file over a widget whose picture only the running game draws, in the glyph's
 * color. The file says how the placeholder lies in the widget.
 */
function buildPlaceholder(file: string): HTMLElement {
  const holder = document.createElement("div");
  applyCss(holder.style, {
    position: "absolute",
    inset: "0",
    background: "var(--link)",
    mask: `${findPlaceholder(file)} 0 0 / 100% 100% no-repeat`,
  });
  return holder;
}

/** Builds a widget's text as a span in its cell, placed by its align values, or null where it has none. */
function buildText(widget: Widget, element: HTMLElement): HTMLElement | null {
  const text = Object.values(widget.texts ?? {}).join(" ");
  if (!text) return null;
  const start = widget.row.CentersText ? 0.5 : 0;
  const across = findNumber(element, "--align-x", start);
  const down = findNumber(element, "--align-y", start);
  const span = document.createElement("span");
  span.textContent = text;
  applyCss(span.style, {
    position: "relative",
    "grid-area": "1 / 1",
    "min-width": "0",
    "line-height": "normal",
    "white-space": "pre",
    "font-size": "14px",
    "place-self": `${toPlace(down)} ${toPlace(across)}`,
    "text-align": ["left", "center", "right"][Math.round(across * 2)] ?? "left",
  });
  copyVariables(element.style, TEXT, span.style);
  return span;
}

/** Finds the axis a scroll bar lies along from the slot it fills. */
function findAxis(bar: Widget): "horizontal" | "vertical" {
  return bar.slot.endsWith("horizontal") ? "horizontal" : "vertical";
}

/** Whether the policy of a widget turns off the scroll bar along its axis. */
function isBarOff(holder: Widget, bar: Widget): boolean {
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

/** Makes a row that sets a count of cells as that many tracks as wide as their content. */
function makeCount(property: string): Apply {
  return (value, style) => {
    const count = Number(value);
    if (count > 0) style.setProperty(property, `repeat(${count}, max-content)`);
  };
}

/**
 * Makes a row that sets a color written as red, green, blue and alpha, each out of one. Given a second property, the
 * color is set opaque and its alpha goes to that property.
 */
function makeColor(property: string, alphaProperty = ""): Apply {
  return (value, style) => {
    const [red, green, blue, alpha = 1] = splitValue(value).map(Number);
    if (![red, green, blue, alpha].every(Number.isFinite)) return;
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
function findValue(holder: Pick<Widget, "properties">, key: string): string {
  return holder.properties?.findLast((property) => property.key.toLowerCase() === key)?.value ?? "";
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
