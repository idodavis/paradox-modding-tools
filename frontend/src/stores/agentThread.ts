/**
 * A thread: what an agent's updates build for one conversation, a session's
 * own or a subagent's: its transcript, its terminals and its checklists.
 */
import type * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import type * as unstable from "@agentclientprotocol/sdk/experimental/v2/unstable";
import { reactive } from "vue";

import type { Diagnostic } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import type { FormProperty } from "./agentRequests";
import { fileLabel, toEdits, type DiffContent, type FileEdit } from "./edits";

/** An Entry is one item of a thread's transcript. */
export type Entry =
  | {
      kind: "user";
      id: string;
      text: string;
      chips: string[];
      cursor?: string; // the agent's id for the point before the prompt, where it can copy the session from
    }
  | { kind: "agent" | "thought"; id: string; text: string }
  | {
      kind: "tool";
      id: string;
      title: string;
      status: string;
      type: string;
      paths: string[];
      edits: FileEdit[];
      text: string;
      terminals: string[];
      canCancel: boolean; // whether the agent takes a stop of this call alone
    }
  | {
      kind: "permission";
      id: string;
      title: string;
      permit: string; // the agent and the tool, as the setting of tools allowed always writes them
      options: { id: string; name: string; kind: string }[];
      plan?: string; // the id of the plan document the request asks to start on, if it asks that
    }
  | { kind: "link"; id: string; message: string; url: string }
  | { kind: "form"; id: string; message: string; properties: FormProperty[] } // questions the agent asks the user to answer
  | { kind: "notice"; id: string; text: string } // what PMT tells the user in the transcript
  | { kind: "compaction"; id: string; status: string; text: string } // the agent shortening its context; text is its summary
  | { kind: "subagent"; id: string } // where the agent started the subagent that has this id
  | { kind: "plan"; id: string; plan: string; isUpdate: boolean } // where the agent wrote or changed a plan document
  | {
      kind: "turn"; // where a turn ended; its id is the one Go keeps its files' texts under
      id: string;
      duration: number; // how long it took, in milliseconds
      tokens: number; // the tokens it used, 0 where the agent reports none
      cost: string; // what it cost, "" where the agent reports no cost
      isUnfinished: boolean; // whether it was stopped or failed
      paths: string[]; // the files it changed that went under review
      problems: FileProblems[]; // the problems it added, by file, filled in once Go has judged the files
    };

/** A FileProblems is the problems an agent's turn added to one file. */
export type FileProblems = { place: Place; problems: Diagnostic[] };

/** A ChecklistStep is one line of a checklist an agent keeps. */
export type ChecklistStep = { text: string; status: string };

/** A Checklist is a list of steps an agent keeps under an id of its own. */
export type Checklist = { id: string; steps: ChecklistStep[] };

/** A Terminal is a command an agent ran, with what it printed so far. */
export type Terminal = {
  id: string;
  command: string;
  output: string;

  /**
   * exitCode is the command's exit code once it has ended. It is null while
   * the command runs, and stays null if the agent sends none.
   */
  exitCode: number | null;
  isDone: boolean;

  /** canCancel is whether the agent takes a stop of this command alone. */
  canCancel: boolean;

  /**
   * startedAt and endedAt are when the page first heard of the command and
   * when it ended, in milliseconds. endedAt is 0 while it runs.
   */
  startedAt: number;
  endedAt: number;
};

/** A Thread is what an agent's updates build for one conversation. */
export type Thread = { entries: Entry[]; terminals: Record<string, Terminal>; checklists: Checklist[] };

/**
 * A MessageUpdate is what one kind of update about a message writes: an
 * entry's kind, and a whole text or a chunk.
 */
type MessageUpdate = { kind: "user" | "agent" | "thought"; isWhole: boolean };

/**
 * A PlanUpdate is a plan as either version of the protocol sends it: version
 * 1 its entries alone, version 2 a checklist or a document under an id.
 */
export type PlanUpdate = { entries?: v2.PlanEntry[]; plan?: unstable.PlanUpdateContent };

/** IMAGE_LABEL is what the transcript shows for an image a prompt carried. */
export const IMAGE_LABEL = "Image";

/** CONTROL_CODES matches a terminal's color and cursor codes. */
const CONTROL_CODES = /\u001b\[[0-9;?]*[ -/]*[@-~]|\u001b\][^\u0007]*\u0007/g;

/** MESSAGES holds what each kind of update about a message writes. */
export const MESSAGES: Partial<Record<string, MessageUpdate>> = {
  user_message_chunk: { kind: "user", isWhole: false },
  user_message: { kind: "user", isWhole: true },
  agent_message_chunk: { kind: "agent", isWhole: false },
  agent_message: { kind: "agent", isWhole: true },
  agent_thought_chunk: { kind: "thought", isWhole: false },
  agent_thought: { kind: "thought", isWhole: true },
};

