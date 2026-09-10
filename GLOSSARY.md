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
| Console dumps | script_docs | OS user-data `logs/` (CK3) or `docs/` (Vic3/EU5), archived to `cache/script-docs-*` | **The type system**, not an overlay. In-game `script_docs` output, parsed by `catalog/schema.go` into `Schema`. Source is `live` / `archive` / `missing`. |
| Install prose | game info | Shipped `_*.info`, readmes, `*.md` **inside the game install** | Harvested into `fieldInfo` / `fieldInfoByKind`. Never call this script_docs or wiki. Never name the cache field `GameInfo` (collides with `game.GameInfo`). |

Hover one-liners are `session.FieldDoc`: kind `fieldInfo` → global `fieldInfo` →
`Schema` token doc. Signatures come from `EngineToken.Usage`. There is no flat
`TokenDoc` / `TokenUsage` / `TokenScopes` map and no `VanillaCache.Effects` /
`.Triggers` — `Schema` is the single representation, read through the session
accessors. Never persist a rendered hover string; `TokenScopes` used to hold
`"character; targets: culture"` and that display-flattening is the bug this
engine was rebuilt to remove.

## Install, user data, workshop

| Tree | What |
|---|---|
| **Install** | Steam/Paradox game folder. Vanilla script, GUI, loc, game info. `VanillaCache` is keyed by install id + version. |
| **User data** | `game.UserDataDir`: first existing of launcher `gameDataPath`; Win/mac `~/Documents/Paradox Interactive/<DocsFolderName>`; Linux `$XDG_DATA_HOME` or `~/.local/share/Paradox Interactive/<DocsFolderName>`; Proton `compatdata/<SteamAppID>/pfx/drive_c/users/steamuser/Documents/Paradox Interactive/<DocsFolderName>` (also Flatpak Steam). script_docs and the default `mod/` parent live here. |
| **Workshop / mods** | User-specified mod roots on the workspace. Harvest is RAM-only (`BuildIndex`). |

## Script

Clausewitz/Jomini text (`.txt`, `.gui`). One CST in `parser/jomini`. Loc YAML is
`parser/loc`. No third GUI parser. `MatchExtract` is **def kind**. `ClassifyRel`
is **file bucket** (`script` / `gui` / `loc` / `info` / `meta`).

**Game-aware vs shared.** `internal/game` is the only package that knows the
titles apart; everything per-game is a field on `GameInfo` rather than a `switch`
on a game id. The shared language — `CanonicalKind`, `ScriptSlot`,
`ParseTyped`, `TypedSpans`, `ScriptName`, `IsEphemeral`, `ScriptParamSpan` —
lives in `parser/jomini`, which is a leaf and must never import `game`. The test
for membership is behavioural: a function belongs in `game` only if it acts
differently on CK3, Victoria 3 and EU5. Taking a `gameID` parameter is not
evidence — `ScriptName` and `SkipFieldRHS` both took one and never read it.
Where a title reserves part of the shared grammar (EU5 spends `INJECT:` on entry
modes), `game.ParseTyped` wraps `jomini.ParseTyped`; the leaf stays ignorant.

## Modding practice

Load-order is **LIOS** (last in, only saved / last listed wins). CK3/Vic3 path
overwrite; EU5 also `INJECT:` / `REPLACE:` entry modes. Patch = retarget a mod
onto a new game version.

## Canonical names

