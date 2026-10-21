/**
 * The coat's canvas: the one pixel computation the page makes. It colors each layer's texture by its channels the
 * way the game's shaders do, with the colors Go resolved, and composites the layers in their instances onto the
 * field.
 */
import type { Coat } from "@services/draw/models";

/** Draws a coat onto a canvas at the given size, loading each layer's texture from the files route. */
export function drawCoat(canvas: HTMLCanvasElement, coat: Coat, size: number): Promise<void> {
  throw new Error("stub", { cause: [canvas, coat, size] });
}

/** Recolors one texture's pixels by its channels and the colors, the way the shader does. */
export function recolor(pixels: ImageData, channels: readonly string[], colors: readonly string[]): ImageData {
  throw new Error("stub", { cause: [pixels, channels, colors] });
}
