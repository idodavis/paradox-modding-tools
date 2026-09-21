# Terminology

How to use this file: names here win over older code and comments. Rename in the
owning cleanup phase; do not invent a synonym. Go JSON tags match Vue. Wails
params stay `workspaceID`; Vue locals may be `workspaceId`.

This file defines what words mean. **[GAME-SYNTAX.md](GAME-SYNTAX.md) records
what the three games actually do**, measured against real installs with a
reproduction per claim — start there when a game update breaks something, and
before changing any inference pass.

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
| Console dumps | vocabulary (script_docs, dump_data_types) | OS user-data `logs/` (CK3) or `docs/` (Vic3/EU5), data types under `logs/data_types` | **What the game declares about its own language**, not an overlay: effects, triggers, links, scope types, modifiers, data types. |
| Install prose | game info | Shipped `_*.info`, readmes, `*.md` **inside the game install** | The prose and the `<type>` a game writes beside its script. Never call this script_docs or wiki. Never name a field `GameInfo` (collides with `game.GameInfo`). |

Never persist a rendered hover string: a cache that held `"character; targets: culture"` as display text is the bug the type system was rebuilt to remove.

## Install, user data, workshop

| Tree | What |
|---|---|
| **Install** | Steam/Paradox game folder. Vanilla script, GUI, loc, game info. An install's cache is keyed by install id + version. |
| **User data** | `game.UserDataDir`: first existing of launcher `gameDataPath`; Win/mac `~/Documents/Paradox Interactive/<DocsFolderName>`; Linux `$XDG_DATA_HOME` or `~/.local/share/Paradox Interactive/<DocsFolderName>`; Proton `compatdata/<SteamAppID>/pfx/drive_c/users/steamuser/Documents/Paradox Interactive/<DocsFolderName>` (also Flatpak Steam). The dumps and the default `mod/` parent live here. |
| **Workshop / mods** | User-specified mod roots on the workspace. A workspace's index is RAM-only. |

## Script

Clausewitz/Jomini text (`.txt`, `.gui`, `.asset`, `.gfx`, `.font` and more: plan §4). One CST in `lang/jomini`. Loc YAML is `lang/loc`. No third GUI parser.

**Game-aware vs shared.** `internal/game` is the only package that knows the titles apart; everything per-game is a field on `GameInfo` rather than a `switch` on a game id. The shared language lives in `lang/jomini`, which is a leaf and must never import `game`. The test for membership is behavioural: a function belongs in `game` only if it acts differently on CK3, Victoria 3 and EU5. Taking a `gameID` parameter is not evidence.

## Modding practice

Load-order is **LIOS** (last in, only saved / last listed wins), with first-wins kinds per game in the registry. CK3 overrides by path and by key; Victoria 3 and EU5 also write entry modes before a definition's name (`INJECT:`, `REPLACE:`, `TRY_INJECT:`, `TRY_REPLACE:`, `INJECT_OR_CREATE:`, `REPLACE_OR_CREATE:`). Patch = retarget a mod onto a new game version.

## Canonical names

| Concept | Canonical | Kill |
|---|---|---|
| Product pane | Guide, `GuidePane`, `pmt.guide`, `ui.guide.*` | Helper, HelperPane, `pmt.helper`, `ui.helper`, `ide.helperWidth`, `WikiGuide` |
| Guide RPC | `WikiService.Guide` | `SessionService.WikiGuide`; query key `wiki-guide` → `guide` |
| Events | `wiki:first`, `wiki:updated` | `docs:wiki-*` |
| Package / load | `package wiki`, `LoadGuides`, `guidesPath()` | `package docs`, `LoadDocs`, persist `docsPath()` |
| script_docs health | `scriptDocsEffects` (declared effect count) | `docsPresent`, UI “dumps required” |
| Dump signature | `EngineToken.Usage` | prose in the usage field; a flat `TokenUsage` map |
| Dump effect text | `EngineToken.Doc` | stuffing into FieldInfo; a flat `TokenDoc` map |
| Token input scope | `EngineToken.In` (`[]string`) | a rendered `TokenScopes` string |
| Origin id on the wire | `"vanilla"` or mod uuid | `""` meaning vanilla |
| Origin display | `originName` | showing the uuid |
| Same origin field | JSON `origin` | `IdeRoot.originId` |
| Def kind | `Def.Kind`, JSON `kind` | `Def.Type` / JSON `type` |
| Absolute path | `path` | `LocEntry.File`, override `file` when absolute |
| Display relative | `rel` | using `path` as the label |
| Live session | `LanguageHealth.indexReady` | `ModelStatus.live` |
| Query gate file | `useLiveEnabled.ts` | `useSessionQuery.ts` |
| Load-order rule | LIOS | **LISO** |
| Graph layout | dagre | AGENTS elkjs (do not add elkjs) |

