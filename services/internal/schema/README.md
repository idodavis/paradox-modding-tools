# schema

What the sources state about one game version's script: each database's positions with their claims and
vanilla's evidence, each command the dumps declare, and the one walker every reader of script uses. It decides
whether a claim may accuse a user's file (`MayAccuse`) and never looks at a mod.

| file | holds |
|---|---|
| `schema.go` | the types (`Schema`, `Database`, `Position`, `Claim`, `Command`, `Evidence`), `Link`, `DatabaseNamed`, `DatabasesWhere`, `MayAccuse` |
| `build.go` | `Build`: one function per source, in trust order, each saying what it reads and writes; `DatabaseNames` for the scan |
| `walk.go` | the walker: `WalkBlock`, `EntryContext`, `ReadKey`, and the four decisions a key leads to (`keyMeaning`, `blockKindOf`, `commandBlockKind`, `blockScope`) |
| `entries.go` | the entry detector, `FindEntries`: which definitions a file makes, by its folder's row, its nested rows and its file type |
| `evidence.go` | `recordEvidence`, what vanilla writes at each position, and `inferValueTypes`, the type its words share where nothing declares one |
| `dumps.go` | the reader of the console dumps: the label table, one reader per record kind, the usages |
| `docs.go` | the reader of docs, wiki examples and dump usages: the normalizer, the phrase catalog, the joiner |
| `shipped.go` | the backup schemas embedded from `shipped/`, `LoadShipped`; `gen/` writes them |

## Sources, claims and accusing

Sources write in trust order: the registry (the game's rows), the dumps, vanilla, the docs, the wiki. A
claim keeps the first source that states it (`SetFirst`), so a later source fills only what an earlier one
left empty. The dumps, docs and wiki declare; vanilla gives evidence (`Position.Evidence`: how often and in
what forms it writes a position) and a value type only where nothing declares one. `MayAccuse` is the one
rule for when a claim may accuse a user's code: a declared source states it, that source is not stale, and
vanilla's evidence at the position does not contradict it; a docs or wiki claim, and any declared word list,
also needs vanilla to write the position. The registry and vanilla never accuse.

## Positions and the walker

A position is a place in a database's entry tree or a command's arguments, keyed by a key's written form: a
number is `<number>`, a date `<date>`, a `prefix:x` word `<prefix:>`, any other key itself. `WalkBlock` reads
a block's statements at a `BlockContext` (position, block kind, calling word, scope, inside a trigger or an
entry); each key becomes a `Reading` by `ReadKey`, which decides in order what the key is (`keyMeaning`, by the
block kind's rules in `game.Blocks`), what its block holds (`blockKindOf`, `commandBlockKind`) and the scope
that block runs in (`blockScope`). Each decision's doc comment states its order. Four readers use the walker:
`recordEvidence`, `applyDocClaims`, `applyUsage` and the index's `Walk`.

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
