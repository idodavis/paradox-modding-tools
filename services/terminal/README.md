# terminal

The package is the IDE's terminal service. It starts and keeps the shells of the IDE's panel, streams each one to the
window that shows it, and finds the files that the shells' output names.

The shells themselves are `internal/terminal`'s. This service gives them their program and folder, holds them by id
until they end, and asks the workspace service for a folder's mod and a file's place.

| file | holds |
|---|---|
| `terminal.go` | the package comment; the events; `Service`; `Start`, `Run`, `Close`, `ListShells`, `SetScreen`, `ListPrograms`, `ListEnvironment`; the stream |
| `links.go` | `Link`; `FindLinks`, `ResolveReference` |

## Starting a shell

`Start` takes the active file's path, which may be "", the path of the program to run, and the terminal's size. A
program of "" is the default shell: the `terminal.shell` setting, else the first shell `internal/terminal` finds. A
program that list holds starts with its arguments, as Git Bash starts with `--login -i`; any other starts with none.
`Run` starts a command, a program and its arguments as given, in a tab of its own, as a coding agent's sign-in runs.
The folder is `workspace.ResolveFolder`'s: the folder of the mod that holds the file, else the workspace's first mod
whose folder exists, else the home folder. A shell ends when its tab closes, when its program exits, or when the app
quits.

`ListEnvironment` lists what every shell is given: `PMT_MCP_URL` and `PMT_MCP_TOKEN`, which PMT's own environment
carries for coding agents (`mcp.Variables`), then `internal/terminal`'s own variables.

## The stream

Each window that shows a shell opens the Wails stream `terminal` to it; no port is opened. The window's first message
names the shell, and its later ones carry what is typed (`input`, or `binary` for xterm's mouse reports), its new size
(`columns`, `rows`) or the offset of the output xterm has drawn up to (`offset`), as JSON. Go sends the shell's `Screen`
as JSON, then the output's bytes from the screen's offset on, at most 512 KiB past what the window has drawn: the shell
waits for the window beyond that. A window that hands the shells to another one first sends its screen with
`SetScreen`, so the next window draws the same screen, full-screen programs included, and then the output that came
after it.

## Links and references

- `FindLinks` reads one line of output for paths with a line, in the suffix forms VS Code's terminal reads (`:L`,
  `:L:C`, `(L,C)`, `", line L` and the rest). A path resolves as absolute, then from the shell's folder, then as a path
  the workspace loads. Only a file under a root of the workspace links. Ranges are in UTF-16 units, as the page counts
  the line's text.
- `ResolveReference` gives the text that names a file to an agent in the shell, as Claude Code and Copilot CLI read
  it: `@` and the path from the shell's folder, or the full path for a file outside it.

## Events

| event | data | when |
|---|---|---|
| `shell:started` | the shell's id | a shell started |
| `shell:exited` | the shell's id | a shell ended, by its tab, its program or the app |
