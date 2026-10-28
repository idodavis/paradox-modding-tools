# health

The package is the Health page's service. It builds the report of how the open workspace's mods touch each other and
what is wrong in each, it lists the game's error log, and it keeps the pairs of mods whose conflicts the workspace
ignores.

It judges nothing of its own. What the game keeps is `index.FindKept` and `index.FindLoaded`, a file's problems are
`lsp.DiagnoseFile`, and every finding's words, question, level and topic are the `lsp.Codes` row of its code. This
package finds which names and files compete, which mod uses which, how far each language covers a mod, and what the
game's error log says, each as findings that point at their sites.

| file | holds |
|---|---|
| `health.go` | the package comment; `EventErrorLogChanged`; `Service` with `ServiceStartup` and `ServiceShutdown`; `FindingSite`, `Finding`, `Change`, `Report`, `GameLog`, `Overview`, `Compared`, `Snippet`; `BuildReport` and the findings of overrides, unused copies, descriptors, unmounted folders, localization and the editor's checks; `ListCodes`, `ListOverview`, `CompareStatements`, `ListSnippets`, `ListReaders`, `WriteScaffold`, `ListScaffolds` |
| `contests.go` | the contests of names and of files, and the undecided ones; their winner, beaten mods, level and suggested order; what a previewed order changes |
| `ignored.go` | `AddIgnoredPair`, `RemoveIgnoredPair`; the ignored pairs and their findings |
| `declared.go` | which mod declares which as a dependency, and the use of a mod that is not declared |
| `errorlog.go` | `ListErrorLog`: the rows of the game's error log, folded and told apart; the log's watch |

## Questions, levels and topics

Every finding answers one of three questions: `compatibility` (how the mods touch the same things), `health` (each
mod's own problems) or `log` (what the game's last run logged). A finding has a level, `serious`, `warning` or `info`,
and the game log has none. Its code's row in `lsp.Codes` gives the question, the level and the topic. Three rules
decide per finding:

- **A contest's level.** A `contest` is a name two or more mods define, and a `file-contest` a file two or more mods
  ship in a folder the game reads. The kept copy's mod is the winner. The beaten mods are the other mods among the
  sites that have no kept copy, never the game; a mod whose copy is kept because it merges into the winner's is not
  beaten. A name whose every mod's copy is kept is no contest and has no finding, and neither is one whose kept copy
  is the game's own: each mod whose copies the game does not use then has an `unused-copy`. The contest is `info`
  when the winner declares every beaten mod as a dependency, or the workspace ignores the pair; otherwise it is
  `serious`. A contest is never a warning. The facts `winner` and `beaten` hold the mods' names, several written "A
  and B" or "A, B and C". A declared dependency forgiving a contest is Health's judgment, not a game rule.
- **An undecided contest.** Where `index.FindWinner` gives the reason `unsettled`, no source says which copy the game
  keeps, so the copies of the mods and the game are one `undecided-contest` finding, with no site kept and each site's
  reason `unsettled`. So are two copies of a block that merged copies cannot both write (`game.Precedence.Contested`,
  an on_action's trigger and effect) in two files. The fact `mods` names the mods and the game, and `why` is the
  sentence that says what leaves it open: a shared override priority, a mode keyword in a folder no
  source says takes it, a mode that adds in a folder that takes REPLACE only, a replace_path, or the block. It is
  `info` when every two of its mods are a declared dependency or an ignored pair, and `serious` otherwise; a pair
  with the game is never forgiven.
- **The question of a shared check.** `duplicate-definition` and `conflicting-priority` are `health` findings of one
  mod's file. When the loaded definitions of the name lie in two or more mods they are `compatibility` findings, and
  their `Mods` are those mods.
- **A shared localization key.** A key that two or more mods define in one language is one `shared-message` finding,
  with no winner, since which copy the game shows is not settled; a copy in a replace folder wins. It is `info` when
  every two of its mods are a declared dependency or an ignored pair, and `warning` otherwise. A key one mod defines
  twice is that mod's `duplicate-message`, and so is one the game defines too outside a replace folder, where the game
  logs that as an error (CK3).

A site's `Kept` and `Reason` are `index.ResolveSite`'s: the winner's reason (`last`, `first`, `playset`, `path`,
`priority`, `mode`, `merge` or `replace`), `injected` or `combined` for a copy merged into the winner, written with an
entry mode or plain. A file the game reads in place of others at its path is `path`.

`Mods` lists the ids of the mods a finding concerns in load order: the origin of a health finding, each mod of a
compatibility one. The game is never among them. `Pairs` lists each two of them that the finding sets against each
other, in load order, which the page offers to ignore: a contest's winner with each beaten mod, and every two mods of
an undecided contest or a shared key. `Count` is 1, except for a log row that repeats.

The other findings are plain facts. An **override** is a name a mod defines that the game defines too, both in files
the game reads, where the game keeps the mod's copy, or a file a mod ships over the game's own. Where the game keeps
its own copy of the name, each mod none of whose copies it uses has an `unused-copy` (warning), beside any other mod's
override; its fact `mod` names that one mod, and `reason` says why in words (`describeLoss`): in a database that takes
entry modes, a plain copy after the first is never created and reads "its file is read before the mod's"; any other
takes the words of the winner's reason. A name whose kept copy no source settles is an undecided contest instead. A
`missing-message` is a definition whose title has no message in the base language, and `coverage` says how many of a
mod's base-language messages another language has. An `unmounted-folder` is a top folder of a mod outside the stage
folders of a game that keeps its files under stages: the game never reads its files, and the finding counts them and
points at the first. The problems in a mod's files are the editor's: each file is
judged by `lsp.DiagnoseFile`, in parallel on `files.Workers` goroutines.

