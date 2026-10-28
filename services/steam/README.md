# steam

The package publishes mods to the Steam Workshop. It keeps a mod's listings in the mod's own files and compares an
upload with Steam, and it uploads from a second copy of the app, which loads the game's own copy of Steam's library.

PMT ships nothing of Steam's. The game's install keeps a copy of Steam's library where the game's `SteamLibrary` row
says, and a second copy of the app loads it through purego and opens it for the game's app id. Of the engine the
package calls `files` for the upload's files and the ignore rules, `game` for the game's rows, the workspace for the
descriptor and for every write into a mod, the session's index for a DLC's name, and the git service for a change
note. It adds the listings, the upload set, the compare, the change notes and the conversion between BBCode and the
HTML of the description's editor.

| file | holds |
|---|---|
| `steam.go` | the events; `Service`; `ServePublish`; the gallery (`AddPreview`, `RemovePreview`, `FetchPreviews`); `DescribeSteam`; `Publish`; `ListRules` |
| `listing.go` | the package comment; `Preview`, `Dependency`, `Listing`, `Upload`; `ReadListing`, `WriteListing`, `ListListings`, `CreateListing`, `ImportListing`, `LinkItem`; the upload set (`ListUpload`, `WriteZip`, `ReadIgnore`, `WriteIgnore`) and an older listing's zip |
| `compare.go` | `Snapshot`, `Details`, `Difference`, `Comparison`; `CompareUpload`, `FetchItem`, `CompareFile`; Steam's kept answers; the snapshots (`LoadSnapshot`, `SaveSnapshot`) |
| `notes.go` | `ChangeNote`; `ListChangeNotes` |
| `pictures.go` | the smaller copies of pictures Steam refuses for their size |
| `workshop.go` | `DLC`, `Rule`, `Tags`, `Rules` and the agents' rule; `ListDLC`, `ListTags` |
| `ugc.go` | `Progress`, `Client`: Steam's library bound once through purego, its UGC calls and their call results; `Open`, `Close`, `FetchDetails`, `CreateItem`, `Submit` |
| `bbcode.go` | `ToBBCode`, `ToHTML`, over one table of Steam's tags |
| `publish.go` | `Params`; the second copy: its flags, `StartPublish`, the work each flag does, its run |
| `steam_windows.go`, `steam_linux.go` | how each system loads the library and finds its functions, and the packing of Steam's structs there |

## The listings

A `Listing` is what one of a mod's Workshop pages shows, as the mod's own files hold it. A mod has its own listing,
whose key is "", and an older listing for each older game version it still serves, a separate Workshop item whose key
is that game version.

The mod's own listing keeps its title, version, supported version, tags, thumbnail and Workshop id in the mod's
descriptor, the Workshop id in the mod's record too. Its page files are in the hidden `.workshop` folder:
`description.bbcode`, `changelog.bbcode` (the next upload's change note), `dlc.txt` (the DLC it requires, one id a
line), and the gallery: the pictures of `previews/` and the video links of `previews/videos.txt`. The thumbnail stays
where the game reads it. `.workshop` starts with a dot, so no upload sends it.

An older listing is a folder `.workshop/<key>/`. Its `listing.json` holds its record: the Workshop id, the title, the
two versions, the tags, the thumbnail and the zip its files come from, those two as paths from the mod's folder when
they are inside it, so the file travels with the mod. The rest is the same page files. The key is the
game version without a closing `.*`: `1.19` and `1.19.*` give the key `1.19` and the supported version `1.19.*`, and a
patch such as `1.19.2` stays exact. `CreateListing` makes one from a zip, with the own listing's title followed by the
game version, its tags, thumbnail, description and DLC, and the version of the zip's descriptor. `ImportListing`
makes one from an existing item: a second copy reads the item's details and has Steam download it, its files are
written as a zip where the user picks, and the listing gets Steam's title, description, tags, gallery and DLC, the
downloaded descriptor's versions and the item's id, so its first upload changes nothing that nobody edited. Both
refuse a game version the mod has a listing for. `ListListings` returns the own listing first, then the older ones,
the newest game version first. Every method that takes a key works on either.

