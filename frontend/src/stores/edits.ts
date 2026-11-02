/**
 * An agent's edits to files: the changes its tool calls report, read from either version's diff, and the undo that
 * gives a file back its text from before them.
 */
import * as workspaceApi from "@services/workspace/service";
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

/** listEdits lists every change a session's tool calls made, the earliest first. */
export function listEdits(session: Session): FileEdit[] {
  return session.entries.flatMap((entry) => (entry.kind === "tool" ? entry.edits : []));
}

/** listLatestEdits lists the files a session's tool calls changed, each once, by its latest change. */
export function listLatestEdits(session: Session): FileEdit[] {
  return [...new Map(listEdits(session).map((change) => [change.path, change])).values()];
}

/**
 * undoFile gives one file back its text from before the given edits, all of the file's, the earliest first, through
 * Go. It rejects with Go's error when the file has unsaved text in the editor, an edit came with no lines, or a
 * change's lines are not in the file once.
 */
export function undoFile(edits: FileEdit[]): Promise<void> {
  const hunks = edits.map((edit) => edit.hunks);
  return workspaceApi.RevertHunks(edits[0]?.path ?? "", hunks, !!edits[0]?.isNew);
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
