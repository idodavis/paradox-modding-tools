# Agent notes

Humans: the user guide is **https://idodavis.github.io/paradox-modding-tools/** (source `docs/`). Do not treat this file
as the manual. The backend's map is [services/README.md](services/README.md) and each package under
`services/internal/` has a README with its design; the frontend's shape and conventions are
[frontend/src/README.md](frontend/src/README.md); the terms are [GLOSSARY.md](GLOSSARY.md); what each game's script
does, measured, is [GAME-SYNTAX.md](GAME-SYNTAX.md).

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
- **Names.** One identifier per thing everywhere (GLOSSARY.md). A parameter, a field or a local is named for its type or
  for what it holds, never for how a call reads as a sentence: no preposition (`in`, `at`, `from`), no sentence word
  (`found`, `written`, `said`, `made`, `left`, `taken`), no letter outside a loop counter. An exported field says what
  it holds (`Count`, `Target`), not which way it points. A receiver is its type's word; a local holding an `*index.Index`
  is `index`. No new identifier is built on `read`, `name`, `form` or `word`. American spelling (`color`).
  (golangci-lint `revive` for receivers and exported names, `misspell` for spelling; the rest review)
- **Comments.** Every exported name and every function has a doc comment: a sentence with a verb that says what it does,
  and why where there is a reason. No history, plan, stage, ruling, date or person. A comment line fits in 120
  columns, a tab counting 2. A comment that restates the next line is deleted. (`revive`'s `exported` for the first;
  the rest review)
- **Files.** The header comment, the imports, the types, the constants and tables, the file's main function, then its
  helpers in the order they are called. A file is cut by topic, never by sub-topic. (review)
- **Tests.** One test file per package, `<package>_test.go`; a test is named for what it tests; none is added unasked.
  A real-install gate lives in the package's test file behind its environment variable. (review)
- **Replacing a reader or a merge.** List every rule the old one applies and carry each over or name it dropped; the
  stop is no real claim lost against the three installs, never a zero diff. (review)
- **Commits.** One commit per piece of work, made when the whole of it passes; never one after each step, which is
  checked and left uncommitted. `task check` passes before every commit; the message says what changed. (review)

## Product / stack

- Terminology: [GLOSSARY.md](GLOSSARY.md). Measured per-game syntax facts, with reproductions: [GAME-SYNTAX.md](GAME-SYNTAX.md) — read it before changing the schema stage or the entry detector.
- Paradox Modding Tools is a Wails **v3.0.0-beta.28** desktop app (Go **1.27.1** backend, Vue 3 + Nuxt UI + Pinia + Colada frontend) for Paradox modders, licensed under GPL-3.0-or-later.
- **Supported OS:** Windows and Linux (traditional desktop distros, GTK4 + WebKitGTK 6). **macOS is not supported** — do not add Mac downloads, Mac user docs, or a macOS issue-template option. Leave the Wails `darwin` Taskfile in the tree; do not advertise it.
- Product is workspace-centric: Library, IDE, Event Graph, **Health** (FIOS/LIOS + depends/dangling + loc coverage), Patch Notes (wiki notes **only**), Publish (Steam + Paradox Mods copy, and a compare with Steam), Settings. CK3, Vic3, EU5 (partial). No Merge (mod merging), Inventory, or Patcher/Impact services; compares are views (`CompareFiles`, `GitDiff`), and git's merge is GitService's.
- GUI preview is a future feature; `.gui` is the same Jomini CST as `.txt`, not a third parser.
- Releases: per-platform **zips** on GitHub (`paradox-modding-tools-{windows-amd64,linux-amd64}.zip` + sibling `SHA256SUMS`; Windows on ARM runs the amd64 zip under emulation). In-app updater is `github.New` (`ChecksumAsset: "SHA256SUMS"`) — do not set `Prerelease: true`. Workshop publish runs in a second copy of the app itself (`steam-publish`), which loads the game's own copy of Steam's library where `GameInfo.SteamLibrary` says the install keeps it; PMT ships nothing of Steam's.
- Local develop/build: Taskfile (`task dev`, `task build`) with `wails3` v3.0.0-beta.28. Windows `CGO_ENABLED=0`. Linux `CGO_ENABLED=1` (WebKit). `task test:race` (`go test -short -race ./...`) works on Windows.

## Commands

