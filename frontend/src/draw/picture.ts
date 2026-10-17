/**
 * The picture: what a holding and a property mean in CSS, as tables, and the DOM built from them, one div per drawn
 * widget, which the browser lays out and paints.
 */
import { defineComponent, h, type CSSProperties, type PropType, type VNode } from "vue";
import type { List as DrawList, Text, Texture, Widget } from "@services/internal/draw";
import { Holding, WidgetRole } from "@services/internal/game";

type Axis = 0 | 1;
type Properties = NonNullable<Widget["properties"]>;

/** What a property means in CSS, by its shape; a rule with nothing in it is a property known and not drawn. */
type Rule =
  | { lengths: [keyof CSSProperties, keyof CSSProperties] }
  | { length: keyof CSSProperties }
  | { number: keyof CSSProperties }
  | { count: keyof CSSProperties }
  | { words: Record<string, CSSProperties> }
  | { flag: CSSProperties }
  | { text: Record<string, CSSProperties> }
  | { anchor: "holder" | "own" | "offset" | "offsetX" | "offsetY" | "text" }
  | { policy: Axis; stretch?: true }
  | { fits: Axis[] | Record<string, Axis[]> }
  | { bar: Axis }
  | Record<string, never>;

/** What a holding means in CSS. The wiki: a box centers its children, the room nobody takes spread before, between
 * and after them; a flow resizes to fit them. */
const HOLDS: Partial<Record<Holding, CSSProperties>> = {
  [Holding.HoldsRow]: { display: "flex", flexDirection: "row", alignItems: "center", justifyContent: "space-evenly" },
  [Holding.HoldsColumn]: {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    justifyContent: "space-evenly",
  },
  [Holding.HoldsFlow]: { display: "flex", alignItems: "start" },
  [Holding.HoldsGrid]: { display: "grid", gridAutoFlow: "row", justifyContent: "start", alignContent: "start" },
  [Holding.HoldsScroll]: {
    display: "grid",
    overflow: "hidden",
    contain: "size",
    justifyItems: "start",
    alignItems: "start",
  },
  // observed: a child anchored to a cell's edge sits at that edge even when larger than the cell, so the one track is
  // the cell itself; a cell with no size still grows to its children
  [Holding.HoldsCell]: { display: "grid", gridTemplateColumns: "100%", gridTemplateRows: "100%" },
};

/** The wiki's five layout policies. */
const POLICIES: Record<string, { grow: boolean; shrink: boolean; expand: boolean }> = {
  fixed: { grow: false, shrink: false, expand: false },
  growing: { grow: true, shrink: false, expand: false },
  shrinking: { grow: false, shrink: true, expand: false },
  preferred: { grow: true, shrink: true, expand: false },
  expanding: { grow: true, shrink: false, expand: true },
};

/** Each anchor or align word's place across and down, as a fraction. */
const ANCHORS: Record<string, number>[] = [
  { left: 0, hcenter: 0.5, center: 0.5, right: 1 },
  { top: 0, vcenter: 0.5, center: 0.5, bottom: 1 },
];

/** How a texture lies by its fittype; any other fills its box. */
const FITS: Record<string, { size: string; position: string }> = {
  centercrop: { size: "cover", position: "center" },
  start: { size: "contain", position: "left top" },
  center: { size: "contain", position: "center" },
  end: { size: "contain", position: "right bottom" },
};
const FILL = { size: "100% 100%", position: "0 0" };

const ELLIPSIS: CSSProperties = { overflow: "hidden", textOverflow: "ellipsis", maxWidth: "100%" };

/** What each property means in CSS, applied in this order, a later row over an earlier. The wiki's Interface page,
 * unless the row says observed. */
