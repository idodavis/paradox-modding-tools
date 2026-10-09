# schema

What the sources state about one game version's script: each database's positions with their claims and
vanilla's evidence, each command the dumps declare, and the one walker every reader of script uses. It decides
whether a claim may accuse a user's file (`MayAccuse`) and never looks at a mod.

Read `build.go` first. `Build` makes one game version's schema in seven steps, in trust order, and each step is a
function named for it, in the file named for the step: `declareDatabases` in `build.go`, `dumpsOrShipped` in
`dumps.go`, `parseDocsAndExamples`, `collectDocFields` and `applyExamples` in `docs.go`, `countVanilla` and
`inferTypesAndPrefixes` in `vanilla.go`.

| file | holds |
|---|---|
| `schema.go` | the types (`Schema`, `Database`, `Position`, `Claim`, `Command`, `Vanilla`, `Shapes`, `ExpectedKind`), `Link`, `DatabaseNamed`, `DatabasesWhere`, `MayAccuse` |
| `build.go` | the pipeline: `Input`, `Build`, `DatabaseNames` for the scan; step 1, declaring the databases (`declare`, `declareDatabases`) |
| `dumps.go` | step 2: `dumpsOrShipped`, then the parsers of the console dumps: the label table, one `parse…` per record kind, the usages |
| `docs.go` | steps 3, 4 and 7: `example`, `parseDocsAndExamples`, `collectDocFields`, `applyExamples`, then the normalizer, the phrase catalog, the joiner |
| `vanilla.go` | steps 5 and 6: `countVanilla` and `countFile`, what vanilla writes at each position, and `inferTypesAndPrefixes`, the kind its scalars share where nothing declares one |
| `entries.go` | the entry detector, `FindEntries`: which definitions a file makes, by its folder's row, its nested rows and its file type |
| `walk.go` | the walker: `WalkFile`, `WalkBlock`, `EntryContext`, `PlaceKey`, and the four decisions a key leads to (`keyMeaning`, `blockKindOf`, `commandBlockKind`, `blockScope`) |
| `mentions.go` | the mention rules: which entry each scalar of a placed statement stands for (`keyMentions`, `valueMention`, `prefixReferences`, `parameterDefinitions`); the `Localization`, `File` and `Parameter` databases |
| `shipped.go` | the backup schemas embedded from `shipped/`, `LoadShipped`; `gen/` writes them |

## Sources, claims and accusing

Sources write in trust order: the registry (the game's rows), the dumps, vanilla, the docs, the wiki. A
claim keeps the first source that states it (`SetFirst`), so a later source fills only what an earlier one
left empty. The dumps, docs and wiki declare; vanilla gives evidence (`Position.Vanilla`: how often and in
what shapes it writes a position) and a value type only where nothing declares one. `MayAccuse` is the one
rule for when a claim may accuse a user's code: a declared source states it, that source is not stale, and
vanilla's evidence at the position does not contradict it; a docs or wiki claim, and any declared word list,
also needs vanilla to write the position. The registry and vanilla never accuse.

## Positions and the walker

A position is a place in a database's entry tree or a command's arguments, keyed by a key's written form: a
number is `<number>`, a date `<date>`, a `prefix:x` word `<prefix:>`, any other key itself. `WalkBlock` reads
a block's statements at a `BlockContext` (position, block kind, calling word, scope, inside a trigger or an
entry); each key becomes a `Placement` by `PlaceKey`, which decides in order what the key is (`keyMeaning`, by the
block kind's rules in `game.Blocks`), what its block holds (`blockKindOf`, `commandBlockKind`) and the scope
that block runs in (`blockScope`). Each decision's doc comment states its order. Four readers use the walker:
`countFile`, `applyDocClaims`, `applyUsage` and the index's `Collect`.

`WalkFile` is the one walk of a whole file: given the entries `FindEntries` returned, it reads every statement in
source order, outer before inner, and each `Placement` carries the entry it is in (`Entry`, an index into the
entries, -1 outside one), what its key and value name (`Mentions`, by the rules of `mentions.go`) and the database a name
command's value names there (`StoredIn`). `WalkBlock` reads a snippet (the docs, the dumps' usages): no entries,
no names.

## Names

A key is named by every key rule that fits (`keyMentions`), a value by the first value rule that fits (`valueMention`):

| Rule | Where | Mention |
|---|---|---|
| history | a history entry's top key, and its value where it is written key = value | a reference to the object it sets up (`Of`) |
| file ref | a value at a key its file type lists (`FileType.Refs`: a GUI `using`) | a reference to that entry |
| list | a call starting with `every_`, `any_`, `random_`, `ordered_` in a triggers, effects or value block | a reference to that scripted list |
| call | a key read as a call | a call reference (`Called`: the block's CalledIn word) |
| parameter | a key in a call's block | a reference to the called entry's parameter, `<entry>:<NAME>` |
| made | the value of a name command that makes a name (`save_scope_as`, `set_variable`) | a definition in the name's database |
| group | a value at a folder's `Names` key | a definition the entry's database names (`Named`) |
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
