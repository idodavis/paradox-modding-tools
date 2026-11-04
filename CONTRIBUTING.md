# Contributing

How to build Paradox Modding Tools from source, run its tests and send a first change. What the app does is in
[README.md](README.md) and [FEATURES.md](FEATURES.md). How it is built is in [ARCHITECTURE.md](ARCHITECTURE.md), and
the code's conventions are in [AGENTS.md](AGENTS.md). PMT runs on Windows and Linux; macOS is not supported.

## What you need

| Tool | Version |
|---|---|
| [Go](https://go.dev/dl/) | The one `go.mod` names |
| [Node.js](https://nodejs.org/) with npm | 24 or later |
| [Task](https://taskfile.dev/installation/) | Any recent one, on your `PATH` |
| [Wails CLI](https://v3.wails.io/) | The same version as the Go module (below) |
| [staticcheck](https://staticcheck.dev/) and [golangci-lint](https://golangci-lint.run/) v2 | For `task check` (below) |

From the repository root, install the Wails CLI at the version `go.mod` names, then the two linters:

```bash
go install "github.com/wailsapp/wails/v3/cmd/wails3@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)"
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
npm ci --prefix frontend
```

Go installs the tools into `$(go env GOPATH)/bin`; make sure it is on your `PATH`.

### Windows

Nothing more. The parser is pure Go, so Windows builds without a C compiler (`CGO_ENABLED=0`). Running the app needs
WebView2, which Windows 11 has.

### Linux

A C compiler, GTK 4 and WebKitGTK 6, with their development packages:

| Distro family | Packages |
|---|---|
| Debian, Ubuntu | `build-essential` `pkg-config` `libgtk-4-dev` `libwebkitgtk-6.0-dev` |
| Fedora, RHEL | `gcc` `pkg-config` `gtk4-devel` `webkitgtk6.0-devel` |
| Arch | `base-devel` `gtk4` `webkitgtk-6.0` |
| openSUSE | `gcc` `pkg-config` `gtk4-devel` and the distro's WebKitGTK 6 `-devel` package |
| Gentoo | `gtk4` `webkit-gtk` and a C toolchain |

Linux builds with cgo (`CGO_ENABLED=1`). Without a C compiler, the Linux Taskfile falls back to Docker
(`wails-cross`).

## Run the app

```bash
task dev
```

This runs `wails3 dev`: the frontend with live reload, the bindings and the window. If port 9245 is taken, pick
another with `WAILS_VITE_PORT=9246 task dev`. To let a tool drive the running app, start it with
`WAILS_MCP=1 wails3 dev`, which turns on Wails' MCP server.

Publishing to the Workshop works in a dev run too, with nothing to install: PMT loads the copy of Steam's library that
the game ships.

## Build it

```bash
task build                 # bin/paradox-modding-tools(.exe)
task build VERSION=x.y.z   # the version the app shows and its updater compares
```

To build for Windows from Linux: `wails3 task windows:build VERSION=x.y.z ARCH=amd64`.

## Bindings

After you change a Go service method or a type the frontend sees, regenerate the frontend's bindings:

```bash
task common:generate:bindings
```

Never edit `frontend/bindings/` by hand.

## Tests

Each Go package keeps its tests in one file, `<package>_test.go`.

| Task | What it runs |
|---|---|
| `task check` | Everything a commit must pass: gofmt, the vets, staticcheck, golangci-lint and the fast tests, then the frontend's type check, lint and tests |
| `task test` | The fast Go tests alone (`go test -short -timeout 60s ./...`) |
| `task test:race` | The same tests under the race detector, as CI runs them |
| `task test:gate` | The tests over real installs of CK3, Victoria 3 and EU5 |
| `task test:full` | `test:gate`, then the frontend's type check: run it before a change to the engine |

The install gates read each game from the folder its variable names, the one that holds `game/`:
`PMT_TEST_INSTALL_CK3`, `PMT_TEST_INSTALL_VIC3` and `PMT_TEST_INSTALL_EU5`. A game whose variable is unset is
skipped. The game's dumps are read from its folder under Documents. `task test:gate PKGS="./internal/schema/..."`
runs only the gates of the packages you name: those your change reaches, which `go list -deps` shows. Encoding every
picture of the installs takes most of half an hour, so that gate also needs `PMT_TEST_ENCODE` set; set it when you
change `internal/texture`.

## Your first change

1. Fork the repository and make a branch.
2. Run `task dev` and find your way around; [ARCHITECTURE.md](ARCHITECTURE.md) is the map, and each package's README
   explains its part.
3. Pick an issue. Those marked [good first issue](https://github.com/idodavis/paradox-modding-tools/labels/good%20first%20issue)
   are a good start.
4. Make the change, following [AGENTS.md](AGENTS.md). Run `task check`.
5. Open a pull request. Its template asks what you changed and why, which games and system you tried it on, and for
   a screenshot when something on screen changed.

Questions are welcome in [Discussions](https://github.com/idodavis/paradox-modding-tools/discussions). How a release
goes out is in [RELEASING.md](RELEASING.md).

## License

PMT is licensed under [Apache-2.0](LICENSE). By sending a change, you agree that it is licensed the same way, as
section 5 of the license says.