const PROPERTIES: Record<string, Rule> = {
  size: { lengths: ["width", "height"] },
  minimumsize: { lengths: ["minWidth", "minHeight"] },
  maximumsize: { lengths: ["maxWidth", "maxHeight"] },
  min_width: { length: "minWidth" },
  min_height: { length: "minHeight" },
  max_width: { length: "maxWidth" },
  max_height: { length: "maxHeight" },
  margin: { lengths: ["paddingInline", "paddingBlock"] },
  margin_left: { length: "paddingLeft" },
  margin_right: { length: "paddingRight" },
  margin_top: { length: "paddingTop" },
  margin_bottom: { length: "paddingBottom" },
  spacing: { length: "gap" },
  alpha: { number: "opacity" },
  parentanchor: { anchor: "holder" },
  widgetanchor: { anchor: "own" },
  position: { anchor: "offset" },
  position_x: { anchor: "offsetX" },
  position_y: { anchor: "offsetY" },
  layoutpolicy_horizontal: { policy: 0 },
  layoutpolicy_vertical: { policy: 1 },
  layoutstretchfactor_horizontal: { policy: 0, stretch: true },
  layoutstretchfactor_vertical: { policy: 1, stretch: true },
  ignore_layout: { words: { yes: { position: "absolute" } } },
  set_parent_size_to_minimum: { fits: [0, 1] },
  set_parent_dimension_to_minimum: { fits: { width: [0], height: [1] } },
  resizeparent: { fits: [0, 1] },
  righttoleft: { words: { yes: { flexDirection: "row-reverse" } } },
  bottomtotop: { words: { yes: { flexDirection: "column-reverse" } } },
  direction: { words: { vertical: { flexDirection: "column" } } },
  addcolumn: { length: "gridAutoColumns" },
  addrow: { length: "gridAutoRows" },
  datamodel_wrap: { flag: { flexWrap: "wrap" } },
  maxhorizontalslots: { count: "gridTemplateColumns" },
  maxverticalslots: { count: "gridTemplateRows" },
  flipdirection: { words: { yes: { gridAutoFlow: "column" } } },
  autoresizescrollarea: { words: { yes: { overflow: "visible", contain: "none" } } },
  scrollbarpolicy_horizontal: { bar: 0 },
  scrollbarpolicy_vertical: { bar: 1 },
  align: { anchor: "text" },
  autoresize: { words: { yes: { width: "max-content", height: "max-content" } } },
  multiline: { text: { yes: { whiteSpace: "pre-wrap" } } },
  elide: { text: { left: ELLIPSIS, right: ELLIPSIS } },
  setitemsizefromcell: {},
  wrap_count: {},
  alwaystransparent: {},
};

/** The picture of a list on a screen: its holders around its root, each a div the browser lays out. */
export const Picture = defineComponent({
  props: {
    list: { type: Object as PropType<DrawList>, required: true },
    hidden: { type: Object as PropType<Set<number>>, required: true },
    rows: { type: Number, required: true },
    width: { type: Number, required: true },
    height: { type: Number, required: true },
  },
  setup(props) {
    return () => {
      const { list, hidden, rows } = props;
      const elements = list.elements ?? [];
      const childrenOf = Map.groupBy(elements.keys(), (index) => elements[index].parent);
      const screen: Widget = {
        ...elements[0],
        key: "screen",
        holds: Holding.HoldsCell,
        properties: {},
        dropsHidden: false,
      };
      const holders = [screen, ...(list.holders ?? [])];
      let node = nodeOf(list, childrenOf, hidden, rows, 0, holders.at(-1) ?? screen, Holding.HoldsCell, "0");
      for (let i = holders.length - 1; i > 0; i--) {
        const holder = holders[i];
        node = h("div", { key: `h${i}`, class: "pmt-holder", style: styleOf(holder, holders[i - 1], [], [], false) }, [
          node,
        ]);
      }
      return h("div", { style: { position: "relative", width: `${props.width}px`, height: `${props.height}px` } }, [
        node,
      ]);
    };
  },
});

/** One element's div, in the game's paint order: its skins, its own textures, its placeholder, its text, its
 * children in written order, its bars last; a data model's item once a row. */
