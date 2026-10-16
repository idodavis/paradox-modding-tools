/**
 * A coat of arms on the page: its picture, one div per layer on its field, each texture colored as the game's coat
 * shaders do (the one pixel computation the page makes, since products of channels and an overlay are no CSS filter,
 * each image colored once on a canvas and kept by what colors it); and what reads and writes a coat's layers.
 */
import { defineComponent, h, shallowReactive, type CSSProperties, type PropType, type VNode } from "vue";
import type { Coat as DrawCoat, Layer } from "@services/internal/draw";
import type { Location } from "@services/internal/lsp";
import { pathKey } from "../composables/useIdeRoots";
import { isReadOnly } from "../editor/models";
import { isAtOrBefore, type DrawElement } from "../stores/draw";
import { quote } from "./picture";

type Channel = "r" | "g" | "b" | "a";
type Color = [number, number, number, number];
type Recolor = { start: number; lerps: [Channel, number][]; overlay: [Channel, number]; shape: Channel | "" };
/** A part of the transform a placed layer is moved, sized and turned by. */
type Placing = "center" | "corner" | "size" | "turn";
/** What a recolor reads of a layer: its file, how it is colored, its slots and the regions a mask keeps. */
type Image = Pick<Layer, "url" | "recolor" | "colors" | "regions">;

/** How each texture kind is colored, as its shader header says: the slot it starts from (0 the fallback), each lerp
 * toward a slot by a channel in order, an overlay of a channel at a strength (0 none), and the channel that is its
 * alpha ("" the lerps' own). The pattern from coat_of_arms_pattern.fxh's Pattern, the colored emblem from
 * coat_of_arms_textured_emblem.fxh under COLORED_EMBLEM; a textured emblem has no row and draws as its pixels are. */
const RECOLORS: Record<string, Recolor> = {
  pattern: {
    start: 0,
    lerps: [
      ["r", 1],
      ["g", 2],
      ["b", 3],
    ],
    overlay: ["b", 0],
    shape: "",
  },
  colored_emblem: {
    start: 1,
    lerps: [
      ["g", 2],
      ["r", 3],
    ],
    overlay: ["b", 1],
    shape: "a",
  },
};

/** The pattern channel each mask region is, region n at n-1: a region keeps its channel less every later one, and an
 * emblem's alpha is the sum of the regions it keeps (the shader's USE_PATTERN_MASK). */
const REGIONS: Channel[] = ["r", "g", "b"];

/** The color a pattern starts from, the shader's FallbackColor: transparent (observed: Vic3's heater-shield subs). */
const FALLBACK = "rgba(0, 0, 0, 0)";

/** CK3's cloth (game/gfx/FX/coat_of_arms): a channel overlaid on each coat and sub at a strength, and a channel times
 * a factor over each emblem's alpha. */
const SHADING = { overlay: ["b", 0.2] as [Channel, number], grain: ["g", 2] as [Channel, number] };

/** Whether a positive rotation turns clockwise (observed: k_papal_state's keys). */
const ROTATION_CLOCKWISE = true;

/** What a placed layer's keys move, size and turn it by, by its type, each with the values it takes where it writes
 * none (the wiki's Coat of arms modding page; observed: an unwritten position is the field's middle, not the wiki's
 * corner). */
const PLACEMENT: Record<string, Record<string, { part: Placing; defaults: number[] }>> = {
  instance: {
    position: { part: "center", defaults: [0.5, 0.5] },
    scale: { part: "size", defaults: [1, 1] },
    rotation: { part: "turn", defaults: [0] },
  },
  sub: { offset: { part: "corner", defaults: [0, 0] }, scale: { part: "size", defaults: [1, 1] } },
};

const CHANNELS: Record<Channel, number> = { r: 0, g: 1, b: 2, a: 3 };

