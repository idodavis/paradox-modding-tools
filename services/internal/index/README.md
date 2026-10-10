# lang/index

What a scan found: each file's occurrences (a name defined, referred to or localized), kept per index and resolved
by name at query time in the game's load order. The index infers nothing: what a written scalar defines or refers
to is the schema's (`schema/occurrences.go`); the index collects it.

Read `scan.go` first. `Scan` runs an install through six steps, `ScanMods` a mod set over it, `Reindex` one saved
file again; each step is a function named for it, in the file that holds its half: `fromCache` and `SaveInstall` in
`cache.go`, `inventoryOf`, `localizationOf` and `schemaOf` below `Scan`, `Index.collectFiles` in `collect.go`.

| file | holds |
|---|---|
| `scan.go` | the pipeline: `Request`, `Install`, `Mod`; `Scan`, `ScanMods`, `Reindex`; the steps only these call: `inventoryOf`, `localizationOf`, `schemaOf`, `schemaInput`, `BuildSchema` |
| `collect.go` | step 5: `Index.collectFiles`, `collectFile`, `Collect`, `collector.collect`, `scopePair`, `collectLoc`, `conceptReferences` |
| `store.go` | what a scan keeps: `Occurrence` (the stored form), `At` (a handle), the symbol `table`s, `Index`; `Rebuild`, `add`, `Drop`, `At`, `InFile`, `Forget`, `WritesInScope`, `LocKeys`, `Repeated`; the handle's accessors `Occurrence`, `File`, `Entry`, `Text`, `Language` |
| `lookup.go` | the store's questions in the game's load order: `Lookup`, `FileAt`, `Loaded`, `RefsInside`, `Names`, `Resolve`, `RefsTo`, `Winner`, `Fires`, `Runs`, `Scripted` |
| `cache.go` | steps 1 and 6: `fromCache`, `SaveInstall`, `LoadInstall`, `CachedAt`, `DropInstall` and the cache file's name |

## The collector (`collect.go`)

`Collect(schema, file, parsed, visit)` makes each entry `FindEntries` returns a `schema.Occurrence` that `Defines`,
first and in source order, then walks the file once with `schema.WalkFile` and appends each placement's occurrences
(`Found`), gives every occurrence its line and column, and keeps the scope each command, link or link-read scalar is
written in (`scopePair`). An occurrence's `Entry` is the position, among the file's occurrences, of the definition of
the entry it is in; an entry's definition points at itself. `visit` sees each placement: the index passes none,
diagnostics judges each statement, and the editor stops at the cursor. `collectFile` picks by format, and
`Index.collectFiles` runs it on a worker per CPU, handing each file's result (`collected`) to `add` in file order. A
loc file is collected by `collectLoc`: each entry `Localizes` its key, each `$key$` in a text is a loc reference, and
`[x|E]` or `Concept('x', …)` a reference to a game concept (`conceptReferences`).

## The store and the lookups (`store.go`, `lookup.go`)

An `Index` is one inventory (`Files`) with each file's occurrences (`Occurrences`, parallel to `Files`), its
`Roots` in load order, and, for the mods' index, the folders its descriptors unload (`Replaced`). The stored
`Occurrence` is pointer-free: its strings are ordinals into five symbol tables (`Databases`, `IDs`, `Callers`,
`Modes`, `Languages`), so no occurrence pins a file's text and a cache load copies each word once. A query answers
with handles (`At`), read through `Occurrence()` (the walk's form, strings back), `File()`, `Entry()`, `Text()` and
`Language()`; the zero `At` is no occurrence. `add`, `Drop` and `Rebuild` keep the postings (a role of a name →
its occurrences) and the per-database name lists; `Rebuild` builds them again after a cache load, and marks the
databases where two files define one name (`Repeated`): there `duplicate-definition` never speaks.

**Scopes.** `add` moves a file's scope pairs into `Index.Scoped`, the one derived set the cache keeps, since a
scope pair is not an occurrence. The install's pairs are vanilla's evidence: `WritesInScope` answers whether
vanilla writes a word in a scope, and `wrong-scope` accuses only a pair vanilla never writes.

**Load order.** `Lookup(indexes, role, database, id)` gives a role of a name across the indexes in the game's
load order: a file is hidden by a later file at its `Rel`, or by a later index's `Replaced` folder (`Loaded`); the
rest sort by `Rel`, then by position in the file. `Names` is every name a database defines, for completion.
`RefsInside` is the references written inside an entry's definition.

**Resolution.** `Resolve` is the one resolver: a call looks in the databases its CalledIn word names
(`Schema.Callees`); a scope the game saves from code, a loc key or a file path is known with no
definitions; anything else is `Lookup`. A local definition answers only its own file. `RefsTo` is
Resolve turned around: every reference to a name of a database, the calls into it included.

**Entry modes.** `Winner` applies a name's definitions in load order, plain first, then each entry
mode in the game's processing order: a plain definition or a replacing mode takes the winner's place
(an earlier one holds in a first-wins database); an inject adds to it; an `INJECT:` or `REPLACE:` with
nothing before it is an orphan. A plain on_action, a `Named` and a `Local` definition never take a
place. Two mods contest a name when one takes the other's place.

**Fires.** `Fires` is a reference to an event or on_action outside a trigger; `Runs` is a call, or a name of a
database called in effects, outside a trigger, whose fires are fired through it. `Scripted` is a definition a
reference may run: one of a database some block calls. The event graph's `workspace.Fires` and
`workspace.FiredBy` follow these three and nothing else.

## The scan (`scan.go`)

`Scan(ctx, Request)` gives an install's `Install{Schema, Index, Scanned, Previous}`:

1. **Cache** (`fromCache`). `LoadInstall` answers when the cache file exists (`cache.go`).
2. **Inventory** (`inventoryOf`) of the install and the dump folders.
3. **Localization** (`localizationOf`), read once in english and the workspace language; its keys feed the
   schema's loc type test, its records the index.
4. **Schema** (`schemaOf`). `schema.Build` on the user's install, with the local dumps when every dump file is
   there, else the shipped dump part (`schema.LoadShipped`). `BuildSchema` is the generator's dump part.
5. **Index.** Every script, table and sound list file is read on a worker per CPU and added in file order
   (`collectFiles`). A mod's localization entries become occurrences too.
6. **Save** (`SaveInstall`).

`ScanMods` indexes the mods in playset order; `Replaced` comes from each descriptor's
`replace_path`s. `Reindex` walks one saved mod file again in place; `Forget` removes a deleted one.

## The cache (`cache.go`)

An install's schema and index live in one cache file whose name holds the game, a hash of the install folder,
the game and app versions, and a hash of the language, the install's update time and the dump files' names and
dates, so any change is another file. A dev build's app part hashes the engine's code (the game, jomini, loc,
inventory, schema, index and wiki packages in its repo), so a rebuild that leaves the engine alone reuses the
cache; an empty `App` (tests) never caches. `SaveInstall` writes the file whole or not at all, deletes the older
files of the same install folder, and deletes any cache file no current reader names.
