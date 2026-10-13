/**
 * A drawn window as nested elements for the browser to lay out: each element of Go's list is one div, its kind's
 * display and each of its layout words mapped to the CSS word that means the same thing, its textures as layers under
 * it and its text as spans in it. Nothing here measures or places: a word with no row is not carried, and the browser
 * lays the rest out as if it were not written.
 */
import { computed, defineComponent, h, type CSSProperties, type PropType, type VNode } from "vue";
import type { Element as DrawElement, Texture, Text } from "@services/internal/draw";
import { WidgetRole } from "@services/internal/game";

/** An element's layout words as written, by key. */
type Layout = NonNullable<DrawElement["layout"]>;

/** How a kind holds its children: a row or column of flex items, a wrapping flow, a grid of cells, a clipped column
 * (a scroll area) or, every other kind, one cell its children sit in by their anchors. */
type Holding = "row" | "column" | "flow" | "grid" | "scroll" | "cell";
const HOLDINGS: Record<string, Holding> = {
  hbox: "row",
  uberhbox: "row",
  vbox: "column",
  ubervbox: "column",
  flowcontainer: "flow",
  fixedgridbox: "grid",
  dynamicgridbox: "grid",
  scrollarea: "scroll",
  uberscrollarea: "scroll",
  scrollvbox: "scroll",
};
const holdingOf = (kind: string): Holding => HOLDINGS[kind] ?? "cell";

/** The CSS of each holding; a child of a row or column keeps its own size across it unless its policy expands. */
const HOLDING_STYLE: Record<Holding, CSSProperties> = {
  row: { display: "flex", flexDirection: "row", alignItems: "flex-start" },
  column: { display: "flex", flexDirection: "column", alignItems: "flex-start" },
  flow: { display: "flex", flexWrap: "wrap", alignContent: "flex-start", alignItems: "flex-start" },
  grid: { display: "grid", justifyContent: "start", alignContent: "start" },
  scroll: { display: "flex", flexDirection: "column", alignItems: "stretch", overflow: "hidden" },
  // one track the size of the holder, so a percent is of the holder and a child grows no track; an unsized holder's
  // track is its content
  cell: { display: "grid", gridTemplateColumns: "100%", gridTemplateRows: "100%" },
};

/** What a layout policy word means to a flex item: whether it takes extra room, and whether it gives room up. */
const POLICIES: Record<string, { grow: number; shrink: number }> = {
  expanding: { grow: 1, shrink: 1 },
  growing: { grow: 1, shrink: 0 },
  preferred: { grow: 0, shrink: 1 },
  shrinking: { grow: 0, shrink: 1 },
  fixed: { grow: 0, shrink: 0 },
};
const policyOf = (word?: string) => POLICIES[word?.toLowerCase() ?? ""] ?? POLICIES.fixed;

/** Each anchor word's place across (left, hcenter, right) and down (top, vcenter, bottom) a box, as a fraction. */
const ACROSS: Record<string, number> = { left: 0, hcenter: 0.5, center: 0.5, right: 1 };
const DOWN: Record<string, number> = { top: 0, vcenter: 0.5, center: 0.5, bottom: 1 };
/** A fraction's alignment in a cell, and in a line of text. */
const ALIGN = ["start", "center", "end"] as const;
const TEXT_ALIGN = ["left", "center", "right"] as const;

/** How a texture with no frame lies in its element, by its fittype; fill, and a fittype none of these, stretch it. */
const FILL = { size: "100% 100%", position: "0 0" };
const FITS: Record<string, { size: string; position: string }> = {
  centercrop: { size: "cover", position: "center" },
  start: { size: "contain", position: "left top" },
  center: { size: "contain", position: "center" },
  end: { size: "contain", position: "right bottom" },
};

