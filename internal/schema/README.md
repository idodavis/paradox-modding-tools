# schema

The package models the language of one game version: which databases exist, what each key takes and which commands
run in which scope. It builds the model from the dumps, the docs and the game's own files, and it owns the one walk over
a file that every reader of script is a visitor on.

`Schema` is the only description of the language. The dumps reader, the docs reader and the vanilla count fill it, and
nothing else describes a key. The rules no source states are `game`'s rows, which the schema copies onto its databases
and the walk reads directly. A schema is built once for each version of an install and its dumps, and cached as gob.

| file | holds |
|---|---|
| `schema.go` | the shape: `Claim` and `Claims`, `ValueType` and its constants, `Expected`, `Vanilla`, `Field`, `Command`, `Example`, `CommandPattern`, `Database`, `ScopeType`, `DataFunction`, `DataType`, `Modifier`, `OnAction`, `ScriptedDefinition`, `SourceState`, `Schema`; `Build`; the start setups, the databases and their callees; `JoinNested` and `SplitNested`; the lookups (`FindDatabase`, `FindDatabaseByClass`, `FindCommand`, `ResolveLink`, `IsLink`, the key's link test `isKeyLink`, a command's name with an argument in parentheses `findKeyCall`, the command patterns that make a key `findPatterns`); the data calls (`ResolveCallTypes`, `FindFunction`, `FindUnlisted`, `ResolveMacro`, `ResolveDatacontext`, `ListCallReferences`, `ResolveTypeDatabase`); the databases each `game.Contents` kind names (`Events`, `Messages`, `Lists`, `Concepts`, `ModifierTypes`, `FireDatabases`); the folders under which the game reads a mod's files (`TopFolders`: the first folder of each of the game's files, less its stage folder, in lower case); `ToPhrase`, `DropGetter` (a function's name without its getter's word), `DescribeStep` (a data call's step in words, a call that names a definition read as that name), `MatchValue`, `Database.IsCaseless` |
| `dumps.go` | the dumps reader: `DumpCommand`, `Dumps`, `ReadDumps`, `FindDumpsAt`; the commands, the arguments a usage writes, a trigger's Traits line (`resolveTraits`), `game.CommandKeys`, the keyword rows under a command, the list families' shared arguments, the union of an effect's and a trigger's; the backup for a missing section |
| `docs.go` | the docs reader, `docReader`: which files are docs and which database each describes, and the blurbs, types, requirements, defaults and choices their prose states |
| `vanilla.go` | the vanilla count, `CountVanilla`: the `counter` and its `holder`s that count what the game's own files write, and the claims the counts support (`resolveClaims`); the command patterns, the prefixes' databases, and how many places of a name are kept as examples (`ExampleCount`) |
| `scripted.go` | the scripted definitions and the modifier types that files declare: the `declarer`, `parameterRe`; the mods' (`SetScripted`); a call's lookup through the game's and the mods'; the parameters a body writes (`ListParameters`) and those it saves names under; what a modifier type's name takes |
| `definitions.go` | what a statement defines: a constant, a script definition named as its database's row says, an event of a declared namespace, a GUI declaration or window, a top block of an asset file, and a nested definition |
| `walk.go` | the walk: `Category` and its constants, `Definition`, `Reference` with `ToID`, `Saved`, `Context` with `FindDeclaration`, `FindArguments` and `HoldsArguments`, `Visitor`; `Walk`, `WalkTo`; the `walker`, its `frame`s and the `match` a key fits by the rules of its block's shape; the scopes, the saved names and the aliases; the references a key and a value make |
| `shipped.go` | the shipped backup of each game's dumps, `LoadShipped`, embedded from `shipped/*.gob` |
| `gen/main.go` | the program that writes `shipped/<game>.gob` from the dumps of an install that `PMT_TEST_INSTALL_<GAME>` names |

## The shape

A `Database` is one database of the game: the `Field` tree of its definitions, and the `game` rows that govern it
(its `Row`, its `Callable` and its `Precedence`). A database is built for each folder a script file belongs to, each
folder or file a row names that the install has, each nesting (a database inside another's definitions, such as a
culture's dialects), each start setup, the folder a bookmark names, with the start setup's nestings under it, each GUI
database, and each database only the walk names. A nested database is named by its parent's name, a dot and its own.
A nesting with a `NameKey` names a definition by that key's value, as the three kinds of block in
`gfx/portraits/accessory_variations` are each named by their `name`. A definition in a nested database whose parent
writes a separator is named by its holder's name, the separator and its key (`JoinNested`, `SplitNested`): a define is
`NAI|X`, as script reads it, so two groups' same key are two names. A data call's argument in such a database takes
the argument before it as its holder, as `GetDefine('NAI', 'X')` does, and a script link `define:NAI|X` (or
`array_define:`) names the group `NAI` at its span and the define `NAI|X` at the key's (`listLabelReferences`).

A `Field` is one key that may be written in a definition, with what it takes and the fields that may be written inside
it. Its `Children` are keyed by the key as written, or by a pattern: `<number>` (`jomini.NumberKey`) for any number,
and `<any>` (`game.AnyKey`) for any key. A database's root field is the whole definition block. A field carries its
`Claims`, the highest-ranked `Source` that declares it, and its `Vanilla` counts. `Expected` is what its value must
be: a `ValueTarget` is a link to the scope in `Expected.Scope`, or a name of `Expected.Database` where it is set, and
`Expected.Values` lets a number be written as a script value.

A `Command` is one effect, trigger or link the dumps declare: what declares it, the scopes it runs in, the scope a link
leads to, the keys it takes as a field, the key its parenthesized argument stands for (`ArgumentKey`), its blurb and
usage and the places the game's own files write it. A link may be `Wild` (a pattern no written name matches) and `Bare`
(written as its name alone, not only as `name:data`). The rest of the schema is the dumps' tables as they are: `Scopes`, `Saved`, `Modifiers`, `OnActions` and `DataTypes`, and the link `Prefixes` with
the database each one's names are in. The databases add `TypeDatabases` and `InlineDatabases`, and the count and the
declarers add `ModifierFields`, `CommandPatterns`, `ScriptedDefinitions`, `WidgetTypes` and `UnlistedCalls`.

## Sources

Every fact the schema learns has a `game.Source`, and a lower source outranks a higher one: the rows, the dumps, the
executable, the logs, the docs, the wiki, the game's own files, and inferred last. Only the dumps, the docs, the wiki
and a definition's own word on its name (`game.SourceDefinitions`, as a modifier type's) can accuse a mod
(`Source.CanAccuse`). A fact learned from the game's own files completes a hover card, marked as inferred, and never
fails a verdict. A reference that only such a fact makes is `Reference.Inferred`.

`Build` fills the schema in that order of trust. It builds the databases from the folders and the rows, reads the dumps
and then the docs, counts the game's own files, and last gives a field with no blurb the one the docs write for the
same key elsewhere in its database.

## Claims

A field's `Claims` are its facts: its value, shape, requirement, default, blurb and keys. Each is a `Claim`,
which holds one value and the `game.Source` that states it. A higher-ranked source replaces a lower one, and a claim
that states nothing takes any source (`Claim.resolve`). `Field.take` gives a field another field's claims where they
outrank its own, all but `Keys`. The docs reader gives a database's fields what a doc says through it, and the dumps
reader unites the arguments of one list family (any_, every_, random_ and ordered_) through it (`unite`): the members
share one tree of argument keys, and each keeps its own arguments field and shape. A command declared as both an
effect and a trigger gets the two declarations' arguments united, claims, keys and counts alike, as `Command.Union`
(`buildUnions`). The unions are built before the count, so it reads the blocks that take one as every later walk
does, and again after it, with what it counted. The union keeps a shape only where each declaration that writes the key states
that shape, or where the shape allows effects and triggers alike, as value does, so a shape that holds one kind never
reaches a block of the other kind (`keepSharedShapes`).

`CountVanilla` states what no source does, field by field, in `resolveClaims`. First comes the shape its blocks tell
(`resolveShapeClaim`). Where its blocks call scripted definitions, it is the one shape every call allows. Otherwise it
is the one shape the keys written in its blocks tell, and a value key wins over triggers. Shapes that disagree, or a
key that fits more than one kind of block, give unknown. Then come its keys (`resolveKeys`), then its value.

A stated value is kept, except a reference with no database, a block or a text, which take the reference vanilla shows.
A block or a text never takes a reference to the game concepts, whose names are common words. A stated target that
names a database stays firm only where that database defines every name the game's own files write there; otherwise
it is inferred. A stated number takes a script value too where the game's files write one there, as EU5's
`add_cooldown` does in `years`. Vanilla shows a reference when one database defines every value written at the field,
as `resolveReference` finds it, and none of the values is a number, a date or a yes-or-no word. Where vanilla shows no
reference, a field never written as a block, or written as a value block, takes a number a script value may give
(`takesValues`) when vanilla names a script value there, as `add_stability = stability_mild_bonus` does, and writes
nothing else but numbers, parameters, chains of links and commands the dumps declare; a field with any other name,
such as a flag or a relation level, does not. A script value named there is an inferred reference, so a name nothing
defines is no problem. Where no source states a value, a field always written as a block is a color if every block was
one, and else a block. A field written only as scalars gets the first type in `valueTests` that all its values fit.

`resolveReference` gives a reference when one database defines every value. A quoted value names what it names
unquoted. A value that holds a parameter is left out, and so is a link (`IsLink`), which leads to a scope rather than
naming a definition. A database whose row names another as its target counts as that one, and so does a mirror, such
as the coats of arms, wherever another database defines the values too. Where no one database defines them all, a
database with nested ones may, counted together with them, as a language and its dialects are (`Expected.Nested`). The
game concepts, then the localization, count only when nothing else does.

`Claims.Keys` is the database a block's keys name, as a culture's opinions name cultures. It is set when every key that
only vanilla writes in the block is a definition of one database. Those keys' fields are then folded into the field of
`<any>` (`game.AnyKey`), and the walk reads a field key there as a reference to that database (`findMatch`), inferred
where only the game's files say so. A key a source declares stays as it is, and a key the dumps declare in the block is
an argument and names nothing. Left out are a block whose shape holds commands (`game.Shapes[…].Allows`), and keys
that are game concepts or messages, definitions of a GUI database (whose names are common words, such as value), or
the definitions of a database with its nested ones. A field a command holds names nothing by its keys: what a
command's block takes is its arguments, which the dumps declare and the game's files add to. The commands whose keys
do name a database are `game.CommandKeys` rows, as `stress_impact`'s keys are traits (`buildCommandKeys`). A row may
name a key of a command's block, as CK3's `create_character/random_traits_list` makes each key but `count` a trait.
Where a source states the shape of such a block, the count folds the keys only vanilla writes there into `<any>`, as
it does for a definition's, so every trait's block takes the `trigger` and `weight` that vanilla writes in them. A
command's own block, whose shape no source states, keeps its keys apart, as `stress_impact`'s does.

## The dumps

`ReadDumps` reads the script_docs and data types dumps under the given folders: the effects, triggers and event
targets logs, the modifiers, the on_actions, the event scopes, the customizable localizations' names (`Customs`, which
lsp counts as known though no file defines some), and the data types files. `FindDumpsAt` gives when they were
written, the time of the newest dump file, without reading one. A section the install lacks is taken from the shipped
backup, one section at a time; a dump file that cannot be read as text, such as one over 32 MiB or in UTF-16, counts
as lacking. The schema's `Dumps` is a `SourceState`: whether the dumps came from the install, the shipped backup or
neither, when they were written, and whether they are older than the install.

