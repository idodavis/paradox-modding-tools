/**
 * An agent's edits to files: the changes its tool calls report, read from either version's diff, and the undo that
 * gives a file back its text from before them.
 */
import * as workspaceApi from "@services/workspace/service";
import { findModel } from "../pages/ide/editor/models";
import { loadQuery, placeQuery } from "../queries";
import type { Session } from "./agent";

/**
 * A FileEdit is one file a tool call changes, by its path and the file's own name, with its text before and after, or
 * the patch between them and how many lines the patch adds and removes. Its hunks are what an undo takes back.
 */
export type FileEdit = {
  path: string;
  label: string;
  before: string | null;
  after: string | null;
  patch: string;
  added: number;
  removed: number;
  hunks: Hunk[];
  isNew: boolean; // the change made the file
  isUndone: boolean;
};

/** A Hunk is one run of a change: its lines as the file had them before the change, and as it has them after. */
type Hunk = { before: string[]; after: string[] };

/** A DiffContent is a tool call's diff in either version: version 1's old and new text, or version 2's patch. */
export type DiffContent = {
  path?: string;
  oldText?: string | null;
  newText?: string;
  changes?: { path: string; operation?: string }[];
  patch?: { text: string } | null;
};

/** TAB_SPACES is what an agent's diff can write in place of each tab of a file's line. */
const TAB_SPACES = "  ";

/** listEdits lists every change a session's tool calls made, the earliest first. */
export function listEdits(session: Session): FileEdit[] {
  return session.entries.flatMap((entry) => (entry.kind === "tool" ? entry.edits : []));
}

/** listLatestEdits lists the files a session's tool calls changed, each once, by its latest change. */
export function listLatestEdits(session: Session): FileEdit[] {
  return [...new Map(listEdits(session).map((change) => [change.path, change])).values()];
}

/**
 * undoFile gives one file back its text from before the given edits, all of the file's, the latest first, and
 * writes it through Go in its own line endings. A file the earliest change made that ends empty goes to PMT's trash.
 * It throws when the file has unsaved text in the editor, when a change came with no lines, or when a change's lines
 * are not in the file once.
 */
export async function undoFile(edits: FileEdit[]): Promise<void> {
  const path = edits[0]?.path ?? "";
  const place = await loadQuery(placeQuery(path));
  if (place && findModel(place.key)?.unsaved) {
    throw new Error("It has unsaved text in the editor: save or revert it first.");
  }
  const file = await workspaceApi.Read(path);
  let lines = splitLines(file.text.text);
  for (const change of [...edits].reverse()) {
    if (!change.hunks.length) throw new Error("The agent sent no lines for one of its edits.");
    for (const hunk of [...change.hunks].reverse()) lines = revertHunk(lines, hunk);
  }
  if (edits[0]?.isNew && !lines.join("")) {
    await workspaceApi.Delete(path);
    return;
  }
  const eol = file.text.text.includes("\r\n") ? "\r\n" : "\n";
  await workspaceApi.Write(path, { ...file.text, text: lines.join(eol) });
}

/**
 * revertHunk returns a file's lines with one hunk taken back: its lines after the change, found once in the file, in
 * place of its lines before. A hunk whose lines before are in the file once, and its lines after nowhere, is undone
 * already, and the lines stay as they are. It throws when the lines after are in the file more than once, or when
 * neither are. A tab of the file's and the spaces a diff writes for it count as the same.
 */
function revertHunk(lines: string[], hunk: Hunk): string[] {
  const starts = listStarts(lines, hunk.after);
  const [start] = starts;
  if (starts.length === 1 && start !== undefined) {
    const end = start + hunk.after.length;
    return [...lines.slice(0, start), ...restoreLines(hunk, lines.slice(start, end)), ...lines.slice(end)];
  }
  if (!starts.length && hunk.before.length && listStarts(lines, hunk.before).length === 1) return lines;
  throw new Error(starts.length ? "Its edited lines are in it more than once." : "It changed since the agent's edit.");
}