`ReadListing` reads a listing. Its `Tags` are the category tags: a version tag in the descriptor is left out, and the
listing's `VersionTag` is the one of Steam's version tags that starts with the supported version's major and minor
numbers, or "" when none does. Steam's tags are fetched once a run when none are kept for the game, and kept. Its
`Dependencies` are the descriptor's, each with the Workshop id of the workspace mod that answers it, as
`workspace.FindMod` finds it, "" when none has one. A mod's Workshop id is its record's, else its descriptor's
(`workspace.Mod.ResolveWorkshopID`). Each gallery picture carries its size.

`WriteListing` is called as the user types. It writes only the files whose text changed, each through the workspace,
which refuses a path outside the mods. It never writes the gallery, which only `AddPreview`, `RemovePreview` and
`FetchPreviews` change, or the Workshop id, which only `LinkItem` changes: a page's copy from before an upload linked
its new item cannot unlink it. The descriptor gets the category tags with the version tag. A thumbnail picked from
outside the mod is copied in: where the game reads it for the own listing, into the listing's folder for an older one.
`LinkItem` links a listing to an existing item by its id or the address of its page, and an empty text unlinks it.

## The upload set

An `Upload` is the set of files a listing's next upload sends. The own listing sends the mod's folder. An older one
sends the files of its zip, extracted once into PMT's cache folder and kept until the zip's path, size or time changes,
with the extracted descriptor's name, versions and Workshop id set to the listing's; the zip itself is never changed.
Either way `ListUpload` leaves out what the rules name: files that start with a dot, though the `.metadata` folder
stays, the coding agents' instruction files the agents' rows name (`agent.ListFiles`), and what the mod's
`.gitignore` and its own exclusions cover, each read as a `files.Ignore`. `WriteZip` writes
the own listing's set as a zip. Before an upload, the set is copied into a folder of its own, so Steam is sent those
files and no others.

## The compare

The compare's reference is Steam. A `Snapshot` is PMT's kept copy of an item's files as Steam has them, with the time
Steam last updated the item. The service keeps Steam's answer about each item: its `Details` (the page, the
subscribers, the gallery, the DLC and the required items) or why they could not be read, with when it asked.
`CompareUpload` asks Steam only when nothing is kept for the item, a failed answer included, so the page can compare at
every save, and comparisons that need the same item while Steam is asked wait for that one answer. `FetchItem` drops
the kept answer and compares again; an upload drops it too. When Steam is asked, a second copy reads the details, and
the item's files are downloaded into the snapshot only when Steam's time differs from the kept copy's.

