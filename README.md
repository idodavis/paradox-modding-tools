**My Discord User:** https://discord.com/users/168438474905616386

# Paradox Modding Tools

![Paradox Modding Tools — Alpha · CK3 · Vic3 · EU5](docs/static/assets/banner.png)

Desktop workspace for **Crusader Kings III**, **Victoria 3**, and **Europa Universalis V** (partial) modders. One workspace is one game install plus the mods you are editing: script-aware IDE, event graph, compatibility and loc health, wiki patch notes, and Steam Workshop / Paradox Mods publish.

**Alpha.** Expect bugs. **Windows and Linux** (traditional desktop distros). **macOS is not supported.**

[Download](https://github.com/idodavis/paradox-modding-tools/releases) · [Docs](https://idodavis.github.io/paradox-modding-tools/) · [License](LICENSE)

Unofficial. Not affiliated with Paradox Interactive. Licensed **GPL-3.0-or-later**.

## What's in the app

| Page | What it does |
|------|----------------|
| **Library** | Workspaces by game, the active one in full; New workspace / New mod; Reset all PMT data |
| **New workspace** | Game → install (found or added) → mods in load order → name and language |
| **IDE** | Monaco editors in groups and splits; hover cards, completion, checks; search and replace; git source control with a graph and inline merge conflicts; the Wiki pane; game files read only |
| **Event Graph** | Events and on_actions and what fires them, by origin; open from the IDE with Ctrl Shift G |
| **Health** | Which mod's definition wins (first or last loaded), overrides, dependencies, dangling references, untranslated and misplaced localization, the editor's findings |
| **Patch Notes** | The game's patch notes from its wiki |
| **Publish** | Descriptor, Markdown/BBCode/Steam description, screenshots and videos, Steam Workshop upload and compare, Paradox Mods copy |
| **Settings** | The workspace (name, language, install, mods, upload exclusions) and the app's appearance (seven theme families, scale, pages) |

## Supported games

| Game | Coverage |
|------|----------|
| Crusader Kings III | Script, loc, events, health, wiki, publish |
| Victoria 3 | Same |
| Europa Universalis V | **Partial** — stage roots and `INJECT:` / `REPLACE:` |

## Supported OS

| OS | Notes |
|----|--------|
| **Windows** 10 / 11 | amd64 zip; Windows on ARM runs it under x64 emulation. Needs WebView2. |
| **Linux** amd64 | Debian/Ubuntu, Fedora/RHEL, Arch, openSUSE, Gentoo — GTK4 + WebKitGTK 6. |
| **macOS** | **Not supported.** |

Steam Deck, Flatpak, and AppImage are unsupported. See [limitations](https://idodavis.github.io/paradox-modding-tools/limitations/).

## Download

[GitHub Releases](https://github.com/idodavis/paradox-modding-tools/releases) publish zips:

- `paradox-modding-tools-windows-amd64.zip`
- `paradox-modding-tools-linux-amd64.zip`

plus a `SHA256SUMS` sidecar. Extract and run. Steam Workshop publish uses the game's own copy of Steam's library, so nothing extra ships.

In the app, the **footer version button** checks for updates against those zips. Steam client must be running to publish to Workshop.

Windows zips are **unsigned** (SmartScreen will warn).

## Develop locally

**Windows and Linux only.** Details and distro packages: [docs/content/develop.md](docs/content/develop.md) (same content on [Pages](https://idodavis.github.io/paradox-modding-tools/develop/)).

### Prerequisites

- **[Go](https://go.dev/dl/) 1.27.1** (`go.mod`)
- **[Node.js](https://nodejs.org/)** `^20.19.0` or `>=22.12.0`, + npm
- **[Task](https://taskfile.dev/installation/)** (Taskfile v3)
- **Wails v3 CLI** pinned to the module:

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.28
```

Windows builds use `CGO_ENABLED=0`. Linux needs GTK4 + WebKitGTK 6 **dev** packages (e.g. Debian/Ubuntu: `build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev`).

Workshop publish in `task dev` needs nothing extra: it loads the game's own copy of Steam's library.

### Commands

```bash
task dev
WAILS_VITE_PORT=9246 task dev   # if 9245 is taken
task build                      # bin/paradox-modding-tools[.exe]
task common:generate:bindings   # after Go DTO / service changes — never hand-edit frontend/bindings/**
go test ./...
```

## Project layout

| Path | Role |
|------|------|
| `main.go` | Wails entry, updater, single window |
| `services/` | Wails services; `services/internal/` holds the packages in layers: the leaves (`game`, `jomini`, `loc`, `appdata`), the files (`inventory`, `texture`, `modfiles`, `wiki`, `steam`), the engine (`schema`, `index`) and the workspace (`workspace`, `lsp`, `pages`) |
| `frontend/` | Vue 3 + Nuxt UI + Pinia + Pinia Colada; `monaco-editor`, `@pierre/trees`, Vue Flow |
| `docs/` | User guide (GitHub Pages) |
| `build/` | Wails Taskfiles and icons |

How the backend fits together: [services/README.md](services/README.md). The frontend's conventions:
[frontend/src/README.md](frontend/src/README.md). Terms: [GLOSSARY.md](GLOSSARY.md). Notes for coding agents:
[AGENTS.md](AGENTS.md).

## License

[GNU GPL v3.0 or later](LICENSE).

---

## Other Paradox projects

### CK3 Quieter Events

Successor to **Less Event Spam**: turns several full-screen or intrusive vanilla events into smaller toasts or messages.

[idodavis/ck3-quieter-events](https://github.com/idodavis/ck3-quieter-events)
