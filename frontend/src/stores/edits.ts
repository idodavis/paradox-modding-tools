/**
 * An agent's edits to files: the changes its tool calls report, and the review
 * Go starts from them.
 */
import type { Place } from "@services/workspace/models";
import * as workspaceApi from "@services/workspace/service";
import { confirm } from "../lib/feedback";
import { plural } from "../lib/format";
import type { Entry } from "./agent";

/** A FileEdit is the change one tool call makes to one file. */
export type FileEdit = {
  path: string;
  label: string;
  patch: string;
  added: number;
  removed: number;
  hunks: Hunk[];
  isNew: boolean; // the change creates the file
  /** reviewError is why Go could not start the file's review; "" if it did. */
  reviewError: string;
};

/** A Hunk is one run of a change, with its lines before and after. */
type Hunk = { before: string[]; after: string[] };

/** A DiffContent is a tool call's diff in either protocol version. */
export type DiffContent = {
  path?: string;
  oldText?: string | null;
  newText?: string;
  changes?: { path: string; operation?: string }[];
  patch?: { text: string } | null;
};

/**
 * trackEdits has Go start the review of a file, given the edits one tool call
 * made to it. It rejects with Go's error.
 */
export function trackEdits(edits: FileEdit[]): Promise<void> {
  const hunks = edits.map((edit) => edit.hunks);
  return workspaceApi.TrackEdits(edits[0]?.path ?? "", hunks, !!edits[0]?.isNew);
}

/**
 * listChanges returns the files the given entries' tool calls changed, each
 * once, with the lines all its changes add and remove.
 */
export function listChanges(entries: Entry[]): FileEdit[] {
  const changes = new Map<string, FileEdit>();
  for (const edit of entries.flatMap((entry) => (entry.kind === "tool" ? entry.edits : []))) {
    const known = changes.get(edit.path);
    const sum = known && { added: known.added + edit.added, removed: known.removed + edit.removed };
    changes.set(edit.path, known ? { ...known, ...sum } : edit);
  }

  return [...changes.values()];
}

/**
 * confirmUndo asks whether to undo the agents' edits to the given files under
 * review, and reports whether the user said yes.
 */
export async function confirmUndo(places: Place[]): Promise<boolean> {
  const subject = places.length === 1 ? (places[0]?.rel ?? "") : plural(places.length, "file");
  const answer = await confirm.open({
    title: "Undo the agent's changes?",
    body:
      `The edits to ${subject} are taken back, except for the changes you kept. ` +
      "A file an agent created goes to PMT's trash.",
    confirm: "Undo all",
    destructive: true,
  });

  return answer === true;
}

/** fileLabel returns a file's name without its folders. */
export function fileLabel(path: string): string {
  return path.split(/[\\/]/).at(-1) ?? path;
}

/** toEdits returns the files a diff changes. */
export function toEdits(diff: DiffContent): FileEdit[] {
  if (diff.path) {
    return [
      {
        path: diff.path,
        label: fileLabel(diff.path),
        patch: "",
        added: 0,
        removed: 0,
        hunks: [{ before: splitLines(diff.oldText ?? ""), after: splitLines(diff.newText ?? "") }],
        isNew: !diff.oldText,
        reviewError: "",
      },
    ];
  }
  const patch = diff.patch?.text ?? "";
  return (diff.changes ?? []).map((change) => {
    const lines = splitPatch(patch, change.path);
    return {
      path: change.path,
      label: fileLabel(change.path),
      patch,
      added: lines.filter((line) => line.startsWith("+")).length,
      removed: lines.filter((line) => line.startsWith("-")).length,
      hunks: toHunks(lines),
      isNew: change.operation === "add",
      reviewError: "",
    };
  });
}

/**
 * splitPatch returns the lines of one file's part of a unified patch, or none
 * if no part names the file. A part names it by its whole path, or by its
 * name under git's a/ and b/ folders.
 */
function splitPatch(patch: string, path: string): string[] {
  const parts = patch.split(/^(?=--- )/m);
  const filePart = parts.find((part) => {
    const header = part.split("\n").find((line) => line.startsWith("+++ "));
    if (!header) return false;
    // git ends a name that holds a space with a tab.
    const named = unquoteGitPath(header.slice(4).replace(/\t?\r?$/, ""));

    return named === path || fileLabel(named) === fileLabel(path);
  });
  const lines = splitLines(filePart ?? (parts.length === 1 ? patch : ""));
  const first = lines.findIndex((line) => line.startsWith("@@"));
  return first < 0 ? [] : lines.slice(first);
}

/** GIT_ESCAPES holds the byte that each of git's letter escapes stands for. */
const GIT_ESCAPES: Record<string, number> = { a: 7, b: 8, t: 9, n: 10, v: 11, f: 12, r: 13, '"': 34, "\\": 92 };

/**
 * unquoteGitPath returns a path from a patch's header without git's quotes:
 * "C:\\mod\\a b.txt" is C:\mod\a b.txt. A path git did not quote is returned
 * as it is. The bytes git writes as octal escapes are read as UTF-8.
 */
function unquoteGitPath(header: string): string {
  if (header.length < 2 || !header.startsWith('"') || !header.endsWith('"')) return header;

  const chars = [...header.slice(1, -1)];
  const encoder = new TextEncoder();
  const bytes: number[] = [];
  for (let index = 0; index < chars.length; index++) {
    const char = chars[index] ?? "";
    if (char !== "\\") {
      bytes.push(...encoder.encode(char));
      continue;
    }

    const octal = /^[0-7]{3}/.exec(chars.slice(index + 1, index + 4).join(""));
    if (octal) {
      bytes.push(parseInt(octal[0], 8));
      index += 3;
      continue;
    }

    const next = chars[index + 1] ?? "";
    bytes.push(GIT_ESCAPES[next] ?? next.charCodeAt(0));
    index++;
  }

  return new TextDecoder().decode(new Uint8Array(bytes));
}

/** toHunks returns the hunks of the lines of one file's patch. */
function toHunks(lines: string[]): Hunk[] {
  const hunks: Hunk[] = [];
  for (const line of lines) {
    const hunk = hunks.at(-1);
    if (line.startsWith("@@")) hunks.push({ before: [], after: [] });
    else if (line.startsWith("-")) hunk?.before.push(line.slice(1));
    else if (line.startsWith("+")) hunk?.after.push(line.slice(1));
    else if (line.startsWith(" ")) {
      hunk?.before.push(line.slice(1));
      hunk?.after.push(line.slice(1));
    }
  }
  return hunks;
}

/** splitLines returns a text's lines. An empty text has none. */
function splitLines(text: string): string[] {
  return text ? text.replace(/\r\n/g, "\n").split("\n") : [];
}
