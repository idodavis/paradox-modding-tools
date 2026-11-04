/**
 * Builds the picture of a window as DOM from Go's widgets, using the tables in `properties.ts` for what each holding
 * and property means in CSS. The browser lays it out and each widget's rect is read back for the marks, the panel and
 * the code link. Go measures nothing and the page computes no layout.
 */
import { Markup, type Segment } from "@internal/loc/models";
import type { Choice, Font, Overlay, Property, Row, States, Widget, Window } from "@services/draw/models";

import { SAMPLE_COLORS, findPlaceholder } from "../../../lib/placeholders";
import { fileUrl } from "../../../queries";
import { HOLDS, PICTURE, PROPERTIES, TEXT, applyCss } from "./properties";

/**
 * A Drawing is how a window is drawn: which widgets are hidden, which values the states choose, and how many rows a
 * list that reads no database draws.
 */
type Drawing = { hidden: ReadonlySet<number>; choices: States["choices"]; rows: number };

/** A Scene is a window being built into the DOM: its drawing, its lists' rows by item, its widgets' kids and nodes. */
type Scene = Drawing & { lists: Window["rows"]; children: Map<number, Drawn[]>; nodes: PictureNode[] };

/**
 * A Drawn is a widget with its properties and its overlays' properties looked up in its window's tables, where Go gives
 * only their positions.
 */
type Drawn = Omit<Widget, "properties" | "overlays"> & {
  properties: Property[];
  overlays: (Omit<Overlay, "properties"> & { properties: Property[] })[];
};

/** A Sprite is one texture: its image, cut to one frame of a sheet, and how it lies in its box. */
type Sprite = { url: string; border: number[]; density: number; isTiled: boolean; fit: string };

/** A Mask is a sprite that keeps part of a layer, by its alpha or by its brightness. */
type Mask = [sprite: Sprite, mode: "alpha" | "luminance"];

/**
 * WRITTEN_SIZES lists the built-in types that are as large as they write, or 0 where they write nothing, whatever their
 * children's size: the plain widget, which EU5 builds as uberwidget. Other types grow to their children.
 */
const WRITTEN_SIZES = ["widget", "uberwidget"];

/** MASKS lists the texture keys whose image is a mask, not a picture: a coat of arms' mask or the widget's own. */
const MASKS = ["coat_of_arms_mask", "mask"];

const SIDES = ["top", "right", "bottom", "left"];
const PLACES = ["start", "center", "end"] as const;

/** GRID_AXES is the CSS of a grid's tracks on each axis: its auto flow, and the properties for their count and size. */
const GRID_AXES = {
  x: { flow: "row", template: "grid-template-columns", track: "grid-auto-columns" },
  y: { flow: "column", template: "grid-template-rows", track: "grid-auto-rows" },
};

/** FITS is how a texture lies in its widget for each fittype. Any other fittype fills the widget. */
const FITS: Record<string, { size: string; position: string }> = {
  centercrop: { size: "cover", position: "center" },
  start: { size: "contain", position: "left top" },
  center: { size: "contain", position: "center" },
  end: { size: "contain", position: "right bottom" },
};
const FILL = { size: "100% 100%", position: "0 0" };

/** CUTS lists the custom properties sent with an image's path, from which Go cuts one frame out of a sprite sheet. */
const CUTS = ["frame", "framesize", "framegrid", "density"];

/** A PictureNode is one drawn widget: its element and its id. */
export type PictureNode = { id: number; element: HTMLElement };

/** faces is the font faces the page has, by the font's name and style, with the file each is loaded from. */
const faces = new Map<string, { path: string; face: FontFace }>();

/**
 * buildPicture builds the DOM of a window's widgets inside the given floor element, as the given drawing says. The
 * window sits inside its holders, nested boxes with the holders' own properties, the outermost on the floor. It returns
 * a node for each widget built; a hidden widget that takes no room has none.
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
  // A text with no font or size of its own uses those of the game's text type, applied through their rows.
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

/**
 * readRects reads the rect of each node relative to the floor, after layout. A widget has one rect for each copy of it
 * in the rows of an item.
 */
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
 * placeOnScreen moves the window's root widget flush with the floor's edge it lies wholly past, so that it can be
 * seen. The game places such a window by code, next to what opened it. A window partly on the floor stays where its
 * properties put it, cut by the floor's edge. It returns whether the window was moved.
 */
