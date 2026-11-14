/**
 * The agent store: this window's chats with coding agents, and what the agent pane and the status bar show of them.
 * Each session speaks the Agent Client Protocol over its own stream, version 2 first and version 1 if the agent asks.
 */
import * as v1 from "@agentclientprotocol/sdk";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { useQuery } from "@pinia/colada";
import { until, useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import { groupBy, lowerFirst } from "es-toolkit";
import { defineStore } from "pinia";
import { computed, markRaw, reactive, shallowRef } from "vue";

import type { Agent } from "@services/agent/models";
import type { Tool } from "@services/mcp/models";
import type { Place, Review } from "@services/workspace/models";
import { confirm } from "../lib/feedback";
import { describeError, describeTime, plural } from "../lib/format";
import { openStream, type Start } from "../lib/stream";
import { accessQuery, agentsQuery, loadQuery, placeQuery, reviewsQuery, toolsQuery } from "../queries";
import { useEditorStore } from "./editor";
import { fileLabel, toEdits, trackEdits, type DiffContent, type FileEdit } from "./edits";
import { useSettingsStore } from "./settings";
import { useToolViewStore } from "./toolView";
import { useWindowsStore, WINDOW_QUERY } from "./windows";
import { useWorkspaceStore } from "./workspace";

/** A Protocol is a version of the Agent Client Protocol. */
type Protocol = 1 | 2;

/** A SignIn is one way to sign in to an agent: a command for PMT's terminal, or a request to the agent. */
export type SignIn = { id: string; name: string; description: string; command: string[] };

/** A LinkRequest is a link an agent asks the user to open. */
export type LinkRequest = { id: string; message: string; url: string };

/** An Entry is one item of a session's transcript. */
export type Entry =
  | { kind: "user"; id: string; text: string; chips: string[] }
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
    }
  | { kind: "plan"; id: string; steps: { text: string; status: string }[] }
  | {
      kind: "permission";
      id: string;
      title: string;
      permit: string; // the agent and the tool, as the setting of tools allowed always writes them
      options: { id: string; name: string; kind: string }[];
    }
  | { kind: "link"; id: string; message: string; url: string }
  | { kind: "notice"; id: string; text: string };

/**
 * A PlanLimit is the limit of the user's plan that an agent last said is in
 * play. Only Claude's adapter sends one.
 */
export type PlanLimit = {
  /**
   * status is the agent's own word. It warns past a threshold of its own and
   * refuses at the limit.
   */
  status: "allowed" | "allowed_warning" | "rejected";

  /** window is the limit's name, such as "5-hour limit". */
  window: string;

  /** percent is how much of the limit is used. It is null if not sent. */
  percent: number | null;

  /** resetsAt is when the limit resets, in milliseconds. Null if not sent. */
  resetsAt: number | null;
};

/**
 * A Usage is what an agent last said of a session: the tokens in its context,
 * the context's size, and its cost and plan limit if it sent them.
 */
export type Usage = { used: number; size: number; cost: v2.Cost | null; limit: PlanLimit | null };

/** A Session is one chat with an agent in this window. */
export type Session = {
  key: string; // this window's own id for it
  agent: Agent;
  protocol: Protocol;
  id: string; // the agent's id for it, "" until the agent gives one
  title: string;
  isRenamed: boolean; // the agent's titles do not replace a title the user gave
  state: "starting" | "sign-in" | "idle" | "running" | "ended";
  entries: Entry[];
  terminals: Record<string, string>;
  options: v2.SessionConfigOption[];
  commands: v2.AvailableCommand[];
  methods: SignIn[];
  usage: Usage | null; // null until the agent sends a usage update
  resumable: boolean;
  deletable: boolean;
  directories: boolean;
  error: string;
};

/** A SessionRecord is one session as every window's sessions list remembers it. */
export type SessionRecord = {
  agent: string;
  /** The id of the workspace the session was started in. */
  workspace: string;
  id: string;
  title: string;
  updatedAt: number;
  resumable: boolean;
  deletable: boolean;
  isArchived: boolean;
  isRenamed: boolean;
};

/** A Dialect is one version of the protocol as a session speaks it: the same steps in that version's words. */
type Dialect = {
  initialize: () => Promise<void>;
  create: (start: Start) => Promise<string>;
  resume: (id: string, start: Start) => Promise<void>;
  prompt: (id: string, blocks: v2.ContentBlock[]) => Promise<void>;
  configure: (id: string, configId: string, value: string | boolean) => Promise<Offers>;
  cancel: (id: string) => Promise<void>;
  delete: (id: string) => Promise<void>;
  signIn: (methodId: string) => Promise<void>;
  close: () => void;
};

/** A Method is one way to sign in, as either version declares it. */
type Method = {
  id: string;
  name: string;
  description?: string | null;
  type?: string;
  args?: string[];
  _meta?: { "terminal-auth"?: { command: string; args?: string[] } } | null;
};

/** An HttpServer is an MCP server reached over HTTP. */
type HttpServer = { type: "http"; name: string; url: string; headers: { name: string; value: string }[] };

/** A PermissionAnswer is the user's answer to a permission request. */
type PermissionAnswer = { outcome: { outcome: "cancelled" } | { outcome: "selected"; optionId: string } };

/** Answers are what a session's open requests wait for, by the id of the entry that shows each. */
type Answers = Map<string, (answer: string | null) => void>;

/** A Link is what a session holds that is never shown. */
type Link = { dialect: Dialect; answers: Answers };

/** Offers are the settings and commands an agent offers a session. */
type Offers = {
  configOptions?: (v1.SessionConfigOption | v2.SessionConfigOption)[] | null;
  availableCommands?: v2.AvailableCommand[];
};

/** A RateLimit is Claude's adapter's report of a plan limit, as it sends it. */
type RateLimit = { status: PlanLimit["status"]; rateLimitType?: string; utilization?: number; resetsAt?: number };

