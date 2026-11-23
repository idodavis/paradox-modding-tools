/**
 * The agent store: this window's chats with coding agents, and what the agent pane and the status bar show of them.
 * Each session speaks the Agent Client Protocol over its own stream, version 2 first and version 1 if the agent asks.
 */
import * as v1 from "@agentclientprotocol/sdk";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import * as unstable from "@agentclientprotocol/sdk/experimental/v2/unstable";
import { useQuery } from "@pinia/colada";
import { until, useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { groupBy, isEqual, partition } from "es-toolkit";
import { defineStore } from "pinia";
import { computed, markRaw, reactive, shallowRef, watch } from "vue";

import type { Agent } from "@services/agent/models";
import { FlashWindow } from "@services/app/service";
import type { Finding } from "@services/health/models";
import type { Diagnostic } from "@services/lsp/models";
import { ListNewDiagnostics } from "@services/lsp/service";
import type { Chip, Tool } from "@services/mcp/models";
import type { Place, Review } from "@services/workspace/models";
import { KeepFile, Read, UndoFile } from "@services/workspace/service";
import { confirm, reportError } from "../lib/feedback";
import { describeCost, describeError, describeTime, joinParts, plural } from "../lib/format";
import { openStream, type Start } from "../lib/stream";
import { accessQuery, agentsQuery, loadQuery, placeQuery, reviewsQuery, toolsQuery } from "../queries";
import { unsavedModels } from "../pages/ide/editor/models";
import {
  answerLink,
  describePermission,
  findOption,
  findReadOnlyFolder,
  listChangedPlaces,
  toForm,
  toLink,
  toPermissionResponse,
} from "./agentRequests";
import {
  applyMessage,
  applyToolCall,
  BUILDERS,
  IMAGE_LABEL,
  MESSAGES,
  type Entry,
  type PlanUpdate,
  type Thread,
} from "./agentThread";
import {
  applyBackground,
  applyDocument,
  applyPlanRequest,
  applyRunning,
  applySubagent,
  isPlanFile,
  receivePlan,
  resolveSubagent,
  type Subagent,
  type Work,
} from "./agentWork";
import { useEditorStore } from "./editor";
import { confirmUndo, trackEdits, type FileEdit } from "./edits";
import { useSettingsStore } from "./settings";
import { useToolViewStore } from "./toolView";
import { THIS_WINDOW, useWindowsStore, WINDOW_QUERY } from "./windows";
import { useWorkspaceStore, VANILLA } from "./workspace";

/** A Protocol is a version of the Agent Client Protocol. */
type Protocol = 1 | 2;

/** A SignIn is one way to sign in to an agent: a command for PMT's terminal, or a request to the agent. */
export type SignIn = { id: string; name: string; description: string; command: string[] };

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

/**
 * A Prompt is a prompt as it is sent: its text and chips as the transcript
 * shows them, and its blocks. A session holds the prompts written while a
 * turn runs, for an agent that takes none into a running turn.
 */
export type Prompt = { id: string; text: string; chips: string[]; blocks: v2.ContentBlock[] };

/**
 * A Session is one chat with an agent in this window, with its thread and
 * what it has beside it.
 */
export type Session = Thread &
  Work & {
    key: string; // this window's own id for it
    workspace: string; // the workspace it was started in, whose mod its agent runs in
    agent: Agent;
    protocol: Protocol;
    id: string; // the agent's id for it, "" until the agent gives one
    title: string;
    isRenamed: boolean; // the agent's titles do not replace a title the user gave
    state: "starting" | "sign-in" | "idle" | "running" | "ended";
    startedAt: number; // when its running turn started, in milliseconds; 0 while none runs
    turn: string; // the id of its running turn, or of its last
    paths: string[]; // the files that turn changed that went under review
    startCost: number; // what the session had cost when that turn started
    last: Prompt | null; // the prompt sent last, which Retry sends again
    isQuick: boolean; // whether it started on its agent's Quick choices
    /**
     * allowedPaths is null for a session the pane shows. For a session run
     * out of sight it lists the files the session may change: it is asked
     * nothing else, so every other request is refused.
     */
    allowedPaths: string[] | null;
    isFailed: boolean; // whether its last turn failed, or it ended with an error
    draft: string; // the text in its prompt box, not sent yet
    queue: Prompt[]; // the prompts held until the running turn ends, oldest first
    options: v2.SessionConfigOption[];
    commands: v2.AvailableCommand[];
    methods: SignIn[];
    usage: Usage | null; // null until the agent sends a usage update
    resumable: boolean;
    deletable: boolean;
    takesDirectories: boolean; // whether its agent takes other folders to work in
    takesImages: boolean; // whether its agent takes images in a prompt
    embeds: boolean; // whether its agent takes a file's text in a prompt
    forkable: boolean; // whether its agent can copy a session into a new one
    renamable: boolean; // whether its agent takes a title the user gives a session
    error: string;
  };

/** A SessionState is where a session stands, as the session menu marks it. */
export type SessionState = "waiting" | "working" | "failed" | "done";

/** An Image is a picture a prompt carries, as the protocol sends it. */
export type Image = Extract<v2.ContentBlock, { type: "image" }>;

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
  isFailed: boolean;
};

