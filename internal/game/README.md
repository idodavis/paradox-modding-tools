# game

The only game-aware package: the table of supported games, the shared tables of what every game's script, GUI and
coat of arms files mean, and where the games are installed.

Every fact is a row of a table or a field of a game's row, written as a literal: no row is built by a function. A
column whose values are words lists them in its comment and names the one function that reads them, so the count of
readers that switch on a word is the count of word columns, each in one place. A row's evidence (a `.info` file, the
wiki, the executable, a shader, "observed" in the app) is its trailing comment; nothing reads it at run time. The
`Source` type ranks the sources the schema learns claims from and is the schema's, not the rows'.

| file | holds |
|---|---|
| `game.go` | `Game`: a game's names, wiki, stages, descriptor kind, Steam app id and library, new-mod folders, how each OS starts it (`Program`), its error log (`ErrorLog`), and what is its own beside the shared tables: its `Databases`, `Nestings`, `Precedences` and `References`, its `Events` (the root scope's key and the `Picture`), its `EntryModes` and `ModeFolders`, its concept prefix, its GUI holders, undeclared types and hidden-room types, its coat folder, shading and designer frames; `Source`; `LookupGame`, `ListGames` |
| `databases.go` | the tables of databases, one per kind of fact: `Databases` (where definitions are written and how named), `Nestings` (databases inside another's definitions), `Callables` (what script and GUI files call by name), `FileTypes` (what an extension says), `Precedences` (how definitions combine), a game's `References` (what key paths name), `EntryModes`, the `Picture` chains, the databases no definition carries, `ClassDatabases`, the loc replace folders and fill-ins; `ToDatabase`, `FindDatabase`, `FindNestings`, `LookupPrecedence`, `SplitMode`, `DescribeDefinition` |
| `language.go` | the language: `Shapes` (how a block of each shape reads its keys, by the `KeyRules` words), `Keywords` (the keys the language gives a shape to and its operators, each with the sentence a hover card shows), `SavedNames` (the commands that make or read a saved scope or variable), the dumps' words, `Bools`; `LookupKeyword`, `MatchListIterator`, `IsOperator`, `ClassifyKey` |
| `gui.go` | `GUI`, one literal: which words declare a GUI file's databases, which keys name its widgets, the slots, how each built-in `WidgetType` draws, the opening states, the hidden keys, the calls the Visual view answers, the picture keys |
| `coat.go` | `Coat`, one literal: the databases a coat reads, its texture folders, which way depth and rotation turn, how each texture kind is colored (`CoatRecolor`), the mask regions, the color slots, the placement keys, how a key is edited, what a new coat writes, the frame mask |
| `colors.go` | the `Colors` rows, `ParseColor` and `EncodeColor` |
| `install.go` | where a game is on this machine: its installs through Steam's library folders, its version, its user-data and dump folders, its descriptor file and when an install was last updated |

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
walk's: where the block's fields come from, what decides its shape, how its scope follows. `Precedence.Keeps` and
`Modes` are `index.FindWinner`'s. `Database.NamedBy` is the scan's. `WidgetType.Holding` and `WidgetSlot.Role` are
`picture.ts`'s. `CoatPlace.Part` is `coat.ts`'s. `Keyword.Shows` is the walk's.

## Carried over and dropped

The tables carry every row of the rules they replace (one `DatabaseRule` union of 35 fields built by ten functions, a
13-column `Keyword`, five iota enums, a `Source` field on 123 rows). Dropped, each named here: `Source` as a field
(evidence is a comment; nothing read it); `ReferenceRule.Folder` (no row used it); `Keyword.Scope` (a list iterator's
block runs in the list's item scope, the walk's rule, unknown where the list's command has no target); `Keyword.Leaf`
(a description's `desc` holds a description: a scalar is the text itself); `Game.FirstWins` (a sentence for a page,
which the page builds from the precedence rows); `Game.DocsFolderName` (it is the game's `Name`); `Game.ScriptRoot`,
`BaseLanguage` and `DumpFolders` as fields (the same in every game: package constants); `DefaultPrecedence` as a field
(last, in every game); `HoldsCell` (no row used it); `GameList` (unused).
