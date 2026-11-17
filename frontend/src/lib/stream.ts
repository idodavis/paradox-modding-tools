/**
 * Opens the stream to an agent's program and gives its messages to the Agent
 * Client Protocol's SDK as its streams.
 */
import type * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { JSONStream } from "@wailsio/runtime";

import type { Agent } from "@services/agent/models";

/**
 * A Start is Go's first message on an agent's stream: the folder the program
 * started in and the other mods' folders, or the error that kept it from
 * starting.
 */
export type Start = { folder: string; directories: string[] | null; error: string };

/** An AgentStream is an agent's stream as the SDK reads it. */
export type AgentStream = {
  stream: v2.Stream;
  // started resolves with Go's Start. It rejects if Go reports an error or the
  // stream ends before it.
  started: Promise<Start>;
  close: () => void;
};

/**
 * openStream opens a stream to an agent's program and sends the opening: the
 * agent, and the file whose mod's folder the program starts in. onEnd is
 * called once if the stream ends after Go's Start.
 */
export function openStream(agent: Agent, path: string, onEnd: () => void): AgentStream {
  const socket = JSONStream("agent");
  let controller!: ReadableStreamDefaultController<v2.AnyMessage>;
  // The SDK cancels the stream it reads when its connection closes, and the
  // stream takes nothing after that.
  let isCancelled = false;
  const readable = new ReadableStream<v2.AnyMessage>({
    start: (streamController) => (controller = streamController),
    cancel: () => void (isCancelled = true),
  });
  const writable = new WritableStream<v2.AnyMessage>({ write: (message) => socket.send(message) });

  const started = new Promise<Start>((resolve, reject) => {
    let first: Start | null = null;
    socket.onopen = () => socket.send({ agent: agent.id, path });
    socket.onmessage = (event) => {
      if (first) {
        if (!isCancelled) controller.enqueue(event.data as v2.AnyMessage);

        return;
      }

      first = event.data as Start;
      if (first.error) reject(new Error(first.error));
      else resolve(first);
    };
    // A stream closed to start the agent again is not the session's end.
    socket.onclose = () => {
      if (!isCancelled) controller.close();
      if (!first) reject(new Error(`${agent.name} stopped before it started`));
      else onEnd();
    };
  });

  return { stream: { readable, writable }, started, close: () => socket.close() };
}
