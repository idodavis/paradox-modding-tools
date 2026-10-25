/**
 * The placeholders PMT draws where a picture has no file: a shape for each kind of object, which Go names, tinted in a
 * syntax color of its own so that placeholders side by side stay apart.
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

/**
 * Finds the placeholder of the given shape, as the CSS that draws it over a box: the shape in its color, over a soft
 * tint of that color. A shape with no file draws the plain icon.
 */
export function findPlaceholder(shape: string): Record<string, string> {
  const known = FILES[`../assets/placeholders/${shape}.svg`] ? shape : "icon";
  const url = FILES[`../assets/placeholders/${known}.svg`];
  return {
    background: `var(--syn-${COLORS[known]})`,
    mask: `url("${url}") 0 0 / 100% 100% no-repeat, linear-gradient(rgb(0 0 0 / 16%) 0 0)`,
  };
}