function nodeOf(
  list: DrawList,
  childrenOf: Map<number, number[]>,
  hidden: Set<number>,
  rows: number,
  index: number,
  holder: Widget,
  holding: Holding,
  key: string,
): VNode {
  const elements = list.elements ?? [];
  const element = elements[index];
  const kids = (childrenOf.get(index) ?? []).map((i) => elements[i]);
  const parts: VNode[] = [];
  const byRole = (role: WidgetRole) => (childrenOf.get(index) ?? []).filter((i) => elements[i].role === role);
  for (const i of byRole(WidgetRole.RoleSkin))
    parts.push(nodeOf(list, childrenOf, hidden, rows, i, element, Holding.HoldsCell, `${key}.${i}`));
  if (element.textures?.length) parts.push(texturesNode(list.textures ?? [], element.textures, `${key}t`));
  if (element.textureBinding)
    parts.push(h("div", { key: `${key}p`, class: "pmt-placeholder absolute inset-0 grid place-items-center" }));
  if (element.text) parts.push(textNode(element, element.text, `${key}x`));
  for (const i of byRole(WidgetRole.$zero)) {
    for (let row = 0; row < (elements[i].isItem ? rows : 1); row++)
      parts.push(nodeOf(list, childrenOf, hidden, rows, i, element, element.holds, `${key}.${i}.${row}`));
  }
  for (const i of byRole(WidgetRole.RoleLabel))
    parts.push(nodeOf(list, childrenOf, hidden, rows, i, element, Holding.HoldsCell, `${key}.${i}`));
  for (const i of byRole(WidgetRole.RoleChrome)) {
    const axis: Axis = elements[i].key.toLowerCase().includes("vertical") ? 1 : 0;
    const policy = element.properties?.[`scrollbarpolicy_${axis ? "vertical" : "horizontal"}`]?.toLowerCase();
    if (policy !== "always_off")
      parts.push(nodeOf(list, childrenOf, hidden, rows, i, element, Holding.HoldsCell, `${key}.${i}`));
  }
  const siblings = (childrenOf.get(element.parent) ?? []).map((i) => elements[i]);
  const isHidden = element.isHidden || hidden.has(index);
  const texture = (list.textures ?? [])[element.textures?.[0] ?? -1];
  const style = styleOf(element, { ...holder, holds: holding }, siblings, kids, isHidden, texture);
  return h("div", { key, "data-element": index, style }, parts);
}

/** An element's CSS: its holding, each property through its row in the table's order, its texture's size where none
 * is written, a child that sizes it, how its role sits in its holder, and whether it is hidden, taking no room where
 * the holder drops hidden children. */
function styleOf(
  element: Widget,
  holder: Widget,
  siblings: Widget[],
  children: Widget[],
  isHidden: boolean,
  texture?: Texture,
): CSSProperties {
  const properties = element.properties ?? {};
  const holds = holder.holds ?? Holding.HoldsCell;
  const style: CSSProperties = {
    ...HOLDS[element.holds ?? Holding.HoldsCell],
    position: "relative",
    boxSizing: "border-box",
    minWidth: 0,
    minHeight: 0,
  };
  for (const [key, rule] of Object.entries(PROPERTIES)) {
    const value = properties[key];
    if (value === undefined) continue;
    if ("anchor" in rule || "policy" in rule) continue;
    Object.assign(style, plainStyle(rule, value));
  }
  // the wiki: a widget with a texture and no size is the texture's size
  if (texture && element.role !== WidgetRole.RoleSkin) {
    style.width ??= `${texture.width / (texture.density || 1)}px`;
    style.height ??= `${texture.height / (texture.density || 1)}px`;
  }
  const isPlaced = !element.fills && element.role !== WidgetRole.RoleSkin && element.role !== WidgetRole.RoleChrome;
  if (isPlaced) Object.assign(style, anchorStyle(properties, holds));
  Object.assign(style, policyStyle(properties, holds, siblings), fitStyle(children));
  // the wiki: a box in a fixed-size parent expands to the parent's size unless anchored
  const isBox = element.holds === Holding.HoldsRow || element.holds === Holding.HoldsColumn;
  if (holds === Holding.HoldsCell && isBox && properties.parentanchor === undefined)
    Object.assign(style, { width: "100%", height: "100%" });
  if (element.clips) style.overflow = "hidden";
  if (element.fills) Object.assign(style, { position: "absolute", inset: 0, width: "auto", height: "auto" });
  if (element.role === WidgetRole.RoleSkin) Object.assign(style, skinStyle(properties));
  if (element.role === WidgetRole.RoleChrome) Object.assign(style, barStyle(element));
  if (holds === Holding.HoldsScroll)
    Object.assign(style, { position: "relative", minWidth: "100%", minHeight: "100%" });
  if (isHidden) Object.assign(style, holder.dropsHidden ? { display: "none" } : { visibility: "hidden" });
  return style;
}

