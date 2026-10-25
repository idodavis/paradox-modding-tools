# index

The package records what a scan found in the files of an install or a workspace's mods: every definition, reference
and localization message, with lookups by name. It decides which file and which definition the game keeps when several
compete, lists what fires what, and loads an install's index from its cache.

It stores nothing per statement but those three. Hover, completion, diagnostics and the rest walk the file again when
asked; the index only says where a name is defined and used. The rules it applies are `game`'s rows, read through the
schema's databases.

| file | holds |
|---|---|
| `index.go` | `Role` and its constants, `Occurrence`, `Table` with `Intern` and `Lookup`, `Site`, `Posting`, `Index`; the accessors of a site (`Occurrence`, `File`, `Language`, `Text`); `FindFile`, `IsListed`, `ListOwnFiles`, `FindRoot` |
| `scan.go` | the scan and the load: `Scan`, `ScanMods`, `LoadIndex`, `Index.ScanFiles`, `Index.Clone`, `ListFiles`; the scan's visitor, `collector`; the messages of a localization file, the references in their text, and the rows of a table file; `ToCaseless` |
| `lookup.go` | the lookups by name: `Resolve`, `ResolveLocal`, `ListDatabases`, `ListUses`, `ListCallees`, `ListDefinitions`, `ListMessages`, `FindDefinition`, `ToReference`, `CountDefinitions`; the postings |
| `combine.go` | what the game keeps: `Winner` with `IsKept`, `FindWinner`, `FindKept`, `FindKeptLocal`, `ListKept`; the file the game reads at a path, `FindLoaded`, `IsLoaded`, `ListImages`, `ResolveTexture`; the load order |
| `fires.go` | `Fire`, `ListFires`, `ListFiredBy`; the rule that marks a reference `Firing` |

## Occurrences and tables

An `Occurrence` is the stored form of one definition, reference or message, compact enough to keep for every file of
the game. Its `Role` says which of the three it is. Its database, name, entry mode and language are ordinals into the
index's four `Table`s, `Databases`, `Names`, `Modes` and `Languages`. `Table.Intern` gives a text its one ordinal, and
ordinal 0 of each table is the empty text. An occurrence also keeps its span, its line, CK3's override priority, whether
it is local to its file or an alias of the definition it sits in, the defining occurrence it sits inside (`Parent`,
where `Nested` is set), whether a reference fires what it names (`Firing`), and the statement that writes it.

`Occurrences` holds a list per file, in the order `Files` lists the files, each in source order with a definition before
what it holds. So the references inside a definition follow it without a break. `Texts` runs beside it and holds each
message's text.

A `Site` is a handle to one occurrence: a file's position and the occurrence's position in it. Its text comes from the
file's tree, which the workspace keeps; the index stores no source.

## The install's index and the mods'

There are two indexes in an open workspace. The install's is scanned once for each version of the install and its
dumps, and cached. The mods' index stands on it. It starts with copies of the install's files, roots and tables, so a
file's position and an ordinal mean the same in both, and a `Site` names one file in either. The mods' index scans and
posts only the mods' own files. A lookup reads the install's postings first and its own after them, so the result is in
load order.

`ScanFiles` scans changed files again after they change on disk, and refuses a file of the install. A file deleted
since the scan keeps its position, which a new file at the same path takes again. `IsListed` tells a listed file from a
deleted one. `Clone` copies what `ScanFiles` writes, so a rescan changes a copy while readers go on reading the
original.

## The scan

`Scan` builds the install's index and `ScanMods` the mods'. Each runs over the files in parallel on `files.Workers`
goroutines.

- A script, GUI, asset, font or GFX file is walked once by `schema.Walk` with the `collector`, which turns each
  statement's definition and references into occurrences.
- A localization file is read by `loc.Parse`. Each message is an occurrence, followed by what its text refers to: the
  keys between dollar signs, the text formats and icons it names, the names each data call's quoted arguments hold, and
  a game concept that a call names in the concept format.
- A table file of a database named by its first column gives a definition for each row.

A file is built against tables of its own, so building takes no lock. Its ordinals move into the index's tables
afterwards, under a lock taken a few times for the whole file.

The install keeps the texts of its base-language messages only, since another language's text can be read from its
file when asked. The mods keep every language's texts. A database whose names match without regard to case is listed
in `Caseless`, and its names are stored in lower case; `ToCaseless` writes a name as the index stores it.

`ListFiles` lists the files under a list of roots, in root order. A file under a root that sits inside another root is
listed once, under the inner one. `FindRoot` applies the same rule to any path.

## Lookups

