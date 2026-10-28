# agent

The package starts the coding agents PMT chats with. It lists the agents, installs them from the ACP registry with npm
on Node, and pipes each running agent to the window that chats with it.

Go holds none of the Agent Client Protocol: the page speaks it, through the official TypeScript SDK, over the stream
this package serves. Go starts the program, carries its lines both ways and ends it.

| file | holds |
|---|---|
| `agent.go` | the package comment; `Service`; `Agent`, `Node`; the registry's types; the agents table; `ListFiles`; `ListAgents`, `AddAgent`, `RemoveAgent`; `DescribeNode`, `InstallNode`, `InstallAgent`; the registry, the installed versions, npm, Node's archives |
| `process.go` | the stream: its first messages, `ServiceStartup`, the program and its pipes, the lines both ways |
| `process_windows.go`, `process_linux.go` | the group each agent runs in: a job object on Windows, a process group on Linux |

## The agents

`agents` is the table of the agents PMT offers: Claude, GitHub Copilot and Codex, the coding agents whose makers stand
behind an ACP agent for them. A row is an `app.Agent` with its registry id, the text that registers PMT's tools with
it for use outside PMT, and the instruction files and folders it reads in a mod. `ListFiles` gives those files to
Publish, which leaves them out of an upload.

The user's own agents are `Config.Agents`, written by `AddAgent` and `RemoveAgent` as the config section `agents`.
One is a name with a program and its arguments.

`ListAgents` returns both, each an `Agent`: its record, the registry's version and words, and the version installed.
An agent PMT offers has its register text with PMT's address and token filled in, and once installed with a Node new
enough, the program and arguments that start it: its package's script on Node, with the registry's arguments.

## Installing

The registry is fetched from the ACP project at most once a day and kept as `registry.json`; without a network the
last copy is used. Everything lives under `Dirs.Cache/agents/`, so resetting PMT's data removes it:

- `<id>@<version>/` holds one agent, installed by `InstallAgent` with npm at the registry's version; its other
  versions are removed. Its program is the one script its package declares.
- `node/` holds PMT's own Node, which `InstallNode` downloads: the newest long-term release from nodejs.org, checked
  against the sums nodejs.org publishes, a zip on Windows and a gzipped tar on Linux.

`DescribeNode` says which Node the agents run on: the system's if it is 22 or newer, the floor Claude's adapter sets,
else PMT's own, else none.

## The stream

Each session opens the Wails stream `agent`; no port is opened. The window's first message names the agent and the
active file. Go starts the agent in the folder of the mod that holds the file, else the workspace's first mod
(`workspace.ResolveFolder`, the terminal's rule), with PMT's own environment and the agents' Node first on its PATH.
It answers with a `Start`: the folder and the workspace's other mod folders, or the error that kept the agent from
starting.

Each later message is one JSON-RPC message, written to the agent's input as one line, and each line of its output goes
back as one message. What it writes as errors goes to PMT's log under its id. The agent runs in a group of its own, so
closing the stream ends it and every program it started; its end closes the stream.