/** A plain row's CSS for a value: a pair of lengths, one length, a number, a count of cells, or a word's CSS. */
function plainStyle(rule: Rule, value: string): CSSProperties {
  const [across, down] = value.split(/\s+/);
  if ("lengths" in rule) {
    const style: CSSProperties = {};
    const first = lengthOf(across);
    const second = lengthOf(down ?? across);
    if (first !== undefined) Object.assign(style, { [rule.lengths[0]]: first });
    if (second !== undefined) Object.assign(style, { [rule.lengths[1]]: second });
    return style;
  }
  if ("length" in rule) {
    const length = lengthOf(across);
    return length === undefined ? {} : { [rule.length]: length };
  }
  if ("number" in rule) return Number.isFinite(Number(across)) ? { [rule.number]: Number(across) } : {};
  if ("count" in rule) return Number(across) > 0 ? { [rule.count]: `repeat(${Number(across)}, max-content)` } : {};
  if ("flag" in rule) return rule.flag;
  if ("words" in rule) return rule.words[across.toLowerCase()] ?? {};
  return {};
}

/** A length as CSS: a number in px, a percent as itself; none for -1, 0 or a binding. */
function lengthOf(value?: string): string | undefined {
  if (!value) return undefined;
  if (value.endsWith("%")) return value;
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? `${number}px` : undefined;
}

/** The fraction anchor or align words name on an axis; undefined for none. */
export function anchorOf(value: string | undefined, axis: 0 | 1): number | undefined {
  let fraction: number | undefined;
  for (const choice of value?.toLowerCase().split(/[|\s]+/) ?? []) fraction = ANCHORS[axis][choice] ?? fraction;
  return fraction;
}

/** Where an element sits by its anchors: in a cell, as one grid item among its siblings so the cell can fit them,
 * aligned at its parentanchor's point and shifted to its widgetanchor's and by its position (one that ignores the
 * layout sits over the cell instead); in a box, the across-axis word aligns it. */
function anchorStyle(properties: Properties, holds: Holding): CSSProperties {
  const [x, y] = properties.position?.split(/\s+/) ?? [];
  const offset = [properties.position_x ?? x ?? "0", properties.position_y ?? y ?? "0"].map((word) =>
    word.endsWith("%") ? word : `${Number(word) || 0}px`,
  );
  const holder = [anchorOf(properties.parentanchor, 0) ?? 0, anchorOf(properties.parentanchor, 1) ?? 0];
  const own = [
    properties.widgetanchor ? (anchorOf(properties.widgetanchor, 0) ?? 0) : holder[0],
    properties.widgetanchor ? (anchorOf(properties.widgetanchor, 1) ?? 0) : holder[1],
  ];
  const place = ["start", "center", "end"] as const;
  if (holds === Holding.HoldsRow || holds === Holding.HoldsColumn) {
    const across: Axis = holds === Holding.HoldsRow ? 1 : 0;
    const fraction = anchorOf(properties.parentanchor, across);
    return fraction === undefined ? {} : { alignSelf: place[fraction * 2] };
  }
  if (holds !== Holding.HoldsCell) return {};
  if (properties.ignore_layout?.toLowerCase() === "yes")
    return {
      position: "absolute",
      left: `calc(${holder[0] * 100}% + ${offset[0]})`,
      top: `calc(${holder[1] * 100}% + ${offset[1]})`,
      translate: `${-own[0] * 100}% ${-own[1] * 100}%`,
    };
  return {
    gridArea: "1 / 1",
    justifySelf: place[holder[0] * 2],
    alignSelf: place[holder[1] * 2],
    translate: `calc(${(holder[0] - own[0]) * 100}% + ${offset[0]}) calc(${(holder[1] - own[1]) * 100}% + ${offset[1]})`,
  };
}

