# lang/index

What a scan found: each file's definitions, references and localization entries (`walk.go`), kept
per index and resolved by name at query time in the game's load order (`index.go`). The scan
(`scan.go`) builds the schema and the index for an install, caches them, indexes a workspace's mods,
and walks one saved mod file again.

## The walk (`walk.go`)

`Walk(sch, file, parsed, visit)` turns one parsed file into `Records`. The schema's walker reads the
tree (it never writes the schema, so walks run in parallel); the name rules in `game/lang-rules.go`
say what each word defines or refers to. Every record keeps its 0-based line and column; `Index.Add`
gives it shared copies of its strings. For a table or sound list (`parsed` nil) only the entry
detector's definitions come back.

**Entries.** `Schema.Entries` gives the file's entries first; each is a `Def`, so an entry's index in
`Defs` is its statements' `Context.Entry` and its references' `From`. A `Local` entry (declared inline
by a folder's keyword in another folder's file) answers only references in its own file.

**Names.** A key is named by every key rule that fits, a value by the first value rule that fits:

| Rule | Where | Record |
|---|---|---|
| history | a history entry's top key | a reference to the object it sets up (`Of`) |
| list | a call starting with `every_`, `any_`, `random_`, `ordered_` in a triggers, effects or value block | a reference to that scripted list |
| call | a key read as a call | a call reference (`Called`: the block's CalledIn word) |
| parameter | a key in a call's block | a reference to the called entry's parameter, `<entry>:<NAME>` |
| made | the value of a name command that makes a name (`save_scope_as`, `set_variable`) | a definition in the name's database |
| group | a value at a folder's `Names` key | a definition the entry's database names (`Named`) |
| read | the value of a name command that reads one (`has_variable`) | a reference into the name's database |
| claim | a value at a position typed as a database, a loc key or a file | a reference to it; a number at a position vanilla typed is none |
| value | a word at a position declared to take a number | a call to a script value |
| event | a value written as an event id | a reference to the events database |

Always, on any word: each `prefix:name` part a link reads is a reference into the prefix's database
(`Schema.Prefixes`); each `$NAME$` in an entry of a folder with `Parameters` defines a parameter.

**Scopes.** Each command or link key, and each value read through a link, written in a known scope
records its `Pair`; `Add` moves them into `Index.Scoped`. The install's pairs are vanilla's evidence:
`wrong-scope` accuses only a pair vanilla never writes.

**Visit.** `visit` gets each statement's `Context` (the walker's `Step`, its `Entry`, and the
database its value names by a name command) in source order, outer before inner; returning false
stops the walk. The index passes none, diagnostics judges each statement, and the editor stops at
the cursor.

## The index (`index.go`)

An `Index` is one inventory (`Files`) with each file's `Records` by path, its `Roots` in load order,
and, for the mods' index, the folders its descriptors unload (`Replaced`). `Add`, `Drop` and `Ready`
keep the lookup maps; `Ready` rebuilds them after a cache load. `Add` keeps one copy of each record
string (names, databases) and gives every record its file's path, so no record pins a file's text
and a cache load does not copy a name per record. `Repeated` marks the databases where
vanilla defines one name in two files: there `duplicate-definition` never speaks.

**Load order.** `Lookup`, `LookupRefs` and `LookupLocs` give a name's items across the indexes in the
game's load order: a file is hidden by a later file at its `Rel`, or by a later index's `Replaced`
folder (`Loaded`); the rest sort by `Rel`. `Names` is every name a database defines, for completion.

**Resolution.** `Resolve` is the one resolver: a call looks in the databases its CalledIn word names
(`Schema.Callees`); a scope the game saves from code, a loc key or a file path is known with no
definitions; anything else is `Lookup`. A local definition answers only its own file.

**Entry modes.** `Winner` applies a name's definitions in load order, plain first, then each entry
mode in the game's processing order: a plain definition or a replacing mode takes the winner's place
(an earlier one holds in a first-wins database); an inject adds to it; an `INJECT:` or `REPLACE:` with
nothing before it is an orphan. A plain on_action, a `Named` and a `Local` definition never take a
place. Two mods contest a name when one takes the other's place.

**Fires.** `Ref.Fires` is a reference to an event or on_action outside a trigger; `Ref.Runs` is a
call, or a name of a database called in effects, outside a trigger, whose fires are fired through it.

## Scan and cache (`scan.go`)

`Scan(ctx, Request)` gives an install's `Install{Schema, Index, Scanned, Previous}`:

1. **Cache.** `LoadInstall` answers when the cache file exists. Its name holds the game, a hash of
   the install folder, the game and app versions, and a hash of the language, the install's update
   time and the dump files' names and dates, so any change is another file. A dev build's app part
   hashes the engine's code (the game, lang and wiki packages in its repo), so a rebuild that
   leaves the engine alone reuses the cache; an empty `App` (tests) never caches.
2. **Inventory** of the install and the dump folders.
3. **Localization**, read once in english and the workspace language; its keys feed the schema's
   loc type test, its records the index.
4. **Schema.** `schema.Build` on the user's install, with the local dumps when every dump file is
   there, else the shipped dump part (`shipped.Load`). `BuildSchema` is the generator's dump part.
5. **Index.** Every script, table and sound list file is parsed and walked on a worker per CPU and
   added in file order. A mod's localization entries become records too; each `$key$` in their text
   is a loc reference, and `[x|E]` or `Concept('x', …)` a reference to a game concept.
6. **Save.** `SaveInstall` writes the file whole or not at all, deletes the older files of the same
   install folder, and deletes any cache file no current reader names.

`ScanMods` indexes the mods in playset order; `Replaced` comes from each descriptor's
`replace_path`s. `Reindex` walks one saved mod file again in place; `Forget` removes a deleted one.
