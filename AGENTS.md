# Agent notes

The tree being built is the `rearchitecture` branch: its design, with the feature list every page answers to, is
`.claude/plans/rearchitecture.md`, its builder briefs are under `.superpowers/sdd/rearchitecture/briefs/`, and each
package's README holds its design and its terms.

## Learned User Preferences

- No code for backwards compatibility.
- Display fields (relative paths, origin) are on Go payloads (`workspace.Place`). Pages render a payload's fields as
  they are: no path helper, no view-model re-derived from what Go computed. Where client-side path math is
  unavoidable, use the standard library (`filepath.Rel`, Node `node:path`).
- Change only what was explicitly requested; no unrelated refactors or drive-by edits. Ask when a requirement is
  ambiguous.
- Parser and semantics logic is game-agnostic and shared; game-specific facts live in `game`'s tables.
- What PMT stores is JSON (`config.json`) and gob caches; no SQLite.
- Performance-sensitive work goes in Go behind the Wails bridge where that stays maintainable; UI logic stays in the
  frontend.
- "Patch" (not "update" or "campaign") for mod version retargeting; clear product names over jargon.
- Nuxt UI component patterns (`:items` and similar) over ad-hoc `v-if`/`v-model` control flow where Nuxt UI fits.
- Dense layouts where editor, diff or merge content fills the viewport; path and config chrome stays compact.

## Standing orders

The owner has given each of these many times. They hold for every task, plan and file; none is open to interpretation.

- **The guiding words:** consolidate, simplify, idiomatic, reduce, efficient, readable, DRY. Architecture over arbitrary
  fixes, helpers, statements, loops and checks.
- **Cleanup is a rewrite.** A file is written whole from a written target, then the old one is deleted. Never a pile of
  one-line patches, never a shim or adapter, never a representation half left behind. Only the user-facing features are
  sacred; no code is. A cleanup never ends with more files, functions or lines than it started with: a file is cut by
  topic, never split into composables or components with one caller to look shorter.