- `task dev` / `WAILS_VITE_PORT=9246 task dev` — `wails3 dev`.
- `task build VERSION=…` — platform Taskfile → `bin/`.
- `task common:generate:bindings` after Go DTO/service changes. Never hand-edit `frontend/bindings/**`.
- `task check` — everything a commit must pass, Go and frontend.
- Docs-only `docs/**` pushes deploy Pages (Hugo in `docs/`, `.github/workflows/pages.yml`). Preview: `hugo server` in `docs/` (plain Hugo, not Extended). Non-docs `master` pushes cut a full release (`.github/workflows/release.yml`); first floor **v0.4.0**, then patch bumps.

## Learned Workspace Facts

- **The backend** is `services/*.go` (Wails services only) over the packages of `services/internal/`, in layers a package imports only from above; the map, the services, the events and where to change what are [services/README.md](services/README.md), each package's design its own README. The one walker is `schema.WalkFile`; `lsp`, `pages` and the services answer from the same index.
- **Thin language engine:** the only embedded game data is `schema/shipped.go`'s backup schemas (what the dumps declare, never dumps or vanilla words), used when a dump file is missing. The language rules and each game's file layout live only in `game/lang-rules.go`, each game's identity and install facts in `game/game.go`; the shared Jomini syntax is in `jomini`, which never imports `game`. No file PMT writes carries a format version: the app is unreleased, so a changed struct means Rebuild or Reset data, never a migration.
- **Test layout (enforced by hand, not by a test):** each package has one test file, `<pkg>_test.go`, and a gate over a real install lives in it behind its environment variable, which `task test:gate` turns on together. The sweep (`TestSweep` in `services_test.go`) runs only behind `PMT_TEST_SWEEP` (`task test:sweep`). A test is named for what it tests.
- **App config** is JSON (`config.json`, no format version), held by `ConfigFile` (`OpenConfig`, `appservice.go`) for every service through `read` and `mutate`; a missing file is a first run, any other read failure means empty and read-only so a transient failure never overwrites the real file. Types: `GameInstall` (`VersionDetected`), `Workspace` (`DefaultLocLang`, `HideExplorerBinaries`; each mod's `IsBroken` is derived from disk), a settings map. No SQLite, no DbService.
- **Bindings:** every bound field has a camelCase JSON tag. Frontend aliases: `@services` → `frontend/bindings/paradox-modding-tools/services`; the payload models from `@services/models` and `@services/internal/<package>/models`; the services from `@services/<name>service`. A workspace switch is localStorage `pmt.workspace.activeId` + `Open`.
- **The open workspace:** `workspace.Slot` holds one; `Open` closes the one open before it; closing is not a Wails RPC. `DeleteGameInstall` refuses while its install is in use (it does not close); `DeleteWorkspace` removes the record only, mod folders on disk stay. A workspace's mod whose folder is missing is marked broken.
- **Events** are declared by the service that sends them and listed in [services/README.md](services/README.md). `config:changed` carries the section written (workspaces, installs or settings, "" for all) and `app/events.ts` refreshes what it feeds; `fs:changed` (`{path, deleted}`, from the open workspace's file watcher) is read again by the editor's models and applied by the explorer.
- Tool pages' reads are enabled by the workspace store's `isOpen`: `WorkspaceState.open` from `State`, not merely `defs > 0`.
- **The IDE** (`pages/ide/`, `editor/`, `stores/editor.ts`) edits on Monaco (`monaco-editor`, its editor features without its languages). `lib/monaco.ts` sets Monaco up (the Paradox languages as the Monarch grammars of `lib/grammars.ts`, one theme per page theme read from the tokens, since Monaco takes no CSS variables); because Monaco makes its services once, with the first editor, it creates a throwaway one first, carrying the model loader a peek needs. `lib/codeView.ts` draws what pages show without an editor: read-only code as the page's own spans colored by `--syn-*`, and a compare of two texts. `editor/models.ts` holds one model per file, shared by every group that shows it, and shows problems as markers; `editor/providers.ts` registers the language providers over IdeService, for `file:` models only, never a compare's text models; the hover card stays ours (`ui/HoverCard.vue`), Monaco's is off. `editor/columns.ts` converts columns: UTF-16 exists only there, everywhere else is UTF-8 byte offsets. A `.gui` file has a Visual view (`draw/`, `DrawService`): a list of elements Go resolves, each with its layout words as written, which `draw/widgets.ts` nests as HTML with the CSS each word means and the browser lays out (Go measures nothing); nothing about the game's state is simulated or asked, a binding is drawn as written, and the panel hides a widget for the session (the draw store's `hidden`, never written anywhere); the selected widget is marked in the code that writes it (`editor.marked`). The explorer is `@pierre/trees` (`pages/ide/ExplorerTree.vue`), its git marks through the tree's own `gitStatus`. Groups sit in rows and columns (`stores/editor.ts`' `Layout`), kept per workspace in localStorage (`pmt.ide.layouts`), tabs included: the app's config holds no tabs. The explorer's single click opens a file as the preview tab (`editor.preview`, `open(path, position, true)`), which the next single click replaces; `pin` keeps it on a double click, an edit or any other opening.
- **Windows:** the main window (named `main`, the only one with the top bar, whose close quits the app; the palette is in every window) and windows of one thing named in their URL (`?window=page:<route>|editor:<n>|game|files:<folder>|panel`). A file is open in one window at a time, else in the window whose explorer shows its folder (`WindowState.Roots`); a navigation to a popped page goes to its window; a tab dragged between windows rides `application/x-pmt-tab` and is asked of its holder (`window:tab-requested`); a tab handed on carries its unsaved text, and the giver never calls `DidClose`; closing another window hands its tabs back first (WindowService holds its first close). JS's runtime acts only on the calling window (Wails sets a call's window to its caller's), so showing or closing another window is WindowService's; the protocol table is at the head of `stores/windows.ts`.
- **Source control** (`pages/ide/SourceControlView.vue`, `BranchMenu.vue`, `GitGraph.vue`, `stores/git.ts`) runs GitService on the repositories of the workspace's mods, following the active file's; its graph is the current branch's and its upstream's commits, a commit opening to its files. A file's diff is a tab of its own, `git:changes|staged|conflict:<path>` or `git:commit@<hash>:<path>` (`editor/tabs.ts`' `diffTab` and `tabOf`; the editor store's `activeFile` is a diff tab's file), drawn by `pages/ide/GitDiff.vue` with the file's live model as its working side and a block staged from the gutter through `StageText`; merge conflicts are resolved in the editor (`editor/conflicts.ts`).
- **Event Graph** (`pages/graph/`, `composables/useEventGraph.ts`): Go returns nodes and edges with no coordinates; `useEventGraph` lays them out left to right with `dagre` and Vue Flow draws them. The graph draws events, on_actions and the definitions that fire them; a scripted effect between two is its edges' label (`Edge.Via`: the chain of scripted effects entered), or a node of its own when Filters › Kinds' "scripted effect" is on (the default). The detail's Fires and Fired by follow the same rule (`Session.Fires`, `Session.FiredBy`). Vue templates the payload.
- **Workspace Health** (`pages/health/`, `composables/useHealth.ts`): cards (contests, overrides, dependencies — depends on or affected by, from comparing a winning definition's statement with the one it hides — and the problems in the mods: Dangling Refs one row per name, Localization Issues and Script Issues one row per place to fix, each typed and colored by `TYPES` in `useHealth.ts`) pick the table; each column head sorts and, where its values are few, filters; a row opens its sites in the pane.
- **Frontend husk:** appearance + interaction only. `styles/themes.css` is the only palette: a block of tokens per `data-theme`, a dark and a light one per family (pmt, catppuccin, one, pierre, ck3, eu5, vic3; `stores/settings.ts`), and the `@theme` / `--ui-*` bridge. The settings store puts the theme on `<html>`; the code's colors are the `--syn-*` tokens through `lib/monaco.ts`. Table filters live in UTable column state; fetch state in Pinia Colada `useQuery`/`useMutation`. No path-stripping helpers; payloads carry `rel`/`origin`, and the workspace store names an origin (`originName`).
- On Windows, the Jomini parser (`jomini`) is pure Go (`CGO_ENABLED=0`). Linux stays `CGO_ENABLED=1` for Wails WebKit.
- **Measure sprawl** (repo root, bash): `find services -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1`, same for `*_test.go`, and `find frontend/src -name '*.vue' -o -name '*.ts' -o -name '*.css' | xargs wc -l | tail -1`. Success of a de-sprawl pass is fewer files and lines, not extra layers.