export function placeOnScreen(nodes: PictureNode[], floor: HTMLElement): boolean {
  const root = nodes[0];
  if (!root) return false;
  root.element.style.removeProperty("transform");
  const [box] = readRects([root], floor).get(root.id) ?? [];
  if (!box) return false;
  const across = findShift(box.left, box.right, floor.offsetWidth);
  const down = findShift(box.top, box.bottom, floor.offsetHeight);
  if (!across && !down) return false;
  root.element.style.setProperty("transform", `translate(${across}px, ${down}px)`);
  return true;
}

/** findShift returns how far to move a span to make it flush with the floor's edge it lies wholly past, else 0. */
function findShift(start: number, end: number, length: number): number {
  if (start >= length) return length - end;
  return end <= 0 ? -start : 0;
}

/**
 * fitTexts fits each text that its box cuts. It first gives each text back its written size and whole samples, so the
 * picture can be fitted again once its fonts and images load. Then it trims the samples to the words that fit, one word
 * of each at least, so a box that holds one line gets a short sample. A text still cut shrinks, where its widget writes
 * a least font size, as far as it needs and never below that size, as the game shrinks it.
 */
export function fitTexts(nodes: PictureNode[]): void {
  const spans = nodes.flatMap(({ element }) => element.querySelector<HTMLElement>(":scope > span") ?? []);
  for (const span of spans) {
    span.dataset.size ??= span.style.fontSize;
    span.style.fontSize = span.dataset.size;
    for (const sample of listSamples(span)) sample.textContent = sample.dataset.sample ?? "";
  }
  trimSamples(spans.filter((span) => listSamples(span).length && isCut(span)));
  shrinkTexts(spans.filter(isCut));
}

/**
 * fitPlaceholders fits each placeholder to the part of its widget's box that lies inside the widget's holder, the box
 * the game draws the picture in, so the placeholder's whole shape shows. A widget wholly outside its holder shows none.
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

/**
 * listProperties looks up the properties at the given positions in a window's property table, where Go gives a widget
 * or an overlay only the positions. No positions give no properties.
 */
export function listProperties(window: Window, positions: number[] | null): Property[] {
  return (positions ?? []).map((position) => (window.properties ?? [])[position]);
}

/**
 * applyProperties applies the given properties to a style, each through its row in PROPERTIES, in the order of that
 * table. If a key is written twice, the last value wins.
 */
export function applyProperties(properties: Property[] | null, style: CSSStyleDeclaration): void {
  const values = new Map((properties ?? []).map((property) => [property.key.toLowerCase(), property.value]));
  for (const [key, row] of Object.entries(PROPERTIES)) {
    const value = values.get(key);
    if (value !== undefined) row.apply(value, style);
  }
}

/**
 * loadFonts adds a font face to the page for each style of the given fonts, named by its font and loaded from its
 * file. A face of the same font and style loaded from another file, such as another game's, is replaced.
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
 * buildWidget builds the element of a widget with its pictures, text and children. It returns null for a hidden widget
 * that takes no room. In a row of a list that reads a database, the widget takes the values of the row's definition.
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
 * listRows lists the rows to draw a child of a widget in. An item of a list that reads a database gets a row for each
 * definition. Any other item gets the scene's sample number of copies of the given row. Any other child gets the given
 * row once.
 */
function listRows(scene: Scene, kid: Drawn, row: Row | null): (Row | null)[] {
  if (!kid.item) return [row];
  return scene.lists?.[`${kid.id}`] ?? Array.from({ length: scene.rows }, () => row);
}

/** toDrawn makes a Drawn of a widget by looking up its and its overlays' properties in the window's tables. */
function toDrawn(window: Window, widget: Widget): Drawn {
  const overlays = (widget.overlays ?? []).map((position) => (window.overlays ?? [])[position]);
  return {
    ...widget,
    properties: listProperties(window, widget.properties),
    overlays: overlays.map((overlay) => ({ ...overlay, properties: listProperties(window, overlay.properties) })),
  };
}

/**
 * applyChoices returns the widget with the values the states choose. Each property takes the value chosen for its key.
 * The texts, textures and colors the choices lead to replace those of the same key, and a chosen texture that is not a
 * mask removes the placeholder. It returns the widget as it is when there are no choices.
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

/**
 * applyStyle applies a widget's CSS to its element: its holding, each property through its row, and what its role
 * makes of it.
 */
