// Arranges a catalog's rows under its groups, a group inside its wider group.
import type { Node } from "@services/graph/models";

/** Section is a group with the rows listed under it and the groups in it. */
export type Section = { group: Node; rows: Node[]; sections: Section[] };

/**
 * arrangeCatalog puts the rows of a catalog under its groups, in the order of
 * the groups. The loose rows are in no group the catalog lists.
 */
export function arrangeCatalog(groups: Node[], rows: Node[]): { sections: Section[]; loose: Node[] } {
  const listed = new Set(groups.map((group) => group.id));
  const rowsOf = Map.groupBy(rows, (row) => row.group ?? "");
  const groupsOf = Map.groupBy(groups, (group) => (group.group && listed.has(group.group) ? group.group : ""));
  const place = (wider: string): Section[] =>
    (groupsOf.get(wider) ?? []).map((group) => ({
      group,
      rows: rowsOf.get(group.id) ?? [],
      sections: place(group.id),
    }));

  return { sections: place(""), loose: rows.filter((row) => !listed.has(row.group ?? "")) };
}

/** listRows returns the rows of a section and of every section inside it. */
export function listRows(section: Section): Node[] {
  return [...section.rows, ...section.sections.flatMap(listRows)];
}
