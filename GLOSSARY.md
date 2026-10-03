# Terminology

How to use this file: names here win over older code and comments. Rename in the
owning cleanup phase; do not invent a synonym. Go JSON tags match Vue. Wails
params stay `workspaceID`; Vue locals may be `workspaceId`.

This file defines what words mean. **[GAME-SYNTAX.md](GAME-SYNTAX.md) records
what the three games actually do**, measured against real installs with a
reproduction per claim — start there when a game update breaks something, and
before changing the schema stage or the entry detector.

## Guide, not Helper

The product pane beside the IDE editor is **Guide**. The SFC is `GuidePane.vue`.
The monaco-vscode custom view id is `pmt.guide`. Prefs are `pmt.guide.*` /
`ui.guide.*`. RPC is `WikiService.Guide`. Do not say Helper, HelperPane,
`pmt.helper`, `WikiGuide`, or `ide.helperWidth`.

## Three things people call “docs”

These are different pipelines. Never collapse them into one word.

| What | Canonical | Where | What it is |
|---|---|---|---|
| Community wiki | wiki, Guide | `package wiki` | Sanitized MediaWiki HTML. Guide is wiki-only. |
| Console dumps | dumps (script_docs, dump_data_types) | OS user-data `logs/` (CK3) or `docs/` (Vic3/EU5), data types under `logs/data_types` | **What the game declares about its own language**, not an overlay: effects, triggers, links, scope types, modifiers, data types. |
| Install prose | docs (`source.Info`) | Shipped `_*.info`, readmes, `*.md` **inside the game install** | The prose and the `<type>` a game writes beside its script; not the `docs/` folder the Vic3 and EU5 dumps land in. Never call this script_docs or wiki. Never name a field `GameInfo` (collides with `game.GameInfo`). |

Never persist a rendered hover string: a cache that held `"character; targets: culture"` as display text is the bug the type system was rebuilt to remove.

## Install, user data, workshop

| Tree | What |
|---|---|
| **Install** | Steam/Paradox game folder. Vanilla script, GUI, loc, docs. An install's cache is keyed by game, game version, app version, the install folder, the language and the dump files (`services/internal/lang/README.md`). |
| **User data** | `game.UserDataDir`: first existing of launcher `gameDataPath`; Win/mac `~/Documents/Paradox Interactive/<DocsFolderName>`; Linux `$XDG_DATA_HOME` or `~/.local/share/Paradox Interactive/<DocsFolderName>`; Proton `compatdata/<SteamAppID>/pfx/drive_c/users/steamuser/Documents/Paradox Interactive/<DocsFolderName>` (also Flatpak Steam). The dumps and the default `mod/` parent live here. |
| **Workshop / mods** | User-specified mod roots on the workspace. A workspace's index is RAM-only. |

## Script

Clausewitz/Jomini text (`.txt`, `.gui`, `.asset`, `.gfx`, `.font` and more: the format table in `lang/source`). One CST in `lang/jomini`. Loc YAML is `lang/loc`. No third GUI parser.

**Game-aware vs shared.** `internal/game` is the only package that knows the titles apart; everything per-game is a field on `GameInfo` rather than a `switch` on a game id. The shared language lives in `lang/jomini`, which is a leaf and must never import `game`. The test for membership is behavioural: a function belongs in `game` only if it acts differently on CK3, Victoria 3 and EU5. Taking a `gameID` parameter is not evidence.

## Modding practice

Load-order is **LIOS** (last in, only saved / last listed wins), with first-wins databases per game in the registry. CK3 overrides by path and by key; Victoria 3 and EU5 also write entry modes before a definition's name (`INJECT:`, `REPLACE:`, `TRY_INJECT:`, `TRY_REPLACE:`, `INJECT_OR_CREATE:`, `REPLACE_OR_CREATE:`). Patch = retarget a mod onto a new game version.

## Canonical names