function applyStyle(element: HTMLElement, widget: Drawn, holder: Drawn | null): void {
  const { style } = element;
  applyCss(style, { position: "relative", "box-sizing": "border-box", "min-width": "0", "min-height": "0" });
  applyCss(style, HOLDS[widget.row.holding || "cell"].css);
  // custom properties are inherited, so a widget with no margin of its own starts from none
  for (const side of SIDES) style.setProperty(`--margin-${side}`, "0px");
  applyProperties(widget.properties, style);
  // A color Go resolved for a binding goes through the row of the property it belongs to.
  for (const [key, color] of Object.entries(widget.colors ?? {})) {
    if (color) PROPERTIES[key.toLowerCase()]?.apply(color, style);
  }
  const margins = SIDES.map((side) => `var(--margin-${side}, 0px)`).join(" ");
  const isFree = ["skin", "chrome"].includes(widget.role) || !!widget.row.fills;
  element.dataset.free = String(isFree);
  // The content of a scroll area is sized by what it holds, whatever its type. A size written as a binding comes from
  // the running game, which the page cannot know, so that widget grows to its children.
  const isScrolled = holder?.row.holding === "scroll";
  const isBound = findValue(widget, "size").includes("[");
  if (WRITTEN_SIZES.includes(widget.builtin) && !isFree && !isScrolled && !isBound) {
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
  // The content of a scroll area fills it at least. Its skins and scroll bars keep their own boxes.
  if (isScrolled && !isFree) applyCss(style, { "min-width": "100%", "min-height": "100%" });
}

/**
 * applyLayout applies the layout of the given children inside their holder, from what their properties wrote: anchors,
 * policies and fits. A child that sizes its holder to its minimum makes the holder as large as what it holds, and the
 * child's own maximum does not cut it. EU5's basic_dialog caps its box at 300 high, and the agenda inside it stands 885
 * high and whole in the game.
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
 * applyGrid applies how a grid fills its cells. It fills down a column, then the next, or across a row, then the next,
 * when it is flipped. A line holds as many cells as the grid's wrap, or its most slots along the fill if that is
 * fewer. A grid that writes neither, but writes its most slots across the fill, fills that many lines. Any other grid
 * fills one line.
 */
function applyGrid(grid: HTMLElement): void {
  const [along, across] = findVariable(grid, "--flip") === "1" ? (["x", "y"] as const) : (["y", "x"] as const);
  const counts = [findNumber(grid, "--wrap", 0), findNumber(grid, `--slots-${along}`, 0)].filter((count) => count > 0);
  // With a line length, the tracks run along the fill with that count. Without one, they run across it with the slots
  // written there, or with no count, which CSS fills as one line.
  const [axis, count] = counts.length
    ? [along, Math.min(...counts)]
    : [across, findNumber(grid, `--slots-${across}`, 0)];
  const { style } = grid;
  const { flow, template, track } = GRID_AXES[axis];
  style.setProperty("grid-auto-flow", flow);
  if (count > 0) style.setProperty(template, `repeat(${count}, ${style.getPropertyValue(track) || "max-content"})`);
}

/**
 * applyCell applies the place of a child in a cell: a grid item at its holder's anchor, shifted by its own anchor, or
 * positioned over the cell if the child is ignored by layout.
 */
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
  // A widget that grows fills the cell on each axis where it writes no length, so a window keeps its written size.
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

/** applyBox applies the place of children of a box: along its axis by their policies and across it by their anchors. */
function applyBox(children: HTMLElement[], along: "x" | "y"): void {
  const across = along === "x" ? "y" : "x";
  const isExpanding = (child: HTMLElement) => findVariable(child, `--expand-${along}`) === "1";
  const hasExpanding = children.some(isExpanding);
  for (const child of children) {
    const { style } = child;
    const isGrowing = findVariable(child, `--grow-${along}`) === "1";
    const grows = isExpanding(child) || (isGrowing && !hasExpanding);
    style.setProperty("flex-grow", String(findNumber(child, `--stretch-${along}`, 0) || (grows ? 1 : 0)));
    style.setProperty("flex-shrink", findVariable(child, `--shrink-${along}`) === "1" ? "1" : "0");
    // A widget that grows across, or a plain widget that writes no breadth, takes the box's breadth.
    const breadth = across === "x" ? "width" : "height";
    if (findVariable(child, `--grow-${across}`) === "1" || child.dataset.unsized?.includes(breadth)) {
      applyCss(style, { "align-self": "stretch", [breadth]: "auto" });
    } else if (child.dataset.free !== "true" && findVariable(child, `--parent-${across}`)) {
      style.setProperty("align-self", toPlace(findNumber(child, `--parent-${across}`, 0)));
    }
  }
}

/**
 * buildPictures builds a widget's textures, then its placeholder over them, then its text. The placeholder is the
 * stand-in Go names for a picture that has no file, such as a portrait's silhouette over its transparent texture. A
 * mask is not a texture: it cuts the placeholder to its shape. A line draws no picture.
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
    // A widget with a texture and no size is the texture's size, as the wiki says.
    if (!findVariable(element, "width") && !findVariable(element, "height")) {
      const sizer = document.createElement("img");
      sizer.src = own.url;
      sizer.alt = "";
      applyCss(sizer.style, {
        "grid-area": "1 / 1",
        display: "block",
        "max-width": "none",
        visibility: "hidden",
        zoom: String(1 / own.density),
      });
      pictures.push(sizer);
    }
  }
  if (widget.placeholder && !isLine) {
    const placeholder = document.createElement("div");
    placeholder.dataset.placeholder = widget.placeholder;
    applyCss(placeholder.style, {
      position: "absolute",
      inset: "0",
      ...findPlaceholder(widget.placeholder, mask && fileUrl(mask)),
    });
    pictures.push(placeholder);
  }
  const text = buildText(widget, element);
  return text ? [...pictures, text] : pictures;
}

/** makeSprite makes a sprite of an image file, reading its cut, border, density, tiling and fit from a style. */
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
 * buildTextures builds a widget's own texture and its overlays as one group over the widget, in the order the game
 * lays them on. The group uses the own texture's blend and shows only where every alphamultiply overlay is opaque. Each
 * other overlay blends with the own texture alone, kept to the own texture's shape and to where the last luminance
 * mask overlay before it is bright. An overlay with visible = no is left out.
 */
function buildTextures(own: Sprite, overlays: Drawn["overlays"], style: CSSStyleDeclaration): HTMLElement {
  const group = document.createElement("div");
  const alphas: Mask[] = [];
  let shade: Mask | undefined;
  const layers = [buildLayer(own, style, 1, [])];
  for (const overlay of overlays) {
    if (!overlay.path || findValue(overlay, "visible").toLowerCase() === "no") continue;
    const scratch = document.createElement("div").style;
    // An overlay that writes no blend mode adds, the shader's first mode. Measured against the game on EU5's
    // building_view: its minus and plus icons and its production method header.
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
 * buildLayer builds one texture as a layer over its widget, at the given alpha and kept to the given masks. A tint is
 * multiplied over the whole layer, which is then kept to the image's shape, as the game's shader multiplies each pixel
 * by the tint's color. The tint's alpha scales the layer.
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

/** toPaint converts a sprite to the CSS that paints its box: by its nine slices if it has a border. */
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
 * toMask converts masks to the CSS that keeps only what every mask keeps, each by its alpha or its brightness. A sprite
 * with a border masks by its nine slices, and the browser takes only one of those.
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

/** toPlacement converts a sprite to how its image lies in its box: tiled at its size on the screen, else by its fit. */
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
 * toSlices converts a sprite's border to a CSS nine-slice, as the game's shader cuts it. Each side is cut at its
 * border in image pixels and drawn at that border divided by the density. The middle is tiled or stretched.
 */
function toSlices(sprite: Sprite): string {
  // measured: EU5's paper_card.dds is 400x212 with a border of 100 at density 2, which only fits in image pixels
  const widths = sprite.border.map((side) => `${side / sprite.density}px`).join(" ");
  const middle = sprite.isTiled ? "repeat" : "stretch";
  return `url("${sprite.url}") ${sprite.border.join(" ")} fill / ${widths} / 0 ${middle}`;
}

/** isSliced reports whether a sprite has a border, which cuts it into nine slices. */
function isSliced(sprite: Sprite): boolean {
  return sprite.border.some(Boolean);
}

/** buildText builds a widget's texts as a span in its cell, placed by its align values. Null if it has none. */
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
    // A text is cut at its box, to the whole lines the box holds and at least one. A box that grows with its text has
    // no size of its own to cut at.
    "max-width": "100%",
    "max-height": "max(1lh, round(down, 100%, 1lh))",
    overflow: "hidden",
  });
  copyVariables(element.style, TEXT, span.style);
  return span;
}

