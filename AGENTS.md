# Agent notes

The backend's map is [services/README.md](services/README.md), the frontend's is
[frontend/src/README.md](frontend/src/README.md), and each package's README holds its design and its terms.

## Learned User Preferences

- Prefer the smallest clear implementation: cut duplication, dead-end helpers, and over-engineering; do not add code for backwards compatibility.
- Never hand-edit `frontend/bindings/**`; regenerate with `task common:generate:bindings` after Go DTO/service changes.
- Put display fields (relative paths, origin) on Go payloads. `Session.PlaceOf` already gives origin and rel; use `filepath.Rel` for vanilla/install paths. Do not write frontend path-stripping helpers.
- Render inspector/table fields from the payload. Do not re-derive view-models for values Go already computed.
- Prefer stdlib (`filepath.Rel`, Node `node:path`) over custom utilities when client-side path math is unavoidable.
- Change only what was explicitly requested; avoid unrelated refactors or drive-by edits.
- Keep plans short, actionable, and free of stale wording after iterative edits; ask clarifying questions when requirements are ambiguous.
- Prefer game-agnostic shared parser/semantics logic; keep game-specific facts and outputs under each game's own files/dirs.
- Prefer a JSON semantic model when it can hold the needed baseline structure; avoid SQLite unless relationships or volume truly require it.
- Put performance-sensitive work in Go behind the Wails 3 bridge when that stays maintainable; keep UI logic in the frontend otherwise.
- Prefer "patch" (not "update"/"campaign") for mod version retargeting, and prefer clear product names over ambiguous jargon.
- Prefer Nuxt UI component patterns (`:items` and similar) over ad-hoc `v-if`/`v-model` control flow where Nuxt UI fits.
- Prefer denser layouts where editor/diff/merge content dominates the viewport and path/config chrome stays compact.
- When cleaning code, keep useful comments and user-facing behavior; reduce complexity without expanding scope.
- Do not re-wrap, pack, or densify code to move a line-count gate. A reduction counts only when a representation, pass, helper, or file is deleted. `wc` is a consequence metric, never a reason to change formatting.

## Standing orders

The owner has given each of these many times. They hold for every task, plan and file; none is open to interpretation.

- **The guiding words:** consolidate, simplify, idiomatic, reduce, efficient, readable, DRY. Architecture over arbitrary
  fixes, helpers, statements, loops and checks.
- **Cleanup is a rewrite.** A file is written whole from a written target, then the old one is deleted. Never a pile of
  one-line patches, never a shim or adapter, never a representation half left behind. Only the user-facing features are
  sacred; no code is. A cleanup never ends with more files, functions or lines than it started with: a file is cut by
  topic, never split into composables or components with one caller to look shorter.
