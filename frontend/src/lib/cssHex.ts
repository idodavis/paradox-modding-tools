/**
 * A CSS color as #rrggbb: what a color input and the hover's markdown take, var() and oklch resolved.
 */
import Color from "colorjs.io";

export function cssHex(color: string): string {
  const probe = document.body.appendChild(document.createElement("span"));
  probe.style.color = color;
  const computed = getComputedStyle(probe).color;
  probe.remove();
  return new Color(computed).to("srgb").toString({ format: "hex" });
}