`Resolve` returns the definitions that a reference names, in load order. A local definition never counts;
`ResolveLocal` lets the definitions local to the reference's own file count too. A reference with no database names
the definitions in every database that defines its name. A reference in `game.AnyCall`, a call that may name any
callable database, stands for one reference per callable database, as `ListCallees` lists them. `ListUses` returns the
references to a name, and for a callable database the `game.AnyCall` references to it as well.

A localization key is defined by its messages, so its role is `RoleLocalizes` where every other database's is
`RoleDefines`.

## What the game keeps

Two questions decide what the game reads, each answered in one place in `combine.go`.

**The file at a path.** `FindLoaded` returns the file the game reads for a path from a root: the last one in load order.
A mod whose `replace_path` holds the path's folder drops the files of every root before it in that folder. A stage
folder (the game's `StageRoots`) is not part of the path, so the same path in two stages is one path. `FindLoaded` also
returns the files the loaded one hides. `IsLoaded` asks whether the game reads a file at all, and `ResolveTexture` finds
the image file a written texture path names among the files the game reads.

**The definition of a name.** `FindWinner` decides which of several definitions of one name the game keeps, by the
database's `game.Precedence`. The definitions are put in the game's load order first: by their files, where a folder's
files come before its subfolders' and names compare byte by byte with case, or by their roots where the precedence says
the playset decides. Then:

- A database whose precedence names entry modes reads them in load order. A plain definition replaces what came before
  it. A mode that replaces takes its place, a mode that adds joins it, and a mode with nothing to act on does nothing
  unless it creates. `game.EntryModes` says which mode does what.
- Otherwise the first or the last one loaded wins, or the one with the highest override priority, or all of them are
  merged, as the precedence's `Keeps` says.
- Where a `replace_path` dropped some of the definitions, the game decides among the rest, and the reason is `replace`
  if that changes which one wins.

A `Winner` is the kept definition, the reason it wins (`order`, `mode`, `priority`, `merge` or `replace`) and the
definitions merged into it. `FindKept` finds the winner of a name in a database, and `ListKept` the winner of every
name.

## Fires

A `Fire` is one firing: one definition fires another through the reference that names it (`Use`), passing through the
scripted effects in `Via`. `ListFires` returns what a definition fires, and `ListFiredBy` what fires it. What is fired
is an event, an on_action or a decision, the databases in `Schema.FireDatabases`. What fires can be a definition of any
database, such as a character interaction or a journal entry.

A reference fires what it names only where the scan marks it `Firing`. The walker's context at the reference decides:

- In effect script it fires. That is an effect, or the arguments of one: `trigger_event = { id = … }`,
  `start_travel_plan`'s on_actions, a scripted effect's parameters. An effect is a command the dumps declare as an
  effect where it is written, or a call of a scripted effect. A decision fires only through an effect the game's
  `DecisionEffects` names (CK3's `execute_decision`), so `remove_decision_cooldown` or `open_view_data` does not fire
  it.
- In an on_action's own lists it fires: `events`, `random_events`, `on_actions`, `fallback` and the rest.
- In the fields a game's `FiringFields` lists it fires. The schema reads them as a definition's own fields, though the
  game runs what they name: CK3's council tasks' `monthly_on_action`, Vic3's journal entries' pulses. A row is a key
  path from the definition, matched against the statement's `Context.Path` and `Context.Label`.
- As a scope link (`decision:x = { … }`) it never fires, nor inside a trigger block, nor in a preview, a block that is
  shown in the tooltip and never run, such as `show_as_tooltip`'s, whose row in `game.Keywords` sets `Preview`. Nor
  does it fire anywhere else: a GUI file, a localization file, or a definition's other fields and lists, such as a
  situation's `decisions`.

Both directions read that mark and nothing else, so they agree, and a definition that fires itself is listed both ways.
With the follow flag set, a firing through a scripted effect (`Schema.Effects`) is followed into it. Only the
definition the game keeps is called by name, and scripted effects that call each other end the walk.

## The cache

`LoadIndex` loads an install's index from its gob file. The gob holds the exported fields only, so the load builds the
tables' lookups, the paths and the postings again. The workspace saves the index beside the schema, in a folder named
for the install's version, its dumps and the app's version.

## Terms

- **Index**: what a scan found in one list of files, with the postings that answer lookups by name.
- **Occurrence**: the stored form of one definition, reference or message.
- **Site**: a handle to one occurrence: a file's position and the occurrence's position in it.
- **Role**: what an occurrence does with its name: defines it, refers to it or localizes it.
- **Table**: the interned texts of one kind, each given one ordinal.
- **Posting**: one group of occurrences: those of one role in one database.
- **Winner**: the definition the game keeps when several share a name, and why.
- **Fire**: one firing: a source, a target, the reference that names the target, and the scripted effects between.
- **Load order**: the order the game reads roots, stages and files in.
