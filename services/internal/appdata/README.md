# appdata

PMT's own files on this machine: the app's folders, the write that keeps a reader from seeing half a file, the stored
form of a cache or shipped file, and the names fit for them. Nothing here understands the script language.

| file | holds |
|---|---|
| `appdata.go` | `Dir` and `CacheDir`; `Write`, `WriteBytes`, `CopyFile` and `IsLeftoverTemp`; `Encode` and `Decode`; `SafeName` and `PathKey` |

## Decisions

- **Every save goes through `Write`:** a temporary file beside the target, synced, then renamed over it, so a crash
  leaves the old file or the new one.
- **The stored form is gob inside gzip** (`Encode`, `Decode`), the one form of a cache and of a shipped schema.
- **No file carries a format version.** A changed struct is another cache file by the cache's own name (a development
  build's key hashes the engine's code); there is no migration.
