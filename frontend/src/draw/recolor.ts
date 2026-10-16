/**
 * The recolors: how the page colors a coat of arms' textures as the game's coat shaders do, the one pixel computation
 * it makes, since products of channels and an overlay are no CSS filter. Each image is colored once on a canvas and
 * kept by what colors it.
 */
import { shallowReactive } from "vue";
import type { Texture } from "@services/internal/draw";

type Channel = "r" | "g" | "b" | "a";
type Color = [number, number, number, number];
type Recolor = { start: number; lerps: [Channel, number][]; overlay: [Channel, number]; shape: Channel | "" };

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

/** CK3's cloth (game/gfx/FX/coat_of_arms): a channel overlaid on the coat at a strength, and a channel times a factor
 * over each emblem's alpha. */
const SHADING = { overlay: ["b", 0.2] as [Channel, number], grain: ["g", 2] as [Channel, number] };

/** Whether a positive rotation turns clockwise (observed: k_papal_state's keys). */
export const ROTATION_CLOCKWISE = true;

const CHANNELS: Record<Channel, number> = { r: 0, g: 1, b: 2, a: 3 };

/** What each recolor does to an image's pixels, by its key. */
const PIXELS: Record<string, (data: Uint8ClampedArray, texture: Texture) => void> = {
  ...Object.fromEntries(
    Object.entries(RECOLORS).map(([key, row]) => [
      key,
      (data: Uint8ClampedArray, texture: Texture) => recolor(data, row, texture),
    ]),
  ),
  regions: (data, texture) => eachPixel(data, (pixel) => [1, 1, 1, kept(pixel, texture.regions ?? [])]),
  shading: (data) =>
    eachPixel(data, (pixel) => {
      const gray = pixel[CHANNELS[SHADING.overlay[0]]];
      return [gray, gray, gray, 1];
    }),
  grain: (data) =>
    eachPixel(data, (pixel) => [1, 1, 1, Math.min(1, pixel[CHANNELS[SHADING.grain[0]]] * SHADING.grain[1])]),
};

// each image colored, by what colors it; one asked for and not yet colored is drawn as nothing
const images = shallowReactive(new Map<string, string>());
const asked = new Set<string>();

/** A texture's image as the page draws it: its file where its recolor does nothing to it, else its colored copy, once
 * it is made (undefined until then). */
export function recolored(texture: Texture): string | undefined {
  const apply = PIXELS[texture.recolor];
  if (!apply) return texture.url;
  const key = [texture.url, texture.recolor, texture.colors, texture.regions].join("|");
  if (!asked.has(key)) {
    asked.add(key);
    void paint(texture, apply).then(
      (url) => images.set(key, url),
      () => undefined,
    );
  }
  return images.get(key);
}

/** How an element draws a texture: as a mask of its box (a pattern's regions, the cloth's grain), over its children
 * in an overlay at a strength (the cloth), or as its layer under them (any other). */
export function placeOf(texture: Texture): { place: "layer" | "mask" | "over"; opacity: number } {
  if (texture.recolor === "regions" || texture.recolor === "grain") return { place: "mask", opacity: 1 };
  if (texture.recolor === "shading") return { place: "over", opacity: SHADING.overlay[1] };
  return { place: "layer", opacity: 1 };
}

/** An image colored on a canvas, as a data URL. */
async function paint(texture: Texture, apply: (data: Uint8ClampedArray, texture: Texture) => void): Promise<string> {
  const image = new Image();
  image.src = texture.url;
  await image.decode();
  const canvas = Object.assign(document.createElement("canvas"), {
    width: image.naturalWidth,
    height: image.naturalHeight,
  });
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return texture.url;
  context.drawImage(image, 0, 0);
  const pixels = context.getImageData(0, 0, canvas.width, canvas.height);
  apply(pixels.data, texture);
  context.putImageData(pixels, 0, 0);
  return canvas.toDataURL();
}

/** Writes each pixel of an image as a function of it, both as four numbers out of 1. */
function eachPixel(data: Uint8ClampedArray, color: (pixel: Color) => Color): void {
  for (let i = 0; i < data.length; i += 4) {
    const out = color([data[i] / 255, data[i + 1] / 255, data[i + 2] / 255, data[i + 3] / 255]);
    out.forEach((value, k) => (data[i + k] = Math.round(Math.min(1, Math.max(0, value)) * 255)));
  }
}

/** Colors an image by a row: from its start slot toward each lerp's slot by its channel, then the overlay of its
 * channel by the coat shaders' GetOverlay, which chooses its formula by the overlay's value (CSS's hard-light with the
 * overlay on top), its alpha the shape channel where the row names one. */
function recolor(data: Uint8ClampedArray, row: Recolor, texture: Texture): void {
  const slots = (texture.colors ?? []).map((css) => colorOf(css || FALLBACK));
  const slot = (n: number) => slots[n] ?? colorOf(FALLBACK);
  const [overlayChannel, strength] = row.overlay;
  eachPixel(data, (pixel) => {
    let color = [...slot(row.start)] as Color;
    for (const [channel, n] of row.lerps) color = lerp(color, slot(n), pixel[CHANNELS[channel]]);
    const blend = pixel[CHANNELS[overlayChannel]];
    for (let k = 0; k < 3 && strength; k++) {
      const target = blend < 0.5 ? 2 * blend * color[k] : 1 - 2 * (1 - blend) * (1 - color[k]);
      color[k] += (target - color[k]) * strength;
    }
    if (row.shape) color[3] = pixel[CHANNELS[row.shape]];
    return color;
  });
}

/** The alpha a mask keeps of a pattern's pixel: each kept region's channel less every later channel, summed. */
function kept(pixel: Color, regions: number[]): number {
  const values = REGIONS.map((channel) => pixel[CHANNELS[channel]]);
  const own = values.map((value, i) => Math.max(0, value - values.slice(i + 1).reduce((sum, later) => sum + later, 0)));
  return regions.reduce((sum, region) => sum + (own[region - 1] ?? 0), 0);
}

const lerp = (from: Color, to: Color, t: number): Color => from.map((value, k) => value + (to[k] - value) * t) as Color;

/** A color Go writes as CSS (rgba(r, g, b, a)) as four numbers out of 1. */
function colorOf(css: string): Color {
  const [r = 0, g = 0, b = 0, a = 1] = (/\(([^)]*)\)/.exec(css)?.[1] ?? "").split(",").map(Number);
  return [r / 255, g / 255, b / 255, a];
}
