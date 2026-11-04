/**
 * The agent store: this window's chats with coding agents, and what the agent pane and the status bar show of them.
 * Each session speaks the Agent Client Protocol over its own stream, version 2 first and version 1 if the agent asks.
 */
import * as v1 from "@agentclientprotocol/sdk";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { useQuery } from "@pinia/colada";
import { until, useLocalStorage } from "@vueuse/core";
import { Browser, JSONStream } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { defineStore } from "pinia";
import { computed, markRaw, reactive, shallowRef } from "vue";

import type { Agent } from "@services/agent/models";
import type { Place } from "@services/workspace/models";
import { confirm } from "../lib/feedback";
import { describeError, plural } from "../lib/format";
import { accessQuery, agentsQuery } from "../queries";
import { useEditorStore } from "./editor";
import { listEdits, listLatestEdits, toEdits, undoFile, type DiffContent, type FileEdit } from "./edits";
import { useToolViewStore } from "./toolView";
import { useWindowsStore, WINDOW_QUERY } from "./windows";

/** A Protocol is a version of the Agent Client Protocol. */
type Protocol = 1 | 2;

/** A Start is Go's first message on an agent's stream: the session's folder and the other mods', or an error. */
type Start = { folder: string; directories: string[] | null; error: string };

/** A SignIn is one way to sign in to an agent: a command for PMT's terminal, or a request to the agent. */
type SignIn = { id: string; name: string; description: string; command: string[] };

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
  | { kind: "permission"; id: string; title: string; options: { id: string; name: string; kind: string }[] }
  | { kind: "link"; id: string; message: string; url: string }
  | { kind: "notice"; id: string; text: string };

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
  resumable: boolean;
  deletable: boolean;
  directories: boolean;
  error: string;
};

