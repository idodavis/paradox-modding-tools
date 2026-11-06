/** The placeholders PMT draws where a picture has no file, and the colors of a data call's sample text. */

/** FILES holds the URL of each shape's SVG file, by its path. */
const FILES = import.meta.glob<string>("../assets/placeholders/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

/** COLORS holds the syntax color of each shape. */
const COLORS: Record<string, string> = {
  "crowned-ruler": "name",
  "top-hat": "name",
  "plumed-hat": "name",
  shield: "macro",
  flag: "command",
  faith: "keyword",
  illustration: "comment",
  goods: "number",
  action: "keyword",
  military: "operator",
  building: "field",
  trait: "link",
  modifier: "string",
  concept: "command",
  rank: "name",
  map: "string",
  message: "operator",
  icon: "comment",
};

/** SAMPLE_COLORS holds the syntax color of a data call's sample text, by the kind Go gives the sample. */
export const SAMPLE_COLORS: Record<string, string> = {
  number: "var(--syn-number)",
  date: "var(--syn-number)",
  text: "var(--syn-string)",
};

/** findPlaceholder returns the CSS that draws the given shape over a box, cut to an image's outline if one is given. */
export function findPlaceholder(shape: string, cut = ""): Record<string, string> {
  const known = FILES[`../assets/placeholders/${shape}.svg`] ? shape : "icon";
  const drawing = `url("${FILES[`../assets/placeholders/${known}.svg`]}") 0 0 / 100% 100% no-repeat`;
  return {
    background: `var(--syn-${COLORS[known]})`,
    mask: cut ? `url("${cut}") 0 0 / 100% 100% no-repeat, ${drawing}` : drawing,
    // Set after the mask, which resets it.
    "mask-composite": "intersect",
  };
}