/**
 * applyMessage applies an update about a message to its entry, making the
 * entry on the first update.
 */
export function applyMessage(thread: Thread, update: v2.ContentChunk | v2.UserMessage, row: MessageUpdate): void {
  if (row.isWhole && !("content" in update)) return;
  const blocks = row.isWhole ? ((update as v2.UserMessage).content ?? []) : [(update as v2.ContentChunk).content];
  const id = update.messageId ?? "";
  // An update without an id belongs to the last entry, if that has the kind.
  const candidates = id ? thread.entries : thread.entries.slice(-1);
  let entry = candidates.find((candidate) => candidate.kind === row.kind && (!id || candidate.id === id)) as
    Extract<Entry, { kind: MessageUpdate["kind"] }> | undefined;
  if (!entry) {
    const key = id || `${row.kind}${thread.entries.length}`;
    entry =
      row.kind === "user" ? { kind: "user", id: key, text: "", chips: [] } : { kind: row.kind, id: key, text: "" };
    thread.entries.push(entry);
    entry = thread.entries.at(-1) as typeof entry;
  }
  if (row.isWhole) entry.text = "";
  if (row.isWhole && entry.kind === "user") entry.chips = [];
  for (const block of blocks) {
    if (entry.kind === "user" && block.type === "resource_link") entry.chips.push((block as v2.ResourceLink).name);
    else if (entry.kind === "user" && block.type === "image") entry.chips.push(IMAGE_LABEL);
    else if (entry.kind === "user" && block.type === "resource")
      entry.chips.push(fileLabel(decodeURI((block as v2.EmbeddedResource).resource.uri)));
    else entry.text += (row.isWhole && entry.text ? "\n\n" : "") + toText(block);
  }
}

/**
 * applyToolCall applies an update about a tool call to its entry, making the
 * entry on the first update. It returns the entry, and whether this update
 * is the one that completed the call.
 */
export function applyToolCall(
  thread: Thread,
  update: v2.ToolCallUpdate | v2.ToolCallContentChunk,
): { entry: Extract<Entry, { kind: "tool" }>; isNewlyDone: boolean } {
  let entry = thread.entries.find((candidate) => candidate.kind === "tool" && candidate.id === update.toolCallId);
  if (entry?.kind !== "tool") {
    entry = reactive<Extract<Entry, { kind: "tool" }>>({
      kind: "tool",
      id: update.toolCallId,
      title: "",
      status: "pending",
      type: "other",
      paths: [],
      edits: [],
      text: "",
      terminals: [],
      canCancel: false,
    });
    thread.entries.push(entry);
  }
  const wasDone = entry.status === "completed";
  const whole =
    "title" in update || "status" in update || Array.isArray(update.content) ? (update as v2.ToolCallUpdate) : null;
  if (whole?.title) entry.title = whole.title;
  if (whole?.status) entry.status = whole.status;
  if (whole?.kind) entry.type = whole.kind;
  if (whole?.capabilities) entry.canCancel = !!whole.capabilities.cancel;
  if (Array.isArray(update.content)) Object.assign(entry, { text: "", edits: [], terminals: [] });
  const contents = [update.content ?? []].flat(); // the update's content is one item, a list, or absent
  for (const content of contents) {
    if (content.type === "content") entry.text += toText((content as { content: v2.ContentBlock }).content);
    else if (content.type === "diff") entry.edits.push(...toEdits(content as unknown as DiffContent));
    else if (content.type === "terminal") entry.terminals.push((content as { terminalId: string }).terminalId);
  }
  const paths = (whole?.locations ?? []).map((location) => location.path);
  entry.paths = [...new Set([...entry.paths, ...paths, ...entry.edits.map((edit) => edit.path)])];

  return { entry, isNewlyDone: entry.status === "completed" && !wasDone };
}

/**
 * applyChecklist stores a checklist an agent sent, in place of the one it kept
 * under the same id. Version 1 of the protocol sends one checklist with no
 * id.
 */
export function applyChecklist(thread: Thread, update: PlanUpdate): void {
  const checklist = (update.plan ?? { planId: "", entries: update.entries }) as Partial<v2.PlanItems>;
  const steps = (checklist.entries ?? []).map((step) => ({ text: step.content, status: step.status }));
  const next = { id: checklist.planId ?? "", steps };
  const index = thread.checklists.findIndex((other) => other.id === next.id);
  if (index < 0) thread.checklists.push(next);
  else thread.checklists[index] = next;
}

