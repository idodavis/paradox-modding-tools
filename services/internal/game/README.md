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
| `Shape` | the kinds of block: fields, triggers, effects, arguments, value, description, unknown, setup (a history or start block: its fields, then effects), widget (GUI) | everything |
| `Shapes` (`ShapeRule`) | per shape: the key rules it tries in order, the word its calls resolve by, the commands it allows | the walker, completion |
| `KeyRule`, `KeyRules` (`KeyRuleRow`: `Where`, `HoldsFrom`, `ScopeVerb`) | the ten ways a key can be read, and for each where its block reads, what decides what the block holds, and how its scope follows; whether a rule fits is the walker's test | the walker |
| `Kinds`, `KindWords` | what a declared command is (effect, trigger, link) and the dumps' word for each | dumps, hover |
| `Contents`, `ContentsOrder` | what a command's own block holds, first rule that fits | the walker |
| `Constructs` (`Construct`; `ConstructOf`, `ListIterator`, `IsOperator`, `AnyKey`) | the keys the language gives a fixed shape to and no dump declares: random_list's branches, switch's cases, the list iterators, a description's markers, trigger and leaf, a value block's operators, a widget's blocks; the first row that fits wins | the walker, the schema's stamps, completion, hover |
| `Name`, `KeyNames`, `ValueNames` | what a key or a value word defines or refers to | the index |
| `NameCommands`, `SavedScopes` | commands whose value makes or reads a script-made name (`save_scope_as`, `set_variable`, `has_variable`) | the index, the schema's prefixes |
| `DynamicDescription` | the words of a dynamic description (`first_valid`, `triggered_desc`, `desc`), as the pages read one; the walker reads them as construct rows | the event graph's detail, the walker's description test |
| `Bools`, `Colors`, `GlobalType` | how yes/no, colors and data calls are written | the readers, hover |
| `EventPortraits`, `OptionFlags` | the keys an event names its portraits with (by their last word, and the one that names the character), the yes-or-no words an option is marked with | the event graph's detail |
| `DumpWords` | the dumps' words the engine decides by (`Iterate through`, `none`/`all`) | the dump reader |
| `MisspelledFolders` | folder names the game never reads | diagnostics |
| `EntryMode`, `GameInfo.Mode` | the words before a definition's name (`REPLACE:`, `INJECT:`), what each does, and the split of one off a name | the entry detector, diagnostics, `index.Winner` |
| `FileTypes` | how `.txt`, `.asset`, `.font` and `.gui` files name entries, the block kind their entries hold (`Body`) and the keys that name another entry (`Refs`: `using`, `layer`) | the entry detector, the index |
| `Layout` (`Descriptor`, `Identity`, `Purpose`) | per game: one row per database that needs one: how its entries are identified (`Identity`: by key, by a name key's value, one per file, by a table's column, none), what it is for (`Purpose`: events, on_actions, lists, functions, concepts, history and the database it sets up, `Of`), wrappers, the keyword that declares an entry inline, dated blocks, what an entry's block holds (`Body`), the words a call resolves by (`Callers`), parameters, references and names; a nested database by its `Parent` and the key path `Within`; entry modes; first-wins databases. `At` is a path's row, `Nested` a file's nested rows, `NameOf` the database a path makes | the schema, the index |

**How a key reads.** In a block of kind K, the walker tries `Blocks[K].Keys` in order; the first step
that fits decides: a vanilla scripted entry's name is a call, then a declared position, a declared
command, a value operator, a link, a position vanilla writes, the position itself, else a call. An
arguments block reads no links; a GUI widget's keys are positions only. To change how keys read in
a kind of block, reorder or edit that row.

**Layouts.** A folder row holds for its path and everything below it; a path takes the longest row at
or above it. `sharedFolders` and `sharedNested` are the rows every game has; each game's `layouts`
entry adds its own over them. A database is found in the install, one per script folder (a `common/` subfolder joins its
ancestor's), so a folder
needs a row only to say what its files cannot: how entries are named, what wraps them, what calls
them.

## Games (`game.go`)

`registry` is the table of supported games: names, wiki endpoint, script and stage roots, descriptor
kind, Documents folder, Steam app id and library, the first-wins sentence the pages show, the new-mod
folders and the data-types command. `Get` and `All` read it. The rest finds installs
through Steam's library folders, loads a game's version, and locates its user-data and dump folders on
Windows and Linux.
