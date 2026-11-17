/**
 * The completion store: this window's one connection to the agent that
 * completes code in the editor, and the state Settings shows of it.
 */
import * as nes from "@agentclientprotocol/sdk/experimental/nes";
import type * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { useQuery } from "@pinia/colada";
import { defineStore } from "pinia";
import { computed, shallowRef, watch } from "vue";

import type { Agent } from "@services/agent/models";
import { describeError } from "../lib/format";
import { openStream } from "../lib/stream";
import { agentsQuery } from "../queries";
import {
  answerLink,
  CLIENT,
  isAuthRequired,
  runSignIn,
  toLink,
  toSignIn,
  toUri,
  V1_INITIALIZE,
  type LinkRequest,
  type SignIn,
} from "./agent";
import { useSettingsStore } from "./settings";

/**
 * A State is where the connection stands: not wanted, starting, ready,
 * waiting for the user to sign in, or failed.
 */
export type State = "off" | "starting" | "ready" | "sign-in" | "error";

/**
 * A Connection is a ready connection: the agent to ask, its session for
 * suggestions and what the agent asks to be told of.
 */
export type Connection = {
  agent: nes.ClientContext;
  sessionId: string;
  capabilities: nes.NesCapabilities;
};

/** SETTING is the setting that turns tab completion on. */
const SETTING = "agents.completion";

/** CAPABILITIES is what the editor shows of suggestions: edits and jumps. */
const CAPABILITIES: nes.ClientNesCapabilities = { jump: {} };

export const useCompletionStore = defineStore("completion", () => {
  const settings = useSettingsStore();
  const { data: agents } = useQuery(agentsQuery);
  const state = shallowRef<State>("off");
  const error = shallowRef("");
  const methods = shallowRef<SignIn[]>([]);
  // The link the agent asks the user to open while they sign in.
  const link = shallowRef<LinkRequest | null>(null);
  const connection = shallowRef<Connection | null>(null);
  const firstPath = shallowRef("");
  // A sign-in runs on the agent before there is a connection to suggest from.
  let channel: { close: () => void } | null = null;
  let server: nes.ClientContext | null = null;
  let settle: ((action: string | null) => void) | null = null;

  const completer = computed(() => {
    const row = agents.value?.find((other) => other.completion && other.installed);

    return row && firstPath.value && settings.isOn(SETTING) ? row : undefined;
  });

  // The words that ask for a sign-in, while the state is "sign-in".
  const signInNotice = computed(() =>
    state.value === "sign-in" && completer.value
      ? `${completer.value.name} needs you to sign in for tab completion`
      : "",
  );

  // The connection runs the installed version, so an update starts the new one.
  watch(() => completer.value && `${completer.value.id}@${completer.value.installed}`, restart, {
    immediate: true,
  });

  /** openFile tells the store a file is open. The first one starts it. */
  function openFile(path: string): void {
    if (!firstPath.value) firstPath.value = path;
  }

  /**
   * stop ends the connection. It starts again when the agent, the setting or
   * the open file changes, or with restart.
   */
  function stop(): void {
    const previous = channel;
    channel = null;
    server = null;
    connection.value = null;
    methods.value = [];
    error.value = "";
    state.value = "off";
    answer(null);
    previous?.close();
  }

  /** restart ends the connection and starts it again if it is wanted. */
  function restart(): void {
    stop();
    if (completer.value) void begin(completer.value, firstPath.value);
  }

  /**
   * begin starts the agent and its suggestions. If the agent needs the user to
   * sign in, the state is "sign-in" until restart.
   */
  async function begin(agent: Agent, path: string): Promise<void> {
    state.value = "starting";
    const agentStream = openStream(agent, path, () => {
      if (channel === agentStream) fail(`${agent.name} stopped`);
    });
    channel = agentStream;

    try {
      const { folder } = await agentStream.started;
      server = connect(agentStream.stream);

      const request: nes.InitializeRequest = {
        ...V1_INITIALIZE,
        clientCapabilities: {
          ...V1_INITIALIZE.clientCapabilities,
          nes: CAPABILITIES,
          positionEncodings: ["utf-16"],
        },
      };
      const { agentCapabilities, authMethods } = (await server.request(
        "initialize",
        request,
      )) as nes.InitializeResponse;
      methods.value = (authMethods ?? []).map((method) => toSignIn(agent, method));
      if (!agentCapabilities?.nes) throw new Error(`${agent.name} does not complete code`);

      const { sessionId } = await server.request("nes/start", { workspaceUri: toUri(folder) });
      connection.value = { agent: server, sessionId, capabilities: agentCapabilities.nes };
      state.value = "ready";
    } catch (cause) {
      if (channel !== agentStream) return;

      if (isAuthRequired(cause)) state.value = "sign-in";
      else fail(describeError(cause));
    }
  }

  /**
   * requireSignIn turns the state to "sign-in", at the start or later. The
   * connection is cleared so no more suggestions are asked, and the agent is
   * kept so a sign-in can run on it. It does nothing if the given connection is
   * no longer the current one, as a restart has replaced it.
   */
  function requireSignIn(refusing: Connection | null): void {
    if (connection.value !== refusing) return;

    connection.value = null;
    state.value = "sign-in";
  }

  /**
   * connect returns the agent side of a new SDK client over a stream. The
   * client shows the links the agent asks to open.
   */
  function connect(stream: v2.Stream): nes.ClientContext {
    const client = nes
      .client(CLIENT)
      .onRequest("elicitation/create", ({ params }) => {
        const ask = toLink(params as v2.CreateElicitationRequest);
        if (!ask) return { action: "decline" };

        link.value = ask;

        return new Promise((resolve) => {
          settle = (action) => resolve(answerLink(ask.url, action));
        });
      })
      .onNotification("elicitation/complete", ({ params }) => {
        if (link.value?.id === params.elicitationId) answer(null);
      });

    return client.connect(stream).agent;
  }

  /** fail shows why the connection stopped. */
  function fail(message: string): void {
    state.value = "error";
    error.value = message;
    connection.value = null;
  }

  /**
   * answer answers the link the agent asks to open, with accept or decline,
   * or cancels it for null. Either way the link is hidden.
   */
  function answer(action: string | null): void {
    settle?.(action);
    settle = null;
    link.value = null;
  }

  /**
   * signIn signs in through PMT's terminal or through the agent. A sign-in
   * through the agent starts the connection again when it is done.
   */
  async function signIn(method: SignIn): Promise<void> {
    if (method.command.length) return runSignIn(method.command);

    try {
      await server?.request("authenticate", { methodId: method.id });
      restart();
    } catch (cause) {
      fail(describeError(cause));
    }
  }

  return {
    state,
    error,
    methods,
    link,
    connection,
    signInNotice,
    openFile,
    stop,
    restart,
    requireSignIn,
    signIn,
    answer,
  };
});
