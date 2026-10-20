# game

The only game-aware package: what is true of each game, and the language rules of Paradox script, as rows.

A **row** carries its whole condition and its consequence as fields; a reader looks rows up and never decides by a
`case` on a word a row holds. Every row says what it stands on, its **source** (`Source`, below), in one of two ways.
A row a reader judges by (`Precedence`, `DatabaseRule`, `Keywords`, and the coat rows the draw payload sends:
`CoatRecolors`, `CoatShading`, `CoatPlacement`, `CoatSlots`) carries `Source` as a field; `DatabaseRule.Source` is the
rule's standing for every fact on it that carries none of its own (`FoldsCase`, the event keys, a rule that only makes a
database reached). A plain mapping (`ClassDatabases`, `IconPathOverrides`, `CoatInputs`, `CoatRegions`, `CoatNew`) and a
scalar constant (`CoatFallback`, `CoatRotationClockwise`, `CoatDepthInFront`) name their `Source` in their doc comment. A
row no source states says `SourceInferred` and names its evidence in a comment of one line. A **precedence** says how
the definitions of one database combine across files and mods. An **entry term** is what one entry of a database is
called on screen (`DescribeEntry`).

**Case.** Command names match without regard to case (the dumps write them in lower case and vanilla writes `IF`, `NOt`
and `limiT`; `SourceVanilla`); every other lookup is exact, except in the databases whose `DatabaseRule` says `FoldsCase`.

Seven files, one per thing a game is:

| file | holds |
|---|---|
| `game.go` | `Game`: a game's names, wiki, script and stage roots, descriptor kind, Documents folder, base language, Steam app id and library, first-wins sentence, new-mod and dump folders, how each OS starts it (`Program`), where it writes its error log (`ErrorLog`) and the rules of its script (`Rules`, `EntryModes`, `DefaultPrecedence`, `ModeFolders`); the `Source` every row names; `Lookup` and `ListGames` |
| `install.go` | where a game is on this machine: its installs through Steam's library folders, its version, its user-data and dump folders, its descriptor file and when an install was last updated |
| `language.go` | the language: the shapes of block, the key rules and how each reads, where a key's block reads, what decides its shape, how its scope follows, the operators and fixed-shape keys (`Keywords`, each with the sentence a hover card shows), the words that name a scope, a variable or a yes-or-no |
| `databases.go` | `DatabaseRule`, one row shape for a folder's database (`Path`) and a file type's (`Extension`): how entries are named, what they house, how their definitions combine, an event's scope, blocks and picture; the entry modes, the localization database's facts, the databases no definition carries, the class names no rule maps, and the entry term |
| `colors.go` | the `Colors` rows, a color block's numbers read into one color (`ParseColor`, `RGBA`) and a color written back in a block's form (`FormatColor`) |
| `coat.go` | what a game does with a coat of arms beyond its syntax: the databases it reads, where each game keeps its coats and textures, the field's file, which way depth and rotation turn its layers, how each texture is colored and shaded, where each placement key puts a layer, how a key is edited, what a new coat writes, and the frames its designer previews it in |
| `gui.go` | what a game does with a GUI file beyond its syntax, as tables the visual editors read |

## Source

`Source` says where a fact or a claim came from, by rank; a lower value outranks a higher one: the registry's own rows
(`SourceRegistry`), the console dumps, a message the game's executable carries, a line its logs write, the install's own
files (vanilla), the prose the install ships beside its script (docs), the community wiki, and `SourceInferred`.
`Accuses` is true for the dumps, the docs and the wiki, the sources that may accuse a user's file.

## The language (`language.go`)

The schema's walker (`schema/walk.go`) reads script by these tables and nothing else, and the entry detector
(`schema/entries.go`) reads the `Game.Rules`. A rule changes here, in one row, never as a branch in a reader.