/** What each recolor does to an image's pixels, by its key. */
const PIXELS: Record<string, (data: Uint8ClampedArray, image: Image) => void> = {
  ...Object.fromEntries(
    Object.entries(RECOLORS).map(([key, row]) => [
      key,
      (data: Uint8ClampedArray, image: Image) => recolor(data, row, image),
    ]),
  ),
  regions: (data, image) => eachPixel(data, (pixel) => [1, 1, 1, regionAlpha(pixel, image.regions ?? [])]),
  shading: (data) =>
    eachPixel(data, (pixel) => {
      const gray = pixel[CHANNELS[SHADING.overlay[0]]];
      return [gray, gray, gray, 1];
    }),
  grain: (data) =>
    eachPixel(data, (pixel) => [1, 1, 1, Math.min(1, pixel[CHANNELS[SHADING.grain[0]]] * SHADING.grain[1])]),
};

// each image colored, by what colors it, the oldest let go past KEPT (made again if asked); one asked for and not yet
// colored is drawn as nothing
const images = shallowReactive(new Map<string, string>());
const asked = new Set<string>();

/** The most colored images kept: far more than a picture and a feed in view show at once. */
const KEPT = 600;

/** A coat of arms on its field, cut by a frame's mask: one div per layer, as the game draws it. */
export const CoatPicture = defineComponent({
  props: {
    coat: { type: Object as PropType<DrawCoat>, required: true },
    hidden: { type: Object as PropType<Set<number>>, required: true },
    /** The route of the mask whose alpha cuts the field (the game's frame); "" none. */
    frame: { type: String, default: "" },
  },
  setup(props) {
    return () => {
      const { coat, hidden, frame } = props;
      const layers = coat.layers ?? [];
      const childrenOf = Map.groupBy(
        layers.map((_, index) => index),
        (index) => layers[index].parent,
      );
      const field: CSSProperties = {
        position: "relative",
        width: `${coat.width}px`,
        height: `${coat.height}px`,
        overflow: "hidden",
        "--coat-aspect": coat.height ? coat.width / coat.height : 1,
        ...(frame ? { maskImage: `url(${quote(frame)})`, maskSize: "100% 100%" } : {}),
      };
      return h("div", { style: field }, layers.length ? [layerNode(coat, childrenOf, hidden, 0)] : []);
    };
  },
});

/** A layer as a div the field's size: hidden for the session, placed where it is an instance or a sub, painting its
 * file recolored, masked by its pattern's regions and the cloth's grain where it is an emblem, and the cloth over what
 * it holds where it is a coat or a sub. */
function layerNode(coat: DrawCoat, childrenOf: Map<number, number[]>, hidden: Set<number>, index: number): VNode {
  const layer = (coat.layers ?? [])[index];
  const parts: VNode[] = [];
  if (layer.url) parts.push(h("div", { key: "p", style: { ...FILLING, backgroundImage: imageOf(layer) } }));
  for (const child of childrenOf.get(index) ?? []) parts.push(layerNode(coat, childrenOf, hidden, child));
  const style: CSSProperties = { ...FILLING, ...transformOf(layer) };
  if (hidden.has(index)) style.visibility = "hidden";
  if (layer.type === "emblem") Object.assign(style, maskOf(coat, layer));
  const isShaded = (layer.type === "coat" || layer.type === "sub") && coat.shading;
  if (isShaded) {
    const shading = { url: coat.shading, recolor: "shading", colors: [], regions: [] };
    const over = { mixBlendMode: "hard-light", opacity: SHADING.overlay[1], pointerEvents: "none" } as const;
    parts.push(h("div", { key: "o", style: { ...FILLING, ...over, backgroundImage: imageOf(shading) } }));
    style.isolation = "isolate";
  }
  return h("div", { key: index, "data-element": index, style }, parts);
}

/** A box that fills its holder, drawing an image stretched over it. */
const FILLING: CSSProperties = {
  position: "absolute",
  inset: 0,
  backgroundSize: "100% 100%",
  backgroundRepeat: "no-repeat",
};

/** An image as a CSS url, colored; none until it is made. */
function imageOf(image: Image): string | undefined {
  const source = recolored(image);
  return source ? `url(${quote(source)})` : undefined;
}

/** An emblem's masks, what is kept being what each keeps: its pattern's regions, and the cloth's grain. */
function maskOf(coat: DrawCoat, layer: Layer): CSSProperties {
  const masks = [
    ...(layer.mask ? [{ url: layer.mask, recolor: "regions", colors: [], regions: layer.regions }] : []),
    ...(coat.shading ? [{ url: coat.shading, recolor: "grain", colors: [], regions: [] }] : []),
  ];
  if (!masks.length) return {};
  return {
    maskImage: masks.map((mask) => imageOf(mask) ?? "none").join(", "),
    maskMode: "alpha",
    maskSize: "100% 100%",
    maskRepeat: "no-repeat",
    maskComposite: "intersect",
  };
}