- **DRY against the engine.** Before writing a function, find the one the app has (`lsp`, `schema`, `index`, `workspace`,
  `loc`, `jomini`, `pages`; the page's `lib/`, `editor/`, `stores/`) and call it; where it needs a parameter, it gets one
  in its one place. No parallel helper, no recomputing what another package computed.
- **A near-duplicate is a duplicate.** Two pieces of logic 60–70% alike are one function both callers use, the
  difference a parameter or a row. A thing is computed once, written once, explained once; a duplicate check that
  passes only exact matches has not been run.
- **No carrier types.** Data that exists is used as it is, extended by a field where it lacks one. Never a second shape
  of the same data made to pass it along and shape it back (a DTO beside the payload, a record beside the engine's).
- **Easy to digest** outranks every count: simple, straightforward, coherent, one whole. Not smart, not clever, no magic.
- **Services are shells.** A memo is the internal package's type held by the service (as `HealthService` holds
  `pages.Findings`); logic and state live in the internal package named for them.
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

- Paradox Modding Tools is a Wails desktop app at the versions `go.mod` names (a Go backend, a Vue 3 + Nuxt UI + Pinia + Colada frontend) for Paradox modders, licensed under GPL-3.0-or-later.
- **Supported OS:** Windows and Linux (traditional desktop distros, GTK4 + WebKitGTK 6). **macOS is not supported** — do not add Mac downloads, Mac user docs, or a macOS issue-template option. Leave the Wails `darwin` Taskfile in the tree; do not advertise it.
- Product is workspace-centric: Library, IDE, Event Graph, **Health** (FIOS/LIOS + depends/dangling + loc coverage), Patch Notes (wiki notes **only**), Publish (Steam + Paradox Mods copy, and a compare with Steam), Settings. CK3, Vic3, EU5 (partial). No Merge (mod merging), Inventory, or Patcher/Impact services; compares are views (`CompareFiles`, `GitDiff`), and git's merge is GitService's.
- GUI preview is a future feature; `.gui` is the same Jomini CST as `.txt`, not a third parser.
- Releases: per-platform **zips** on GitHub (`paradox-modding-tools-{windows-amd64,linux-amd64}.zip` + sibling `SHA256SUMS`; Windows on ARM runs the amd64 zip under emulation). In-app updater is `github.New` (`ChecksumAsset: "SHA256SUMS"`) — do not set `Prerelease: true`. Workshop publish runs in a second copy of the app itself (`steam-publish`), which loads the game's own copy of Steam's library where `Game.SteamLibrary` says the install keeps it; PMT ships nothing of Steam's.
- Local develop/build: Taskfile (`task dev`, `task build`) with `wails3` at the version `go.mod` names. Windows `CGO_ENABLED=0`. Linux `CGO_ENABLED=1` (WebKit). `task test:race` (`go test -short -race ./...`) works on Windows.

## Commands

- `task dev` / `WAILS_VITE_PORT=9246 task dev` — `wails3 dev`.
- `task build VERSION=…` — platform Taskfile → `bin/`.
- `task common:generate:bindings` after Go DTO/service changes. Never hand-edit `frontend/bindings/**`.
- `task check` — everything a commit must pass, Go and frontend.
- Every `main` push that changes more than Markdown cuts a release (`.github/workflows/release.yml`); first floor
  **v0.4.0**, then patch bumps.

## Learned Workspace Facts

- **The backend** is `services/*.go` (Wails services only) over the packages of `services/internal/`, in layers a package imports only from above; the map, the services, the events and where to change what are [services/README.md](services/README.md), each package's design its own README. The one walker is `schema.WalkFile`; `lsp`, `pages` and the services answer from the same index.
- **Thin language engine:** the only embedded game data is `schema/shipped.go`'s backup schemas (what the dumps declare, never dumps or vanilla words), used when a dump file is missing. The language rules and each game's file layout live only in `game/` as tables (`language.go` the language's own words and the walker's, `databases.go` the databases and file types, `coat.go` and `gui.go` the visual editors'), each game's identity in `game/game.go` and its install facts in `game/install.go`; the shared Jomini syntax is in `jomini`, which never imports `game`. No file PMT writes carries a format version: the app is unreleased, so a changed struct means Rebuild or Reset data, never a migration.
- **Test layout (enforced by hand, not by a test):** each package has one test file, `<pkg>_test.go`, and a gate over a real install lives in it behind its environment variable, which `task test:gate` turns on together. The sweep (`TestSweep` in `services_test.go`) runs only behind `PMT_TEST_SWEEP` (`task test:sweep`). A test is named for what it tests.
- **App config** is JSON (`config.json`, no format version), held by `ConfigFile` (`OpenConfig`, `appservice.go`) for every service through `read` and `mutate`; a missing file is a first run, any other read failure means empty and read-only so a transient failure never overwrites the real file. Types: `GameInstall` (`VersionDetected`), `Workspace` (`DefaultLocLang`, `HideExplorerBinaries`; each mod's `IsBroken` is derived from disk), a settings map. No SQLite, no DbService.
- **Bindings:** every bound field has a camelCase JSON tag. Frontend aliases: `@services` → `frontend/bindings/paradox-modding-tools/services`; the payload models from `@services/models` and `@services/internal/<package>/models`; the services from `@services/<name>service`. A workspace switch is localStorage `pmt.workspace.activeId` + `Open`.
- **The open workspace:** `workspace.Slot` holds one; `Open` closes the one open before it; closing is not a Wails RPC. `DeleteGameInstall` refuses while its install is in use (it does not close); `DeleteWorkspace` removes the record only, mod folders on disk stay. A workspace's mod whose folder is missing is marked broken.
- **Events** are declared by the service that sends them and listed in [services/README.md](services/README.md). `config:changed` carries the section written (workspaces, installs or settings, "" for all) and `app/events.ts` refreshes what it feeds; `fs:changed` (`{path, deleted}`, from the open workspace's file watcher) is read again by the editor's models and applied by the explorer.
- Tool pages' reads are enabled by the workspace store's `isOpen`: `WorkspaceState.open` from `State`, not merely `defs > 0`.
- **The IDE** (`pages/ide/`, `editor/`, `stores/editor.ts`) edits on Monaco (`monaco-editor`, its editor features without its languages). `lib/monaco.ts` sets Monaco up (the Paradox languages as the Monarch grammars of `lib/grammars.ts`, which color code by its shape, and the engine's tokens over them for the keys, values and names only its walk knows, through `IdeService.Tokens`, and `IdeService.TextTokens` for a version of a file a diff shows (`lsp/tokens.go`, `editor/models.ts`' `versionModel`); one theme per page theme read from the tokens, since Monaco takes no CSS variables); because Monaco makes its services once, with the first editor, it creates a throwaway one first, carrying the model loader a peek needs. `lib/codeView.ts` draws what pages show without an editor: read-only code as the page's own spans colored by `--syn-*`, and a compare of two texts. `editor/models.ts` holds one model per file, shared by every group that shows it, and shows problems as markers; `editor/providers.ts` registers the language providers over IdeService, for `file:` models only, never a compare's text models; the hover card stays ours (`ui/HoverCard.vue`), Monaco's is off. `editor/columns.ts` converts columns: UTF-16 exists only there, everywhere else is UTF-8 byte offsets. A `.gui` file and a coat of arms file have a Visual view (`draw/`, `DrawService`): for a GUI entry a list of widgets Go resolves, each with its type's row (`game/gui.go`'s `GUITypes`: how it holds its children, clips, fills, centers text, drops hidden children) and every property as written, with the widgets the game puts the window in (`List.Holders`: EU5's lateral view layer, Vic3's `ingame_hud`); `draw/picture.ts` builds one div per widget from two tables (`HOLDS`, `PROPERTIES`: what a holding and a property mean in CSS, each row sourced to the wiki or marked observed) and the browser lays it out, each widget's rect read back for the marks, the panel and the code (Go measures nothing, the page computes no layout), on a floor that is a `@panzoom/panzoom` camera (`draw/Viewport.vue`: drag to pan, Ctrl+wheel to zoom, Fit); nothing about the game's state is simulated or asked, a binding in a text is drawn `[…]` and a texture binding as the file the scan finds (`texture.go`'s `pictureOf`), a widget shown on a condition is drawn in one state of its window (the toolbar's States: each group of conditions at its first value unless another is picked, a column's pages after its first and the side windows closed), and the panel hides a widget for the session (the draw store's record per file, never written anywhere); beside the code (Split), `draw/CodeLink.vue` marks the picture in the code that writes it (the selected widget's lines, the hovered one's, an eye and the drawn size on each widget's first line, what was not found squiggled) and the pointer on a widget's code outlines it in the picture; the caret in another entry draws it, and a selection made anywhere scrolls the code to it. A coat Go resolves into layers on its field (`draw.Coat`), which `draw/coat.ts` draws and colors as its shaders do and the Layers panel (`draw/CoatLayers.vue`) edits through the file's model, a drop moving a block in the file and Ctrl Z and Ctrl Y stepping the file's history; the sidebar's Entries tab (`draw/FileEntries.vue`) lists what the active file draws, a coat with its picture. The explorer is `@pierre/trees` (`pages/ide/ExplorerTree.vue`), its git marks through the tree's own `gitStatus`. Groups sit in rows and columns (`stores/editor.ts`' `Layout`), kept per workspace in localStorage (`pmt.ide.layouts`), tabs included: the app's config holds no tabs. The explorer's single click opens a file as the preview tab (`editor.preview`, `open(path, position, true)`), which the next single click replaces; `pin` keeps it on a double click, an edit or any other opening.
- **Windows:** the main window (named `main`, the only one with the top bar, whose close quits the app; the palette is in every window) and windows of one thing named in their URL (`?window=page:<route>|editor:<n>|game|files:<folder>|panel`). A file is open in one window at a time, else in the window whose explorer shows its folder (`WindowState.Roots`); a navigation to a popped page goes to its window; a tab dragged between windows rides `application/x-pmt-tab` and is asked of its holder (`window:tab-requested`); a tab handed on carries its unsaved text, and the giver never calls `DidClose`; closing another window hands its tabs back first (WindowService holds its first close). JS's runtime acts only on the calling window (Wails sets a call's window to its caller's), so showing or closing another window is WindowService's; the protocol table is at the head of `stores/windows.ts`.
- **Source control** (`pages/ide/SourceControlView.vue`, `BranchMenu.vue`, `GitGraph.vue`, `stores/git.ts`) runs GitService on the repositories of the workspace's mods, following the active file's; its graph is the current branch's and its upstream's commits, a commit opening to its files. A file's diff is a tab of its own, `git:changes|staged|conflict:<path>` or `git:commit@<hash>:<path>` (`editor/tabs.ts`' `diffTab` and `tabOf`; the editor store's `activeFile` is a diff tab's file), drawn by `pages/ide/GitDiff.vue` with the file's live model as its working side and a block staged from the gutter through `StageText`; merge conflicts are resolved in the editor (`editor/conflicts.ts`).
- **Event Graph** (`pages/graph/`, `composables/useEventGraph.ts`): Go returns nodes and edges with no coordinates; `useEventGraph` lays them out left to right with `dagre` and Vue Flow draws them. The graph draws events, on_actions and the definitions that fire them; a scripted effect between two is its edges' label (`Edge.Via`: the chain of scripted effects entered), or a node of its own when Filters › Kinds' "scripted effect" is on (the default). The detail's Fires and Fired by follow the same rule (`Session.Fires`, `Session.FiredBy`). Vue templates the payload.
- **Workspace Health** (`pages/health/`, `composables/useHealth.ts`): cards (contests, overrides, dependencies — depends on or affected by, from comparing a winning definition's statement with the one it hides — and the problems in the mods: Dangling Refs one row per name, Localization Issues and Script Issues one row per place to fix, each typed and colored by `TYPES` in `useHealth.ts`) pick the table; each column head sorts and, where its values are few, filters; a row opens its sites in the pane.
- **Frontend husk:** appearance + interaction only. `styles/themes.css` is the only palette: a block of tokens per `data-theme`, a dark and a light one per family (pmt, catppuccin, one, pierre, ck3, eu5, vic3; `stores/settings.ts`), and the `@theme` / `--ui-*` bridge. The settings store puts the theme on `<html>`; the code's colors are the ten `--syn-<category>` tokens per theme through `lib/monaco.ts`. Table filters live in UTable column state; fetch state in Pinia Colada `useQuery`/`useMutation`. No path-stripping helpers; payloads carry `rel`/`origin`, and the workspace store names an origin (`originName`).
- On Windows, the Jomini parser (`jomini`) is pure Go (`CGO_ENABLED=0`). Linux stays `CGO_ENABLED=1` for Wails WebKit.
- **Measure sprawl** (repo root, bash): `find services -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1`, same for `*_test.go`, and `find frontend/src -name '*.vue' -o -name '*.ts' -o -name '*.css' | xargs wc -l | tail -1`. Success of a de-sprawl pass is fewer files and lines, not extra layers.
