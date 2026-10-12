/**
 * The file kinds the IDE tells apart: each Paradox kind's icon and extensions, those icons as the explorer tree's
 * sprite, and the binary files the explorer and the search leave out while a workspace hides binaries.
 */

const RAW = import.meta.glob<string>("./fileicons/*.svg", { query: "?raw", import: "default", eager: true });
const URLS = import.meta.glob<string>("./fileicons/*.svg", { query: "?url", import: "default", eager: true });
const iconFile = (kind: string) => `./fileicons/paradox-${kind}.svg`;

/** The Paradox files' icons: each kind (its paradox-<kind>.svg) and the extensions it draws. */
export const ICON_EXTENSIONS = {
  script:
    "txt editordata compound schematic aggregate curves layout lines anchor modifierpack guistateset meta smv ugraph dep skin lookup",
  loc: "yml yaml",
  gui: "gui",
  mod: "mod info dlc",
  mesh: "mesh",
  asset: "asset",
  anim: "anim animsm guianimset",
  texture: "dds tga",
  shader: "shader fxh shadernodes shadergraph",
  gfx: "gfx",
  font: "font fnt",
  audio: "bank bnk wem audio2 ogg wav mp3 flac",
  video: "bk2",
  particle: "particle particle2",
  map: "map terrain heightmap splnet vertical_borders",
  settings: "settings shortcuts bindings profile config",
  bbcode: "bbcode",
};

const KIND_BY_EXTENSION: Record<string, string> = Object.fromEntries(
  Object.entries(ICON_EXTENSIONS).flatMap(([kind, extensions]) => extensions.split(" ").map((one) => [one, kind])),
);

/** A Paradox file's icon, for the editor's tabs; none for other files. */
export function iconOf(path: string): string | undefined {
  const kind = KIND_BY_EXTENSION[path.slice(path.lastIndexOf(".") + 1).toLowerCase()];
  return kind && URLS[iconFile(kind)];
}

// Lucide's app-window, the mark of a folder in a window of its own
const WINDOW_SYMBOL = `<symbol id="pmt-window" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M10 4v4M2 8h20M6 4v4"/></symbol>`;

// the icons as one sprite in the tree's shadow root, each gradient renamed so no two share an id
const SPRITE = `<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position: absolute">${Object.keys(
  ICON_EXTENSIONS,
)
  .map((kind) =>
    RAW[iconFile(kind)]
      .replace(/<svg[^>]*>/, `<symbol id="pmt-${kind}" viewBox="0 0 800 800">`)
      .replace("</svg>", "</symbol>")
      .replaceAll('id="pmt"', `id="pmt-${kind}-fill"`)
      .replaceAll("url(#pmt)", `url(#pmt-${kind}-fill)`),
  )
  .join("")}${WINDOW_SYMBOL}</svg>`;

/** The tree's icon set: Pierre's for every other kind of file, ours by extension for the Paradox kinds. */
export const TREE_ICONS = {
  set: "complete" as const,
  spriteSheet: SPRITE,
  byFileExtension: Object.fromEntries(
    Object.entries(KIND_BY_EXTENSION).map(([extension, kind]) => [
      extension,
      { name: `pmt-${kind}`, viewBox: "0 0 800 800" },
    ]),
  ),
};

const BINARY_LIST = ["dll", "exe", "pdb", "hash", "hex", "bin", "dat"];

/** The binary files, and git's own folder: hidden while the workspace hides binaries, as search leaves them out. */
export const BINARY_EXTENSIONS = new RegExp(`\\.(${BINARY_LIST.join("|")})$|^\\.git$`, "i");

/** The binary files as the search's exclude globs. */
export const binaryGlobs = (): string[] => [`*.{${BINARY_LIST.join(",")}}`];
