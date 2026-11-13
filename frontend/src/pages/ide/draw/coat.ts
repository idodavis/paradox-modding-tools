/** Draws a coat of arms on a canvas, coloring each layer's texture the way the game's shaders do. */
import Color from "colorjs.io";
import { Range } from "monaco-editor/editor/editor.api";

import type { Coat, CoatStrength, Instance, Layer, Recolor, Shading } from "@services/draw/models";
import type { Mod } from "@services/workspace/models";

import { fileUrl } from "../../../queries";

/** An Rgba is a color as red, green, blue and alpha, each out of one. */
export type Rgba = [number, number, number, number];

/** CHANNELS names the channels of a pixel in the order of its bytes. A region mask uses the first three. */
const CHANNELS = "rgba";
const REGION_CHANNELS = 3;

/** TILE is the size in pixels of a texture's tile in the picker. */
const TILE = 96;

/** images holds the textures loaded, by path. */
const images = new Map<string, Promise<HTMLImageElement>>();

/** latest holds the newest drawing of each canvas, so an older drawing that finishes later is dropped. */
const latest = new WeakMap<HTMLCanvasElement, object>();

/** tiles holds the canvases that already show a tile. */
const tiles = new WeakSet<HTMLCanvasElement>();

/** drawCoat draws a coat onto a canvas at the given size, loading each layer's texture. A newer drawing wins. */
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

/** drawTile draws a texture on a canvas as the game colors it, at tile size, once per canvas. */
export async function drawTile(canvas: unknown, path: string, rule: Recolor | null): Promise<void> {
  if (!(canvas instanceof HTMLCanvasElement) || tiles.has(canvas)) return;
  tiles.add(canvas);
  const texture = await buildTexture(path, rule);
  Object.assign(canvas, { width: TILE, height: TILE });
  if (texture) canvas.getContext("2d")?.drawImage(texture, 0, 0, TILE, TILE);
}

/** isOwn reports whether a layer may be changed: it is writable, in the given file, and in the coat's own block. */
export function isOwn(coat: Coat, layer: Layer, key: string): boolean {
  return !layer.place.readOnly && layer.place.key === key && Range.containsRange(coat.range, layer.range);
}

/** listMods lists the given mods as select items. */
export function listMods(mods: readonly Mod[] | null | undefined): { label: string; value: string }[] {
  return (mods ?? []).map((mod) => ({ label: mod.descriptor.name || mod.id, value: mod.id }));
}

/** findBoxes finds the boxes each layer's instances cover on the coat's canvas. A layer with none covers it all. */
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

/** recolor recolors pixels as the shader does: the first color blends toward each other color by its channel. */
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

/** findSize finds the size of the canvas a coat is drawn on, its longer side being the given size. */
function findSize(coat: Coat, size: number): { width: number; height: number } {
  const scale = size / (Math.max(coat.width, coat.height) || size);
  return { width: Math.round(coat.width * scale) || size, height: Math.round(coat.height * scale) || size };
}

/** buildCoat draws a coat's layers onto a canvas the size of its field, then lays the cloth's shading over them. */
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

/** listPasses lists the layers to draw a coat with, in order. A layer with no instance, such as a pattern, is first. */
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

