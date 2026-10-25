/**
 * The coat's canvas: the one pixel computation the page makes. It colors each layer's texture by its channels the
 * way the game's shaders do, with the colors Go resolved, and composites the layers in their instances onto the
 * field.
 */
import { Range } from "monaco-editor";

import type { Coat, Instance, Layer, Recolor, Shading } from "@services/draw/models";
import type { Mod } from "@services/workspace/models";
import type { CoatStrength } from "@internal/game/models";

import { fileUrl } from "../../../queries";

/** A color as red, green, blue and alpha, each out of one. */
export type Rgba = [number, number, number, number];

/** The channels of a pixel in the order its bytes are. A region keeps the channels before the alpha. */
const CHANNELS = "rgba";
const REGION_CHANNELS = 3;

/** The size in pixels of a texture's tile in the picker. */
const TILE = 96;

/** The textures loaded, by path, so a coat drawn again does not fetch them again. */
const images = new Map<string, Promise<HTMLImageElement>>();

/** The latest drawing of each canvas, so a drawing that finishes after a newer one began is dropped. */
const latest = new WeakMap<HTMLCanvasElement, object>();

/** The canvases that show a tile already. */
const tiles = new WeakSet<HTMLCanvasElement>();

/** Draws a coat onto a canvas at the given size, loading each layer's texture from the files route. */
export function drawCoat(canvas: HTMLCanvasElement, coat: Coat, size: number): Promise<void> {
  const { width, height } = findSize(coat, size);
  const drawing = {};
  latest.set(canvas, drawing);
  return buildCoat(coat, coat.shading, width, height).then((field) => {
    if (latest.get(canvas) !== drawing) return;
    canvas.width = width;
    canvas.height = height;
    canvas.getContext("2d")?.drawImage(field, 0, 0);
  });
}

/** Draws a texture on a canvas as the game colors it, at tile size. A canvas that shows a tile is left as it is. */
export async function drawTile(canvas: unknown, path: string, how: Recolor | null): Promise<void> {
  if (!(canvas instanceof HTMLCanvasElement) || tiles.has(canvas)) return;
  tiles.add(canvas);
  const texture = await buildTexture(path, how);
  Object.assign(canvas, { width: TILE, height: TILE });
  if (texture) canvas.getContext("2d")?.drawImage(texture, 0, 0, TILE, TILE);
}

/** Whether a layer may be changed: it is written in the file of the given key, inside the coat's own block. */
export function isOwn(coat: Coat, layer: Layer, key: string): boolean {
  return !layer.place.readOnly && layer.place.key === key && Range.containsRange(coat.range, layer.range);
}

/** Lists the mods a coat can be written into, each with its name. A mod whose folder is missing is left out. */
export function listMods(mods: readonly Mod[] | null | undefined): { label: string; value: string }[] {
  return (mods ?? [])
    .filter((mod) => !mod.broken)
    .map((mod) => ({ label: mod.descriptor.name || mod.id, value: mod.id }));
}

/** Finds the boxes the instances of each layer cover on the coat's canvas, by the layer's place in the coat. */
export function findBoxes(coat: Coat, size: number): Map<number, DOMRect[]> {
  const { width, height } = findSize(coat, size);
  const corners = [new DOMPoint(-1, -1), new DOMPoint(1, -1), new DOMPoint(1, 1), new DOMPoint(-1, 1)];
  return new Map(
    (coat.layers ?? []).map((layer, index) => {
      const boxes = (layer.instances ?? []).map((instance) => {
        const matrix = buildMatrix(instance, layer.role === "sub", width, height);
        const points = corners.map(({ x, y }) =>
          matrix.transformPoint(new DOMPoint((x * width) / 2, (y * height) / 2)),
        );
        const [left, right] = [Math.min(...points.map(({ x }) => x)), Math.max(...points.map(({ x }) => x))];
        const [top, bottom] = [Math.min(...points.map(({ y }) => y)), Math.max(...points.map(({ y }) => y))];
        return new DOMRect(left, top, right - left, bottom - top);
      });
      return [index, boxes.length ? boxes : [new DOMRect(0, 0, width, height)]];
    }),
  );
}

/** Recolors one texture's pixels by its channels and the colors, the way the shader does. */
export function recolor(pixels: ImageData, channels: readonly string[], colors: readonly string[]): ImageData {
  const [start, ...targets] = colors.map(toRgba);
  const result = new ImageData(pixels.width, pixels.height);
  for (let offset = 0; offset < pixels.data.length; offset += 4) {
    const color: Rgba = [...start];
    channels.forEach((channel, position) => {
      const fraction = findChannel(pixels, offset, channel);
      color.forEach((value, part) => (color[part] = value + ((targets[position]?.[part] ?? value) - value) * fraction));
    });
    writeColor(result, offset, color);
  }
  return result;
}

