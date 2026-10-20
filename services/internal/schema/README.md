# schema

What the sources state about one game version's script: each database's positions with their claims and
vanilla's evidence, each command the dumps declare, and the one walker every reader of script uses. It decides
whether a claim may accuse a user's file (`MayAccuse`) and never looks at a mod.

Read `build.go` first. `Build` makes one game version's schema in seven steps, each a function named for it, in
the file named for the step: `declareDatabases` in `build.go`, `dumpsOrShipped` in `dumps.go`, `stampConstructs` in
`build.go`, `parseDocs` in `docs.go`, `countVanilla` (after `registerDefinitions`) and `inferTypesAndPrefixes` in `vanilla.go`, `mergeDocs` in `docs.go`.

| file | holds |
|---|---|
| `schema.go` | the types (`Schema`, `Database`, `Position` with its `Claims`, `Claim`, `Command`, `Vanilla`, `Writes`, `ValueType`), `Link`, `CallLeadsTo` and `CallStep` (where a data call's steps lead), `ResolveNamedDatabase`, `ResolveTypeDatabase`, `DatabasesWhere`, `MayAccuse` |
| `build.go` | the pipeline: `Input`, `Build`; declaring the databases (`declareDatabase`, `declareDatabases`) and `FindHousingDatabase`, the one search for the database that houses a thing (`Schema.Lists`, `Functions`, `Events`, `Concepts`) |
| `dumps.go` | `dumpsOrShipped`, then the parsers of the console dumps: the label table, one `parse…` per record; the cut of a usage from a description (`cutScript`); a Traits class read as a database (`resolveClassDatabase`); the usages merged into each command's arguments once every command is declared (`mergeUsages`, `usageRoots`, `mergeUsage`, `argument`) |
| `docs.go` | the documentation dialect: `parseDocScript`, the one reader of docscript that the install's docs, the wiki's examples and the dumps' usages share (`DocNode`: a statement's key, value slot, prose and block); `parseDocs`, the docs and examples parsed to trees (`resolveDocDatabase`: which database a prose file documents) with every key they write noted for the entry detector; `mergeDocs`, their claims merged into the entry trees after vanilla (`mergeNode`, `mergeTemplate`, `docClaims`); the phrase table `docPhrases` and `claimsOf`, which both merges read claims with |
| `vanilla.go` | steps 5 and 6: `registerDefinitions`, the pass before the count (the scripted entries and the built-in widget types the GUI files imply: `Schema.WidgetTypes`, by `typeCollector`, and `game.UndeclaredTypes`), `countVanilla` and `countFile`, what vanilla writes at each position, and `inferTypesAndPrefixes`, the kind its scalars share where nothing declares one |
| `fields.go` | `Fields`, the keys an entry of a database writes, each with its value's type, word list, database, requirement, default and blurb |
| `entries.go` | the entry detector, `entryAt`: whether a statement defines an entry and in which database, by the game's `Rules` (`game.DatabaseRule`: identity, wrapper keys, keywords, nested key paths, the namespace key: an event is a top key `x.N` whose namespace `x` a statement before it declares, `declaresNamespace`), the file type and the file's own database (`folder`, `DatabaseAt`); a table's or list's entries (`tableEntries`), a whole-file entry (`wholeFileEntry`) |
| `walk.go` | the walker: `Context` (where statements read), `ValuePosition` (the position that types a statement's value: the key's own, or the block's for a word listed in it), `Visit`, `WalkFile` (the entries decided as it meets them, `entryOpens` the context an entry opens), `EntryContext`, `ResolveEntryScope`; `Enter`, the one step from the context a statement is written in to the one its block reads in, by the first key rule that fits (`fits`), its row (`game.KeyRules`) and the first construct (`game.Keywords`); `holdsOf`, `commandShape`, `scopeOf` (the scope record `Scope`: this scope, the entry's root, the ones before, the saved scopes, the reason), `Follow` (a link chain, link by link), `commandScope` |
| `occurrences.go` | `Occurrence`, one occurrence of a name with its `Role` (`Defines`, `Refers`, `Localizes`), and the occurrence rules: which entry each scalar of a placed statement stands for (`keyOccurrences`, `valueOccurrence`, `prefixReferences`, `parameterDefinitions`); the `Localization`, `File` and `Parameter` databases |
| `shipped.go` | the backup schemas embedded from `shipped/`, `LoadShipped`; `gen/` writes them |

## Sources, claims and accusing