/** A MessageUpdate is what one kind of update about a message writes: an entry's kind, and a whole text or a chunk. */
type MessageUpdate = { kind: "user" | "agent" | "thought"; isWhole: boolean };

/** PROTOCOLS is the local storage key of the version each agent answered last. */
const PROTOCOLS = "pmt.agent.protocol";
/** OPTIONS is the local storage key of the model, effort and mode each agent was last given. */
const OPTIONS = "pmt.agent.options";
/** ALLOWED is the setting that holds the tools the user allowed always, each with its agent. */
export const ALLOWED = "agents.allowed";
/**
 * FILE_KINDS holds the kinds of tool call an agent allows always as a whole, as Claude's "allow all edits" does, with
 * the words Settings shows for each. Any other tool is allowed always by its title, such as one command.
 */
export const FILE_KINDS: Record<string, string> = {
  read: "Reading files",
  edit: "Changing files",
  delete: "Deleting files",
  move: "Moving files",
};

/**
 * RATE_LIMIT is the key of Claude's adapter in a usage update's metadata. It is
 * the adapter's own, so no other agent sends it. Its value is the status of the
 * limit now in play, the window's type, how much of it is used as a share
 * (usually 0 to 1) and when it resets in seconds since 1970.
 */
const RATE_LIMIT = "_claude/rateLimit";

/** LIMIT_WINDOWS holds the words for each window type of Claude's limit. */
const LIMIT_WINDOWS: Record<string, string> = {
  five_hour: "5-hour limit",
  seven_day: "Weekly limit",
  seven_day_opus: "Weekly Opus limit",
  seven_day_sonnet: "Weekly Sonnet limit",
  seven_day_overage_included: "Weekly limit with extra usage",
  overage: "Extra usage",
};

/** PMT_TOOL matches the title of a tool call for one of PMT's tools, in each form agents write it. */
const PMT_TOOL = /^(?:mcp__pmt__|pmt[_:/])(.+)$/;
/** AUTH_REQUIRED is the error code an agent answers with while the user has not signed in. */
const AUTH_REQUIRED = -32000;
/** CLIENT is how PMT names itself to an agent. */
export const CLIENT = { name: "pmt", title: "Paradox Modding Tools", version: "1" };

/** V1_INITIALIZE is the request that opens a version 1 connection. */
export const V1_INITIALIZE = {
  protocolVersion: 1,
  clientInfo: CLIENT,
  clientCapabilities: {
    fs: { readTextFile: false, writeTextFile: false },
    terminal: false,
    auth: { terminal: true },
    elicitation: { url: {} },
  },
} satisfies v1.InitializeRequest;

