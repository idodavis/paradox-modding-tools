/** Tab ids and what they open, with the views each file format offers. */
import type { Format } from "@internal/files/models";
import type { Place } from "@services/workspace/models";
import type { View } from "../../../stores/editor";
import assetIcon from "../fileicons/paradox-asset.svg?url";
import fontIcon from "../fileicons/paradox-font.svg?url";
import gfxIcon from "../fileicons/paradox-gfx.svg?url";
import guiIcon from "../fileicons/paradox-gui.svg?url";
import locIcon from "../fileicons/paradox-loc.svg?url";
import modIcon from "../fileicons/paradox-mod.svg?url";
import scriptIcon from "../fileicons/paradox-script.svg?url";
import shaderIcon from "../fileicons/paradox-shader.svg?url";
import textureIcon from "../fileicons/paradox-texture.svg?url";

const SIDES = ["changes", "staged", "conflict", "commit"] as const;

/** A DiffSide names the two revisions a diff tab compares. */
type DiffSide = (typeof SIDES)[number];

/** A TabTarget is what one tab id opens, a file or a diff of one. */
export type TabTarget =
  { tab: "file"; key: string } | { tab: "diff"; side: DiffSide; root: string; key: string; hash?: string };

/** The pattern of a diff tab's id. */
const DIFF = new RegExp(`^diff:(${SIDES.join("|")})(?:@([0-9a-f]+))?:([^:]*):(.+)$`, "s");

/** VIEWS holds the views and the icon of each file format. A format with no icon of its own has "". */
export const VIEWS: Readonly<Record<Format, { views: readonly View[]; icon: string }>> = {
  "": { views: ["code"], icon: "" },
  script: { views: ["code"], icon: scriptIcon },
  gui: { views: ["code", "visual", "split"], icon: guiIcon },
  asset: { views: ["code"], icon: assetIcon },
  font: { views: ["code"], icon: fontIcon },
  gfx: { views: ["code"], icon: gfxIcon },
  localization: { views: ["code"], icon: locIcon },
  image: { views: ["image"], icon: textureIcon },
  table: { views: ["code"], icon: "" },
  descriptor: { views: ["code"], icon: modIcon },
  shader: { views: ["code"], icon: shaderIcon },
  text: { views: ["code"], icon: "" },
  binary: { views: [], icon: "" },
};

/** VIEW_ROWS holds each view's label and icon, in the order a tab offers them. */
export const VIEW_ROWS: readonly { view: View; label: string; icon: string }[] = [
  { view: "code", label: "Code", icon: "i-lucide-code" },
  { view: "split", label: "Split", icon: "i-lucide-columns-2" },
  { view: "visual", label: "Visual", icon: "i-lucide-eye" },
  { view: "image", label: "Image", icon: "i-lucide-image" },
];

/** listViews lists the views a file can show. A file the Visual view can draw also gets Visual and Split. */
export function listViews(place: Place, drawable: boolean): View[] {
  const more: View[] = drawable ? ["visual", "split"] : [];
  return [...new Set([...VIEWS[place.format].views, ...more])];
}

/** parseTab decodes a tab id into what it opens. Any id that is not a diff's is a file's. */
export function parseTab(id: string): TabTarget {
  const [, text, hash, root = "", key] = DIFF.exec(id) ?? [];
  const side = SIDES.find((candidate) => candidate === text);
  if (side && key) return { tab: "diff", side, root: decodeURIComponent(root), key, hash };
  return { tab: "file", key: id.replace(/^file:/, "") };
}

/** fileTab makes a file tab's id from the file's key. */
export function fileTab(key: string): string {
  return `file:${key}`;
}

/** diffTab makes a diff tab's id. A diff of a commit also holds the commit's hash. */
export function diffTab(side: DiffSide, root: string, key: string, hash?: string): string {
  return `diff:${side}${hash ? `@${hash}` : ""}:${encodeURIComponent(root)}:${key}`;
}