/** The transform a placed layer is drawn by, as the coat shader moves an emblem's quad: a box the field's size turned
 * in the field's own units (its aspect, --coat-aspect, undone around the turn), sized, flipped where a size is
 * negative, and moved to its center, or to its corner plus half its size, each a fraction of the field. */
function transformOf(layer: Layer): CSSProperties {
  const parts = partsOf(layer);
  if (!parts.size) return {};
  const [width, height] = parts.get("size") ?? [1, 1];
  const corner = parts.get("corner");
  const [x, y] = corner
    ? [corner[0] + Math.abs(width) / 2, corner[1] + Math.abs(height) / 2]
    : (parts.get("center") ?? [0.5, 0.5]);
  const turn = (parts.get("turn")?.[0] ?? 0) * (ROTATION_CLOCKWISE ? 1 : -1);
  return {
    transform: [
      `translate(${(x - 0.5) * 100}%, ${(y - 0.5) * 100}%)`,
      `scale(${Math.abs(width)}, ${Math.abs(height)})`,
      `scale(var(--coat-aspect, 1), 1) rotate(${turn}deg) scale(calc(1 / var(--coat-aspect, 1)), 1)`,
      `scale(${Math.sign(width) || 1}, ${Math.sign(height) || 1})`,
    ].join(" "),
  };
}

/** The parts of a placed layer's transform by its type's rows: each its numbers as written, else the row's defaults;
 * none where its type is placed by no rows. */
function partsOf(layer: Layer): Map<Placing, number[]> {
  const parts = new Map<Placing, number[]>();
  for (const [key, { part, defaults }] of Object.entries(PLACEMENT[layer.type] ?? {})) {
    const numbers = (layer.properties?.[key] ?? "").split(/\s+/).filter(Boolean).map(Number);
    parts.set(
      part,
      defaults.map((fallback, index) => (Number.isFinite(numbers[index]) ? numbers[index] : fallback)),
    );
  }
  return parts;
}

/** The values a placed layer's key takes where it writes none; none for a key that places nothing. */
export const placementDefaults = (layerType: string, key: string): number[] =>
  PLACEMENT[layerType]?.[key]?.defaults ?? [];

/** Where a placed layer's center is as fractions of its coat's box, written or not; undefined for one placed by no
 * center. */
export const centerOf = (layer: Layer): number[] | undefined => partsOf(layer).get("center");

/** An image as the page draws it: its file where its recolor does nothing to it, else its colored copy, once it is
 * made (undefined until then), at most a size in pixels on its longer side (a tile's copy is colored small). */
export function recolored(image: Image, size = Infinity): string | undefined {
  const apply = PIXELS[image.recolor];
  if (!apply) return image.url;
  const key = [image.url, image.recolor, image.colors, image.regions, size].join("|");
  if (!asked.has(key)) {
    asked.add(key);
    void paint(image, apply, size).then(
      (url) => keep(key, url),
      () => undefined,
    );
  }
  return images.get(key);
}

/** Keeps a colored image, letting the oldest go past KEPT. */
function keep(key: string, url: string): void {
  images.set(key, url);
  for (const oldest of images.keys()) {
    if (images.size <= KEPT) break;
    images.delete(oldest);
    asked.delete(oldest);
  }
}

/** An image colored on a canvas, at most a size on its longer side, as a data URL. */
async function paint(
  image: Image,
  apply: (data: Uint8ClampedArray, image: Image) => void,
  size: number,
): Promise<string> {
  const source = new Image();
  source.src = image.url;
  await source.decode();
  const scale = Math.min(1, size / Math.max(source.naturalWidth, source.naturalHeight));
  const canvas = Object.assign(document.createElement("canvas"), {
    width: Math.round(source.naturalWidth * scale),
    height: Math.round(source.naturalHeight * scale),
  });
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return image.url;
  context.drawImage(source, 0, 0, canvas.width, canvas.height);
  const pixels = context.getImageData(0, 0, canvas.width, canvas.height);
  apply(pixels.data, image);
  context.putImageData(pixels, 0, 0);
  return canvas.toDataURL();
}