export const useAgentStore = defineStore("agent", () => {
  const toast = useToast();
  const editor = useEditorStore();
  const workspace = useWorkspaceStore();
  const windows = useWindowsStore();
  const toolView = useToolViewStore();
  const settings = useSettingsStore();
  const { data: agents } = useQuery(agentsQuery);
  const { data: access } = useQuery(accessQuery);
  const { data: tools } = useQuery(toolsQuery);
  const { data: reviews } = useQuery(reviewsQuery);
  const sessions = reactive<Session[]>([]);
  const links = new Map<string, Link>();
  const protocols = useLocalStorage<Record<string, Protocol>>(PROTOCOLS, {});
  const records = useLocalStorage<SessionRecord[]>("pmt.agent.sessions", []);
  const lastAgent = useLocalStorage("pmt.agent.last", "");
  // The options the user last picked for each agent, by agent and by option.
  const lastOptions = useLocalStorage<Record<string, Record<string, string | boolean>>>(OPTIONS, {});
  const activeKey = shallowRef("");
  // Whether the agent pane is open. It stays so across a reload.
  const isPaneOpen = useLocalStorage("pmt.agent.paneOpen", false);
  // The workspaces whose latest session this window has resumed.
  const resumed = new Set<string>();
  // Follow the agent: whether to open the files the agent's tool calls name.
  const follow = useLocalStorage("pmt.agent.follow", true);
  let count = 0;

  const isPromptWanted = shallowRef(false);

  const active = computed(() => sessions.find((session) => session.key === activeKey.value));
  // The agents a chat can start with, which leaves out the one that completes
  // code.
  const ready = computed(() => (agents.value ?? []).filter((row) => row.program && !row.completion));
  const current = computed(
    () => active.value?.agent ?? ready.value.find((row) => row.id === lastAgent.value) ?? ready.value[0],
  );
  /** isWaiting reports whether a session waits for the user: a permission to give or a link to open. */
  const isWaiting = (session: Session): boolean =>
    session.entries.some((entry) => entry.kind === "permission" || entry.kind === "link");

  /** findReview returns a file's place while it is under review. */
  const findReview = (path: string): Review | undefined => reviews.value?.find((place) => place.path === path);

  const status = computed(() => {
    const session = active.value;
    if (!session) return "";
    const name = session.agent.name;
    const asking = session.entries.find((entry) => entry.kind === "permission");
    if (asking?.kind === "permission") return asking.title;
    if (session.entries.some((entry) => entry.kind === "link")) return `${name} asks to open a link`;
    if (session.state === "running") return `${name} is working: ${describeTitle(session)}`;
    if (session.state === "sign-in") return `${name} needs you to sign in`;
    if (session.state === "starting") return `${name} is starting`;
    const fileCount = reviews.value?.length ?? 0;

    return fileCount ? `${name} is done: ${plural(fileCount, "file")} to review` : "";
  });

  /**
   * warnedLimit is the active session's plan limit while its agent warns or
   * refuses. It is undefined otherwise.
   */
  const warnedLimit = computed(() => {
    const limit = active.value?.usage?.limit;

    return limit && limit.status !== "allowed" ? limit : undefined;
  });

  /** start starts a session with an agent, or the agent's replay of a record, and shows it. */
  function start(agentId: string, record?: SessionRecord): Session {
    const agent = agents.value?.find((candidate) => candidate.id === agentId);
    if (!agent) throw new Error("PMT does not list that agent");
    const session = makeSession(agent, record);
    sessions.push(session);
    activeKey.value = session.key;
    lastAgent.value = agent.id;
    void connect(session, record ? "resume" : "create");
    return session;
  }

  /** makeSession makes a session with an agent, new or from a record, without showing it. */
  function makeSession(agent: Agent, record?: SessionRecord): Session {
    return reactive<Session>({
      key: `s${++count}`,
      agent,
      protocol: protocols.value[`${agent.id}@${agent.installed}`] ?? 2,
      id: record?.id ?? "",
      title: record?.title ?? "New session",
      isRenamed: record?.isRenamed ?? false,
      state: "starting",
      entries: [],
      terminals: {},
      options: [],
      commands: [],
      methods: [],
      usage: null,
      resumable: false,
      deletable: false,
      directories: false,
      error: "",
    });
  }

  /** connect opens a session's stream and has the agent create or resume the session. */
  async function connect(session: Session, step: "create" | "resume"): Promise<void> {
    try {
      const { start: startMessage, dialect } = await openDialect(session);
      try {
        await dialect.initialize();
      } catch (error) {
        unlink(session);
        if (session.protocol === 1) throw error;
        session.protocol = 1;
        return await connect(session, step);
      }
      protocols.value = { ...protocols.value, [`${session.agent.id}@${session.agent.installed}`]: session.protocol };
      if (step === "resume") await dialect.resume(session.id, startMessage);
      else session.id = await dialect.create(startMessage);
      session.state = "idle";
      remember(session);
      if (step === "create") await applyLastOptions(session);
    } catch (error) {
      if (isAuthRequired(error)) {
        session.state = "sign-in";
        return;
      }
      session.state = "ended";
      session.error = describeError(error);
    }
  }

  /** openDialect opens a stream to a session's agent and wraps it for the SDK. */
  async function openDialect(session: Session): Promise<{ start: Start; dialect: Dialect }> {
    const opened = openStream(session.agent, editor.activeFile()?.path ?? "", () => {
      if (links.get(session.key) === link) session.state = "ended";
    });
    const answers: Answers = new Map();
    const dialect = markRaw(makeDialect(session, opened.stream, opened, answers));
    const link: Link = { dialect, answers };
    links.set(session.key, link);

    return { start: await opened.started, dialect };
  }

  /** unlink closes a session's stream and forgets its link. */
  function unlink(session: Session): void {
    links.get(session.key)?.dialect.close();
    links.delete(session.key);
  }

  /** VERSIONS holds what the protocol's two versions say differently. */
  const VERSIONS = {
    2: {
      client: () => v2.client(CLIENT),
      toPermission: (params: unknown) => {
        const { title, subject, options } = params as v2.RequestPermissionRequest;
        return { title, toolCall: (subject as { toolCall?: v2.ToolCallUpdate } | null)?.toolCall, options };
      },
      initialize: {
        protocolVersion: 2,
        info: CLIENT,
        capabilities: { auth: { terminal: {} }, elicitation: { url: {} } },
      } satisfies v2.InitializeRequest,
      toCapabilities: (answer: unknown) => {
        const { capabilities, authMethods } = answer as v2.InitializeResponse;
        return {
          resumable: true,
          deletable: !!capabilities?.session?.delete,
          directories: !!capabilities?.session?.additionalDirectories,
          methods: (authMethods ?? []).map((method) => ({ ...method, id: method.methodId })),
        };
      },
      resume: (id: string, start: Start, folders: object): [string, object] => [
        "session/resume",
        {
          sessionId: id,
          cwd: start.folder,
          mcpServers: listServers(),
          replayFrom: { type: "start" },
          ...folders,
        } satisfies v2.ResumeSessionRequest,
      ],
      configure: (id: string, configId: string, value: string | boolean) =>
        (typeof value === "boolean"
          ? { sessionId: id, configId, type: "boolean", value }
          : { sessionId: id, configId, type: "id", value }) satisfies v2.SetSessionConfigOptionRequest,
      login: "auth/login",
      endsTurnOnPrompt: false,
    },
    1: {
      client: () => v1.client(CLIENT),
      toPermission: (params: unknown) => {
        const { toolCall, options } = params as v1.RequestPermissionRequest;
        return { title: toolCall.title ?? "", toolCall: toolCall as v2.ToolCallUpdate, options };
      },
      initialize: V1_INITIALIZE,
      toCapabilities: (answer: unknown) => {
        const { agentCapabilities, authMethods } = answer as v1.InitializeResponse;
        return {
          resumable: !!agentCapabilities?.loadSession,
          deletable: !!agentCapabilities?.sessionCapabilities?.delete,
          directories: !!agentCapabilities?.sessionCapabilities?.additionalDirectories,
          methods: authMethods ?? [],
        };
      },
      resume: (id: string, start: Start, folders: object): [string, object] => [
        "session/load",
        { sessionId: id, cwd: start.folder, mcpServers: listServers(), ...folders } satisfies v1.LoadSessionRequest,
      ],
      configure: (id: string, configId: string, value: string | boolean) =>
        (typeof value === "boolean"
          ? { sessionId: id, configId, type: "boolean", value }
          : { sessionId: id, configId, value }) satisfies v1.SetSessionConfigOptionRequest,
      login: "authenticate",
      endsTurnOnPrompt: true,
    },
  };

  /** makeDialect makes the Dialect for a session's version of the protocol over a stream. */
  function makeDialect(session: Session, stream: v2.Stream, socket: { close: () => void }, answers: Answers): Dialect {
    const row = VERSIONS[session.protocol];
    // Both versions' clients take a method by its name, with params in each version's own shape.
    const client: {
      onNotification(method: string, handler: (context: { params: unknown }) => void): unknown;
      onRequest(method: string, handler: (context: { params: unknown }) => unknown): unknown;
      connect(stream: unknown): {
        agent: {
          request(method: string, params: object): Promise<unknown>;
          notify(method: string, params: object): Promise<void>;
        };
        close(): void;
      };
    } = row.client();
    client.onNotification("session/update", ({ params }) =>
      applyUpdate(session, (params as v2.UpdateSessionNotification).update),
    );
    client.onRequest("session/request_permission", ({ params }) => {
      const { title, toolCall, options } = row.toPermission(params);
      return ask(session, answers, title, toolCall, options);
    });
    client.onRequest("elicitation/create", ({ params }) =>
      askLink(session, answers, params as v2.CreateElicitationRequest),
    );
    client.onNotification("elicitation/complete", ({ params }) =>
      closeEntry(session, answers, (params as v2.CompleteElicitationNotification).elicitationId, null),
    );
    const connection = client.connect(stream);
    const agent = connection.agent;
    return {
      initialize: async () => {
        const { methods, ...capabilities } = row.toCapabilities(await agent.request("initialize", row.initialize));
        Object.assign(session, capabilities, { methods: methods.map((method) => toSignIn(session.agent, method)) });
      },
      create: async (start) => {
        const request = { cwd: start.folder, mcpServers: listServers(), ...folders(session, start) };
        const answer = (await agent.request("session/new", request)) as Offers & { sessionId: string };
        applyOffers(session, answer);
        return answer.sessionId;
      },
      resume: async (id, start) => {
        if (!session.resumable) throw new Error(`${session.agent.name} cannot replay a session`);
        applyOffers(session, (await agent.request(...row.resume(id, start, folders(session, start)))) as Offers);
      },
      prompt: async (id, blocks) => {
        session.state = "running";
        const request = { sessionId: id, prompt: blocks };
        const { stopReason } = (await agent.request("session/prompt", request)) as { stopReason: string };
        if (row.endsTurnOnPrompt) finishTurn(session, stopReason);
      },
      configure: async (id, configId, value) =>
        (await agent.request("session/set_config_option", row.configure(id, configId, value))) as Offers,
      cancel: (id) => agent.notify("session/cancel", { sessionId: id }),
      delete: async (id) => void (await agent.request("session/delete", { sessionId: id })),
      signIn: async (methodId) => void (await agent.request(row.login, { methodId })),
      close: () => {
        connection.close();
        socket.close();
      },
    };
  }

  /** listServers lists the MCP servers a session is given, which is PMT's own. */
  function listServers(): HttpServer[] {
    if (!access.value) return [];
    const headers = [{ name: "Authorization", value: `Bearer ${access.value.token}` }];
    return [{ type: "http", name: "pmt", url: access.value.url, headers }];
  }

  /** applyUpdate applies one update an agent sent about a session to its transcript. */
  function applyUpdate(session: Session, update: v2.SessionUpdate): void {
    const kind = update.sessionUpdate;
    const message = MESSAGES[kind];
    if (message) {
      applyMessage(session, update as v2.ContentChunk | v2.UserMessage, message);
    } else if (kind === "tool_call" || kind === "tool_call_update" || kind === "tool_call_content_chunk") {
      applyTool(session, update as v2.ToolCallUpdate | v2.ToolCallContentChunk);
    } else if (kind === "plan" || kind === "plan_update") {
      const plan = update as { entries?: v2.PlanEntry[]; plan?: { entries?: v2.PlanEntry[] } };
      const steps = (plan.entries ?? plan.plan?.entries ?? []).map((step) => ({
        text: step.content,
        status: step.status,
      }));
      const entry = session.entries.find((candidate) => candidate.kind === "plan");
      if (entry?.kind === "plan") entry.steps = steps;
      else session.entries.push({ kind: "plan", id: "plan", steps });
    } else if (kind === "session_info_update") {
      const title = (update as v2.SessionInfoUpdate).title;
      if (title && !session.isRenamed) session.title = title;
      remember(session);
    } else if (kind === "state_update") {
      const state = update as { state: string; stopReason?: string | null };
      if (state.state === "running") session.state = "running";
      else if (state.state === "idle") finishTurn(session, state.stopReason ?? "end_turn");
    } else if (kind === "terminal_update") {
      // An update with output holds the terminal's whole output so far.
      const terminal = update as v2.TerminalUpdate;
      const command = terminal.command ? `$ ${terminal.command}\n` : "";
      const previous = session.terminals[terminal.terminalId] ?? command;
      session.terminals[terminal.terminalId] = terminal.output ? command + terminal.output.data : previous;
    } else if (kind === "terminal_output_chunk") {
      const chunk = update as v2.TerminalOutputChunk;
      session.terminals[chunk.terminalId] = (session.terminals[chunk.terminalId] ?? "") + chunk.data;
    } else if (kind === "config_option_update" || kind === "available_commands_update") {
      applyOffers(session, update as Offers);
    } else if (kind === "usage_update") {
      applyUsage(session, update as v2.UsageUpdate);
    }
  }

  /** applyMessage applies an update about a message to its entry, making the entry on the first update. */
  function applyMessage(session: Session, update: v2.ContentChunk | v2.UserMessage, row: MessageUpdate): void {
    if (row.isWhole && !("content" in update)) return;
    const blocks = row.isWhole ? ((update as v2.UserMessage).content ?? []) : [(update as v2.ContentChunk).content];
    const id = update.messageId ?? "";
    // An update without an id belongs to the last entry, if that has the kind.
    const candidates = id ? session.entries : session.entries.slice(-1);
    let entry = candidates.find((candidate) => candidate.kind === row.kind && (!id || candidate.id === id)) as
      Extract<Entry, { kind: MessageUpdate["kind"] }> | undefined;
    if (!entry) {
      const key = id || `${row.kind}${session.entries.length}`;
      entry =
        row.kind === "user" ? { kind: "user", id: key, text: "", chips: [] } : { kind: row.kind, id: key, text: "" };
      session.entries.push(entry);
    }
    if (row.isWhole) entry.text = "";
    if (row.isWhole && entry.kind === "user") entry.chips = [];
    for (const block of blocks) {
      if (entry.kind === "user" && block.type === "resource_link") entry.chips.push((block as v2.ResourceLink).name);
      else entry.text += (row.isWhole && entry.text ? "\n\n" : "") + toText(block);
    }
  }

  /** applyTool applies an update about a tool call to its entry, making the entry on the first update. */
  function applyTool(session: Session, update: v2.ToolCallUpdate | v2.ToolCallContentChunk): void {
    let entry = session.entries.find((candidate) => candidate.kind === "tool" && candidate.id === update.toolCallId);
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
      });
      session.entries.push(entry);
    }
    const wasDone = entry.status === "completed";
    const whole =
      "title" in update || "status" in update || Array.isArray(update.content) ? (update as v2.ToolCallUpdate) : null;
    if (whole?.title) entry.title = whole.title;
    if (whole?.status) entry.status = whole.status;
    if (whole?.kind) entry.type = whole.kind;
    if (Array.isArray(update.content)) Object.assign(entry, { text: "", edits: [], terminals: [] });
    const contents = [update.content ?? []].flat(); // the update's content is one item, a list, or absent
    for (const content of contents) {
      if (content.type === "content") entry.text += toText((content as { content: v2.ContentBlock }).content);
      else if (content.type === "diff") entry.edits.push(...toEdits(content as unknown as DiffContent));
      else if (content.type === "terminal") entry.terminals.push((content as { terminalId: string }).terminalId);
    }
    const locations = whole?.locations ?? [];
    const paths = locations.map((location) => location.path);
    entry.paths = [...new Set([...entry.paths, ...paths, ...entry.edits.map((edit) => edit.path)])];
    // A resumed session replays its old calls while it starts. Their edits
    // are old, so they start no review.
    if (entry.status === "completed" && !wasDone && session.state !== "starting") startReviews(entry.edits);
    if (follow.value && locations[0]) void toolView.followFile(locations[0].path, locations[0].line ?? 1);
  }

  /**
   * startReviews sends Go each file the given edits changed, so that Go can
   * start the file's review. A review that starts shows the agent pane once;
   * a file already under review leaves the pane as the user left it. A file
   * Go cannot review says why on its edits.
   */
  function startReviews(edits: FileEdit[]): void {
    for (const [path, fileEdits] of Object.entries(groupBy(edits, (edit) => edit.path))) {
      const isReviewed = !!findReview(path);
      trackEdits(fileEdits)
        .then(() => {
          if (!isReviewed) showPane();
        })
        .catch((error: unknown) => {
          fileEdits.forEach((edit) => (edit.reviewError = describeError(error)));
        });
    }
  }

  /** finishTurn marks a session's turn over, with a notice if the turn did not end normally. */
  function finishTurn(session: Session, reason: string): void {
    session.state = "idle";
    if (reason !== "end_turn")
      session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text: STOPS[reason] ?? reason });
    remember(session);
  }

  /**
   * ask answers a permission request at once if PMT may, else shows it in a session and answers the agent with the
   * option the user picks.
   */
  function ask(
    session: Session,
    answers: Answers,
    title: string,
    toolCall: v2.ToolCallUpdate | undefined,
    options: v2.PermissionOption[],
  ): Promise<PermissionAnswer> {
    if (toolCall) applyTool(session, toolCall);
    const toolTitle = toolCall?.title || title;
    const tool = tools.value?.find((row) => row.name === toPmtTool(toolTitle));
    const scope = toolCall?.kind && toolCall.kind in FILE_KINDS ? toolCall.kind : toolTitle;
    const permit = `${session.agent.id}:${scope.replaceAll(",", ";")}`;
    const allowed = pickAllowed(tool, permit, options);
    if (allowed) return Promise.resolve(toAnswer(allowed.optionId));
    const id = `p${session.entries.length}`;
    session.entries.push({
      kind: "permission",
      id,
      title: tool ? describeWish(session, tool, toolCall) : `${session.agent.name} asks: ${toolTitle || "allow this?"}`,
      permit,
      options: options.map((option) => ({ id: option.optionId, name: option.name, kind: option.kind })),
    });
    return new Promise((resolve) => answers.set(id, (optionId) => resolve(toAnswer(optionId))));
  }

  /**
   * pickAllowed returns the option that answers a permission request without asking the user: for a tool of PMT's
   * that only reads, the one that allows it once, and for a tool the user allowed always, the one that allows it
   * always. It returns undefined if the user must be asked.
   */
  function pickAllowed(
    tool: Tool | undefined,
    permit: string,
    options: v2.PermissionOption[],
  ): v2.PermissionOption | undefined {
    if (tool?.readOnly) return findOption(options, "allow_once", "allow_always");
    if (settings.value(ALLOWED).split(",").includes(permit)) return findOption(options, "allow_always", "allow_once");
    return undefined;
  }

  /** askLink shows a link an agent asks to open and answers with the user's choice. It declines a form. */
  function askLink(
    session: Session,
    answers: Answers,
    request: v2.CreateElicitationRequest,
  ): Promise<v2.CreateElicitationResponse> {
    const link = toLink(request);
    if (!link) return Promise.resolve({ action: "decline" });

    session.entries.push({ kind: "link", ...link });
    return new Promise((resolve) => answers.set(link.id, (action) => resolve(answerLink(link.url, action))));
  }

  /** closeEntry answers a session's open request and removes it from the transcript. */
  function closeEntry(session: Session, answers: Answers, id: string, answer: string | null): void {
    answers.get(id)?.(answer);
    answers.delete(id);
    session.entries = session.entries.filter(
      (entry) => !((entry.kind === "permission" || entry.kind === "link") && entry.id === id),
    );
  }

  /** answer answers a session's open request with an option, an action, or null to cancel it. */
  const answer = (session: Session, id: string, choice: string | null): void => {
    const link = links.get(session.key);
    if (!link) return;
    const entry = session.entries.find((candidate) => candidate.kind === "permission" && candidate.id === id);
    if (entry?.kind === "permission" && entry.options.some((row) => row.id === choice && row.kind === "allow_always"))
      settings.writeItem(ALLOWED, entry.permit, true);
    closeEntry(session, link.answers, id, choice);
  };

  /** prompt sends the user's text to a session, with the picked files and the page's chip as links. */
  const prompt = async (session: Session, text: string, files: Place[]): Promise<void> => {
    const link = links.get(session.key);
    if (!link || session.state !== "idle") return;
    const pageChip = toolView.promptChip;
    const blocks: v2.ContentBlock[] = [{ type: "text", text }];
    for (const file of files) blocks.push({ type: "resource_link", uri: toUri(file.path), name: file.rel });
    if (pageChip?.path) {
      const { startLine, endLine } = pageChip;
      const lines = startLine ? ` (lines ${startLine}–${endLine})` : "";
      blocks.push({
        type: "resource_link",
        uri: toUri(pageChip.path, startLine, endLine),
        name: pageChip.label + lines,
      });
    } else if (pageChip?.text) blocks.push({ type: "text", text: pageChip.text });
    // A version 2 agent sends the prompt back, but a version 1 agent does not.
    if (session.protocol === 1) {
      const chips = [...files.map((file) => file.rel), ...(pageChip ? [pageChip.label] : [])];
      session.entries.push({ kind: "user", id: `u${session.entries.length}`, text, chips });
    }
    try {
      await link.dialect.prompt(session.id, blocks);
    } catch (error) {
      finishTurn(session, describeError(error));
    }
  };

  /**
   * configure sets one of a session's settings and keeps the value for the agent's next session. If the agent
   * refuses, the error goes in the transcript.
   */
  const configure = async (session: Session, configId: string, value: string | boolean): Promise<void> => {
    const link = links.get(session.key);
    if (!link) return;
    try {
      applyOffers(session, await link.dialect.configure(session.id, configId, value));
      lastOptions.value = {
        ...lastOptions.value,
        [session.agent.id]: { ...lastOptions.value[session.agent.id], [configId]: value },
      };
    } catch (error) {
      session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text: describeError(error) });
    }
  };

  /**
   * applyLastOptions gives a new session each option its agent was last given that the session offers and has
   * otherwise.
   */
  async function applyLastOptions(session: Session): Promise<void> {
    for (const [configId, value] of Object.entries(lastOptions.value[session.agent.id] ?? {})) {
      const option = session.options.find((row) => row.configId === configId);
      if (option && option.currentValue !== value && isOffered(option, value))
        await configure(session, configId, value);
    }
  }

  /** cancel asks a session's agent to stop its turn. */
  const cancel = (session: Session): void => void links.get(session.key)?.dialect.cancel(session.id);

  /** close ends a session here: its agent stops, and its record stays in the sessions list. */
  const close = (session: Session): void => {
    unlink(session);
    sessions.splice(sessions.indexOf(session), 1);
    if (activeKey.value === session.key) activeKey.value = sessions.at(-1)?.key ?? "";
  };

  /** signIn signs in to a session's agent, in PMT's terminal or through the agent, and starts the session again. */
  const signIn = async (session: Session, method: SignIn): Promise<void> => {
    if (method.command.length) return runSignIn(method.command);

    await links.get(session.key)?.dialect.signIn(method.id);
    unlink(session);
    session.state = "starting";
    void connect(session, session.id ? "resume" : "create");
  };

  /** retry starts a session's agent again after a sign-in in the terminal or an error. */
  const retry = (session: Session): void => {
    unlink(session);
    Object.assign(session, { state: "starting", error: "" });
    void connect(session, session.id ? "resume" : "create");
  };

  /** listRecords lists the remembered sessions of the active workspace, newest first. */
  const listRecords = (): SessionRecord[] =>
    records.value.filter((record) => record.workspace === workspace.activeId());

  /** remember writes a session to every window's sessions list, newest first. */
  function remember(session: Session): void {
    if (!session.id) return;
    const previous = records.value.find((other) => other.id === session.id);
    const record = {
      agent: session.agent.id,
      workspace: previous?.workspace ?? workspace.activeId(),
      id: session.id,
      title: session.title,
      updatedAt: Date.now(),
      resumable: session.resumable,
      deletable: session.deletable,
      isArchived: previous?.isArchived ?? false,
      isRenamed: session.isRenamed,
    };
    records.value = [record, ...records.value.filter((other) => other.id !== session.id)].slice(0, 50);
  }

  /** rename gives the session with the given id a title of the user's own. A blank title changes nothing. */
  const rename = (id: string, title: string): void => {
    const text = title.trim();
    if (!text) return;
    records.value = records.value.map((other) =>
      other.id === id ? { ...other, title: text, isRenamed: true } : other,
    );
    const live = sessions.find((session) => session.id === id);
    if (live) Object.assign(live, { title: text, isRenamed: true });
  };

  /** open shows a remembered session here, replayed by its agent if it is not running. */
  const open = (record: SessionRecord): void => {
    const live = sessions.find((session) => session.id === record.id);
    if (live) activeKey.value = live.key;
    else start(record.agent, record.resumable ? record : undefined);
  };

  /**
   * resumeLatest resumes the active workspace's latest session the first time
   * the pane shows in that workspace. It does nothing if that session is
   * archived or cannot be resumed, its agent is not ready, or a session shows
   * already.
   */
  const resumeLatest = async (): Promise<void> => {
    const id = workspace.activeId();
    if (!id || resumed.has(id)) return;

    resumed.add(id);
    await until(() => !!agents.value).toBe(true);
    const latest = listRecords().find((record) => !record.isArchived);
    if (!latest?.resumable || active.value || !ready.value.some((row) => row.id === latest.agent)) return;

    open(latest);
  };

  /** archive moves a session into its agent's archived sessions, or back out of them. */
  const archive = (record: SessionRecord, isArchived: boolean): void => {
    records.value = records.value.map((other) => (other.id === record.id ? { ...other, isArchived } : other));
  };

  /** deleteSession asks, then removes a session from every window's list. The agent deletes its copy if it can. */
  const deleteSession = async (record: SessionRecord): Promise<void> => {
    const agent = agents.value?.find((candidate) => candidate.id === record.agent);
    const agentName = agent?.name ?? "The agent";
    const isDeletable = !!agent && record.deletable;
    const confirmed = await confirm.open({
      title: "Delete this session?",
      body: isDeletable
        ? `“${record.title}” leaves PMT, and ${agentName} deletes its own copy. This cannot be undone.`
        : `“${record.title}” leaves PMT. ${agentName} keeps its own copy, since it has no way to delete one.`,
      confirm: "Delete session",
      destructive: true,
    });
    if (confirmed !== true) return;
    const live = sessions.find((session) => session.id === record.id);
    if (live) close(live);
    records.value = records.value.filter((other) => other.id !== record.id);
    if (!agent || !isDeletable) return;
    const session = makeSession(agent, record);
    try {
      const { dialect } = await openDialect(session);
      await dialect.initialize();
      await dialect.delete(record.id);
    } catch (error) {
      toast.add({ title: `${agentName} could not delete its copy`, description: describeError(error), color: "error" });
    } finally {
      unlink(session);
    }
  };

  /**
   * showFile shows a file in a preview tab. It does nothing if the workspace
   * has no such file.
   */
  const showFile = async (path: string): Promise<void> => {
    const place = await loadQuery(placeQuery(path)).catch(() => undefined);
    if (place) editor.show(place, undefined, "changes");
  };

  /** popOut opens the agent pane in a window of its own, which takes over the session shown here. */
  const popOut = (shown: Session | undefined): void => {
    if (shown) close(shown);
    isPaneOpen.value = false;
    windows.popOut("agent", [], shown ? { session: shown.id } : {});
  };

  /** showPane shows the agent pane, or brings the agent's window to the front if it is open. */
  const showPane = (): void => {
    if (windows.listWindows().includes("agent")) windows.showWindow("agent");
    else isPaneOpen.value = true;
  };

  /** focusPrompt shows the agent pane with its prompt box ready, starting a session if the pane shows none. */
  const focusPrompt = (): void => {
    showPane();
    if (!isPaneOpen.value) return;
    if (!active.value && current.value) start(current.value.id);
    isPromptWanted.value = !!active.value;
  };

  /** togglePane hides the agent pane if it is open here, else shows it. */
  const togglePane = (): void => {
    if (isPaneOpen.value) isPaneOpen.value = false;
    else showPane();
  };

  const sessionId = WINDOW_QUERY.get("session");
  if (sessionId) {
    void until(agents)
      .toBeTruthy()
      .then(() => {
        const record = records.value.find((other) => other.id === sessionId);
        if (record) open(record);
      });
  }

  return {
    sessions,
    records,
    listRecords,
    activeKey,
    active,
    lastAgent,
    isPaneOpen,
    isPromptWanted,
    ready,
    current,
    follow,
    status,
    warnedLimit,
    isWaiting,
    start,
    open,
    resumeLatest,
    archive,
    rename,
    deleteSession,
    reviews,
    findReview,
    showFile,
    prompt,
    configure,
    cancel,
    close,
    answer,
    signIn,
    retry,
    popOut,
    showPane,
    focusPrompt,
    togglePane,
  };
});

