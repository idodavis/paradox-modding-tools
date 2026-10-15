# lsp

What the editor asks of one open file, answered from the index and the schema: the card for what is under the
cursor, what may be written there, what is wrong, where a name is defined and used, and how the file is folded,
formatted and annotated. Every answer starts from one walk of the file (`index.Collect`), stopped at the cursor or
run whole. Payloads carry display data; the frontend templates the text. Nothing here persists.

Read `file.go` first.

| file | first line |
|---|---|
| `file.go` | what every request starts from: the open file, the cursor's place in it from one walk that stops there, and the editor's positions and locations (`Locate`, the location of a range of any file, which draw's properties use too) |
| `hover.go` | the card for what is under the cursor: what the scalar stands for, then each fact its written form adds; a data call's step and a loc file's key have cards of their own |
| `signature.go` | a command's arguments, shown while its block is typed |
| `wording.go` | the card's words: what a block holds, what a key takes, what vanilla writes, a requirement, a command's kinds, a scope in a sentence, a database as a heading, a position's keys in the order a card lists them |
| `complete.go` | what may be written at the cursor: at a key, the block's keys, commands, links and names; at a value, what its position declares or vanilla writes, or a database's names; in a data call, its next step; `Resolve` reads an item's place again for the card of what its label names |
| `diagnostics.go` | what is wrong with a file: one method per code; a code accuses only on a claim a declared source states, fresh, that no vanilla evidence contradicts (`schema.MayAccuse`); the install's own files are never judged |
| `definition.go` | navigation: where the word under the cursor is defined, where a name is used, and the definitions a file or the workspace holds |
| `edits.go` | what the editor draws or applies: formatting, folding, quick-fixes, era hints, and the colors a file writes, each a swatch before its block, by the one rule hover shares (`blockColor`: a tagged color block anywhere; an untagged one at a position where vanilla writes nothing but colors, one tagged, and words that name them, a named color's entry or a coat of arms' `color1` (`schema.Vanilla.WritesColors`, the position from the file's walk), and under a key ending as its file type's `ColorSuffix`, a culture's `color` or a GUI file's `fontcolor`, but a key that names one of the install's genes), and a block written again with a color picked, in its own form (`Recolor`, by `game.PartsOf`); nothing here writes to disk |

## From a request to a walk

A request opens the file as the editor sees it (`open`: the editor's text where the file is open there, else the
disk's, parsed once per text, placed in the workspace) and, for a script file, walks it to the cursor
(`openFile.walkTo`). The walk keeps the chain of statements that hold the cursor, outer to inner, each with the context
it is written in and the one its block reads in, and the occurrences it read (`cursor`). `scalarAt` picks the scalar
under the cursor and the reference the walk read in it; `blockAt` and `within` say which block the cursor is in and
which entry. A localization file has no walk: `locRef` reads its line.

## The hover card

`Hover` dispatches by format; `scriptCard` is the list of facts, each one call. What the scalar stands for comes
first, as a `subject` (`subjectAt`): a reference the walk read in it, else its key's position or command, with the
scope it is written in. `describe` fills the card from it by one of three functions, `cardForReference` (a name and
where it is defined), `cardForCommand` (an effect, trigger or link the dumps declare) and `cardForPosition` (a key
only its place knows). Then each fact the written form adds is its own function over the scalar alone: `modifierFact`,
`dateFact`, a constant's value (`jomini.Result.ConstantAt`), `weightFact`, `writtenInFact`, and the color the block writes (`jomini.ColorParts`, then `game.ColorOf`). The words are `wording.go`'s, so no card
function writes prose. A completion item's card (`Resolve`) is the same `describe` over the subject the item was
offered with.

## What a code may accuse

A diagnostic is a method of `judge` named for its code, documented with the code's summary in `Codes`. It speaks
only where `schema.MayAccuse` allows: a declared source states the claim, the source is fresh, and vanilla's evidence
at the position does not contradict it. `wrong-scope` also asks the install's index whether vanilla ever writes the
word in that scope (`WritesInScope`), and a reference that vanilla's own words at the position leave unresolved is not
accused either (`contradicted`). The install's files are never judged, a code is turned off in the settings, and a
`# pmt:ignore code` comment turns it off for a line and the next.