| Concept | Canonical | Kill |
|---|---|---|
| Product pane | Guide, `GuidePane`, `pmt.guide`, `ui.guide.*` | Helper, HelperPane, `pmt.helper`, `ui.helper`, `ide.helperWidth`, `WikiGuide` |
| Guide RPC | `WikiService.Guide` | `SessionService.WikiGuide`; query key `wiki-guide` → `guide` |
| Events | `lang:ready`, `lang:scan-progress`, `lang:cache-updated`, `lang:calendar-changed`, `fs:changed`, `wiki:updated` | `docs:wiki-*` |
| Package / load | `package wiki`, `LoadGuides`, `guidesPath()` | `package docs`, `LoadDocs`, persist `docsPath()` |
| Dumps health | `LanguageHealth.dumpsFrom` (yours, shipped or none), `dumpsStale` | `docsPresent`, UI “dumps required” |
| Dump signature | `Command.Args` | prose in a usage field; a flat usage map |
| Dump command text | `Command.Blurb` | a flat doc map |
| Command input scope | `Command.Scopes` (`[]string`) | a rendered scopes string |
| Origin id on the wire | `"vanilla"` or mod uuid | `""` meaning vanilla |
| Origin display | `originName` | showing the uuid |
| Same origin field | JSON `origin` | `IdeRoot.originId` |
| Def database | `Def.Database`, JSON `database` | `Def.Kind` / `Def.Type`, JSON `kind` / `type` |
| Absolute path | `path` | `LocEntry.File`, override `file` when absolute |
| Display relative | `rel` | using `path` as the label |
| Live session | `LanguageHealth.ready` | `ModelStatus.live` |
| Query gate file | `useLiveEnabled.ts` | `useSessionQuery.ts` |
| Load-order rule | LIOS | **LISO** |
| Graph layout | dagre | AGENTS elkjs (do not add elkjs) |

## Engine terms

These are the terms of the language engine (`services/internal/lang/README.md`), and they win over older code, comments and documents. One word per thing.

| term | means | in a game file |
|---|---|---|
| install | one game folder at one version | `Crusader Kings III/game` |
| root | a folder that mirrors the game layout: the game folder, each DLC folder, each mod | `dlc/dlc001`, a mod's folder |
| origin | which root a file came from: `vanilla` or a mod's id | |
| database | all entries of one type: one per script folder of the install, named by the folder's path without `common/` | `traits`; `culture/cultures`; `events`; `gui.type` |
| definition | one entry of a database, with a name (`index.Def`) | `brave = { … }` |
| nested database | a database whose entries are written inside another database's entry (a registry `Nested` row) | a landed title inside a title; an activity type's `phases` |
| history database | a database whose registry row makes its entries the start state of other databases' entries; it defines none of its own | `history/titles`: `k_france = { 867.1.1 = { holder = 168673 } }`; EU5 `setup/start` |
| script-made name | a name a command creates while script runs, in a database no folder holds (`game.NameCommands`) | `save_scope_as = x`; `set_variable = { name = y }` |
| position | a place in a database's entry or in a command's arguments (`schema.Key`); keys collapse only by written form: `<number>`, `<date>`, `<prefix:>` | `trigger` inside an event; `<number>` inside `random_list` |
| block kind | what a block holds: fields, triggers or effects (`schema.Block`) | `trigger = { … }` is a triggers block; an event's own block is fields |
| slot | where a key is written: a fields block, a triggers or effects block, or a block nothing declares. The slot decides what the key is (`Schema.Slot`), never the word | a declared command's name in an effects block is the command; in a fields block it is a field |
| value | one written word, and the form it is written in: number, date, bool, text or word | `brave`, `character:186004`, `"gfx/x.dds"`, `1066.9.15` |
| reference | a word at a position whose declared value names a database, or a prefixed word whose prefix is a declared link; a word no source declares is no reference | `trait = brave`, `culture = "norse"`, `holder = 168673` |
| prefixed word | a word spelled `prefix:name` (`jomini.SplitPrefixed`) | `title:k_france`, `scope:target` |
| prefix | the word before the colon of a prefixed word | `character`, `title`, `scope`, `var` |
| scope type | the class of object a statement runs against | `character`, `landed_title`, `country` |
| link | a declared hop between scope types | `liege`, `primary_title`, `root` |
| command | an effect, trigger or link the dumps declare (`schema.Command`) | `add_gold`, `has_trait`, `if`, `every_vassal` |
| call | a key that is no command or link, naming an entry of a database whose registry row's `CalledIn` holds the block kind it is written in; resolved at query time | a scripted effect, a scripted trigger |
| dumps | everything the game declares about its language, from `script_docs` and `dump_data_types` | the `logs` and `docs` dumps |
| docs | the prose the install ships beside its script | `_traits.info`, `readme.txt`, `*.md` |
| claim | a fact a source states at a position or about a command, with who stated it (`schema.Said`); the first source in trust order (dumps, docs, wiki) to state it keeps it | `Required` optional, `Scope` character |
| declared / evidence | stated by a source (the dumps, the docs, the wiki, the registry) / written in vanilla's files. A name says which; only a claim of the dumps, docs or wiki lets a diagnostic accuse (`schema.Speaks`); the registry's classify | |
| evidence | what vanilla writes at a position (`schema.Seen`): how many of the parent's blocks hold it, and the forms and values written, with counts. Never a claim | `Seen.In`, `Seen.Values` |
| entry mode | a word before a definition's name that says how it combines with an earlier one | Victoria 3 and EU5 `REPLACE:name`, `INJECT:name` |
| data call | a `[…]` expression in GUI or loc | `[Character.GetName]` |

