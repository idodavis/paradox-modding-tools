# game

The only game-aware package: what is true of each game, and the language rules of Paradox script as data.

| file | holds |
|---|---|
| `lang-rules.go` | the language rules of Paradox script as tables, and each game's file `Layout` |
| `game.go` | each game's identity (`GameInfo`), install and version detection, user-data and dump folders, Steam library paths |

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
| `Bools`, `Colors`, `DumpWords`, `MisspelledFolders` | how yes/no and colors are written, the dumps' words the engine decides by, folder names the game never reads | the readers, diagnostics |
| `EntryMode`, `GameInfo.Mode` | the words before a definition's name (`REPLACE:`, `INJECT:`), what each does, and the split of one off a name | the entry detector, diagnostics, `index.Winner` |
| `FileTypes` | how `.txt`, `.asset`, `.font` and `.gui` files name entries, what their entries' blocks hold, and the keys that name another entry | the entry detector |
| `Layout` (`Descriptor`, `Identity`, `Purpose`) | per game, one row per database that needs one: how its entries are identified, what it is for, wrappers, the keyword that declares an entry inline, dated blocks, what an entry's block holds, the words a call resolves by, parameters, references and names; nested databases by their `Parent` and key path `Within`; entry modes; first-wins databases | the schema, the index |

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

## Games (`game.go`)

`registry` is the table of supported games: names, wiki endpoint, script and stage roots, descriptor kind, Documents
folder, Steam app id and library, the first-wins sentence the pages show, the new-mod folders and the data-types
command. `Get` and `All` read it. The rest finds installs through Steam's library folders, loads a game's version, and
locates its user-data and dump folders on Windows and Linux.