/** A string as a CSS string: quoted, its backslashes and quotes escaped. */
export const quote = (text: string) => `"${text.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;

/** The number a word holds; undefined for none. */
function numberOf(word?: string): number | undefined {
  const number = Number(word);
  return word && Number.isFinite(number) ? number : undefined;
}

/** A length as written: pixels, or a percent; nothing for a negative, which is unset, or for a word. */
function lengthOf(word?: string): string | undefined {
  if (word?.endsWith("%")) return word;
  const number = numberOf(word);
  return number !== undefined && number >= 0 ? `${number}px` : undefined;
}

/** The two words of a pair (size = { 100 20 }): across then down; one word is both. */
function pairOf(value?: string): [string | undefined, string | undefined] {
  const [across, down] = value?.split(/\s+/) ?? [];
  return [across, down ?? across];
}

/** The size written for an axis. */
const sizeOn = (layout: Layout, axis: "horizontal" | "vertical") => pairOf(layout.size)[axis === "horizontal" ? 0 : 1];

/** Whether an element's policy takes room on an axis, so it stretches to what holds it, from whatever size it writes
 * (a window's portrait writes size = { 0 310 } and expands across). */
const grows = (layout: Layout, axis: "horizontal" | "vertical") => policyOf(layout[`layoutpolicy_${axis}`]).grow;

/** The place an anchor's words name, as fractions across and down; null where none is written. */
function anchorOf(words?: string): [number, number] | null {
  if (!words) return null;
  let across = 0;
  let down = 0;
  for (const word of words.toLowerCase().split(/[|\s]+/)) {
    if (word in ACROSS) across = ACROSS[word];
    if (word in DOWN) down = DOWN[word];
  }
  return [across, down];
}

/** The element's own size: size, minimum and maximum as written; an element with a texture and no size is its
 * texture's frame, and with one axis the other follows the frame's ratio; a text that autoresizes is its words' width. */
function sizeStyle(layout: Layout, textures: Texture[]): CSSProperties {
  const [width, height] = pairOf(layout.size);
  const [minWidth, minHeight] = pairOf(layout.minimumsize);
  const [maxWidth, maxHeight] = pairOf(layout.maximumsize);
  const style: CSSProperties = {
    width: lengthOf(width),
    height: lengthOf(height),
    minWidth: lengthOf(layout.min_width ?? minWidth),
    minHeight: lengthOf(layout.min_height ?? minHeight),
    maxWidth: lengthOf(layout.max_width ?? maxWidth),
    maxHeight: lengthOf(layout.max_height ?? maxHeight),
  };
  const [first] = textures;
  if (first) {
    const frameWidth = first.frame.width || first.width;
    const frameHeight = first.frame.height || first.height;
    if (!style.width && !style.height) Object.assign(style, { width: `${frameWidth}px`, height: `${frameHeight}px` });
    else if (!style.width || !style.height) style.aspectRatio = `${frameWidth} / ${frameHeight}`;
  }
  if (layout.autoresize === "yes" && !style.width) style.width = "max-content";
  return style;
}

/** The element's margins, which inset its children, as padding: margin = { x y }, then each side's own key. */
function paddingStyle(layout: Layout): CSSProperties {
  const [across, down] = pairOf(layout.margin);
  const sides = [
    layout.margin_top ?? down,
    layout.margin_right ?? across,
    layout.margin_bottom ?? down,
    layout.margin_left ?? across,
  ];
  if (sides.every((side) => side === undefined)) return {};
  return { padding: sides.map((side) => lengthOf(side) ?? "0").join(" ") };
}

/** A grid's cells: addcolumn and addrow their size, datamodel_wrap how many a row holds (a column where the direction
 * is flipped), else as many as the width holds. */
function gridStyle(layout: Layout): CSSProperties {
  const column = lengthOf(layout.addcolumn);
  const row = lengthOf(layout.addrow);
  const wrap = numberOf(layout.datamodel_wrap);
  const flipped = layout.flipdirection === "yes";
  const along = flipped ? row : column;
  const track = wrap ? `repeat(${wrap}, ${along ?? "max-content"})` : along && `repeat(auto-fill, ${along})`;
  return {
    gridAutoColumns: column ?? "max-content",
    gridAutoRows: row ?? "max-content",
    ...(flipped ? { gridAutoFlow: "column", gridTemplateRows: track } : { gridTemplateColumns: track }),
  };
}

/** The element's place in what holds it: a skin fills it, chrome sits at its edge; in a cell by its anchors, moved by
 * its position; in a row or column by its policies along and across; in a flow or grid in its turn. */
function placeStyle(element: DrawElement, holding: Holding): CSSProperties {
  const layout = element.layout ?? {};
  if (element.role === WidgetRole.RoleSkin) return { position: "absolute", inset: 0 };
  if (element.role === WidgetRole.RoleChrome) {
    return element.key.toLowerCase().includes("vertical")
      ? { position: "absolute", top: 0, right: 0, bottom: 0 }
      : { position: "absolute", left: 0, right: 0, bottom: 0 };
  }
  const anchor = anchorOf(layout.parentanchor) ?? [0, 0];
  const own = anchorOf(layout.widgetanchor) ?? anchor;
  const [positionX, positionY] = pairOf(layout.position);
  const dx = numberOf(layout.position_x ?? positionX) ?? 0;
  const dy = numberOf(layout.position_y ?? positionY) ?? 0;
  switch (holding) {
    case "cell": {
      // a child with no size on an axis and no anchor takes the cell's room there, as a window's main column does;
      // a policy means nothing outside a box
      const stretches = (axis: "horizontal" | "vertical") =>
        lengthOf(sizeOn(layout, axis)) === undefined && !layout.parentanchor;
      return {
        gridArea: "1 / 1",
        justifySelf: stretches("horizontal") ? "stretch" : ALIGN[anchor[0] * 2],
        alignSelf: stretches("vertical") ? "stretch" : ALIGN[anchor[1] * 2],
        translate: `calc(${(anchor[0] - own[0]) * 100}% + ${dx}px) calc(${(anchor[1] - own[1]) * 100}% + ${dy}px)`,
      };
    }
    case "row":
    case "column":
    case "scroll": {
      const along = holding === "row" ? "horizontal" : "vertical";
      const across = holding === "row" ? "vertical" : "horizontal";
      const policy = policyOf(layout[`layoutpolicy_${along}`]);
      const [minWidth, minHeight] = pairOf(layout.minimumsize);
      return {
        flexGrow: numberOf(layout[`layoutstretchfactor_${along}`]) ?? policy.grow,
        flexShrink: policy.shrink,
        // a flex item shrinks no further than its content unless told its minimum
        ...(policy.shrink && holding === "row" ? { minWidth: lengthOf(layout.min_width ?? minWidth) ?? 0 } : {}),
        ...(policy.shrink && holding !== "row" ? { minHeight: lengthOf(layout.min_height ?? minHeight) ?? 0 } : {}),
        // across the box a policy that takes room stretches, from whatever size is written
        alignSelf: grows(layout, across) ? "stretch" : ALIGN[(holding === "row" ? anchor[1] : anchor[0]) * 2],
        ...(grows(layout, across) ? (holding === "row" ? { height: undefined } : { width: undefined }) : {}),
      };
    }
    default:
      return { flex: "none" };
  }
}

/** The CSS of an element: its holding, its size, its margins, its words of its own, its text's font, its place in
 * what holds it, and whether it shows; a hidden element keeps its place unless its holder ignores the invisible. */
function styleOf(element: DrawElement, holding: Holding, holder: Layout, hidden: boolean): CSSProperties {
  const { text } = element;
  const layout = element.layout ?? {};
  const style: CSSProperties = {
    position: "relative",
    ...HOLDING_STYLE[holdingOf(element.kind)],
    ...sizeStyle(layout, element.role === WidgetRole.RoleSkin ? [] : (element.textures ?? [])),
    ...paddingStyle(layout),
    ...(holdingOf(element.kind) === "grid" ? gridStyle(layout) : {}),
    ...placeStyle(element, holding),
  };
  if (layout.spacing) style.gap = lengthOf(layout.spacing);
  if (layout.direction?.toLowerCase() === "vertical" && holdingOf(element.kind) === "flow")
    style.flexDirection = "column";
  if (layout.scissor === "yes") style.overflow = "hidden";
  const alpha = numberOf(layout.alpha);
  if (alpha !== undefined) style.opacity = Math.min(Math.max(alpha, 0), 1);
  if (text) {
    Object.assign(style, {
      fontFamily: quote(text.font),
      fontSize: `${text.size}px`,
      lineHeight: "normal",
      color: text.color || undefined,
      whiteSpace: layout.multiline === "yes" ? "pre-wrap" : "pre",
    });
    if (layout.elide && layout.multiline !== "yes")
      Object.assign(style, { overflow: "hidden", textOverflow: "ellipsis" });
  }
  if (hidden) {
    if (holder.ignoreinvisible === "yes") style.display = "none";
    else style.visibility = "hidden";
  }
  return style;
}

/** How a texture's image lies in its element: a frame fills the element, the image scaled by how many frames it is
 * wide and high and moved to the frame; else by its fit. */
function imageOf(texture: Texture): { size: string; position: string } {
  const { frame } = texture;
  if (frame.width <= 0 || frame.height <= 0) return FITS[texture.fit] ?? FILL;
  const across = texture.width > frame.width ? (frame.x / (texture.width - frame.width)) * 100 : 0;
  const down = texture.height > frame.height ? (frame.y / (texture.height - frame.height)) * 100 : 0;
  return {
    size: `${(texture.width / frame.width) * 100}% ${(texture.height / frame.height) * 100}%`,
    position: `${across}% ${down}%`,
  };
}

/** The CSS of a texture's layer: its frame or fit, its slices, its mirror, tint, blend and the mask that keeps it to
 * another texture's shape. */
function layerStyle(texture: Texture): CSSProperties {
  const image = `url(${quote(texture.url)})`;
  const style: CSSProperties = {};
  if (texture.mirror) style.transform = "scaleX(-1)";
  if (texture.blend && texture.blend !== "normal") style.mixBlendMode = texture.blend as CSSProperties["mixBlendMode"];
  if (texture.border.some((inset) => inset > 0)) {
    const slices = texture.border.map((inset) => `${inset}px`).join(" ");
    return {
      ...style,
      borderStyle: "solid",
      borderWidth: slices,
      borderImage: `${image} ${texture.border.join(" ")} fill / ${slices} ${texture.tiled ? "repeat" : "stretch"}`,
    };
  }
  const placed = imageOf(texture);
  // a tint multiplies the image under a layer of its color, and the image's own alpha keeps the shape
  const tinted = texture.tint !== "";
  const masks = [
    ...(tinted ? [{ image, ...placed }] : []),
    ...(texture.mask ? [{ image: `url(${quote(texture.mask.url)})`, ...imageOf(texture.mask) }] : []),
  ];
  return {
    ...style,
    backgroundImage: tinted ? `linear-gradient(${texture.tint}, ${texture.tint}), ${image}` : image,
    backgroundRepeat: "no-repeat",
    backgroundSize: tinted ? `${FILL.size}, ${placed.size}` : placed.size,
    backgroundPosition: tinted ? `${FILL.position}, ${placed.position}` : placed.position,
    backgroundBlendMode: tinted ? "multiply" : undefined,
    ...(masks.length
      ? {
          maskImage: masks.map((mask) => mask.image).join(", "),
          maskSize: masks.map((mask) => mask.size).join(", "),
          maskPosition: masks.map((mask) => mask.position).join(", "),
          maskRepeat: "no-repeat",
          maskComposite: "intersect",
        }
      : {}),
  };
}

/** A text's runs as spans in its element's cell, aligned as its align words say; an icon is an image a line high. */
function textNode(text: Text, layout: Layout): VNode {
  const align = anchorOf(layout.align) ?? [0, 0];
  const runs = (text.runs ?? []).map((run, i) =>
    run.icon
      ? h("img", { key: i, src: run.icon, alt: "", class: "pmt-icon" })
      : h(
          "span",
          {
            key: i,
            style: {
              color: run.color || undefined,
              fontWeight: run.bold ? 700 : undefined,
              fontStyle: run.italic ? "italic" : undefined,
            },
          },
          run.text,
        ),
  );
  return h(
    "span",
    {
      class: "pmt-text",
      style: {
        gridArea: "1 / 1",
        justifySelf: ALIGN[align[0] * 2],
        alignSelf: ALIGN[align[1] * 2],
        textAlign: TEXT_ALIGN[align[0] * 2],
        minWidth: 0,
        maxWidth: "100%",
      },
    },
    runs,
  );
}

/** The root element and everything under it, nested for the browser to lay out. */
export const DrawnWindow = defineComponent({
  name: "DrawnWindow",
  props: {
    elements: { type: Array as PropType<DrawElement[]>, required: true },
    /** The elements hidden for the session, each with everything under it. */
    hidden: { type: Object as PropType<Set<number>>, required: true },
    selected: { type: Number, required: true },
    /** The UI scale a window is laid out at, a zoom on its root. */
    scale: { type: Number, required: true },
  },
  setup(props) {
    // each element's children in written order, its skins first so they lie under the rest
    const childrenOf = computed(() => {
      const by: number[][] = props.elements.map(() => []);
      props.elements.forEach((element, index) => {
        if (element.parent >= 0) by[element.parent].push(index);
      });
      const isSkin = (index: number) => Number(props.elements[index].role === WidgetRole.RoleSkin);
      return by.map((children) => children.toSorted((a, b) => isSkin(b) - isSkin(a)));
    });

    /** The sibling whose place an element shares, followed to the first of them. */
    function headOf(index: number): number {
      let head = index;
      while (props.elements[head].pair >= 0) head = props.elements[head].pair;
      return head;
    }

    /** A holder's children in the places they take: siblings shown one at a time share a place. */
    function placesOf(children: number[]): number[][] {
      const places: number[][] = [];
      const byHead = new Map<number, number[]>();
      for (const child of children) {
        const shared = byHead.get(headOf(child));
        if (shared) shared.push(child);
        else {
          const place = [child];
          places.push(place);
          byHead.set(child, place);
        }
      }
      return places;
    }

    /** One element as a div: its layers, its text and the places of its children; drawn once a row where it is a
     * data model's item. */
    function nodeOf(index: number, holding: Holding, holder: Layout, key: string): VNode {
      const element = props.elements[index];
      const style = styleOf(element, holding, holder, element.hidden || props.hidden.has(index));
      if (index === 0) style.zoom = props.scale;
      const own = holdingOf(element.kind);
      const layout = element.layout ?? {};
      const children: VNode[] = (element.textures ?? []).map((texture, i) =>
        h("div", { key: `t${i}`, class: "pmt-layer", style: layerStyle(texture) }),
      );
      if (element.text) children.push(textNode(element.text, layout));
      for (const place of placesOf(childrenOf.value[index])) children.push(...placeNodes(place, own, layout));
      return h(
        "div",
        { key, "data-element": index, class: ["pmt-element", { "pmt-selected": index === props.selected }], style },
        children,
      );
    }

    /** The divs of one place: an element's rows, or the elements that share the place, each over the one before, in
     * a cell that takes the place of the first. */
    function placeNodes(place: number[], holding: Holding, holder: Layout): VNode[] {
      const copies = (index: number, inside: Holding) =>
        Array.from({ length: props.elements[index].rows || 1 }, (_, row) =>
          nodeOf(index, inside, holder, `${index}.${row}`),
        );
      if (place.length === 1) return copies(place[0], holding);
      const members = place.flatMap((index) => copies(index, "cell"));
      return [
        h(
          "div",
          {
            key: `p${place[0]}`,
            class: "pmt-place",
            style: { display: "grid", ...placeStyle(props.elements[place[0]], holding) },
          },
          members,
        ),
      ];
    }

    return () => (props.elements.length ? nodeOf(0, "cell", {}, "0") : null);
  },
});