A `Comparison` lists each `Difference` the next upload makes, each page difference with Steam's side as `Before` and
this side as `After`, the texts it compares. The page: the title, the description (compared with its line ends and
trailing spaces made plain, its paragraphs counted by `go-diff`; an empty description is not sent), and the lists, one
entry a line, `After` being the list as Steam has it after the upload: the tags with the version tag, the gallery (by
each preview's label: a picture's file name without its extension, a video's link), the DLC and the required items.
The files: the version first, against the version in the snapshot's descriptor, then each file added, changed or
removed, by its rel, with the lines a text file gains and loses. Both sides are filtered by the upload's rules, so a
file the upload leaves out never shows. Each file's hash is kept by its path, size and time, and each pair of hashes'
line counts by the pair, so a comparison hashes and counts again only what changed. The status is `new` with no item,
`unknown` with the reason when Steam could not be asked or its files could not be downloaded, else `same` or
`changed`. `CompareFile` returns one file as the snapshot has it and as the upload sends it.

## The second copy

`ServePublish` runs first in `main`. Started with one of its flags, the app does that one kind of work and exits: it
reads its `Params` as JSON on standard input, opens Steam's library for the game's app id, does the work and prints
its answer as JSON lines. The flags upload a listing (`--steam-publish`), read an item's details, read Steam's state,
or download an item.

The library's functions are bound by name. Those an upload needs must be there. The ones that read an item's DLC and
change its required items may be missing from an older library: then the details leave those fields nil, the compare
leaves out their difference, and an upload leaves them as Steam has them.

`Publish` reads the item's details first and makes the update a diff against them; an item the copy made a moment
before has nothing yet, and is not asked. The DLC are removed and added to match the listing, except that when Steam
listed only some of them (one answer holds 32) none is removed. A required item is added for each dependency with a
Workshop id, and removed only when it is one of the workspace's mods that the descriptor does not name, so an item
added on Steam's page stays. The compare lists exactly these changes. The gallery is replaced only when its labels
differ, and the tags are sent with the version tag. A thumbnail or gallery picture of 1 MB or more is sent as a
smaller copy made in a folder of its own in the cache: one with transparency stays a PNG, any other becomes a JPEG,
both scaled down until under the limit. A GIF that big is refused. A new item's id is linked to the listing as soon as
Steam makes it, even if the upload then fails. `steam:progress` reports each stage with its bytes. A canceled call
ends the second copy, and the error says Steam may already have applied part of the upload. `DescribeSteam` says why
Steam cannot take an upload now, or nothing if it can.

## Tags

`ListTags` fetches the tags a game's Workshop browse page declares in the JSON of its `valve-ssr-data` script
(`declaredTags.readytouse_tags`), leaving out those only Steam's admins may set, and keeps them in PMT's cache folder
per app id; when the fetch fails the kept ones are used. The version group is the one whose tags start with a version
number: CK3's "Compatible Version" and Vic3's "Update Compatibility". EU5's page declares none today. The category tags
add those the workspace's mods carry, without version tags.

## Change notes

`ListChangeNotes` drafts the next change note. From git: "Version" and the listing's version, then a line for the
subject of each commit of the mod's folder since Steam last updated the item, by Steam's answer or else the snapshot,
or of every commit for a listing with no item; none when git is not installed, or for a folder in no repository or in
one with no commit. Then, if the mod's root has `CHANGELOG.md`, `CHANGELOG.txt`, `changelog.md` or `changelog.txt`,
its section under the first heading that holds the version as a whole version (`1.2` is not in `## 1.21`).

## BBCode

The description is BBCode. `ToHTML` draws it as Steam would, for the editor and the preview: the text is escaped, a
tag Steam draws becomes its element, a link's address its href and another tag's argument the element's title, and a
tag Steam does not draw stays as text. A link is drawn only for a web page's address (`http` or `https`); any other
link shows its text alone, so nothing in it can run in the app's window. `ToBBCode` turns the editor's HTML back into
BBCode by the same table, with the editor's other elements for a tag (`strong` for `b`, `em` for `i`): a link's
address is its href, a paragraph and a break are line breaks, an element Steam has no tag for gives its contents, and
a tag Steam draws inside a text is written inside `noparse`. A description drawn by `ToHTML` and turned back keeps its
tags, but for a link to anything other than a web page.

## Events

| event | data | sent when |
|---|---|---|
| `steam:progress` | a `Progress` | an upload reaches a stage or moves bytes |
| `release:changed` | the mod's id | a mod's listing files were written |

## Terms

- **Listing**: what one of a mod's Workshop pages shows, as the mod's own files hold it.
- **Key**: which of a mod's listings it is: "" for its own, the game version for an older one.
- **Record**: the fields of a listing that its descriptor or its `listing.json` holds.
- **Preview**: one picture or video in the page's gallery.
- **Label**: what tells one preview from another: a picture's file name without its extension, a video's link.
- **Dependency**: one mod a listing's mod depends on, with the Workshop id of the workspace mod that answers it.
- **Upload**: the set of files a listing's next upload sends.
- **Rule**: one pattern of files an upload leaves out, with its reason.
- **Snapshot**: PMT's kept copy of an item's files as Steam has them.
- **Details**: what Steam reports about an item.
- **Answer**: Steam's details of an item, or why they are missing, kept with when Steam was asked.
- **Comparison**: what the next upload changes against what Steam has.
- **Difference**: one thing the next upload changes on Steam.
- **Change note**: one draft of the note an upload sends with it.
- **Client**: a connection to the game's copy of Steam's library, in a second copy of the app.
- **Params**: everything a second copy needs to do its work.
- **Progress**: one stage of an upload.
- **DLC**: one DLC of the game an item may require, with its Steam id.
- **Workshop item**: Steam's own word for a mod's page on the Workshop (`CreateItem`).
