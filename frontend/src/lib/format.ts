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
 * REASONS holds each reason Go gives a copy, in words. A reason says why the game keeps a copy, how a copy joins the
 * kept one, or that no source says which copy the game keeps. A reason a contest's winner can have also says at length
 * why it wins, as a template the contest's facts fill.
 */
export const REASONS: Readonly<Record<string, { label: string; why?: string }>> = {
  last: {
    label: "Last file by name",
    why:
      "The game reads every file in {folder} in order of file name and keeps the copy it reads last. {winner}'s " +
      "file, {file}, comes after {beaten}'s {beatenFile}, so its copy wins. Changing the load order doesn't change this.",
  },
  first: {
    label: "First file by name",
    why:
      "The game reads every file in {folder} in order of file name and keeps the first copy it reads. {winner}'s " +
      "file, {file}, comes before {beaten}'s {beatenFile}, so its copy wins. Changing the load order doesn't change this.",
  },
  playset: {
    label: "Lower in the load order",
    why:
      "The game keeps the copy from the mod lowest in the load order. {winner} is lower than {beaten}, so its copy " +
      "wins. Moving a mod in the load order changes this.",
  },
  path: {
    label: "Same path, lower in the load order",
    why:
      "Another copy would win, but it is in a file the game never reads: a file at the same path, lower in the load " +
      "order, hides it. Of the copies the game reads, it keeps {winner}'s.",
  },
  priority: {
    label: "Higher override priority",
    why:
      "The game keeps the copy with the highest override priority, whatever the file names or the load order. " +
      "{winner}'s copy has the highest, so it wins.",
  },
  mode: {
    label: "By its entry mode",
    why:
      "The game applies entry modes, such as REPLACE:, after every plain copy, and the last one that creates or " +
      "replaces the definition wins: {winner}'s, in {file}.",
  },
  merge: { label: "Combined" },
  injected: { label: "Injected" },
  combined: { label: "Combined" },
  replace: {
    label: "After a replace_path",
    why:
      "The game's own copy would win, but a mod's replace_path makes the game skip the game's files in that folder. " +
      "Of the copies the game reads, it keeps {winner}'s.",
  },
  "replace-folder": { label: "In a replace folder" },
  unsettled: { label: "Not documented" },
};

/** A Level is how the app shows one level of a finding, and the words that count it. */
export type Level = {
  label: string;
  icon: string;
  color: "error" | "warning" | "yellow" | "info";
  noun: string;
  nouns: string;
};

/** LEVELS holds each level of a finding, by Go's level, the worst first. */
export const LEVELS: Readonly<Record<string, Level>> = {
  critical: {
    label: "Critical",
    icon: "i-lucide-circle-x",
    color: "error",
    noun: "critical problem",
    nouns: "critical problems",
  },
  error: { label: "Error", icon: "i-lucide-circle-alert", color: "warning", noun: "error", nouns: "errors" },
  warning: { label: "Warning", icon: "i-lucide-triangle-alert", color: "yellow", noun: "warning", nouns: "warnings" },
  info: { label: "Info", icon: "i-lucide-info", color: "info", noun: "info", nouns: "info" },
};

/** PROBLEM_LEVELS holds the levels that count as problems, the worst first. Info does not. */
export const PROBLEM_LEVELS: readonly string[] = ["critical", "error", "warning"];

/** FIX_LEVELS holds the levels a mod needs a fix for, where the game loses something. A warning may be fine. */
export const FIX_LEVELS: readonly string[] = ["critical", "error"];

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

/** LANGUAGE_FACTS names the facts of a message that hold a language's folder. */
const LANGUAGE_FACTS: readonly string[] = ["language", "header"];

/** A Part is one piece of a message: plain text, or a fact drawn in its style. */
export type Part = { text: string; style: "" | "mod" | "code" | "count" | "path" };

/** FACT_STYLES holds the style of each fact a message draws apart, by the fact's key. Any other fact is plain text. */
const FACT_STYLES: Readonly<Record<string, Part["style"]>> = {
  mod: "mod",
  other: "mod",
  winner: "mod",
  beaten: "mod",
  name: "code",
  key: "code",
  callee: "code",
  namespace: "code",
  file: "path",
  folder: "path",
  beatenFile: "path",
  count: "count",
  have: "count",
  of: "count",
  uses: "count",
  removed: "count",
  total: "count",
};

/** SEPARATOR is the part between two pieces of a line. */
const SEPARATOR: Part = { text: " · ", style: "" };

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

/**
 * splitError returns an error's message in two parts: its first line, which says what failed, and the lines after it,
 * at most the given count of them. Without a count it keeps every line. The second part is "" for a message of one
 * line.
 */