/** How an element takes room by its policies: along a box, it grows for expanding, and for growing where no sibling
 * expands, shrinks for shrinking and preferred, a stretch factor its weight; across a box it stretches for both; in
 * a cell it fills the axis. */
function policyStyle(properties: Properties, holds: Holding, siblings: Widget[]): CSSProperties {
  const policy = (one: Properties, axis: Axis) =>
    POLICIES[(one[axis ? "layoutpolicy_vertical" : "layoutpolicy_horizontal"] ?? "fixed").toLowerCase()] ??
    POLICIES.fixed;
  const stretch = (axis: Axis) =>
    Number(properties[axis ? "layoutstretchfactor_vertical" : "layoutstretchfactor_horizontal"]) || 0;
  if (holds === Holding.HoldsCell) {
    const style: CSSProperties = {};
    if (policy(properties, 0).grow) style.width = "100%";
    if (policy(properties, 1).grow) style.height = "100%";
    return style;
  }
  if (holds === Holding.HoldsFlow) return { flexShrink: 0 };
  if (holds !== Holding.HoldsRow && holds !== Holding.HoldsColumn) return {};
  const along: Axis = holds === Holding.HoldsRow ? 0 : 1;
  const across: Axis = holds === Holding.HoldsRow ? 1 : 0;
  const isShared = siblings.some((one) => policy(one.properties ?? {}, along).expand);
  const own = policy(properties, along);
  const grows = own.expand || (own.grow && !isShared);
  return {
    flexGrow: stretch(along) || (grows ? 1 : 0),
    flexShrink: own.shrink ? 1 : 0,
    alignSelf: policy(properties, across).grow ? "stretch" : undefined,
    ...(policy(properties, across).grow ? { [across ? "height" : "width"]: "auto" } : {}),
  };
}

/** A holder's size follows its children where one writes a fits row, on the axes the row names. */
function fitStyle(children: Widget[]): CSSProperties {
  const style: CSSProperties = {};
  for (const child of children) {
    for (const [key, rule] of Object.entries(PROPERTIES)) {
      const value = child.properties?.[key]?.toLowerCase();
      if (!("fits" in rule) || value === undefined) continue;
      const axes = Array.isArray(rule.fits) ? (value === "yes" ? rule.fits : []) : (rule.fits[value] ?? []);
      for (const axis of axes) style[axis ? "height" : "width"] = "fit-content";
    }
  }
  return style;
}

/** A skin fills its widget, inset by its margin, which the wiki lets be negative. */
function skinStyle(properties: Properties): CSSProperties {
  const [x, y] = (properties.margin ?? "0").split(/\s+/);
  const side = (own: string | undefined, both: string | undefined) => `${Number(own ?? both ?? 0) || 0}px`;
  const inset = [
    side(properties.margin_top, y ?? x),
    side(properties.margin_right, x),
    side(properties.margin_bottom, y ?? x),
    side(properties.margin_left, x),
  ];
  return { position: "absolute", inset: inset.join(" "), padding: 0, width: "auto", height: "auto" };
}

/** A scroll bar sits at the edge its key names, vertical at the right at full height, horizontal at the bottom at
 * full width, as wide or tall as its size. */
function barStyle(element: Widget): CSSProperties {
  const [across, down] = (element.properties?.size ?? "").split(/\s+/);
  return element.key.toLowerCase().includes("vertical")
    ? {
        position: "absolute",
        top: 0,
        bottom: 0,
        right: 0,
        left: "auto",
        translate: "0 0",
        width: lengthOf(across) ?? "max-content",
        height: "auto",
      }
    : {
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 0,
        top: "auto",
        translate: "0 0",
        height: lengthOf(down) ?? "max-content",
        width: "auto",
      };
}

