# AGENTS.md

The conventions for changing Paradox Modding Tools, for contributors and the coding agents they bring. Setting up a
machine, running the app and opening a pull request are in [CONTRIBUTING.md](docs/CONTRIBUTING.md). How the app is built,
and where each feature lives, is in [ARCHITECTURE.md](docs/ARCHITECTURE.md), which links every package's README.

## What PMT is

- A desktop app for Paradox modders, built with [Wails](https://v3.wails.io/) at the versions `go.mod` names: a Go
  backend, and a Vue 3, Nuxt UI, Pinia and Pinia Colada frontend. It is licensed under Apache-2.0.
- It is built around a workspace, one game install and its mods. Its pages are Library, IDE, Event Graph, Health,
  Patch Notes (the wiki's notes only), Publish (the Steam and Paradox Mods copy, and a compare with Steam) and
  Settings. It supports CK3 and Vic3, and EU5 in part.
- It runs on Windows and Linux (traditional desktop distros, with GTK 4 and WebKitGTK 6). **macOS is not supported**:
  add no Mac downloads, Mac user docs, macOS issue-template option or macOS build files.

## Commands

- `task dev` runs the app with live reload (`wails3 dev`). If port 9245 is taken, use `WAILS_VITE_PORT=9246 task dev`.
- `task check` is everything a change must pass: Go's format, vets, linters and tests, then the frontend's type
  check, lint and tests.
- `task test:gate` runs the tests over real installs. Set `PMT_TEST_INSTALL_CK3`, `PMT_TEST_INSTALL_VIC3` and
  `PMT_TEST_INSTALL_EU5`, each to a game's install root; a game whose variable is unset is skipped. `PKGS=` limits it
  to the packages a change reaches (`go list -deps`), and texture's picture encoding also needs `PMT_TEST_ENCODE`.
- `task common:generate:bindings` regenerates the frontend's bindings after a Go type or method the frontend sees
  changes. Never edit `frontend/bindings/` by hand.
- `task build VERSION=…` builds the app into `bin/`.

## Conventions

- Parser and semantics logic is game-agnostic and shared; game-specific facts live in `game`'s tables
  ([internal/game](internal/game/README.md)).
- Display fields (relative paths, origin) are on Go payloads (`workspace.Place`). Pages render a payload's fields as
  they are: no path helper, no view-model re-derived from what Go computed. Where client-side path math is
  unavoidable, use the standard library (`filepath.Rel`, Node `node:path`).
- What PMT stores is JSON (`config.json`) and gob caches; no SQLite.
- Every field the frontend sees has a camelCase JSON tag.
- **The frontend** is appearance and interaction only. `styles/themes.css` is the only palette: a block of tokens per
  `data-theme`, a dark and a light one per family, and the `@theme` / `--ui-*` bridge; the code's colors are the ten
  `--syn-<category>` tokens. Table filters live in UTable column state; fetch state in Pinia Colada.
- Nuxt UI component patterns (`:items` and similar) over ad-hoc `v-if`/`v-model` control flow where Nuxt UI fits.

## Code rules

Each rule says what checks it; "review" means nothing does, so the author and the reviewer do.

- **Functions** are at most 60 lines and four nesting levels; a literal table or a switch over an enum may be longer. A
  function that does two jobs is two functions; a loop body moved out to shed a level is not a helper. (golangci-lint
  `funlen` at 60 lines and 40 statements, and `nestif`; the rest review)
- **Names.** One identifier per thing everywhere (each package's README). A parameter, a field or a local is named for
  its type or for what it holds, never for how a call reads as a sentence: no preposition (`in`, `at`, `from`), no
  sentence word (`found`, `written`, `said`, `made`, `left`, `taken`), no letter outside a loop counter. An exported
  field says what it holds (`Count`, `Target`), not which way it points. A receiver is its type's word, a Wails
  service's its name in three letters (`ide`, `git`, `wsp`), never `s`; a local holding an `*index.Index` is `index`. No
  new identifier is built on `read`, `name`, `form` or `word`. American spelling (`color`).
  (golangci-lint `revive` for receivers and exported names, `misspell` for spelling; the rest review)
- **Comments.** Every exported name and every function has a doc comment written the way Go's own are, in plain
  English: short sentences, one fact each, ordinary words, as you would say it to a colleague. It begins with the
  name and says what the thing does or returns, then what happens in the special cases (not found, nil, "", -1,
  the error). A parameter is described in words ("one layer of a coat", "the given role"); its name is never
  dropped into a sentence as if it were a noun ("the layer at index layer", "the file at path"). A type's comment
  says what an instance is, not its fields; a package's says in two sentences what it does; a Vue or TS header is
  one or two plain sentences on what the file shows or does. Never the algorithm (that is a comment inside the
  body), never who calls it, never a library inventory, and no history, plan, stage, ruling, date or person. A
  comment line fits in 120 columns, a tab counting 2. A struct field's comment sits beside the field, on its line, a
  short phrase of what it holds and its special value ("" means none); that line may run to 240 columns, and no field
  takes a comment above or below it. A comment that restates the next line is deleted. The sample
  is `services/draw/*.go` in this repository. Before and after, from it: "ResolveWindow resolves a
  window of a file: its types and templates the ones the game keeps, block overrides applied, each property by the
  last written" became "ResolveWindow resolves one window in a GUI file. It applies the window's type and
  templates, looks up its texts and finds its textures. It returns an error if the file is not in the workspace or
  does not define the window." (`revive`'s `exported` for the leading name; the rest review)
- **Tests.** One test file per package, `<package>_test.go`; a test is named for what it tests. A real-install gate
  lives in the package's test file behind its environment variable. (review)

## Pull requests

- Say what you changed and why, which games and system you tried it on, and add a screenshot when something on
  screen changed. The pull request template asks for each.
- `task check` passes.
- A push to `main` drafts a release. The maintainer writes its notes and publishes it.