A **scope type** is not a database, and a database is not a scope type: `character` is what a statement runs against, `characters` is the database that defines them, and `Schema.Named` is the one place a word names a database.

## Command and call

A **command** is an effect, trigger or link the game's dumps declare: `Commands` / `Effects` / `Triggers` mean that **declared** API. A **call** is a key that is none of them, naming an entry of a database whose registry row says it is called there: a scripted effect, a scripted trigger, a script value. Never reuse the one word for the other.

## Declared and evidence

Two sources of fact, and a name must say which one it came from. Getting this wrong is what produced hovers showing the wrong kind.

| Word | Source |
|---|---|
| **Declared** | A source states it: the dumps, the docs, the wiki and the registry's rows. Read as written, each claim keeping who stated it. |
| **Evidence** | Written in vanilla's own files and counted by the schema stage: which positions exist, which keys sit under each, which forms and values are written. Never a claim, never hand-written. |

Only a declared claim that is fresh and that no evidence contradicts lets a diagnostic accuse (`schema.Speaks`); evidence offers a completion, feeds a hover and keeps a declared claim from accusing where vanilla writes otherwise. Do not say "learned", "voted" or "inferred" for either.

**Keep as different words:** `lsp.Position.character` vs a card's `col`. Disk `DocsFolderName` / `ScriptDocsDirs`.

## Code map

