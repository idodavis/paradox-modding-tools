**My Discord User:** https://discord.com/users/168438474905616386

# Paradox Modding Tools

Cross-platform desktop utilities for **Paradox Interactive** game modders. The app is built with **[Wails v3](https://v3.wails.io/)** (**Go** backend, **Svelte** frontend). **Crusader Kings III** and **Europa Universalis V (Partial)** are supported today; the direction is to grow coverage and workflows across Paradox titles, not only CK3.

## Download / Use The Tool! (testing)

Builds are published under [GitHub Releases](https://github.com/idodavis/paradox-modding-tools/releases) for manual download while the modding community tries them out. An in-app updater is planned so testers do not need to fetch every build from Releases; until that ships, **Releases remain the source of binaries**. Release packaging is manual for now; **GitHub Actions** automation for builds and uploads is planned.

## What’s in the app

- **Compare tool** — Diff files or whole directory trees (e.g. vanilla vs mod, or two mod versions) to see what changed after patches.
- **Merge tool** — Merge Paradox-style script pairs using the internal parser underneath for fully automatic merges or for assisted merges alongside merge editor ui.
- **Inventory** — Explore extracted game/script objects (game-dependent), saved to a local DB file in AppData (Default User files location dependent on OS being used).
- **Modding docs** — Browse info_file related reference material  written by Paradox for modders. Also embedded Paradox Games Wiki.
- **Settings** — Game paths, Steam integration, and other preferences backed by a local database.

### Paradox script parser (Go)

A **Go** parser (Participle-based) parses typical Paradox `.txt` script for compare/merge and related features. Implementation lives under `services/internal/interpreter/`.

## Prerequisites (from source)

- **[Go](https://go.dev/dl/)** (see `go.mod` for the required version)
- **[Node.js](https://nodejs.org/)** and **npm** (for the Svelte frontend)
- **[Task](https://taskfile.dev/installation/)** (Taskfile v3)
- **[Wails v3 CLI](https://v3.wails.io/)** (`wails3`), aligned with the `github.com/wailsapp/wails/v3` version in `go.mod`

Install the Wails CLI following the official v3 docs so `wails3` is on your `PATH`.

## Develop (hot reload)

From the repository root:

```bash
task dev
```

This runs `wails3 dev` with `./build/config.yml` (frontend build, binding generation, and the app in development mode). Override the Vite port if needed, for example:

```bash
WAILS_VITE_PORT=9246 task dev
```

## Build a production binary

OS-specific tasks are selected automatically (`windows`, `darwin`, `linux`):

```bash
task build
```

The executable is written under `bin/` (e.g. `bin/paradox-modding-tools.exe` on Windows, `bin/paradox-modding-tools` on macOS/Linux).

## Run the built binary

After a successful `task build`:

```bash
task run
```

## Package installers (optional)

```bash
task package
```

Uses the platform’s configured format (e.g. NSIS on Windows). You need the extra tooling each format expects (see `build/windows/Taskfile.yml` and sibling platform Taskfiles).

## Other useful tasks

| Task | Purpose |
|------|--------|
| `task setup:docker` | Docker image for cross-compilation / CGO workflows |
| `task build:server` / `task run:server` | Server-style build without the desktop shell (see Taskfiles) |

## Project layout (high level)

| Path | Role |
|------|------|
| `main.go` | Wails app entry, services, embedded `frontend/dist` |
| `services/` | Go services exposed to the UI |
| `frontend/` | Svelte + Vite UI |
| `build/` | Wails build config, icons, platform Taskfiles |

---

## Other Paradox projects

### CK3 Quieter Events Mod

Successor to **Less Event Spam**: turns several full-screen or intrusive vanilla events into smaller toasts or messages.

Find it [here](https://github.com/idodavis/ck3-quieter-events).