| Concept | Canonical | Kill |
|---|---|---|
| Product pane | Guide, `GuidePane`, `pmt.guide`, `ui.guide.*` | Helper, HelperPane, `pmt.helper`, `ui.helper`, `ide.helperWidth`, `WikiGuide` |
| Guide RPC | `WikiService.Guide` | `SessionService.WikiGuide`; query key `wiki-guide` → `guide` |
| Events | `wiki:first`, `wiki:updated` | `docs:wiki-*` |
| Package / load | `package wiki`, `LoadGuides`, `guidesPath()` | `package docs`, `LoadDocs`, persist `docsPath()` |
| script_docs health | `scriptDocsEffects` (declared effect count) | `docsPresent`, UI “dumps required” |
| Install prose maps | `fieldInfo` / `fieldInfoByKind` | `fieldDocs*`; never `VanillaCache.GameInfo` |
| One hover sentence | `session.FieldDoc` | `FieldDocsForKind`, `Guide.FieldDocs` |
| Dump signature | `EngineToken.Usage` | prose in the usage field; a flat `TokenUsage` map |
| Dump effect text | `EngineToken.Doc` | stuffing into FieldInfo; a flat `TokenDoc` map |
| Token input scope | `EngineToken.In` (`[]string`) | a rendered `TokenScopes` string |
| Origin id on the wire | `"vanilla"` or mod uuid | `""` meaning vanilla |
| Origin display | `originName` | showing the uuid |
| Same origin field | JSON `origin` | `IdeRoot.originId` |
| Def kind | `Def.Kind`, JSON `kind` | `Def.Type` / JSON `type` |
| File bucket | `ClassifyRel` → `script`/`gui`/`loc`/`info`/`meta` | `FileKind`; bucket `"docs"` |
| Absolute path | `path` | `LocEntry.File`, override `file` when absolute |
| Display relative | `rel` | using `path` as the label; `DisplayRel` **fills** `rel` |
| Live session | `LanguageHealth.indexReady` | `ModelStatus.live` |
| Query gate file | `useLiveEnabled.ts` | `useSessionQuery.ts` |
| Load-order rule | LIOS | **LISO** |
| Graph layout | dagre | AGENTS elkjs (do not add elkjs) |

## Type system words

| Term | Means |
|---|---|
| **Scope type** | A class an effect or trigger runs against (`character`, `country`). Declared in `event_scopes.log`. Not a "kind". |
| **Kind** | A harvested database, named after its folder (`cultures`). `Def.Kind`. Not a scope type. |
| **Scope link** | A named hop between scope types (`ruler`, `capital_county`), from `event_targets.log`. |
| **Prefix** | A scope link citable as `name:key` (`culture:english`, `cu:`). Global + Requires Data. |
| **Binding** | The join from a scope type to the kind(s) holding its rows. `catalog.bindKinds`. |
| **Schema** | The whole declared type system for one install. `VanillaCache.Schema`. |

`KindScope` maps kind → scope type. `PrefixKinds` maps prefix → kind. Do not
swap those two.

## Macro, not "call kind"

A **macro** is a `scripted_*` definition invoked by name. `jomini.IsMacroKind`
settles it from the folder name. The old "call kind" vote is gone.

| Concept | Canonical | Kill |
|---|---|---|
| Macro def keys → kind | `catalog.macroDefKinds` | `CallKindSet` |
| Macro def key set | `catalog.macroDefKeys` | `EffectSet` (means engine effects) |
| Persisted macro call sites | `macroCallRefs` | `callKindRefs` |
| Session macro maps | `s.macroDefKinds`, `s.macroKeys` | `callKindSet`, `effectSet` |
| Session predicates | `IsMacroKind`, `IsMacroDef` | `IsCallKind`, `IsCallKey` |
| Session macro lookups | `FindMacroDefs`, `addMacroDefsLocked`, `rebuildMacroSetsLocked` | `FindDefsOfCallKind`, `addCallKindsLocked`, `rebuildCallSetsLocked` |
| Macro call at a cursor | `macroCallAt`, `macroCallIdentity` | `callKindDefAt`, `callKindID` |

`Effects` / `Triggers` mean the game's **declared** engine API from `Schema`.
Never reuse those words for macro keys.

## Declared, derived, inferred

Three sources, and a name must say which one it came from. Getting this wrong is
what produced hovers showing the wrong kind.

| Word | Source | Where |
|---|---|---|
| **Declared** | The game states it in `script_docs` | `Schema`. Read verbatim. |
| **Bound** | Deterministic join of declared types against harvested def keys | `bindKinds` → `KindScope`, `PrefixKinds` |
| **Derived** | Install evidence, for the four things no dump states | `catalog/derive.go`, `Derived` |

Nothing is **voted** any more. `Derive*` functions resolve by unanimity or a
stated threshold, and the doc comment must name which — not "votes".

