/** The sentences and numbers the UI writes from facts. A name or a description from Go is shown as it is. */
import { formatTimeAgo, type UseTimeAgoMessages } from "@vueuse/core";
import { countBy, groupBy, sumBy } from "es-toolkit";

import type { SourceState } from "@internal/schema/models";
import type { Header } from "@internal/texture/models";
import type { Difference } from "@services/steam/models";
import type { State } from "@services/workspace/models";
import type { Finding } from "@services/health/models";
import type { Code } from "@services/lsp/models";

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

/**
 * REASONS holds each reason Go gives a copy, in words: why the game keeps it, how it joins the kept copy, or that no
 * source says which copy the game keeps.
 */
export const REASONS: Readonly<Record<string, string>> = {
  last: "Last file by name",
  first: "First file by name",
  playset: "Lower in the load order",
  path: "Same path, lower in the load order",
  priority: "Higher override priority",
  mode: "By its entry mode",
  merge: "Combined",
  injected: "Injected",
  combined: "Combined",
  replace: "After a replace_path",
  "replace-folder": "In a replace folder",
  unsettled: "Not documented",
};

/** LEVELS holds the name, icon and color of each level of a finding, by Go's level. */
export const LEVELS: Readonly<Record<string, { label: string; icon: string; color: "error" | "warning" | "info" }>> = {
  serious: { label: "Serious", icon: "i-lucide-circle-x", color: "error" },
  warning: { label: "Warning", icon: "i-lucide-circle-alert", color: "warning" },
  info: { label: "Info", icon: "i-lucide-info", color: "info" },
};

/** TOPICS holds each topic of the checks in words, by Go's topic. */
export const TOPICS: Readonly<Record<string, string>> = {
  syntax: "Syntax",
  references: "References",
  values: "Scopes and values",
  localization: "Localization",
  definitions: "Definitions",
  descriptor: "Descriptor",
};

/** LANGUAGES holds each localization language's name in English, by the folder the game keeps it in. */
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

/** LANGUAGE_FACTS and COUNT_FACTS name the facts of a message that hold a language's folder or a count. */
const LANGUAGE_FACTS: readonly string[] = ["language", "header"];
const COUNT_FACTS: readonly string[] = ["count", "have", "of"];

/** describeLanguages returns languages as a select's items, named in English or else by folder. Null gives none. */
export function describeLanguages(languages: readonly string[] | null | undefined): { label: string; value: string }[] {
  return (languages ?? []).map((value) => ({ label: LANGUAGES[value] ?? value, value }));
}

/** describeCount returns a count as the user reads it, such as "8" or "1,350". */
export function describeCount(count: number): string {
  return NUMBERS.format(count);
}

/** plural returns a count with its noun, such as "1 file" or "1,350 files". Without a plural form it adds an s. */
export function plural(count: number, noun: string, plural?: string): string {
  return `${describeCount(count)} ${PLURALS.select(count) === "one" ? noun : (plural ?? `${noun}s`)}`;
}