/** TITLE_WORDS is how many words of an agent's title a list shows. */
const TITLE_WORDS = 4;

/** describeTitle returns a session's title as a list shows it: the user's whole, the agent's cut to a few words. */
export function describeTitle(item: { title: string; isRenamed: boolean }): string {
  if (item.isRenamed) return item.title;
  const words = (item.title.split("\n")[0] ?? "").trim().split(/\s+/);
  return words.length > TITLE_WORDS ? `${words.slice(0, TITLE_WORDS).join(" ")}…` : words.join(" ");
}

/** STOPS holds the words for each reason an agent's turn ends early. */
const STOPS: Record<string, string> = {
  cancelled: "Stopped.",
  max_tokens: "The agent reached the most it can write in one answer.",
  max_turn_requests: "The agent reached the most steps it takes in one turn.",
  refusal: "The agent declined to go on.",
};

/** MESSAGES holds what each kind of update about a message writes. */
const MESSAGES: Partial<Record<string, MessageUpdate>> = {
  user_message_chunk: { kind: "user", isWhole: false },
  user_message: { kind: "user", isWhole: true },
  agent_message_chunk: { kind: "agent", isWhole: false },
  agent_message: { kind: "agent", isWhole: true },
  agent_thought_chunk: { kind: "thought", isWhole: false },
  agent_thought: { kind: "thought", isWhole: true },
};

