# Contributing

Paradox Modding Tools is a desktop app for Windows and Linux. This page is for people who build it from source.
macOS is not supported (the `darwin` Taskfile under `build/` comes from the Wails template; do not use it). What the
app does and how to install it is in [README.md](README.md); where it is going is in [ROADMAP.md](ROADMAP.md).

## What you need

| Tool | Notes |
|------|-------|
| [Go](https://go.dev/dl/) | The version `go.mod` names |
| [Node.js](https://nodejs.org/) with npm | Version 24 or later |
| [Task](https://taskfile.dev/installation/) | With `task` on your `PATH` |
| [Wails CLI](https://v3.wails.io/) | The same version as the Go module, installed with the command below |

Run this from the repository root, so Go reads the Wails version from `go.mod`:

```bash
go install "github.com/wailsapp/wails/v3/cmd/wails3@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)"
```

Check that `wails3` is on your `PATH`: Go installs it into `$(go env GOPATH)/bin`.

## Windows

The script parser (`services/internal/jomini`) is written in Go alone, and `task build` builds without C code
(`CGO_ENABLED=0`), so a normal Windows build needs no C compiler. To run the app, Windows needs WebView2 (Windows 11
usually has it).

## Linux

A Linux build needs a C compiler, GTK 4 and WebKitGTK 6, with their development packages (the headers, not only the
libraries the app runs on).

| Distro family | Packages to install for a build |
|---------------|---------------------------------|
| Debian / Ubuntu | `build-essential` `pkg-config` `libgtk-4-dev` `libwebkitgtk-6.0-dev` |
| Fedora / RHEL | `gcc` `pkg-config` `gtk4-devel` `webkitgtk6.0-devel` |
| Arch | `base-devel` `gtk4` `webkitgtk-6.0` |
| openSUSE | `gcc` `pkg-config` `gtk4-devel` plus the distro's WebKitGTK 6 `-devel` package |
| Gentoo | `gtk4` `webkit-gtk` (and a C toolchain) |

On Debian or Ubuntu:

```bash
sudo apt-get update
sudo apt-get install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
```

`task build` on Linux builds with C code (`CGO_ENABLED=1`). If no C compiler is found, the Linux Taskfile tries
Docker (`wails-cross`), which this repository does not build ahead of time in CI.

## Steam Workshop in dev

There is nothing to install. Publish to Steam starts a second copy of the app, which loads the game's own copy of
Steam's library from the game's install (`binaries/steam_api64.dll`, or `binaries/libsteam_api.so` for CK3 and Vic3
on Linux; EU5 has no Linux build).

## Run the app with live reload

From the repository root:

```bash
task dev
```

This starts `wails3 dev` with `build/config.yml`: the frontend, the bindings and the desktop window. If port 9245 is
taken, pick another for the frontend's dev server:

```bash
WAILS_VITE_PORT=9246 task dev
```

To let a tool drive the running app, start it with `WAILS_MCP=1 wails3 dev`, which turns on Wails' MCP server.

## Build the app

```bash
task build
```

This writes `bin/paradox-modding-tools.exe` on Windows and `bin/paradox-modding-tools` on Linux. The version shown in
the app and used by its updater comes from `VERSION`:

```bash
task build VERSION=x.y.z
```

To build for Windows from Linux (without C code, as the Windows Taskfile does by default):

```bash
wails3 task windows:build VERSION=x.y.z ARCH=amd64
```

GitHub Releases ship zips.

## Bindings

After you change a Go payload type or a service method, regenerate the frontend's bindings:

```bash
task common:generate:bindings
```

Never edit `frontend/bindings/**` by hand.

## Tests

Each Go package keeps its tests in one file, `<package>_test.go`.

| Task | What it runs |
|------|--------------|
| `task test` | The fast tests (`go test -short -timeout 60s ./...` in `services/`) |
| `task test:race` | The same tests under the race detector. This is what CI runs, and it works on Windows |
| `task check` | Everything a commit must pass: gofmt, the vets, staticcheck, golangci-lint and the fast tests, then the frontend's type check, lint and tests. Needs staticcheck 0.8.2 or later (`go install honnef.co/go/tools/cmd/staticcheck@latest`): older ones cannot read this Go version's export data |
| `task test:gate` | The tests over real installs of CK3, Victoria 3 and EU5. Set `PMT_TEST_INSTALL_CK3`, `PMT_TEST_INSTALL_VIC3` and `PMT_TEST_INSTALL_EU5` to each game's root folder (the one that holds `game/`); a game whose variable is unset is skipped |
| `task test:full` | `test:gate`, then the frontend's type check: run it before a commit that touches the engine |
| `task test:sweep` | A scoreboard, not a pass or fail: every mod in a game's Documents mod folder is opened over its install, and the findings and the hover answer rate are printed. It needs `PMT_TEST_INSTALL_<GAME>` and `PMT_TEST_MODS_<GAME>`, and it runs only when `PMT_TEST_SWEEP` is set, which the task does |

## Layout

| Path | What it holds |
|------|---------------|
| `main.go` | The app's start: Wails, the updater (checked against `SHA256SUMS`) and the single main window |
| `services/` | The Wails services, and `internal/` with the packages they use |
| `frontend/` | Vue 3, Nuxt UI, Pinia and Pinia Colada; `monaco-editor` (the editor), `@pierre/trees` (the explorer), Vue Flow and dagre (the Event Graph) |
| `build/` | The Wails Taskfiles, icons and the Windows version info |

How the backend fits together: [services/README.md](services/README.md). How the frontend is written:
[frontend/src/README.md](frontend/src/README.md). Notes for coding agents: [AGENTS.md](AGENTS.md).

## Issues and license

Use the issue templates for bugs and features; blank issues are turned off.

This project is licensed under **GPL-3.0-or-later**. See [LICENSE](LICENSE).
