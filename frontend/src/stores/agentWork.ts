/**
 * What a session has beside its own thread: its subagents, what still works
 * in the background, and the plan documents its agent wrote.
 */
import type * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import type * as unstable from "@agentclientprotocol/sdk/experimental/v2/unstable";

import { applyPlan, type PlanUpdate, type Thread } from "./agentThread";
import { fileLabel } from "./edits";

/**
 * A Subagent is an agent that a session's agent started for a part of its
 * work. It has a thread of its own.
 */
export type Subagent = Thread & {
  id: string; // the agent's id for its session
  title: string;
  kind: string; // the agent's word for what it is, such as Explore
  isRunning: boolean;
  canCancel: boolean; // whether the agent takes a stop of it alone

  /**
   * startedAt and endedAt are when the page first heard of it and when it
   * stopped, in milliseconds. endedAt is 0 while it runs.
   */
  startedAt: number;
  endedAt: number;
};

/** A PlanDocument is a plan an agent wrote as a document, in Markdown. */
export type PlanDocument = {
  id: string; // the agent's id for it, or its file's path where the agent gave none
  text: string;
  path: string; // its file on disk, "" for a document the agent keeps in no file
  before: string; // its text before the agent last changed it, "" if it never did
  request: string; // the id of the entry that asks to start on it, "" while none asks
  isApproved: boolean; // whether the user's last answer was to start on it
};

/** A Work is what a session has beside its own thread. */
export type Work = {
  subagents: Subagent[];

  /**
   * background is what the agent last said still works after its turn's
   * answer: terminals, tool calls and subagents. backgrounded holds the id
   * of every terminal that was ever among it, the ended ones too.
   */
  background: v2.BackgroundWork[];
  backgrounded: string[];

  documents: PlanDocument[]; // the plan documents its agent wrote, oldest first
  shownPlan: string; // the id of the document the pinned row shows, "" for none
  planFolder: string; // the folder where its agent keeps its plan files, "" where it has none
  ownPlans: string; // a part of the path of the agent's own folder of plan files outside the workspace, "" for none
};

/** A Worked is a session as this file sees it: its own thread and its work. */
type Worked = Thread & Work;

/**
 * keepBackground stores what a session's agent says still works in the
 * background, and remembers each terminal among it.
 */
export function keepBackground(session: Worked, work: v2.BackgroundWork[]): void {
  const terminals = work.flatMap((item) =>
    item.type === "terminal" ? [(item as v2.BackgroundTerminal).terminalId] : [],
  );
  session.background = work;
  session.backgrounded = [...new Set([...session.backgrounded, ...terminals])];
}

/**
 * findSubagent returns a session's subagent by its id. On the first update
 * about it, it makes the subagent and its line in the session's transcript.
 */
export function findSubagent(session: Worked, id: string): Subagent {
  const known = session.subagents.find((subagent) => subagent.id === id);
  if (known) return known;
  session.entries.push({ kind: "subagent", id });
  session.subagents.push({
    id,
    title: "",
    kind: "",
    isRunning: true,
    canCancel: false,
    startedAt: Date.now(),
    endedAt: 0,
    entries: [],
    terminals: {},
    plans: [],
  });

  return session.subagents.at(-1) as Subagent;
}

/**
 * applySubagent applies what an agent says of a subagent: its title, its
 * kind, whether it can be stopped alone, and its state.
 */
export function applySubagent(subagent: Subagent, update: v2.SubagentUpdate): void {
  if (update.title) subagent.title = update.title;
  if (update.description) subagent.kind = update.description;
  if (update.capabilities) subagent.canCancel = !!update.capabilities.cancel;
  if (update.state) applyRunning(subagent, update.state.state);
}

/**
 * applyRunning marks a subagent as running or stopped by the name of its
 * state, and notes when it stopped. A subagent that waits for the user still
 * runs.
 */
export function applyRunning(subagent: Subagent, state: string): void {
  const isRunning = state === "running" || state === "requires_action";
  if (subagent.isRunning && !isRunning) subagent.endedAt = Date.now();
  subagent.isRunning = isRunning;
}

/**
 * trackPlan stores a plan an agent sent: a document for the session, a
 * checklist for the thread it came on.
 */
export function trackPlan(session: Worked, thread: Thread, update: PlanUpdate): void {
  if (update.plan?.type !== "markdown") return applyPlan(thread, update);
  const { planId, content, path } = update.plan as unstable.PlanMarkdown;
  keepDocument(session, { id: planId, text: content, path: path ?? "" });
}

/**
 * keepRequested stores the plan document a request to start on a plan
 * carries. It returns undefined for a request that carries none.
 */
export function keepRequested(session: Worked, toolCall: v2.ToolCallUpdate | undefined): PlanDocument | undefined {
  const input = toolCall?.rawInput as { plan?: unknown; planFilePath?: unknown } | null | undefined;
  if (!toolCall || typeof input?.plan !== "string") return undefined;
  const path = typeof input.planFilePath === "string" ? input.planFilePath : "";

  return keepDocument(session, { id: path || toolCall.toolCallId, text: input.plan, path });
}

/**
 * keepDocument stores a plan document for a session and shows it in the
 * pinned row. A document is the one the session has under the same id, or
 * with the same text: an agent sends one document under two ids, when it
 * asks to start on it and as its plan, and the two differ by the blank end
 * of the text, which is cut. A new document and a changed one get a line in
 * the transcript.
 */
export function keepDocument(session: Worked, sent: { id: string; text: string; path: string }): PlanDocument {
  const text = sent.text.trimEnd();
  const known =
    session.documents.find((other) => other.id === sent.id) ?? session.documents.find((other) => other.text === text);
  const isChanged = known?.text !== text;
  const fresh = { ...sent, text, before: "", request: "", isApproved: false };
  if (!known) session.documents.push(fresh);
  else if (isChanged) Object.assign(known, { before: known.text, text, isApproved: false });
  // A new document is taken back from the session, where it is reactive.
  const document = known ?? session.documents.find((other) => other.id === sent.id) ?? fresh;
  if (sent.path) document.path = sent.path;
  if (isChanged)
    session.entries.push({ kind: "plan", id: `d${session.entries.length}`, plan: document.id, isUpdate: !!known });
  session.shownPlan = document.id;

  return document;
}

/**
 * isPlanFile reports whether a path is a Markdown file in a folder where a
 * session's agent keeps its plans: the workspace's plans folder, or the
 * agent's own outside the workspace, as GitHub Copilot's is. Such a file is
 * a plan of the session, whichever way the agent wrote it, and never a
 * change to review.
 */
export function isPlanFile(session: Work, path: string): boolean {
  const folder = `${session.planFolder}/`.replaceAll("\\", "/").toLowerCase();
  const file = path.replaceAll("\\", "/").toLowerCase();
  const isOwn = !!session.ownPlans && file.includes(`/${session.ownPlans.toLowerCase()}/`);

  return ((!!session.planFolder && file.startsWith(folder)) || isOwn) && file.endsWith(".md");
}

/**
 * describePlan returns a plan document's title: its first heading, else its
 * file's name, else "Plan".
 */
export function describePlan(document: PlanDocument): string {
  const heading = /^#+\s+(.+)$/m.exec(document.text)?.[1]?.trim();

  return heading || fileLabel(document.path) || "Plan";
}
