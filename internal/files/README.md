# files

[Architecture](../../ARCHITECTURE.md) › [Engine](../../ARCHITECTURE.md#the-engine) › files

The files package lists the files of an install, a mod or a dump folder and tells what format each one is. It reads and
writes a file's text in its own encoding, compares and joins paths, unpacks a zip and applies ignore rules.

It knows no game. Which folders a game reads and what each one holds are [game](../game/README.md)'s rows. This package
names a file by its place under a root and by its extension, and nothing more.

**Uses** nothing else of PMT · **Used by** most of the engine and most services.

```mermaid
flowchart LR
  disk[("File on disk")] -->|ReadText| text["Text: the string and its encoding"]
  text -->|edited| text
  text -->|WriteText| atomic["WriteAtomic: a temporary file, then a rename"]
  atomic --> disk
```

## Roots and files

A `Root` is one folder laid out like the game's own, and `Walk` lists every file under one, sorted by `Rel`. It skips
every dot folder but `.metadata`, where Vic3 and EU5 keep a mod's descriptor, and it follows a link to a folder.

When one root sits inside another, the file belongs to the inner one. That rule is the [index](../index/README.md)'s, in
`index.ListFiles`.

## Formats

`Classify` gives a file its `Format` from its extension and its folder, never from its contents:

- A `.yml` file is localization only under a `localization` folder.
- A `.mod` file is a descriptor only at the root, and `metadata.json` only in `.metadata`.
- A `.txt` file whose name starts with `readme` is a readme: the games' are comments only.
- Script written directly at a root is text, since the games never read it.
- A `.jpg`, `.jpeg`, `.gif` or `.webp` is a web image: a picture the games do not read, such as a Workshop preview,
  which the app shows but never offers as a texture.
- An extension the table of formats does not know is text too.

`IsScript` is true for the formats [jomini](../jomini/README.md) parses: `.txt`, `.gui`, `.asset`, `.font` and `.gfx`.
`IsText` is true for every format but images and binaries.

## Text and its encoding

`DecodeText` reads a byte order mark as UTF-8 with one, valid UTF-8 as UTF-8, and any other bytes as Windows-1252. A
`Text` keeps the encoding it was read in, so `Text.Encode` writes it back the same way. A character that Windows-1252
cannot hold becomes `?`.

`ReadText` refuses a file over `TextLimit` and a file with a NUL byte in its first kilobyte. Both errors wrap
`ErrBinary`. `LoadText` gives empty text for a missing file.

Every write goes through `WriteAtomic`, which writes a temporary file beside the target and renames it over the target,
so a file is replaced whole or not at all. A gob cache that is missing or does not decode loads as false, and the caller
builds it again.

`Unzip` leaves out a top folder that holds every file, skips links, and refuses a zip that names a path outside the
folder. `FindZipFolder` finds that top folder, so a zip read in place starts from it too.

## Paths and keys

Two paths are compared by their `Key`, whichever platform wrote them. A map of files is keyed by it. `ToRel` and
`IsUnder` compare by key too. `ToSlug` makes a user's text into a file or folder name: it folds a letter with a mark to
its plain form and drops a letter with none.

## Ignore rules

An `Ignore` has gitignore's meaning. A rule with a slash anywhere in it is anchored to the root, `!` negates a rule, and
a trailing slash makes a rule cover folders only. A folder the rules cover takes everything inside it, and among the
rules the last one that matches wins. The globs are `doublestar`'s.

## Workers

`Workers` is how many goroutines a parallel pass over files runs: every core but two, and at least one. Every parallel
pass in the app takes it from here.

## Files

| file | what it holds |
|---|---|
| `files.go` | Formats and how a file gets one, roots and files, the walk, and ignore rules. |
| `contents.go` | Text and its encodings, atomic writes, gob caches and unzipping. |
| `paths.go` | Keys, relative paths and slugs, and whether a path is a file or a folder. |

## Terms

- **Root**: one folder laid out like the game's own: the game's folder, a DLC's or a mod's.
- **File**: one file under a root: its path, key, rel, origin and format.
- **Rel**: a file's path from its root, with forward slashes.
- **Key**: the path cleaned, lower-cased and with forward slashes. It is what two paths are compared by.
- **Origin**: whose a root is: `game.OriginVanilla`, or a mod's id.
- **Format**: the class of a file, told by its extension and its folder.
- **Text**: a file's text, with its encoding.
- **Encoding**: how a text file was stored: UTF-8, UTF-8 with a byte order mark, or Windows-1252.
- **Slug**: a user's text made into a file or folder name: lower case, ASCII only, words joined by `_`.
- **Ignore**: a list of ignore rules with gitignore's meaning.
