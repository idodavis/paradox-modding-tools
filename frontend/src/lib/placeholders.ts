/**
 * The placeholders PMT draws where a game draws a picture it makes as it runs: a character's silhouette from each
 * game's period, a coat of arms and a faith. A game's row names each file.
 */
const FILES = import.meta.glob<string>("../assets/placeholders/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

/** Finds the placeholder in the given file, as a CSS image, or none where no placeholder has that file. */
export function findPlaceholder(file: string): string {
  const url = FILES[`../assets/placeholders/${file}.svg`];
  return url ? `url("${url}")` : "none";
}
