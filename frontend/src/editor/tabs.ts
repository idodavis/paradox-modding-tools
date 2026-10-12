/**
 * A tab's key: a file's path, or a diff of it; the files that have two views and the images.
 */

/** A file's view: its text (code), or what it draws (visual). */
export type View = "code" | "visual";

/** A diff tab's kind: a file's changes (its staged copy against it), its staged changes (the last commit against
 * the staged copy), a conflict's two sides, or what a commit changed in it (its first parent against it). */
export type Diff = "changes" | "staged" | "conflict" | "commit";

/** The images the IDE draws (ImageView); the raster ones have no text to show. */
export const IMAGES = /\.(png|jpe?g|gif|webp|bmp|ico|dds|tga|svg)$/i;

const VIEWS: Record<string, { code: string; visual: string; first: View }> = {
  md: { code: "Raw", visual: "Rich", first: "code" },
  svg: { code: "Text", visual: "Image", first: "visual" },
};

/** The tab of a file's diff; a commit's names the commit. */
export const diffTab = (diff: Diff, path: string, commit = "") => `git:${diff}${commit && `@${commit}`}:${path}`;

/** A tab's file, and its diff (and commit) when it shows one; a file's own tab is its path. */
export function tabOf(tab: string): { file: string; diff?: Diff; commit?: string } {
  const parts = /^git:(changes|staged|conflict|commit)(?:@([0-9a-f]+))?:(.+)$/s.exec(tab);
  return parts ? { file: parts[3] ?? "", diff: parts[1] as Diff, commit: parts[2] } : { file: tab };
}

/** A file's two views where it has both, named as its kind names them, and the one it opens in. */
export function viewsOf(path: string): (typeof VIEWS)[string] | undefined {
  return VIEWS[path.slice(path.lastIndexOf(".") + 1).toLowerCase()];
}
