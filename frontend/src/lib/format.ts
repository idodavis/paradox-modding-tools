/**
 * The formatters: every sentence or number the UI writes from facts, one function each, used wherever that
 * sentence appears. A name or a description from Go is shown as it is; these are the sentences that combine facts.
 */
import type { Comparison, Details } from "@services/steam/models";

/** "1 file", "3 files". */
export function plural(count: number, noun: string, plural?: string): string {
  throw new Error("stub", { cause: [count, noun, plural] });
}

/** "a, b and c". */
export function list(items: readonly string[]): string {
  throw new Error("stub", { cause: items });
}

/** A date as the user reads it. */
export function day(date: Date | string): string {
  throw new Error("stub", { cause: date });
}

/** "1.2 MB". */
export function bytes(count: number): string {
  throw new Error("stub", { cause: count });
}

/** A diagnostic's message from its code's template and its facts. */
export function diagnosticMessage(template: string, facts: Record<string, string>): string {
  throw new Error("stub", { cause: [template, facts] });
}

/** The install line: "CK3 1.20.1, scanned 4 min ago". */
export function installLine(game: string, version: string, scannedAt: string): string {
  throw new Error("stub", { cause: [game, version, scannedAt] });
}

/** An item's state on Steam as sentences: its visibility, subscribers and last update. None when there is no item. */
export function steamSentences(details: Details | null): string[] {
  throw new Error("stub", { cause: details });
}

/** What the next upload changes, as sentences: the files added, changed and removed, and the listing fields changed. */
export function uploadSentences(comparison: Comparison): string[] {
  throw new Error("stub", { cause: comparison });
}