/** listStarts lists where a run of a diff's lines starts in a file's lines, each place it is. */
function listStarts(lines: string[], run: string[]): number[] {
  return lines
    .map((_, start) => start)
    .filter((start) => run.every((line, offset) => isSameLine(lines[start + offset], line)));
}

/** isSameLine reports whether a file's line is a diff's line, each tab of the file's read as a diff's spaces. */
function isSameLine(fileLine: string | undefined, diffLine: string): boolean {
  return fileLine === diffLine || fileLine?.replaceAll("\t", TAB_SPACES) === diffLine;
}

/**
 * restoreLines returns a hunk's lines before the change as the file writes them, from the file's lines that hold its
 * lines after. A line the change kept is the file's own. A line it took out gets the file's tabs back in its indent
 * when the file wrote tabs where the diff wrote spaces.
 */
function restoreLines(hunk: Hunk, fileLines: string[]): string[] {
  const own = new Map(hunk.after.map((line, offset) => [line, fileLines[offset] ?? line]));
  const hasTabs = fileLines.some((line, offset) => line !== hunk.after[offset]);
  const indent = new RegExp(`^(?:${TAB_SPACES})+`);
  return hunk.before.map(
    (line) =>
      own.get(line) ?? (hasTabs ? line.replace(indent, (run) => "\t".repeat(run.length / TAB_SPACES.length)) : line),
  );
}

/**
 * toEdits reads the files a diff of either version changes. A file's own name is its path's last part. A version 1
 * diff is one hunk, its old and new text; a version 2 patch's hunks, and the lines they add and remove, are read
 * from the file's part of it.
 */
export function toEdits(diff: DiffContent): FileEdit[] {
  const label = (path: string): string => path.split(/[\\/]/).at(-1) ?? path;
  const marks = { isUndone: false };
  if (diff.path) {
    const texts = { before: diff.oldText ?? null, after: diff.newText ?? null, patch: "" };
    const hunks = [{ before: splitLines(diff.oldText ?? ""), after: splitLines(diff.newText ?? "") }];
    return [
      {
        path: diff.path,
        label: label(diff.path),
        ...texts,
        added: 0,
        removed: 0,
        hunks,
        isNew: !diff.oldText,
        ...marks,
      },
    ];
  }
  const patch = diff.patch?.text ?? "";
  return (diff.changes ?? []).map((change) => {
    const lines = splitPatch(patch, label(change.path));
    return {
      path: change.path,
      label: label(change.path),
      before: null,
      after: null,
      patch,
      added: lines.filter((line) => line.startsWith("+")).length,
      removed: lines.filter((line) => line.startsWith("-")).length,
      hunks: toHunks(lines),
      isNew: change.operation === "add",
      ...marks,
    };
  });
}

/**
 * splitPatch returns the lines of one file's part of a unified patch, from its first hunk on, by the file's own name:
 * the part whose +++ line names it, or the whole patch when it holds one file. A patch that holds no such part gives
 * none.
 */
function splitPatch(patch: string, label: string): string[] {
  const parts = patch.split(/^(?=--- )/m);
  const filePart = parts.find((part) =>
    part
      .split("\n")
      .find((line) => line.startsWith("+++ "))
      ?.trimEnd()
      .endsWith(label),
  );
  const lines = splitLines(filePart ?? (parts.length === 1 ? patch : ""));
  const first = lines.findIndex((line) => line.startsWith("@@"));
  return first < 0 ? [] : lines.slice(first);
}

/**
 * toHunks reads the hunks of one file's part of a unified patch: each one's lines before the change, its context and
 * the lines it removes, and after, its context and the lines it adds.
 */
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

/** splitLines returns a text's lines, a line that ends in \r\n read as one that ends in \n. An empty text has none. */
function splitLines(text: string): string[] {
  return text ? text.replace(/\r\n/g, "\n").split("\n") : [];
}
