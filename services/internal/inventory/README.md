# inventory

Every file of a game install, a mod or a dump folder: which folders mirror the game layout, what each file is relative
to, where it came from, and which reader its format needs. It reads a file's text for one purpose only, to tell the
prose a game ships beside its script from the script itself; it interprets nothing else.

| file | holds |
|---|---|
| `inventory.go` | the format table; `Files`, `Roots`, `Classify` and `Walk`; `Ordered`, the scan's worker pool |
| `text.go` | a file's bytes as text: UTF-8 with or without a byte order mark, Windows-1252 where the bytes are not UTF-8 |

## Decisions

- **One format table for every game.** A file's `Format` (`Script`, `Loc`, `Table`, `List`, `JSON`, `Prose`, `Image`,
  `Skipped`, or `Unknown`) is decided by its extension and folder here, once, so no reader classifies again.
- **Origin is a root's.** The install's root, each DLC folder and each mod are roots in load order; a file carries its
  origin and its path relative to its root.
- **`Ordered` makes a scan deterministic.** It runs work on a worker per CPU a bounded window ahead and hands each
  result back in file order on the calling goroutine, so what a scan builds never depends on timing.
