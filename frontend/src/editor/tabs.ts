/**
 * A tab's key: a file's path, or a diff of it; the files that have views and the images.
 */

/** A file's view: its text (code), what it draws (visual), or both side by side (split). */
export type View = "code" | "split" | "visual";

/** A kind of file with views, found by its extension or the folder it sits in: what the settings call it, what its
 * code and visual views are called, the one it opens in unless the IDE settings say another, whether it can be split,
 * the name of its panel ("" for none), and what its picture's toolbar offers: a window's resolution and states, or
 * a coat's field alone. */
export type ViewRow = {
  ext?: string;
  folder?: string;
  label: string;
  code: string;
  visual: string;
  first: View;
  split: boolean;
  panel: string;
  toolbar?: "window" | "field";
};

/** A diff tab's kind: a file's changes (its staged copy against it), its staged changes (the last commit against
 * the staged copy), a conflict's two sides, or what a commit changed in it (its first parent against it). */
export type Diff = "changes" | "staged" | "conflict" | "commit";

/** The images the IDE draws (ImageView); the raster ones have no text to show. */
export const IMAGES = /\.(png|jpe?g|gif|webp|bmp|ico|dds|tga|svg)$/i;

export const VIEW_ROWS: ViewRow[] = [
  { ext: "md", label: "MD", code: "Raw", visual: "Rich", first: "code", split: false, panel: "" },
  { ext: "svg", label: "SVG", code: "Text", visual: "Image", first: "visual", split: false, panel: "" },
  {
    ext: "gui",
    label: "GUI",
    code: "Code",
    visual: "Visual",
    first: "split",
    split: true,
    panel: "Widgets",
    toolbar: "window",
  },
  {
    folder: "common/coat_of_arms/coat_of_arms",
    label: "Coat of arms",
    code: "Code",
    visual: "Visual",
    first: "split",
    split: true,
    panel: "Layers",
    toolbar: "field",
  },
];

/** The settings a kind's views are kept under, by its extension, else its folder: the view it opens in, and whether
 * its panel opens with the picture. */
export const viewSettings = (row: ViewRow) => ({
  view: `ide.view.${row.ext ?? row.folder}`,
  panel: `ide.panel.${row.ext ?? row.folder}`,
});

/** The tab of a file's diff; a commit's names the commit. */
export const diffTab = (diff: Diff, path: string, commit = "") => `git:${diff}${commit && `@${commit}`}:${path}`;

/** A tab's file, and its diff (and commit) when it shows one; a file's own tab is its path. */
export function tabOf(tab: string): { file: string; diff?: Diff; commit?: string } {
  const parts = /^git:(changes|staged|conflict|commit)(?:@([0-9a-f]+))?:(.+)$/s.exec(tab);
  return parts ? { file: parts[3] ?? "", diff: parts[1] as Diff, commit: parts[2] } : { file: tab };
}

/** A file's views where its kind has them, by its path: the row whose extension is its own, else the one whose folder
 * holds it anywhere in the path (EU5 puts its folders under main_menu/ and in_game/). */
export function viewsOf(path: string): ViewRow | undefined {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  const folders = `/${path.replaceAll("\\", "/").toLowerCase()}`;
  return VIEW_ROWS.find((row) => row.ext === ext || (row.folder !== undefined && folders.includes(`/${row.folder}/`)));
}

/** A kind's views in the order its switch shows them and Ctrl+Shift+V steps through them, each with its icon. */
export function viewItems(row: ViewRow): { label: string; value: View; icon: string }[] {
  return [
    { label: row.code, value: "code", icon: "i-lucide-code" },
    ...(row.split ? [{ label: "Split", value: "split" as const, icon: "i-lucide-columns-2" }] : []),
    { label: row.visual, value: "visual", icon: "i-lucide-eye" },
  ];
}
