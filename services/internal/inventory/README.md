# inventory

Every file of a game install, a mod or a dump folder, and what every reader of a file shares: which folders mirror the
game layout, what each file is relative to, where it came from, which reader its format needs, the file's text and how
it was stored, how paths are compared and joined, the ignore rules a folder is filtered by, and the one rule for a name
made from a user's text. It reads a file's text for one purpose of its own, to tell the prose a game ships beside its
script from the script itself; it interprets nothing else.

| file | holds |
|---|---|
| `inventory.go` | the format table; `Files`, `Roots`, `Classify`; `Walk`, `Hidden` and `Children`; `Ordered`, the scan's worker pool; `Binary` |
| `text.go` | `Text`; `LoadText`, `File.Text` and `TextOf` (a file's bytes to its text); `SaveText` and `Text.Bytes` (back the way it was stored); `MaxTextBytes`, the cap |
| `paths.go` | `PathKey`, the one key two paths are compared by; `Join` and `RelOf` |
| `ignore.go` | `IgnoreRules`, `Rules.Covers` and `Rules.Matches`: the git form of an ignore file, for Steam's upload and a search's folders |
| `filenames.go` | `FileName`, the one rule for a file or folder name made from a user's text |

## Decisions

- **One format table for every game.** A file's `Format` (`Script`, `Loc`, `Table`, `List`, `JSON`, `Prose`, `Image`,
  `Skipped`, `Descriptor` or `Unknown`) is decided by its extension and folder here, once, so no reader classifies
  again. `.mod` is the descriptor, `.info` is prose, and no game ships or reads a `.yaml` localization.
- **Origin is a root's.** The install's root, each DLC folder and each mod are roots in load order; a file carries its
  origin and its path relative to its root.
- **A file's text has one reader and one writer.** `LoadText` takes a file's bytes to `Text`: the content without a
  byte order mark, line ends kept, Windows-1252 read where the bytes are not UTF-8, and the byte order mark and the
  encoding remembered so `SaveText` writes the file back the way it was. A binary file (a NUL among the first
  `BinaryHead` bytes) is `ErrBinary`, never a text, found from the file's head before the rest is read, so a search
  over a mod's meshes and images reads a few kilobytes of each; a file past `MaxTextBytes` is `ErrTooLarge`. That cap is
  the editor's, a search's and the scan's, so it is set above the largest text file the three installs ship (56 MB,
  EU5's generated map objects, whose records the index keeps). `File.Text` is `LoadText` of the file's path. `TextOf`
  and `Text.Bytes` are the same two steps over bytes that never touch the disk (git's blobs).
- **One walk.** `Walk` follows links and junctions once, refuses a link back to a folder above, reads folders in name
  order and leaves out every name `Hidden` says: one that starts with a dot, but `game.MetadataDir`, where an EU5 mod
  keeps its descriptor. `Children` is every item of one folder; the explorer applies `Hidden` to it unless the user
  asked for everything.
- **Ignore rules are one matcher.** `IgnoreRules` reads the git form (last match wins, `!` takes a match back, a
  leading `/` anchors, a trailing `/` matches folders, `**` spans folders); `Covers` is the one answer to whether the
  rules leave a file out, for Steam's upload and a search's folders alike: a rule that matches the file or a folder
  above it takes the file in, and no later rule takes it back.
- **`Ordered` makes a scan deterministic.** It runs work on a worker per CPU a bounded window ahead and hands each
  result back in file order on the calling goroutine, so what a scan builds never depends on timing.