A command that a `game.DumpCorrections` row names, for a dump row that contradicts its own text, takes the scopes and
the target of the row in place of the dump's (`applyCorrections`), whether the dumps came from the install or the
backup.

A command named by several dumps is one `Command`, which keeps each dump's arguments under the word that declares it
there (`ArgumentsOf`), so an effect and a trigger of one name take their own. Its arguments are the keys of every usage
block written under its name, on a line of its own or inside a sentence, and the key each line of its prose opens with,
as in `key = value`. The usage starts at the first line that opens a block (`splitUsage`), but a line that closes its
block and then describes it after a dash is prose, as create_character's `random_traits_list = { count = … } - A
number of traits …` is (`describedLine`). A usage's comments are dropped. A key written with nothing after it is declared all the same, unless the block it
opens starts on the next line, where it is the scope the usage runs in. A statement written in `[ ]` or `< >` is
optional, and a value written as `[a|b]` or `<A|B>` gives the choices. A placeholder such as `<triggers>` gives the
block the shape it names, and a docs' type word gives the type. Any other placeholder value names what its words name
exactly (`resolvePlaceholder`): words that end in key or name, a name of the database the others name, as
`<effect_localization_key>` does; any other words, a target of the scope type they name, with or without a closing
scope or target, or a name of that scope's database, as `<country>` does. `ArgumentKey` is the first key of the first
block form, which an argument in parentheses after the command's name stands for. A link that the dump says requires
data is a prefix, written with a name after a colon. A line that writes a value after the command's own name, as
`add_trait = <trait_key>` does, gives the command's value as an argument's value is read (`resolveTitleValue`), unless
its Traits line says otherwise. A value that gives a block's shape, as `<dynamic_description>` does, gives no shape
where the dump also writes keys of the command's block, as `change_first_name = { template_character = … }` does: that
block holds the command's arguments. A value written alone there names what the shape's values name
(`game.Shapes[…].Values`), as a description's localization key does. A trigger's Traits line says what its value takes
(`resolveTraits`): a comparison takes a number or a script value, or a date where the line says so; yes/no takes yes
or no; a class key takes a name of the database that holds its keys; a scope or a target takes a link to that scope (`ValueTarget`,
`Expected.Scope`); a tag or an id takes such a link or a name of the database of the scope's names; a list takes one of
its words. A keyword row written under a command, such as random_list's numbered branches, gives the command an
argument of that shape.

A prefix's database is resolved from the names the game's own files write after it (`resolvePrefix`). A prefix whose
link leads to a scope from any scope names the database of that scope's type, by the getters of `game.AnyScopeType`,
where every name written after it is defined there. Otherwise it is the one database that defines every name written
after it, and none where no database or several do. A link whose name is a literal, as `flag:the_boss` is
(`game.DumpPhrases.Literal`), takes no part in that vote.

## The docs

A doc is an `.info`, `.md` or readme file, or a `.txt` file whose name marks it as one, in a database's folder or below
one, or named for one (`findDocDatabase`). A doc tied by a folder above its own describes one part of its database and
gives it no blurb. A file with no prose and no comment, or a `.txt` file with more script lines than prose, is data and
is skipped. The `docReader` reads a doc line by line, by pattern: prose is no valid script, so nothing is parsed. Its
example's statements are fields, except below a block that holds script (`holdsScript`), and the comments beside and
above them are their text. A key that stands for names (`<key>`, `scope:actor`, `trigger/effect`, `$target$`) makes no
field, but a block it opens is still a level of the example; written with prose instead of a block, as `scope:actor =
the actor` is, it is a note. That text states a blurb, a type by the words of `game.DocTypes`, a requirement, a
default and choices. It states no scope: a list of scopes under a label is read as no field's text. A sample value such
as `innovation_key` names the database of its words, by `ResolveTypeDatabase`, and the key `date` stands for a date
key, as a dated history block's. The example's first block is the definition, but a definition named by its file is the
whole file, so such a doc is read as already inside it. A definition whose block holds script, as a scripted effect's
does, takes no field. A later doc only fills what an earlier one left out. A database that gets a blurb from a doc keeps
the doc's path from the install (`Database.Readme`). A database of records, such as `history/titles`, gives no data type its words: "title" names a
landed title, not its history.

## The vanilla count

`CountVanilla` walks every script, GUI, asset, font and GFX file of the install, the DLC folder left out, and reads
every localization file for its message keys. It runs in two passes. The `declarer` pass reads the files that declare
what script and GUI files call by name, or whose definitions say what their names take, so that a later key under a
call counts as a call: the scripted definitions with the parameters their bodies write and the names they save, the
widget types, and the modifier types. The `counter` pass then counts, on `files.Workers` workers each with fields of
its own, what the files write: the keys under each database's definitions, each value with its count, the shapes and
proofs a block's keys tell, the names after each prefix, and where each command and data function is written. The
workers' counts are added together, and `resolveCounts` gives the schema what they support: the claims above, the
command patterns, the prefixes' databases and each command's examples.

**Command patterns** (`resolvePatterns`). A `CommandPattern` is a family of commands that the dumps declare once for
each definition of a database, as `has_relation_*` is for the scripted relations. A database makes one where the names
of all its definitions, two or more, make commands the dumps declare, each with one name in the place of the star;
each such command is `Patterned`. The walk reads a key a pattern makes as the command of one of the game's own
definitions, and for a list the plain list of its scope, whose arguments it takes, and the text in the star's place as
a reference to the pattern's database (`findPatterns`); a text that holds a dot, as a chain of links does, fits none.

## Scripted definitions and modifier types

The `declarer` of a file records each definition that script or GUI files call by name: a `ScriptedDefinition` with the
parameters its body writes in its keys, tags and values (`ListParameters`, by `parameterRe`), and the parameters it
saves a name under, directly or by passing them on to a call of another one that saves them (`resolveSaves`). A widget
that calls a type records the widget type. A modifier type whose database row has a `game.Database.BoolKey` records what
its name takes in a modifiers block, as `ModifierFields` holds it: yes or no where its definition writes yes at that
key, else a number or a script value, claimed by `game.SourceDefinitions`. `SetScripted` reads the same from the
workspace's mods' files, in place of what those files declared before, so a call to a mod's scripted definition is a
known call and a mod's modifier type takes what its definition says; a call finds the game's definition first, and a
mod's never takes the place of a command the dumps declare by that name.

## The walk

`Walk` goes through a file's tree once and calls a `Visitor` at every statement it enters and leaves. At each one it
decides what the schema and the game's rules say: the field, the scope, the shape, the definition and the references.
It fills one `Context` per depth and reuses it, so a visitor copies what it keeps. `WalkTo` is `Walk` restricted to
one offset: it offers the visitor only the statements that cover it, and still resolves the statements before it
silently, to learn what they saved. Nothing else in the app walks a file.

The `walker` keeps a `frame` per depth: its context, the label last read there, and the key path from the definition
the statement is in. The context carries the statement's label (`Context.Label`) and the key path to the block that
holds it (`Context.Path`), so a visitor can match the statement against a key path as `jomini.MatchKeyPath` reads one.
Each statement is resolved in one of two ways.

- **It defines a name** (`definitions.go`). A constant, a nested definition, a GUI declaration, a top block of an asset
  file, a name an events file declares inline, or a script definition named as its database's row says (`NamedBy`). A
  window at the top of a GUI file that writes no name takes its file's name where it is the file's only keyed block
  (`findWindowLabel`). An event must follow a namespace its file declares (`listNamespaces`). The command that saves a
  scope or a variable makes a definition of the name it saves, and the value at an alias key is a second name of its
  definition.
- **Its key fits a rule.** The block's shape lists its key rules in order (`game.Shapes[…].Rules`), and the first one
  the key fits wins (`findMatch`, `matchRule`). The rule says where the block's fields come from, what decides its
  shape, and how its scope follows. A keyword of the language (`game.Keywords`) adjusts what the rule gave.

A command fits only where the block's shape allows the kind it is declared as, and an effect never fits inside a
trigger block (`Context.FindDeclaration`). In a block that allows both, a command declared as both takes its
`Union`, else it takes the arguments of the word it fits as (`Context.FindArguments`). A command's block holds its
arguments and the commands of its own kind: the shape its arguments state, else unknown for a union, which takes
effects and triggers alike, else the effect arguments or the trigger arguments. A statement below a block of triggers,
a trigger's arguments included, is `Context.Conditional` (`holdsConditions`), and one below a keyword whose block is
shown in the tooltip and never run (`game.Keyword.Preview`, as `show_as_tooltip`'s is) is `Context.Preview`. A key the
links test takes (`isKeyLink`) is a link: a link that requires data, as EU5's `modifier:<key>` does, is none where a key
writes it alone (`Command.Bare`), and in a block of effects or of a command's arguments a plain name with a plain value
is none either (`scope-link`). A link or a command that leads to a scope moves the scope there and pushes the one it
leaves onto `Previous`, and a chain of links is followed link by link. A definition's root scope is the one its row's
root key names (`game.Database.RootKey`, as CK3's events write `scope`), else its event type's, or the dumps' for an
on_action; it is unknown for the rest. Where a database gives its blocks a run order, the walk first reads the blocks
silently in that order, so that a block starts with the scopes saved by the blocks that run before it.

**A command's name with an argument in parentheses** (`findKeyCall`). A key written so, as
`aptitude(court_jester_court_position)` is, is read as the command, and its argument fills the command's
`ArgumentKey`: a name of the database that key takes, unless it is a link, else a chain of links
(`listArgumentReferences`). A link that requires data reads its argument in parentheses too, as
`relations(scope:overlord)` does. A key compared with a block, as `list_size(x) < { … }` is, writes no call
(`findStatementCall`): the game reads that block as a value it works out. The value of a math keyword written so, as
`opinion(scope:ruler)` is, stands for the command's number, and its argument makes the references.

The references a statement makes are found here and nowhere else: the names its key calls, the names after a link's
prefixes, the database its field claims, the modifier type a modifier the dumps declare names, which never accuses
since nothing says every modifier has a definition, the database a key path row gives (`game.Game.ListReferences`), a
step of the game's event pictures (`findPictureDatabase`), a coat's template list, named color and images, as
`game.Coat` places them (`findCoatDatabase`), an event id, a script value, a quoted data-call argument, a macro's
expansion and a `list "x"` value. The text a keyword shows names the database of its row's `Values`
(`findTextDatabase`). An argument `game.LocStems` names gives the localization key built on its value, as an activity
log entry's `key` names `<key>_title`. A name after a prefix
that is itself a chain of links, as `scope:overlord` is in `relations:scope:overlord`, names no definition: it makes the
references of its own prefixes (`listPrefixed`). A value names a script value where it is a chain of links ending in a
name, a math keyword's value, or a value whose field takes a script value; a link key's field is its holder's, so it
gives the link's value none (`findScriptValue`).

## Data calls

A data call's steps are read by the dumps' data types. `ResolveMacro` expands a call that opens with one of the game's
data binding macros. `ResolveCallTypes` gives the type each step leads to from a GUI block's data context, and
`FindFunction` the function one step calls. `ResolveDatacontext` gives the data context type a block's datacontext key
sets. `ListCallReferences` gives the names a call's quoted arguments hold, each in the database of the type its step
returns, or as `game.GUI.CallTargets` says; a promote of `game.TopScopeType` that leads to a scope (`isScopeType`) reads
a saved scope, as `SCOPE.sC('heir')` does, so its name never accuses. `ResolveTypeDatabase` finds the database that
holds a data type's names by the type's words, as `ToPhrase` writes them. Where two databases have the same words, the one whose own name is those
words takes them (`isNamedExactly`: EU5's `institution` over `setup/1337.institution`), and else they name neither.

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
- **CommandPattern**: a family of commands the dumps declare once for each definition of a database.
- **ScopeType**: one kind of scope, with whether triggers and effects run in it.
- **DataType**: one type of the data binding language, with its functions.
- **DataFunction**: one function of a data type.
- **Modifier**: one modifier the dumps declare.
- **OnAction**: one on_action the dumps declare, with its scope and whether the game calls it from its own code.
- **ScriptedDefinition**: a scripted effect, trigger, value or list the game or a mod defines, with the parameters it
  takes.
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