/** applyOffers stores the settings and commands an agent offers a session. A list the offers leave out is kept. */
function applyOffers(session: Session, offers: Offers): void {
  // Version 1 names a setting's id "id".
  const options = offers.configOptions?.map((option) =>
    "configId" in option ? option : ({ ...option, configId: option.id } as unknown as v2.SessionConfigOption),
  );
  if (options) session.options = options;
  if (offers.availableCommands) session.commands = offers.availableCommands;
}

/**
 * applyUsage stores what a usage update says of a session. The tokens and the
 * size replace the last. A cost the update leaves out is kept, a null cost
 * clears it. A plan limit the update leaves out is kept.
 */
function applyUsage(session: Session, update: v2.UsageUpdate): void {
  const last = session.usage;
  const report = update._meta?.[RATE_LIMIT] as RateLimit | undefined;
  session.usage = {
    used: update.used,
    size: update.size,
    cost: update.cost === undefined ? (last?.cost ?? null) : update.cost,
    limit: report ? toPlanLimit(report) : (last?.limit ?? null),
  };
}

/**
 * toPlanLimit returns Claude's report of a plan limit as a PlanLimit. A window
 * type without words shows as "Plan limit".
 */
function toPlanLimit(report: RateLimit): PlanLimit {
  return {
    status: report.status,
    window: LIMIT_WINDOWS[report.rateLimitType ?? ""] ?? "Plan limit",
    percent: report.utilization === undefined ? null : Math.round(report.utilization * 100),
    resetsAt: report.resetsAt === undefined ? null : report.resetsAt * 1000,
  };
}