export function splitError(error: unknown, count = Infinity): [summary: string, details: string] {
  const message = error instanceof Error ? error.message : String(error);
  const [summary = "", ...details] = message.split("\n");
  return [summary, details.slice(0, count).join("\n")];
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
 * splitMessage fills a code's message template with a diagnostic's facts, as parts. Languages are named in English
 * and counts get thousands separators. The name of a file is a path. A placeholder with no fact stays as written.
 */
export function splitMessage(template: string, facts: Record<string, string | undefined>): Part[] {
  return template.split(/(\{\w+\})/).flatMap((piece): Part[] => {
    const key = /^\{(\w+)\}$/.exec(piece)?.[1] ?? "";
    const value = facts[key];
    if (!piece) return [];
    if (value === undefined) return [{ text: piece, style: "" }];
    if (LANGUAGE_FACTS.includes(key)) return [{ text: describeLanguages([value])[0]?.label ?? value, style: "" }];
    const style = key === "name" && facts.database === "file" ? "path" : (FACT_STYLES[key] ?? "");
    const numeric = style === "count" && value !== "" && !Number.isNaN(Number(value));
    return [{ text: numeric ? describeCount(Number(value)) : value, style }];
  });
}

/** joinParts returns parts as one plain sentence. */
export function joinParts(parts: readonly Part[]): string {
  return parts.map((part) => part.text).join("");
}

/** diagnosticMessage fills a code's message template with a diagnostic's facts, as splitMessage does, as text. */
export function diagnosticMessage(template: string, facts: Record<string, string | undefined>): string {
  return joinParts(splitMessage(template, facts));
}

/**
 * splitFinding returns the message of a finding or an editor diagnostic, as parts. Where a mod the finding's mod does
 * not depend on defines the name, it is the code's elsewhere message. It is the code's id if the app does not list the
 * code.
 */
export function splitFinding(
  finding: Pick<Finding, "code" | "facts">,
  codes: readonly Code[] | null | undefined,
): Part[] {
  const code = codes?.find((one) => one.id === finding.code);
  const facts = finding.facts ?? {};
  if (!code) return [{ text: finding.code, style: "" }];
  return splitMessage(code.elsewhere && facts.other ? code.elsewhere : code.message, facts);
}

/** describeFinding returns the message of a finding or an editor diagnostic, as splitFinding does, as text. */
export function describeFinding(
  finding: Pick<Finding, "code" | "facts">,
  codes: readonly Code[] | null | undefined,
): string {
  return joinParts(splitFinding(finding, codes));
}

/** describeTitle returns the thing a finding is about, as a part: its name, or the path of a file. */
export function describeTitle(finding: Finding): Part[] {
  return [{ text: finding.name, style: finding.database === "file" ? "path" : "" }];
}

/**
 * describeBrief returns the line under a finding's name in a list, as parts. It says what kind of thing the name is,
 * how many names a replaced file leaves out that are still used and how often, the mod that alone defines the name,
 * and why the game keeps a copy. It is empty if the finding holds none of these.
 */
export function describeBrief(finding: Finding): Part[] {
  const site = finding.sites?.find((one) => one.kept) ?? finding.sites?.[0];
  const facts = finding.facts ?? {};
  const hasUses = !!finding.uses?.length;
  const pieces: Part[][] = [
    finding.database && finding.database !== "file" ? [{ text: finding.database, style: "" }] : [],
    hasUses
      ? [
          ...splitMessage("{count}", facts),
          { text: ` ${facts.count === "1" ? "name" : "names"} still used · `, style: "" },
          ...splitMessage("{uses} uses", facts),
        ]
      : [],
    facts.other
      ? [
          { text: "only in ", style: "" },
          { text: facts.other, style: "mod" },
        ]
      : [],
    REASONS[site?.reason ?? ""] && !hasUses ? [{ text: REASONS[site?.reason ?? ""]?.label ?? "", style: "" }] : [],
  ];
  return pieces.filter((piece) => piece.length).flatMap((piece, index) => [...(index ? [SEPARATOR] : []), ...piece]);
}

/**
 * toWeight returns how bad a finding is among its check's: how often the names a replaced file leaves out are still
 * used, else 1.
 */
export function toWeight(finding: Finding): number {
  return Number(finding.facts?.uses) || 1;
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
 * describeDetail returns a finding's place, such as "a.txt:12": its kept site's, else its first site's. A log row with
 * no site says what it names instead. It is "" if there is nothing to add.
 */
export function describeDetail(finding: Finding): string {
  const site = finding.sites?.find((one) => one.kept) ?? finding.sites?.[0];
  if (site) return `${site.place.rel}:${site.range.startLineNumber}`;
  return finding.facts?.named ? `Names ${finding.facts.named}` : "";
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

/**
 * splitLogLine returns a log row's message as one line, as describeLogLine does, as parts. The name or file that
 * ties the row to a mod is drawn apart where the message writes it.
 */
export function splitLogLine(finding: Finding): Part[] {
  const line = describeLogLine(finding);
  const identifier = finding.facts?.named ?? "";
  const start = identifier ? line.indexOf(identifier) : -1;
  if (start < 0) return [{ text: line, style: "" }];
  return [
    { text: line.slice(0, start), style: "" },
    { text: identifier, style: finding.facts?.why === "file" ? "path" : "code" },
    { text: line.slice(start + identifier.length), style: "" },
  ];
}

/** countLevels returns how many of the given findings or problems there are of each level. A level with none has 0. */
export function countLevels(findings: readonly { level: string }[]): Record<string, number> {
  return {
    ...Object.fromEntries(Object.keys(LEVELS).map((level) => [level, 0])),
    ...countBy(findings, (finding) => finding.level),
  };
}

/**
 * describeLevels returns the count of each of the given levels in words, in their order, such as "235 critical
 * problems". A level with none is left out.
 */
export function describeLevels(counts: Record<string, number>, levels: readonly string[]): string[] {
  return levels.flatMap((level) => {
    const row = LEVELS[level];
    const count = counts[level] ?? 0;
    return row && count ? [plural(count, row.noun, row.nouns)] : [];
  });
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
