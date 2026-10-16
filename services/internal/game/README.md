# game

The only game-aware package: what is true of each game, and the language rules of Paradox script as data.

| file | holds |
|---|---|
| `lang-rules.go` | the language rules of Paradox script as tables, and each game's file `Layout` |
| `gui-rules.go` | what a game does with a GUI file beyond its syntax, as tables the visual editors read |
| `coa-rules.go` | what a game does with a coat of arms beyond its syntax: the databases it reads, where each game keeps its coats and their textures, the field's file, which way depth orders its layers, the texture that shades it and the frames its windows cut it to |
| `game.go` | each game's identity (`GameInfo`), install and version detection, user-data and dump folders, Steam library paths |
| `colors.go` | a color block's numbers read into one color by its `Colors` row (`ColorOf`, `RGBA`), and a color written back in a block's form (`PartsOf`) |

## The language rules (`lang-rules.go`)

The schema's walker (`schema/walk.go`) reads script by these tables and nothing else, and the entry detector
(`schema/entries.go`) reads the layouts. A rule changes here, in one row, never as a branch in a reader.

| table | says | read by |
|---|---|---|
| `Shape` | the kinds of block: fields, triggers, effects, arguments, value, description, unknown, setup (a history or start block), widget (GUI) | everything |
| `Shapes` (`ShapeRule`) | per shape: the key rules it tries in order, the word its calls resolve by, the commands it allows | the walker, completion |
| `KeyRules` (`KeyRuleRow`: `At`, `Holds`, `Scope`) | per key rule: where its block reads, what decides what the block holds, how its scope follows; whether a rule fits a key is the walker's test | the walker |
| `Kinds`, `KindWords` | what a declared command is (effect, trigger, link) and the dumps' word for each | the dump reader, hover |
| `Constructs` (`ConstructOf`, `IsOperator`, `ListIterator`) | the keys the language gives a fixed shape to and no dump declares: random_list's branches, switch's cases, the list iterators, a description's markers, a value block's operators, a widget's blocks; the first row that fits wins | the walker, completion, hover |
| `ScopeLinks`, `NameCommands` | the links the language itself writes (`root`, `prev`, `this`, a saved scope), and the commands whose value makes or reads a script-made name (`save_scope_as`, `set_variable`) | the walker, the schema's occurrences |
| `DynamicDescription`, `EventPortraits`, `OptionFlags` | the words of a dynamic description, the keys an event names its portraits with, the yes-or-no words an option is marked with | the event graph's detail |
| `Bools`, `Colors`, `DumpWords`, `MisspelledFolders` | how yes/no and colors are written (a tagged block, or an untagged one of numbers, which `colors.go` reads), the dumps' words the engine decides by (a return type of the dumps naming no type among them), folder names the game never reads | the readers, hover, the color swatches, the visual editors, diagnostics |
| `EntryMode`, `GameInfo.Mode` | the words before a definition's name (`REPLACE:`, `INJECT:`), what each does, and the split of one off a name | the entry detector, diagnostics, `index.Winner` |
| `FileTypes` (`Ref`) | how `.txt`, `.asset`, `.font` and `.gui` files name entries, what their entries' blocks hold, the word that calls the tag a block is written with (`TagCalls`), the key a block's bindings read their data context from (`ContextKey`), the ending of the keys whose untagged number block is a color (`ColorSuffix`, a GUI file's `fontcolor`), and the keys whose value names an entry, a file, a localization key, or a call by the key's own word | the entry detector, the schema's occurrences, the visual editors |
| `Layout` (`Descriptor`, `Identity`, `Purpose`) | per game, one row per database that needs one: how its entries are identified, what it is for, wrappers, the keyword that declares an entry inline, dated blocks, what an entry's block holds, the words a call resolves by, parameters, whether its entries answer only in their own file (`Local`), references (`Ref`), names, and what a loc key puts before an entry's name (`LocPrefix`); nested databases by their `Parent` and key path `Within`; entry modes; first-wins databases | the schema, the index |

**How a key reads.** In a block of a shape, the walker tries `Shapes[shape].Keys` in order and the first rule that fits
decides. A triggers or effects block tries a vanilla scripted entry's name (a call), a declared position, a declared
command, a link, a position vanilla writes, else a call; a value block tries a value operator after the commands; an
arguments block reads no links; a setup block tries the declared and documented positions before a call; a block of
unknown shape ends with its own position or a call; a fields block reads its own positions, a widget a call or its
positions, and a description block only the construct rows. To change how keys read in a kind of block, reorder or
edit that row.

**Layouts.** A folder row holds for its path and everything below it; a path takes the longest row at or above it
(`Layout.At`). A database is found in the install, one per script folder (a `common/` subfolder joins its ancestor's),
so a folder needs a row only to say what its files cannot: how entries are named, what wraps them, what calls them.

## The GUI rules (`gui-rules.go`)

What a game does with a GUI file beyond its syntax, which `draw` reads and nothing else. Each row names its source:
the CK3 wiki's Interface page, the game's shaders, or vanilla observed in the app.

| table | says |
|---|---|
| `WidgetSlots` (`WidgetSlot`, `WidgetRole`) | the keys whose block is a widget no type is named after: a skin (`background`), chrome (a scroll bar), an attachment (a tooltip), a label (`buttonText`), or the widget a scroll or zoom area holds, a child like any other |
| `GUITypes` (`WidgetType`, `Holding`) | how each built-in type draws: how it holds its children (row, column, flow, grid, scroll, else by their anchors), whether it clips them, fills its holder or centers its own text, and the games in which a hidden child of it takes no room |
| `UndeclaredTypes` | the built-in types a game draws though no file of its install declares them |
| `GUIHolders` | the window the engine lays a window made from a type out in (EU5's lateral views) |
| `GUIOpeningStates` | which states run as a window opens, and the keys of a state that are its own rather than end values |
| `GUIAnswered` | the data calls the preview answers from the index (`GetDefine`), and the database each reads |
| `GUIBooleans` | the data functions a binding combines conditions with |
| `GUIPictureKeys`, `GUIIconPathSuffix` | the keys an entry names its picture by, and the ending of the defines that name icon folders |

## The coat of arms rules (`coa-rules.go`)

What a game does with a coat of arms beyond its syntax, which `draw`'s `coat.go` reads; what the page applies to the
pixels (how each texture kind is colored, which channel each mask region is, which way a rotation turns) is
`frontend/src/draw/recolor.ts`'s, each fact in one of the two.

| table | says |
|---|---|
| `CoatDatabases` | the databases a coat reads: its own, and the named colors its slots name |
| `CoatFolders` | the folder each game keeps its coats in, from its stages' folder: where vanilla's are and a mod's new ones go (EU5's under `main_menu/`) |
| `CoatFieldFile` | the file whose first atlas' `tile_size` is the field a coat is drawn on |
| `CoatTextureFolders` | the folder each texture key's files are under (`pattern`, `colored_emblem`, `textured_emblem`) |
| `CoatDepthInFront` | which way `depth` orders a coat's layers: false, a larger depth draws behind (observed) |
| `CoatShading` | the texture a game shades every coat with (CK3's cloth); a game not listed shades none |
| `CoatFrames` (`CoatFrame`) | the frames a game's windows cut its coats to, each the mask its GUI names (CK3's title and dynasty) |

## Games (`game.go`)

`registry` is the table of supported games: names, wiki endpoint, script and stage roots, descriptor kind, Documents
folder, Steam app id and library, the first-wins sentence the pages show, the new-mod folders and the data-types
command. `Get` and `All` read it. The rest finds installs through Steam's library folders, loads a game's version, and
locates its user-data and dump folders on Windows and Linux.