/**
 * describeLimit returns how much of a plan limit is used and when it resets,
 * such as "85% used, resets today at 14:00". It leaves out what was not sent.
 */
export function describeLimit(limit: PlanLimit): string {
  const resets = limit.resetsAt === null ? "" : `resets ${describeTime(new Date(limit.resetsAt))}`;

  return [limit.percent === null ? "" : `${limit.percent}% used`, resets].filter(Boolean).join(", ");
}

/** toPmtTool returns the name of the PMT tool a tool call's title names, or "" if it names one of the agent's own. */
export function toPmtTool(title: string): string {
  return PMT_TOOL.exec(title)?.[1] ?? "";
}

/** toAnswer returns the answer to a permission request that selects an option, or cancels it for null. */
function toAnswer(optionId: string | null): PermissionAnswer {
  return { outcome: optionId ? { outcome: "selected", optionId } : { outcome: "cancelled" } };
}

/** findOption returns the first option of a permission request that has one of the kinds, in their order. */
function findOption(options: v2.PermissionOption[], ...kinds: string[]): v2.PermissionOption | undefined {
  return kinds.flatMap((kind) => options.filter((option) => option.kind === kind))[0];
}

/** isOffered reports whether a session's option takes the value: a switch takes a boolean, a list one of its own. */
function isOffered(option: v2.SessionConfigOption, value: string | boolean): boolean {
  if (option.type === "boolean") return typeof value === "boolean";
  const values = (option as Extract<v2.SessionConfigOption, { type: "select" }>).options as (
    v2.SessionConfigSelectOption | v2.SessionConfigSelectGroup
  )[];
  return values.flatMap((row) => ("options" in row ? row.options : [row])).some((row) => row.value === value);
}

