/**
 * The formatters: every sentence or number the UI writes from facts, one function each, used wherever that
 * sentence appears. A name or a description from Go is shown as it is; these are the sentences that combine facts.
 */
import { formatTimeAgo, type UseTimeAgoMessages } from "@vueuse/core";

import type { SourceState } from "@internal/schema/models";
import type { Comparison, Details } from "@services/steam/models";

const PLURALS = new Intl.PluralRules("en");
const LISTS = new Intl.ListFormat("en-GB");
const DAYS = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "long" });

// short units, so the install line stays on one line
const AGO: UseTimeAgoMessages = {
  justNow: "just now",
  past: (text) => (/\d/.test(String(text)) ? `${text} ago` : String(text)),
  future: (text) => String(text),
  month: (count) => `${count} mo`,
  year: (count) => `${count} y`,
  day: (count) => `${count} d`,
  week: (count) => `${count} wk`,
  hour: (count) => `${count} h`,
  minute: (count) => `${count} min`,
  second: (count) => `${count} s`,
  invalid: "",
};

/** Why the game keeps a definition, in words, by Go's reason for it. */
export const REASONS: Readonly<Record<string, string>> = {
  order: "Last in load order",
  mode: "By its entry mode",
  priority: "Highest priority",
  merge: "Merged",
  merged: "Injected",
  replace: "After a replace_path",
};

/** Each localization language's name in English, by the folder the game keeps it in. */
const LANGUAGES: Readonly<Record<string, string>> = {
  english: "English",
  french: "French",
  german: "German",
  spanish: "Spanish",
  russian: "Russian",
  korean: "Korean",
  simp_chinese: "Simplified Chinese",
  polish: "Polish",
  turkish: "Turkish",
  braz_por: "Brazilian Portuguese",
  japanese: "Japanese",
};

/** The given localization languages as a select's items, each named in English, or by its folder when it has no name. */
export function describeLanguages(languages: readonly string[] | null | undefined): { label: string; value: string }[] {
  return (languages ?? []).map((value) => ({ label: LANGUAGES[value] ?? value, value }));
}

/** "1 file", "3 files". The plural is the noun with an s unless one is given. */
export function plural(count: number, noun: string, plural?: string): string {
  return `${count} ${PLURALS.select(count) === "one" ? noun : (plural ?? `${noun}s`)}`;
}

/** "Has landed titles" from "has landed titles": words as the start of a sentence. */
export function toSentence(words: string): string {
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** "a, b and c". It is an empty string for no items. */
export function list(items: readonly string[]): string {
  return LISTS.format(items);
}

/** A date as the user reads it: "21 April". It is an empty string for a date that does not parse. */
export function day(date: Date | string): string {
  const parsed = new Date(date);
  return Number.isNaN(parsed.getTime()) ? "" : DAYS.format(parsed);
}

/** "1.2 MB". A size under a megabyte is in kilobytes, however small. */
export function bytes(count: number): string {
  const megabytes = count >= 1048576;
  return new Intl.NumberFormat(undefined, {
    style: "unit",
    unit: megabytes ? "megabyte" : "kilobyte",
    maximumFractionDigits: 1,
  }).format(count / (megabytes ? 1048576 : 1024));
}

/**
 * A diagnostic's message from its code's template and its facts. A placeholder with no fact stays as written, so a
 * missing fact shows in the message.
 */
export function diagnosticMessage(template: string, facts: Record<string, string | undefined>): string {
  return template.replace(/\{(\w+)\}/g, (placeholder, key: string) => facts[key] ?? placeholder);
}

/** The install line: "CK3 1.20.1, scanned 4 min ago". An install never scanned says "not scanned". */
export function installLine(game: string, version: string, scannedAt: string): string {
  const scanned = new Date(scannedAt);
  // Go writes a time never set as the year 1
  const ago = scanned.getFullYear() > 1 ? `scanned ${formatTimeAgo(scanned, { messages: AGO })}` : "not scanned";
  return [[game, version].filter(Boolean).join(" "), ago].filter(Boolean).join(", ");
}

/**
 * Says whose script_docs a workspace reads, and whether that is the game's own and current. The game's dumps older
 * than the install, PMT's shipped copy and no dumps at all are each not current.
 */
export function describeDumps(dumps: SourceState): { current: boolean; label: string } {
  if (dumps.origin === "install" && !dumps.stale) return { current: true, label: "script_docs loaded" };
  if (dumps.origin === "install") return { current: false, label: "script_docs older than the game" };
  if (dumps.origin === "shipped") return { current: false, label: "Using PMT's copy of script_docs" };
  return { current: false, label: "No script_docs" };
}

/** An item's state on Steam as sentences: its visibility, subscribers and last update. None when there is no item. */
export function steamSentences(details: Details | null): string[] {
  if (!details) return [];
  return [
    `It is ${details.visibility} on Steam.`,
    `${plural(details.subscribers, "subscriber")}.`,
    `Last updated ${day(details.updatedAt)}.`,
  ];
}

/** What the next upload changes, as sentences: the files added, changed and removed, and the listing fields changed. */
export function uploadSentences(comparison: Comparison): string[] {
  const files = [
    { names: comparison.added, verb: "added" },
    { names: comparison.changed, verb: "changed" },
    { names: comparison.removed, verb: "removed" },
  ];
  const sentences = files.flatMap(({ names, verb }) =>
    names?.length ? [`${plural(names.length, "file")} ${verb}.`] : [],
  );
  if (comparison.listing?.length) sentences.push(`The ${list(comparison.listing)} will change.`);
  return sentences.length ? sentences : ["Nothing will change."];
}
