/**
 * Tab ids and what they open. A file tab is `file:<key>`; a diff tab is `diff:changes|staged|conflict:<key>` or
 * `diff:commit@<hash>:<key>`. One function parses an id and one makes a diff's. The table of views by file format
 * is here too.
 */
import type { Format } from "@internal/files/models";
import type { View } from "../../../stores/editor";
/** What a tab id decodes to: a file at a place, or a diff of a file at a revision. */
export type TabTarget =
  | { tab: "file"; key: string }
  | { tab: "diff"; side: "changes" | "staged" | "conflict" | "commit"; key: string; hash?: string };
/** Decodes a tab id into what it opens. */
export function parseTab(id: string): TabTarget {
  throw new Error("stub", { cause: id });
}
/** Makes a diff tab's id from its kind, revision and path. */
export function diffTab(side: TabTarget extends { side: infer S } ? S : never, key: string, hash?: string): string {
  throw new Error("stub", { cause: [side, key, hash] });
}

/** How each file format is shown: its views, its icon and its editor language, keyed by Go's format. */
export const VIEWS: Readonly<Record<Format, { views: readonly View[]; icon: string; language: string }>> = {} as Record<
  Format,
  { views: readonly View[]; icon: string; language: string }
>;
