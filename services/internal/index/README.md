# lang/index

What a scan found: each file's definitions, references and localization entries, kept per index and resolved by
name at query time in the game's load order. The index infers nothing: what a written scalar defines or refers to
is the schema's (`schema/names.go`); the index collects it.

| file | holds |
|---|---|
| `index.go` | `Index`, `Def`, `Ref`, `Loc`, `Records`; storage (`Add`, `Drop`, `Forget`, `Rebuild`, `PlaceFiles`) and the lookups (`Loaded`, `Lookup`, `Resolve`, `RefsTo`, `Winner`, `WritesInScope`, …); `Fires`, `Runs`, `Scripted` |
| `records.go` | `Read`, the collector; `recordsOf` by format, `locRecords` and `conceptReferences` for localization |
| `scan.go` | `Scan`, `BuildSchema`, `ScanMods`, `Reindex` |
| `cache.go` | the cache file: `SaveInstall`, `LoadInstall`, `CachedAt`, `DropInstall` and its name |

## The records (`records.go`)

`Read(schema, file, parsed, visit)` makes each entry `FindEntries` returns a `Def`, then walks the file once with
`schema.WalkFile` and turns each reading's `Names` into a `Def` or a `Ref` with its file, line and entry (`From`,
the entry's index in `Defs`), and keeps the scope each command, link or link-read scalar is written in
(`scopePair`). `visit` sees each reading: the index passes none, diagnostics judges each statement, and the editor
stops at the cursor. A loc file is read by `locRecords`: each `$key$` in a text is a loc reference, and `[x|E]` or
`Concept('x', …)` a reference to a game concept (`conceptReferences`).

## The index (`index.go`)

An `Index` is one inventory (`Files`) with each file's `Records` by path, its `Roots` in load order,
and, for the mods' index, the folders its descriptors unload (`Replaced`). `Add`, `Drop` and `Rebuild`
keep the lookup maps; `Rebuild` builds them again after a cache load. `Add` keeps one copy of each record
string (names, databases) and gives every record its file's path, so no record pins a file's text
and a cache load does not copy a name per record. `Repeated` marks the databases where
vanilla defines one name in two files: there `duplicate-definition` never speaks.

**Scopes.** `Add` moves a file's scope pairs into `Index.Scoped`. The install's pairs are vanilla's evidence:
`WritesInScope` answers whether vanilla writes a word in a scope, and `wrong-scope` accuses only a pair vanilla
never writes.

**Load order.** `Lookup`, `LookupRefs` and `LookupLocs` give a name's items across the indexes in the
game's load order: a file is hidden by a later file at its `Rel`, or by a later index's `Replaced`
folder (`Loaded`); the rest sort by `Rel`. `Names` is every name a database defines, for completion.
`RefsInside` is the references written inside one definition.

**Resolution.** `Resolve` is the one resolver: a call looks in the databases its CalledIn word names
(`Schema.Callees`); a scope the game saves from code, a loc key or a file path is known with no
definitions; anything else is `Lookup`. A local definition answers only its own file. `RefsTo` is
Resolve turned around: every reference to a name of a database, the calls into it included.

**Entry modes.** `Winner` applies a name's definitions in load order, plain first, then each entry
mode in the game's processing order: a plain definition or a replacing mode takes the winner's place
(an earlier one holds in a first-wins database); an inject adds to it; an `INJECT:` or `REPLACE:` with
nothing before it is an orphan. A plain on_action, a `Named` and a `Local` definition never take a
place. Two mods contest a name when one takes the other's place.

**Fires.** `Ref.Fires` is a reference to an event or on_action outside a trigger; `Ref.Runs` is a
call, or a name of a database called in effects, outside a trigger, whose fires are fired through it.
`Def.Scripted` is a definition a reference may run: one of a database some block calls. The event
graph's `session.Fires` and `session.FiredBy` follow these three and nothing else.

## The scan (`scan.go`)

`Scan(ctx, Request)` gives an install's `Install{Schema, Index, Scanned, Previous}`:

1. **Cache.** `LoadInstall` answers when the cache file exists (`cache.go`).
2. **Inventory** of the install and the dump folders.
3. **Localization** (`readLocalization`), read once in english and the workspace language; its keys feed the
   schema's loc type test, its records the index.
4. **Schema.** `schema.Build` on the user's install, with the local dumps when every dump file is
   there, else the shipped dump part (`schema.LoadShipped`). `BuildSchema` is the generator's dump part.
5. **Index.** Every script, table and sound list file is read on a worker per CPU and added in file order
   (`walkFiles`). A mod's localization entries become records too.
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
