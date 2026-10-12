# steam

Publishes a mod to the Steam Workshop and downloads one back, keeps what a mod holds for its Workshop page, and converts
its description between Markdown and the Workshop's BBCode.

| file | holds |
|---|---|
| `steam.go` | both ends of a publish or download: the parent that starts the child (`Run`) and the child's request loop (`Serve`) |
| `ugc.go` | the child's side: Steam's library opened, its flat ISteamUGC functions bound, an item created, updated and submitted, or one item's page read and downloaded |
| `steam_windows.go` | opens Steam's library with the Windows loader; callback structs are packed to 8 bytes |
| `steam_unix.go` | opens it with `dlopen`; callback structs are packed to 4 bytes |
| `workshop.go` | what a mod keeps under `workshop/`: the description file, the change note, extra previews, `.workshop-ignore`, the upload's file set, the compare with Steam |
| `bbcode.go` | the description format: Markdown written as the BBCode subset Steam shows, and read back |

## Decisions

- **A second process.** Steam ties a process to one game's app id for as long as it lives, so each publish runs in a
  second copy of this executable (`Serve`, started before anything else), and a crash in Steam's library costs that
  process, not the editor. The parent talks to it over stdin and stdout.
- **The game's own library.** The child loads the copy of Steam's library the game ships, where the game table says the
  install keeps it; PMT ships none.
- **The build tag is the one OS mechanism.** What differs between Windows and the rest is in the two small files.
