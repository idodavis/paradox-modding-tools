# game

The only game-aware package. Two files:

| File | Holds |
|---|---|
| `lang-rules.go` | the language rules of Paradox script, as tables, and each game's file `Layout` |
| `game.go` | each game's identity (`GameInfo`), install and version detection, user-data and dump folders, Steam library paths |

## The language rules (`lang-rules.go`)

The schema's walker (`lang/schema/read.go`) reads script by these tables and nothing else; the index
(`lang/index/walk.go`) names words by them; completion and hover read them too. A rule changes here,
in one row, never as a branch in a reader.

| Table | Says | Read by |
|---|---|---|
| `Block` | the kinds of block: fields, triggers, effects, arguments, value, description, unknown | everything |
| `Blocks` (`BlockRule`) | per kind: the order a key's readings are tried (`Step`), the word its calls resolve by, the commands it holds | the walker, completion |
| `Kinds`, `KindWords` | what a declared command is (effect, trigger, link) and the dumps' word for each | dumps, hover |
| `Contents`, `ContentsOrder` | what a command's own block holds, first rule that fits | the walker |
| `Structurals` | keys with a fixed block kind inside a command: random_list's branches, switch's cases | the walker |
| `ScopeRules` (`When`, `To`) | how the scope changes going into a block, first row that fits | the walker |
| `Name`, `KeyNames`, `ValueNames` | what a key or a value word defines or refers to | the index |
| `NameCommands`, `SavedScopes` | commands whose value makes or reads a script-made name (`save_scope_as`, `set_variable`, `has_variable`) | the index, the schema's prefixes |
| `ListIterators` | the prefixes a scripted list is iterated by, and their block kind | the walker, the index |
| `ValueOperators` | a value block's operators, and whether each takes a number | the walker, completion |
| `DynamicDescription` | the shape of a dynamic description (`first_valid`, `triggered_desc`, `desc`) | the walker |
| `Bools`, `Colours`, `Placeholders`, `UsagePlaceholder`, `GlobalType` | how values, colours, dump placeholders and data calls are written | the readers, hover |
| `MisspelledFolders` | folder names the game never reads | diagnostics |
| `EntryMode` | the words before a definition's name (`REPLACE:`, `INJECT:`) and what each does | the entry detector, `index.Winner` |
| `FileTypes` | how `.txt`, `.asset`, `.font` and `.gui` files name entries | the entry detector |
| `Layout` (`Folder`, `Nested`, `MapTable`) | per game: folder rows (naming, wrappers, history, scripted folders, references), entries nested in entries, the map table, entry modes, first-wins databases | the schema, the index |

**How a key reads.** In a block of kind K, the walker tries `Blocks[K].Keys` in order; the first step
that fits decides: a vanilla scripted entry's name is a call, then a declared position, a declared
command, a value operator, a link, a position vanilla writes, the position itself, else a call. To
change how keys read in a kind of block, reorder or edit that row.

**Layouts.** A folder row holds for its path and everything below it; a path takes the longest row at
or above it. `sharedFolders` and `sharedNested` are the rows every game has; each game's `layouts`
entry adds its own over them. A database is found in the install, one per script folder, so a folder
needs a row only to say what its files cannot: how entries are named, what wraps them, what calls
them.

## Games (`game.go`)

`registry` is the table of supported games: names, wiki endpoint, script and stage roots, descriptor
kind, Documents folder, Steam app id and library, the first-wins sentence the pages show, the new-mod
folders and the data-types command. `Get`, `All` and `FirstWinsAt` read it. The rest finds installs
through Steam's library folders, reads a game's version, and locates its user-data and dump folders on
Windows and Linux.