/** A Dialect is one version of the protocol as a session speaks it: the same steps in that version's words. */
type Dialect = {
  initialize: () => Promise<void>;
  create: (start: Start) => Promise<string>;
  resume: (id: string, start: Start) => Promise<void>;
  prompt: (id: string, blocks: v2.ContentBlock[]) => Promise<void>;
  configure: (id: string, configId: string, value: string | boolean) => Promise<Offers>;
  cancel: (id: string) => Promise<void>;
  cancelTerminal: (id: string, terminalId: string) => Promise<void>;
  cancelToolCall: (id: string, toolCallId: string) => Promise<void>;
  fork: (id: string, start: Start, cursorId: string) => Promise<string>;
  rename: (id: string, title: string) => Promise<void>;
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

/**
 * A Reply is the user's reply to an open request: a permission's option or
 * a link's action by its id, what a form was filled in with, or null to
 * cancel or decline.
 */
type Reply = string | Record<string, unknown> | null;

/**
 * Replies are what a session's open requests wait for, by the id of the
 * entry that shows each.
 */
type Replies = Map<string, (reply: Reply) => void>;

/**
 * A Connection is what a session holds that is never shown. Its start is what
 * Go said of where the agent runs, null until Go has.
 */
type Connection = { dialect: Dialect; replies: Replies; start: Start | null };

/** REQUESTS holds the kinds of entry that wait for the user's answer. */
const REQUESTS = ["permission", "link", "form"];

/** Offers are the settings and commands an agent offers a session. */
type Offers = {
  configOptions?: (v1.SessionConfigOption | v2.SessionConfigOption)[] | null;
  availableCommands?: v2.AvailableCommand[];
};

/** A RateLimit is Claude's adapter's report of a plan limit, as it sends it. */
type RateLimit = { status: PlanLimit["status"]; rateLimitType?: string; utilization?: number; resetsAt?: number };

/** PROTOCOLS is the local storage key of the version each agent answered last. */
const PROTOCOLS = "pmt.agent.protocol";
/** CHAT_OPTIONS is the local storage key of the model, effort and mode each agent was last given. */
const CHAT_OPTIONS = "pmt.agent.chat";
/** ALLOWED is the setting that holds the tools the user allowed always, each with its agent. */
export const ALLOWED = "agents.allowed";
/** The kinds of option Settings keeps a Chat and a Quick choice of. */
export const CHOICE_CATEGORIES = ["model", "thought_level"];
/** The settings of the shell rules: the prefixes allowed, and those denied. */
export const SHELL_ALLOWED = "agents.shellAllowed";
export const SHELL_DENIED = "agents.shellDenied";
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

/**
 * PMT_TOOL matches the title of a tool call for one of PMT's tools, in each
 * form agents write it: Claude mcp__pmt__hover, Codex mcp.pmt.hover, GitHub
 * Copilot's CLI pmt-hover.
 */
const PMT_TOOL = /^(?:mcp__pmt__|mcp\.pmt\.|pmt[-_:/])(.+)$/;
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
    elicitation: { url: {}, form: {} },
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
  const connections = new Map<string, Connection>();
  // The sessions whose user just refused a plan, by key, until their turn ends.
  const revising = new Set<string>();
  const protocols = useLocalStorage<Record<string, Protocol>>(PROTOCOLS, {});
  const records = useLocalStorage<SessionRecord[]>("pmt.agent.sessions", []);
  const lastAgent = useLocalStorage("pmt.agent.last", "");
  // The Chat options the user last picked for each agent, by agent and by option.
  const chatOptions = useLocalStorage<Record<string, Record<string, string | boolean>>>(CHAT_OPTIONS, {});
  // The options of each agent's quick sessions that differ from its chats'.
  const quickOptions = useLocalStorage<Record<string, Record<string, string | boolean>>>("pmt.agent.quick", {});
  // The models and efforts each agent last offered a session, by agent, so
  // that Settings lists them while no session runs.
  const offeredOptions = useLocalStorage<Record<string, v2.SessionConfigOption[]>>("pmt.agent.offeredOptions", {});
  // The sessions run out of sight, which no list of sessions holds.
  const unseen = new Set<Session>();
  // The unsent text of each remembered session, by the session's id. A text
  // whose session is no longer remembered is dropped when the window loads.
  const drafts = useLocalStorage<Record<string, string>>("pmt.agent.drafts", {});
  drafts.value = Object.fromEntries(
    Object.entries(drafts.value).filter(([id]) => records.value.some((record) => record.id === id)),
  );
  const activeKey = shallowRef("");
  // Whether the agent pane is open. It stays so across a reload.
  const isPaneOpen = useLocalStorage("pmt.agent.paneOpen", false);
  // The workspaces whose latest session this window has resumed.
  const resumed = new Set<string>();
  // Follow the agent: whether to open the files the agent's tool calls name.
  const follow = useLocalStorage("pmt.agent.follow", true);
  let count = 0;

  const isPromptWanted = shallowRef(false);
  // The models and efforts a session offers are kept for its agent.
  watch(
    () => sessions.map((session) => [session.agent.id, session.options] as const),
    (pairs) => {
      for (const [agentId, options] of pairs) {
        const keptOptions = options.filter((option) => CHOICE_CATEGORIES.includes(option.category ?? ""));
        if (keptOptions.length && !isEqual(keptOptions, offeredOptions.value[agentId]))
          offeredOptions.value = { ...offeredOptions.value, [agentId]: keptOptions };
      }
    },
  );

  const active = computed(() => sessions.find((session) => session.key === activeKey.value));
  /**
   * showLive shows the newest running session of the active workspace, or
   * none. A session of another workspace is never shown: its agent runs in
   * that workspace's mod.
   */
  const showLive = (): void => {
    activeKey.value = sessions.findLast((session) => session.workspace === workspace.activeId())?.key ?? "";
  };
  watch(() => workspace.activeId(), showLive);
  // A session's unsent text is kept under its id, so it is there again after
  // a reload.
  watch(
    () => sessions.filter((session) => session.id).map((session) => [session.id, session.draft] as const),
    (pairs) => {
      const live = new Map(pairs);
      const others = Object.entries(drafts.value).filter(([id]) => !live.has(id));
      drafts.value = Object.fromEntries([...others, ...live].filter(([, draft]) => draft));
    },
  );
  // The agents a chat can start with, which leaves out the one that completes
  // code.
  const ready = computed(() => (agents.value ?? []).filter((row) => row.program && !row.completion));
  const current = computed(
    () => active.value?.agent ?? ready.value.find((row) => row.id === lastAgent.value) ?? ready.value[0],
  );
  /** isWaiting reports whether a session waits for the user: a permission to give or a link to open. */
  const isWaiting = (session: Session): boolean => session.entries.some((entry) => REQUESTS.includes(entry.kind));
  /**
   * classify returns the state the session menu shows a remembered session
   * in: it waits for the user, it is at work, its last turn failed, or it is
   * done. A session that does not run in this window is failed or done, as
   * its last turn here left it.
   */
  const classify = (record: SessionRecord): SessionState => {
    const live = sessions.find((session) => session.id === record.id);
    if (live && (isWaiting(live) || live.state === "sign-in")) return "waiting";
    if (live?.state === "running") return "working";

    return (live ?? record).isFailed ? "failed" : "done";
  };
  // The taskbar button flashes when one more session waits for the user.
  watch(
    () => sessions.filter(isWaiting).length,
    (count, previous) => {
      if (count > previous) flash();
    },
  );

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

  /**
   * start starts a session with an agent, or the agent's replay of a record,
   * and shows it. A quick session starts on the agent's Quick choices.
   */
  function start(agentId: string, record?: SessionRecord, isQuick = false): Session {
    const agent = agents.value?.find((candidate) => candidate.id === agentId);
    if (!agent) throw new Error("PMT does not list that agent");
    const session = makeSession(agent, record);
    session.isQuick = isQuick;
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
      workspace: record?.workspace ?? workspace.activeId(),
      agent,
      protocol: protocols.value[`${agent.id}@${agent.installed}`] ?? 2,
      id: record?.id ?? "",
      title: record?.title ?? "New session",
      isRenamed: record?.isRenamed ?? false,
      state: "starting",
      startedAt: 0,
      turn: "",
      paths: [],
      startCost: 0,
      last: null,
      isQuick: false,
      allowedPaths: null,
      isFailed: record?.isFailed ?? false,
      entries: [],
      draft: (record && drafts.value[record.id]) ?? "",
      queue: [],
      terminals: {},
      checklists: [],
      subagents: [],
      background: [],
      backgroundTerminalIds: [],
      documents: [],
      shownPlan: "",
      planFolder: "",
      homePlanFolder: agent.homePlanFolder ?? "",
      options: [],
      commands: [],
      methods: [],
      usage: null,
      resumable: false,
      deletable: false,
      takesDirectories: false,
      takesImages: false,
      embeds: false,
      forkable: false,
      renamable: false,
      error: "",
    });
  }

  /** connect opens a session's stream and has the agent create or resume the session. */
  async function connect(session: Session, step: "create" | "resume"): Promise<void> {
    try {
      const { start: startMessage, dialect } = await openDialect(session);
      session.planFolder = startMessage.planFolder;
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
      // An agent keeps nothing of a session before its first turn, so a new
      // session is remembered when that turn ends.
      if (step === "resume") remember(session);
      else await applyLastOptions(session);
      await applyStartMode(session);
    } catch (error) {
      if (isAuthRequired(error)) {
        session.state = "sign-in";
        return;
      }
      Object.assign(session, { state: "ended", error: describeError(error), isFailed: true });
    }
  }

  /** openDialect opens a stream to a session's agent and wraps it for the SDK. */
  async function openDialect(session: Session): Promise<{ start: Start; dialect: Dialect }> {
    const opened = openStream(session.agent, editor.activeFile()?.path ?? "", () => {
      if (connections.get(session.key) === connection) session.state = "ended";
    });
    const replies: Replies = new Map();
    const dialect = markRaw(makeDialect(session, opened.stream, opened, replies));
    const connection: Connection = { dialect, replies, start: null };
    connections.set(session.key, connection);
    connection.start = await opened.started;

    return { start: connection.start, dialect };
  }

  /** unlink closes a session's stream and forgets its connection. */
  function unlink(session: Session): void {
    connections.get(session.key)?.dialect.close();
    connections.delete(session.key);
  }

  /** VERSIONS holds what the protocol's two versions say differently. */
  const VERSIONS = {
    2: {
      // The SDK's stable client drops what the stable schema lacks: forks,
      // titles and cursors among them.
      client: () => unstable.client(CLIENT),
      toPermission: (params: unknown) => {
        const { title, subject, options } = params as v2.RequestPermissionRequest;
        return { title, toolCall: (subject as { toolCall?: v2.ToolCallUpdate } | null)?.toolCall, options };
      },
      initialize: {
        protocolVersion: 2,
        info: CLIENT,
        capabilities: {
          auth: { terminal: {} },
          elicitation: { url: {}, form: {} },
        },
      } satisfies unstable.InitializeRequest,
      toCapabilities: (response: unknown) => {
        const { capabilities, authMethods } = response as unstable.InitializeResponse;
        return {
          resumable: true,
          deletable: !!capabilities?.session?.delete,
          takesDirectories: !!capabilities?.session?.additionalDirectories,
          takesImages: !!capabilities?.session?.prompt?.image,
          embeds: !!capabilities?.session?.prompt?.embeddedContext,
          forkable: !!capabilities?.session?.fork,
          renamable: !!capabilities?.session?.title,
          methods: (authMethods ?? []).map((method) => ({ ...method, id: method.methodId })),
        };
      },
      resume: (id: string, start: Start, params: object): [string, object] => [
        "session/resume",
        {
          sessionId: id,
          cwd: start.folder,
          mcpServers: listServers(),
          replayFrom: { type: "start" },
          ...params,
        } satisfies v2.ResumeSessionRequest,
      ],
      configure: (id: string, configId: string, value: string | boolean) =>
        (typeof value === "boolean"
          ? { sessionId: id, configId, type: "boolean", value }
          : { sessionId: id, configId, type: "id", value }) satisfies v2.SetSessionConfigOptionRequest,
      login: "auth/login",
      endsTurnOnPrompt: false,
      keysPrompts: true,
    },
    1: {
      client: () => v1.client(CLIENT),
      toPermission: (params: unknown) => {
        const { toolCall, options } = params as v1.RequestPermissionRequest;
        return { title: toolCall.title ?? "", toolCall: toolCall as v2.ToolCallUpdate, options };
      },
      initialize: V1_INITIALIZE,
      toCapabilities: (response: unknown) => {
        const { agentCapabilities, authMethods } = response as v1.InitializeResponse;
        return {
          resumable: !!agentCapabilities?.loadSession,
          deletable: !!agentCapabilities?.sessionCapabilities?.delete,
          takesDirectories: !!agentCapabilities?.sessionCapabilities?.additionalDirectories,
          takesImages: !!agentCapabilities?.promptCapabilities?.image,
          embeds: !!agentCapabilities?.promptCapabilities?.embeddedContext,
          forkable: !!(agentCapabilities?.sessionCapabilities as { fork?: object } | undefined)?.fork,
          renamable: !!(agentCapabilities?.sessionCapabilities as { title?: object } | undefined)?.title,
          methods: authMethods ?? [],
        };
      },
      resume: (id: string, start: Start, params: object): [string, object] => [
        "session/load",
        { sessionId: id, cwd: start.folder, mcpServers: listServers(), ...params } satisfies v1.LoadSessionRequest,
      ],
      configure: (id: string, configId: string, value: string | boolean) =>
        (typeof value === "boolean"
          ? { sessionId: id, configId, type: "boolean", value }
          : { sessionId: id, configId, value }) satisfies v1.SetSessionConfigOptionRequest,
      login: "authenticate",
      endsTurnOnPrompt: true,
      keysPrompts: false,
    },
  };

  /** makeDialect makes the Dialect for a session's version of the protocol over a stream. */
  function makeDialect(session: Session, stream: v2.Stream, socket: { close: () => void }, replies: Replies): Dialect {
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
    client.onNotification("session/update", ({ params }) => {
      const { sessionId, update } = params as v2.UpdateSessionNotification;
      // A subagent's updates come on its parent's stream under its own id.
      if (!session.id || sessionId === session.id) applyUpdate(session, update);
      else applySubagentUpdate(session, resolveSubagent(session, sessionId), update);
    });
    client.onRequest("session/request_permission", ({ params }) => {
      const { title, toolCall, options } = row.toPermission(params);
      // The call a subagent asks about joins that subagent's thread.
      const asker = (params as { sessionId?: string }).sessionId;
      const thread = asker && session.id && asker !== session.id ? resolveSubagent(session, asker) : session;
      return askPermission(session, thread, replies, title, toolCall, options);
    });
    client.onRequest("elicitation/create", ({ params }) =>
      elicit(session, replies, params as v2.CreateElicitationRequest),
    );
    client.onNotification("elicitation/complete", ({ params }) =>
      closeEntry(session, replies, (params as v2.CompleteElicitationNotification).elicitationId, null),
    );
    const connection = client.connect(stream);
    const agent = connection.agent;
    // How many prompts the agent has not answered yet.
    let unanswered = 0;

    /** locate returns the folder the agent keeps a session by, "" if unlisted. */
    const locate = async (id: string): Promise<string> => {
      type Page = { sessions?: { sessionId: string; cwd: string }[]; nextCursor?: string | null };
      let cursor: string | null | undefined;
      do {
        const page = (await agent.request("session/list", cursor ? { cursor } : {}).catch(() => ({}))) as Page;
        const listed = page.sessions?.find((other) => other.sessionId === id);
        if (listed) return listed.cwd;
        cursor = page.nextCursor;
      } while (cursor);

      return "";
    };

    return {
      initialize: async () => {
        const { methods, ...capabilities } = row.toCapabilities(await agent.request("initialize", row.initialize));
        Object.assign(session, capabilities, { methods: methods.map((method) => toSignIn(session.agent, method)) });
      },
      create: async (start) => {
        const request = { cwd: start.folder, mcpServers: listServers(), ...toSessionParams(session, start) };
        const response = (await agent.request("session/new", request)) as Offers & { sessionId: string };
        applyOffers(session, response);
        return response.sessionId;
      },
      resume: async (id, start) => {
        if (!session.resumable) throw new Error(`${session.agent.name} cannot replay a session`);
        // An agent replays a session's history only from the folder the
        // session was made in, which is not always the one it starts in now.
        const folder = (await locate(id)) || start.folder;
        const others = [start.folder, ...(start.folders ?? [])].filter((other) => other !== folder);
        const own = { ...start, folder, folders: others };
        applyOffers(session, (await agent.request(...row.resume(id, own, toSessionParams(session, own)))) as Offers);
      },
      prompt: async (id, blocks) => {
        startTurn(session);
        // A key lets the agent run a prompt once if the stream sends it twice.
        const key = row.keysPrompts ? { idempotencyKey: crypto.randomUUID() } : {};
        const request = { sessionId: id, prompt: blocks, ...key };
        unanswered++;
        let response: { stopReason: string; usage?: unstable.Usage | null };
        try {
          response = (await agent.request("session/prompt", request)) as typeof response;
        } finally {
          unanswered--;
        }
        // A prompt taken into a running turn is answered with it, so the turn
        // is over once every prompt is answered.
        if (row.endsTurnOnPrompt && !unanswered)
          finishTurn(session, response.stopReason, "", response.usage?.totalTokens);
      },
      configure: async (id, configId, value) =>
        (await agent.request("session/set_config_option", row.configure(id, configId, value))) as Offers,
      cancel: (id) => agent.notify("session/cancel", { sessionId: id }),
      cancelTerminal: async (id, terminalId) =>
        void (await agent.request("terminal/cancel", { sessionId: id, terminalId })),
      cancelToolCall: async (id, toolCallId) =>
        void (await agent.request("tool_call/cancel", { sessionId: id, toolCallId })),
      fork: async (id, start, cursorId) => {
        const point = cursorId ? { cursorId } : {};
        const request = {
          sessionId: id,
          cwd: start.folder,
          mcpServers: listServers(),
          ...toSessionParams(session, start),
          ...point,
        };

        return ((await agent.request("session/fork", request)) as { sessionId: string }).sessionId;
      },
      rename: async (id, title) => void (await agent.request("session/set_title", { sessionId: id, title })),
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

  /**
   * applyUpdate applies one update an agent sent about a session: what
   * builds its thread, then its title, its state and what works in the
   * background, its subagents, its settings and its usage.
   */
  function applyUpdate(session: Session, update: v2.SessionUpdate): void {
    const kind = update.sessionUpdate;
    if (applyThread(session, session, update)) return;

    if (kind === "session_info_update") {
      const title = (update as v2.SessionInfoUpdate).title;
      if (title && !session.isRenamed) session.title = title;
      remember(session);
    } else if (kind === "state_update") {
      const state = update as v2.StateUpdate & v2.IdleStateUpdate & v2.ErrorStopReason & { usage?: unstable.Usage };
      if (state.background) applyBackground(session, state.background);
      if (state.state === "running") startTurn(session);
      else if (state.state === "idle")
        finishTurn(session, state.stopReason ?? "end_turn", state.error?.message, state.usage?.totalTokens);
    } else if (kind === "subagent_update") {
      const subagentUpdate = update as v2.SubagentUpdate;
      applySubagent(resolveSubagent(session, subagentUpdate.sessionId), subagentUpdate);
    } else if (kind === "config_option_update" || kind === "available_commands_update") {
      applyOffers(session, update as Offers);
    } else if (kind === "usage_update") {
      applyUsage(session, update as v2.UsageUpdate);
    }
  }

  /**
   * applySubagentUpdate applies one update a subagent of a session sent: what
   * builds its thread, its state, and the message that started it, which is
   * its first prompt.
   */
  function applySubagentUpdate(session: Session, subagent: Subagent, update: v2.SessionUpdate): void {
    if (applyThread(session, subagent, update)) return;

    if (update.sessionUpdate === "state_update") applyRunning(subagent, (update as { state: string }).state);
    else if (update.sessionUpdate === "session_message") {
      // A message from the subagent is its answer to the session's agent,
      // and one to it is that agent's prompt.
      const message = update as v2.SessionMessage;
      const kind = message.senderSessionId === subagent.id ? "agent" : "user";
      applyMessage(subagent, message, { kind, isWhole: true });
    }
  }

  /**
   * applyThread applies an update that builds a thread, a session's own or
   * one of its subagents': a message, a tool call, a plan or a terminal. It
   * returns false for any other update.
   */
  function applyThread(session: Session, thread: Thread, update: v2.SessionUpdate): boolean {
    const kind: string = update.sessionUpdate;
    const message = MESSAGES[kind];
    if (message) applyMessage(thread, update as v2.ContentChunk | v2.UserMessage, message);
    else if (kind === "tool_call" || kind === "tool_call_update" || kind === "tool_call_content_chunk")
      receiveToolCall(session, thread, update as v2.ToolCallUpdate | v2.ToolCallContentChunk);
    else if (kind === "plan" || kind === "plan_update") receivePlan(session, thread, update as PlanUpdate);
    else if (kind in BUILDERS) BUILDERS[kind]?.(thread, update as never);
    else return false;

    return true;
  }

  /**
   * receiveToolCall applies an update about a tool call to a thread of a
   * session. It starts the review of the files a call changed once the call is
   * done, and shows the file the call names if the user follows the agent.
   */
  function receiveToolCall(
    session: Session,
    thread: Thread,
    update: v2.ToolCallUpdate | v2.ToolCallContentChunk,
  ): void {
    const { entry, isNewlyDone } = applyToolCall(thread, update);
    // A resumed session replays its old calls while it starts. Their edits
    // are old, so they start no review. Nor does an edit of a plan file,
    // which an agent writes to often: it is a plan, not a change to review.
    // An edit that gives the plan file's whole text is the plan document.
    const [planEdits, otherEdits] = partition(entry.edits, (edit) => isPlanFile(session, edit.path));
    if (isNewlyDone && session.state !== "starting") {
      startReviews(session, otherEdits);
      for (const edit of planEdits.filter((candidate) => !candidate.patch))
        applyDocument(session, { id: edit.path, text: edit.hunks.at(-1)?.after.join("\n") ?? "", path: edit.path });
    }
    const location = "locations" in update ? update.locations?.[0] : undefined;
    if (follow.value && location) void toolView.followFile(location.path, location.line ?? 1);
  }

  /**
   * startReviews sends Go each file the given edits changed, so that Go can
   * start the file's review. A review that starts shows the agent pane once;
   * a file already under review leaves the pane as the user left it. A file
   * Go cannot review says why on its edits. A review that starts outside the
   * workspace is told in the session, since an agent in a mode that asks
   * nothing changes such a file unasked.
   */
  function startReviews(session: Session, edits: FileEdit[]): void {
    for (const [path, fileEdits] of Object.entries(groupBy(edits, (edit) => edit.path))) {
      const isReviewed = !!findReview(path);
      trackEdits(fileEdits, session.turn)
        .then(async () => {
          if (!session.paths.includes(path)) session.paths.push(path);
          if (isReviewed) return;
          // A session run out of sight leaves the pane as it is.
          if (!session.allowedPaths) showPane();
          const place = await loadQuery(placeQuery(path));
          if (place && !place.root)
            session.entries.push({
              kind: "notice",
              id: `n${session.entries.length}`,
              text: `${session.agent.name} changed a file outside the workspace: ${path}. It is under review.`,
            });
        })
        .catch((error: unknown) => {
          fileEdits.forEach((edit) => (edit.reviewError = describeError(error)));
        });
    }
  }

  /**
   * startTurn marks a session's turn as running, from now and under a new id
   * if none ran.
   */
  function startTurn(session: Session): void {
    if (session.state !== "running")
      Object.assign(session, {
        startedAt: Date.now(),
        turn: crypto.randomUUID(),
        paths: [],
        startCost: session.usage?.cost?.amount ?? 0,
      });
    session.state = "running";
  }

  /**
   * finishTurn marks a session's turn over, with a notice if the turn did not
   * end normally: the words for its reason, then the agent's own message if
   * it gave one. The transcript then gets the turn's record: how long it
   * took, the tokens the agent says it used, what it cost and the files it
   * changed. A turn that ended normally is followed by the oldest queued
   * prompt; one that was stopped or failed leaves the queue as it is.
   */
  function finishTurn(session: Session, reason: string, message = "", tokens = 0): void {
    const text = [STOPS[reason] ?? reason, message].filter(Boolean).join(" ");
    // The error that ends the turn of a plan the user refused is expected.
    const isExpected = reason === "end_turn" || (revising.delete(session.key) && reason === "error");
    if (!isExpected) session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text });
    // An agent also says it is idle when no turn ran, as a resumed one does.
    if (session.startedAt) {
      const cost = session.usage?.cost;
      session.entries.push({
        kind: "turn",
        id: session.turn,
        duration: Date.now() - session.startedAt,
        tokens,
        cost: cost ? describeCost({ ...cost, amount: cost.amount - session.startCost }) : "",
        isUnfinished: !isExpected,
        paths: session.paths,
        problems: [],
      });
      void diagnoseTurn(session.entries.at(-1));
    }

    Object.assign(session, { state: "idle", startedAt: 0, isFailed: !isExpected && reason !== "cancelled" });
    remember(session);
    const next = reason === "end_turn" ? session.queue.shift() : undefined;
    if (next) void send(session, next);
    else flash();
  }

  /**
   * diagnoseTurn fills a turn's record with the problems the turn's files
   * have that their texts from before the agent's changes do not. A file Go
   * cannot judge, or whose review is over, adds none.
   */
  async function diagnoseTurn(turn?: Entry): Promise<void> {
    if (turn?.kind !== "turn") return;

    for (const path of turn.paths) {
      const problems = (await ListNewDiagnostics(path).catch(() => null)) ?? [];
      const place = problems.length ? await loadQuery(placeQuery(path)) : undefined;
      if (place) turn.problems.push({ place, problems });
    }
  }

  /**
   * askPermission answers a permission request at once if PMT may, else shows
   * it in a session and answers the agent with the option the user picks. A
   * request to change a file PMT keeps read-only (the game's, an external
   * mod's) is refused at once, with a notice, whatever the user allowed
   * always. A request that changes only the agent's own plan files is allowed
   * at once. A request to change another file outside the workspace, or to
   * run a shell command that names a read-only folder, is always shown, says
   * so, and offers no "allow always".
   */
  async function askPermission(
    session: Session,
    thread: Thread,
    replies: Replies,
    title: string,
    toolCall: v2.ToolCallUpdate | undefined,
    options: v2.PermissionOption[],
  ): Promise<v2.RequestPermissionResponse> {
    if (toolCall) receiveToolCall(session, thread, toolCall);
    const places = await listChangedPlaces(toolCall);
    const readOnlyPlace = places.find((place) => place.readOnly);
    if (readOnlyPlace) {
      const owner =
        readOnlyPlace.origin === VANILLA
          ? "one of the game's own files"
          : `in ${workspace.originName(readOnlyPlace.origin)}, an external mod`;
      session.entries.push({
        kind: "notice",
        id: `n${session.entries.length}`,
        text: `Refused: ${readOnlyPlace.rel || readOnlyPlace.name} is ${owner}.`,
      });
      return toPermissionResponse(findOption(options, "reject_once", "reject_always")?.optionId ?? null);
    }

    // An agent writes its own plan files often, and they are no change to
    // the mods: a call that changes nothing else is not asked about.
    if (places.length && places.every((place) => isPlanFile(session, place.path)))
      return toPermissionResponse(findOption(options, "allow_once", "allow_always")?.optionId ?? null);

    const toolTitle = toolCall?.title || title;
    // GitHub Copilot's CLI names some calls of PMT's tools by the tool's own
    // title, "Read a file", with the kind it gives a tool of another server.
    const tool = tools.value?.find(
      (row) => row.name === toPmtTool(toolTitle) || (toolCall?.kind === "other" && row.title === toolTitle),
    );
    const scope = toolCall?.kind && toolCall.kind in FILE_KINDS ? toolCall.kind : toolTitle;
    const permit = `${session.agent.id}:${scope.replaceAll(",", ";")}`;
    const foreignPlace = places.find((place) => !place.root);
    const readOnlyFolder = await findReadOnlyFolder(toolCall);
    // Neither is ever allowed for good: each is asked again, every time.
    const isOnce = !!foreignPlace || !!readOnlyFolder;
    // A request to start on a plan is always asked too, and its answer is
    // never kept: the agents offer "use auto mode" as an allow always, which
    // would approve every later plan unseen.
    const isPlan = toolCall?.kind === "switch_mode";
    const autoOption = isOnce || isPlan ? undefined : pickAllowed(tool, permit, options);
    if (autoOption) return toPermissionResponse(autoOption.optionId);
    // A session run out of sight has no one to ask: it may change the files
    // it was started for, and nothing else.
    if (session.allowedPaths) {
      const isOwn = places.length > 0 && places.every((place) => session.allowedPaths?.includes(place.path));
      const kinds = isOwn ? ["allow_once", "allow_always"] : ["reject_once", "reject_always"];

      return toPermissionResponse(findOption(options, ...kinds)?.optionId ?? null);
    }

    const id = `p${session.entries.length}`;
    let permissionTitle = `${session.agent.name} asks: ${toolTitle || "allow this?"}`;
    if (tool) permissionTitle = describePermission(session.agent.name, tool, toolCall);
    if (foreignPlace)
      permissionTitle = `${session.agent.name} wants to change a file outside the workspace: ${foreignPlace.path}`;
    if (readOnlyFolder)
      permissionTitle =
        `${session.agent.name} wants to run a command that names ${workspace.originName(readOnlyFolder.origin)}, ` +
        `which is read-only. PMT cannot see what the command does there: ${toolTitle}`;
    const document = isPlan ? applyPlanRequest(session, toolCall) : undefined;
    if (document) document.request = id;
    session.entries.push({
      kind: "permission",
      id,
      title: permissionTitle,
      permit: isPlan ? "" : permit,
      options: options
        .filter((option) => !isOnce || option.kind !== "allow_always")
        .map((option) => ({ id: option.optionId, name: option.name, kind: option.kind })),
      plan: document?.id,
    });
    return new Promise((resolve) =>
      replies.set(id, (optionId) => resolve(toPermissionResponse(typeof optionId === "string" ? optionId : null))),
    );
  }

  /**
   * pickAllowed returns the option that answers a permission request without
   * asking the user: for a tool of PMT's that only reads, the one that allows
   * it once, and for a tool the user allowed always, the one that allows it
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

  /**
   * elicit shows what an agent asks of the user beside a permission: a link
   * to open, or a form to fill in. It answers with what the user chose or
   * filled in, and declines a request that is neither.
   */
  function elicit(
    session: Session,
    replies: Replies,
    request: v2.CreateElicitationRequest,
  ): Promise<v2.CreateElicitationResponse> {
    if (session.allowedPaths) return Promise.resolve({ action: "decline" });

    const link = toLink(request);
    const form = link ? null : toForm(request, `f${session.entries.length}`);
    if (link) session.entries.push({ kind: "link", ...link });
    if (form) session.entries.push({ kind: "form", ...form });

    return new Promise((resolve) => {
      if (link)
        replies.set(link.id, (action) => resolve(answerLink(link.url, typeof action === "string" ? action : null)));
      else if (form)
        replies.set(form.id, (content) =>
          resolve(typeof content === "object" && content ? { action: "accept", content } : { action: "decline" }),
        );
      else resolve({ action: "decline" });
    });
  }

  /** closeEntry answers a session's open request and removes it from the transcript. */
  function closeEntry(session: Session, replies: Replies, id: string, reply: Reply): void {
    replies.get(id)?.(reply);
    replies.delete(id);
    session.entries = session.entries.filter((entry) => !(REQUESTS.includes(entry.kind) && entry.id === id));
  }

  /**
   * reply answers a session's open request: a permission with one of its
   * options, a link with an action, a form with what the user filled in. Null
   * cancels a permission or a link and declines a form.
   */
  const reply = (session: Session, id: string, reply: Reply): void => {
    const connection = connections.get(session.key);
    if (!connection) return;
    const entry = session.entries.find((candidate) => candidate.kind === "permission" && candidate.id === id);
    const option = entry?.kind === "permission" ? entry.options.find((row) => row.id === reply) : undefined;
    // A request without a permit is never allowed for good.
    if (entry?.kind === "permission" && entry.permit && option?.kind === "allow_always")
      settings.writeItem(ALLOWED, entry.permit, true);
    const document = session.documents.find((other) => other.request === id);
    const isApproved = !!option?.kind.startsWith("allow");
    if (document) Object.assign(document, { request: "", isApproved });
    // Claude ends the turn of a refused plan with an error of its own, which
    // says nothing to the user.
    if (document && !isApproved) revising.add(session.key);
    closeEntry(session, connection.replies, id, reply);
  };

  /**
   * prompt sends the user's text to a session, with the picked chips, then
   * the page's own, then the pictures. While a turn runs, an agent that
   * steers gets it at once, into that turn; for any other agent it joins the
   * session's queue.
   */
  const prompt = async (session: Session, text: string, pickedChips: Chip[], images: Image[] = []): Promise<void> => {
    const isRunning = session.state === "running";
    if (session.state !== "idle" && !isRunning) return;
    // A command goes without the page's chip: Claude takes "/compact" with
    // a file attached for plain words and runs nothing.
    const pageChip = text.startsWith("/") ? null : toolView.promptChip;
    const chips = [...pickedChips, ...(pageChip ? [pageChip] : [])];
    const chipBlocks = chips.flatMap((chip) => toBlocks(chip, session.embeds));
    const blocks: v2.ContentBlock[] = [{ type: "text", text }, ...chipBlocks, ...images];
    // An agent that drops the tool server's instructions gets them once,
    // after the user's own words, which name the session.
    if (session.agent.instructions && !session.entries.some((entry) => entry.kind === "user"))
      blocks.push({ type: "text", text: session.agent.instructions });
    const labels = [...chips.map((chip) => chip.label), ...images.map(() => IMAGE_LABEL)];
    const prompt = { id: crypto.randomUUID(), text, chips: labels, blocks };
    if (isRunning && !session.agent.steers) session.queue.push(prompt);
    else await send(session, prompt);
  };

  /**
   * send sends a prompt to a session's agent, and shows it in the transcript
   * where the agent does not send it back.
   */
  async function send(session: Session, prompt: Prompt): Promise<void> {
    const connection = connections.get(session.key);
    if (!connection) return;
    session.last = prompt;
    // A version 2 agent sends the prompt back, but a version 1 agent does not.
    if (session.protocol === 1)
      session.entries.push({ kind: "user", id: `u${session.entries.length}`, text: prompt.text, chips: prompt.chips });
    try {
      await connection.dialect.prompt(session.id, prompt.blocks);
    } catch (error) {
      finishTurn(session, describeError(error));
    }
  }

  /**
   * sendNow sends one of a session's queued prompts now. It stops a running
   * turn first and waits for it to end.
   */
  const sendNow = async (session: Session, id: string): Promise<void> => {
    const prompt = session.queue.find((other) => other.id === id);
    if (!prompt) return;
    unqueue(session, id);
    if (session.state === "running") {
      cancel(session);
      await until(() => session.state).not.toBe("running");
    }
    await send(session, prompt);
  };

  /**
   * run has the agent used last do one job out of sight and returns its
   * answer: a session of its own on the agent's Quick choices, which no list
   * shows and which ends with its turn. The session may change the given
   * files and is refused everything else it asks. It rejects if no agent can
   * start, or with the agent's words if the turn did not end normally.
   */
  const run = async (text: string, allowedPaths: string[] = []): Promise<string> => {
    if (!current.value) throw new Error("No agent is installed");
    const session = makeSession(current.value);
    Object.assign(session, { isQuick: true, allowedPaths });
    unseen.add(session);
    try {
      await connect(session, "create");
      if (session.state !== "idle") throw new Error(session.error || `${session.agent.name} needs you to sign in`);
      void send(session, { id: crypto.randomUUID(), text, chips: [], blocks: [{ type: "text", text }] });
      await until(() => session.state).not.toBe("running");
      const turn = session.entries.findLast((entry) => entry.kind === "turn");
      const notice = session.entries.findLast((entry) => entry.kind === "notice");
      if (turn?.kind !== "turn" || turn.isUnfinished)
        throw new Error(notice?.kind === "notice" ? notice.text : "No answer");

      const answer = session.entries.findLast((entry) => entry.kind === "agent");

      return answer?.kind === "agent" ? answer.text : "";
    } finally {
      unlink(session);
      unseen.delete(session);
    }
  };

  /** cancelUnseen stops the turn of every session that runs out of sight. */
  const cancelUnseen = (): void => unseen.forEach(cancel);

  /**
   * retry sends a session's last prompt again, which is what Retry does
   * after a turn that was stopped or failed. It does nothing while a turn
   * runs.
   */
  const retry = (session: Session): void => {
    if (session.last && session.state === "idle") void send(session, session.last);
  };

  /** unqueue takes a prompt out of a session's queue. */
  const unqueue = (session: Session, id: string): void => {
    session.queue = session.queue.filter((prompt) => prompt.id !== id);
  };

  /**
   * configure sets one of a session's settings and keeps the value for the
   * agent's next session, unless told to keep none, as Plan's switch does:
   * planning is one session's. A mode in which the agent asks PMT nothing is
   * confirmed first and is never kept, so each session is asked again. If
   * the agent refuses, the error goes in the transcript.
   */
  const configure = async (
    session: Session,
    configId: string,
    value: string | boolean,
    isKept = true,
  ): Promise<void> => {
    const connection = connections.get(session.key);
    if (!connection) return;
    const option = session.options.find((row) => row.configId === configId);
    const isBypass = !!option && isBypassMode(session.agent, option, value);
    if (isBypass && !(await confirmBypass(session.agent, option, value))) return;
    try {
      applyOffers(session, await connection.dialect.configure(session.id, configId, value));
      if (isKept && !isBypass) choose(session.agent.id, session.isQuick, configId, value);
    } catch (error) {
      session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text: describeError(error) });
    }
  };

  /**
   * resolveKeptChoices returns the options kept for an agent's next session,
   * by option: its Chat choices, or its Quick ones, which are the Chat choices
   * but where a Quick one is set.
   */
  const resolveKeptChoices = (agentId: string, isQuick: boolean): Record<string, string | boolean> => ({
    ...chatOptions.value[agentId],
    ...(isQuick ? quickOptions.value[agentId] : {}),
  });

  /**
   * choose keeps a value of one option for an agent's next session, among
   * its Chat choices or its Quick ones.
   */
  const choose = (agentId: string, isQuick: boolean, configId: string, value: string | boolean): void => {
    const choices = isQuick ? quickOptions : chatOptions;
    choices.value = { ...choices.value, [agentId]: { ...choices.value[agentId], [configId]: value } };
  };

  /**
   * applyLastOptions gives a new session each option kept for its agent, the
   * Chat choices or the Quick ones, that the session offers and has
   * otherwise. A mode in which the agent asks PMT nothing is left out,
   * whatever was kept before.
   */
  async function applyLastOptions(session: Session): Promise<void> {
    for (const [configId, value] of Object.entries(resolveKeptChoices(session.agent.id, session.isQuick))) {
      const option = session.options.find((row) => row.configId === configId);
      if (!option || option.currentValue === value || !isOffered(option, value)) continue;
      if (!isBypassMode(session.agent, option, value)) await configure(session, configId, value);
    }
  }

  /**
   * applyStartMode puts a session that starts in a mode in which its agent
   * asks PMT nothing into the mode the agent's row starts in. Codex starts
   * every session in such a mode by itself.
   */
  async function applyStartMode(session: Session): Promise<void> {
    const { startMode } = session.agent;
    const mode = session.options.find((option) => isBypassMode(session.agent, option, option.currentValue));
    if (!mode || !startMode || !isOffered(mode, startMode)) return;
    const offers = await connections.get(session.key)?.dialect.configure(session.id, mode.configId, startMode);
    if (offers) applyOffers(session, offers);
  }

  /** cancel asks a session's agent to stop its turn. */
  const cancel = (session: Session): void => void connections.get(session.key)?.dialect.cancel(session.id);

  /** report shows and logs a failure of what the user asked of an agent. */
  const report = (error: unknown): void => reportError(error, "agent");

  /**
   * cancelSubagent asks a session's agent to stop one subagent, which leaves
   * the turn going.
   */
  const cancelSubagent = (session: Session, subagent: Subagent): void =>
    void connections.get(session.key)?.dialect.cancel(subagent.id).catch(report);

  /**
   * cancelTerminal asks a session's agent to stop one terminal of the
   * session's own thread or of a subagent's, which leaves the turn going.
   */
  const cancelTerminal = (session: Session, thread: Session | Subagent, terminalId: string): void =>
    void connections.get(session.key)?.dialect.cancelTerminal(thread.id, terminalId).catch(report);

  /**
   * cancelToolCall asks a session's agent to stop one tool call of the
   * session's own thread or of a subagent's, which leaves the turn going.
   */
  const cancelToolCall = (session: Session, thread: Session | Subagent, toolCallId: string): void =>
    void connections.get(session.key)?.dialect.cancelToolCall(thread.id, toolCallId).catch(report);

  /** close ends a session here: its agent stops, and its record stays in the sessions list. */
  const close = (session: Session): void => {
    unlink(session);
    sessions.splice(sessions.indexOf(session), 1);
    if (activeKey.value === session.key) showLive();
  };

  /** signIn signs in to a session's agent, in PMT's terminal or through the agent, and starts the session again. */
  const signIn = async (session: Session, method: SignIn): Promise<void> => {
    if (method.command.length) return runSignIn(method.command);

    await connections.get(session.key)?.dialect.signIn(method.id);
    unlink(session);
    session.state = "starting";
    void connect(session, session.id ? "resume" : "create");
  };

  /** restart starts a session's agent again after a sign-in in the terminal or an error. */
  const restart = (session: Session): void => {
    unlink(session);
    Object.assign(session, { state: "starting", error: "" });
    void connect(session, session.id ? "resume" : "create");
  };

  /** listRecords lists the remembered sessions of the active workspace, newest first. */
  const listRecords = (): SessionRecord[] =>
    records.value.filter((record) => record.workspace === workspace.activeId());

  /** remember writes a session to every window's sessions list, newest first. */
  function remember(session: Session): void {
    if (!session.id || session.allowedPaths) return;
    const previous = records.value.find((other) => other.id === session.id);
    const record = {
      agent: session.agent.id,
      workspace: session.workspace,
      id: session.id,
      title: session.title,
      updatedAt: Date.now(),
      resumable: session.resumable,
      deletable: session.deletable,
      isArchived: previous?.isArchived ?? false,
      isRenamed: session.isRenamed,
      isFailed: session.isFailed,
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
    // The agent keeps the title too, where it takes one.
    if (live?.renamable) void connections.get(live.key)?.dialect.rename(id, text).catch(report);
  };

  /**
   * fork copies a session into a new one and shows the copy: the whole
   * session, or what came before one of the user's prompts, with that
   * prompt's text in the copy's prompt box to write again. It does nothing
   * for an agent that cannot copy a session.
   */
  const fork = async (session: Session, prompt?: Extract<Entry, { kind: "user" }>): Promise<void> => {
    const connection = connections.get(session.key);
    if (!connection?.start || !session.forkable) return;
    try {
      const id = await connection.dialect.fork(session.id, connection.start, prompt?.cursor ?? "");
      const copy = start(session.agent.id, {
        agent: session.agent.id,
        workspace: session.workspace,
        id,
        title: session.title,
        updatedAt: Date.now(),
        resumable: true,
        deletable: session.deletable,
        isArchived: false,
        isRenamed: session.isRenamed,
        isFailed: false,
      });
      if (prompt) copy.draft = prompt.text;
    } catch (error) {
      report(error);
    }
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

  /**
   * resolveAgentWindow returns the window that shows the agent: the agent's
   * own if it is open, else the main one.
   */
  const resolveAgentWindow = (): string => (windows.listWindows().includes("agent") ? "agent" : "main");
  // What a window hands the agent's window for its prompt box: a text, or ""
  // for the caret alone. The time makes each one new.
  const handoff = useLocalStorage("pmt.agent.handoff", { sentAt: 0, text: "" });
  watch(handoff, ({ text }) => {
    if (THIS_WINDOW === resolveAgentWindow()) ask(text);
  });

  /**
   * ask shows the agent pane with the caret in its prompt box, and puts the
   * given text there unless it is "". It starts a session if the pane shows
   * none. In a window that is not the agent's own, that window does it.
   */
  function ask(text = ""): void {
    const home = resolveAgentWindow();
    if (THIS_WINDOW !== home) {
      handoff.value = { sentAt: Date.now(), text };
      windows.showWindow(home);
      return;
    }

    showPane();
    if (!active.value && current.value) start(current.value.id);
    if (text && active.value) active.value.draft = text;
    isPromptWanted.value = !!active.value;
  }

  // A Markdown file written in a session's plans folder while its turn runs
  // is a plan of that session, whichever way its agent wrote it.
  Events.On("fs:changed", ({ data }) => {
    const { path, deleted } = data as { path: string; deleted: boolean };
    const writers = deleted
      ? []
      : sessions.filter((session) => session.state === "running" && isPlanFile(session, path));
    for (const session of writers)
      void Read(path)
        .then((file) => applyDocument(session, { id: path, text: file.text.text, path }))
        .catch(() => undefined);
  });

  /**
   * showPlan has a session's pinned row show the plan document with the
   * given id.
   */
  const showPlan = (session: Session, id: string): void => {
    session.shownPlan = id;
  };

  /** setDraft sets the unsent text of a session's prompt box. */
  const setDraft = (session: Session, text: string): void => {
    session.draft = text;
  };

  /** keep leaves files as the agents wrote them, which ends their reviews. */
  const keep = (places: Place[]): void => void Promise.all(places.map((place) => KeepFile(place.path))).catch(report);

  /**
   * undo asks, then takes the agents' edits to the given files back: every
   * edit under review, or with a turn's id those of that turn and of the
   * turns after it.
   */
  const undo = async (places: Place[], turn = ""): Promise<void> => {
    if (!(await confirmUndo(places, !!turn))) return;

    const undonePaths = await Promise.all(
      places.map((place) =>
        UndoFile(place.path, turn)
          .then(() => place.path)
          .catch(report),
      ),
    );
    // An undone turn, and each turn after it, no longer changed those files.
    for (const session of sessions) {
      const first = session.entries.findIndex((entry) => entry.kind === "turn" && entry.id === turn);
      for (const entry of first < 0 ? [] : session.entries.slice(first))
        if (entry.kind === "turn") entry.paths = entry.paths.filter((path) => !undonePaths.includes(path));
    }
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
    classify,
    start,
    open,
    resumeLatest,
    archive,
    rename,
    fork,
    deleteSession,
    reviews,
    findReview,
    showFile,
    prompt,
    sendNow,
    run,
    cancelUnseen,
    offeredOptions,
    resolveKeptChoices,
    choose,
    retry,
    unqueue,
    configure,
    cancel,
    cancelSubagent,
    cancelTerminal,
    cancelToolCall,
    close,
    reply,
    signIn,
    restart,
    popOut,
    showPane,
    ask,
    showPlan,
    setDraft,
    keep,
    undo,
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
  error: "The turn ended with an error.",
  max_tokens: "The agent reached the most it can write in one answer.",
  max_turn_requests: "The agent reached the most steps it takes in one turn.",
  refusal: "The agent declined to go on.",
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

/**
 * listChoices returns the values a session's option offers, its groups'
 * values among them. A switch offers none.
 */
export function listChoices(option: v2.SessionConfigOption): v2.SessionConfigSelectOption[] {
  if (option.type !== "select") return [];
  const options = option.options as (v2.SessionConfigSelectOption | v2.SessionConfigSelectGroup)[];

  return options.flatMap((row) => ("options" in row ? row.options : [row]));
}

/**
 * isOffered reports whether a session's option takes the value: a switch
 * takes a boolean, a list one of its own.
 */
function isOffered(option: v2.SessionConfigOption, value: string | boolean): boolean {
  if (option.type === "boolean") return typeof value === "boolean";

  return listChoices(option).some((row) => row.value === value);
}

/**
 * isBypassMode reports whether a value of one of an agent's settings makes
 * the agent ask PMT nothing, as its row lists them: a mode by its value, and
 * a value of any other setting as "setting=value", such as GitHub Copilot's
 * "allow_all=on".
 */
export function isBypassMode(agent: Agent, option: v2.SessionConfigOption, value: unknown): boolean {
  const choice = option.category === "mode" ? String(value) : `${option.configId}=${String(value)}`;

  return !!agent.bypasses?.includes(choice);
}

/**
 * confirmBypass asks whether an agent may use a mode or a setting with
 * which it asks PMT nothing, and says what PMT cannot do then. A mode goes
 * by its own name, any other setting by the setting's.
 */
async function confirmBypass(agent: Agent, option: v2.SessionConfigOption, value: unknown): Promise<boolean> {
  const mode = listChoices(option).find((row) => row.value === value)?.name ?? String(value);
  const subject = option.category === "mode" ? mode : option.name;
  const answer = await confirm.open({
    title: `Let ${agent.name} use ${subject}?`,
    body:
      `With ${subject}, ${agent.name} changes files and runs commands without asking PMT. ` +
      "PMT cannot keep it off the game's files or your external mods then.",
    confirm: `Use ${subject}`,
    destructive: true,
  });

  return answer === true;
}

/**
 * toBlocks returns a chip as a prompt carries it: a link to its file, with
 * its line or lines if it has them, else its text. A chip with neither adds
 * nothing.
 *
 * An agent reads a linked file from disk. So where the agent takes text in
 * a prompt and the editor holds unsaved text of the file, the chip carries
 * that text itself, the chip's lines of it or the whole.
 */
function toBlocks(chip: Chip, embeds: boolean): v2.ContentBlock[] {
  const { path, text, startLine, endLine } = chip;
  if (!path) return text ? [{ type: "text", text }] : [];
  let lines = "";
  if (startLine) lines = endLine > startLine ? ` (lines ${startLine}–${endLine})` : ` (line ${startLine})`;
  const uri = toUri(path, startLine, endLine);
  const unsaved = embeds ? unsavedModels().find((model) => model.place.path === path)?.model : undefined;
  if (!unsaved) return [{ type: "resource_link", uri, name: chip.label + lines }];
  const range = { startLineNumber: startLine, startColumn: 1, endLineNumber: endLine + 1, endColumn: 1 };

  return [
    { type: "resource", resource: { uri, text: startLine ? unsaved.getValueInRange(range) : unsaved.getValue() } },
  ];
}

/**
 * toFindingPrompt returns the prompt that asks an agent to fix a finding of
 * Health or of the Patch Center: the check's code, what is wrong, then each
 * place the finding is at by its whole path and its line.
 */
export function toFindingPrompt(finding: Finding): string {
  const sites = (finding.sites ?? []).map((site) => `- ${site.place.path}:${site.line}`);

  return [`Fix this finding (${finding.code}): ${joinParts(finding.message)}`, ...sites].join("\n");
}

/**
 * toFixPrompt returns the prompt that asks an agent to fix problems of one
 * file: the file's whole path, since two mods can hold a file at one path
 * under their roots, then each problem's line, message and code.
 */
export function toFixPrompt(place: Place, problems: Diagnostic[]): string {
  const subject = problems.length === 1 ? "this problem" : "these problems";
  const lines = problems.map(
    ({ range, message, code }) => `- line ${range.startLineNumber}: ${message}${code ? ` (${code})` : ""}`,
  );

  return [`Fix ${subject} in ${place.path}:`, ...lines].join("\n");
}

/**
 * flash flashes this window's button in the taskbar while the window is not
 * in front, since a session ended its turn or waits for the user. Linux has
 * no such signal, so nothing shows there.
 */
function flash(): void {
  if (!document.hasFocus()) void FlashWindow(THIS_WINDOW).catch(() => undefined);
}

/** runSignIn runs a sign-in command in PMT's terminal and shows it. */
export async function runSignIn(command: string[]): Promise<void> {
  // Imported here because the terminal store needs the command table.
  await (await import("./terminal")).useTerminalStore().run(command);

  const windows = useWindowsStore();
  windows.sendCommand("terminal.show", windows.resolveTerminal());
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

/**
 * toSessionParams returns what a session's agent is told beside the folder it
 * starts in: the other folders it may work in, if it takes them, and the
 * metadata Go gives its sessions.
 */
function toSessionParams(session: Session, start: Start): { additionalDirectories?: string[]; _meta?: object } {
  return {
    ...(session.takesDirectories && start.folders?.length ? { additionalDirectories: start.folders } : {}),
    ...(start.meta ? { _meta: start.meta } : {}),
  };
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
