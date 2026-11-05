/** An agent's edits to files: the changes its tool calls report, and the undo that takes them back. */
import * as workspaceApi from "@services/workspace/service";
import type { Session } from "./agent";

/** A FileEdit is the change one tool call makes to one file. */
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

/** listEdits lists every change a session's tool calls made, the earliest first. */
export function listEdits(session: Session): FileEdit[] {
  return session.entries.flatMap((entry) => (entry.kind === "tool" ? entry.edits : []));
}

/** listLatestEdits lists the files a session's tool calls changed, each once, by its latest change. */
export function listLatestEdits(session: Session): FileEdit[] {
  return [...new Map(listEdits(session).map((change) => [change.path, change])).values()];
}

/** undoFile gives a file back the text it had before the given edits, through Go. It rejects with Go's error. */
export function undoFile(edits: FileEdit[]): Promise<void> {
  const hunks = edits.map((edit) => edit.hunks);
  return workspaceApi.RevertHunks(edits[0]?.path ?? "", hunks, !!edits[0]?.isNew);
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
    const lines = splitPatch(patch, fileLabel(change.path));
    return {
      path: change.path,
      label: fileLabel(change.path),
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

/** splitPatch returns the lines of one file's part of a unified patch, or none if no part matches the label. */
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
