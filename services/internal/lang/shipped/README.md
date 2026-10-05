# lang/shipped

The backup for a user missing a dump file: a schema, never the dumps, that the maintainer builds
on their own install and ships inside the app. `shipped.go` embeds the files and picks one;
`gen/main.go` writes them.

## What is shipped

Only what the dumps declare: the schema's `Commands` (with their argument claims), `Scopes`,
`Saved`, `Modifiers`, `OnActions`, `DataTypes` and `CustomLoc`, the dumps' `Sources` entry, and the
game and version. `Databases`, the docs and wiki claims, vanilla's evidence, its value types and
`Prefixes` are not in it: no vanilla words are shipped. When a user lacks a dump file the scan runs the schema stage on
their own install as usual and takes the dump part from this file (`Input.Shipped`), so
everything else is theirs.

One `<game>-<version>.gob.gz` per game version, gob inside gzip with no format version (`local.Encode`
and `local.Decode`, the install cache's form too). The name is
`shipped.Name(game, version)`, the one builder `Load` and the generator share: `Safe(game + "-" +
version)` + `.gob.gz`, so version `build 24187685` is `eu5-build_24187685.gob.gz`. The files sit
beside `shipped.go` and are embedded by its `//go:embed`; a game with none gives `Load` no schema.

## Load

`Load(game, version)` gives the file of the exact version with `exact` true. With none, it gives the
newest file of the game with `exact` false, which the scan marks stale (a stale source feeds hover
and completion but never accuses). Newest is by the digit runs of the file names taken in order
(`1.20.0.3` over `1.9.2`, `build_24187685` over `build_9`); no table of versions is kept. A game with
no file gives nil and no error.

## The generator

From the repo root, with the game's dump files in place:

    go run ./services/internal/lang/shipped/gen -game ck3
    go run ./services/internal/lang/shipped/gen -game eu5 -dir "D:/Steam/steamapps/common/Europa Universalis V"

`-game` is `ck3`, `vic3` or `eu5`. The install is the first one `game.FindInstalls` detects, or
`-dir`; its version is `game.ReadGameVersion`. The generator runs `index.BuildSchema`, which
is the schema stage stopped after the dumps (`Input.DumpsOnly`), clears the databases, and writes
the file. It stops with a message when it is not run from the repo root, the game
or version is unknown, `BuildSchema` refuses because `schema.MissingDumps` names a dump file missing
from the inventory it lists once (the message lists them: a partial backup never ships), or no dump
is read.

A file of the same version is replaced only when it decodes to a different schema: gob writes maps
in random order, so equal schemas differ in bytes. The generator prints whether it wrote, replaced
or left the file.

Run it:

- after a game update, so the new version has a file;
- with fresh dumps: run the game's script_docs and data type dumps first, so every dump file in
  `schema.DumpFiles` is there (the generator refuses to run without them);
- before an app release, for each game, so the release carries the newest dumps.

The maintainer decides when to ship and commits the files. Old versions' files stay until deleted: they
are the stale fallback for a user on a version with no file.
