# lang/index

The index stage: one classify walk per file (`walk.go`) reading the schema, and the records it
produces (`index.go`): each file's definitions, references and localization entries, kept per index
and resolved by name at query time in the game's load order. The scan (`scan.go`) runs the schema
stage and this one over an install, caches the result, indexes a workspace's mods, and walks one
saved mod file again.

## The classify walk (`walk.go`)

`Walk(sch, file, parsed, visit)` reads one parsed file with the schema and returns its `Records`
(definitions and references; `Loc` is filled by the scan, not here). It is the only place that
decides what a written word is (spec §7.2). Every name a record keeps is cloned, so a record never
pins its file's text. Every definition and reference keeps where its `Range` starts (`Line`, `Col`,
0-based), from the parse's line index, so the editor places it without reading the file; for a
table or a sound list `parsed` is nil, only the entry detector's definitions come back, and the
file is read for their lines. The walk reads the schema and never writes it (`Slot` with `create`
false, `Key.At`), so several walks run at once over one schema.

Its rules that completion also applies are exported, so neither copies them: `Called(block, under)`
is the `CalledIn` word a key in a block of that kind calls a name by (`trigger`, `effect`, `*`, or
""); `Prefixed(sch, prefix)` is the database a link prefix's names belong to; `Made(enclosing, key,
command)` is the database a word written as a key's value is a new name in; `ScriptMade(database)`
reports a database of script-made names (one `game.NameCommands` names), which diagnostics read too;
`Pair(written, scope)` is how a command, link or link-read word is kept with the scope it is read in
(its first part's name before any colon, lower-case: `scope:x.liege` is `scope`).

**Vanilla's (command, scope) pairs (D-34).** Each statement whose key reads as a command or link,
and each value word that reads through a declared link, written in a known scope, records its
`Pair` in the records; `Add` moves them into `Index.Scoped` (the records keep none, so no file's
copy is cached). The install's `Scoped` is vanilla's evidence: `wrong-scope` (the command and the
variable-read checks) speaks only for a pair vanilla never writes (§4.3 rule 3). `Ready` never
resets it, so a cache load keeps it; mods add to their own index's, which nothing reads.

`visit` gets each statement's `Context` in source order, outer before inner, before the walk
descends into the statement's block; returning false stops the walk. Callers pass none (the
index), a per-statement check (diagnostics), or a stop at the cursor's statement (hover,
completion, go-to, references).

| `Context` field | Holds |
|---|---|
| `Statement` | the statement |
| `Entry` | the enclosing definition's index in `Records.Defs`; -1 outside any entry |
| `Key` | the statement's position; nil when unknown, and for a link (its block reads at its parent) |
| `Command` | the declared command or link the key reads as |
| `Block` | the kind of block the statement is written in |
| `Scope` | the scope it runs in; "" unknown |
| `InTrigger` | inside a triggers block or a trigger-only command, or the trigger-only command itself |
| `InnerKey` | the position the statement's own block reads at (a link's is its parent's); nil when unknown or outside any entry |
| `InnerBlock` | the kind of the statement's own block |
| `InnerScope` | the scope its own block runs in; "" unknown |
| `InnerTrigger` | its own block reads inside a trigger (`InTrigger` for the statements in it) |

The `Inner` fields are the reading the walk gives the statement's block, so a caller at an empty
block (completion) reads them instead of the rules below.

### Definitions

1. **Entries.** `Schema.Entries` gives every entry in source order; each is a `Def` (database,
   name, mode, the file's path, the key's range for an assignment, else the statement's), so an
   entry's index in `Defs` is its `Context.Entry` and its references' `From`. A whole file's entry
   (a `ByFile` or history row) holds the root's statements as one block.
2. **Script-made names.** `game.NameCommands` maps each command whose value is a new name, and
   each `command/key` whose key in that command's block holds one, to the database the name
   belongs to. The database is named by the link prefix that reads the name: `save_scope_as` and
   `save_scope_value_as/name` → `scope`, `set_variable` and `set_variable/name` → `var`, the local,
   global and list forms likewise. A word value becomes a definition in that database in two
   cases (`Made`). It is the value of a key that reads as a listed command (so the game's dumps
   declare it), matched lower-case. Or it is the value of a key listed under the command whose
   `Args` the block reads at (`set_variable = { name = x }`).
3. **Parameters (D-31).** In an entry whose database's registry row has `Parameters` (scripted
   effects, triggers, modifiers), each `$NAME$` written in a key or a word value is a definition in
   the `Parameter` database (`parameter`) named `<entry>:<NAME>`, its range the `$NAME$` itself; a
   name written several times is defined at each place.

### What a key is (D-17)

Statements outside any entry are visited and nothing about them is classified. Inside an entry,
the statement that defines it starts at its database's `Entry` position. Below that, each key reads
where `Schema.Slot` says:

| Where | The key is | Its block reads at |
|---|---|---|
| a fields block | a field: the position's child, unknown when missing | that child |
| a triggers or effects block, or one nothing declares | a field a declared source states: a command usage's own argument (`create_pop = { culture = x }`, D-34) | that field |
| | else a declared command (not a link), its name matched ignoring case (D-26) | the command's `Args` |
| | else a link (`Schema.Link`: `root`, `scope:x`, `c:FRA`), or a link chain whose first part is one (`root.liege`, `scope:x.culture`) | the parent position |
| | else a data call written `name(…)` (jomini's call form, `jomini.ParseCall`): no field and no call | a block nothing declares |
| | else a field the position holds; in a triggers or effects block only one a declared source states (D-24) | that field |
| | else unknown | a block nothing declares |

A list element sits at the position holding it, and its block reads there too. Command and link
names are looked up lower-case (D-26).

**Calls.** A key that is no command, link or data call is also a call, resolved at query time
(`Resolve`): in a triggers or effects block when no stated field takes it (D-24), named by the
block kind; in a block nothing declares, named `*`, whatever field position it keeps there, but
only when vanilla writes a declared command directly in that block (its position's `Seen.Forms`
holds `FormCommand`, D-29). CK3 writes its event and on_action blocks (`immediate`, `option`,
`effect`) with no declared kind, so a scripted effect or trigger called there is still found;
blocks vanilla never writes a command in (map objects, GUI, coat-of-arms data) record no calls. An
unresolved call never accuses (§8). A fields block holds no calls.

### Scope and triggers

An entry's statements run in its database `Entry`'s `Scope` claim. Inside a statement's block the
scope is, in order: its position's `Scope` claim; where the key leads, when that is a scope the
dumps declare (`Schema.Scopes`): a command's `Out`, or for a link the `Out` of its chain's last
link (`root.liege` leads where `liege` does); the scope it is written in, for a command that is no
link and for a position that states a block kind, when no declared `Out` leads elsewhere (a
command whose `Out` is empty or names no declared scope keeps the enclosing scope; so do `limit`
and `if`); otherwise unknown. So a link leading to no declared scope, a data call, an unknown key
and a field nothing declares all leave the scope unknown. A scope whose dump entry says it neither
evaluates triggers nor executes effects (`Scope.Triggers`, `Scope.Effects`: CK3's `value`,
`boolean` and `flag`, every game's `flag`) leaves the block's scope unknown too (D-34): it is no
scope commands run in. A command written with a word value has no block and changes no scope.

`InTrigger` holds for a statement written in a triggers block (a position whose `Block` claim is
triggers) or below one, for a command whose only kind is trigger (`KindTrigger`), and for
everything below either. A definition starts fresh: true only when its own block is triggers.

### References

A word is a reference only where a declared fact says so; an undeclared word is no reference. A
constant (`@x`) and a word holding a parameter (`$X$`) are never names.

| Written | Rule | `Database` |
|---|---|---|
| any word, key or value, with a part `prefix:name` whose prefix is a declared link (parts split at `.`: `scope:x.var:y`, `root.var:y`) | a script-made name's link (`NameCommands`' databases) is that database; any other prefix names one by D-15 (`Schema.Named`), none → no reference | `scope`, `var`, …; `culture/cultures`, `traits`, `defines`, … |
| a value word at a position whose `Value` is `WordReference` (a declared source's claim, or a registry row's `Refs`: an on_action's `on_actions`, D-35) | the claim's database | the claimed database |
| a value word at a position whose `Value` is `WordLoc` | a localization key | `Localization` (`localization`) |
| a value word at a position whose `Value` is `WordFile` | a file path | `File` (`file`) |
| any other value word that `jomini.IsEventID` reads and that is not a number (`ns.12`) | an event id: the registry declares events are named by it (D-25) | the database whose registry row is `Events` (`Schema.Flagged`) |
| a key the walk reads as a call (Calls above) | `Called` set to the block kind's `CalledIn` word (`trigger`, `effect`), or `*` in a block nothing declares that holds `FormCommand` | "" |
| a key written directly in a call's block (`my_effect = { NAME = x }`) | it passes the called entry's parameter (D-31), named `<call>:<key>` as the definitions are; the call resolves at query time, so does the parameter | `Parameter` (`parameter`) |

A value word read as a link form is not also read by its position's claim. Each reference keeps
its enclosing entry (`From`) and whether it sits inside a trigger (`InTrigger`).

## The index (`index.go`)

An `Index` is one inventory (`Files`, as `source.Inventory` lists it) with each file's `Records` by
path, the folders the inventory was listed from (`Roots`, in load order: the install's, or the mods'
in playset order), and for a mods index the folders its descriptors unload (`Replaced`). Unexported
maps answer queries:

| Map | Holds | Kept by |
|---|---|---|
| `order` | a lower-case path's position in `Files` | `Ready`, `Reindex` |
| `rels` | each lower-case `Rel` of `Files`, with the last position holding it | `Ready`, `Reindex` |
| `defs`, `refs` | pointers into the stored `Records`' definitions and references, by database and name | `Add`, `Drop`, `Ready` |
| `locs` | pointers into the stored `Records`' loc entries, by key | `Add`, `Drop`, `Ready` |

`Add(records, path)` stores a file's records and appends each definition, reference and loc entry
to its list; a caller drops a path before adding it again. `Drop(path)` removes the file's records
and takes each of its entries out of the lists by identity, deleting a list left empty. `Ready()`
builds every map from `Files` and `Records`: before the first `Add`, and after a cache load (gob
skips unexported fields). A record whose path is no longer in `Files` is dropped.

`Scoped` holds the walk's (command, scope) pairs (see the walk above); `Add` fills it and `Ready`
keeps it, so it is the one map besides `Files` and `Records` the cache holds.

`Repeated` marks the databases in which two files of the index define one name (a definition with
a name, in another file than the first of its name): `Ready` empties it and `Add` marks it, so the
install's is computed when the install is indexed or loaded from the cache, and mods never touch
it. In the install's index it is vanilla's evidence that a database's names repeat by design (a GUI
state's `_show`, CK3's tradition parameters); `duplicate-definition` speaks only in databases it
does not mark (§4.3 rule 3).

`At(path)` is the file listed at a path, found through `order` whatever case the path is spelled in
(Windows spells one path by any). The editor uses it to find the inventory's spelling of a path, which
`Reindex` and `Drop` take. `FileAt(indexes, rel)` is the file at a `Rel` across the indexes, through
`rels` whatever its case: the last index's that lists it (`Resolve` and the editor's file cards).
`Forget(path)` removes a deleted file: it takes the file out of `Files` and runs `Ready`, which
renumbers the positions and drops the file's records, so the file no longer hides a file at its
`Rel`. `LocKeys()` ranges over the keys of `locs`, for completion.

### Load order (D-30)

`Lookup(indexes, database, name)` is every definition of a name across the indexes (install, then
mods), `LookupRefs` every reference to it and `LookupLocs(indexes, key)` every localization entry of
a key, all through `gather`, in the game's load order:

1. **Hidden files.** `Loaded(indexes, path)` is the one test of whether the game loads a file, for
   `gather` and the pages alike. A file is hidden when a later file has its `Rel`: one later in its
   own index's `Files` (two mods shipping one path: the later mod's file wins) or any file of a later
   index (a mod's `events/birth_events.txt` unloads vanilla's); and when its `Rel` is under a later
   index's `Replaced` folder. `Rel`s and folders compare lower-case. A later mod's `replace_path`
   does not unload an earlier mod's files in the same index (a known limit).
2. **Order.** The rest sort by `Rel` across all indexes, so a mod's `common/traits/00_a.txt` loads
   before vanilla's `00_traits.txt`. After step 1 equal `Rel`s are one file's, so D-30's "a later
   source first among equal paths" never decides; the sort is stable and keeps a file's items in
   source order.

`Names(indexes, database)` is every name a database defines, for completion: its definitions in each
index's file order go through `gather`, and each name is kept once, at its first place in load order;
a definition that names nothing (`Name` "") gives none.

### Resolution

`Resolve(sch, indexes, ref)` is the one resolver; it gives a reference's definitions and whether
the name is known:

- **A call** (`Called` set) gives the definitions, in `Lookup`'s order, of every database whose
  registry row's `CalledIn` holds that word (`effect` → scripted_effects; `trigger` →
  scripted_triggers, script_values); `*` takes every database with a `CalledIn` (D-29). The list is
  `Schema.Callees[word]`, built once by `Build` in name order, so map order never decides and no
  call loops over the databases; completion, the editor's references and the event graph read it.
- **A scope the game saves from code**: a reference into `save_scope_as`'s database
  (`game.NameCommands`: `scope`) whose name is in `Schema.Saved` is known with no definitions.
- **A `Localization` reference** is known when any index holds a loc entry of that key, in any
  language (`locs`); no definitions.
- **A `File` reference** is known when any index's inventory has that `Rel`, compared lower-case
  (`FileAt`); no definitions.
- **Any other reference** gives `Lookup(indexes, ref.Database, ref.Name)`.

The name is known when there is a definition, or in the three cases known with none.

### Entry modes (`Winner`, spec §7.1)

`Winner(info, defs, origin)` is the one mode function. `defs` is a name's definitions in `Lookup`'s
order; `origin` gives a path's file origin (`vanilla` or a mod's id). It takes the plain
definitions (no mode word) first, then each of `GameInfo.EntryModes` in the registry's order
(`INJECT_OR_CREATE`, `REPLACE_OR_CREATE`, `TRY_INJECT`, `TRY_REPLACE`, `INJECT`, `REPLACE`), load
order inside each:

| Definition | With no winner yet | With a winner |
|---|---|---|
| plain | becomes the winner | takes its place and clears the adds; in a first-wins database (`game.FirstWinsAt`) the earlier one holds |
| a mode that `Replaces` (`REPLACE`, `TRY_REPLACE`, `REPLACE_OR_CREATE`) | `_OR_CREATE` becomes the winner; `TRY_` does nothing; `REPLACE` is an orphan | takes its place and clears the adds |
| an inject (`INJECT`, `TRY_INJECT`, `INJECT_OR_CREATE`) | `_OR_CREATE` becomes the winner; `TRY_` does nothing; `INJECT` is an orphan | is added to the adds, never displacing the winner |

It returns the winner, its adds, the orphans (an `INJECT:` or `REPLACE:` with nothing loaded before
it does nothing in the game; §8's `inject-without-target` reads them) and whether the name is
contested: a plain or replacing definition met a winner of another origin, neither vanilla. CK3,
with no modes, falls out: last wins, or first wins for the listed databases.

### Fires (spec §7.3)

`ref.Fires(sch)` is the fires rule: a reference to an entry of a database whose registry row is
`Events` or `OnActions` fires it unless it sits inside a trigger (`InTrigger`). The event graph's
edges are the firing references out of a definition (`From`). `ref.Runs(sch)` is the one test of a
reference that runs a scripted entry outside a trigger (a call, or a name of a database called in
effects), whose fires are fired through it (`session.Fires`, the event graph).

## Scan and cache (`scan.go`)

`Scan(ctx, Request)` gives an install's `Install{Schema, Index, Scanned, Previous}`. `Scanned` is
when the install was built, kept in the cache so a load shows the original time. `Previous` is the
version in the name of the older cache file the scan replaced (a version has no dash), which
`SaveInstall` sets after writing and the file never holds, so a load leaves it "". The steps:

1. **Cache.** Unless `Request.App` is `"dev"` or empty, `LoadInstall(req)` answers when the file
   exists; a missing or unreadable file means a rescan. The three cache functions take the
   `Request` and build the key themselves (D-28): game, a hash of the install folder, game version
   (D-11), app version, and a hash of the language, `game.InstallUpdatedAt` (it decides whether the
   dumps are stale) and each dump file's name and date. The file is
   `install-<game>-<folder hash>-<version>-<app>-<state hash>.gob.gz` under `local.CacheDir()`
   (`shipped.Safe` for the names), gob inside gzip, with no format version. A game update, an app
   update, a moved install, another language or a new dump file is another file, so a rescan.
   `SaveInstall` deletes the other cache files of the same game and install folder after writing
   (another folder's files stay); `DropInstall` deletes every file of the game and install folder,
   whatever its state. A dev build neither reads nor writes
   the cache, so it rebuilds from the current code. `LoadInstall` gives an index that is ready to
   query.
2. **Inventory.** `inventory` lists the install's files (`source.Inventory` over the install's
   `source.Roots`, which the scan keeps as the index's `Roots`; origin `vanilla`) and the dump
   folders' files (`game.DumpDirs`, origin `dumps`) into the schema stage's `schema.Input`, with the
   wiki sidecar and `game.InstallUpdatedAt`.
3. **Schema.** `schema.Build` runs on the user's own install. When `schema.MissingDumps` names no
   file it reads the dump files (D-5: local dumps win, whatever their age). Otherwise
   `shipped.Load(game, version)` gives the shipped dump part (D-27), its `Sources[SourceDumps]` set
   to `From: "shipped"`, `Stale` unless the version is the shipped file's own, and `Build` takes
   it as `Input.Shipped`; the databases, docs, wiki and vanilla are still the user's own. With no
   shipped file for the game, or one that does not decode, `Build` reads what dump files exist.
   `BuildSchema` is the generator's: the dump part alone (`Input.DumpsOnly`), refused with the
   missing names when `MissingDumps` gives any.
4. **Index.** `Index.Files` is the whole inventory, set before the first `Add`. `index` sends
   every script, table, sound list and localization file through `source.Ordered`: a worker per
   CPU reads it (`File.Text`), parses it and runs `Walk` with no visit (a table or sound list has
   no tree); `Add` runs in file order on one goroutine, so the lists never depend on timing. A
   localization file's entries become `Records.Loc` (key, language, text, path, 0-based line),
   each string cloned so a record never pins the file's text; its language is the header's, else
   the file name's or folder's. Each `$key$` an entry's text interpolates (`loc.Interps`) is a
   `Localization` reference (range the key, `From` -1: in no definition, D-31); a `$key$` the game
   fills from code names no loc key and stays unresolved. In the text's data calls
   (`jomini.Calls`, `jomini.ParseCall`), a one-step call written with the format `E` or `e`
   (`[glory|E]`) and the quoted first argument of a `Concept('x', …)` step are references to the
   database whose registry row has `Concepts` (`game_concepts`, `Schema.Flagged`); none when no
   row has it. Other formats (`|EU`, `|El`) and Vic3's bare `[concept_x]` are not read here: a data
   call step no data type declares is looked up in the `Functions` and `Concepts` databases when
   the editor reads it. The install keeps english and `Request.Language`; mods keep every
   language. No pass runs before this one: a reference resolves at query time (§7.3).
5. **Save.** `SaveInstall(req, install)` writes the file whole or not at all (`local.Write`), then
   deletes the older files and sets `install.Previous` from the first. A failed save returns the
   install with its error.

`Request.Progress` hears the stages `inventory`, `schema`, `index` (after each file) and `saving`.
`CachedAt(req)` is the cache file's modification time, zero for none, for an install this run has
not built.

`ScanMods(ctx, install, mods)` builds one `Index` over the mods' inventories in playset order,
read the same way with every language kept; its `Roots` are each mod's `source.Roots`, in order. `Replaced` holds the folders the mods unload: a CK3
`descriptor.mod`'s `replace_path` lines (read with `jomini.Parse`), a Vic3 or EU5
`.metadata/metadata.json`'s `game_custom_data.replace_paths` (its byte-order mark stripped). Each
is lower-case, slash separated, trimmed of slashes, without a leading stage folder
(`GameInfo.StageRoots`), as an inventory root's `Rel` is. `gather` hides an earlier index's files
under them (Load order).

`Reindex(sch, file, text)` is a saved mod file: `Drop`, parse the given text, `Walk`, `Add`. A file
not yet in `Files` is appended to it, its path listed in `order` and its `Rel` in `rels`. The file
keeps its place in `Files`, so an edit never changes its load order. Nothing else is recomputed
(§7.3). A deleted file is `Forget`, not `Reindex`.
