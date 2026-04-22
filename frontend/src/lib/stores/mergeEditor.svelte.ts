import type { MergeConflictChunk } from "@services/models";

export type ResolvedSide = "A" | "B" | "Custom" | undefined;

const ADDITIONS_HEADER = "\n############# Additional Entries From B (PDX-Merge-Tools) #############\n";

const getIndicesByType = (chunks: MergeConflictChunk[], type: "conflict" | "added") =>
  chunks.flatMap((c, i) => (c.type === type ? [i] : []));
export const getConflictIndices = (chunks: MergeConflictChunk[]) => getIndicesByType(chunks, "conflict");
export const getAddedIndices = (chunks: MergeConflictChunk[]) => getIndicesByType(chunks, "added");

export function buildMergedContent(
  chunks: MergeConflictChunk[],
  rv: Record<number, string>,
  includedAdditions?: Record<number, boolean>,
): string {
  let out = "";
  let addHeader = false;
  for (let i = 0; i < chunks.length; i++) {
    const c = chunks[i];
    if (c.type === "unchanged") {
      out += c.textA;
      continue;
    }
    if (c.type === "added") {
      if (includedAdditions && includedAdditions[i] === false) continue;
      if (!addHeader) {
        if (out.length > 0 && !/\n$/.test(out)) out += "\n";
        out += ADDITIONS_HEADER;
        addHeader = true;
      }
      out += c.textB;
      continue;
    }
    out += rv[i] ?? "";
  }
  return out;
}

export const computeMergeStats = (
  chunks: MergeConflictChunk[],
  resolved: Record<number, ResolvedSide>,
  includedAdditions?: Record<number, boolean>,
) => ({
  changed: chunks.filter((c, i) => c.type === "conflict" && resolved[i] !== "A").length,
  added: includedAdditions
    ? chunks.filter((c, i) => c.type === "added" && includedAdditions[i] !== false).length
    : chunks.filter((c) => c.type === "added").length,
});
