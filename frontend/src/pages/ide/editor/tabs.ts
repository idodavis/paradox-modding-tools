/** Tab ids and what they open, with how each file format is shown and its icon. */
import type { Format } from "@internal/files/models";
import type { Place } from "@services/workspace/models";
import animIcon from "../fileicons/paradox-anim.svg?url";
import assetIcon from "../fileicons/paradox-asset.svg?url";
import audioIcon from "../fileicons/paradox-audio.svg?url";
import bbcodeIcon from "../fileicons/paradox-bbcode.svg?url";
import binaryIcon from "../fileicons/paradox-binary.svg?url";
import csvIcon from "../fileicons/paradox-csv.svg?url";
import fontIcon from "../fileicons/paradox-font.svg?url";
import gfxIcon from "../fileicons/paradox-gfx.svg?url";
import guiIcon from "../fileicons/paradox-gui.svg?url";
import infoIcon from "../fileicons/paradox-info.svg?url";
import jsonIcon from "../fileicons/paradox-json.svg?url";
import locIcon from "../fileicons/paradox-loc.svg?url";
import logIcon from "../fileicons/paradox-log.svg?url";
import mdIcon from "../fileicons/paradox-md.svg?url";
import meshIcon from "../fileicons/paradox-mesh.svg?url";
import modIcon from "../fileicons/paradox-mod.svg?url";
import scriptIcon from "../fileicons/paradox-script.svg?url";
import shaderIcon from "../fileicons/paradox-shader.svg?url";
import textIcon from "../fileicons/paradox-text.svg?url";
import textureIcon from "../fileicons/paradox-texture.svg?url";
import videoIcon from "../fileicons/paradox-video.svg?url";

const SIDES = ["changes", "staged", "conflict", "commit"] as const;

/** A DiffSide names the two revisions a diff tab compares. */
type DiffSide = (typeof SIDES)[number];

/** A TabTarget is what one tab id opens, a file or a diff of one. */
export type TabTarget =
  { tab: "file"; key: string } | { tab: "diff"; side: DiffSide; root: string; key: string; hash?: string };

/** The pattern of a diff tab's id. */
const DIFF = new RegExp(`^diff:(${SIDES.join("|")})(?:@([0-9a-f]+))?:([^:]*):(.+)$`, "s");

/** VIEWS holds how each file format is shown, as code or as an image, or not at all for null, and its icon. */
export const VIEWS: Readonly<Record<Format, { view: "code" | "image" | null; icon: string }>> = {
  "": { view: "code", icon: textIcon },
  script: { view: "code", icon: scriptIcon },
  gui: { view: "code", icon: guiIcon },
  asset: { view: "code", icon: assetIcon },
  font: { view: "code", icon: fontIcon },
  gfx: { view: "code", icon: gfxIcon },
  localization: { view: "code", icon: locIcon },
  image: { view: "image", icon: textureIcon },
  "web-image": { view: "image", icon: textureIcon },
  table: { view: "code", icon: csvIcon },
  descriptor: { view: "code", icon: modIcon },
  shader: { view: "code", icon: shaderIcon },
  text: { view: "code", icon: textIcon },
  binary: { view: null, icon: binaryIcon },
};

/** EXTENSION_ICONS holds the icons of the extensions that have one of their own, by the format they are in. */
const EXTENSION_ICONS: Partial<Record<Format, Readonly<Record<string, string>>>> = {
  text: { ".json": jsonIcon, ".info": infoIcon, ".md": mdIcon, ".bbcode": bbcodeIcon, ".log": logIcon },
  binary: {
    ".ogg": audioIcon,
    ".wav": audioIcon,
    ".mp3": audioIcon,
    ".bk2": videoIcon,
    ".mesh": meshIcon,
    ".anim": animIcon,
  },
};

/** findIcon returns the icon of a file: its extension's where its format has one for it, else its format's. */
export function findIcon(place: Place): string {
  const name = place.name.toLowerCase();
  const extensions = Object.entries(EXTENSION_ICONS[place.format] ?? {});
  return extensions.find(([extension]) => name.endsWith(extension))?.[1] ?? VIEWS[place.format].icon;
}

/** listIcons lists every file icon once. */
export function listIcons(): string[] {
  const extensions = Object.values(EXTENSION_ICONS).flatMap((icons) => Object.values(icons));
  return [...new Set([...Object.values(VIEWS).map(({ icon }) => icon), ...extensions])];
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
