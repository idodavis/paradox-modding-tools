# steam

The package publishes mods to the Steam Workshop. It keeps a mod's listing in the mod's own files and compares an
upload with Steam, and it uploads from a second copy of the app, which loads the game's own copy of Steam's library.

PMT ships nothing of Steam's. The game's install keeps a copy of Steam's library where the game's `SteamLibrary` row
says, and a second copy of the app loads it through purego and opens it for the game's app id. Of the engine the
package calls `files` for the upload's files and the ignore rules, `game` for the game's rows, and the session's index
for a DLC's name. It adds the listing, the upload set, the compare and the conversion between Markdown, BBCode and the
HTML Steam draws.

| file | holds |
|---|---|
| `steam.go` | the events; `Service`; `ServePublish`; the description's source and the gallery (`SetDescriptionSource`, `AddPreview`, `RemovePreview`); the compare (`CompareUpload`, `CompareFile`); Steam's state and its item (`DescribeSteam`, `FetchItem`, `FetchPreviews`); `Publish`; `ListRules` |
| `listing.go` | the package comment; `Preview`, `Listing`, `Upload`, `Snapshot`, `Details`, `Comparison`, `LineCount`; `ReadListing`, `WriteListing`, `LinkItem`; the upload set (`ListUpload`, `WriteZip`, `ReadIgnore`, `WriteIgnore`); the snapshots (`LoadSnapshot`, `SaveSnapshot`) and `Compare` |
| `workshop.go` | `DLC`, `Rule`, `Rules`; `ListDLC`, `ListTags` |
| `ugc.go` | `Progress`, `Client`: Steam's library bound once through purego, its UGC calls and their call results; `Open`, `Close`, `FetchDetails`, `CreateItem`, `Submit` |
| `bbcode.go` | Markdown to BBCode through a goldmark renderer, `ToBBCode`; BBCode to the HTML Steam draws, `ToHTML` |
| `publish.go` | `Params`; the second copy: its flags, `StartPublish`, the work each flag does, its run |
| `steam_windows.go`, `steam_linux.go` | how each system loads the library, and the packing of Steam's structs there |

## The listing

A `Listing` is what a mod's Workshop page shows, as the mod's own files hold it. The title, the version, the supported
version, the tags and the Workshop id are the descriptor's. The description comes from the source the mod's record
names: Markdown or BBCode, kept in the file the record names or else in the source's own file in the `workshop` folder
(`description.md`, `description.bbcode`), or Steam's own text, which an upload leaves as it is. A mod that names no
source takes the first description file it has, else Steam's text. `SetDescriptionSource` sets the source, and makes
an empty file for a Markdown or BBCode source the mod lacks. The `workshop` folder also holds the changelog, the DLC
the item requires, and the gallery: the pictures of `workshop/previews` and the list of its videos.
Steam and Paradox Mods take BBCode, so the listing's `BBCode` is a Markdown description converted, and any other as
written. `ReadListing` reads the listing and `WriteListing` writes back only the files whose text changed, each through
the workspace, which refuses a path outside the mods. `LinkItem` links a mod to an existing item by its id or the
address of its page, and an empty text unlinks it.

## The upload set

An `Upload` is the set of files a mod's next upload sends. `ListUpload` takes the mod's files and leaves out what the
`Rules` name: files that start with a dot, though the `.metadata` folder stays, the previews, which Steam takes as the
gallery, and what the mod's `.gitignore` and its own exclusions cover, each read as a `files.Ignore`. `WriteZip` writes
the set as a zip. Before an upload, the set is copied into a folder of its own, so Steam is sent those files and no
others.

## The compare

A `Snapshot` is the record of what the last upload of an item sent: the hash of each file, the listing, and a copy of
the files. `Compare` lists what the next upload changes against it: the files added, changed and removed, the listing's
fields that differ, and a status, `new`, `unknown`, `same` or `changed`. `CompareUpload` adds the lines each text file
gains and loses, counted by `go-diff`, and Steam's own `Details` of the item, read by a second copy. `FetchItem` has
Steam download the item, saves it as the mod's snapshot, and compares the next upload with it.

## The second copy

`ServePublish` runs first in `main`. Started with one of its flags, the app does that one kind of work and exits: it
reads its `Params` as JSON on standard input, opens Steam's library for the game's app id, does the work and prints
its answer as JSON lines. The flags upload a mod (`--steam-publish`), read an item's details, its previews or Steam's
state, or download an item. `Publish` starts a copy with `StartPublish` and emits each stage of its progress as
`steam:progress`. A new item's id is saved in the mod's record as soon as Steam creates it, even if the upload then
fails, so the next upload goes to the same item. `DescribeSteam` says why Steam cannot take an upload now, or nothing
if it can.

## BBCode

`ToBBCode` converts a Markdown description to Steam's BBCode through goldmark, with a renderer of its own that writes
each node as Steam's tags and drops raw HTML. `ToHTML` draws BBCode as Steam would, for the page's preview: the text is
escaped, a tag Steam draws becomes its element, and a tag it does not draw stays as text.

## Events

| event | data | sent when |
|---|---|---|
| `steam:progress` | a `Progress` | an upload reaches a stage |
| `release:changed` | the mod's id | a mod's listing files were written |

## Terms

- **Listing**: what a mod's Workshop page shows, as the mod's own files hold it.
- **Preview**: one picture or video in the page's gallery.
- **Upload**: the set of files a mod's next upload sends.
- **Rule**: one pattern of files an upload leaves out, with its reason.
- **Snapshot**: the record of what the last upload of an item sent.
- **Details**: what Steam reports about an item.
- **Comparison**: what the next upload changes against the last one, beside what Steam reports.
- **LineCount**: how many lines a change adds to a text and removes from it.
- **Client**: a connection to the game's copy of Steam's library, in a second copy of the app.
- **Params**: everything a second copy needs to do its work.
- **Progress**: one stage of an upload.
- **DLC**: one DLC of the game an item may require, with its Steam id.
- **Workshop item**: Steam's own word for a mod's page on the Workshop (`CreateItem`).