| table | says | read by |
|---|---|---|
| `Shape` | the kinds of block: fields, triggers, effects, arguments, value, description, unknown, setup (a history or start block), widget (GUI) | everything |
| `Shapes` (`ShapeRule`) | per shape: the key rules it tries in order, the word its calls resolve by, the declared commands it holds (`Allows`) | the walker, completion |
| `KeyRules` (`KeyRuleRow`: `BlockAt`, `ShapeFrom`, `ScopeFrom`) | per key rule: where its block reads, what decides what the block holds, how its scope follows; whether a rule fits a key is the walker's test | the walker |
| `Keywords` (`LookupKeyword`, `IsOperator`, `MatchListIterator`, `ClassifyKey`, `LookupDescriptionKeyword`) | the keys of the language itself, each with its class, its `Blurb` and its `Source`: the ones it gives a fixed shape to and no dump declares (random_list's branches, switch's cases, the list iterators, a description's markers with their `Shows`, a value block's operators, a widget's blocks) and its operators the dumps declare as commands, which name no shape and leave it to the key's rule; the first row that fits wins | the walker, completion, hover, the editor's tokens, the event graph's detail |
| `DeclaredEffect`, `DeclaredTrigger`, `DeclaredLink` | the dumps' words for what declares a command (effect, trigger, event target), which `schema.Command.Declared` lists in dump order | the dump reader, the walker, completion, hover |
| `SavedScopes`, `Variables`, `LocalVariables`, `GlobalVariables`, `ScopeVarCommands` | the link prefixes that read a saved scope or a variable back (`scope:x`, `var:x`; `root`, `prev` and `this` are the walk's own, `Follow`), and the commands whose value makes or reads a script-made name (`save_scope_as`, `set_variable`) | the walker, the schema's occurrences |
| `Bools`, `Colors`, `DumpWords` | how yes/no and colors are written, and the dumps' words the engine decides by | the readers, hover, the color swatches, the visual editors |

**How a key reads.** In a block of a shape, the walker tries `Shapes[shape].KeyRules` in order and the first rule that fits
decides. A triggers or effects block tries a vanilla scripted entry's name (a call), a declared position, a declared
command, a link, a position vanilla writes, else a call; a value block tries a value operator after the commands; an
arguments block reads no links; a setup block tries the declared and documented positions before a call; a block of
unknown shape ends with its own position or a call; a fields block reads its own positions, a widget a call or its
positions, and a description block only the construct rows. To change how keys read in a kind of block, reorder or
edit that row.

## The databases (`databases.go`)

A folder rule holds for its path and everything below it; a path takes the longest rule at or above it
(`schema.folder` walks up, asking `Game.FindRule` of each step). A database is found in the install, one per script folder (a `common/` subfolder joins its
ancestor's), so a folder needs a rule only to say what its files cannot: how entries are named, what wraps them, what
calls them, how their definitions combine, what an event's blocks mean. A rule with an `Extension` and no `Name` is a
file type's (`FindRuleByExtension`): how `.txt`, `.asset`, `.font` and `.gui` files name entries, what their entries'
blocks hold, the word that calls the tag a block is written with, the key a block's bindings read their data context
from, the ending of the keys whose untagged number block is a color, and the keys whose value names an entry, a file, a
localization key, or a call (`Reference`).

| table | says | read by |
|---|---|---|
| `DatabaseRule` | per database: how its entries are identified, what it houses beyond plain entries (`Houses`: events, on_actions, lists, functions, concepts, history), `WrapperKeys`, the `DeclaringKeys` that declare an entry inline, dated blocks, what an entry's block holds (`Shape`), the `CallerWords` a call resolves by, `TakesParameters`, `Local`, `References`, `AliasKeys`, `LocPrefix`, `FoldsCase` (read by the walk); nested databases by their `ParentPath` and `KeyPath` | the schema, the index |
| `DatabaseRule.Precedence`, `Game.DefaultPrecedence`, `Game.ModeFolders` | `Game.LookupPrecedence(database, folder)`, the folder the database's rule declares (`DatabaseRule.ResolveFolder`): the database's rule, else the precedence of the databases that take entry modes (by `Paths` where the game lists them, else by `Roots`; never one in `Except` or with no folder), else the game's default: which definition the game keeps (last, first, highest priority, merged), what orders them, where an entry mode is written, what appends or may not repeat, what a higher priority replaces | `index.Winner`, the health page |
| `DatabaseRule.ScopeKey`, `TypeKey`, `TypeSuffix`, `DefaultScope`, `RunOrder` | an entry's root scope (the scope key's value, else the type key's value less its suffix where that names a declared scope type, else the default) and the order its blocks run in | `schema.ResolveEntryScope`, the judging of a block's scopes |
| `DatabaseRule.EventPicture` | the hops from an event to its picture: the key that writes it outright, else the first chain of keys and databases that ends in a picture file | the event graph's detail |
| `Game.EntryModes` (`EntryMode`, `Game.SplitMode`) | the prefixes before a definition's name (`REPLACE:`, `INJECT:`), what each does, and the split of one off a name | the entry detector, diagnostics, `index.Winner` |
| `DescribeEntry`, `plurals` | what one entry of a database, or entries of it, are called: its last path word spaced and made singular | every card, row and message that names an entry |
| `Localization`, `File`, `DataFunction`, `GlobalType` | the databases no definition carries (a loc key, a file path, a data function) and the data type a data call's first step is read on | the index, the schema, completion, hover |
| `ClassDatabases` | the dumps' class names no rule maps to a database, by the database's path | the dumps' reader |
| `LocReplaceFolders`, `LocFillIns`, `MisspelledFolders`, `EventPortraits`, `OptionFlags` | the folders whose localization overrides every other, the values a localization text fills in, the folder names the game never reads, and the event words the event graph's detail reads | localization, diagnostics, the event graph |

An inferred row says so in its `Source`: EU5's default precedence, Vic3's `technology` and `strategic_regions`, a
placement key's default position. A row whose reader is not written yet names its reader in its doc comment.

## Games (`game.go`, `install.go`)

`registry` is the table of supported games: names, wiki endpoint, script and stage roots, descriptor kind, Documents
folder, Steam app id and library, the first-wins sentence the pages show, the new-mod and dump folders, how each OS
starts the game (`Program`: the executable, the debug flags, the launcher settings that override them; a Linux row has
no executable, since none is measured, so launching stays off there), its error log (`ErrorLog`: the file, the rotated
name, the header line and where a message names a script path and line) and the rules of its script. `Lookup` and
`ListGames` read it. `install.go` finds installs through Steam's library folders, loads a game's version, and locates
its user-data and dump folders on Windows and Linux.

## The GUI rules (`gui.go`)

What a game does with a GUI file beyond its syntax, which `draw` reads and nothing else. Each row names its source: the
CK3 wiki's Interface page, the game's shaders, or vanilla observed in the app.

| table | says |
|---|---|
| `WidgetSlots` (`WidgetSlot`, `WidgetRole`) | the keys whose block is a widget no type is named after: a skin, chrome, an attachment, a label, or the widget a scroll or zoom area holds |
| `GUITypes` (`WidgetType`, `Holding`) | how each built-in type draws: how it holds its children, whether it clips them, fills its holder or centers its own text, and the games in which a hidden child of it takes no room |
| `UndeclaredTypes` | the built-in types a game draws though no file of its install declares them, each with its games |
| `GUIHolders` | the window the engine lays a window made from a type out in, each with its games (EU5's lateral views) |
| `GUIOpeningStates` | which states run as a window opens, and the keys of a state that are its own rather than end values |
| `GUIAnswered` | the data calls the preview answers from the index (GetDefine), and the database each reads |
| `GUIBooleans` | the data functions a binding combines conditions with |
| `GUIPictureKeys`, `GUIIconPathSuffix`, `IconPathOverrides` | the keys an entry names its picture by, the ending of the defines that name icon folders, and the file in an icon folder that names its icons' paths |

## The coat of arms rules (`coat.go`)

What a game does with a coat of arms beyond its syntax. `draw`'s `coat.go` reads the databases, folders, field file,
shading texture and frames; the rest (how each texture is colored, which channel each mask region is, which way a
rotation turns, where each placement key puts a layer, how a key is edited, what a new coat writes) is the frontend's
copy until the payload carries it. A row from a game's shaders says `SourceVanilla`; one observed in the app is
`SourceInferred`.

| table | says |
|---|---|
| `CoatDatabases` | the databases a coat reads: its own, and the named colors its slots name |
| `CoatFolders` | the folder each game keeps its coats in, from its stages' folder (EU5's under `main_menu/`) |
| `CoatFieldFile` | the file whose first atlas' `tile_size` is the field a coat is drawn on |
| `CoatTextureFolders` | the folder each texture key's files are under (`pattern`, `colored_emblem`, `textured_emblem`) |
| `CoatDepthInFront`, `CoatRotationClockwise` | which way `depth` orders a coat's layers and which way a positive rotation turns |
| `CoatRecolors`, `CoatRegions`, `CoatFallback` | how each texture kind is colored (its starting slot, its blends, its overlay, its alpha channel), the channel of each mask region, and a pattern's starting color |
| `CoatSlots` | the color slots a layer's shaders read, and which is required |
| `CoatShading` | the texture a game shades every coat with and the overlay and grain strengths; a game not listed shades none |
| `CoatPlacement`, `CoatInputs`, `CoatNew` | what each placement key moves, sizes or turns and its default, how each key's value is edited, and what a new coat, instance and colored emblem write |
| `CoatFrames` (`CoatFrame`, `CoatOverride`) | the shapes a game's coat designer previews a coat in, in its order: a mask file, or the frame a define names, with the defines that override it (CK3 1.20); a game with no designer has none |
