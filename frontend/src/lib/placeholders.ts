/**
 * The placeholders PMT draws where a picture has no file: a shape for each kind of object, which Go names, tinted in a
 * syntax color of its own so that placeholders side by side stay apart. Also the colors of the sample text that Go puts
 * in place of a data call in a text.
 */
const FILES = import.meta.glob<string>("../assets/placeholders/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

/** The syntax color of each shape: a game's portrait and its coat of arms share their kind's. */
const COLORS: Record<string, string> = {
  "crowned-ruler": "entry",
  "top-hat": "entry",
  "plumed-hat": "entry",
  shield: "variable",
  flag: "block",
  faith: "literal",
  illustration: "comment",
  goods: "number",
  action: "literal",
  military: "operator",
  building: "field",
  trait: "scope",
  modifier: "string",
  concept: "block",
  rank: "entry",
  map: "string",
  message: "operator",
  icon: "comment",
};

/** The syntax color of the sample text of a data call, by the kind Go gives the sample. */
export const SAMPLE_COLORS: Record<string, string> = {
  number: "var(--syn-number)",
  date: "var(--syn-literal)",
  text: "var(--syn-string)",
};

/**
 * Finds the placeholder of the given shape, as the CSS that draws the shape over a box in its color and nothing else
 * of the box. A shape with no file draws the plain icon. Given the URL of an image, the shape is cut to the image's
 * outline.
 */
export function findPlaceholder(shape: string, cut = ""): Record<string, string> {
  const known = FILES[`../assets/placeholders/${shape}.svg`] ? shape : "icon";
  const drawing = `url("${FILES[`../assets/placeholders/${known}.svg`]}") 0 0 / 100% 100% no-repeat`;
  return {
    background: `var(--syn-${COLORS[known]})`,
    mask: cut ? `url("${cut}") 0 0 / 100% 100% no-repeat, ${drawing}` : drawing,
    // set after the mask, which resets it: the cut keeps what it covers of the shape
    "mask-composite": "intersect",
  };
}