/** buildSegment builds one piece of a text: plain text as it is, or a data call's sample in the color of its kind. */
function buildSegment(segment: Segment): Node | string {
  if (segment.markup !== Markup.MarkupCall) return segment.text;
  const sample = document.createElement("span");
  sample.textContent = segment.text;
  sample.dataset.sample = segment.text;
  sample.style.setProperty("color", SAMPLE_COLORS[segment.kind]);
  return sample;
}

/** listSamples lists the samples in a text, the spans that show the sample text of its data calls. */
function listSamples(span: HTMLElement): HTMLElement[] {
  return [...span.querySelectorAll<HTMLElement>("[data-sample]")];
}

/** isCut reports whether a text's box cuts part of it. */
function isCut(span: HTMLElement): boolean {
  return span.scrollWidth > span.clientWidth || span.scrollHeight > span.clientHeight;
}

/**
 * trimSamples trims the samples of the given texts to the words that fit their boxes, one word of each sample at
 * least. The first word count is the share of the text the box shows. Then one word at a time is dropped while the box
 * still cuts the text.
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
 * findShare returns the share of a text that shows in its box: 1 for a text shown whole. The share of the height is
 * raised to the given power: 1 for the number of words that fit, 0.5 for the font size, since a wrapped text grows both
 * longer and taller with its size.
 */