/** Finds the size of the canvas a coat is drawn on: its field scaled so that the longer side is the given size. */
function findSize(coat: Coat, size: number): { width: number; height: number } {
  const scale = size / (Math.max(coat.width, coat.height) || size);
  return { width: Math.round(coat.width * scale) || size, height: Math.round(coat.height * scale) || size };
}

/**
 * Builds a coat's layers in the order it draws them on a canvas the size of its field, the cloth's shading over them.
 */
async function buildCoat(
  coat: Coat,
  shading: Shading | null,
  width: number,
  height: number,
): Promise<HTMLCanvasElement> {
  const field = buildCanvas(width, height);
  const context = field.getContext("2d");
  for (const layer of listPasses(coat)) {
    const layerCanvas = await buildLayer(layer, shading, width, height);
    if (layerCanvas) context?.drawImage(layerCanvas, 0, 0);
  }
  if (context && shading?.overlay.strength) {
    const cloth = await loadPixels(shading.path, width, height);
    const gray = new ImageData(width, height);
    for (let offset = 0; offset < gray.data.length; offset += 4) {
      const value = findChannel(cloth, offset, shading.overlay.channel);
      writeColor(gray, offset, [value, value, value, 1]);
    }
    context.globalCompositeOperation = "hard-light";
    context.globalAlpha = shading.overlay.strength;
    context.drawImage(toCanvas(gray), 0, 0);
  }
  return field;
}

/**
 * Lists the passes that draw a coat, in order: a layer with no instance first, as a pattern draws under all, then each
 * run of instances of one layer that lie next to each other in the order of their ranks, as that layer with only them.
 */
function listPasses(coat: Coat): Layer[] {
  const layers = coat.layers ?? [];
  const spots = layers
    .flatMap((layer) => (layer.instances ?? []).map((instance) => ({ layer, instance })))
    .toSorted((first, second) => first.instance.rank - second.instance.rank);
  const passes = layers
    .filter((layer) => !layer.instances?.length)
    .map((layer) => ({ layer, instances: [] as Instance[] }));
  for (const { layer, instance } of spots) {
    const last = passes.at(-1);
    if (last?.layer === layer && last.instances.length) last.instances.push(instance);
    else passes.push({ layer, instances: [instance] });
  }
  return passes.map(({ layer, instances }) => (instances.length ? { ...layer, instances } : layer));
}

/** Builds one layer on a canvas the size of its field: its texture or sub-coat in each instance, masked. */
async function buildLayer(
  layer: Layer,
  shading: Shading | null,
  width: number,
  height: number,
): Promise<HTMLCanvasElement | null> {
  const source = layer.sub
    ? await buildCoat(layer.sub, shading, width, height)
    : await buildTexture(layer.path, layer.recolor);
  if (!source) return null;
  const canvas = buildCanvas(width, height);
  const context = canvas.getContext("2d");
  if (!context) return null;
  const instances: (Instance | null)[] = layer.instances?.length ? layer.instances : [null];
  for (const instance of instances) applyInstance(context, source, instance, layer.role === "sub");
  const isEmblem = layer.role !== "pattern" && layer.role !== "sub";
  const masks = [
    ...(layer.mask ? [{ path: layer.mask, alpha: buildRegionMask(layer.maskChannels ?? []) }] : []),
    ...(isEmblem && shading?.grain.strength ? [{ path: shading.path, alpha: buildGrainMask(shading.grain) }] : []),
  ];
  for (const mask of masks) {
    const pixels = await loadPixels(mask.path, width, height);
    const alphas = new ImageData(width, height);
    for (let offset = 0; offset < alphas.data.length; offset += 4) {
      writeColor(alphas, offset, [1, 1, 1, mask.alpha(pixels, offset)]);
    }
    context.globalCompositeOperation = "destination-in";
    context.drawImage(toCanvas(alphas), 0, 0);
  }
  return canvas;
}

/** Builds a texture, recolored where the game colors it, at its own size. It is null with no file. */
async function buildTexture(path: string, how: Recolor | null): Promise<HTMLCanvasElement | HTMLImageElement | null> {
  if (!path) return null;
  const image = await loadImage(path);
  if (!how) return image;
  const pixels = await loadPixels(path, image.naturalWidth, image.naturalHeight);
  return toCanvas(resolveRecolor(pixels, how));
}

/** Applies an instance to a canvas: the layer's texture or sub-coat drawn moved, sized, turned and flipped. */
function applyInstance(
  context: CanvasRenderingContext2D,
  source: CanvasImageSource,
  instance: Instance | null,
  isSub: boolean,
): void {
  const { width, height } = context.canvas;
  context.save();
  if (instance) {
    context.setTransform(buildMatrix(instance, isSub, width, height));
    context.drawImage(source, -width / 2, -height / 2, width, height);
  } else {
    context.drawImage(source, 0, 0, width, height);
  }
  context.restore();
}