- **DRY against the engine.** Before writing a function, find the one the app has (`jomini`, `loc`, `files`, `game`,
  `schema`, `index`, `workspace`, `lsp`; the page's own folder, `stores/`, `lib/`) and call it; where it needs a
  parameter, it gets one in its one place. No parallel helper, no recomputing what another package computed.
- **A near-duplicate is a duplicate.** Two pieces of logic 60–70% alike are one function both callers use, the
  difference a parameter or a row. A thing is computed once, written once, explained once; a duplicate check that
  passes only exact matches has not been run.
- **No carrier types.** Data that exists is used as it is, extended by a field where it lacks one. Never a second shape
  of the same data made to pass it along and shape it back (a DTO beside the payload, a record beside the engine's).
- **Easy to digest** outranks every count: simple, straightforward, coherent, one whole. Not smart, not clever, no magic.
- **Services are Wails's own shape.** A service is a package under `services/` whose `Service` methods hold their
  logic in the files of their topics; no wrapper of a same-named function, no constructor, no `ServiceName`. The
  engine is the packages under `internal/`, which no service duplicates.
- **Commits.** Never without the owner saying so in that message. The index is the owner's: no `add`, `reset` or `stash`.
- **STOP** means everything stops at once, background agents included, with no follow-up question.
- **No size forecasts.** Sizes are measured after the work and reported as consequences. Claims about the games are
  measured on all three installs, never CK3 alone.
- **A false premise is a question.** When a plan or order rests on something untrue, stop and ask with options drawn in
  a plain message, answered by letter; never solve it quietly. A question is answered, then work waits for the go.
- **My edits are mine.** A report names what I changed as my change, never as something that "changed".
- **Plans** are short, every decision made up front by identifier, progress ticked in the plan file the same turn, no
  stale wording, no questions mid-execution.
- **Before done**, the naming, code-style, refactoring and Vue best-practices skills run over every touched file.
- **Files the owner adds** without a path: ask where they are; never sweep home folders. Screenshots are of the preview
  pane only.
- **Side windows, tools and widgets** open closed by default.
- **Comments are written one way**, the owner's, set out under Code rules › Comments: plain English, what the thing
  does and its special cases, parameters described in words. No agent, session or subagent deviates from it unless
  the owner says so in that message.

## Code rules

Each rule says what checks it; "review" means nothing does, so the author and the reviewer do.

- **Reading is the gate.** A change is done when its package can be read and a reader can say what it does, where each
  decision is made and why. Size is reported, never a reason to pack, to skip a split or to keep a helper out. (review)
- **One decision, one place.** A rule is a data row that carries its whole condition, or one named function with a doc
  comment; never a constant whose meaning is a `case` somewhere else. (review)
- **Functions** are at most 60 lines, four nesting levels and four parameters; a literal table or a switch over an enum
  may be longer. A function that does two jobs is two functions; a loop body moved out to shed a level is not a helper.
  (golangci-lint `funlen` at 60 lines and 40 statements, and `nestif`; the rest review)
- **Helpers.** A helper exists when it names a decision or has two callers. Nothing is inlined to save a function. (review)
- **Structs.** A struct with methods carries state that changes across its calls; two values that never change are
  parameters, and no holder struct is made to spare a parameter list. (review)
- **Data over code.** A table row carries its consequence as a field, not as a function per row; one reader per dialect,
  and no flag that switches a reader's behavior for a second caller. (review)
- **Cuts delete a representation:** a type, a pass over the data, a cache, a function. Moving and lightly altering is not
  cutting, and two clear functions that share a skeleton are never merged into one with a mode flag. (review)
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
  comment line fits in 120 columns, a tab counting 2. A comment that restates the next line is deleted. The sample
  is `services/draw/*.go` on the `rearchitecture` branch. Before and after, from it: "ResolveWindow resolves a
  window of a file: its types and templates the ones the game keeps, block overrides applied, each property by the
  last written" became "ResolveWindow resolves one window in a GUI file. It applies the window's type and
  templates, looks up its texts and finds its textures. It returns an error if the file is not in the workspace or
  does not define the window." This style is the owner's. No agent, session or subagent deviates from it unless
  the owner says so in that message, and a subagent's prompt carries this rule verbatim. (`revive`'s `exported`
  for the leading name; `.claude/hooks/comments.mjs` reports a parameter used as a noun and a line past 120
  columns; the rest review)
- **Files.** The header comment, the imports, the types, the constants and tables, the file's main function, then its
  helpers in the order they are called. A file is cut by topic, never by sub-topic. (review)
- **Tests.** One test file per package, `<package>_test.go`; a test is named for what it tests; none is added unasked.
  A real-install gate lives in the package's test file behind its environment variable. (review)
- **Replacing a reader or a merge.** List every rule the old one applies and carry each over or name it dropped; the
  stop is no real claim lost against the three installs, never a zero diff. (review)
- **Commits.** One commit per piece of work, made when the whole of it passes; never one after each step, which is
  checked and left uncommitted. `task check` passes before every commit; the message says what changed. (review)

## Product / stack

- Paradox Modding Tools is a Wails desktop app at the versions `go.mod` names (a Go backend, a Vue 3 + Nuxt UI + Pinia
  + Colada frontend) for Paradox modders, licensed under GPL-3.0-or-later.
- **Supported OS:** Windows and Linux (traditional desktop distros, GTK4 + WebKitGTK 6). **macOS is not supported** —
  do not add Mac downloads, Mac user docs, or a macOS issue-template option. Leave the Wails `darwin` Taskfile in the
  tree; do not advertise it.
- The product is workspace-centric: Library, IDE, Event Graph, Health, Patch Notes (wiki notes **only**), Publish
  (Steam + Paradox Mods copy, and a compare with Steam), Settings. CK3, Vic3, EU5 (partial).
- Releases: per-platform **zips** on GitHub (`paradox-modding-tools-{windows-amd64,linux-amd64}.zip` + sibling
  `SHA256SUMS`; Windows on ARM runs the amd64 zip under emulation). The in-app updater is `github.New`
  (`ChecksumAsset: "SHA256SUMS"`) — do not set `Prerelease: true`. Workshop publish runs in a second copy of the app
  itself (`--steam-publish`), which loads the game's own copy of Steam's library where `Game.SteamLibrary` says the
  install keeps it; PMT ships nothing of Steam's.
- Local develop/build: Taskfile (`task dev`, `task build`) with `wails3` at the version `go.mod` names. Windows
  `CGO_ENABLED=0` (the parser is pure Go); Linux `CGO_ENABLED=1` (WebKit).

## Commands

- `task dev` / `WAILS_VITE_PORT=9246 task dev` — `wails3 dev`.
- `task build VERSION=…` — platform Taskfile → `bin/`.
- `task common:generate:bindings` after a Go type or method the frontend sees changes. Never hand-edit
  `frontend/bindings/**`.
- `task check` — everything a commit must pass, Go and frontend.
- The install gates run with `PMT_TEST_INSTALL_CK3`, `PMT_TEST_INSTALL_VIC3` and `PMT_TEST_INSTALL_EU5` set, each a
  game's install root.
- Every `main` push that changes more than Markdown cuts a release (`.github/workflows/release.yml`); first floor
  **v0.4.0**, then patch bumps.
- **Measure sprawl** (repo root, bash): `find internal services main.go -name '*.go' ! -name '*_test.go' | xargs wc -l
  | tail -1`, the same for `*_test.go`, and `find frontend/src -name '*.vue' -o -name '*.ts' -o -name '*.css' | xargs
  wc -l | tail -1`. Success of a de-sprawl pass is fewer files and lines, not extra layers.

## Learned Workspace Facts

- **The engine** is `internal/`: `jomini` (the syntax, never importing `game`), `loc`, `files`, `texture`, `game`,
  `schema` and `index`. The only embedded game data is the shipped backup schemas (`internal/schema/shipped`, made by
  `schema/gen`): what the dumps declare, used where a dump file is missing. The language rules and each game's file
  layout live only in `game`'s tables. The one walker is `schema.Walk`.
- **No migrations.** `config.json` carries no format version: the app is unreleased, so a changed struct means Reset
  data, never a migration. The schema and index caches are named by the app's version, so a new build scans again.
- **App config** is held by `app.ConfigFile` (`OpenConfig`) for every service, through `Config` and `Update`. A missing
  file is a first run; any other read failure makes it empty and read-only, so a passing fault never overwrites the
  real file.
- **The open workspace:** the workspace service holds the one open session, and every other service asks it through
  `Session()` at each call. Opening closes the one open before it. `DeleteInstall` refuses while a workspace uses the
  install; `DeleteWorkspace` removes the record only, mod folders on disk stay. A mod whose folder is missing is broken.
- **Bindings:** every bound field has a camelCase JSON tag; `@services` is `frontend/bindings/paradox-modding-tools/
  services`. A workspace switch is localStorage `pmt.workspace.activeId` plus `Open`. Tool pages' reads are enabled by
  the workspace store's `isOpen`, which is `State().open`.
- **Events** are each service's `Event*` constants, registered in its `init()`. `config:changed` carries the section
  written (workspaces, installs or settings, "" for all) and `app/events.ts` refreshes what it feeds; `fs:changed`
  (`{path, deleted}`, from the open workspace's watcher) is read again by the editor's models and applied by the
  explorer.
- **Windows:** Wails's JS runtime acts only on the calling window, so showing or closing another window goes through
  `app.Service`'s window methods.
- **The frontend** is appearance and interaction only. `styles/themes.css` is the only palette: a block of tokens per
  `data-theme`, a dark and a light one per family, and the `@theme` / `--ui-*` bridge; the code's colors are the ten
  `--syn-<category>` tokens. Table filters live in UTable column state; fetch state in Pinia Colada.
