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
| `health.go` | the service and its payload types; `BuildReport`, the grouping and the findings other than contests; the page's reads and `WriteScaffold` |
| `contests.go` | the findings of one name's copies and of the files at one path; their winner, level, pairs and suggested order; what a previewed order changes |
| `ignored.go` | the ignored pairs and their findings; the mods the compatibility checks judge |
| `errorlog.go` | the rows of the game's error log, folded, told apart and worded; the log's watch |

## Questions, levels and topics

Every finding answers one of three questions: `compatibility` (how the mods touch the same things), `health` (each
mod's own problems) or `log` (what the game's last run logged). A finding has a level by how much the game loses,
`critical`, `error`, `warning` or `info`, and the game log has none. Its code's row in `lsp.Codes` gives the
question, the level and the topic. Three rules decide per finding:

- **A contest's level.** A `contest` is a name two or more mods define, and a `file-contest` a file two or more mods
  ship in a folder the game reads. The kept copy's mod is the winner. The beaten mods are the other mods among the
  sites that have no kept copy, never the game; a mod whose copy is kept because it merges into the winner's is not
  beaten. A name whose every mod's copy is kept is no contest and has no finding, and neither is one whose kept copy
  is the game's own: each mod whose copies the game does not use then has an `unused-copy`. The contest is `info`
  when the winner declares every beaten mod as a dependency, or the workspace ignores the pair; otherwise it is
  `critical`. A contest is never a warning. A declared dependency forgiving a contest is Health's judgment, not a game
  rule.