The sources are ranked: the registry (the game's rows), the dumps, vanilla, the docs, the wiki. A claim keeps
the highest-ranked source that states it (`Claim.Set`), whatever order the sources are applied in, so a
lower-ranked source fills only what a higher one leaves empty; the steps of `Build` follow their data, not the
ranks. The dumps, docs and wiki declare; vanilla gives evidence (`Position.Vanilla`: how often and in
what shapes it writes a position, and how many of the values written there are no color (neither a color's numbers
nor a word, which may name one) and whether one is a tagged color, which `Vanilla.WritesColors` reads for the
editor's swatches) and a value type only where nothing declares one. `MayAccuse` is the one
rule for when a claim may accuse a user's code: a declared source states it, that source is not stale, and
vanilla's evidence at the position does not contradict it; a docs or wiki claim, and any declared word list,
also needs vanilla to write the position. The registry and vanilla never accuse.

## The dumps' cut and classes

The dumps write a command's description and its usage in one text, often on one line (`Add building to the
province<province> = { add_building = x }`), and `parseEntryBody` cuts the usage from the description at the first place
a description line writes script (`cutScript`: a usage label such as `Usage:` or `(example:`, a statement's key before
an operator or a brace, a `<placeholder>` in a statement's place, a brace; the doc comment names each condition), keeping
what is before it on the line as description and everything after it as usage, so a usage never reaches a blurb.

A command whose Traits line says `class CTenetTypeDatabase key` claims a reference to a database.
`resolveClassDatabase` reads the class: the row of `game.ClassDatabases` where it has one, else the database
`ResolveNamedDatabase` gives the class's snake_case stem (`DataBase` is read as `Database`), else the stem without a
trailing `type` or `types` word.

## What a database houses, data types and fields

A database rule says what its database houses beyond plain entries (`Houses`: events, on_actions, lists, functions,
concepts, history). Four of those have one database each, found once by `FindHousingDatabase` when the databases are
declared: the first by name, a second one logged. `draw`, `index` and the
schema's own readers ask the field (`Schema.Lists`, `Functions`, `Events`, `Concepts`); the index asks the function
over the game's rows for the localization it reads before a schema exists. On_actions have no field, and the docs'
`event id` and `on action` phrases ask the field and `FindHousingDatabase`.
`ResolveTypeDatabase` is the one answer to which database a data type's entries are in: the concepts for the global type,
else the database of the first link prefix, in name order, whose link leads to the type's scope. `ResolveNamedDatabase` is
the one rule from a written name to a database: the name or its plural exactly, else the one database whose last path
word is either.

`Fields` reads a database's entry position: its keys, each with the type and word list its value claim holds, the
database its values name (the claim's, else the one every value vanilla writes there names by a link prefix), whether
the position is always required, its default and its blurb. It is nil where the database is unknown or an entry holds
script or widgets rather than fields, or has no key.

## Learning from vanilla

Before the count, `registerDefinitions` walks the files of the scripted folders and the GUI files, so the count reads a key
under a call as the call (the scripted entries and widget types vanilla defines). A scripted folder's entries are its
files' top statements and its wrappers' children, so that walk reads on only there (`topOnly`); a GUI file declares
at any depth (a `block` sits inside a template's widgets), so it is read whole. The count is one
`WalkFile` over every script, table and list file, the parses of the first pass reused.

## Positions and the walker

A position is a place in a database's entry tree or a command's arguments, keyed by a key's written form: a
number is `<number>`, a date `<date>`, a `prefix:x` word `<prefix:>`, any other key itself, and every key where
the position has `<any>` (switch's cases). A `Context` is where statements read: the position, the shape of the
block, where its script runs (`Scope`: this scope, the scope the entry runs in, the ones entered on the way, the
scopes the entry has saved by name so far, and why, in plain words for the hover card and `wrong-scope`), the word
its calls resolve by, the entry and database it is in, and for the block a key opened, how that key was read
(`KeyRule`, `Call`, `Command`, `List`, `Leaf`, `LeafPosition`). `Enter` is the one step: a statement
with no key, outside an entry, a date key or a key written in place reopens its holder; any other key is read by
the first rule the holder's shape tries whose test holds (`fits`), and the rule's row says where its block reads
(`BlockAt`), what decides what it holds (`ShapeFrom`) and how its scope follows (`ScopeFrom`); the first construct that fits
the key refines the shape (a construct that names no shape leaves it to the key's rule), a block written as a dynamic description is one whatever the rows say, and a scope
claim on the key's position beats the verb. A link chain leads link by link (`Follow`): a declared link to its
target, `root`, `prev`, `this` and `scope:x` by the record (`game.ScopeLinks`); `save_scope_as` records the scope
under its name for the entry where its block runs effects, and inside a command's arguments (`create_character = {
save_scope_as = x }`) a scope the command makes, unknown. Two readers use the walker: `countFile` and the index's
`Collect`;
the doc and usage merges call `Enter` on a synthetic statement.

**What a word names.** Beside the rules of `occurrences.go`: the tag of a block an entry is written with (`type x = button { }`) is a call by the file type's `TagCalls` word; a key a file type's `Reference` names refers to an entry, a file or a localization key, or calls by its own word (`using = x`); and each step of a data call in a value is a reference to the function it names (`Type.Function` in the `datafunction` database), read from the data type the block's `datacontext` leads to. A first step no type declares, written with arguments, refers to the functions database's entry of its name (a `data_binding` macro).

`WalkFile` is the one walk of a whole file: it reads every statement in source order, outer before inner, decides
at each whether it defines an entry (`entryAt`: by the game's `Rules`, the file type and the file's
database), and hands each to the `Visit` with the context it is written in (its `Entry` is the entry it is in, -1
outside one), the one its block reads in, and what its key and value name (the occurrences, by the rules of
`occurrences.go`); it returns the file's entries, each a `Defines` occurrence, in source order. `StoredIn` is the
database a name command's value names there.

## Docscript

The docs, the wiki's examples and the dumps' usages write script with prose in it, in three conventions, and
`parseDocScript` reads all three into one tree. A line is a statement, a commented-out statement, a comment, a
brace, prose or a heading. A statement's value slot is one token: a `<placeholder>`, a word, alternatives
(`yes/no`, `a | b`), a "quoted text", or several words read as one text (`ai_value = script value`); the words
after a `<placeholder>` or after a block closing on the line are its prose. Braces build the tree, and a text
whose braces do not balance closes a block when a code line comes back to its indentation. A comment trailing a
line belongs to the first statement on it; a run of whole-line comments leads the statement under it, and an
empty comment or a heading (a line in capitals or between `==` marks) keeps the run together and says nothing.
`claimsOf` reads a node's claims from its value slot and each line of its prose through `docPhrases`; what no
phrase reads is the blurb, cut from a sentence that opens with `E.G.`, `e.g.` or `Example` to the end of the line
(`cutExample`: it shows how to write a key, it does not say what the key is). The tokenizer, `tokenizeDocLine`, reads
one line that may be prose, where jomini's lexer reads a whole file of script; its `keyWord` is its own key rule, not
`jomini.IsIdentifier`.

The two merges differ only in where a key is placed. `mergeDocs` places each key where the walker reads it under
the entry (`Enter`), writes the node's claims where vanilla writes that position, and reads a key vanilla
writes nowhere below as a template (an example entry's name, a `<key>`) whose block merges in the holder's
place. `mergeUsages` places a usage's arguments under the command's `Args`, made on write; the nodes that stand
for the command are `usageRoots`: those named as it, else the one top statement (an alias family's sibling
name), else the tree itself when the usage lists the arguments.

### The prose files

Every file the inventory classes as prose (`.info`, `.md`, `readme.txt`) is parsed into the database it documents:
the one its folder is, when the file sits directly in that folder, or the one its stem names beside it
(`history/_characters.info` documents `history/characters`); `resolveDocDatabase` is that rule. A file that names no
database is not parsed. A database's blurb is the prose of the first top statement of its doc that is an example
entry (`mergeTemplate`) and has prose of its own: a heading or an example alone gives none, and the next top
statement with prose then gives its.

The prose files no database names, by install, as the inventory names their paths (measured on the three installs).
`gui/scripted_widgets/_scripted_widgets.info` (CK3, EU5), `gui/scripted_widgets/scripted_widgets.md` (Vic3) and
`reader_export/_reader_export.info` (CK3) are reached once the registry has a rule for their folder:

- CK3, 10 of 207: `_commandline_options.info`, `common/culture/_cultural_traits.info`,
  `common/menu_scenes/_default_menu_scenes.info`, `common/religion/great_holy_wars.info`,
  `gfx/models/units/_unit_states.info`, `gui/scripted_widgets/_scripted_widgets.info`, `gui/shared/animation.info`,
  `history/_history.info`, `music/_music.info`, `reader_export/_reader_export.info`.
- Vic3, 9 of 105: `common/audio_persistent_objects.md`,
  `common/history/military_formations/99_military_formations_example.txt`,
  `common/history/military_formations/military_formations.md`, `common/history/pops/100_pops_example.txt`,
  `common/scripted_modifiers/scripted_modifiers.md`, `gfx/models/utility/README.txt`, `gfx/models/zzz_clones/info.txt`,
  `gui/scripted_widgets/scripted_widgets.md`, `input_profile/input_profile.md`.
- EU5, 13 of 115: `common/mission_task_defs/missin_task_defs.info`, `common/policies/policies.info`,
  `common/scripted_modifiers/scripted_modifiers.info`, `common/scripted_rules/scripted_rules.info`,
  `gfx/interface/illustrations/units/00_naming_convention.info`, `gfx/terrain2/terrain_cache/heightmap.info`,
  `gfx/terrain2/terrain_cache/index_map.info`, `gfx/terrain2/terrain_cache/intensity_map.info`,
  `gfx/terrain2/terrain_cache/materials.info`, `gui/panels/disaster/readme.txt`, `gui/panels/situation/readme.txt`,
  `gui/scripted_widgets/_scripted_widgets.info`, `input_profile/_input_profile.info`.

## Names

A key is named by every key rule that fits (`keyOccurrences`), a value by the first value rule that fits (`valueOccurrence`):

| Rule | Where | Occurrence |
|---|---|---|
| history | a history entry's top key, and its value where it is written key = value | a reference to the object it sets up (`Target`) |
| file ref | a value at a key its file type lists (`DatabaseRule.References` of its extension: a GUI `using`) | a reference to that entry |
| list | a call starting with `every_`, `any_`, `random_`, `ordered_` in a triggers, effects or value block | a reference to that scripted list |
| call | a key read as a call | a call reference (`Caller`: the word the block resolves calls by, one of the database's `CallerWords`) |
| parameter | a key in a call's block | a reference to the called entry's parameter, `<entry>:<NAME>` |
| made | the value of a name command that makes a name (`save_scope_as`, `set_variable`) | a definition in the name's database |
| group | a value at a folder's `AliasKeys` key | a definition the entry's database names (`IsName`) |
| read | the value of a name command that reads one (`has_variable`) | a reference into the name's database |
| claim | a value at a position typed as a database, a loc key or a file | a reference to it; a number at a position vanilla typed is none |
| value | a scalar at a position declared to take a number | a call to a script value |
| event | a value written as an event id (a number is none) | a reference to the events database |

Always, on any scalar: each `prefix:name` part a link reads is a reference into the prefix's database
(`prefixReferences`, by `Schema.Prefixes`), and a scalar with such a part takes no value rule; each `$NAME$` in an
entry of a folder with `TakesParameters` defines a parameter at its first use in the file (`parameterDefinitions`). A
constant (`@x`) and a scalar holding a parameter (`$X$`) name nothing.

## The shipped schemas

A user missing a dump file gets a backup: a schema, never the dumps, that the maintainer builds on their own
install and ships inside the app. It holds only what the dumps declare (`Commands` with their argument
claims, `Scopes`, `Saved`, `Modifiers`, `OnActions`, `DataTypes`, `CustomLoc`, the dumps' `Sources` entry, the
game and version); no database, docs claim, evidence or vanilla word is shipped. The scan runs the schema
stage on the user's own install and takes only the dump part from the file (`Input.Shipped`).

One `<game>-<version>.gob.gz` per game version, gob inside gzip, named by `ShippedName`, in the `shipped/`
folder beside `shipped.go` and embedded by it. `LoadShipped(game, version)` returns the exact version's file with `exact` true, else the
newest by the digit runs of the names (`1.20.0.3` over `1.9.2`) with `exact` false, which the scan marks
stale; a stale source feeds hover and completion but never accuses. A game with no file gives nil.

The generator, from the repo root with the game's dumps in place:

    go run ./services/internal/schema/gen -game ck3
    go run ./services/internal/schema/gen -game eu5 -dir "D:/Steam/steamapps/common/Europa Universalis V"

It runs the schema stage stopped after the dumps (`index.BuildSchema`, `Input.DumpsOnly`) and refuses when a
dump file is missing, so a partial backup never ships. A file of the same version is replaced only when it
decodes to a different schema, since gob writes maps in random order. Run it after a game update, after fresh
dumps, and before a release; old versions' files stay until deleted, as the stale fallback for a user on a
version with no file. The struct fields' names are the gob field names, so a renamed field needs the files
written again.