## Engine terms

The language engine is being rebuilt by `.claude/plans/language-engine.md`. These are its terms, copied from the plan's §5.0, and they win over older code, comments and documents. One word per thing.

| term | means | in a game file |
|---|---|---|
| install | one game folder at one version | `Crusader Kings III/game` |
| root | a folder that mirrors the game layout: the game folder, each DLC folder, each mod | `dlc/dlc001`, a mod's folder |
| origin | which root a file came from: `vanilla` or a mod's id | |
| kind | one database of the game: all definitions of one type | traits; characters; GUI types |
| definition | one entry of a kind, with a name | `brave = { … }` |
| nested kind | a kind whose definitions are written inside another kind's definition | faiths inside a religion; settings inside a game rule |
| subject kind | a kind whose entries are named after definitions made elsewhere and add facts to them; it defines nothing. History is the main case | `history/titles`: `k_france = { 867.1.1 = { holder = 168673 } }`; EU5 `location_templates`; GUI `blockoverride "text"` |
| file template | a file the game finds by a definition's name in a fixed folder, never named in script | `gfx/interface/icons/traits/brave.dds` for the trait `brave` |
| script-made kind | a kind with no folder, whose definitions script creates while it runs | saved scopes, variables, lists, flags |
| field | one position below a definition: a key, learned from every place the install writes it under the same parent | `trigger` inside an event; `texture` inside a widget |
| member | a field with a fixed key, as seen from its parent block | `title`, `desc`, `option` are members of an event |
| position | a field whose key is not a fixed word (§5.3) | a date key, a numeric key, a bare word in a list |
| body | a block of statements from the vocabulary | `trigger = { … }`, `immediate = { … }` |
| value | one written word, and what it turned out to be | `brave`, `character:186004`, `"gfx/x.dds"`, `1066.9.15` |
| reference | a word that names a definition, however it is spelled | `trait = brave`, `culture = "norse"`, `holder = 168673` |
| cite | a reference spelled `prefix:name` | `title:k_france`, `scope:target` |
| prefix | the word before the colon of a cite | `character`, `title`, `scope`, `var` |
| scope type | the class of object a statement runs against | `character`, `landed_title`, `country` |
| link | a declared hop between scope types | `liege`, `primary_title`, `root` |
| engine token | an effect or trigger the game declares | `add_gold`, `has_trait`, `if`, `every_vassal` |
| macro | a definition invoked by writing its name as a key | a scripted effect, a scripted trigger |
| vocabulary | everything the game declares about its language, from `script_docs` and `dump_data_types` | the `logs` and `docs` dumps |
| game info | the prose the install ships beside its script | `_traits.info`, `readme.txt`, `*.md` |
| declared / learned | stated by the game (vocabulary, game info) / seen in the install's files. A name says which; only a declared fact lets a diagnostic accuse (§6a) | |
| entry mode | a word before a definition's name that says how it combines with an earlier one | EU5 `REPLACE:name`, `INJECT:name` |
| data call | a `[…]` expression in GUI or loc | `[Character.GetName]` |

A **scope type** is not a kind, and a kind is not a scope type: `character` is what a statement runs against, `characters` is the database that defines them, and `Kind.ScopeType` is the one place the two are tied together.

## Macro, not "call kind"

A **macro** is a definition invoked by writing its name as a key: a scripted effect, a scripted trigger, a scripted list. `Effects` / `Triggers` mean the game's **declared** engine API from the vocabulary. Never reuse those words for macro names.

## Declared and learned

Two sources, and a name must say which one it came from. Getting this wrong is what produced hovers showing the wrong kind.

