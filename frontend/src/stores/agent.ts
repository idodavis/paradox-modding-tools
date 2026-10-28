/**
 * The agent store: this window's chats with coding agents, and what the agent pane, the status bar and PMT's tools see
 * of them. Each session runs over its own stream to the agent's program, and the page speaks the Agent Client Protocol
 * with the agent through the official SDK: version 2 first, and version 1 on a new stream when the agent answers 1.
 */
import * as v1 from "@agentclientprotocol/sdk";
import * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { useQuery } from "@pinia/colada";
import { useEventListener, useLocalStorage } from "@vueuse/core";
import { Browser, Events, JSONStream } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { defineStore } from "pinia";
import { computed, markRaw, onScopeDispose, reactive, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import type { Agent } from "@services/agent/models";
import * as mcp from "@services/mcp/service";
import type { Chip, Show } from "@services/mcp/models";
import type { Place } from "@services/workspace/models";
import * as workspaceApi from "@services/workspace/service";
import { isWorkspacePage, PAGES, type PageName } from "../app/pages";
import { plural } from "../lib/format";
import { findModel } from "../pages/ide/editor/models";
import { fileTab } from "../pages/ide/editor/tabs";
import { accessQuery, agentsQuery, loadQuery, placeQuery } from "../queries";
import ConfirmModal from "../ui/ConfirmModal.vue";
import { useDrawStore } from "./draw";
import { useEditorStore } from "./editor";
import { THIS_WINDOW, useWindowsStore } from "./windows";
import { useWorkspaceStore } from "./workspace";

/** A Protocol is a version of the Agent Client Protocol. */
type Protocol = 1 | 2;

/** A Start is Go's first message on an agent's stream: the session's folder and the other mods', or an error. */
type Start = { folder: string; directories: string[] | null; error: string };

/** A SignIn is one way to sign in to an agent: a command PMT's terminal runs, or a request to the agent itself. */
export type SignIn = { id: string; name: string; description: string; command: string[] };

/**
 * A Change is one file a tool call changes, by its path and the file's own name, with its text before and after, or
 * the patch between them and how many lines the patch adds and removes. Its hunks are what an undo takes back.
 */
export type Change = {
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
      changes: Change[];
      text: string;
      terminals: string[]; // the agent's display-only terminals the call shows, by id
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
  isRenamed: boolean; // the title is the user's, which the agent's titles do not replace
  state: "starting" | "sign-in" | "idle" | "running" | "ended";
  entries: Entry[];
  terminals: Record<string, string>; // what each of the agent's display-only terminals shows, its command first
  options: v2.SessionConfigOption[]; // the settings the agent offers, such as its model, with their values
  commands: v2.AvailableCommand[]; // the commands the agent runs for a prompt that starts with "/"
  methods: SignIn[];
  resumable: boolean;
  deletable: boolean; // whether the agent deletes its own copy of a session
  error: string;
};

/**
 * A SessionRecord is one session as every window's sessions list remembers it, and whether the user archived it or
 * gave it a title of their own.
 */
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

/**
 * A Dialect is one version of the protocol as a session speaks it: the same steps, in that version's words. A
 * prompt resolves when the agent takes it in version 2, and when the turn ends in version 1.
 */
type Dialect = {
  initialize: () => Promise<Agreement>;
  create: (start: Start, agreement: Agreement) => Promise<string>;
  resume: (id: string, start: Start, agreement: Agreement) => Promise<void>;
  prompt: (id: string, blocks: v2.ContentBlock[]) => Promise<void>;
  configure: (id: string, configId: string, value: string | boolean) => Promise<Offers>;
  cancel: (id: string) => Promise<void>;
  delete: (id: string) => Promise<void>;
  signIn: (methodId: string) => Promise<void>;
  close: () => void;
};

/** An Agreement is what an agent answered to initialize, read the same way in both versions. */
type Agreement = { resumable: boolean; directories: boolean; deletable: boolean; methods: SignIn[] };

/** A Method is one way to sign in as either version declares it, its id under one name. */
type Method = {
  id: string;
  name: string;
  description?: string | null;
  type?: string;
  args?: string[];
  _meta?: { "terminal-auth"?: { command: string; args?: string[] } } | null;
};

/** An HttpServer is an MCP server reached over HTTP, as both versions give one to a session. */
type HttpServer = { type: "http"; name: string; url: string; headers: { name: string; value: string }[] };

/** A PermissionAnswer is the user's answer to a permission request, as both versions take it. */
type PermissionAnswer = { outcome: { outcome: "cancelled" } | { outcome: "selected"; optionId: string } };

/** A Link is what a session holds that is never shown: its dialect and the answers its open requests wait for. */
type Link = { dialect: Dialect; answers: Map<string, (answer: string | null) => void> };

/** Offers are the settings and commands an agent offers a session, in either version's words. */
type Offers = {
  configOptions?: (v1.SessionConfigOption | v2.SessionConfigOption)[] | null;
  availableCommands?: v2.AvailableCommand[];
};

/**
 * A MessageUpdate is what one kind of update about a message writes: the kind of the message's entry, and whether
 * it holds the whole message rather than a chunk of it.
 */
type MessageUpdate = { kind: "user" | "agent" | "thought"; isWhole: boolean };

/** The version an agent answered last, by its id and installed version, so its next session starts on it. */
const PROTOCOLS = "pmt.agent.protocol";
/** The error code an agent answers a session with while the user has not signed in. */
const AUTH_REQUIRED = -32000;
/** The client PMT says it is. */
const CLIENT = { name: "pmt", title: "Paradox Modding Tools", version: "1" };

export const useAgentStore = defineStore("agent", () => {
  const route = useRoute();
  const router = useRouter();
  const toast = useToast();
  const confirm = useOverlay().create(ConfirmModal);
  const editor = useEditorStore();
  const windows = useWindowsStore();
  const workspace = useWorkspaceStore();
  const { data: agents } = useQuery(agentsQuery);
  const { data: access } = useQuery(accessQuery);
  const sessions = reactive<Session[]>([]);
  const links = new Map<string, Link>();
  const protocols = useLocalStorage<Record<string, Protocol>>(PROTOCOLS, {});
  const records = useLocalStorage<SessionRecord[]>("pmt.agent.sessions", []);
  // the agent a task in the pane starts a session with
  const lastAgent = useLocalStorage("pmt.agent.last", "");
  // the session the pane and the Agents page show, by its key
  const activeKey = shallowRef("");
  // the pane starts closed, and stays as it is from page to page
  const isPaneOpen = shallowRef(false);
  // follow the agent: open the files its tool calls name
  const follow = useLocalStorage("pmt.agent.follow", true);
  // what each page offers a prompt, by the page's name; the chip is the page shown's, and the user can leave it out
  const chips = reactive(new Map<string, Chip | null>());
  const chip = computed(() => chips.get(String(route.name)) ?? null);
  const chipLeftOut = shallowRef(false);
  let count = 0;

  const active = computed(() => sessions.find((session) => session.key === activeKey.value));
  /** Whether a session waits for the user: a permission to give or a link to open. */
  const isWaiting = (session: Session): boolean =>
    session.entries.some((entry) => entry.kind === "permission" || entry.kind === "link");
  /** The status bar's line: what the active session's agent is doing. */
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
    const fileCount = listChanges(session).length;
    return fileCount ? `${name} is done: ${fileCount} ${fileCount === 1 ? "file" : "files"} edited` : "";
  });

  watch(chip, () => (chipLeftOut.value = false));
  // Go's tools say what the user has in front of them: the page and its chip, sent again when the window has focus
  const sendView = (): void =>
    void mcp.SetView({
      page: String(route.name ?? ""),
      query: Object.fromEntries(Object.entries(route.query).map(([key, value]) => [key, String(value ?? "")])),
      chip: chip.value ?? { label: "", path: "", startLine: 0, endLine: 0, text: "" },
    });
  watch([() => route.fullPath, chip], sendView, { immediate: true });
  useEventListener(window, "focus", sendView);
  if (THIS_WINDOW === "main") {
    void mcp.SetPages(Object.entries(PAGES).map(([id, page]) => ({ id, title: page.title, help: [...page.help] })));
    onScopeDispose(Events.On("mcp:show", ({ data }: { data: Show }) => void show(data)));
  }

  /**
   * start starts a session with an agent: a new one, or for a record, the agent's replay of it. It shows the session
   * here and returns it.
   */
  function start(agentId: string, record?: SessionRecord): Session {
    const agent = agents.value?.find((one) => one.id === agentId);
    if (!agent) throw new Error("PMT does not list that agent");
    const session = makeSession(agent, record);
    sessions.push(session);
    activeKey.value = session.key;
    lastAgent.value = agent.id;
    void connect(session, record ? "resume" : "create");
    return session;
  }

  /**
   * makeSession makes a session with an agent, a new one or a record's, on the version the agent answered last. It
   * is not shown.
   */
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
      error: "",
    });
  }

  /**
   * connect opens a session's stream and has the agent create or resume the session. An agent that does not speak
   * the session's version is tried once more on the other.
   */
  async function connect(session: Session, step: "create" | "resume"): Promise<void> {
    try {
      const { start: folders, dialect } = await openDialect(session);
      let agreement: Agreement;
      try {
        agreement = await dialect.initialize();
      } catch (error) {
        dialect.close();
        if (session.protocol === 1) throw error;
        session.protocol = 1;
        return await connect(session, step);
      }
      protocols.value = { ...protocols.value, [`${session.agent.id}@${session.agent.installed}`]: session.protocol };
      session.resumable = agreement.resumable;
      session.deletable = agreement.deletable;
      session.methods = agreement.methods;
      if (step === "resume") await dialect.resume(session.id, folders, agreement);
      else session.id = await dialect.create(folders, agreement);
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

  /** openDialect opens a stream to a session's agent and wraps it as the SDK's message stream, in its version. */
  async function openDialect(session: Session): Promise<{ start: Start; dialect: Dialect }> {
    const socket = JSONStream("agent");
    let controller!: ReadableStreamDefaultController<v2.AnyWireMessage>;
    // the SDK cancels the stream it reads when its connection closes, and the stream takes nothing after that
    let isCancelled = false;
    const readable = new ReadableStream<v2.AnyWireMessage>({
      start: (each) => (controller = each),
      cancel: () => void (isCancelled = true),
    });
    const writable = new WritableStream<v2.AnyWireMessage>({ write: (message) => socket.send(message) });
    const ready = new Promise<Start>((resolve, reject) => {
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
      // a stream closed to start the agent again is not the session's end
      socket.onclose = () => {
        if (!isCancelled) controller.close();
        if (!first) reject(new Error(`${session.agent.name} stopped before it started`));
        else if (links.get(session.key) === link) session.state = "ended";
      };
    });
    const link: Link = { dialect: undefined as unknown as Dialect, answers: new Map() };
    link.dialect = markRaw(
      session.protocol === 2
        ? makeV2(session, { readable, writable }, link)
        : makeV1(session, { readable, writable } as unknown as v1.Stream, link),
    );
    const close = link.dialect.close;
    link.dialect.close = () => {
      close();
      socket.close();
    };
    links.set(session.key, link);
    return { start: await ready, dialect: link.dialect };
  }

  /** makeV2 speaks version 2 of the protocol over a stream for a session. */
  function makeV2(session: Session, stream: v2.Stream, link: Link): Dialect {
    const connection = v2
      .client(CLIENT)
      .onNotification("session/update", ({ params }) => applyUpdate(session, params.update))
      .onRequest("session/request_permission", ({ params }) =>
        ask(
          session,
          link,
          params.title,
          (params.subject as { toolCall?: v2.ToolCallUpdate } | null)?.toolCall,
          params.options,
        ),
      )
      .onRequest("elicitation/create", ({ params }) => askLink(session, link, params))
      .onNotification("elicitation/complete", ({ params }) => closeEntry(session, link, params.elicitationId, null))
      .connect(stream);
    const agent = connection.agent;
    return {
      initialize: async () => {
        const answer = await agent.request("initialize", {
          protocolVersion: 2,
          info: CLIENT,
          capabilities: { auth: { terminal: {} }, elicitation: { url: {} } },
        });
        const methods = (answer.authMethods ?? []).map((method) =>
          toSignIn(session.agent, { ...method, id: method.methodId }),
        );
        const capabilities = answer.capabilities?.session;
        return {
          resumable: true,
          directories: !!capabilities?.additionalDirectories,
          deletable: !!capabilities?.delete,
          methods,
        };
      },
      create: async (start, agreement) => {
        const answer = await agent.request("session/new", {
          cwd: start.folder,
          mcpServers: listServers(),
          ...folders(start, agreement),
        });
        applyOffers(session, answer);
        return answer.sessionId;
      },
      resume: async (id, start, agreement) => {
        const answer = await agent.request("session/resume", {
          sessionId: id,
          cwd: start.folder,
          mcpServers: listServers(),
          replayFrom: { type: "start" },
          ...folders(start, agreement),
        });
        applyOffers(session, answer);
      },
      prompt: async (id, blocks) => {
        session.state = "running";
        await agent.request("session/prompt", { sessionId: id, prompt: blocks });
      },
      configure: async (id, configId, value) => {
        const choice =
          typeof value === "boolean" ? { type: "boolean" as const, value } : { type: "id" as const, value };
        return agent.request("session/set_config_option", { sessionId: id, configId, ...choice });
      },
      cancel: (id) => agent.notify("session/cancel", { sessionId: id }),
      delete: async (id) => void (await agent.request("session/delete", { sessionId: id })),
      signIn: async (methodId) => void (await agent.request("auth/login", { methodId })),
      close: () => connection.close(),
    };
  }

  /** makeV1 speaks version 1 of the protocol over a stream for a session. */
  function makeV1(session: Session, stream: v1.Stream, link: Link): Dialect {
    const connection = v1
      .client(CLIENT)
      .onNotification("session/update", ({ params }) => applyUpdate(session, params.update as v2.SessionUpdate))
      .onRequest("session/request_permission", ({ params }) =>
        ask(session, link, params.toolCall.title ?? "", params.toolCall as v2.ToolCallUpdate, params.options),
      )
      .onRequest("elicitation/create", ({ params }) => askLink(session, link, params as v2.CreateElicitationRequest))
      .onNotification("elicitation/complete", ({ params }) => closeEntry(session, link, params.elicitationId, null))
      .connect(stream);
    const agent = connection.agent;
    let loads = false;
    return {
      initialize: async () => {
        const request: v1.InitializeRequest = {
          protocolVersion: 1,
          clientInfo: CLIENT,
          clientCapabilities: {
            fs: { readTextFile: false, writeTextFile: false },
            terminal: false,
            auth: { terminal: true },
            elicitation: { url: {} },
          },
        };
        const answer = await agent.request("initialize", request);
        loads = !!answer.agentCapabilities?.loadSession;
        const methods = (answer.authMethods ?? []).map((method) => toSignIn(session.agent, method as Method));
        const capabilities = answer.agentCapabilities?.sessionCapabilities;
        return {
          resumable: loads,
          directories: !!capabilities?.additionalDirectories,
          deletable: !!capabilities?.delete,
          methods,
        };
      },
      create: async (start, agreement) => {
        const request: v1.NewSessionRequest = {
          cwd: start.folder,
          mcpServers: listServers(),
          ...folders(start, agreement),
        };
        const answer = await agent.request("session/new", request);
        applyOffers(session, answer);
        return answer.sessionId;
      },
      resume: async (id, start, agreement) => {
        if (!loads) throw new Error(`${session.agent.name} cannot replay a session`);
        const request: v1.LoadSessionRequest = {
          sessionId: id,
          cwd: start.folder,
          mcpServers: listServers(),
          ...folders(start, agreement),
        };
        applyOffers(session, await agent.request("session/load", request));
      },
      prompt: async (id, blocks) => {
        session.state = "running";
        const answer = await agent.request("session/prompt", { sessionId: id, prompt: blocks as v1.ContentBlock[] });
        finishTurn(session, answer.stopReason);
      },
      configure: async (id, configId, value) => {
        // version 1 sends a value of the list as it is, and a switch's with its type
        const request: v1.SetSessionConfigOptionRequest =
          typeof value === "boolean"
            ? { sessionId: id, configId, type: "boolean", value }
            : { sessionId: id, configId, value };
        return agent.request("session/set_config_option", request);
      },
      cancel: (id) => agent.notify("session/cancel", { sessionId: id }),
      delete: async (id) => {
        const request: v1.DeleteSessionRequest = { sessionId: id };
        await agent.request("session/delete", request);
      },
      signIn: async (methodId) => void (await agent.request("authenticate", { methodId })),
      close: () => connection.close(),
    };
  }

  /** listServers lists the MCP servers a session is given: PMT's own, over HTTP with its token. */
  function listServers(): HttpServer[] {
    if (!access.value) return [];
    const headers = [{ name: "Authorization", value: `Bearer ${access.value.token}` }];
    return [{ type: "http", name: "pmt", url: access.value.url, headers }];
  }

  /** applyUpdate applies one update an agent sent about a session to its transcript, in either version's words. */
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
      const entry = session.entries.find((one) => one.kind === "plan");
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
      // an update with output holds the terminal's whole output so far
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

  /**
   * applyMessage applies an update about a message to its entry, which the first update makes. A whole message
   * replaces the entry's content, and a chunk adds to it. A whole message's texts are paragraphs, and the files a
   * user's message links are its chips. A whole message that leaves its content out changes nothing.
   */
  function applyMessage(session: Session, update: v2.ContentChunk | v2.UserMessage, row: MessageUpdate): void {
    if (row.isWhole && !("content" in update)) return;
    const blocks = row.isWhole ? ((update as v2.UserMessage).content ?? []) : [(update as v2.ContentChunk).content];
    const id = update.messageId ?? "";
    const last = session.entries.at(-1);
    let entry = (session.entries.find((one) => one.kind === row.kind && !!id && one.id === id) ??
      (!id && last?.kind === row.kind ? last : undefined)) as
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

  /**
   * applyTool applies a tool call's update to its entry, which the first update makes. An update's content replaces
   * the entry's, and a content chunk adds to it. It opens the files the call names when the user follows the agent.
   */
  function applyTool(session: Session, update: v2.ToolCallUpdate | v2.ToolCallContentChunk): void {
    let entry = session.entries.find((one) => one.kind === "tool" && one.id === update.toolCallId);
    if (entry?.kind !== "tool") {
      entry = {
        kind: "tool",
        id: update.toolCallId,
        title: "",
        status: "pending",
        type: "other",
        paths: [],
        changes: [],
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
    if (Array.isArray(update.content)) Object.assign(entry, { text: "", changes: [], terminals: [] });
    const contents = Array.isArray(update.content) ? update.content : update.content ? [update.content] : [];
    for (const content of contents) {
      if (content.type === "content") entry.text += toText((content as { content: v2.ContentBlock }).content);
      else if (content.type === "diff") entry.changes.push(...toChanges(content as unknown as DiffContent));
      else if (content.type === "terminal") entry.terminals.push((content as { terminalId: string }).terminalId);
    }
    const locations = whole?.locations ?? [];
    const paths = locations.map((location) => location.path);
    entry.paths = [...new Set([...entry.paths, ...paths, ...entry.changes.map((change) => change.path)])];
    if (follow.value && locations[0])
      void followFile(locations[0].path, locations[0].line ?? 1, entry.type === "edit" ? "edit" : "agent");
  }

  /** finishTurn marks a session's turn over, and says why when the agent did not end it itself. */
  function finishTurn(session: Session, reason: string): void {
    session.state = "idle";
    if (reason !== "end_turn")
      session.entries.push({ kind: "notice", id: `n${session.entries.length}`, text: STOPS[reason] ?? reason });
    remember(session);
  }

  /** ask shows a permission request in a session and answers the agent with the option the user picks. */
  function ask(
    session: Session,
    link: Link,
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
      link.answers.set(id, (optionId) =>
        resolve({ outcome: optionId ? { outcome: "selected", optionId } : { outcome: "cancelled" } }),
      ),
    );
  }

  /**
   * askLink shows a link an agent asks to open in a session, and answers with what the user chooses. Only the URL
   * mode is offered; a form is declined.
   */
  function askLink(
    session: Session,
    link: Link,
    request: v2.CreateElicitationRequest,
  ): Promise<v2.CreateElicitationResponse> {
    if (request.mode !== "url" || !("url" in request)) return Promise.resolve({ action: "decline" });
    const id = String(request.elicitationId);
    session.entries.push({ kind: "link", id, message: request.message, url: String(request.url) });
    return new Promise((resolve) =>
      link.answers.set(id, (action) => {
        if (action === "accept") void Browser.OpenURL(String(request.url));
        resolve({
          action: action === "accept" ? "accept" : action === "decline" ? "decline" : "cancel",
        } as v2.CreateElicitationResponse);
      }),
    );
  }

  /** closeEntry answers a session's open request and takes it out of the transcript. An unknown id is ignored. */
  function closeEntry(session: Session, link: Link, id: string, answer: string | null): void {
    link.answers.get(id)?.(answer);
    link.answers.delete(id);
    session.entries = session.entries.filter(
      (entry) => !((entry.kind === "permission" || entry.kind === "link") && entry.id === id),
    );
  }

  /** answer answers a session's open request with an option, an action, or null to cancel it. */
  const answer = (session: Session, id: string, choice: string | null): void => {
    const link = links.get(session.key);
    if (link) closeEntry(session, link, id, choice);
  };

  /**
   * prompt sends the user's text to a session, with the files picked and the page's chip unless it was left out. A
   * file goes as a link to it, its lines in its name and in its address, since some agents keep only the address.
   */
  const prompt = async (session: Session, text: string, files: Place[]): Promise<void> => {
    const link = links.get(session.key);
    if (!link || session.state !== "idle") return;
    const pageChip = chipLeftOut.value ? null : chip.value;
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
    // a version 2 agent sends the prompt back, and that is the transcript's copy; a version 1 agent does not
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
   * configure sets one of a session's settings to a value the agent offers, and shows the settings the agent answers
   * with. A setting the agent refuses is said in the transcript.
   */
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

  /** close ends a session here: its agent stops, and the session stays in the list. */
  const close = (session: Session): void => {
    links.get(session.key)?.dialect.close();
    links.delete(session.key);
    sessions.splice(sessions.indexOf(session), 1);
    if (activeKey.value === session.key) activeKey.value = sessions.at(-1)?.key ?? "";
  };

  /**
   * signIn signs in to a session's agent: a command in PMT's terminal, which the user finishes there, or the agent's
   * own sign-in, after which the session starts again.
   */
  const signIn = async (session: Session, method: SignIn): Promise<void> => {
    if (method.command.length) {
      // loaded here, as the windows store loads it, since the terminal store needs the command table
      await (await import("./terminal")).useTerminalStore().run(method.command);
      windows.sendCommand("terminal.show", windows.resolveTerminal());
      return;
    }
    await links.get(session.key)?.dialect.signIn(method.id);
    links.get(session.key)?.dialect.close();
    session.state = "starting";
    void connect(session, session.id ? "resume" : "create");
  };

  /** retry starts a session's agent again after a sign-in in the terminal or an error. */
  const retry = (session: Session): void => {
    links.get(session.key)?.dialect.close();
    Object.assign(session, { state: "starting", error: "" });
    void connect(session, session.id ? "resume" : "create");
  };

  /** remember writes a session to every window's sessions list, the newest first. An archived session stays so. */
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

  /**
   * rename gives the session of the given id the user's own title, in the sessions list and here, which the agent's
   * later titles do not replace. A blank title changes nothing.
   */
  const rename = (id: string, title: string): void => {
    const text = title.trim();
    if (!text) return;
    records.value = records.value.map((other) =>
      other.id === id ? { ...other, title: text, isRenamed: true } : other,
    );
    const live = sessions.find((session) => session.id === id);
    if (live) Object.assign(live, { title: text, isRenamed: true });
  };

  /**
   * open shows a remembered session here: the one this window runs, else the agent's replay of it. A session whose
   * agent cannot replay it starts anew.
   */
  const open = (record: SessionRecord): void => {
    const live = sessions.find((session) => session.id === record.id);
    if (live) activeKey.value = live.key;
    else start(record.agent, record.resumable ? record : undefined);
  };

  /** archive moves a session into its agent's archived sessions, or back out of them. The agent is not told. */
  const archive = (record: SessionRecord, isArchived: boolean): void => {
    records.value = records.value.map((other) => (other.id === record.id ? { ...other, isArchived } : other));
  };

  /**
   * deleteSession asks before a session leaves every window's sessions list, then takes it out and closes it here if
   * it runs here. An agent that can delete its own copy deletes it too; another keeps it, as the question says.
   */
  const deleteSession = async (record: SessionRecord): Promise<void> => {
    const row = agents.value?.find((one) => one.id === record.agent);
    const name = row?.name ?? "The agent";
    const isDeletable = !!row && record.deletable;
    const confirmed = await confirm.open({
      title: "Delete this session?",
      body: isDeletable
        ? `“${record.title}” leaves PMT, and ${name} deletes its own copy. This cannot be undone.`
        : `“${record.title}” leaves PMT. ${name} keeps its own copy, since it has no way to delete one.`,
      confirm: "Delete session",
      destructive: true,
    });
    if (confirmed !== true) return;
    const live = sessions.find((session) => session.id === record.id);
    if (live) close(live);
    records.value = records.value.filter((other) => other.id !== record.id);
    if (row && isDeletable) {
      await deleteCopy(row, record).catch((error: unknown) =>
        toast.add({ title: `${name} could not delete its copy`, description: describeError(error), color: "error" }),
      );
    }
  };

  /** deleteCopy has an agent delete its own copy of a session, over a stream opened for that alone. */
  async function deleteCopy(agent: Agent, record: SessionRecord): Promise<void> {
    const session = makeSession(agent, record);
    try {
      const { dialect } = await openDialect(session);
      await dialect.initialize();
      await dialect.delete(record.id);
    } finally {
      links.get(session.key)?.dialect.close();
      links.delete(session.key);
    }
  }

  /**
   * undo gives each file the given changes made back its text from before them, the latest change first, and marks
   * them undone. A file it cannot give back is left as it is, and a toast says why.
   */
  const undo = async (changes: Change[]): Promise<void> => {
    const files = groupBy(
      changes.filter((change) => !change.isUndone),
      (change) => change.path,
    );
    for (const fileChanges of Object.values(files)) {
      try {
        await undoFile(fileChanges);
        fileChanges.forEach((change) => (change.isUndone = true));
      } catch (error) {
        const title = `Could not undo the edits to ${fileChanges[0]?.label ?? ""}`;
        toast.add({ title, description: describeError(error), color: "error" });
      }
    }
  };

  /** isUndoable reports whether a session made a change that is not undone yet. */
  const isUndoable = (session: Session): boolean => listEdits(session).some((change) => !change.isUndone);

  /** undoAll asks first, then undoes every change of a session's that is not undone yet. */
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

  /**
   * popOut opens the agent pane in a window of its own. The session shown here, if any, ends here, and that window
   * resumes it from the sessions list if its agent can replay it.
   */
  const popOut = (shown: Session | undefined): void => {
    if (shown) close(shown);
    windows.popOut({ agent: true });
  };

  /** setChip keeps what a page offers a prompt, or null for nothing. */
  const setChip = (page: string, next: Chip | null): void => {
    chips.set(page, next);
  };

  /**
   * followFile opens a file an agent's tool call names at a line, in the main window's IDE, as a file the agent reads
   * or one it edits. The file is the agent's, so the chip stays the user's own and the focus stays where it is.
   */
  async function followFile(path: string, line: number, source: "agent" | "edit"): Promise<void> {
    const place = await loadQuery(placeQuery(path)).catch(() => undefined);
    if (!place?.root) return;
    const position = { lineNumber: line, column: 1 };
    if (THIS_WINDOW === "main") editor.show(place, position, source);
    else windows.showElsewhere("main", place, position, source);
  }

  /** show brings to the front what an agent asked to show: a page with its query, a file, a window or a look-up. */
  async function show(request: Show): Promise<void> {
    if (request.page && request.page in PAGES) {
      const name = request.page as PageName;
      const params = isWorkspacePage(name) ? { id: workspace.activeId() } : {};
      await router.push({ name, params, query: request.query ?? {} } as Parameters<typeof router.push>[0]);
    }
    if (request.path) {
      const place = await loadQuery(placeQuery(request.path));
      if (!place) return;
      // the editor ends a selection at its line's end
      const start = request.startLine ?? 1;
      const end = { endLineNumber: request.endLine ?? start, endColumn: Number.MAX_SAFE_INTEGER };
      editor.show(place, { startLineNumber: start, startColumn: 1, ...end });
      if (request.drawable) {
        editor.setView(fileTab(place.key), "visual");
        useDrawStore().draw(place.path, request.drawable);
        if (request.picks) useDrawStore().pick(request.picks);
      }
    }
    if (request.lookUp) {
      const { title = "", class: kind = "", name = "" } = request.lookUp;
      windows.lookUp(title ? { title } : { class: kind, name });
    }
  }

  return {
    sessions,
    records,
    activeKey,
    active,
    lastAgent,
    isPaneOpen,
    follow,
    chip,
    chipLeftOut,
    setChip,
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
    listChanges,
  };
});

/**
 * useChip keeps what the calling page offers a prompt, which the prompt takes while the page is shown. The source
 * gives the chip, or null for none.
 */
export function useChip(source: () => Chip | null): void {
  const agent = useAgentStore();
  // a kept-alive page is made on its own route, whose name is the page's
  const page = String(useRoute().name);
  watch(source, (next) => agent.setChip(page, next), { immediate: true });
}

/** makeChip returns a chip that carries a text, such as an event's id, under a label. */
export function makeChip(label: string, text: string): Chip {
  return { label, path: "", startLine: 0, endLine: 0, text };
}

/** TITLE_WORDS is how many words of an agent's title a list shows. */
const TITLE_WORDS = 4;

/**
 * describeTitle returns a session's title as a list shows it: a title the user gave whole, else the first line of the
 * agent's cut to its first words, with "…" where it was cut.
 */
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

/** MESSAGES holds what each kind of update about a message writes, in either version's words. */
const MESSAGES: Partial<Record<string, MessageUpdate>> = {
  user_message_chunk: { kind: "user", isWhole: false },
  user_message: { kind: "user", isWhole: true },
  agent_message_chunk: { kind: "agent", isWhole: false },
  agent_message: { kind: "agent", isWhole: true },
  agent_thought_chunk: { kind: "thought", isWhole: false },
  agent_thought: { kind: "thought", isWhole: true },
};

/**
 * applyOffers keeps the settings and commands an agent offers a session, from its answer to a new or resumed session
 * or to a setting, or from an update. A list the agent leaves out stays as it is, since an update may have brought it
 * first.
 */
function applyOffers(session: Session, offers: Offers): void {
  // version 1 names a setting's id "id"
  const options = offers.configOptions?.map((option) =>
    "configId" in option ? option : ({ ...option, configId: option.id } as unknown as v2.SessionConfigOption),
  );
  if (options) session.options = options;
  if (offers.availableCommands) session.commands = offers.availableCommands;
}

/** A DiffContent is a tool call's diff in either version: version 1's old and new text, or version 2's patch. */
type DiffContent = {
  path?: string;
  oldText?: string | null;
  newText?: string;
  changes?: { path: string; operation?: string }[];
  patch?: { text: string } | null;
};

/** listEdits lists every change a session's tool calls made, the earliest first. */
function listEdits(session: Session): Change[] {
  return session.entries.flatMap((entry) => (entry.kind === "tool" ? entry.changes : []));
}

/** listChanges lists the files a session's tool calls changed, each once, by its latest change. */
function listChanges(session: Session): Change[] {
  return [...new Map(listEdits(session).map((change) => [change.path, change])).values()];
}

/**
 * undoFile gives one file back its text from before the given changes, all of the file's, the latest first, and
 * writes it through Go in its own line endings. A file the earliest change made that ends empty goes to PMT's trash.
 * It throws when the file has unsaved text in the editor, when a change came with no lines, or when a change's lines
 * are not in the file once.
 */
async function undoFile(changes: Change[]): Promise<void> {
  const path = changes[0]?.path ?? "";
  const place = await loadQuery(placeQuery(path));
  if (place && findModel(place.key)?.unsaved) {
    throw new Error("It has unsaved text in the editor: save or revert it first.");
  }
  const file = await workspaceApi.Read(path);
  let lines = splitLines(file.text.text);
  for (const change of [...changes].reverse()) {
    if (!change.hunks.length) throw new Error("The agent sent no lines for one of its edits.");
    for (const hunk of [...change.hunks].reverse()) lines = revertHunk(lines, hunk);
  }
  if (changes[0]?.isNew && !lines.join("")) {
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
 * neither are.
 */
function revertHunk(lines: string[], hunk: Hunk): string[] {
  const starts = listStarts(lines, hunk.after);
  const [start] = starts;
  if (starts.length === 1 && start !== undefined) {
    return [...lines.slice(0, start), ...hunk.before, ...lines.slice(start + hunk.after.length)];
  }
  if (!starts.length && hunk.before.length && listStarts(lines, hunk.before).length === 1) return lines;
  throw new Error(starts.length ? "Its edited lines are in it more than once." : "It changed since the agent's edit.");
}

/** listStarts lists where a run of lines starts in a file's lines, each place it is. */
function listStarts(lines: string[], run: string[]): number[] {
  return lines.map((_, start) => start).filter((start) => run.every((line, offset) => lines[start + offset] === line));
}

/**
 * toChanges reads the files a diff of either version changes. A file's own name is its path's last part. A version 1
 * diff is one hunk, its old and new text; a version 2 patch's hunks, and the lines they add and remove, are read
 * from the file's part of it.
 */
function toChanges(diff: DiffContent): Change[] {
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

/**
 * toSignIn reads one way to sign in to an agent: a command in PMT's terminal, the agent's own program with the
 * method's arguments or the command the method names, or else a request to the agent itself.
 */
function toSignIn(agent: Agent, method: Method): SignIn {
  const terminalAuth = method._meta?.["terminal-auth"];
  let command: string[] = [];
  if (terminalAuth) command = [terminalAuth.command, ...(terminalAuth.args ?? [])];
  else if (method.type === "terminal" && agent.program)
    command = [agent.program, ...(agent.args ?? []), ...(method.args ?? [])];
  return { id: method.id, name: method.name, description: method.description ?? "", command };
}

/** folders returns the other mods' folders for a new session, where the agent takes them. */
function folders(start: Start, agreement: Agreement): { additionalDirectories?: string[] } {
  return agreement.directories && start.directories?.length ? { additionalDirectories: start.directories } : {};
}

/** toText returns the text of a content block, or a placeholder for one that is not text. */
function toText(block: v2.ContentBlock | undefined): string {
  if (!block) return "";
  if (block.type === "text") return (block as v2.TextContent).text;
  if (block.type === "resource_link") return (block as v2.ResourceLink).name;
  return "";
}

/**
 * toUri returns a file's address as an agent reads it, percent-encoded. When the first and last selected lines are
 * given, they follow it as "#L3-10", or "#L3" for one line.
 */
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

/** describeError returns an error's message. */
function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
