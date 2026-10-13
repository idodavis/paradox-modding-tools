# Glossary

The terms used across the PMT codebase and what each one means, so that a reader or an agent finds one word for one
thing. The right column says where a term lives: an identifier, a file or a folder. What each game's script actually
does, measured against real installs, is in [GAME-SYNTAX.md](GAME-SYNTAX.md); how the packages fit together is in
[services/README.md](services/README.md).

## The workspace

| term | means | lives in |
|---|---|---|
| install | one game folder at one version, as the app keeps it | `services.GameInstall` |
| user data | the game's Documents folder: where its dumps are written and the default `mod/` parent is | `game.UserDataDir` |
| mod | a folder with a descriptor and the files it adds or replaces | `modfiles.LoadDescriptor` |
| workspace | one game install plus the mods being edited, in load order; the saved record | `services.Workspace` |
| open workspace | the one workspace built into an index and held in memory | `workspace.Session`, `workspace.Slot` |
| root | a folder that mirrors the game layout: the game folder, each DLC folder, each mod | `inventory.Roots` |
| origin | which root a file came from: `vanilla`, or a mod's id | `inventory.File` |
| load order | the order the game loads roots; a file at the same relative path in a later root, or under a later mod's `replace_path`, hides the earlier one | `index.Loaded` |
| FIOS, LIOS | first in, only served and last in, only served: in a first-wins database the first definition in load order wins, in every other the last | `game.Layout` |
| listing | what a mod's page shows: its name, version, tags, picture and description | `modfiles.ListingFields` |

## Where the game's facts come from

| term | means | lives in |
|---|---|---|
| script | the Clausewitz/Jomini text the games' `.txt`, `.gui`, `.asset`, `.gfx` and `.font` files are written in; one syntax tree for all | `jomini` |
| localization | the YAML-like files of display text, `$key$` interpolations and `[data.calls]` inside | `loc` |
| dumps | what the game declares about its own language (`script_docs`, `dump_data_types`): effects, triggers, links, scope types, modifiers, data types | `schema/dumps.go` |
| docs | the prose and the `<type>` a game ships beside its script: `_*.info`, readmes and `*.md` inside the install | `schema/docs.go` |
| wiki | the community wiki: guides and patch notes, and a source of script examples, last in trust order | `wiki` |
| vanilla | the game's own files; what they write at a position is evidence, never a claim that accuses | `schema.Vanilla` |
| shipped schema | the backup schema, built from the dumps alone, used when a dump file is missing | `schema.LoadShipped` |
| source | where a claim came from, in trust order: the registry's rows, the dumps, vanilla, the docs, the wiki | `schema.Source` |

## The engine

| term | means | lives in |
|---|---|---|
| database | all entries of one type: one per script folder of the install, named by the folder's path without `common/` | `schema.Database` |
| entry | one thing a database holds, with a name; the entry detector decides where one is written | `schema/entries.go` |
| nested database | a database whose entries are written inside another database's entry | `game.Descriptor` (`Parent`, `Within`) |
| history database | a database whose entries are the start state of other databases' entries; it defines none of its own | `game.PurposeHistory` |
| script-made name | a name a command creates while script runs, in a database no folder holds | `game.NameCommands` |
| occurrence | one place a name is written in a file, with its role there | `schema.Occurrence`, `index.Occurrence` |
| role | what an occurrence does: defines a name, refers to one, or localizes one | `schema.Role` |
| reference | a word at a position whose declared value names a database, or a prefixed word whose prefix is a declared link; a word no source declares is no reference | `schema.Refers` |
| prefixed word | a word spelled `prefix:name`; the prefix is a declared link | `jomini.SplitPrefixed` |
| data call | a `[…]` expression in GUI or localization | `jomini.ParseCall` |
| data function | one function of a data type, written `Type.Function`; each step of a binding refers to one | `game.DataFunction` |
| fact | what a binding asks the game, with the answer the user gave once per workspace; a binding no fact or rule answers is not known | `draw.Fact`, `services.Workspace` (`Facts`) |
| position | a place in a database's entry or in a command's arguments; keys collapse only by written form (`<number>`, `<date>`, `<prefix:>`) | `schema.Position` |
| shape | what a block holds: fields, triggers, effects, arguments, value, description, setup or widget | `game.Shape` |
| key rule | one way a key can be read; a block tries its rules in order and the first that fits wins | `game.KeyRule` |
| construct | a key the language gives a fixed shape to and no dump declares: a random_list's branches, a switch's cases, the list iterators | `game.Construct` |
| value type | what a position's value is: number, date, bool, text, a database's name | `schema.ValueType` |
| command | an effect, trigger or link the dumps declare | `schema.Command` |
| call | a key that is no command or link, naming an entry of a database whose descriptor says it is called there: a scripted effect, a scripted trigger, a script value | `game.Descriptor` (`Callers`) |
| scope type | the class of object a statement runs against (`character`); the database that defines them is another word (`characters`) | `schema.ScopeType` |
| link | a declared hop between scope types | `schema.Link` |
| scope | where a block's script runs: this scope, the entry's root, the ones entered before, the scopes the entry saved by name, and why | `schema.Scope` |
| claim | a fact a source states at a position or about a command, with who stated it; the highest-ranked source to state it keeps it | `schema.Claim` |
| declared | stated by the dumps, the docs or the wiki; only a declared claim, fresh and uncontradicted by vanilla, lets a diagnostic accuse | `schema.MayAccuse` |
| entry mode | a word before a definition's name that says how it combines with an earlier one (Victoria 3 and EU5: `REPLACE:`, `INJECT:`) | `game.EntryMode` |
| descriptor, layout | how one database's entries are written and named, and one game's set of them | `game.Descriptor`, `game.Layout` |

