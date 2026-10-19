# appdata

PMT's own files on this machine: the app's folders, the write that keeps a reader from seeing half a file, the one
load-and-write pair of the app's JSON files, and the stored form of a cache or shipped file. Nothing here understands
the script language.

| file | holds |
|---|---|
| `appdata.go` | `Dir` and `CacheDir`; `Write`, `WriteBytes`, `CopyFile` and `IsLeftoverTemp`; `LoadJSON` and `SaveJSON`; `Encode` and `Decode` |

## Decisions

- **Every save goes through `Write`:** a temporary file beside the target, synced, then renamed over it, so a reader
  never sees half a file; where the rename is refused and the old file removed, a failure leaves the new contents in
  the temporary file.
- **The app's JSON has one pair.** `LoadJSON` and `SaveJSON` serve `config.json`, the wiki's sidecars and the Steam
  snapshot's `listing.json`; a byte order mark on load is dropped. A mod's own `metadata.json` is a file a modder
  writes, so it goes through `inventory`'s text and keeps its mark.
- **The stored form is gob inside gzip** (`Encode`, `Decode`), the one form of a cache and of a shipped schema.
- **No file carries a format version.** A changed struct is another cache file by the cache's own name (a development
  build's key hashes the engine's code); there is no migration.
