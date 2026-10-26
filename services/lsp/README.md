# lsp

The package is the editor's language service. It answers what the editor asks about a file: hover cards, completions,
definitions, diagnostics, tokens, folds and the rest, with every range in the editor's own form.

There is no protocol, no stream and no document table. Each of Monaco's providers asks one method of `Service`, typed
end to end by the bindings, and the session's buffers are the open files. Every answer is a visitor on `schema.Walk` or
`schema.WalkTo` over the open session, computed when asked; the package keeps nothing between calls. It reads the
index's lookups and `index.FindWinner` for what the game keeps, and adds the editor's view of them: the cards, the
lists and the problems in words.

| file | holds |
|---|---|
| `lsp.go` | the package comment, `Service`; Monaco's ranges: `Range`, `Location`, `Edit`, and the one conversion from byte spans (`ToRange`, `ToLineRange`, `ToOffset`, `ToEdits`); `SetText`, `CloseFile` |
| `hover.go` | `Card`, `CardScope`, `CardSite`, `CardDate`; `Hover` |
| `complete.go` | `Completion`; `Complete` |
| `navigate.go` | `Symbol`, `WorkspaceSymbol`; `Definition`, `References`, `PrepareRename`, `Rename`, `ListSymbols`, `ListFileSymbols`, `FindSymbols` |
| `diagnostics.go` | `Code`, `Codes`, `Diagnostic`; `Diagnose`, `DiagnoseFile`, `DescribeReference` |
| `tokens.go` | `Legend`; `Tokens`, `ListLegend` |
| `edits.go` | `Fix`; `Format`, `DescribeIndentation`, `ListFixes` |
| `details.go` | `Fold`, `Hint`, `ColorSite`, `Signature`; `ListFolds`, `ListHints`, `ListColorSites`, `PresentColor`, `Signature` |

## Positions and ranges

A position comes in as Monaco gives it: a line from one and a UTF-16 column from one. An answer's `Range` is Monaco's
`IRange` with Monaco's own field names, so the frontend hands it over as it is. Every other package keeps UTF-8 byte
offsets. The one conversion between the two is `lsp.go`'s: `ToOffset` takes a Monaco position to a byte offset, and
`ToRange` and `ToEdits` take byte spans and the engine's `jomini.Edit`s to Monaco's. No other package converts.

## The open file

`SetText` gives the session the editor's text of an open file, which becomes the file's buffer, and answers with the
file's problems. `CloseFile` drops the buffer. Every other method reads the file's tree through the session, so an open
file is read as the editor holds it.

## The answers

- **Hover.** `Hover` walks to the position and returns a `Card`: what the key or value is, what it takes, what it
  returns or leads to, the scopes script can name there (`CardScope`), a date in the mod's calendar (`CardDate`), a
  color, an image's header, and the sites that define the name (`CardSite`), the one the game keeps first and why. A
  fact learned only from the game's files marks the card inferred.
- **Completion.** `Complete` returns what fits at a key: the language's operators that fit the block, then its
  commands, ranked by how often the game writes each in that field, then its fields. At a value it returns the field's
  choices, yes and no, the names the game loads of its database, or the image files under the folder typed so far.
- **Navigation.** `Definition` returns where a name is defined, the kept definition first. `References` adds where it
  is used, a localization key's translations left out. `Rename` returns the edits of a rename, grouped by the mod file
  they change; `PrepareRename` refuses a name the game defines too, or one no mod defines. `ListSymbols` is a file's
  outline, each definition with its title, and `FindSymbols` searches the workspace's definitions and keys.
- **Tokens.** `Tokens` returns the color category of every key and value, from the walk's `schema.Category`, encoded as
  Monaco's semantic tokens against `ListLegend`.
- **Edits.** `Format` keeps the file's own indentation, else the given one, and gives no edits for a file that does not
  parse or that would change in more than its spacing. Its edits come from `jomini.DiffEdits`. `ListFixes` gives the
  quick fixes: the nearest defined name for one that nothing defines, a missing key with its default, and an entry mode
  out of place removed.
- **Details.** `ListFolds` returns every block that spans lines. `ListHints` reads each date in the mod's calendar,
  where its field expects a date. `ListColorSites` lists the colors a file writes, and `PresentColor` writes a color in
  the form the file already wrote it in; a color not written yet, such as a coat's layer's, takes the form the game's
  own coats write their colors in (`game.Game.CoatColorForm`). `Signature` returns the parameters of the scripted
  definition a call names.

## Diagnostics

A `Diagnostic` is a code, a range and its facts. `Codes` is the one table of every code: its id, the question Health
asks it under (`compatibility`, `health` or `log`), its level (`serious`, `warning` or `info`), its topic (syntax,
references, values, localization, definitions or descriptor), its label and its message, a template that the facts
fill. The game log has no level and no topic, since the game writes none. A sentence about a problem is written once,
there, and the editor's markers and Health's rows both read the level from it. `DiagnoseFile` returns a file's problems
in a session against a given index, and a file of the game has its parse errors only. `Diagnose` is the same against
the open session, with the checks the user turned off (`ide.checks`) left out. `DescribeReference` gives the facts
that name what a problem is about.

A check accuses only on firm ground. A reference is judged dangling only where its database is known and holds no names
made as the script runs, and where the claim that makes it is not inferred from the game's files alone. A name the
game's own files use is never dangling, unless it is defined and every definition lies in a file the game does not
read (`isKnown`). A key is unknown, or a value of the wrong type, only
where a source that can accuse describes the block and the game's own files never write otherwise. The parse errors and
the localization codes come from `jomini` and `loc` under the same table.

A name others share is judged by what the game keeps (`index.FindWinner`). A copy with the same override priority as
another loaded copy is `conflicting-priority`, which the game logs as an error. A plain copy that its own mod's other
copy wins over is `duplicate-definition`, put on the copy the game drops; never in a database whose copies all stay,
nor where no source says which copy the game keeps. A localization key outside a replace folder is
`duplicate-message` against its own mod's other copies, and against the game's only where the game logs that as an
error (`game.Game.LogsRepeatedKey`). EU5's stages count as one folder for both checks. A hover card's sites are kept
or not, and why, as `index.ResolveSite` says: where no source says which copy the game keeps, none is kept.

## Terms

- **Range**: a span as Monaco counts it: lines and UTF-16 columns from one, the end exclusive.
- **Location**: a range in a file, with the file's place and the text of its line.
- **Edit**: one change to a file's text, as a range and the text that replaces it.
- **Card**: a hover card: the facts about a key or a value, for the page to lay out.
- **CardScope**: one scope script can name at a statement.
- **CardSite**: one place a name is defined, with whether the game keeps it and why.
- **CardDate**: a date read in the mod's calendar.
- **Completion**: one candidate in the completion list.
- **Symbol**: one entry of a file's outline.
- **WorkspaceSymbol**: one definition found by the symbol search across the workspace.
- **Code**: one kind of problem, with its question, its level, its topic and its message.
- **Diagnostic**: one problem found in a file: its code, its range and its facts.
- **Legend**: the token types and modifiers the semantic tokens are encoded against.
- **Fix**: one quick fix, with its edits grouped by file.
- **Fold**: one foldable region.
- **Hint**: a text drawn inline beside the code.
- **ColorSite**: one color written in a file.
- **Signature**: the parameter list of a scripted definition at a call.
