/**
 * Counts, lists, dates and sizes as the app writes them in its sentences.
 */

/** "1 file", "3 files". */
export const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? "" : "s"}`;

const AND = new Intl.ListFormat("en");
/** "a, b, and c". */
export const list = (words: Iterable<string>) => AND.format(words);

/** A path's last part, its file's (or folder's) name, whichever way its slashes go. */
export const fileName = (path: string) => path.split(/[\\/]/).at(-1) ?? path;

/** The text with its first letter capital. */
export const capital = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

/** A date as its day and month: "21 April". */
export const day = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "long" });

/** A size in MB, or in KB under one MB. */
export function bytes(count: number): string {
  const mb = count >= 1048576;
  const unit = mb ? "megabyte" : "kilobyte";
  return new Intl.NumberFormat(undefined, { style: "unit", unit, maximumFractionDigits: 1 }).format(count / (mb ? 1048576 : 1024));
}

/** An image as "1020 by 621, 245 kB". */
export const imageSize = (image: { width: number; height: number; bytes: number }) =>
  `${image.width} by ${image.height}, ${bytes(image.bytes)}`;

/** How many of each kind a list holds, each kind where it first appears. */
export function countKinds(list: { kind: string }[]): { kind: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const { kind } of list) counts.set(kind, (counts.get(kind) ?? 0) + 1);
  return [...counts].map(([kind, count]) => ({ kind, count }));
}
