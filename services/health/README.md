# health

The package is the Health page's service. It builds the report of what is wrong in the open workspace's mods, and it
lists the game's error log.

It judges nothing of its own. What the game keeps is `index.FindKept` and `index.FindLoaded`, a file's problems are
`lsp.DiagnoseFile`, and every finding's words are the `lsp.Codes` row of its code. This package adds the sections:
which names and files compete, which mods depend on which, how far each language covers a mod, and the game's own error
log, each as findings that point at their sites.

| file | holds |
|---|---|
| `health.go` | the package comment; `EventErrorLogChanged`; `Service` with `ServiceStartup` and `ServiceShutdown`; `Section`, `Tally`, `Sections`, `FindingSite`, `Finding`, `Report`, `Overview`, `Compared`; `BuildReport` and one function per section; `ListSections`, `ListCodes`, `ListOverview`, `CompareStatements`, `ListSources`, `ListReaders`, `ListErrorLog`, `WriteScaffold`; the error log's watch |

## The report

`BuildReport` builds the report for one load order: the workspace's own, or another the page previews. A previewed
order is scanned by `workspace.Session.ScanOrder` and saved nowhere. The definition the game keeps of each name is
found once and shared by the sections, which then run side by side. A `Report` holds the `Sections` with their counts,
and their tallies by kind where the section has them, the `Finding`s, and the total.

A `Finding` is one row of the page: a problem, or a fact about the mods, with the `FindingSite`s it points at, the kept
ones first. A site carries its place, its range, and whether the game keeps it and why. A finding's code is an
`lsp.Codes` id, and its section is the code's.

## The sections

`Sections` lists every section in the page's order, each a row with its id, its group, its label and its note. The
groups are the page's three bands: `load order` (contests, overrides, dependencies), `files` (dangling, localization,
script) and `beyond` (the error log and the overview).

- **Contests.** A name that two or more mods define, with the definition the game keeps and the rule that keeps it:
  LIOS, FIOS, priority or mode. A path that two or more mods ship in a folder the game reads is a contest of files. A
  database whose definitions merge has no contests.
- **Overrides.** A name a mod defines that the game defines too, and a file a mod ships that replaces the game's.
- **Dependencies and versions.** One mod depends on another where the game keeps its definition of a name over the
  other's and the two are written differently; comments, spacing and quotes do not count. Each such pair gives a
  `depends` finding and an `affects` finding. A dependency a descriptor names that the workspace lacks, and a mod made
  for an older release than the game's (`workspace.IsOlderRelease`), are findings here too.
- **Dangling references.** A name that script writes and nothing defines, from the mods' diagnostics.
- **Localization.** A definition whose title has no message in the base language, each language's coverage of a
  mod's base-language messages, and the localization problems the diagnostics find.
- **Script.** Every problem the editor finds in a mod's file, one row each. The files are diagnosed in parallel on
  `files.Workers` goroutines.
- **Game errors.** The game's error log as findings. A line that names a line of a mod's file points at it.
- **Mod overview.** What each mod defines, by database, with its files and messages (`ListOverview`).

## The error log

`ListErrorLog` reads the game's error log, in the game's user data folder where the game's `ErrorLog` row says. While
a workspace is open, the service watches the log, from the nearest folder above it that exists while its own folder
does not, and emits `errorlog:changed` once each burst of writes to it ends. The watch stops when the workspace closes,
and at shutdown.

## Reading a finding

`CompareStatements` returns the texts of two sites' statements, so a kept definition and one it replaces show side by
side. `ListSources` returns the first lines of each site, an open file's from the editor's text. `ListReaders` lists
every place in script and localization that reads a localization key.

## The translation scaffold

`WriteScaffold` writes the localization files a mod lacks in a language. Each holds the mod's base-language messages
whose file does not exist in that language yet, with the base text as the placeholder, written by `loc.Encode`.

## Events

| event | data | sent when |
|---|---|---|
| `errorlog:changed` | nothing | the game's error log grew while a workspace is open |

## Terms

- **Section**: one card of the Health page, in its group, with how many findings it holds.
- **Tally**: how many of one section's findings are of one kind.
- **Finding**: one row of the Health page: a problem or a fact, with the places it points at.
- **FindingSite**: one place a finding points at, with whether the game keeps it and why.
- **Report**: the sections and findings for one load order.
- **Overview**: what one mod holds, counted.
- **Compared**: the texts of two statements, side by side.
- **LIOS** and **FIOS**: last in, only served and first in, only served: the game keeps the last or the first
  definition loaded.
