/**
 * An agent's edits to files: the changes its tool calls report, read from either version's diff, and the undo that
 * gives a file back its text from before them.
 */
import * as workspaceApi from "@services/workspace/service";
import type { Session } from "./agent";

/** A FileEdit is the change one tool call makes to one file. An undo takes back its hunks. */
export type FileEdit = {
  path: string;
  label: string;
  before: string | null;
  after: string | null;
  patch: string;
  added: number;
  removed: number;
  hunks: Hunk[];
  isNew: boolean; // the change creates the file
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

/** listEdits lists every change a session's tool calls made, the earliest first. */
export function listEdits(session: Session): FileEdit[] {
  return session.entries.flatMap((entry) => (entry.kind === "tool" ? entry.edits : []));
}

/** listLatestEdits lists the files a session's tool calls changed, each once, by its latest change. */
export function listLatestEdits(session: Session): FileEdit[] {
  return [...new Map(listEdits(session).map((change) => [change.path, change])).values()];
}

/**
 * undoFile gives a file back the text it had before the given edits, through Go. The edits are all of that file's,
 * earliest first. It rejects with Go's error if the file has unsaved text in the editor, an edit has no lines, or a
 * hunk's lines are not in the file exactly once.
 */
export function undoFile(edits: FileEdit[]): Promise<void> {
  const hunks = edits.map((edit) => edit.hunks);
  return workspaceApi.RevertHunks(edits[0]?.path ?? "", hunks, !!edits[0]?.isNew);
}

/**
 * toEdits returns the files a diff changes. A file's label is the last part of its path. A version 1 diff gives one
 * hunk of old and new text. A version 2 patch gives each file's hunks and its counts of added and removed lines.
 */
export function toEdits(diff: DiffContent): FileEdit[] {
  const label = (path: string): string => path.split(/[\\/]/).at(-1) ?? path;
  if (diff.path) {
    return [
      {
        path: diff.path,
        label: label(diff.path),
        before: diff.oldText ?? null,
        after: diff.newText ?? null,
        patch: "",
        added: 0,
        removed: 0,
        hunks: [{ before: splitLines(diff.oldText ?? ""), after: splitLines(diff.newText ?? "") }],
        isNew: !diff.oldText,
        isUndone: false,
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
      isUndone: false,
    };
  });
}

/**
 * splitPatch returns the lines of one file's part of a unified patch, from its first hunk on. The part is the one
 * whose +++ line ends with the label, or the whole patch if it has only one part. It returns no lines if no part
 * matches.
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
 * toHunks returns the hunks of the lines of one file's patch. A hunk's before lines are its context and removed
 * lines, and its after lines are its context and added lines.
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

/** splitLines returns a text's lines, reading \r\n as \n. An empty text has no lines. */
function splitLines(text: string): string[] {
  return text ? text.replace(/\r\n/g, "\n").split("\n") : [];
}