/** describeWish returns a permission prompt for a tool of PMT's: who wants to do what, and to which file if one. */
function describeWish(session: Session, tool: Tool, toolCall: v2.ToolCallUpdate | undefined): string {
  const path = findPath(toolCall);
  return `${session.agent.name} wants to ${lowerFirst(tool.title)}${path ? `: ${fileLabel(path)}` : ""}`;
}

/** findPath returns the one file a tool call names, by its locations or its input, or "" if it names none or many. */
function findPath(toolCall: v2.ToolCallUpdate | undefined): string {
  const paths = new Set(toolCall?.locations?.map((location) => location.path));
  const input = toolCall?.rawInput as { path?: unknown } | null | undefined;
  if (typeof input?.path === "string") paths.add(input.path);
  return paths.size === 1 ? ([...paths][0] ?? "") : "";
}

/** runSignIn runs a sign-in command in PMT's terminal and shows it. */
export async function runSignIn(command: string[]): Promise<void> {
  // Imported here because the terminal store needs the command table.
  await (await import("./terminal")).useTerminalStore().run(command);

  const windows = useWindowsStore();
  windows.sendCommand("terminal.show", windows.resolveTerminal());
}

/**
 * toLink returns the link an agent's request asks the user to open. It returns
 * null if the request asks for a form.
 */