| Concept | Canonical | Kill |
|---|---|---|
| Install-derived facts | `catalog/derive.go`, `Derived`, `derivedFromCache` | `votes.go`, `Votes`, "vote maps" |
| Fire-key derivation | `deriveFireKeys`, `resolveFireKinds`, `fireTargets` | `VoteFireKeys`, `finishFireVotes`, `voteFireTargets` |
| Other derivations | `deriveNestedShapes`, `deriveWrappers`, `deriveLocAffixes`, `deriveLocFields`, `deriveFieldValueKinds`, `deriveFieldEnums` | any `Vote*` name |
| Prefix → kind origin | "bound by `bindKinds`" | "voted" |

Only the derivations above exist, because only they are unstated by every dump:
fire keys (`event` is not a scope type in any game), nested databases (CK3
faiths under religions), setup wrappers, and the two localization conventions —
how a kind names its keys (`deriveLocAffixes`) and which script properties hold
one (`deriveLocFields`). Adding another means first proving no dump declares it.

Every one carries an **evidence floor** as well as a coverage ratio —
`minFieldHits`, `minOptionRefs`, `minAffixDefs`, `minLocFieldHits`. What each
one is and why it is that number is measured in
[GAME-SYNTAX.md §3e](GAME-SYNTAX.md).

**Keep as different words:** `lsp.Position.character` vs `HoverResult.col`.
`EventFieldInfo` vs cache `fieldInfo`. `Ref.Kind` vs `Def.Kind`. Disk
`DocsFolderName` / `ScriptDocsSubdir`.

## Code map

| Name | Lives in |
|---|---|
| `GameInfo`, `UserDataDir`, `MatchExtract`, `IsFIOS`, `KeyIdentity` | `services/internal/game` — **the only game-aware package** |
| `CanonicalKind`, `ScriptSlot`, `ParseTyped`, `ScriptName`, `IsEphemeral` | `parser/jomini` — shared Jomini grammar, no game id |
| `WriteNewMod`, `ReadListingFields`, `DescriptorModKeys` | `services/internal/modfile` |
| `VanillaCache`, `Scan`, `BuildIndex`, `ClassifyRel` | `services/internal/catalog` |
| `ReadSchema`, `bindKinds`, dump archive | `catalog/schema.go`, `schemaarchive.go` |
| The four `Derive*` passes | `catalog/derive.go` |
| `FieldDoc`, the whole session query API | `session/query.go` (read-only; `session.go` is the mutable half) |
| LSP tests | beside the request: `hover_test.go`, `definition_test.go`, `complete_test.go`, `diagnostics_test.go`, `scope_test.go`; `lsp_test.go` is fixtures only |
| Wiki persist / sanitize / Guide payload | `services/internal/wiki` |
| Health `scriptDocsEffects` | `SessionService.GetLanguageHealth` |
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

**A derived pass never accuses. That is enforced, not asserted.** The derived
passes — install-inferred field types, coverage-floored conventions — exist to
offer a completion and to name a hover, where being wrong costs a wrong
suggestion; a diagnostic that is wrong tells a modder their correct script is
broken. See GAME-SYNTAX §13.

The enforcement is `catalog.Ref.Derived`, set where a reference's KIND came from
an install-derived pass rather than from a declaration. `refRows` drops those
before emitting a **dangling** row, and `unknownEventDiags` skips them. Both
incidents in this project's history were a strong derived pass reaching an
accusing surface — 859 `body_part` rows from `FieldValueKinds`, 4,033 fire rows
from `FireKeys` — and this is the mechanism that stops it.

**Two codes are derived-fed with a calibration, and saying otherwise was the
bug.** `required-loc` rests on `LocAffixes` at floor 100 and `unknown-key` on
`NestedMembers` + `UnambiguousBlocks`; both are kept, because each has a
published zero-findings-on-vanilla calibration. The honest claim is *"derived
where the floor is calibrated against vanilla, declared everywhere else"* — the
previous wording, that no diagnostic consults a derived pass, was false for
three of the twelve.

**Declared beats derived cuts both ways.** A fire reference is NOT derived when
the game declares the fire key: CK3 states `trigger_event`, so `unknown-event`
still reports a mod firing an event nobody defines. Only a fire key PMT inferred
from the corpus is silenced. With no `script_docs` at all nothing is declared,
so these checks correctly go quiet rather than guess.