function findShare(span: HTMLElement, power: number): number {
  return Math.min(1, span.clientWidth / span.scrollWidth, (span.clientHeight / span.scrollHeight) ** power);
}

/** keepWords keeps the given number of sample words in a text, in order, and at least one word of each sample. */
function keepWords(span: HTMLElement, count: number): void {
  const samples = listSamples(span);
  let remaining = count;
  samples.forEach((sample, position) => {
    const words = (sample.dataset.sample ?? "").split(" ");
    const kept = Math.max(1, Math.min(words.length, remaining - (samples.length - position - 1)));
    sample.textContent = words.slice(0, kept).join(" ");
    remaining -= kept;
  });
}

/**
 * shrinkTexts shrinks the given texts, where their widgets write a least font size, as far as each needs and never
 * below that size. The first size comes from the share of the text the box shows, then one pixel lower where the text
 * is still cut.
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

/** findAxis finds the axis a scroll bar lies along from the slot it fills. */
function findAxis(bar: Drawn): "horizontal" | "vertical" {
  return bar.slot.endsWith("horizontal") ? "horizontal" : "vertical";
}

/** isBarOff reports whether the policy of a widget turns off the scroll bar along its axis. */
function isBarOff(holder: Drawn, bar: Drawn): boolean {
  return findValue(holder, `scrollbarpolicy_${findAxis(bar)}`).toLowerCase() === "always_off";
}

/** toPlace converts a fraction of the free room to the place a grid or a box aligns to: start, center or end. */
function toPlace(fraction: number): string {
  return PLACES[Math.round(fraction * 2)] ?? "start";
}

/** findValue finds the value a widget or an overlay writes for a key, or "" if it writes none. */
function findValue(holder: Pick<Drawn, "properties">, key: string): string {
  return holder.properties.findLast((property) => property.key.toLowerCase() === key)?.value ?? "";
}

/** findVariable finds a custom property of an element, or "" if it has none. */
function findVariable(element: HTMLElement, property: string): string {
  return element.style.getPropertyValue(property);
}

/** findNumber finds a custom property of an element as a number, or the given fallback if it has none. */
function findNumber(element: HTMLElement, property: string, fallback: number): number {
  const text = findVariable(element, property);
  return text ? Number(text) : fallback;
}

/** copyVariables copies the custom properties of a style that start with a prefix to another, minus the prefix. */
function copyVariables(source: CSSStyleDeclaration, prefix: string, target: CSSStyleDeclaration): void {
  for (const property of Array.from(source)) {
    if (property.startsWith(prefix))
      target.setProperty(property.slice(prefix.length), source.getPropertyValue(property));
  }
}
