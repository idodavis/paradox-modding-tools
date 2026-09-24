---
title: Develop
description: Build Paradox Modding Tools from source on Windows or Linux.
weight: 9
---

# Develop

How to run and build PMT from this repo. **Windows and Linux only.** macOS is not a supported develop target (a `darwin` Taskfile may exist for the Wails template; do not use it for product work).

Human-facing product docs start at the [home page]({{< relref "/" >}}). This page is for contributors.

## Prerequisites

| Tool | Version / notes |
|------|-----------------|
| [Go](https://go.dev/dl/) | **1.27.1** (see `go.mod`) |
| [Node.js](https://nodejs.org/) + npm | Current LTS (18+). Frontend is Vue 3 + Vite |
| [Task](https://taskfile.dev/installation/) | Taskfile v3 (`task` on your `PATH`) |
| [Wails v3 CLI](https://v3.wails.io/) | Pin to the module: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.21` |

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.21
```

Confirm `wails3` is on your `PATH` (`$(go env GOPATH)/bin`).

## Windows extra

The script parser (`services/internal/lang/jomini`) is **pure Go**. Production and `task build` use `CGO_ENABLED=0`. You do not need GCC for a normal Windows desktop build.

WebView2 must be installed to **run** the app (Win11 usually has it).

## Linux extra (build)

Linux desktop builds need **CGO**, a C compiler, GTK 4, and WebKitGTK 6 **dev** packages (not only the runtime `.so` files).

| Distro family | Typical **build** packages |
|---------------|----------------------------|
| Debian / Ubuntu | `build-essential` `pkg-config` `libgtk-4-dev` `libwebkitgtk-6.0-dev` |
| Fedora / RHEL | `gcc` `pkg-config` `gtk4-devel` `webkitgtk6.0-devel` |
| Arch | `base-devel` `gtk4` `webkitgtk-6.0` |
| openSUSE | `gcc` `pkg-config` `gtk4-devel` plus the distro’s WebKitGTK 6 `-devel` |
| Gentoo | `gtk4` `webkit-gtk` (and a C toolchain) |

Debian/Ubuntu example:

```bash
sudo apt-get update
sudo apt-get install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev
```

`task build` on Linux uses `CGO_ENABLED=1`. If `gcc`/`clang` is missing, the Linux Taskfile tries Docker (`wails-cross`), which this repo does not pre-build in CI.

## Steam Workshop in dev

Nothing to fetch: **Publish → Steam** runs a second copy of the app, which loads the game's own copy of Steam's library from its install (`binaries/steam_api64.dll`, or `binaries/libsteam_api.so` on Linux).

## Develop (hot reload)

From the repository root:

```bash
task dev
```

This runs `wails3 dev` with `./build/config.yml` (frontend, bindings, desktop window). Override the Vite port if 9245 is taken:

```bash
WAILS_VITE_PORT=9246 task dev
```

Optional: `WAILS_MCP=1 wails3 dev` starts Wails’ MCP server for driving the running app.

## Build a production binary

```bash
task build
```

Writes `bin/paradox-modding-tools.exe` (Windows) or `bin/paradox-modding-tools` (Linux). Pass a version for the UI / updater string:

```bash
task build VERSION=0.4.0
```

Cross-compile Windows from Linux with `CGO_ENABLED=0` (the Windows Taskfile default):

```bash
wails3 task windows:build VERSION=0.4.0 ARCH=amd64
```

GitHub Releases ship **zips**.

## Bindings

After Go DTO or service signature changes:

```bash
task common:generate:bindings
```

**Never hand-edit** `frontend/bindings/**`.

## Tests

Each Go package keeps its tests in one file, `<pkg>_test.go`.

| Task | What it runs |
|------|--------------|
| `task test` | The fast tests (`go test -short -timeout 60s ./...` in `services/`) |
| `task test:race` | The same under the race detector; what CI runs. It works on Windows (`CGO_ENABLED=0`) |
| `task check` | The frontend typecheck, `.vue` files included (`npm run check` in `frontend/`) |
| `task test:gate` | The real-install tier: the source, schema, learner, index and vanilla-diagnostics gates over CK3, Victoria 3 and EU5. Set `PMT_TEST_INSTALL_CK3`, `PMT_TEST_INSTALL_VIC3` and `PMT_TEST_INSTALL_EU5` to each game's root folder (the one that holds `game/`); a game whose variable is unset is skipped |
| `task test:full` | `test:gate`, then `check`: before a commit that touches the engine |
| `task test:sweep` | The engine's scoreboard, not a gate: every mod in a game's Documents mod folder opened over its install, with the health report and hover answer rate printed. Needs `PMT_TEST_INSTALL_<GAME>` and `PMT_TEST_MODS_<GAME>`; `services/sweep_test.go` builds only with the `sweep` tag |

## Layout (high level)

| Path | Role |
|------|------|
| `main.go` | Wails entry, updater (`SHA256SUMS`), single window |
| `services/` | Wails services only, + `internal/` (game, lang, local, lsp, session, steam, wiki) |
| `frontend/` | Vue 3 + Nuxt UI + Pinia + monaco-vscode-api |
| `docs/` | This site (Hugo). Content in `docs/content/` |
| `build/` | Wails Taskfiles, icons, Windows `info.json` |

Agent-facing architecture notes: [`AGENTS.md`](https://github.com/idodavis/paradox-modding-tools/blob/master/AGENTS.md) in the repo. Terminology: [`GLOSSARY.md`](https://github.com/idodavis/paradox-modding-tools/blob/master/GLOSSARY.md).

## Docs site locally

No Ruby. Hugo is a single binary (plain, not Extended — this site is CSS, not Sass):

```bash
go install github.com/gohugoio/hugo@v0.147.8
cd docs
hugo server
```

Open the URL Hugo prints (usually `http://localhost:1313/paradox-modding-tools/`). On Windows you can also `winget install Hugo.Hugo`. You do not need Hugo installed to work on the app.
