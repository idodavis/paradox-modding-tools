/** The tables of what each holding and property mean in CSS, each row sourced to the wiki or marked observed. */

type Apply = (value: string, style: CSSStyleDeclaration) => void;

const WIKI = "the wiki's Interface page";
const GAME_FILES = "the game's own GUI files";

/** TEXT and PICTURE prefix the custom properties a row writes for its widget's text or pictures. */
export const TEXT = "--text-";
export const PICTURE = "--picture-";

/** ANCHORS maps each anchor term to its place as a fraction, across first and then down. */
const ANCHORS: Record<string, number>[] = [
  { left: 0, hcenter: 0.5, center: 0.5, right: 1 },
  { top: 0, vcenter: 0.5, center: 0.5, bottom: 1 },
];

/** POLICIES is the wiki's five layout policies, each as whether it grows, shrinks or expands. */
const POLICIES: Record<string, { grow: string; shrink: string; expand: string }> = {
  fixed: { grow: "0", shrink: "0", expand: "0" },
  growing: { grow: "1", shrink: "0", expand: "0" },
  shrinking: { grow: "0", shrink: "1", expand: "0" },
  preferred: { grow: "1", shrink: "1", expand: "0" },
  expanding: { grow: "1", shrink: "1", expand: "1" },
};

/** ELIDED cuts a text with "…" where it is too long for its box. */
const ELIDED = { [`${TEXT}text-overflow`]: "ellipsis" };

/** TYPE_ICONS is the icon of each widget type. */
const TYPE_ICONS: Record<string, string> = {
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

/** COLOR matches a color as a GUI file writes it, such as { 1 0.5 0 }. */
const COLOR = /^\{\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)(?:\s+([\d.]+))?\s*\}$/;

/** HOLDS is how each holding lays its children out, as CSS, with the source of the meaning. */
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

/** PROPERTIES holds what a property means, as CSS on the widget, with the source of the meaning. */
export const PROPERTIES: Readonly<Record<string, { apply: Apply; source: string }>> = {
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
  // A column written as a share is a share of the grid's holder, so the grid spans its holder (EU5's building_view).
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
      // These two draw nothing of their own: alphamultiply keeps the widget's texture where it is opaque, and mask
      // keeps each overlay after it where it is bright.
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

/** findTypeIcon returns the icon of a widget's type. A type with none of its own is a plain box. */
export function findTypeIcon(type: string): string {
  return TYPE_ICONS[type] ?? "i-lucide-square-dashed";
}

/** splitColor splits a color as a GUI file writes it into four channels. It returns null for any other value. */
export function splitColor(value: string): string[] | null {
  const parts = COLOR.exec(value.trim());
  return parts ? [parts[1], parts[2], parts[3], parts[4] ?? "1"] : null;
}

/** toCss converts a color's channels to CSS. The given opacity replaces the alpha. */
export function toCss([red, green, blue, alpha]: string[], opacity = alpha): string {
  return `color(srgb ${red} ${green} ${blue} / ${opacity})`;
}

/** makeLength makes a row that sets the given properties to the parts of the value, in order. */
function makeLength(...properties: string[]): Apply {
  return (value, style) => {
    const parts = splitValue(value);
    properties.forEach((property, position) => {
      const length = toLength(parts[position] ?? parts[0]);
      if (length) style.setProperty(property, length);
    });
  };
}

/** makeChoice makes a row that applies the CSS of the choice the value names. */
function makeChoice(choices: Record<string, Record<string, string>>): Apply {
  return (value, style) => applyCss(style, choices[value.trim().toLowerCase()] ?? {});
}

/** makeSides makes a row that writes the four sides of the given prefix. */
function makeSides(prefix: string): Apply {
  return (value, style) => {
    const [across, down = across] = splitValue(value);
    for (const side of ["left", "right"]) makeSide(prefix, side)(across, style);
    for (const side of ["top", "bottom"]) makeSide(prefix, side)(down, style);
  };
}

/** makeSide makes a row that writes one side of the given prefix in pixels. The value may be negative. */
function makeSide(prefix: string, side: string): Apply {
  return (value, style) => style.setProperty(`${prefix}-${side}`, `${Number(splitValue(value)[0]) || 0}px`);
}

/** makeAnchor makes a row that writes the fractions its anchor terms name, as the given prefix with -x and -y. */
function makeAnchor(prefix: string): Apply {
  return (value, style) => {
    const terms = value.toLowerCase().split(/[|\s]+/);
    ANCHORS.forEach((anchors, axis) => {
      const fraction = terms.reduce<number | undefined>((latest, term) => anchors[term] ?? latest, undefined);
      if (fraction !== undefined) style.setProperty(`${prefix}-${axis ? "y" : "x"}`, String(fraction));
    });
  };
}

/** makeOffset makes a row that writes the parts of a position as offsets on the given axes. */
function makeOffset(axes: string[]): Apply {
  return (value, style) => {
    const parts = splitValue(value);
    axes.forEach((axis, position) => {
      const part = parts[position] ?? "0";
      style.setProperty(`--offset-${axis}`, part.endsWith("%") ? part : `${Number(part) || 0}px`);
    });
  };
}

/** makePolicy makes a row that writes a layout policy as three custom properties on an axis. */
function makePolicy(axis: string): Apply {
  return (value, style) => {
    const policy = POLICIES[value.trim().toLowerCase()] ?? POLICIES.fixed;
    for (const [property, setting] of Object.entries(policy)) style.setProperty(`--${property}-${axis}`, setting);
  };
}

/** makeStretch makes a row that writes a stretch factor on an axis. */
function makeStretch(axis: string): Apply {
  return (value, style) => style.setProperty(`--stretch-${axis}`, String(Number(value) || 0));
}

/**
 * makeColor makes a row that sets a color, written in the GUI file's way or as a CSS color Go resolved for a binding.
 * With an alpha property, the color is set opaque and its alpha goes to that property.
 */
function makeColor(property: string, alphaProperty = ""): Apply {
  return (value, style) => {
    const channels = splitColor(value);
    if (!channels) {
      if (CSS.supports("color", value)) style.setProperty(property, value);
      return;
    }
    style.setProperty(property, toCss(channels, alphaProperty ? "1" : undefined));
    if (alphaProperty) style.setProperty(alphaProperty, channels[3] ?? "1");
  };
}

/** makeVariable makes a row that writes its value as a custom property. */
function makeVariable(property: string): Apply {
  return (value, style) => style.setProperty(property, splitValue(value).join(" "));
}

/** makeWikiRow makes a table row whose source is the wiki. */
function makeWikiRow(apply: Apply): { apply: Apply; source: string } {
  return { apply, source: WIKI };
}

/** applyCss applies CSS properties to a style. */
export function applyCss(style: CSSStyleDeclaration, css: Record<string, string>): void {
  for (const [property, setting] of Object.entries(css)) style.setProperty(property, setting);
}

/** splitValue splits a value into its parts, a block's braces dropped. */
function splitValue(value: string): string[] {
  return value.replace(/[{}]/g, " ").trim().split(/\s+/);
}

/** toLength converts text to a CSS length. It returns undefined for -1, 0 or a binding. */
function toLength(text: string | undefined): string | undefined {
  if (!text) return undefined;
  if (text.endsWith("%")) return text;
  const number = Number(text);
  return Number.isFinite(number) && number > 0 ? `${number}px` : undefined;
}
