# workspace

One workspace open in the editor: the install's schema and index, the mods' index built over it, the files the editor
has open, the watcher over the mods' folders, and one lock. It decides nothing about words (the schema's walker does); a
query resolves names across the two indexes in load order. The open one is `Session`; the saved record is
`services.Workspace`.

| file | holds |
|---|---|
| `workspace.go` | `Session`: the indexes, the open buffers (`DidChange`, `DidSave`, `DidClose`, `DiskChanged`), the text and parse of a file (`Parsed`, which keeps what a request parses in its `Parses`), `Winner`, the lock |
| `open.go` | `Request` and `Slot`: the one workspace open at a time, built once for many callers |
| `locate.go` | where a path and a definition sit: the file, the mod and the database at a path, a definition's statement, a key's localization and a definition's title, a mod's calendar |
| `fires.go` | what fires what: the events and on_actions a definition fires, through the scripted entries it runs, and what fires it |
| `watcher.go` | debounced changes to the files under a set of roots, folders made later included |

## Decisions

- **One open at a time.** `Slot.Open` closes the open one before another builds, since an install's index is most of its
  memory; callers asking for the same workspace share one build. `Slot` imports no Wails: what happened comes back
  through the request's callbacks and `Closed`.
- **One lock.** A reader holds `RLock` for a whole request; the writers (`DidChange`, `DidSave`, `DidClose`,
  `DiskChanged`) lock themselves and are never called under a read lock.
- **The install's index never changes from the editor.** A saved or changed mod file is walked again into the mods'
  index (`index.Reindex`) and a deleted one forgotten (`index.Forget`); a file of the install or outside any root does
  nothing.
- **A buffer's parse is kept per text,** so a request on the open file parses nothing twice; a file read from disk is parsed once per request, in the `Parses` the request makes, and never kept past it.
