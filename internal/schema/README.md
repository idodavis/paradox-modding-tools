# schema

The package models the language of one game version: which databases exist, what each key takes and which commands
run in which scope. It builds the model from the dumps, the docs and the game's own files, and it owns the one walk over
a file that every reader of script is a visitor on.

`Schema` is the only description of the language. The dumps reader, the docs reader and the vanilla count fill it, and
nothing else describes a key. The rules no source states are `game`'s rows, which the schema copies onto its databases
and the walk reads directly. A schema is built once for each version of an install and its dumps, and cached as gob.

| file | holds |
|---|---|
| `schema.go` | the shape: `Claim` and `Claims`, `ValueType` and its constants, `Expected`, `Vanilla`, `Field`, `Command`, `Example`, `Database`, `ScopeType`, `DataFunction`, `DataType`, `Modifier`, `OnAction`, `ScriptedDefinition`, `SourceState`, `Schema`; `Build`; the databases and their callees; `SplitStage`, `IsLocReplacement`; the lookups (`FindDatabase`, `FindDatabaseByClass`, `FindCommand`, `ResolveLink`); the data calls (`ResolveCallTypes`, `FindFunction`, `ResolveMacro`, `ResolveDatacontext`, `ListCallReferences`, `ResolveTypeDatabase`); `ToPhrase`, `MatchValue` |
| `dumps.go` | the dumps reader: `DumpCommand`, `Dumps`, `ReadDumps`, `FindDumpsAt`; the commands, the arguments a usage writes, the list families' shared arguments; the backup for a missing section |
| `docs.go` | the docs reader, `docReader`: which files are docs, and the blurbs, types, requirements, defaults, choices and scopes their prose states |
| `vanilla.go` | the vanilla count, `CountVanilla`: the `declarer` that records what script and GUI files call by name, the `counter` and its `holder`s that count what the game's own files write, and the claims the counts support |
| `walk.go` | the walk: `Category` and its constants, `Definition`, `Reference`, `Saved`, `Context`, `Visitor`; `Walk`, `WalkTo`, `ListNamespaces`, `ParseCall`; the `walker`, its `frame`s and the `match` a key fits |
| `shipped.go` | the shipped backup of each game's dumps, `LoadShipped`, embedded from `shipped/*.gob` |
| `gen/main.go` | the program that writes `shipped/<game>.gob` from the dumps of an install that `PMT_TEST_INSTALL_<GAME>` names |

## The shape