/** buildLayer builds one layer on a canvas the size of its field. */
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
  for (const instance of instances) {
    context.save();
    if (instance) {
      context.setTransform(buildMatrix(instance, layer.role === "sub", width, height));
      context.drawImage(source, -width / 2, -height / 2, width, height);
    } else {
      context.drawImage(source, 0, 0, width, height);
    }
    context.restore();
  }
  const masks = [
    ...(layer.mask ? [{ path: layer.mask, alpha: buildRegionMask(layer.maskChannels ?? []) }] : []),
    ...(layer.emblem && shading?.grain.strength ? [{ path: shading.path, alpha: buildGrainMask(shading.grain) }] : []),
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

/** buildTexture builds a texture, recolored where the game colors it, at its own size. It is null with no file. */
async function buildTexture(path: string, rule: Recolor | null): Promise<HTMLCanvasElement | HTMLImageElement | null> {
  if (!path) return null;
  const image = await loadImage(path);
  if (!rule) return image;
  const pixels = await loadPixels(path, image.naturalWidth, image.naturalHeight);
  return toCanvas(resolveRecolor(pixels, rule));
}

/** buildMatrix builds the transform that places an instance on a field of the given size. */
function buildMatrix(instance: Instance, isSub: boolean, width: number, height: number): DOMMatrix {
  const [scaleX, scaleY] = instance.scale;
  const [across, down] = instance.position;
  // A sub is placed by its corner, and any other layer by its center.
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

/** resolveRecolor colors a texture's pixels as the game does: the color blends, then the overlay and the alpha. */
function resolveRecolor(pixels: ImageData, rule: Recolor): ImageData {
  const blends = rule.blends ?? [];
  const result = recolor(
    pixels,
    blends.map((blend) => blend.channel),
    [rule.start, ...blends.map((blend) => blend.color)],
  );
  for (let offset = 0; offset < result.data.length; offset += 4) {
    const [red, green, blue, alpha] = result.data.slice(offset, offset + 4);
    const color: Rgba = [red / 255, green / 255, blue / 255, alpha / 255];
    if (rule.overlay.strength) {
      const base = findChannel(pixels, offset, rule.overlay.channel);
      for (let part = 0; part < 3; part++) {
        const target = base < 0.5 ? 2 * base * color[part] : 1 - 2 * (1 - base) * (1 - color[part]);
        color[part] += (target - color[part]) * rule.overlay.strength;
      }
    }
    if (rule.alpha) color[3] = findChannel(pixels, offset, rule.alpha);
    writeColor(result, offset, color);
  }
  return result;
}

/** buildRegionMask builds the alpha a mask keeps of a pattern's pixel. */
function buildRegionMask(channels: readonly string[]): (pixels: ImageData, offset: number) => number {
  return (pixels, offset) =>
    channels.reduce((sum, channel) => {
      const later = CHANNELS.slice(CHANNELS.indexOf(channel) + 1, REGION_CHANNELS).split("");
      const rest = later.reduce((total, other) => total + findChannel(pixels, offset, other), 0);
      return sum + Math.max(0, findChannel(pixels, offset, channel) - rest);
    }, 0);
}

/** buildGrainMask builds the alpha the cloth's grain gives an emblem's pixel. */
function buildGrainMask(grain: CoatStrength): (pixels: ImageData, offset: number) => number {
  return (pixels, offset) => Math.min(1, findChannel(pixels, offset, grain.channel) * grain.strength);
}

/** loadImage loads a texture from the files route. Loading it again gives the same image. */
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

/** loadPixels loads a texture's pixels, stretched to the given size. */
async function loadPixels(path: string, width: number, height: number): Promise<ImageData> {
  const canvas = buildCanvas(width, height);
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return new ImageData(width, height);
  context.drawImage(await loadImage(path), 0, 0, width, height);
  return context.getImageData(0, 0, width, height);
}

/** buildCanvas builds a canvas of the given size. */
function buildCanvas(width: number, height: number): HTMLCanvasElement {
  return Object.assign(document.createElement("canvas"), { width, height });
}

/** toCanvas converts pixels to a canvas of the same size. */
function toCanvas(pixels: ImageData): HTMLCanvasElement {
  const canvas = buildCanvas(pixels.width, pixels.height);
  canvas.getContext("2d")?.putImageData(pixels, 0, 0);
  return canvas;
}

/** findChannel finds one channel of the pixel at the given offset, out of one. */
function findChannel(pixels: ImageData, offset: number, channel: string): number {
  return pixels.data[offset + CHANNELS.indexOf(channel)] / 255;
}

/** writeColor writes four numbers out of one as the pixel at the given offset, each kept between zero and one. */
function writeColor(pixels: ImageData, offset: number, color: Rgba): void {
  color.forEach((value, part) => (pixels.data[offset + part] = Math.round(Math.min(1, Math.max(0, value)) * 255)));
}

/** toRgba converts a CSS color, Go's rgba() or a picked #rrggbb, to four numbers out of one. "" is clear. */
export function toRgba(css: string): Rgba {
  if (!css) return [0, 0, 0, 0];
  const color = new Color(css).to("srgb");
  const [red, green, blue] = color.coords;
  return [red ?? 0, green ?? 0, blue ?? 0, color.alpha];
}
