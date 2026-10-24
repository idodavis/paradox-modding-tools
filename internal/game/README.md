# game

The only game-aware package: the table of supported games, the shared tables of what every game's script, GUI and
coat of arms files mean, and where the games are installed.

Every fact is a row of a table or a field of a game's row, written as a literal: no row is built by a function. A
column whose values are words lists them in its comment and names the one function that reads them, so the count of
readers that switch on a word is the count of word columns, each in one place. A row's evidence (a `.info` file, the
wiki, the executable, a shader) is its trailing comment; nothing reads it at run time. The
`Source` type ranks the sources the schema learns claims from and is the schema's, not the rows'.

| file | holds |
|---|---|
| `game.go` | `Game`: a game's names, wiki, stages, descriptor kind, Steam app id and library, new-mod folders, how each OS starts it (`Program`), its error log (`ErrorLog`), and what is its own beside the shared tables: its `Databases`, `Nestings`, `Precedences` and `References`, its `Events` (the root scope's key and the `Picture`), its `EntryModes` and `ModeFolders`, its concept prefix, its GUI holders, undeclared types and hidden-room types, its coat folder, shading and designer frames; `Source`; the install roots the game mounts (`InstallRoots`); `LookupGame`, `ListGames` |
| `databases.go` | the tables of databases, one per kind of fact: `Databases` (where definitions are written and how named), `Nestings` (databases inside another's definitions), `Callables` (what script and GUI files call by name), `FileTypes` (what an extension says), `Precedences` (how definitions combine), `References` (what key paths name; a game's own beside them), `EntryModes`, the `Picture` chains, the databases no definition carries, `Constant` (the file's own `@name` constants), `ClassDatabases`, the loc replace folders and fill-ins; `ToDatabase`, `FindDatabase`, `FindNestings`, `MatchKeyPath` (a key path's segments, globs, `<number>` and `**`), `LookupPrecedence`, `SplitMode`, `DescribeDefinition` |
| `language.go` | the language: `Shapes` (how a block of each shape reads its keys, by the `KeyRules` words) and the `Shape…` names Go code writes (`ShapeUnknown`, `ShapeFields`, `ShapeArguments`, `ShapeTriggers`, `ShapeEffects`, `ShapeValue`, `ShapeWidget`), `Keywords` (the keys the language gives a shape to and its operators, each with the sentence a hover card shows), `ScopeLinks` (root, prev, this), `LinkMakers` (for each link of the dumps that reads a name back, the commands that make its names), `DocTypes` (the docs' type words), the dumps' words, `Bools`; `LookupKeyword` (a number or a date fits `<number>`, `IsNumberKey`), `LookupLinkMaker`, `MatchListIterator`, `IsOperator`, `ClassifyKey` |
| `gui.go` | `GUI`, one literal: which words declare a GUI file's databases and what their names start with (`DatabasePrefix`, `WidgetDatabase`), which keys name its widgets, the slots, how each built-in `WidgetType` draws, the opening states, the hidden keys, the calls the Visual view answers, the picture keys |
| `coat.go` | `Coat`, one literal: the databases a coat reads (coats, named colors, template lists), its texture folders, which way depth and rotation turn, how each texture kind is colored (`CoatRecolor`), the mask regions, the color slots, the placement keys, how a key is edited, what a new coat writes, the frame mask |
| `colors.go` | the `Colors` rows, `ParseColor` and `EncodeColor` |
| `install.go` | where a game is on this machine: its installs through Steam's library folders, its version (the launcher's settings, else the revision log of the game's last start when that came after the install's last update, else Steam's build), its user-data and dump folders, its descriptor file, when an install was last updated, and the program that starts it in debug mode (`ResolveProgram`); `DLCFolder` |

## Lookups

A game's own rows are tried before the shared table of the same name: `FindDatabase(folder)` reads the game's
`Databases` then `Databases`; `FindNestings(rel)` concatenates; `LookupPrecedence(database, folder)` reads the game's
`Precedences`, then `Precedences`, then `modePrecedence` where the folder takes entry modes (`ModeFolders`), then
`defaultPrecedence`. A database's name is derived in one place, `ToDatabase`: the path without `common/` and without a
file's extension; a nested database is its parent's name, a dot and its own. `Callables` and `FileTypes` are maps the
reader indexes directly.

## The words

Each word column's comment lists its words and its reader. `Shape` (on a `Database`, a `Callable`, a `FileType`, a
`Keyword`, the schema's claims and the walk's context) is a `Shapes` key: fields, triggers, effects, arguments, value,
unknown, setup, widget, description; `enclosing` in a keyword means the holder's. `KeyRule`'s three columns are the
walk's: where the block's fields come from, what decides its shape, how its scope follows. `ShapeRule.Values` and
`ShapeRule.Blocks` are the walk's: the database every value in a block of the shape names, and the shape of a block
whose key gives none. `Precedence.Keeps` and `Modes` are `index.FindWinner`'s. `Database.NamedBy` is the walk's and
the scan's (`call` names a definition by the first step of the call its `NameKey` writes), and so are
`Database.RunOrder` (the order the walk follows to know the scopes a block starts with), `Database.AliasKeys` (the keys
whose value is a second name of the definition, which the index records as an alias and the iterators over a geographic
region are named by), and `Database.Prefixes` with `Database.Separator` (the link prefixes that name a definition, and
the character that ends the name in a prefixed reference). `DocType.Values` marks the docs' terms that take a script
value. `WidgetType.Holding` and `WidgetSlot.Role` are
`picture.ts`'s. `CoatPlace.Part` is `coat.ts`'s. `Keyword.Shows` is the walk's.