A `Database` is one database of the game: the `Field` tree of its definitions, and the `game` rows that govern it
(its `Row`, its `Callable` and its `Precedence`). A database is built for each folder a script file belongs to, each
folder or file a row names that the install has, each nesting (a database inside another's definitions, such as a
culture's dialects), each GUI database, and each database only the walk names. A nested database is named by its
parent's name, a dot and its own.

A `Field` is one key that may be written in a definition, with what it takes and the fields that may be written inside
it. Its `Children` are keyed by the key as written, or by a pattern: `<number>` (`game.NumberKey`) for any number, and
`<any>` (`game.AnyKey`) for any key. A database's root field is the whole definition block. A field carries its
`Claims`, the highest-ranked `Source` that declares it, and its `Vanilla` counts.

A `Command` is one effect, trigger or link the dumps declare: what declares it, the scopes it runs in, the scope a link
leads to, the keys it takes as a field, its blurb and usage, and the places the game's own files write it. The rest of
the schema is the dumps' tables as they are: `Scopes`, `Saved`, `Modifiers`, `OnActions` and `DataTypes`, and the
link `Prefixes` with the database each one's names are in.

## Sources

Every fact the schema learns has a `game.Source`, and a lower source outranks a higher one: the rows, the dumps, the
executable, the logs, the docs, the wiki, the game's own files, and inferred last. Only the dumps, the docs and the wiki
can accuse a mod (`Source.CanAccuse`). A fact learned from the game's own files completes a hover card, marked as
inferred, and never fails a verdict. A reference that only such a fact makes is `Reference.Inferred`.

`Build` fills the schema in that order of trust. It builds the databases from the folders and the rows, reads the dumps
and then the docs, counts the game's own files, and last gives a field with no blurb the one the docs write for the
same key elsewhere in its database.

## Claims

A field's `Claims` are its facts: its value, shape, scope, requirement, default, blurb and keys. Each is a `Claim`,
which holds one value and the `game.Source` that states it. A higher-ranked source replaces a lower one, and a claim
that states nothing takes any source (`Claim.resolve`). `Field.take` gives a field another field's claims where they
outrank its own, all but `Keys`. The docs reader gives a database's fields what a doc says through it, and the dumps
reader merges the arguments of one list family (any_, every_, random_ and ordered_) through it.

`CountVanilla` states what no source does, field by field, in `resolveClaims`. First comes the shape its blocks tell
(`resolveShapeClaim`). Where its blocks call scripted definitions, it is the one shape every call allows. Otherwise it
is the one shape the keys written in its blocks tell, and a value key wins over triggers. Shapes that disagree, or a
key that fits more than one kind of block, give unknown. Then come its keys (`resolveKeys`), then its value.

A stated value is kept, except a reference with no database, a block or a text, which take the reference vanilla shows.
A block or a text never takes a reference to the game concepts, whose names are common words. Vanilla shows a reference
when one database defines every value written at the field, as `resolveReference` finds it, and none of the values is
a number, a date or a yes-or-no word. Where vanilla shows no reference and no source states a value, a field always
written as a block is a color if every block was one, and else a block. A field written only as scalars gets the first
type in `valueTests` that all its values fit.

`resolveReference` gives a reference when one database defines every value. A value that holds a parameter is left
out. A database whose row names another as its target counts as that one, and so do the coats of arms where a database
that holds coats defines the values too. Where no one database defines them all, a database with nested ones may,
counted together with them, as a language and its dialects are (`Expected.Nested`). The game concepts, then the
localization, count only when nothing else does.

`Claims.Keys` is the database a block's keys name, as a culture's opinions name cultures. It is set when every key that
only vanilla writes in the block is a definition of one database. Those keys' fields are then folded into the field of
`<any>` (`game.AnyKey`), and the walk reads a field key there as a reference to that database (`findMatch`). A key a
source declares stays as it is. Left out are a block whose shape holds commands (`game.Shapes[…].Allows`), and keys
that are game concepts or messages, definitions of a GUI database (whose names are common words, such as value), or
the definitions of a database with its nested ones.

## The dumps

`ReadDumps` reads the script_docs and data types dumps under the given folders: the effects, triggers and event
targets logs, the modifiers, the on_actions, the event scopes, and the data types files. `FindDumpsAt` gives when they
were written, the time of the oldest dump file, without reading one. A section the install lacks is taken from the
shipped backup, one section at a time. The schema's `Dumps` is a `SourceState`: whether the dumps came from the install,
the shipped backup or neither, when they were written, and whether they are older than the install.

A command named by several dumps is one `Command`. Its arguments are the keys inside every block of its usage keyed by
its name, and the keys its prose writes as `key = value`. A link that requires data is a prefix, and a trigger whose
traits name a database's class takes that database's names.

## The docs

A doc is an `.info`, `.md` or readme file, or a `.txt` file whose name marks it as one, in a database's folder. A
`.txt` file with more script lines than prose is data and is skipped. The `docReader` reads a doc line by line, by
pattern: prose is no valid script, so nothing is parsed. Its example's statements are fields, and the comments beside
and above them are their text. That text states a blurb, a type by the words of `game.DocTypes`, a requirement, a
default, choices and a scope. A later doc only fills what an earlier one left out.

## The vanilla count

`CountVanilla` walks every script, GUI, asset, font and GFX file of the install, the DLC folder left out, and reads
every localization file for its message keys. It runs in two passes. The `declarer` pass reads the files that declare
what script and GUI files call by name, so that a later key under a call counts as a call: the scripted definitions
with the parameters their bodies write, and the widget types. The `counter` pass then counts, on `files.Workers`
workers each with fields of its own, what the files write: the keys under each database's definitions, each value with
its count, the shapes and proofs a block's keys tell, the names after each prefix, and where each command and data
function is written. The workers' counts are added together, and `resolveCounts` gives the schema what they support:
the claims above, the prefixes' databases, each command's examples and the scopes the game runs it in.

## The walk

`Walk` goes through a file's tree once and calls a `Visitor` at every statement it enters and leaves. At each one it
decides what the schema and the game's rules say: the field, the scope, the shape, the definition and the references.
It fills one `Context` per depth and reuses it, so a visitor copies what it keeps. `WalkTo` is `Walk` restricted to
one offset: it offers the visitor only the statements that cover it, and still resolves the statements before it
silently, to learn what they saved. Nothing else in the app walks a file.

The `walker` keeps a `frame` per depth: its context, the label last read there, and the key path from the definition
the statement is in. The context carries the statement's label (`Context.Label`) and the key path to the block that
holds it (`Context.Path`), so a visitor can match the statement against a key path as `game.MatchKeyPath` reads one.
Each statement is resolved in one of two ways.

- **It defines a name.** A constant, a nested definition, a GUI declaration, a top block of an asset file, a name an
  events file declares inline, or a script definition named as its database's row says (`NamedBy`). A window at the
  top of a GUI file that writes no name takes its file's name where it is the file's only keyed block
  (`findWindowLabel`). An event must follow a namespace its file declares. The command that saves a scope or a
  variable makes a definition of the name it saves, and the value at an alias key is a second name of its definition.
- **Its key fits a rule.** The block's shape lists its rules in order (`game.Shapes[…].Rules`), and the first one the
  key fits wins (`findMatch`). The rule's `game.KeyRules` row says where the block's fields come from, what decides
  its shape, and how its scope follows. A keyword of the language (`game.Keywords`) adjusts what the rule gave.

A command fits only where the block's shape allows the kind it is declared as, and an effect never fits inside a
trigger block (`Context.FindDeclaration`). A statement below a block of triggers is `Context.Conditional`, and one
below a keyword whose block is shown in the tooltip and never run (`game.Keyword.Preview`, as `show_as_tooltip`'s is)
is `Context.Preview`. A link or a command that leads to a scope moves the scope there and pushes
the one it leaves onto `Previous`, and a chain of links is followed link by link. A definition's root scope is its
event's own, the dumps' for an on_action, or its docs' for the rest. Where a database gives its blocks a run order, the
walk first reads the blocks silently in that order, so that a block starts with the scopes saved by the blocks that run
before it.

The references a statement makes are found here and nowhere else: the names its key calls, the names after a link's
prefixes, the database its field claims, the database a key path row gives, a coat's template list, an event id, a
script value, a quoted data-call argument, a macro's expansion and a `list "x"` value.

## Data calls

A data call's steps are read by the dumps' data types. `ResolveMacro` expands a call that opens with one of the game's
data binding macros. `ResolveCallTypes` gives the type each step leads to from a GUI block's data context, and
`FindFunction` the function one step calls. `ResolveDatacontext` gives the data context type a block's datacontext key
sets. `ListCallReferences` gives the names a call's quoted arguments hold, each in the database of the type its step
returns, or as `game.GUI.CallTargets` says. `ResolveTypeDatabase` finds the database that holds a data type's names by
the type's words, as `ToPhrase` writes them. Where two databases have the same words, the one whose own name is those
words takes them (`isNamedExactly`: EU5's `institution` over `setup/start.institution`), and else they name neither.

## The shipped backup

`shipped/*.gob` holds, for each game, what its dumps declare, written by `gen/main.go` from an install's dumps. It is
read only where an install's dump section is missing. It never holds the game's own words or a built schema.

## Terms

- **Schema**: the language of one game version.
- **Database**: one database of the game, with the field tree of its definitions and its rows.
- **Field**: one key that may be written in a definition, with what it takes.
- **Claim**: one fact about a field, with the source that states it.
- **Claims**: every fact the sources state about one field.
- **Expected**: what a field's value must be.
- **ValueType**: the kind of value a field takes.
- **Vanilla**: what the game's own files write at a field, counted.
- **Command**: one effect, trigger or link the dumps declare.
- **ScopeType**: one kind of scope, with whether triggers and effects run in it.
- **DataType**: one type of the data binding language, with its functions.
- **DataFunction**: one function of a data type.
- **Modifier**: one modifier the dumps declare.
- **OnAction**: one on_action the dumps declare, with its scope.
- **ScriptedDefinition**: a scripted definition the game defines, with the parameters it takes.
- **Example**: one place the game's own files write a command or call a data function.
- **SourceState**: where one source's data came from and how fresh it is.
- **Dumps**: everything the dumps declare.
- **DumpCommand**: one effect, trigger or link as its dump lists it.
- **Context**: everything the walk has decided at the statement it is on.
- **Visitor**: what the walk calls at each statement it enters and leaves.
- **Definition**: a name a statement defines, with the facts that decide which definition the game keeps.
- **Reference**: a name a key or a value refers to, in a database.
- **Saved**: one scope the definition saved, with its type.
- **Category**: the color category of a key or a value.
