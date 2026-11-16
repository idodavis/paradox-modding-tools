/** The colors of code: each token type with the theme variable for it. */

/**
 * SYNTAX holds, by the token type Go or an editor grammar gives, the name of
 * the `--syn-<name>` theme variable that colors it.
 */
export const SYNTAX: Readonly<Record<string, string>> = {
  comment: "comment",
  string: "string",
  number: "number",
  choice: "number",
  icon: "number",
  operator: "operator",
  format: "comment",
  block: "field",
  field: "field",
  entry: "name",
  keyword: "keyword",
  command: "command",
  call: "call",
  definition: "name",
  reference: "reference",
  link: "link",
  parameter: "macro",
  constant: "macro",
  // Monaco's own names for the parts of a JSON file. A value takes the
  // reference color, as a .mod descriptor's does.
  "string.key.json": "field",
  "string.value.json": "reference",
  "number.json": "reference",
  "keyword.json": "reference",
};