/** A string as a CSS string, quoted and escaped. */
export const quote = (text: string) => `"${text.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;

/** A widget's textures as one group in its own texture's blend, each overlay blending with that texture alone; the
 * own texture's alphamultiplies mask the group, a sliced one by its slices. */
function texturesNode(textures: Texture[], indexes: number[], key: string): VNode {
  const [own, ...overlays] = indexes.map((index) => textures[index]);
  const alphas = masksOf(textures, own);
  const sliced = alphas.find(isSliced);
  const style: CSSProperties = {
    position: "absolute",
    inset: 0,
    mixBlendMode: blendOf(own),
    isolation: overlays.some(blendOf) ? "isolate" : undefined,
    ...maskStyle(alphas.filter((mask) => mask !== sliced).map((mask) => [mask, "alpha"])),
    WebkitMaskBoxImage: sliced && sliceOf(sliced),
  };
  return h("div", { key, style }, [
    layerNode(textures, own, undefined, `${key}o`),
    ...overlays.map((overlay, i) => layerNode(textures, overlay, own, `${key}${i}`)),
  ]);
}

/** A texture's blend as CSS, none for the normal one. */
const blendOf = (texture: Texture) =>
  texture.blend && texture.blend !== "normal" ? (texture.blend as CSSProperties["mixBlendMode"]) : undefined;

/** Whether a texture is a 9-slice. */
const isSliced = (texture: Texture) => texture.border.some((inset) => inset > 0);

/** The textures that mask a texture, each the one its mask names in turn. */
function masksOf(textures: Texture[], texture: Texture): Texture[] {
  const masks: Texture[] = [];
  for (let mask = textures[texture.mask]; mask; mask = textures[mask.mask]) masks.push(mask);
  return masks;
}

/** How a texture lies as a background: tiled at its size on the screen, else by its fit. */
function backgroundOf(texture: Texture): { size: string; position: string; repeat: string } {
  const density = texture.density || 1;
  if (texture.isTiled)
    return { size: `${texture.width / density}px ${texture.height / density}px`, position: "0 0", repeat: "repeat" };
  return { ...(FITS[texture.fit] ?? FILL), repeat: "no-repeat" };
}

/** Mask layers as CSS, each by a channel, what is kept being what every one keeps. */
function maskStyle(masks: [Texture, string][]): CSSProperties {
  if (!masks.length) return {};
  const layers = masks.map(([texture, mode]) => ({
    url: `url(${quote(texture.url)})`,
    mode,
    ...backgroundOf(texture),
  }));
  return {
    maskImage: layers.map((one) => one.url).join(", "),
    maskMode: layers.map((one) => one.mode).join(", "),
    maskSize: layers.map((one) => one.size).join(", "),
    maskPosition: layers.map((one) => one.position).join(", "),
    maskRepeat: layers.map((one) => one.repeat).join(", "),
    maskComposite: "intersect",
  };
}

/** A 9-slice as CSS, as the sprite shader cuts it: its border drawn at its width in the screen's pixels and cut from
 * the image at that width times the density, whole pixels, short of the image's middle; the middle tiled or
 * stretched. The browser reduces widths that would overlap. */
function sliceOf(texture: Texture): string {
  const sides = [texture.height, texture.width, texture.height, texture.width];
  const cut = texture.border.map((width, side) =>
    Math.min(Math.round(width * (texture.density || 1)), Math.floor((sides[side] - 1) / 2)),
  );
  const widths = texture.border.map((width) => `${width}px`).join(" ");
  const middle = texture.isTiled ? "repeat" : "stretch";
  return `url(${quote(texture.url)}) ${cut.join(" ")} fill / ${widths} / 0 ${middle}`;
}

/** A texture as one layer: its image, with its fit or tiling, slices and mirror, and under a tint a second div in
 * the image's shape multiplying it, as the shader multiplies a sample by its color, the pair's opacity the tint's
 * alpha; an overlay (on `base`) at its alpha and blend, kept to the own texture's shape and to the brightness of
 * its masks. */
function layerNode(textures: Texture[], texture: Texture, base: Texture | undefined, key: string): VNode {
  const layer: CSSProperties = {
    position: "absolute",
    inset: 0,
    opacity: (base ? texture.alpha : 1) * (texture.tint ? texture.tintAlpha : 1),
    isolation: texture.tint ? "isolate" : undefined,
    transform: texture.isMirrored ? "scaleX(-1)" : undefined,
  };
  if (base) layer.mixBlendMode = blendOf(texture);
  if (base && isSliced(base)) layer.WebkitMaskBoxImage = sliceOf(base);
  if (base && !isSliced(base))
    Object.assign(
      layer,
      maskStyle([[base, "alpha"], ...masksOf(textures, texture).map((mask): [Texture, string] => [mask, "luminance"])]),
    );
  const image: CSSProperties = { position: "absolute", inset: 0 };
  const tint: CSSProperties = {
    position: "absolute",
    inset: 0,
    backgroundColor: texture.tint,
    mixBlendMode: "multiply",
  };
  if (isSliced(texture)) {
    const slices = sliceOf(texture);
    Object.assign(image, { borderStyle: "solid", borderWidth: 0, borderImage: slices });
    tint.WebkitMaskBoxImage = slices;
  } else {
    const background = backgroundOf(texture);
    Object.assign(image, {
      backgroundImage: `url(${quote(texture.url)})`,
      backgroundRepeat: background.repeat,
      backgroundSize: background.size,
      backgroundPosition: background.position,
    });
    Object.assign(tint, maskStyle([[texture, "alpha"]]));
  }
  return h("div", { key, style: layer }, [
    h("div", { key: "i", style: image }),
    ...(texture.tint ? [h("div", { key: "t", style: tint })] : []),
  ]);
}

/** A text in its widget's cell: its runs in the text's font, placed by its align words (centered where the type
 * centers text), on one line unless multiline, cut with "…" where it elides or holds a binding. */
function textNode(element: Widget, text: Text, key: string): VNode {
  const properties = element.properties ?? {};
  const start = element.centersText ? 0.5 : 0;
  const across = anchorOf(properties.align, 0) ?? start;
  const down = anchorOf(properties.align, 1) ?? start;
  const place = ["start", "center", "end"] as const;
  const style: CSSProperties = {
    fontFamily: quote(text.font),
    fontSize: `${text.size}px`,
    lineHeight: "normal",
    color: text.color || undefined,
    whiteSpace: "pre",
    position: "relative",
    gridArea: "1 / 1",
    placeSelf: `${place[down * 2]} ${place[across * 2]}`,
    textAlign: (["left", "center", "right"] as const)[across * 2],
    minWidth: 0,
    ...(text.runs?.some((run) => run.call) ? ELLIPSIS : {}),
  };
  for (const [property, rule] of Object.entries(PROPERTIES)) {
    const value = properties[property]?.toLowerCase();
    if ("text" in rule && value !== undefined) Object.assign(style, rule.text[value] ?? {});
  }
  return h("span", { key, style }, runNodes(text));
}

/** A text's runs as spans, an icon a line high, a binding in the code's binding color. */
function runNodes(text: Text): VNode[] {
  return (text.runs ?? []).map((run, i) =>
    run.icon
      ? h("img", {
          key: i,
          src: run.icon,
          alt: "",
          style: { display: "inline-block", verticalAlign: "middle", height: "1em" },
        })
      : h(
          "span",
          {
            key: i,
            style: {
              color: run.call ? "var(--syn-scope)" : run.color || undefined,
              fontWeight: run.isBold ? 700 : undefined,
              fontStyle: run.isItalic ? "italic" : undefined,
            },
          },
          run.text,
        ),
  );
}