## Dependencies

A mod's declared dependencies are the ones its descriptor names, matched to a mod of the workspace by the mod's
descriptor id or its name. A dependency's own dependencies count, all the way down. A dependency the workspace lacks is
a `missing-dependency` finding, which names it by the title its relationship gives (`display_name`), else by its id;
the fact `mod` names the mod that declares it.

**Used without being declared.** For each mod, a use of a name that nothing in the game, in the mod itself or in its
declared dependencies makes, and that another mod does. A use is a reference, or a name written with `INJECT:` or
`REPLACE:` (not `TRY_`, not `_OR_CREATE`), which needs a definition to act on; only a use in a file the game reads
counts. A name is made by a plain definition, or one written with a mode that creates, and only loaded ones count. Each
pair of a using mod and a used mod, the used mod being the one whose definition the game keeps, is one
`undeclared-dependency` finding. It counts the distinct names and points at the first use of each. A name that the
using file defines for itself is not a use.

## The load order

`BuildReport` builds the report for one load order: the workspace's own, or another the page previews. A previewed
order is scanned by `workspace.Session.ScanOrder` and saved nowhere. The definition the game keeps of each name is found
once and shared by the checks, which then run side by side.

- **A suggested order.** The load order decides only a file two mods ship at one path, and a name whose winner's
  reason is `playset` (EU5's GUI types and templates). A serious contest of either kind with one beaten mod that
  declares the winner as a dependency carries the order in `Finding.Order` that fixes it: the reported order with the
  beaten mod moved just after the winner. A name decided by file name, priority or entry mode, and an undecided one,
  has none, since no order changes it.
- **Changes.** For a previewed order, `Report.Changes` lists every name and every shipped path whose kept copy lies in
  another mod than in the workspace's own order, with both mods' ids and the game's `vanilla` where it is the game's.
  A database that merges its definitions has none, nor has a name whose kept copy is unsettled in either order, and a
  path counts only in a folder the game reads. The list is sorted by database, then by name. The workspace's own order
  has an empty list.
- **Ignored pairs.** `AddIgnoredPair` and `RemoveIgnoredPair` change the open workspace's record (`IgnoredPairs`, each
  pair sorted). They return an error if either mod is not in the workspace, the two are one mod, or the pair is not
  there to remove. The report reads the pairs at each call. Each pair whose two mods are in the workspace is an
  `ignored-pair` finding, with the two mods in the report's load order, that counts the findings whose `Pairs` hold
  the two.

## The error log

`ListErrorLog` reads the game's error log, in the game's user data folder where the game's `ErrorLog` row says. A
message that is repeated with the same text at the same file and line is one row: `Count` is the repeats and `time` is
the last one's. `WrittenAt` is the log file's modification time. A row names its mod in `Origin`, and `why` and `named`
say how:

- `file`: the row names a line of a file that the game loads from the mod; `named` is the file's path in the mod. The
  game's `ErrorLog.Locations` are the forms its log names a line in, each with a `path` and a `line` group: `file: P
  line: N` (CK3), `in file: "P" near line: N` (all three), `P:N` after other words and `line N and column C in P`
  (EU5). The first form that names a line of a mod's file gives the row's place, on the message's line or one after.
- `name`: an identifier in the message, not a plain word, is the name of a definition in some database, and all of that
  definition's loaded copies lie in one mod and none in the game; `named` is the name.

Any other row is the game's own and has no origin.

While a workspace is open, the service watches the log, from the nearest folder above it that exists while its own
folder does not, and emits `errorlog:changed` once each burst of writes to it ends. The watch stops when the workspace
closes, and at shutdown.

## Reading a finding

`CompareStatements` returns the texts of two sites' statements, so a kept definition and one it replaces show side by
side. `ListSnippets` returns each site's lines as a `Snippet`, its text and the line it begins at, an open file's from
the editor's text: a site of one line with two lines around it, a longer one from its own first line. `ListReaders`
lists every place in script and localization that reads a localization key. `ListOverview` counts what each mod holds.

## The translation scaffold

`WriteScaffold` writes the localization keys a mod lacks in a language: each of its base-language messages whose key
has no message of the mod in that language, once, with the base text as the placeholder. These are the keys
`coverage` counts as missing (`listUntranslated`). A key goes at the end of the language's file that matches the
base-language file it is in (`loc.ToLanguageRel`), in that file's line ending and encoding (`loc.EncodeMessages`); a
matching file that is missing or empty is written whole by `loc.Encode`. `ListScaffolds` gives those messages by the
file they go in, which PMT's tools for coding agents preview.

## Events

| event | data | sent when |
|---|---|---|
| `errorlog:changed` | nothing | the game's error log grew while a workspace is open |

## Terms

- **Question**: one of the three things Health answers: compatibility, health and the game log.
- **Level**: how much a finding matters: serious, warning or info.
- **Topic**: what a check is about: syntax, references, values, localization, definitions or descriptor.
- **Finding**: one row of the Health page: a problem or a fact, with the places it points at.
- **FindingSite**: one place a finding points at, with whether the game keeps it and why.
- **Report**: the findings for one load order, and what that order changes against the workspace's own.
- **Change**: one name or path whose kept copy moves to another mod in a previewed order.
- **GameLog**: the rows of the game's last error log, with its path and when it was written.
- **Overview**: what one mod holds, counted.
- **Compared**: the texts of two statements, side by side.
- **Snippet**: the code around one finding site, with the line it begins at.
- **Contest**: two or more mods defining one name or shipping one file, of which the game keeps one copy.
- **Undecided contest**: copies of one name in two or more of the mods and the game, of which no source says which
  the game keeps.
- **Unused copy**: a mod's copy of a name that the game never uses, because it keeps its own.
