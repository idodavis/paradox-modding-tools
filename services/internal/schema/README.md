# schema

What the sources state about one game version's script: each database's positions with their claims and
vanilla's evidence, each command the dumps declare, and the one walker every reader of script uses. It decides
whether a claim may accuse a user's file (`MayAccuse`) and never looks at a mod.

Read `build.go` first. `Build` makes one game version's schema in six steps, each a function named for it, in
the file named for the step: `declareDatabases` in `build.go`, `dumpsOrShipped` in `dumps.go`, `parseDocs` in
`docs.go`, `countVanilla` and `inferTypesAndPrefixes` in `vanilla.go`, `mergeDocs` in `docs.go`.

| file | holds |
|---|---|
| `schema.go` | the types (`Schema`, `Database`, `Position` with its `Claims`, `Claim`, `Command`, `Vanilla`, `Writes`, `ValueType`), `Link`, `DatabaseNamed`, `DatabasesWhere`, `MayAccuse` |
| `build.go` | the pipeline: `Input`, `Build`; step 1, declaring the databases (`declare`, `declareDatabases`) |
| `dumps.go` | step 2: `dumpsOrShipped`, then the parsers of the console dumps: the label table, one `parse…` per record kind; the usages merged into each command's arguments once every command is declared (`mergeUsages`, `usageRoots`, `mergeUsage`, `argument`) |
| `docs.go` | the documentation dialect: `parseDocScript`, the one reader of docscript that the install's docs, the wiki's examples and the dumps' usages share (`DocNode`: a statement's key, value slot, prose and block); step 3, `parseDocs`, the docs and examples parsed to trees with every key they write noted for the entry detector; step 6, `mergeDocs`, their claims merged into the entry trees after vanilla (`mergeNode`, `mergeTemplate`, `docClaims`); the phrase table `docPhrases` and `claimsOf`, which both merges read claims with |
| `vanilla.go` | steps 5 and 6: `countVanilla` and `countFile`, what vanilla writes at each position, and `inferTypesAndPrefixes`, the kind its scalars share where nothing declares one |
| `entries.go` | the entry detector, `entryAt`: whether a statement defines an entry and in which database, by the game's `Layout` rows (`game.Descriptor`: identity, wrappers, keywords, nested key paths), the file type and the file's own database (`folder`, `DatabaseAt`); a table's or list's entries (`tableEntries`), a whole-file entry (`wholeFileEntry`) |
| `walk.go` | the walker: `Context` (where statements read), `Visit`, `WalkFile` (the entries decided as it meets them, `entryOpens` the context an entry opens), `WalkBlock`, `EntryContext`, `EntryScope`; `Enter`, the one step from the context a statement is written in to the one its block reads in, by the first key rule that fits (`fits`), its row (`game.KeyRules`) and the first construct (`game.Constructs`); `holdsOf`, `commandShape`, `scopeOf` (the scope record `Scope`: this scope, the entry's root, the ones before, the saved scopes, the reason), `Follow` (a link chain, link by link), `commandScope` |
| `occurrences.go` | `Occurrence`, one occurrence of a name with its `Role` (`Defines`, `Refers`, `Localizes`), and the occurrence rules: which entry each scalar of a placed statement stands for (`keyOccurrences`, `valueOccurrence`, `prefixReferences`, `parameterDefinitions`); the `Localization`, `File` and `Parameter` databases |
| `shipped.go` | the backup schemas embedded from `shipped/`, `LoadShipped`; `gen/` writes them |

## Sources, claims and accusing

The sources are ranked: the registry (the game's rows), the dumps, vanilla, the docs, the wiki. A claim keeps
the highest-ranked source that states it (`Claim.Set`), whatever order the sources are applied in, so a
lower-ranked source fills only what a higher one leaves empty; the steps of `Build` follow their data, not the
ranks. The dumps, docs and wiki declare; vanilla gives evidence (`Position.Vanilla`: how often and in
what shapes it writes a position) and a value type only where nothing declares one. `MayAccuse` is the one
rule for when a claim may accuse a user's code: a declared source states it, that source is not stale, and
vanilla's evidence at the position does not contradict it; a docs or wiki claim, and any declared word list,
also needs vanilla to write the position. The registry and vanilla never accuse.

## Positions and the walker

A position is a place in a database's entry tree or a command's arguments, keyed by a key's written form: a
number is `<number>`, a date `<date>`, a `prefix:x` word `<prefix:>`, any other key itself, and every key where
the position has `<any>` (switch's cases). A `Context` is where statements read: the position, the shape of the
block, where its script runs (`Scope`: this scope, the scope the entry runs in, the ones entered on the way, the
scopes the entry has saved by name so far, and why, in plain words for the hover card and `wrong-scope`), the word
its calls resolve by, the entry and database it is in, and for the block a key opened, how that key was read
(`Rule`, `Call`, `Command`, `List`, `Leaf`, `Bound`). `Enter` is the one step: a statement
with no key, outside an entry, a date key or a key written in place reopens its holder; any other key is read by
the first rule the holder's shape tries whose test holds (`fits`), and the rule's row says where its block reads
(`At`), what decides what it holds (`Holds`) and how its scope follows (`Scope`); the first construct that fits
the key refines the shape, a block written as a dynamic description is one whatever the rows say, and a scope
claim on the key's position beats the verb. A link chain leads link by link (`Follow`): a declared link to its
target, `root`, `prev`, `this` and `scope:x` by the record (`game.ScopeLinks`); `save_scope_as` records the scope
under its name for the entry. Two readers use the walker: `countFile` and the index's `Collect`;
the doc and usage merges call `Enter` on a synthetic statement.

`WalkFile` is the one walk of a whole file: it reads every statement in source order, outer before inner, decides
at each whether it defines an entry (`entryAt`: by the game's `Layout` rows, the file type and the file's
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
empty comment or a heading keeps the run together. `claimsOf` reads a node's claims from its value slot and each
line of its prose through `docPhrases`; what no phrase reads is the blurb.

The two merges differ only in where a key is placed. `mergeDocs` places each key where the walker reads it under
the entry (`PlaceKey`), writes the node's claims where vanilla writes that position, and reads a key vanilla
writes nowhere below as a template (an example entry's name, a `<key>`) whose block merges in the holder's
place. `mergeUsages` places a usage's arguments under the command's `Args`, made on write; the nodes that stand
for the command are `usageRoots`: those named as it, else the one top statement (an alias family's sibling
name), else the tree itself when the usage lists the arguments.

## Names

A key is named by every key rule that fits (`keyOccurrences`), a value by the first value rule that fits (`valueOccurrence`):

| Rule | Where | Occurrence |
|---|---|---|
| history | a history entry's top key, and its value where it is written key = value | a reference to the object it sets up (`Of`) |
| file ref | a value at a key its file type lists (`FileType.Refs`: a GUI `using`) | a reference to that entry |
| list | a call starting with `every_`, `any_`, `random_`, `ordered_` in a triggers, effects or value block | a reference to that scripted list |
| call | a key read as a call | a call reference (`Caller`: the word the block resolves calls by, one of the database's `Callers`) |
| parameter | a key in a call's block | a reference to the called entry's parameter, `<entry>:<NAME>` |
| made | the value of a name command that makes a name (`save_scope_as`, `set_variable`) | a definition in the name's database |
| group | a value at a folder's `Names` key | a definition the entry's database names (`IsName`) |
| read | the value of a name command that reads one (`has_variable`) | a reference into the name's database |
| claim | a value at a position typed as a database, a loc key or a file | a reference to it; a number at a position vanilla typed is none |
| value | a scalar at a position declared to take a number | a call to a script value |
| event | a value written as an event id (a number is none) | a reference to the events database |

Always, on any scalar: each `prefix:name` part a link reads is a reference into the prefix's database
(`prefixReferences`, by `Schema.Prefixes`), and a scalar with such a part takes no value rule; each `$NAME$` in an
entry of a folder with `Parameters` defines a parameter at its first use in the file (`parameterDefinitions`). A
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
