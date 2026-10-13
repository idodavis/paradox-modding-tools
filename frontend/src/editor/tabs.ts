/**
 * A tab's key: a file's path, or a diff of it; the files that have views and the images.
 */

/** A file's view: its text (code), what it draws (visual), or both side by side (split). */
export type View = "code" | "split" | "visual";

/** A kind of file with views, found by its extension or the folder it sits in: what its code and visual views are
 * called, the one it opens in, whether it can be split, and the name of its panel ("" for none). */
export type ViewRow = {
  ext?: string;
  folder?: string;
  code: string;
  visual: string;
  first: View;
  split: boolean;
  panel: string;
};

/** A diff tab's kind: a file's changes (its staged copy against it), its staged changes (the last commit against
 * the staged copy), a conflict's two sides, or what a commit changed in it (its first parent against it). */
export type Diff = "changes" | "staged" | "conflict" | "commit";

/** The images the IDE draws (ImageView); the raster ones have no text to show. */
export const IMAGES = /\.(png|jpe?g|gif|webp|bmp|ico|dds|tga|svg)$/i;

export const VIEW_ROWS: ViewRow[] = [
  { ext: "md", code: "Raw", visual: "Rich", first: "code", split: false, panel: "" },
  { ext: "svg", code: "Text", visual: "Image", first: "visual", split: false, panel: "" },
  { ext: "gui", code: "Code", visual: "Visual", first: "code", split: true, panel: "Widgets" },
];

/** The tab of a file's diff; a commit's names the commit. */
export const diffTab = (diff: Diff, path: string, commit = "") => `git:${diff}${commit && `@${commit}`}:${path}`;

/** A tab's file, and its diff (and commit) when it shows one; a file's own tab is its path. */
export function tabOf(tab: string): { file: string; diff?: Diff; commit?: string } {
  const parts = /^git:(changes|staged|conflict|commit)(?:@([0-9a-f]+))?:(.+)$/s.exec(tab);
  return parts ? { file: parts[3] ?? "", diff: parts[1] as Diff, commit: parts[2] } : { file: tab };
}

/** A file's views where its kind has them, by its path from its root: the row whose extension is its own, else the
 * one whose folder holds it. */
export function viewsOf(rel: string): ViewRow | undefined {
  const ext = rel.slice(rel.lastIndexOf(".") + 1).toLowerCase();
  return VIEW_ROWS.find((row) => row.ext === ext || (row.folder !== undefined && rel.startsWith(row.folder)));
}
