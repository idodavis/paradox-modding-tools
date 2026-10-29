# workspace

The package holds the one open workspace: the install's schema and index, the mods' index on top of it, the open files
and the watcher. Its service also keeps the records of installs, workspaces and mods.

It is where the engine meets the services. It loads or builds the install's `schema.Schema` and `index.Index`, scans
the mods with `index.ScanMods`, keeps the trees `jomini` parses, and serves files to the page. Every other service
holds the `*Service` and asks it for the session at each call.

| file | holds |
|---|---|
| `workspace.go` | `FileChange`, `Service`, `State`, `Scanning`, `FileText`, `Child`; the events; the installs (`ListGames`, `ListInstalls`, `ListReadyInstalls`, `DetectInstalls`, `AddInstall`, `UpdateInstall`, `ListLanguages`, `DeleteInstall`, `RescanInstall`); `ResetData`; the workspaces (`ListWorkspaces`, `CreateWorkspace`, `UpdateWorkspace`, `DeleteWorkspace`); the mods (`AddMod`, `ListMods`, `CreateMod`, `ResolveModFolder`, `RemoveMod`, `ReorderMods`, `ResetColors`, `UpdateMod`); `Open`, `Session`, `Close`, `State`, `LookupMod`, `ResolveFolder`; `Launch`; the files (`Read`, `ReadHeader`, `Write`, `ListFolder`, `ListRoots`, `Create`, `Rename`, `Delete`, `Restore`, `Locate`); `ServeHTTP` |
| `session.go` | the package comment; `Session`; the open: the install's caches, the mods' scan and the watcher; `ScanOrder`; `Close`; the trees and buffers (`LoadTree`, `SetText`, `CloseBuffer`, `LookupBuffer`); the texts (`LookupText`, `LookupTitle`, `FindTitleKey`); `FindStatement`; `FindDelay` (the delay written for a firing); `Rescan` |
| `mod.go` | `Descriptor`, `Dependency` and `Mod`; `ReadDescriptor`, `ParseDescriptor` (the one reader of a descriptor's text, script or JSON), `WriteDescriptor`; `ListDependencies` (a mod's dependencies, all the way down), `Session.DescribeMod` (a mod's title), `MatchVersion` (whether a supported version fits the game's); `FindMod` (the mod a descriptor's dependency names, by its id or its name); `Mod.ResolveWorkshopID` (the record's Workshop id, else the descriptor's); a new mod's skeleton and its localization file; the thumbnail; `IsOlderRelease` |
| `locate.go` | `Place`, `Session.Locate`, `ListCopies`, `Session.ListRoots` |
| `watcher.go` | `Watcher`: the folders it watches, the bursts of changes it reports together, and `Move` |

## The one open session

The service holds at most one open `Session`. `Session()` returns it, or the error "no workspace is open"; it is Go's
alone and not bound to the page. `Open` closes the session that was open and opens the new one in the background. It
holds the service's lock for the whole open, so a call to `Session`, `State` or `Close` while a workspace opens waits
for it. `install:scanning` reports each stage, and `workspace:opened` or `workspace:failed` ends the open.

A change to a workspace's record that alters what its open read, which is its install, its language, or its mods and
their folders in load order, opens it again. So does moving the install it uses, or rescanning it.

`ResolveFolder` gives the folder a shell or a coding agent starts in: the folder of the mod that holds a file, else
the workspace's first mod whose folder exists, else the home folder. It is Go's alone and not bound to the page.

## The caches

An install's schema and index are cached as two gob files in one folder. The folder is named for the install's
version, the time of its dumps and the app's version, so a new game version, fresher dumps or a new build each scan
again. The open deletes the game's other cache folders. It loads both caches, or builds what one lacks and saves it:
the schema with `schema.Build`, then the index with `index.Scan` over the same listing. An install's version is the
one its files say, else the one its record holds. `ListReadyInstalls` names the installs whose next open scans
nothing.

## The session

A `Session` holds the game's row, the install's folder, the schema, the install's index, the mods' index, the mods in
load order, and the language shown beside the base language. The mods' index sits in an atomic pointer. A reader loads
it once and keeps that copy to its end. A rescan changes a `Clone` and stores it, so no reader takes a lock.

An open file's buffer is the tree of the editor's text, which `SetText` parses and keeps. Any other file's tree is read
and parsed when asked, and kept in a bounded LRU. `LoadTree` returns the buffer's tree, else the cached one, else it
parses the file. `LookupBuffer` gives the editor's text of an open file.

`ScanOrder` scans the mods on the install's index in another load order, and saves the result nowhere. The open scans
through it too. A broken mod, whose folder is missing, is left out.

`LookupText` returns a localization key's text in the session's language, else in the base language, with the message
the game keeps: one in a replace folder wins, for the reason `replace-folder`, in the game's form of the folder that
wins first (`LocReplaceFolders`). Where several messages share the best place, in one form of the folder or outside
it, which the game shows is not documented, so the first loaded comes back with the reason `unsettled`. The install
keeps only the base language's texts, so a game file's text in another language is read from its file when asked.
`LookupTitle` returns the text a definition is shown by: its title key's message, else the message named like it, and
for a game concept the message of the game's concept prefix and its name. `FindStatement` returns the tree and the
statement of a site.

## Places

A `Place` is where one file sits in the workspace: the `files.File` as its root lists it, with its name, its folder, its
root, its database, whether it is read only and whether the game reads it. The game's own files are read only. Every
answer that names a file carries a place, and the page renders its fields as they are. `Locate` makes one, and
`ListCopies` lists the place of every copy of a file's path in load order, the game's first. A place can be of a
folder too, which `IsDir` says: a folder's listing sets it for each entry, and `Service.Locate` for the path it is
given, which may be a folder an agent names.

## Files

Every write is refused outside the workspace's mods and in the game's own files. `Write` writes a text in its own
encoding, atomically. `Delete` moves a file or a folder into PMT's trash and returns where it went, for `Restore`.
Deleting a mod's root folder removes the mod from the workspace too. `ListFolder` lists a folder with the place of each
entry, and leaves out binary files unless the workspace shows them.

`ServeHTTP` serves to the page the file a request names, whether or not a workspace is open, and refuses a path outside
every recorded install and mod. A DDS or TGA is decoded to PNG first, since the browser cannot show it, and so is one
frame of a sprite sheet or one of a DDS's smaller mip levels.

## The watcher

A `Watcher` watches the mods' folders and every folder made under them, the hidden ones left out but the coding
agents' folders (`agentFolders`: `.claude`, `.codex`, `.github`), so that an agent's edit there reaches an open tab and
the explorer. The first event of a burst starts a timer that later events do not reset. When it ends, every path the
burst touched is reported once, as deleted where it no longer exists. The session scans the batch again through
`index.Index.ScanFiles`, the agents' folders left out, since the index skips every dot folder, then emits `fs:changed`
for each path. The watcher judges nothing.

`Rename` and `Delete` move through `Watcher.Move`. It stops watching the folders under the source while the move runs,
since Windows refuses to move a folder while a folder inside it is watched, and watches them again if the move fails.
A renamed folder is watched again from its first event.

## Mods and their descriptors

A `Mod` is the config's record (`app.Mod`) with its `Descriptor`, its thumbnail, and whether it is broken. A
`Descriptor` is the mod's own description as its game writes it: CK3's `.mod` file, or Vic3's and EU5's
`.metadata/metadata.json`, as the game's `ModDescriptor` row says. `ReadDescriptor` reads either form; metadata.json
also gives each `Dependency` its title, its relationship's `display_name`, beside its id.
`WriteDescriptor` writes it back and keeps the fields a `Descriptor` does not model. A mod's thumbnail is a thumbnail
picture where the game keeps one, else the picture the descriptor names.

`CreateMod` makes a new mod's skeleton: its folder, the folders the game expects, a descriptor that supports the game's
release, a launcher file beside the folder for a game that lists mods by one, and a localization file that holds only
its header. `IsOlderRelease` compares the release a mod supports with the game's, by the major and minor numbers alone.

## Launch

`Launch` starts the game with its debug flag, as `game.ResolveProgram` gives it. When the game exits, `game:exited` says
whether it wrote dumps fresher than the schema's.

## Events

| event | data | sent when |
|---|---|---|
| `workspace:opened` | the workspace's id | a workspace is open with its index built |
| `workspace:failed` | the error's text | a workspace could not be opened |
| `workspace:closed` | nothing | the open workspace closed |
| `install:scanning` | a `Scanning` | an install's scan reports a stage |
| `install:scanned` | the install's id | an install's scan finished |
| `fs:changed` | a `FileChange` | a file of the open workspace changed on disk, and the index has it |
| `game:exited` | whether the dumps are fresher | the launched game exited |

## Terms

- **Session**: the open workspace as every service reads it.
- **Buffer**: the tree of the editor's text for one open file.
- **Place**: where one file sits in the workspace.
- **Mod**: one mod of the open workspace: the recorded mod with its descriptor.
- **Descriptor**: a mod's own description, as its game writes it.
- **Broken**: what a mod is when its folder is missing.
- **Watcher**: what watches the mods' folders and reports a burst of changes together.
- **State**: the open workspace as the frontend sees it.
- **FileText**: a file's text in its encoding, with its place and the editor's draft.
- **Child**: one entry in a folder listing.
- **FileChange**: one file that changed or was deleted on disk.
- **Scanning**: one stage of an install's scan, with the percent of its files done.
