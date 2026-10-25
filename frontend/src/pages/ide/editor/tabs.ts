/**
 * Tab ids and what they open. A file tab is `file:<key>`; a diff tab is `diff:changes|staged|conflict:<key>` or
 * `diff:commit@<hash>:<key>`. One function parses an id and one makes a diff's. The table of views by file format
 * is here too.
 */
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

/** What a tab id decodes to: a file at a place, or a diff of a file at a revision. */
export type TabTarget =
  | { tab: "file"; key: string }
  | { tab: "diff"; side: "changes" | "staged" | "conflict" | "commit"; key: string; hash?: string };

type DiffSide = Extract<TabTarget, { tab: "diff" }>["side"];

const SIDES: readonly DiffSide[] = ["changes", "staged", "conflict", "commit"];
const DIFF = new RegExp(`^diff:(${SIDES.join("|")})(?:@([0-9a-f]+))?:(.+)$`, "s");

/**
 * How each file format is shown: its views, the URL of its icon and its editor language, keyed by Go's format. A
 * format with no icon of its own has "" for one.
 */
export const VIEWS: Readonly<Record<Format, { views: readonly View[]; icon: string; language: string }>> = {
  "": { views: ["code"], icon: "", language: "plaintext" },
  script: { views: ["code"], icon: scriptIcon, language: "paradox-script" },
  gui: { views: ["code", "visual", "split"], icon: guiIcon, language: "paradox-gui" },
  asset: { views: ["code"], icon: assetIcon, language: "paradox-script" },
  font: { views: ["code"], icon: fontIcon, language: "paradox-script" },
  gfx: { views: ["code"], icon: gfxIcon, language: "paradox-script" },
  localization: { views: ["code"], icon: locIcon, language: "paradox-localization" },
  image: { views: ["image"], icon: textureIcon, language: "plaintext" },
  table: { views: ["code"], icon: "", language: "plaintext" },
  descriptor: { views: ["code"], icon: modIcon, language: "plaintext" },
  shader: { views: ["code"], icon: shaderIcon, language: "plaintext" },
  text: { views: ["code"], icon: "", language: "plaintext" },
  binary: { views: [], icon: "", language: "plaintext" },
};

/** Each view's name and icon, in the order a tab offers them. */
export const VIEW_ROWS: readonly { view: View; label: string; icon: string }[] = [
  { view: "code", label: "Code", icon: "i-lucide-code" },
  { view: "split", label: "Split", icon: "i-lucide-columns-2" },
  { view: "visual", label: "Visual", icon: "i-lucide-eye" },
  { view: "form", label: "Form", icon: "i-lucide-text-cursor-input" },
  { view: "image", label: "Image", icon: "i-lucide-image" },
];

/**
 * Lists the views a file can show: its format's own, then Visual and Split when it holds something the Visual view
 * draws, else Form when it is text that defines a database entry.
 */
export function listViews(place: Place, drawable: boolean): View[] {
  const isText = VIEWS[place.format].views.includes("code");
  const more: View[] = drawable ? ["visual", "split"] : place.database && isText ? ["form"] : [];
  return [...new Set([...VIEWS[place.format].views, ...more])];
}

/** Decodes a tab id into what it opens. An id that is no diff's is a file's, with or without its prefix. */
export function parseTab(id: string): TabTarget {
  const [, text, hash, key] = DIFF.exec(id) ?? [];
  const side = SIDES.find((candidate) => candidate === text);
  if (side && key) return { tab: "diff", side, key, hash };
  return { tab: "file", key: id.replace(/^file:/, "") };
}

/** Makes a diff tab's id from its kind, revision and path. A diff of a commit names the commit's hash. */
export function diffTab(side: DiffSide, key: string, hash?: string): string {
  return `diff:${side}${hash ? `@${hash}` : ""}:${key}`;
}