/** Builds the transform that places an instance on a field of the given size, about the middle of the field. */
function buildMatrix(instance: Instance, isSub: boolean, width: number, height: number): DOMMatrix {
  const [scaleX, scaleY] = instance.scale;
  const [across, down] = instance.position;
  // a sub is placed by its corner, and any other layer by its center
  const [centerX, centerY] = isSub ? [across + Math.abs(scaleX) / 2, down + Math.abs(scaleY) / 2] : [across, down];
  const aspect = width / height;
  return new DOMMatrix()
    .translate(centerX * width, centerY * height)
    .scale(Math.abs(scaleX), Math.abs(scaleY))
    .scale(aspect, 1)
    .rotate(instance.rotation)
    .scale(1 / aspect, 1)
    .scale(Math.sign(scaleX) || 1, Math.sign(scaleY) || 1);
}

/** Resolves how the game colors a texture: the blends of its colors, then the overlay and the alpha. */
function resolveRecolor(pixels: ImageData, how: Recolor): ImageData {
  const blends = how.blends ?? [];
  const result = recolor(
    pixels,
    blends.map((blend) => blend.channel),
    [how.start, ...blends.map((blend) => blend.color)],
  );
  for (let offset = 0; offset < result.data.length; offset += 4) {
    const [red, green, blue, alpha] = result.data.slice(offset, offset + 4);
    const color: Rgba = [red / 255, green / 255, blue / 255, alpha / 255];
    if (how.overlay.strength) {
      const base = findChannel(pixels, offset, how.overlay.channel);
      for (let part = 0; part < 3; part++) {
        const target = base < 0.5 ? 2 * base * color[part] : 1 - 2 * (1 - base) * (1 - color[part]);
        color[part] += (target - color[part]) * how.overlay.strength;
      }
    }
    if (how.alpha) color[3] = findChannel(pixels, offset, how.alpha);
    writeColor(result, offset, color);
  }
  return result;
}

/** Builds the alpha a mask keeps of a pattern's pixel: each kept channel less every channel after it, summed. */
function buildRegionMask(channels: readonly string[]): (pixels: ImageData, offset: number) => number {
  return (pixels, offset) =>
    channels.reduce((sum, channel) => {
      const later = CHANNELS.slice(CHANNELS.indexOf(channel) + 1, REGION_CHANNELS).split("");
      const rest = later.reduce((total, other) => total + findChannel(pixels, offset, other), 0);
      return sum + Math.max(0, findChannel(pixels, offset, channel) - rest);
    }, 0);
}

/** Builds the alpha the cloth's grain gives an emblem's pixel: its channel times its strength, at most one. */
function buildGrainMask(grain: CoatStrength): (pixels: ImageData, offset: number) => number {
  return (pixels, offset) => Math.min(1, findChannel(pixels, offset, grain.channel) * grain.strength);
}

/** Loads a texture from the files route. Loading it again gives the same image. */
function loadImage(path: string): Promise<HTMLImageElement> {
  let image = images.get(path);
  if (!image) {
    const element = new Image();
    element.src = fileUrl(path);
    image = element.decode().then(() => element);
    images.set(path, image);
  }
  return image;
}

/** Loads a texture's pixels, stretched to the given size. */
async function loadPixels(path: string, width: number, height: number): Promise<ImageData> {
  const canvas = buildCanvas(width, height);
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return new ImageData(width, height);
  context.drawImage(await loadImage(path), 0, 0, width, height);
  return context.getImageData(0, 0, width, height);
}

/** Builds a canvas of the given size. */
function buildCanvas(width: number, height: number): HTMLCanvasElement {
  return Object.assign(document.createElement("canvas"), { width, height });
}

/** Converts pixels to a canvas of the same size. */
function toCanvas(pixels: ImageData): HTMLCanvasElement {
  const canvas = buildCanvas(pixels.width, pixels.height);
  canvas.getContext("2d")?.putImageData(pixels, 0, 0);
  return canvas;
}

/** Finds one channel of the pixel at an offset, out of one. */
function findChannel(pixels: ImageData, offset: number, channel: string): number {
  return pixels.data[offset + CHANNELS.indexOf(channel)] / 255;
}

/** Writes four numbers out of one as the pixel at an offset, each kept between zero and one. */
function writeColor(pixels: ImageData, offset: number, color: Rgba): void {
  color.forEach((value, part) => (pixels.data[offset + part] = Math.round(Math.min(1, Math.max(0, value)) * 255)));
}

/** Converts a color Go writes as CSS, rgba(red, green, blue, alpha), to four numbers out of one. "" is clear. */
export function toRgba(css: string): Rgba {
  const [red = 0, green = 0, blue = 0, alpha = 1] = (/\(([^)]*)\)/.exec(css)?.[1] ?? "").split(",").map(Number);
  return css ? [red / 255, green / 255, blue / 255, alpha] : [0, 0, 0, 0];
}

/** Converts a color picked as #rrggbb to four numbers out of one, opaque. */
export function parseHex(hex: string): Rgba {
  const [red, green, blue] = [1, 3, 5].map((start) => parseInt(hex.slice(start, start + 2), 16) / 255);
  return [red, green, blue, 1];
}