| Word | Source |
|---|---|
| **Declared** | The game states it: the vocabulary (the dumps) and the game info (the install's prose). Read verbatim. |
| **Learned** | Seen in the install's own files by the learner, and written to the schema file. Never hand-written, never voted. |

Only a declared fact lets a diagnostic accuse; a learned fact offers a completion and names a hover, where being wrong costs a wrong suggestion (plan §6a). Do not say "derived", "voted" or "inferred" for either.

**Keep as different words:** `lsp.Position.character` vs a card's `col`. `Ref.Kind` vs `Def.Kind`. Disk `DocsFolderName` / `ScriptDocsSubdir`.

## Code map

The rows for the language engine return as each phase of the plan writes them.

| Name | Lives in |
|---|---|
| `GameInfo`, `UserDataDir`, `MatchExtract`, `IsFIOS`, `KeyIdentity` | `services/internal/game` — **the only game-aware package** |
| The CST, the lexer, the name and literal shapes, encodings; `Cite`, `Mode`, `Constant`, `Interpolations` (the spellings inside one word) | `services/internal/lang/jomini` — shared Jomini syntax, no game id |
| `Roots`, `Inventory`, `Classify`, `File`, `Walk`, `ReadImageInfo`: every file of an install or a mod, its format, an image's header | `services/internal/source` — one format table for every game |
| Loc files, loc key conventions | `services/internal/lang/loc` |
| `Schema`, `Kind`, `Field`, `Accept`, `Value`, and the schema file (`Read`, `Write`); `Reader` (`Value`, `Key`), the one place that decides what a written word is; `Vocab` and `ReadVocab` (the script_docs dumps), `TokenParams`, `UsableIn`; `Info` and `ReadInfo` (the game info); `NameTokensOf` (the one hand table) | `services/internal/lang/schema` — what the game declares, what a scan learned, no game id |
| `CacheDir`, `SaveJSON`, `LoadJSON`, `ErrFormat` | `services/internal/persist` |
| `WriteNewMod`, `ReadListingFields`, `DescriptorModKeys` | `services/internal/modfile` |
| Wiki persist / sanitize / Guide payload | `services/internal/wiki` |
| Guide UI | `GuidePane.vue` + monaco aux bar (`pmt.guide`) |
| Graph layout | frontend `useGraphLayout` + `@dagrejs/dagre` |

## Diagnostic codes

Every check PMT reports carries a code. The code is what `# pmt:ignore <code>`
suppresses, what the settings menu toggles, and what the sweep counts, so it is
part of the vocabulary rather than an implementation detail.

Vanilla files are never diagnosed — harvest belongs in the scan cache, not in
IDE lint — so every code below describes something in a mod.

| Code | Says |
|---|---|
| `duplicate-definition` | one key defined twice inside a single origin, at a file's top level; load order decides which survives |
| `loc-header-mismatch` | a localization file's `l_<lang>:` header disagrees with its folder |
| `missing-bom` | a localization file without the UTF-8 BOM the games require |
| `missing-descriptor` | a mod with no `descriptor.mod` |
| `missing-required-loc` | a key the game will look for that nothing defines |
| `required-loc` | a definition missing localization its kind conventionally has |
| `unknown-event` | an event id fired or called that no file defines |
| `unknown-token` | an effect or trigger neither declared by the game nor defined as a scripted macro. **Experimental, default off**: 442 findings on vanilla fell to 3 by reading declared argument lists, and the last 3 are EU5 `attribute_columns`, where `effect` names a column rather than opening a script slot |
| `unknown-key` | a key that is not a member of the block it sits in, where that block name belongs to exactly one kind |
| `unresolved-ref` | `culture:nonexistent` — a citation naming a database PMT holds, and a key nothing defines |
| `wrong-declared-value` | a value outside the closed set the game documents for that field — EU5 writing `# uses_diplo_capacity = <none/mutual/giving/receiving>`. **Experimental, default off**: it fires once on vanilla Victoria 3, where the game documents `type = <investment/monopoly/new_industry/trade/colonization>` and its own `00_company_charter_types.txt` writes `type = industry`. The check is right and the documentation is stale — see GAME-SYNTAX 22 |
| `wrong-localization-folder` | a localization file under the wrong language folder |
| `wrong-scope` | an effect or trigger used in a scope the game does not allow |
| `wrong-value-type` | a value contradicting the argument type the game declares for that key |

Parse errors carry their own codes from the parser (`unclosed-brace`, `stray-close`,
`unterminated-string`, `missing-value`, and the localization ones prefixed `loc-`).
They are in the registry too, under the `parse` group rather than `check`: they
reach `ApplySeverities` like any other code, so leaving them out made them
settable and not un-settable.

**A learned fact never accuses.** The rule, and the two findings that rest on a learned convention with a calibration against vanilla, are the plan's §6a. With no dumps at all nothing is declared, so the checks that need a declaration correctly go quiet rather than guess.