| Name | Lives in |
|---|---|
| `GameInfo` (with `Folders`, the registry's per-folder rows: how entries are named, wrappers, history, scripted keywords; `FileTypes`, `Nested`, `MapTable`; and `EntryModes`, the six entry-mode words of Victoria 3 and EU5 with what each does, in the order the game processes them), `UserDataDir`, `DumpDirs`, `ReadGameVersion`, `FirstWinsAt` | `services/internal/game` — **the only game-aware package** |
| The CST, the lexer, the name and literal shapes, encodings; `SplitPrefixed`, `Constant`, `Interpolations` (the spellings inside one word); `Result.Attach` (comments onto statements) | `services/internal/lang/jomini` — shared Jomini syntax, no game id |
| `Roots`, `Inventory`, `Classify`, `File`, `Walk`, `ReadImageInfo`: every file of an install, a mod or a dump folder, its format, an image's header | `services/internal/lang/source` — one format table for every game |
| Loc files: entries, interpolations, language | `services/internal/lang/loc` |
| `Schema` with `Database`, `Key` (a position), `Command`, `Said` (a claim), `Seen` (vanilla's evidence); `Build`, `Input`; `Speaks`, the one rule for when a claim may accuse; `Slot`, `Link`, `Named`, `Flagged`; `Entries` and `Entry`, the entry detector; the dump reader, the doc normalizer | `services/internal/lang/schema` — the schema stage: what each source states, in trust order |
| `Load`, `Name`, `Safe`; the generator `gen` | `services/internal/lang/shipped` — the backup schemas, the dump part only, for a user missing a dump file |
| `Index` with `Def`, `Ref`, `Loc`, `Records`; `Walk` and its `Context`, the only place that decides what a written word is; `Scan`, `ScanMods`, `Reindex`, `BuildSchema`; `SaveInstall`, `LoadInstall`, `DropInstall`, `CachedAt`; `Lookup`, `Resolve`, `Winner` | `services/internal/lang/index` — the index stage: what a scan found and where, load order, and the install's cache. It infers nothing |
| `Session` with `New`, `Defs`, `Winner`, `Loc`, `LocKeys`, `Where` (`Place`), `Text`, `Parsed`, `Did*`, `Changed`, `Watch`; `Watcher`; `Fires` (`Fire`: what a definition fires, through scripted effects) | `services/internal/session` — one workspace open in the editor, and its lock |
| `Hover` (`Inspect`), `SignatureHelp`, `Complete` (`Item`, `Resolve`), `Definition`, `References`, `DocumentSymbols`, `WorkspaceSymbols`, `Diagnose` (`Codes`), `Format`, `Folding`, `CodeActions`, `InlayHints` | `services/internal/lsp` — the editor's requests, each from one walk of the open file; payloads carry display data |
| `EventGraphService` (`GetEventGraph`: `Params`, `EventGraph`, `Node`, `Edge`; `GetEventDetail`: `Detail`, `Section`, `Option`; `SearchRoots`: `Roots`, `DatabaseCount`), `HealthService` (`GetHealth`: `HealthParams`, `Health`, `Contest`, `Site`, `Dependency`, `Dangling`, `Language`, `LocRow`, `Problem`; `GetLines`: `SourceAt`; `LookupLoc`: `Lookup`, `LocEntry`) | `services/` — a page's question answered from the index's records; every number in a payload is the page's |
| `ConfigService` (`config.json`, the settings, version, updates, `ResetData`), `SessionService` (`EnsureSession`, `RebuildInstallSemantics`, `GetLanguageHealth`), `IdeService` (the editor's requests), `ReleaseService`, `WorkspaceService`, the `lang:*` events | `services/` — the Wails layer, services only |
| `AppDir`, `CacheDir`, `Write`, `CopyFile`; `ReadListingFields`, `WriteListingFields`, `FindThumbnail`, `CopyThumbnail`, `WriteNewMod` | `services/internal/local` — this machine's files: the app's own, a mod's Paradox files |
| `Library`, `Publish`, `Serve` (a publish run as a second copy of the app); `MarkdownToBBCode`, `BBCodeToMarkdown`; `EnsureDescriptions`, `DescriptionFiles`; `ScanPreviews`, `AddPreview`, `AddVideo`; `CopyMod` | `services/internal/steam` — everything the Steam Workshop reads |
| Wiki persist / sanitize / Guide payload | `services/internal/wiki` |
| Guide UI | `GuidePane.vue`, a Nuxt column beside the workbench (`pmt.guide`) |
| Editor client | `frontend/src/ide/editorCore.ts` (payload → display, no framework) + `ide/languageClient.ts` (the one adapter to the workbench) |
| Graph layout | frontend `useEventGraph` + `@dagrejs/dagre` |

## Diagnostic codes

Every check PMT reports carries a code. The code is what `# pmt:ignore <code>`
suppresses, what the settings menu toggles, and what the sweep counts, so it is
part of the vocabulary rather than an implementation detail.

Vanilla files are never diagnosed — the scan reads them for evidence, not for
lint — so every code below describes something in a mod.

| Code | Says |
|---|---|
| `unresolved-ref` | a declared reference names an entry nothing defines anywhere, names created by commands included (`culture:nonexistent`) |
| `unknown-event` | an event id that no event defines, at a position the walk knows |
| `wrong-value-type` | a value of another type than the position's declared type |
| `wrong-declared-value` | a word outside the position's declared word list |
| `wrong-scope` | a command written where the known scope is not one it runs in, judged on the scopes the dumps declare for it, or a variable read in a scope that stores none; an unknown scope, and a command and scope vanilla writes together (`Index.Scoped`), are quiet |
| `missing-key` | a key a declared source calls required, left out of an entry. **Off unless the user turns it on** |
| `duplicate-definition` | a name the workspace defines twice where the entry modes say neither replaces nor injects, the earlier one in load order named |
| `inject-without-target` | an `INJECT:` or `REPLACE:` entry with nothing loaded before it |
| `missing-bom` | a localization file without the UTF-8 byte order mark the games require |
| `loc-header-mismatch` | a localization file whose `l_<lang>:` header names another language than its name does |
| `wrong-localization-folder` | a localization folder spelled `localisation`, which the game never reads |
| `missing-file` | a path that exists nowhere in the install or the mods, where vanilla's paths at the position all exist. **Off unless the user turns it on** |

Parse errors carry their own codes from the parser (`unclosed-brace`,
`stray-close`, `unterminated-string`, `missing-value`, and a localization
file's prefixed `loc-`). They are not in the list above and cannot be turned
off: a file that does not parse is wrong.

**Evidence never accuses.** The rule is `schema.Speaks` (`.claude/plans/engine.md` §4.3): every code
above except the three localization-format ones accuses only on a claim a
declared source states (dumps, docs or wiki), fresh, that no vanilla evidence
contradicts; a docs or wiki claim also needs vanilla to write the position.
Absence is never a claim. With no dumps at all nothing is declared, so the
checks that need a declaration correctly go quiet rather than guess.
