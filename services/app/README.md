# app

The package is the app's own service: its version, updates, settings, log and windows. It also holds PMT's folders on
this machine and config.json, which every other service reads and writes.

Of the engine it calls only `files`, for the atomic write. Every other service holds the one `ConfigFile` that `main`
opens, and reads and writes the config through it.

| file | holds |
|---|---|
| `app.go` | the events; `DevBuild`; `Service` with `ServiceStartup` and `ServiceShutdown`; `Update`, `Release`; `OpenConfig`, `OpenLog`; the version and the updater (`Version`, `CheckUpdate`, `ApplyUpdate`, `ListReleases`); the settings (`ListSettings`, `ReadSettings`, `WriteSetting`); the log (`CrashedLastRun`, `LogPath`, `BuildDiagnostics`); `ShowFolder`; the windows (`OpenMainWindow`, `OpenWindow`, `ShowWindow`, `CloseWindow`, `ListWindows`) |
| `config.go` | the package comment; `Dirs` and `ResolveDirs`; `LoadJSON`, `SaveJSON`; the records (`Install`, `Era`, `Calendar`, `Mod`, `Workspace`, `Config`); `Setting` and `Catalog`; `ConfigFile` with `Config`, `Update` and `LookupSetting` |

## PMT's folders

`Dirs` is the folders PMT keeps its files in on this machine. `ResolveDirs` puts config.json in the user's config
folder, and the log, the caches and the trash in the user's cache folder, each as the system places them. The cache
folder holds the schema and index caches, the wiki's pages and the Steam snapshots. A reset empties it, so the log,
which is open while the app runs, sits beside it. The trash holds deleted files until the next start, which empties
it.

## config.json

A `ConfigFile` is config.json held in memory for every service. `Config` returns a copy that shares no slice or map
with the file's own. `Update` changes the config under its lock, saves it whole and emits `config:changed` with the
section it wrote: `installs`, `workspaces`, `settings`, or "" for all of them. A change that returns an error saves and
emits nothing.

A missing file is a first run. Any other read failure gives an empty config that is read-only, so a passing fault never
overwrites the real file. The config carries no format version: a changed record means Reset data, never a migration.

The records are plain. An `Install` is one game install PMT has recorded, with its version and when it was last
scanned. A `Workspace` is an install and its mods in load order, with its language, its home page and its own settings.
A `Mod` is a mod's folder with its color, its `Calendar`, its Workshop id, its upload exclusions and where its
Workshop description comes from. The open
workspace's view of a mod, with its descriptor, is `workspace.Mod`, which embeds this record.

## Settings

`Catalog` is the one list of every setting, and the frontend fetches it once. A `Setting` row carries its key
(`<area>.<noun>`), its label and help, its section, its type and default, its choices or range, and the words the
command palette also matches. A setting marked `Workspace` is kept in the workspace's record, and any other in the
config's own `Settings`. `LookupSetting` reads a value by that rule, and falls back to the catalog's default.

## The log and the running mark

`OpenLog` opens pmt.log in the log folder, and starts it over once it has grown too large. `ServiceStartup` writes a
running mark beside it and `ServiceShutdown` removes it, so a mark found at the next start means the last run never shut
down. `CrashedLastRun` reports that, except in a dev build (`DevBuild`), which stops without a clean shutdown each time
it is rebuilt. `BuildDiagnostics` writes what a bug report needs: the version, the system, the
installs and the end of the log.

## Updates

`CheckUpdate` asks the project's GitHub releases through Wails's updater, and emits `app:update` when a newer version
exists. `ApplyUpdate` downloads it beside the app and restarts into it. `ListReleases` lists the releases from the
newest down to the one running, with their notes, and leaves out drafts and prereleases.

## Windows

Every window has no frame of the system's: the page draws its own title bar and buttons. On Windows the page marks
them with `--wails-non-client-region` and the window is hosted through composition, so Windows treats them as its
own and dragging, snapping, the system menu and the Snap Layouts work; Linux drags by `--wails-draggable`.

Wails's JavaScript runtime acts only on the window that calls it, so showing or closing another window goes through
this service. The main window's close is held and sent to the page as `window:closing`, until `CloseWindow` lets it
through and the app quits. A window that `OpenWindow` opens gets one `window:closing` before it closes, so the page can
hand its tabs back. It closes when the page closes it again, or when a timer ends, and `window:closed` follows.

## Events

| event | data | sent when |
|---|---|---|
| `config:changed` | the section written | config.json was written |
| `app:update` | an `Update` | a newer version exists |
| `window:closing` | the window's name | a window waits for the page's answer before it closes |
| `window:closed` | the window's name | a window closed |

## Terms

- **Dirs**: the folders PMT keeps its files in.
- **Config**: the content of config.json.
- **ConfigFile**: config.json held in memory for every service.
- **Install**: one game install that PMT has recorded.
- **Workspace**: one recorded workspace: an install and its mods, in load order.
- **Mod**: one mod that a workspace records.
- **Calendar**: how a mod counts years and names months.
- **Era**: one era of a mod's calendar.
- **Setting**: one row of the catalog of settings.
- **Catalog**: the list of every setting.
- **Update**: the result of a check for a newer version.
- **Release**: one published version of the app, with its notes.