/** Writes each pixel of an image as a function of it, both as four numbers out of 1. */
function eachPixel(data: Uint8ClampedArray, color: (pixel: Color) => Color): void {
  for (let offset = 0; offset < data.length; offset += 4) {
    const pixel: Color = [data[offset] / 255, data[offset + 1] / 255, data[offset + 2] / 255, data[offset + 3] / 255];
    color(pixel).forEach((value, channel) => {
      data[offset + channel] = Math.round(Math.min(1, Math.max(0, value)) * 255);
    });
  }
}

/** Colors an image by a row: from its start slot toward each lerp's slot by its channel, then the overlay of its
 * channel by the coat shaders' GetOverlay, which chooses its formula by the overlay's value (CSS's hard-light with the
 * overlay on top), its alpha the shape channel where the row names one. */
function recolor(data: Uint8ClampedArray, row: Recolor, image: Image): void {
  const slots = (image.colors ?? []).map((css) => colorOf(css || FALLBACK));
  const slotColor = (slot: number) => slots[slot] ?? colorOf(FALLBACK);
  const [overlayChannel, strength] = row.overlay;
  eachPixel(data, (pixel) => {
    let color = [...slotColor(row.start)] as Color;
    for (const [channel, slot] of row.lerps) color = lerp(color, slotColor(slot), pixel[CHANNELS[channel]]);
    const blend = pixel[CHANNELS[overlayChannel]];
    for (let channel = 0; channel < 3 && strength; channel++) {
      const target = blend < 0.5 ? 2 * blend * color[channel] : 1 - 2 * (1 - blend) * (1 - color[channel]);
      color[channel] += (target - color[channel]) * strength;
    }
    if (row.shape) color[3] = pixel[CHANNELS[row.shape]];
    return color;
  });
}

/** The alpha a mask keeps of a pattern's pixel: each kept region's channel less every later channel, summed. */
function regionAlpha(pixel: Color, regions: number[]): number {
  const values = REGIONS.map((channel) => pixel[CHANNELS[channel]]);
  const own = values.map((value, region) =>
    Math.max(0, value - values.slice(region + 1).reduce((sum, later) => sum + later, 0)),
  );
  return regions.reduce((sum, region) => sum + (own[region - 1] ?? 0), 0);
}

/** A color part of the way from one to another. */
const lerp = (start: Color, end: Color, fraction: number): Color =>
  start.map((value, channel) => value + (end[channel] - value) * fraction) as Color;

/** A color Go writes as CSS (rgba(r, g, b, a)) as four numbers out of 1. */
function colorOf(css: string): Color {
  const [red = 0, green = 0, blue = 0, alpha = 1] = (/\(([^)]*)\)/.exec(css)?.[1] ?? "").split(",").map(Number);
  return [red / 255, green / 255, blue / 255, alpha];
}

/** The coat or sub a coat's element is drawn in, the nearest; undefined for an element of no coat. */
export const coatOf = (elements: DrawElement[], index: number): number | undefined =>
  elements[index]?.ancestors?.findLast((one) => elements[one]?.type === "coat" || elements[one]?.type === "sub");

/** A number as vanilla writes a coat's: at most two decimals. */
export const coatNumber = (value: number): string => String(Math.round(value * 100) / 100);

/** Whether a coat edit may write at a location: inside the coat's own entry (the first element, written as the
 * entry's id, so never an alias's target) in the open file, which is no game file; never a parent coat's statements,
 * here or in another file. */
export function isEditable(path: string, entry: string, elements: DrawElement[], location: Location): boolean {
  const coat = elements[0];
  if (!coat || coat.key !== entry || isReadOnly(path)) return false;
  const isHere = (one: Location) => pathKey(one.path) === pathKey(path);
  const [own, inner] = [coat.location.range, location.range];
  return (
    isHere(coat.location) &&
    isHere(location) &&
    isAtOrBefore(own.start, inner.start) &&
    isAtOrBefore(inner.end, own.end)
  );
}