- **An undecided contest.** Where `index.FindWinner` gives the reason `unsettled`, no source says which copy the game
  keeps, so the copies of the mods and the game are one `undecided-contest` finding, with no site kept and each site's
  reason `unsettled`. So are two copies of a block that merged copies cannot both write (`game.Precedence.Contested`,
  an on_action's trigger and effect) in two files. The fact `why` says what leaves it open. It is `info` when every
  two of its mods are a declared dependency or an ignored pair, and `critical` otherwise; a pair with the game is
  never forgiven.
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
an undecided contest or a shared key. `Count` is 1, except for a log row that repeats and a finding the report folded
others into (see The report's groups).

The other findings are plain facts. An **override** is a name a mod defines that the game defines too, both in files
the game reads, where the game keeps the mod's copy, or a file a mod ships over the game's own. Where the game keeps
its own copy of the name, each mod none of whose copies it uses has an `unused-copy` (critical), beside any other mod's
override. A name whose kept copy no source settles is an undecided contest instead. A mod's file the game reads over
its own copy at the same path is a `removed-names` instead of an override where the game's copy defines names that no
file the game reads defines and one still uses: `Uses` lists them, the most used first. A `missing-message` is a
definition whose title has no message in the base language, and `coverage` says how many of a mod's base-language
messages another language has. An `unmounted-folder` is a top folder of a mod outside the stage folders of a game that
keeps its files under stages: the game never reads its files, and the finding counts them and points at the first. The
problems in a mod's files are the editor's: each file is judged by `lsp.DiagnoseFile`, in parallel on `files.Workers`
goroutines.

## Dependencies

A mod's declared dependencies are the ones its descriptor names, matched to a mod of the workspace by the mod's
descriptor id or its name. A dependency's own dependencies count, all the way down. A dependency the workspace lacks is
a `missing-dependency` finding, which names it by the title its relationship gives (`display_name`), else by its id;
the fact `mod` names the mod that declares it.

**A mod on its own.** Each mod's own problems are judged with the index narrowed to the mod and its declared
dependencies, so a name only another mod defines is that mod's `unresolved-reference`. Its message is the code's
elsewhere message, which names the mod that defines it, and its `Defined` is where. `Overview.Judged` lists the mods a
mod is judged with.

**Mods left out.** A mod the workspace's record marks `LeftOut` is left out of the compatibility checks: their lists,
the ignored pairs and a preview's changes are judged on the index narrowed to the other mods. Its own problems are
judged as every mod's are.

## The load order

`BuildReport` builds the report for one load order: the workspace's own, or another the page previews. A previewed
order is scanned by `workspace.Session.ScanOrder` and saved nowhere. The definition the game keeps of each name is found
once and shared by the checks, which then run side by side.

- **A suggested order.** The load order decides only a file two mods ship at one path, and a name whose winner's
  reason is `playset` (EU5's GUI types and templates). A critical contest of either kind with one beaten mod that
  declares the winner as a dependency carries the order in `Finding.Order` that fixes it: the reported order with the
  beaten mod moved just after the winner. A name decided by file name, priority or entry mode, and an undecided one,
  has none, since no order changes it.
- **Changes.** For a previewed order, `Report.Changes` lists every name and every shipped path whose kept copy lies in
  another mod than in the workspace's own order, with both mods' ids and the game's `vanilla` where it is the game's.
  A database that merges its definitions has none, nor has a name whose kept copy is unsettled in either order, and a
  path counts only in a folder the game's own files lie in. The workspace's own order has an empty list.
- **Ignored pairs.** `AddIgnoredPair` and `RemoveIgnoredPair` change the open workspace's record (`IgnoredPairs`, each
  pair sorted). Each pair whose two mods are in the workspace is an `ignored-pair` finding, with the two mods in the
  report's load order, that counts the findings whose `Pairs` hold the two.

## The report's groups

`BuildReport` hands the findings over worded, grouped, counted and sorted, so no page derives them again
(`groupReport`).

- **The words** (`describeFinding`). A finding takes its code's `Label` and `Topic`, and its `Message` is the code's
  template filled with its facts as `lsp.Part`s, the code's elsewhere template where it has one and another mod
  defines the name (`lsp.SplitMessage`). Its `Brief` is the line under its name in a list. Its `Detail` is its first
  site's place, such as `a.txt:12`. Its `Why` is the long form of the winner's reason (`index.Reasons`); a file, a
  finding with no winner and a reason with no long form have none.
- **The fold.** Findings with the same code, the same owner (the first of `Mods`, else `Origin`), the same name and
  the same message are one. The first finding stands for them: its `Count` adds theirs and its `Repeats` holds their
  sites. The game log's rows fold the same way by message and place
  (`foldFindings`), and `Count` is then the lines.
- **The groups.** `Compatibility` holds the findings of the `compatibility` question, folded. `Mods` holds one group
  for each mod of the report's load order, with the mod's `health` findings; its coverage findings are apart, as
  `Languages`. A health finding about a mod outside the order is left out.
- **The sort.** A group's `Checks` are its findings' codes at a level, counted with the folded findings in: the worst
  level first (`lsp.Levels`), and within a level the check with the most findings. `Findings` follow the order of
  their checks, and within a check the heaviest first (`weigh`).
- **The tallies.** A `Tally` counts findings by level, every level present, as `Count` says, words each count as a
  phrase (`Phrases`, such as "235 critical problems", by `lsp.Plural`), and names the worst level that has a finding.
  A group, the report (the compatibility and the mods together) and `Own` (the mods together) each have one.
  Languages are in none.

## The error log

`ListErrorLog` reads the game's error log, in the game's user data folder where `game.ErrorLog` says. A
message that is repeated with the same text at the same file and line is one row: `Count` is the repeats and `time` is
the last one's. `WrittenAt` is the log file's modification time. A row names its mod in `Origin`, and `why` and `named`
say how:

- `file`: the row names a line of a file that the game loads from the mod; `named` is the file's path in the mod. The
  forms a log names a line in are the game's `ErrorLocations`. The first that names a line of a mod's file gives the
  row's place, on the message's line or one after.
- `name`: an identifier in the message, not a plain word, is the name of a definition in some database, and all of that
  definition's loaded copies lie in one mod and none in the game; `named` is the name.

Any other row is the game's own and has no origin. The game's debug log (`game.MountLog`) names each folder the last
run read. A row goes to a mod only if that run read it: `Unloaded` lists the workspace's mods it did not read, and
`Copies` the folder of another copy it read in a mod's place, found by the name the copy's descriptor gives. Without a
debug log, every mod counts as read. A row's `Message` keeps the name or file that ties it to its mod apart, and its
`Why` says what ties it.

While a workspace is open, the service watches the log, from the nearest folder above it that exists while its own
folder does not, and emits `errorlog:changed` once each burst of writes to it ends. The watch stops when the workspace
closes, and at shutdown.

## Reading a finding

`CompareStatements` returns the texts of two sites' statements, so a kept definition and one it replaces show side by
side. `ListSnippets` returns the code at each site as a `Snippet`, an open file's from the editor's text. `ListReaders`
lists every place in script and localization that reads a localization key. `ListOverview` counts what each mod
holds. `ListCodes`, `ListLevels` and `ListTopics` give the page `lsp`'s tables.

## The translation scaffold

`WriteScaffold` writes the localization keys a mod lacks in a language: each of its base-language messages whose key
has no message of the mod in that language, once, with the base text as the placeholder. These are the keys
`coverage` counts as missing (`listUntranslated`). A key goes at the end of the language's file that matches the
base-language file it is in (`loc.ToLanguageRel`); a matching file that is missing or empty is first made with only
its header (`workspace.Service.CreateLocalization`).
`ListScaffolds` gives those messages by the file they go in, which PMT's tools for coding agents preview.

## Events

| event | data | sent when |
|---|---|---|
| `errorlog:changed` | nothing | the game's error log grew while a workspace is open |

## Terms

- **Question**: one of the three things Health answers: compatibility, health and the game log.
- **Level**: how much the game loses by a finding: critical, error, warning or info.
- **Topic**: what a check is about: syntax, references, values, localization, definitions or descriptor.
- **Finding**: one row of the Health page: a problem or a fact, with the places it points at (`lsp.Site`).
- **Use**: one name a finding is about, with how often the files the game reads use it and where they first do.
- **Report**: the findings for one load order, grouped and counted, and what that order changes against the
  workspace's own.
- **Group**: the folded and sorted findings of the workspace's compatibility or of one mod, with their tally and checks.
- **Tally**: how many findings there are of each level, and the worst level.
- **Check**: one code at one level in a group, with how many findings it has.
- **Change**: one name or path whose kept copy moves to another mod in a previewed order.
- **GameLog**: the rows of the game's last error log, with its path and when it was written.
- **Overview**: what one mod holds, counted.
- **Compared**: the texts of two statements, side by side.
- **Snippet**: the code around one finding site, with the line it begins at.
- **Contest**: two or more mods defining one name or shipping one file, of which the game keeps one copy.
- **Undecided contest**: copies of one name in two or more of the mods and the game, of which no source says which
  the game keeps.
- **Unused copy**: a mod's copy of a name that the game never uses, because it keeps its own.