/** A SessionRecord is one session as every window's sessions list remembers it. */
export type SessionRecord = {
  agent: string;
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

/** A MessageUpdate is what one kind of update about a message writes: an entry's kind, and a whole text or a chunk. */
type MessageUpdate = { kind: "user" | "agent" | "thought"; isWhole: boolean };

/** PROTOCOLS is the local storage key of the version each agent answered last. */
const PROTOCOLS = "pmt.agent.protocol";
/** AUTH_REQUIRED is the error code an agent answers with while the user has not signed in. */
const AUTH_REQUIRED = -32000;
/** CLIENT is how PMT names itself to an agent. */
const CLIENT = { name: "pmt", title: "Paradox Modding Tools", version: "1" };

export const useAgentStore = defineStore("agent", () => {
  const toast = useToast();
  const editor = useEditorStore();
  const windows = useWindowsStore();
  const toolView = useToolViewStore();
  const { data: agents } = useQuery(agentsQuery);
  const { data: access } = useQuery(accessQuery);
  const sessions = reactive<Session[]>([]);
  const links = new Map<string, Link>();
  const protocols = useLocalStorage<Record<string, Protocol>>(PROTOCOLS, {});
  const records = useLocalStorage<SessionRecord[]>("pmt.agent.sessions", []);
  const lastAgent = useLocalStorage("pmt.agent.last", "");
  const activeKey = shallowRef("");
  const isPaneOpen = shallowRef(false);
  // Follow the agent: whether to open the files the agent's tool calls name.
  const follow = useLocalStorage("pmt.agent.follow", true);
  let count = 0;

  const isPromptWanted = shallowRef(false);

  const active = computed(() => sessions.find((session) => session.key === activeKey.value));
  const ready = computed(() => (agents.value ?? []).filter((row) => row.program));
  const current = computed(
    () => active.value?.agent ?? ready.value.find((row) => row.id === lastAgent.value) ?? ready.value[0],
  );
  /** isWaiting reports whether a session waits for the user: a permission to give or a link to open. */
  const isWaiting = (session: Session): boolean =>
    session.entries.some((entry) => entry.kind === "permission" || entry.kind === "link");
  const status = computed(() => {
    const session = active.value;
    if (!session) return "";
    const name = session.agent.name;
    const asking = session.entries.find((entry) => entry.kind === "permission");
    if (asking?.kind === "permission") return `${name} asks: ${asking.title}`;
    if (session.entries.some((entry) => entry.kind === "link")) return `${name} asks to open a link`;
    if (session.state === "running") return `${name} is working: ${describeTitle(session)}`;
    if (session.state === "sign-in") return `${name} needs you to sign in`;
    if (session.state === "starting") return `${name} is starting`;
    const fileCount = listLatestEdits(session).length;
    return fileCount ? `${name} is done: ${plural(fileCount, "file")} edited` : "";
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
    const socket = JSONStream("agent");
    let controller!: ReadableStreamDefaultController<v2.AnyWireMessage>;
    // The SDK cancels the stream it reads when its connection closes, and the stream takes nothing after that.
    let isCancelled = false;
    const readable = new ReadableStream<v2.AnyWireMessage>({
      start: (streamController) => (controller = streamController),
      cancel: () => void (isCancelled = true),
    });
    const writable = new WritableStream<v2.AnyWireMessage>({ write: (message) => socket.send(message) });
    const started = new Promise<Start>((resolve, reject) => {
      let first: Start | null = null;
      socket.onopen = () => socket.send({ agent: session.agent.id, path: editor.activeFile()?.path ?? "" });
      socket.onmessage = (event) => {
        if (first) {
          if (!isCancelled) controller.enqueue(event.data as v2.AnyWireMessage);
          return;
        }
        first = event.data as Start;
        if (first.error) reject(new Error(first.error));
        else resolve(first);
      };
      // A stream closed to start the agent again is not the session's end.
      socket.onclose = () => {
        if (!isCancelled) controller.close();
        if (!first) reject(new Error(`${session.agent.name} stopped before it started`));
        else if (links.get(session.key) === link) session.state = "ended";
      };
    });
    const answers: Answers = new Map();
    const dialect = markRaw(makeDialect(session, { readable, writable }, socket, answers));
    const link: Link = { dialect, answers };
    links.set(session.key, link);
    return { start: await started, dialect };
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
      initialize: {
        protocolVersion: 1,
        clientInfo: CLIENT,
        clientCapabilities: {
          fs: { readTextFile: false, writeTextFile: false },
          terminal: false,
          auth: { terminal: true },
          elicitation: { url: {} },
        },
      } satisfies v1.InitializeRequest,
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
      entry = {
        kind: "tool",
        id: update.toolCallId,
        title: "",
        status: "pending",
        type: "other",
        paths: [],
        edits: [],
        text: "",
        terminals: [],
      };
      session.entries.push(entry);
    }
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
    if (follow.value && locations[0])
      void toolView.followFile(locations[0].path, locations[0].line ?? 1, entry.type === "edit" ? "edit" : "agent");
  }

  /** finishTurn marks a session's turn over, with a notice if the turn did not end normally. */
  function finishTurn(session: Session, reason: string): void {
    session.state = "idle";
    if (reason !== "end_turn")
      session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text: STOPS[reason] ?? reason });
    remember(session);
  }

  /** ask shows a permission request in a session and answers the agent with the option the user picks. */
  function ask(
    session: Session,
    answers: Answers,
    title: string,
    toolCall: v2.ToolCallUpdate | undefined,
    options: v2.PermissionOption[],
  ): Promise<PermissionAnswer> {
    if (toolCall) applyTool(session, toolCall);
    const id = `p${session.entries.length}`;
    session.entries.push({
      kind: "permission",
      id,
      title: title || toolCall?.title || "Allow this?",
      options: options.map((option) => ({ id: option.optionId, name: option.name, kind: option.kind })),
    });
    return new Promise((resolve) =>
      answers.set(id, (optionId) =>
        resolve({ outcome: optionId ? { outcome: "selected", optionId } : { outcome: "cancelled" } }),
      ),
    );
  }

  /** askLink shows a link an agent asks to open and answers with the user's choice. It declines a form. */
  function askLink(
    session: Session,
    answers: Answers,
    request: v2.CreateElicitationRequest,
  ): Promise<v2.CreateElicitationResponse> {
    if (request.mode !== "url" || !("url" in request)) return Promise.resolve({ action: "decline" });
    const id = String(request.elicitationId);
    session.entries.push({ kind: "link", id, message: request.message, url: String(request.url) });
    return new Promise((resolve) =>
      answers.set(id, (action) => {
        if (action === "accept") void Browser.OpenURL(String(request.url));
        const choice = action === "accept" || action === "decline" ? action : "cancel";
        resolve({ action: choice } as v2.CreateElicitationResponse);
      }),
    );
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
    if (link) closeEntry(session, link.answers, id, choice);
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

  /** configure sets one of a session's settings. If the agent refuses, the error goes in the transcript. */
  const configure = async (session: Session, configId: string, value: string | boolean): Promise<void> => {
    const link = links.get(session.key);
    if (!link) return;
    try {
      applyOffers(session, await link.dialect.configure(session.id, configId, value));
    } catch (error) {
      session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text: describeError(error) });
    }
  };

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
    if (method.command.length) {
      // Imported here because the terminal store needs the command table.
      await (await import("./terminal")).useTerminalStore().run(method.command);
      windows.sendCommand("terminal.show", windows.resolveTerminal());
      return;
    }
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

  /** remember writes a session to every window's sessions list, newest first. */
  function remember(session: Session): void {
    if (!session.id) return;
    const record = {
      agent: session.agent.id,
      id: session.id,
      title: session.title,
      updatedAt: Date.now(),
      resumable: session.resumable,
      deletable: session.deletable,
      isArchived: records.value.find((other) => other.id === session.id)?.isArchived ?? false,
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

  /** undo restores each file the given edits changed and marks the edits undone. A toast says why one cannot. */
  const undo = async (edits: FileEdit[]): Promise<void> => {
    const files = groupBy(
      edits.filter((change) => !change.isUndone),
      (change) => change.path,
    );
    for (const fileEdits of Object.values(files)) {
      try {
        await undoFile(fileEdits);
        fileEdits.forEach((change) => (change.isUndone = true));
      } catch (error) {
        const title = `Could not undo the edits to ${fileEdits[0]?.label ?? ""}`;
        toast.add({ title, description: describeError(error), color: "error" });
      }
    }
  };

  /** isUndoable reports whether a session made a change that is not undone yet. */
  const isUndoable = (session: Session): boolean => listEdits(session).some((change) => !change.isUndone);

  /** undoAll asks first, then undoes every change of a session's that is not undone. */
  const undoAll = async (session: Session): Promise<void> => {
    const changes = listEdits(session).filter((change) => !change.isUndone);
    const fileCount = new Set(changes.map((change) => change.path)).size;
    if (!fileCount) return;
    const confirmed = await confirm.open({
      title: "Undo all edits?",
      body: `${session.agent.name}'s edits to ${plural(fileCount, "file")} in this session are taken back.`,
      confirm: "Undo all edits",
      destructive: true,
    });
    if (confirmed === true) await undo(changes);
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
    activeKey,
    active,
    lastAgent,
    isPaneOpen,
    isPromptWanted,
    ready,
    current,
    follow,
    status,
    isWaiting,
    start,
    open,
    archive,
    rename,
    deleteSession,
    undo,
    isUndoable,
    undoAll,
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

/** toSignIn returns how to sign in with a method. Its command is empty if PMT asks the agent itself. */
function toSignIn(agent: Agent, method: Method): SignIn {
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
function toUri(path: string, startLine = 0, endLine = 0): string {
  const url = new URL("file://");
  url.pathname = path.replace(/\\/g, "/");
  if (startLine) url.hash = startLine === endLine ? `L${startLine}` : `L${startLine}-${endLine}`;
  return url.href;
}

/** isAuthRequired reports whether an error is an agent's answer that the user must sign in first. */
function isAuthRequired(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === AUTH_REQUIRED;
}