/**
 * applyTerminal applies an update about a terminal: its command, the whole
 * of its output so far, its end, and whether it can be stopped.
 */
export function applyTerminal(thread: Thread, update: v2.TerminalUpdate): void {
  const terminal = resolveTerminal(thread, update.terminalId);
  if (update.command) terminal.command = update.command;
  if (update.output) terminal.output = decode(update.output.data);
  if (update.capabilities) terminal.canCancel = !!update.capabilities.cancel;
  if (update.exitStatus)
    Object.assign(terminal, { isDone: true, exitCode: update.exitStatus.exitCode ?? null, endedAt: Date.now() });
}

/** applyTerminalChunk adds a piece of output to a terminal. */
export function applyTerminalChunk(thread: Thread, chunk: v2.TerminalOutputChunk): void {
  resolveTerminal(thread, chunk.terminalId).output += decode(chunk.data);
}

/**
 * describeTerminal returns a terminal as text: its command on a line of its
 * own, then what it printed, without color and cursor codes.
 */
export function describeTerminal(terminal: Terminal): string {
  const output = terminal.output.replace(CONTROL_CODES, "");

  return terminal.command ? `$ ${terminal.command}\n${output}` : output;
}

/**
 * applyCompaction applies an update about an agent shortening its context:
 * its status, and its summary whole or a piece at a time. A failed one shows
 * the agent's error in the summary's place.
 */
export function applyCompaction(thread: Thread, update: v2.CompactionUpdate | v2.CompactionSummaryChunk): void {
  let entry = thread.entries.find((other) => other.kind === "compaction" && other.id === update.compactionId);
  if (entry?.kind !== "compaction") {
    entry = reactive<Extract<Entry, { kind: "compaction" }>>({
      kind: "compaction",
      id: update.compactionId,
      status: "in_progress",
      text: "",
    });
    thread.entries.push(entry);
  }
  if ("content" in update) entry.text += toText(update.content);
  if ("status" in update) entry.status = update.status;
  if ("summary" in update && update.summary) entry.text = update.summary.map(toText).join("\n\n");
  if ("error" in update && update.error) entry.text = update.error;
}

/**
 * applyCursor notes the point before one of the user's prompts where the
 * agent can copy the session from. Any other point is left out.
 */
export function applyCursor(thread: Thread, update: unstable.CursorUpdate): void {
  const anchor = update.anchor as { type?: string; messageId?: string; position?: string } | null | undefined;
  if (!update.capabilities?.fork || anchor?.type !== "user_message" || anchor.position !== "before") return;
  const entry = thread.entries.find((other) => other.kind === "user" && other.id === anchor.messageId);
  if (entry?.kind === "user") entry.cursor = update.cursorId;
}

/**
 * BUILDERS holds what applies each kind of update that builds a thread and
 * does nothing else.
 */
export const BUILDERS: Partial<Record<string, (thread: Thread, update: never) => void>> = {
  plan_removed: (thread, update: { planId: string }) => removeChecklist(thread, update.planId),
  terminal_update: applyTerminal,
  terminal_output_chunk: applyTerminalChunk,
  compaction_update: applyCompaction,
  compaction_summary_chunk: applyCompaction,
  cursor_update: applyCursor,
};

/** removeChecklist drops the checklist a thread keeps under the given id. */
export function removeChecklist(thread: Thread, id: string): void {
  thread.checklists = thread.checklists.filter((checklist) => checklist.id !== id);
}

/**
 * listChecklist returns a thread's checklist: the steps of every checklist it
 * keeps, in their order.
 */
export function listChecklist(thread: Thread): ChecklistStep[] {
  return thread.checklists.flatMap((checklist) => checklist.steps);
}

/**
 * toText returns the text of a content block, or the name of a resource
 * link. It returns "" for any other block.
 */
export function toText(block: v2.ContentBlock | undefined): string {
  if (!block) return "";
  if (block.type === "text") return (block as v2.TextContent).text;
  if (block.type === "resource_link") return (block as v2.ResourceLink).name;
  return "";
}

/**
 * resolveTerminal returns a thread's terminal by its id, making it on the
 * first update about it.
 */
function resolveTerminal(thread: Thread, id: string): Terminal {
  thread.terminals[id] ??= {
    id,
    command: "",
    output: "",
    exitCode: null,
    isDone: false,
    canCancel: false,
    startedAt: Date.now(),
    endedAt: 0,
  };

  return thread.terminals[id];
}

/**
 * decode returns terminal output as text. The protocol sends it as bytes in
 * base64; data that is no base64 is kept as it came.
 */
function decode(base64: string): string {
  try {
    return new TextDecoder().decode(Uint8Array.fromBase64(base64));
  } catch {
    return base64;
  }
}
