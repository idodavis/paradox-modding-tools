# files

The package lists the files of an install, a mod or a dump folder and tells what format each one is. It reads and
writes a file's text in its own encoding, compares and joins paths, and applies ignore rules.

It knows no game. Which folders a game reads and what each one holds are `game`'s rows. This package names a file by
its place under a root and by its extension, and nothing more.

| file | holds |
|---|---|
| `files.go` | `Format` and its constants, `Classify`, `Format.IsScript`, `Format.IsText`; `Root` and `File`; `Walk`, `IsHiddenDir`, `IsBinary`; `Ignore`, `ParseIgnore`, `Ignore.Match`; `Workers` |
| `contents.go` | `Text` and `Encoding`, `TextLimit`, `ReadText`, `DecodeText`, `Text.Encode`, `WriteText`, `WriteAtomic`; the gob caches, `LoadGob` and `SaveGob` |
| `paths.go` | `ToKey`, `ToRel`, `Join`, `IsUnder`, `ToSlug`, `IsFolder` |

## Roots and files

A `Root` is one folder laid out like the game's own: the game's folder, a DLC's or a mod's. Its `Origin` is
`game.OriginVanilla` or the mod's id. A `File` is one file under a root. It carries its full `Path`, its `Rel` from the
root, its root's `Origin`, its `Format` and its `Key`.

`Walk` lists every file under one root, sorted by `Rel`. It skips the folders `IsHiddenDir` names: every dot folder but
`.metadata`, where Vic3 and EU5 keep a mod's descriptor. It follows a link to a folder. When one root sits inside
another, the file belongs to the inner one; that rule is `index`'s, in `index.ListFiles`.

## Formats

`Classify` gives a file its `Format` from its extension and its folder, never from its contents. A `.yml` file is
localization only under a `localization` folder. A `.mod` file is a descriptor only at the root, and `metadata.json` is
one only in `.metadata`. Script written directly at a root is text, since the games never read it. A `.jpg`, `.jpeg`,
`.gif` or `.webp` is a web image: a picture the games do not read, such as a Workshop preview, which the app shows
but never offers as a texture. An extension the switch does not know is text too.

`IsScript` is true for the formats written in the script language, which `jomini` parses: `.txt`, `.gui`, `.asset`,
`.font` and `.gfx`. `IsText` is true for every format but images and binaries.

## Text and its encoding

A `Text` is a file's text with the `Encoding` it was stored in, so that a write puts it back the same way. `DecodeText`
reads a byte order mark as UTF-8 with one, valid UTF-8 as UTF-8, and any other bytes as Windows-1252. `ReadText` refuses
a file over `TextLimit` and a file that `IsBinary` flags, which means a NUL byte in its first kilobyte; both errors wrap
`ErrBinary`. `Text.Encode`
writes the text back in its encoding. A character that Windows-1252 cannot hold becomes `?`.

Every write goes through `WriteAtomic`. The bytes go to a temporary file beside the target, which is then renamed over
it, so a file is replaced whole or not at all. `WriteText` and `SaveGob` write that way. `LoadGob` and `SaveGob` hold
the gob caches. A cache that is missing or does not decode loads as false, and the caller builds it again.

## Paths and keys

Two paths are compared by their `Key`: the path cleaned, lower-cased and written with forward slashes, whichever
platform wrote it. A map of files is keyed by it. `ToRel` gives a path from a root, or false if the path is not under
the root. `IsUnder` asks only that. `Join` puts a root and a `Rel` back together for this platform. `ToSlug` turns a
user's text into a file or folder name: lower case, ASCII only, words joined by `_`, and a letter with a mark folded to
its plain form.

## Ignore rules

An `Ignore` is a list of rules with gitignore's meaning. A rule with a slash anywhere in it is anchored to the root,
`!` negates a rule, and a trailing slash makes a rule cover folders only. `Match` judges each folder above a path first,
since a folder the rules cover takes everything inside it. Among the rules, the last one that matches wins. The globs
are `doublestar`'s.

## Workers

`Workers` is how many goroutines a parallel pass over files runs: every core but two, so the rest of the machine stays
free, and at least one. Every parallel pass in the app takes it from here.

## Terms

- **Root**: one folder laid out like the game's own, with its origin.
- **File**: one file under a root: its path, key, rel, origin and format.
- **Rel**: a file's path from its root, with forward slashes.
- **Key**: the one text that two paths are compared by.
- **Origin**: whose a root is: `game.OriginVanilla`, or a mod's id.
- **Format**: the class of a file, told by its extension and its folder.
- **Text**: a file's text, with its encoding.
- **Encoding**: how a text file was stored: UTF-8, UTF-8 with a byte order mark, or Windows-1252.
- **Slug**: a user's text made into a file or folder name.
- **Ignore**: a list of ignore rules with gitignore's meaning.