/** toSentence returns words with a capital first letter, such as "Has landed titles". */
export function toSentence(words: string): string {
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** list joins items into a phrase, such as "a, b and c". It is "" for no items. */
export function list(items: readonly string[]): string {
  return LISTS.format(items);
}

/**
 * matchQuery says whether the texts, joined by spaces, hold the query, ignoring case and the spaces around the query.
 * So a query can span two texts, such as a key and its value. An empty query matches everything.
 */
export function matchQuery(query: string, ...texts: (string | null | undefined)[]): boolean {
  const needle = query.trim().toLowerCase();
  return !needle || texts.join(" ").toLowerCase().includes(needle);
}

/**
 * day returns a date as the user reads it, such as "21 April". It is "" for a time that does not parse or that Go
 * never set.
 */
export function day(date: Date | string): string {
  const parsed = parseTime(date);
  return parsed ? DAYS.format(parsed) : "";
}

/** parseTime returns a time as a Date. It returns undefined for a time that does not parse or that Go never set. */
export function parseTime(time: Date | string): Date | undefined {
  const parsed = new Date(time);
  // Go writes a time it never set as the year 1, and one that does not parse has no year
  return parsed.getFullYear() > 1 ? parsed : undefined;
}

/**
 * describeAge returns how long ago a time was, short as "4 min ago" or long as "4 minutes ago". It is "" for a time
 * that does not parse or that Go never set.
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

/**
 * diagnosticMessage fills a code's message template with a diagnostic's facts. Languages are named in English and
 * counts get thousands separators. A placeholder with no fact stays as written.
 */
export function diagnosticMessage(template: string, facts: Record<string, string | undefined>): string {
  return template.replace(/\{(\w+)\}/g, (placeholder, key: string) => {
    const value = facts[key];
    if (value === undefined) return placeholder;
    if (LANGUAGE_FACTS.includes(key)) return describeLanguages([value])[0]?.label ?? value;
    return COUNT_FACTS.includes(key) && value !== "" && !Number.isNaN(Number(value))
      ? describeCount(Number(value))
      : value;
  });
}

/**
 * describeFinding returns the message of a finding or an editor diagnostic. It is the code's id if the app does not
 * list the code.
 */
export function describeFinding(
  finding: Pick<Finding, "code" | "facts">,
  codes: readonly Code[] | null | undefined,
): string {
  const code = codes?.find((one) => one.id === finding.code);
  return code ? diagnosticMessage(code.message, finding.facts ?? {}) : finding.code;
}

/** describeCheck returns the name of a check. It is the code itself if the app does not list it. */
export function describeCheck(code: string, codes: readonly Code[] | null | undefined): string {
  return codes?.find((one) => one.id === code)?.label ?? code;
}

/** A FindingRow is a finding with its index in its list. */
export type FindingRow = { finding: Finding; index: number };

/** listFindingRows pairs each finding with its index. */
export function listFindingRows(findings: readonly Finding[]): FindingRow[] {
  return findings.map((finding, index) => ({ finding, index }));
}

/** listProblems returns each mod's own problems among a report's findings, by mod id. Language coverage is left out. */
export function listProblems(findings: readonly Finding[]): Record<string, Finding[]> {
  return groupBy(
    findings.filter((finding) => finding.question === "health" && finding.code !== "coverage"),
    findOwner,
  );
}

/** toRepeatKey returns the key a finding shares with each finding of the same check, name, mod and message. */
export function toRepeatKey(finding: Finding, codes: readonly Code[] | null | undefined): string {
  return [finding.code, findOwner(finding), finding.name, describeFinding(finding, codes)].join("\n");
}

/** findOwner returns the id of the mod a finding is about. It is the first mod it concerns, else its origin. */
export function findOwner(finding: Finding): string {
  return finding.mods?.[0] ?? finding.origin;
}

/**
 * describeDetail returns a finding's second line. For a problem or a log row it is its place, such as "a.txt:12". For
 * a compatibility finding it is the database and why the game keeps its copy. It is "" if there is nothing to add.
 */
export function describeDetail(finding: Finding): string {
  const site = finding.sites?.find((one) => one.kept) ?? finding.sites?.[0];
  if (finding.question !== "compatibility") {
    if (site) return `${site.place.rel}:${site.range.startLineNumber}`;
    return finding.facts?.named ? `Names ${finding.facts.named}` : "";
  }
  return [finding.database, REASONS[site?.reason ?? ""]].filter(Boolean).join(" · ");
}

/** toSiteKey returns the key of a finding's first site, such as "path:line". It is "" for a finding with no site. */
export function toSiteKey(finding: Finding): string {
  const site = finding.sites?.[0];
  return site ? `${site.place.path}:${site.range.startLineNumber}` : "";
}

/** describeLogLine returns a log row's message as one line, its lines trimmed and joined with " · ". */
export function describeLogLine(finding: Finding): string {
  return (finding.facts?.message ?? "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .join(" · ");
}

/** PROBLEM_LEVELS holds the levels that count as problems. Info does not. */
export const PROBLEM_LEVELS: readonly string[] = ["serious", "warning"];

/** countLevels returns how many of the given findings or problems there are of each level. */
export function countLevels(findings: readonly { level: string }[]): Record<string, number> {
  return { serious: 0, warning: 0, info: 0, ...countBy(findings, (finding) => finding.level) };
}

/** countLines returns how many lines of the game's log a set of rows stands for, repeats included. */
export function countLines(findings: readonly Finding[]): number {
  return sumBy(findings, (finding) => finding.count);
}

/**
 * describeTime returns a time such as "today at 14:02" or "21 April at 14:02". It is "" for a time that does not parse
 * or that Go never set.
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

/** describeDumps describes the script_docs a workspace reads, and whether they are the game's own and current. */
export function describeDumps(dumps: SourceState): { current: boolean; label: string } {
  if (dumps.origin === "install" && !dumps.stale) return { current: true, label: "script_docs loaded" };
  if (dumps.origin === "install") return { current: false, label: "script_docs older than the game" };
  if (dumps.origin === "shipped") return { current: false, label: "Using PMT's copy of script_docs" };
  return { current: false, label: "No script_docs" };
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

/** A DifferenceRow is one difference the next upload makes on Steam, as a row of a list. */
export type DifferenceRow = {
  id: string;
  label: string;
  summary: string;
  mark: string;
  color: string;
  sentence: string;
};

/**
 * MARKS holds, for each status Go gives a difference, its mark and color, the word a count of files uses, and the
 * words a sentence uses. The rows are in the order a count lists them.
 */
export const MARKS: Readonly<Record<string, { mark: string; color: string; count: string; sentence: string }>> = {
  changed: { mark: "~", color: "text-warning", count: "changed", sentence: "changed" },
  added: { mark: "+", color: "text-success", count: "new", sentence: "is new" },
  removed: { mark: "−", color: "text-error", count: "removed", sentence: "was removed" },
};

/** DIFFERENCE_FIELDS holds each field of a listing that a difference names, in words. A file is named by its path. */
const DIFFERENCE_FIELDS: Readonly<Record<string, string>> = {
  title: "Title",
  description: "Description",
  tags: "Tags",
  previews: "Pictures and videos",
  dlc: "Required DLC",
  dependencies: "Required mods",
  version: "Version",
};

/** describeDifference returns a difference as a row of a list, with a short summary of what changes. */
export function describeDifference(difference: Difference): DifferenceRow {
  const { field, rel = "", added, removed, gained, lost, before, after } = difference;
  const items = [...(added ?? []).map((one) => `+${one}`), ...(removed ?? []).map((one) => `−${one}`)];
  const lines = [gained ? `+${gained}` : "", lost ? `−${lost}` : ""].filter(Boolean);
  const values = before || after ? `${before ?? ""} → ${after ?? ""}` : "";
  return {
    id: `${field}:${rel}`,
    label: field === "file" ? rel : DIFFERENCE_FIELDS[field],
    summary: (items.length ? items : lines).join(" ") || values,
    ...MARKS[difference.status],
  };
}
