/** The sentences and numbers the UI writes from facts. A name or a description from Go is shown as it is. */
import { formatTimeAgo, type UseTimeAgoMessages } from "@vueuse/core";
import { groupBy, mapValues, sumBy } from "es-toolkit";

import type { SourceState } from "@internal/schema/models";
import type { Header } from "@internal/texture/models";
import type { Difference } from "@services/steam/models";
import type { Mod, State } from "@services/workspace/models";
import type { Finding } from "@services/health/models";
import type { Part, Site } from "@services/lsp/models";

const PLURALS = new Intl.PluralRules("en");
const NUMBERS = new Intl.NumberFormat("en");
const LISTS = new Intl.ListFormat("en-GB");
const DAYS = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "long" });
const CLOCK = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });

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

/** describeCount returns a count as the user reads it, such as "8" or "1,350". */
export function describeCount(count: number): string {
  return NUMBERS.format(count);
}

/** plural returns a count with its noun, such as "1 file" or "1,350 files". Without a plural form it adds an s. */
export function plural(count: number, noun: string, pluralNoun?: string): string {
  return `${describeCount(count)} ${PLURALS.select(count) === "one" ? noun : (pluralNoun ?? `${noun}s`)}`;
}

/** toSentence returns words with a capital first letter, such as "Has landed titles". */
export function toSentence(words: string): string {
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * abbreviate returns the letters an avatar shows for a name: the first letters of its first two words, such as GC for
 * GitHub Copilot, or the first two letters of a one-word name, such as Cl for Claude.
 */
export function abbreviate(name: string): string {
  const words = name.split(/\s+/).filter(Boolean);
  return words.length > 1 ? `${words[0]?.[0]}${words[1]?.[0]}` : name.slice(0, 2);
}

/** list joins items into a phrase, such as "a, b and c". It is "" for no items. */
export function list(items: readonly string[]): string {
  return LISTS.format(items);
}

/** listLeftOut returns the names of the given mods that the workspace leaves out of its compatibility checks. */
export function listLeftOut(mods: readonly Mod[]): string[] {
  return mods.filter((mod) => mod.leftOut).map((mod) => mod.descriptor.name);
}

/** describeError returns an error's message. For a thrown value that is not an Error, it returns the value as text. */
export function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * splitError splits an error's message into its first line, which says what failed, and the lines after it. It keeps
 * at most the given count of those lines, or all of them without a count. The second part is "" for a one-line message.
 */
export function splitError(error: unknown, count = Infinity): [summary: string, details: string] {
  const [summary = "", ...details] = describeError(error).split("\n");
  return [summary, details.slice(0, count).join("\n")];
}

/**
 * matchQuery says whether the query occurs in the texts joined by spaces, ignoring case and the spaces around the
 * query. So a query can span two texts, such as a key and its value. An empty query matches everything.
 */
export function matchQuery(query: string, ...texts: (string | null | undefined)[]): boolean {
  const needle = query.trim().toLowerCase();
  return !needle || texts.join(" ").toLowerCase().includes(needle);
}

/** day returns a date as the user reads it, such as "21 April". It is "" for a time that does not parse or is unset. */
export function day(date: Date | string): string {
  const parsed = parseTime(date);
  return parsed ? DAYS.format(parsed) : "";
}

/** parseTime returns a time as a Date. It returns undefined for a time that does not parse or is unset. */
export function parseTime(time: Date | string): Date | undefined {
  const parsed = new Date(time);
  // Go writes an unset time as the year 1, and one that does not parse has no year
  return parsed.getFullYear() > 1 ? parsed : undefined;
}

/**
 * describeAge returns how long ago a time was, short as "4 min ago" or long as "4 minutes ago". It is "" for a time
 * that does not parse or is unset.
 */
export function describeAge(time: Date | string, wording: "short" | "long"): string {
  const parsed = parseTime(time);
  return parsed ? formatTimeAgo(parsed, wording === "short" ? { messages: AGO } : {}) : "";
}

/** bytes returns a size as the user reads it, such as "1.2 MB". A size under a megabyte is in kilobytes. */
export function bytes(count: number): string {
  const megabytes = count >= 1048576;
  return new Intl.NumberFormat(undefined, {
    style: "unit",
    unit: megabytes ? "megabyte" : "kilobyte",
    maximumFractionDigits: 1,
  }).format(count / (megabytes ? 1048576 : 1024));
}

/** describePicture returns a picture's size from its header, such as "512 by 512, 184 KB". */
export function describePicture(header: Header): string {
  return `${header.width} by ${header.height}, ${bytes(header.bytes)}`;
}

/** joinParts returns parts as one plain sentence. It is "" for no parts. */
export function joinParts(parts: readonly Part[] | null | undefined): string {
  return (parts ?? []).map((part) => part.text).join("");
}

/** splitSubject returns what a finding is about as one part: its name, styled as a path for a file. */
export function splitSubject(finding: Finding): Part[] {
  return [{ text: finding.name, style: finding.database === "file" ? "path" : "" }];
}

/** findOwner returns the id of the mod a finding is about: the first mod it names, else its origin. */
export function findOwner(finding: Finding): string {
  return finding.mods?.[0] ?? finding.origin;
}

/** toSiteKey returns the key of a finding's first site, such as "path:line". It is "" for a finding with no site. */
export function toSiteKey(finding: Finding): string {
  return describeSiteKey(finding.sites?.[0]);
}

/** listSiteKeys returns the keys of a finding's first site and of the sites of its repeats. */
export function listSiteKeys(finding: Finding): string[] {
  return [finding.sites?.[0], ...(finding.repeats ?? [])].map(describeSiteKey).filter(Boolean);
}

/** describeSiteKey returns the key of a site, such as "path:line". It is "" for no site. */
function describeSiteKey(site: Site | undefined): string {
  return site ? `${site.place.path}:${site.line}` : "";
}

/** countFindings returns how many findings or log lines a set of rows stands for, repeats included. */
export function countFindings(findings: readonly Finding[]): number {
  return sumBy(findings, (finding) => finding.count);
}

/** countFindingsBy returns the findings count, repeats included, for each key that the key function gives. */
export function countFindingsBy(
  findings: readonly Finding[],
  key: (finding: Finding) => string,
): Record<string, number> {
  return mapValues(groupBy(findings, key), countFindings);
}

/**
 * describeTime returns a time such as "today at 14:02" or "21 April at 14:02". It is "" for a time that does not parse
 * or is unset.
 */
export function describeTime(time: Date | string): string {
  const parsed = parseTime(time);
  if (!parsed) return "";
  const clock = CLOCK.format(parsed);
  return parsed.toDateString() === new Date().toDateString()
    ? `today at ${clock}`
    : `${DAYS.format(parsed)} at ${clock}`;
}

/** installLine returns an install's line, such as "CK3 1.20.1, scanned 4 min ago" or "CK3 1.20.1, not scanned". */
export function installLine(game: string, version: string, scannedAt: string): string {
  const age = describeAge(scannedAt, "short");
  const scanned = age ? `scanned ${age}` : "not scanned";
  return [[game, version].filter(Boolean).join(" "), scanned].filter(Boolean).join(", ");
}

/** describeDumps describes the game's documentation a workspace reads, and whether it is the game's own and current. */
export function describeDumps(dumps: SourceState): { current: boolean; label: string } {
  if (dumps.origin === "install" && !dumps.stale) return { current: true, label: "The game's own documentation" };
  if (dumps.origin === "install") return { current: false, label: "The game's documentation, older than the game" };
  if (dumps.origin === "shipped") return { current: false, label: "PMT's copy of the game's documentation" };
  return { current: false, label: "No documentation of the game" };
}

/** describeLexicon returns what a workspace's schema declares, such as "612 effects, 981 triggers, 44 scope types". */
export function describeLexicon(state: State): string {
  const counts = [
    plural(state.effects, "effect"),
    plural(state.triggers, "trigger"),
    plural(state.scopes, "scope type"),
  ];
  return counts.join(", ");
}

/**
 * MARKS holds, for each status of a difference, the mark and color it is drawn with, its word in a count of files
 * ("3 new") and its words in a sentence ("is new"). A count lists the statuses in this order.
 */
export const MARKS: Readonly<Record<string, { mark: string; color: string; count: string; sentence: string }>> = {
  changed: { mark: "~", color: "text-warning", count: "changed", sentence: "changed" },
  added: { mark: "+", color: "text-success", count: "new", sentence: "is new" },
  removed: { mark: "−", color: "text-error", count: "removed", sentence: "was removed" },
};

/** DIFFERENCE_FIELDS holds the label of each listing field a difference can name. A file is labeled by its path. */
const DIFFERENCE_FIELDS: Readonly<Record<string, string>> = {
  title: "Title",
  description: "Description",
  tags: "Tags",
  previews: "Pictures and videos",
  dlc: "Required DLC",
  dependencies: "Required mods",
  version: "Version",
};

/** toDifferenceId returns the key of a difference in a list: its field, then its path for a file. */
export function toDifferenceId({ field, rel = "" }: Difference): string {
  return `${field}:${rel}`;
}

/** describeDifferenceLabel returns the label of what a difference changes: a file's path, else the field's label. */
export function describeDifferenceLabel({ field, rel = "" }: Difference): string {
  return field === "file" ? rel : (DIFFERENCE_FIELDS[field] ?? field);
}

/** describeDifference summarizes a difference: the items added and removed, else the line counts, else the values. */
export function describeDifference({ added, removed, gained, lost, before, after }: Difference): string {
  const items = [...(added ?? []).map((item) => `+${item}`), ...(removed ?? []).map((item) => `−${item}`)];
  const lines = [gained ? `+${gained}` : "", lost ? `−${lost}` : ""].filter(Boolean);
  const values = before || after ? `${before ?? ""} → ${after ?? ""}` : "";
  return (items.length ? items : lines).join(" ") || values;
}
