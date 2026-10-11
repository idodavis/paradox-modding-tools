# pages

What each of the app's pages asks of an open workspace: the Health report, the event graph with one definition's
detail and the root search, and the searches over the workspace's folders. Each answer is a function of the session
(or the folders) and the page's parameters, and each answer over a session takes its read lock itself, so a service
is one call; payloads carry display data and the frontend templates the text. Nothing here persists but `Findings`, the editor's
findings kept while the session's records are unchanged.

Read `health.go` first.

| file | first line |
|---|---|
| `health.go` | the Health page: the names two origins define, what a mod needs from another, the names nothing defines, the localization a language lacks, and the editor's own findings, every row naming its file by its number in Sources |
| `health_contests.go` | the Contests and Overrides cards: each name two origins define, by a load order, and each file path two origins ship, of which the game loads one; a contest where two mods contend, else one mod's override of another origin's |
| `health_dependencies.go` | what each mod needs from another: the names it writes that another mod's definitions decide, and whether that mod's definition differs from the one it hides |
| `health_localization.go` | each language's gaps: the keys a mod writes in the workspace language and not in a language it also ships |
| `health_findings.go` | the editor's findings as Health rows: the findings over every mod file, kept while the records are unchanged; the names nothing defines, one row per name; and the findings no card lists, one row per place |
| `graph.go` | the event graph page: the definitions around a root over what they fire, hop by hop, with stubs for what the page's numbers hide, cut to the origins the page shows; and the roots to pick from |
| `graph_detail.go` | one definition opened: where it is, each key at the top of its block as the page shows it, its options, what it fires and what fires it, and what its block names |
| `search.go` | Find in Files and Replace all over the workspace's folders, with their include and exclude globs, and the file search: each file greped in a worker per core, its hits sent as it is done |

## The Health report

`Health` makes one `sourceIndex`, so every row names its file by a number in `Sources`, and asks each card's function in
turn: `contests` (names two origins define, in the load order the page previews), `fileOverrides` (paths two origins
ship, of which the game loads one; both count a definition by `isEntry`), `dependencies` (what a mod writes that
another mod's definitions decide), `dangling`, `localization` and `problems`. The editor's findings come in as an
argument, from `Findings.Current`, which takes the read lock itself and so runs before `Health` does; `shownBy` says
which card lists which code, so no finding is shown twice.

## The event graph

`Graph` draws with one `grapher`: `hops` out of the root, a stub for what the page's fan-out hides (`moreStub`) and for
the callers into the root (`callersStub`), every fire between two drawn definitions (`between`), the scripted effects
as nodes where the page shows them (`drawScripted`) and only the page's origins (`keepOrigins`). `EventDetail` opens
one definition by the same `def`; `eventText` reads the parts of its block that the page shows as text.

## The searches

`TextSearch` is a `grep`: a worker per core greps the files the walk hands it, each file's hits sent as it is done,
at most the query's cap. `Replace` is a `replacer`: the files on disk, then the editor's unsaved text of the files it
holds, by the same `replaceLines`.