## The editor and the pages

| term | means | lives in |
|---|---|---|
| hover card | what the editor shows for the word under the cursor: what it is, what it takes, where it is defined | `lsp.Hover`, `frontend/src/ui/HoverCard.vue` |
| Visual view | a `.gui` file drawn from its text, beside Code and Split | `frontend/src/draw/`, `frontend/src/editor/tabs.ts` |
| draw list | one render of a window: the elements to paint, the tree of its parts, the fonts and facts it asked | `draw.List` |
| element | one painted rectangle of a draw list, at an absolute box, with its texture or text | `draw.Element` |
| part | one widget of a window, listed whether it is drawn or not | `draw.Part` |
| visibility | whether a part is shown, hidden or unknown as its `visible` is answered | `draw.Part` |
| panel | the Widgets panel in the IDE's right column: the tree, a part's properties, the sample data, its script | `frontend/src/draw/GuiDetail.vue` |
| diagnostic | something wrong in a mod's file, with a code; the code is what `# pmt:ignore <code>` and the settings toggle name | `lsp.Diagnose` |
| tab | an open file's path, or a diff tab `git:<changes\|staged\|conflict>:<path>` or `git:commit@<hash>:<path>` | `frontend/src/editor/tabs.ts` |
| editor group | a strip of tabs in the IDE; the groups sit in rows and columns, laid out per workspace | `frontend/src/stores/editor.ts` |
| window | the main window, or another showing one thing: a page, an editor group, the game's files, a folder or the panel | `frontend/src/stores/windows.ts` |
| Wiki pane | the pane beside the IDE editor showing the wiki's guides for the open file | `frontend/src/pages/ide/WikiPane.vue` |
| contest | a name two mods define where one takes, or tries to take, the other's place; the load order decides which the game keeps | `index.Winner` |
| override | a name or a file path two origins define where no two mods contend: one mod's over another origin's, vanilla's or a mod's it adds to | `pages.Health` |
| dependency | a name a mod writes that another mod's definitions decide | `pages.Health` |
| dangling reference | a reference nothing defines | `pages.Health` |

## Diagnostic codes

Every check PMT reports carries a code. Vanilla files are never diagnosed, so each code describes something in a mod.

| code | says |
|---|---|
| `unresolved-ref` | a declared reference names an entry nothing defines anywhere, names created by commands included (`culture:nonexistent`) |
| `unknown-event` | a reference to an event that no event defines, where a declared source states the position's value |
| `wrong-value-type` | a value of another type than the position's declared type |
| `wrong-declared-value` | a word outside the position's declared word list |
| `wrong-scope` | a command written where the known scope is not one it runs in, or a variable read in a scope that stores none; an unknown scope is quiet |
| `missing-key` | a key a declared source calls required, left out of an entry; off unless turned on |
| `duplicate-definition` | a name the workspace defines twice where the entry modes say neither replaces nor injects |
| `inject-without-target` | an `INJECT:` or `REPLACE:` entry with nothing loaded before it |
| `missing-bom` | a localization file without the UTF-8 byte order mark the games require |
| `loc-header-mismatch` | a localization file whose `l_<lang>:` header names another language than its name does |
| `wrong-localization-folder` | a localization folder spelled `localisation`, which the game never reads |
| `missing-file` | a path that exists nowhere in the install or the mods, where vanilla's paths at the position all exist; off unless turned on |

A file that does not parse carries the parser's own codes (`unclosed-brace`, `stray-close`, `unterminated-string`,
`missing-value`, and a localization file's, prefixed `loc-`); the settings cannot turn them off, a bare
`# pmt:ignore` on the line can.