export function toLink(request: v2.CreateElicitationRequest): LinkRequest | null {
  if (request.mode !== "url" || !("url" in request)) return null;

  return { id: String(request.elicitationId), message: request.message, url: String(request.url) };
}

/**
 * answerLink returns the answer to a request to open a link, and opens the
 * link first if the user accepted. Any answer but accept or decline cancels
 * the request.
 */
export function answerLink(url: string, action: string | null): v2.CreateElicitationResponse {
  if (action === "accept") void Browser.OpenURL(url);

  const choice = action === "accept" || action === "decline" ? action : "cancel";

  return { action: choice } as v2.CreateElicitationResponse;
}

/** toSignIn returns how to sign in with a method. Its command is empty if PMT asks the agent itself. */
export function toSignIn(agent: Agent, method: Method): SignIn {
  const terminalAuth = method._meta?.["terminal-auth"];
  let command: string[] = [];
  if (terminalAuth) command = [terminalAuth.command, ...(terminalAuth.args ?? [])];
  else if (method.type === "terminal" && agent.program)
    command = [agent.program, ...(agent.args ?? []), ...(method.args ?? [])];
  return { id: method.id, name: method.name, description: method.description ?? "", command };
}

/** folders returns the other mods' folders for a session, if its agent takes them. */
function folders(session: Session, start: Start): { additionalDirectories?: string[] } {
  return session.directories && start.directories?.length ? { additionalDirectories: start.directories } : {};
}

/** toText returns the text of a content block, or the name of a resource link. It returns "" for any other block. */
function toText(block: v2.ContentBlock | undefined): string {
  if (!block) return "";
  if (block.type === "text") return (block as v2.TextContent).text;
  if (block.type === "resource_link") return (block as v2.ResourceLink).name;
  return "";
}

/** toUri returns a file's percent-encoded address, with the selected lines as "#L3-10" if they are given. */
export function toUri(path: string, startLine = 0, endLine = 0): string {
  const url = new URL("file://");
  url.pathname = path.replace(/\\/g, "/");
  if (startLine) url.hash = startLine === endLine ? `L${startLine}` : `L${startLine}-${endLine}`;
  return url.href;
}

/** isAuthRequired reports whether an error is an agent's answer that the user must sign in first. */
export function isAuthRequired(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === AUTH_REQUIRED;
}
