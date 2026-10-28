# mcp

The package is PMT's server for coding agents. It serves PMT's tools, its instructions and its starter tasks over MCP
to the agents on this computer, and passes on to the windows what an agent asks them to show.

The server runs on the official `modelcontextprotocol/go-sdk`, imported as `sdk` so that the receiver can be `mcp`.
Its tools answer through the other services' methods, the ones that answer the pages: `lsp`, `health`, `graph`,
`wiki`, `search` and `workspace`. It holds none of their logic.

| file | holds |
|---|---|
| `mcp.go` | the package comment; `Service`; `Task` and `Tasks`; `Variables`; the instructions; `ServiceStartup`, `ServiceShutdown`; `ReadURL`, `ReadToken`, `CreateToken`, `ListTasks`; the handler and its checks; the prompts |
| `tools.go` | the tools' inputs; `Preview`; the guide's texts; the editor's, the pages' and the app's tools; `read_guide`; the write tools' previews and tokens |
| `app.go` | `EventShow`; `View`, `Chip`, `Page`, `Show`, `Lookup`; `SetView`, `SetPages`; `describe_view` and `show` |

## The server

It listens at `http://127.0.0.1:<port>/mcp` from the app's start to its end, the port being the setting `agents.port`
(47820 unless the user sets another, read at the start). A port another program holds leaves the server off until the
next start, and PMT's log says so. It is stateless: each request stands alone.

A request is answered only with the token in its `Authorization` header (`Bearer <token>`). The token is the file
`mcp-token` in PMT's app folder, made at the first start; `CreateToken` makes a new one, after which every agent
registered with the old one must be registered again. The SDK refuses a request whose `Host` is not this computer, and
`http.CrossOriginProtection` refuses one sent from a web page of another site; a request with no `Origin`, as an
agent's is, passes.

PMT's own environment carries `PMT_MCP_URL` and `PMT_MCP_TOKEN`, so every program PMT starts, a shell or an agent,
has them. `Variables` lists the two as the Terminal's settings show them.

## The assistant

The instructions say what PMT is and give the working rules: find a name with PMT's tools and never invent one,
diagnose a file before calling a change done, write only in the workspace's mods, and leave saving, uploading and
Settings to the user. Their first paragraph stands alone, since Codex may read no more, and the whole stays under
Claude's 4,096 characters. `read_guide` holds the rest:

- `pages`: each page's help as the page catalog gives it, which the main window sends once (`SetPages`), then what
  `show` can bring to the front on each page;
- `workflow`: how a change is made with PMT's tools, from finding a name to checking the file;
- `workspace`: the open workspace's game and install, its mods in load order with their folders, and the game's
  folders with what each holds, read from the schema's databases.

The starter tasks are `Tasks`, served as MCP prompts and listed by `ListTasks` for the Agents page and the pane: fix
this file's problems, explain this page, write the missing localization, and why this event fires. Each text works
with the page's chip attached and, outside PMT, through `describe_view`.

## The tools

| tool | answered by |
|---|---|
| `diagnose` | `lsp.Diagnose`, or `lsp.DiagnoseText` for text the agent would write |
| `definition`, `references`, `list_symbols`, `find_symbols`, `complete`, `hover`, `signature` | the `lsp` methods of those names |
| `search_text` | `search.CollectText`, stopped when the agent cancels the call |
| `read_file` | `workspace.Read`, with the user's unsaved text as its draft |
| `build_report`, `list_overview`, `list_codes` | the `health` methods of those names; a report is filtered by mod, check and level |
| `find_roots`, `build_graph`, `build_detail` | the `graph` methods of those names |
| `search_pages`, `read_page`, `build_lexicon` | the `wiki` methods of those names; the Lexicon is filtered by name |
| `read_guide` | the guide above |
| `describe_view` | the view the window the user looked at last sent (`SetView`) |
| `rename` | `lsp.PrepareRename` and `lsp.Rename` |
| `apply_edit` | an entry's range from `lsp.ListSymbols`, its text replaced |
| `write_scaffold` | `health.ListScaffolds` for the preview, `health.WriteScaffold` for the change |
| `show` | the `mcp:show` event |

Positions are lines and columns from one, as the editor counts them. A tool's error comes back to the agent as the
tool's result, not as a protocol error.

The three write tools preview first: without a token, a call answers with the change it would make and a token, and
called again with that token, it makes the change once. They write only in the workspace's mods, through
`workspace.Write`, and leave alone a file with text the user has not saved.

## Showing

`show` sends `mcp:show` with a `Show`: a page with its query, a file with its lines selected, a Visual view window
with the states it is drawn at, or a Wiki look-up. The main window's agent store carries it out. The pages take what
an agent sets as their route's query, as Health's already did: the Event Graph's `node` and `filters`, Patch Notes'
`patch` and `find`.

## Events

| event | data | when |
|---|---|---|
| `mcp:show` | a `Show` | an agent asked PMT to show something |
