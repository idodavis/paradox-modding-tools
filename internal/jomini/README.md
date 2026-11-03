# jomini

The package parses Paradox script, GUI and asset files into a flat tree and tells what shape a written key or value
has. It knows nothing about any game.

What a key means is the schema's question, and what a game reads is `game`'s. This package answers only what the text
says: where each statement starts and ends, what its parts are, and whether a word is a number, a date, a parameter or a
data call. It never imports `game`.

| file | holds |
|---|---|
| `tree.go` | `Span` with `IsEmpty` and `Contains`, `Position`, `Error`, `Lines` with `NewLines`, `CountLines`, `ToPosition` and `LineSpan`; `Statement` with `IsBlock`, `IsListed` and `ResolveValueSpan`; `Tree` with `Text`, `Unquoted`, `UnquotedSpan`, `ParseBracketed`, `Children`, `FindStatement`, `FindPrevious`, `LookupStatement`, `FindChild` and `FindChildValue` |
| `parse.go` | the lexer and the parser: `Parse`, `NewTree`, `CountBrackets` |
| `shapes.go` | `NumberKey`; the predicates on a written word (`IsIdentifier`, `IsNumber`, `IsEventID`, `IsParameter`, `IsConstant`, `IsNumberKey`); `Date` with `IsValid`, and `ParseDate`; `SplitPrefixed`, `SplitChain`, `MatchKeyPath`, `ColorParts`; the constants and expressions a file writes (`Tree.ResolveNumber`, `ListConstants`) |
| `colors.go` | `Color`, `Colors`, `TagHSV360`, `RGBA` with `ToCSS`, `ColorForm`; `ParseColor`, `ToColor`, `EncodeColor` |
| `call.go` | the data calls: `Call`, `Step`, `Argument`; `ParseBracketed`, `ParseCall`, `SubstituteCall` |
| `edit.go` | `Edit`, `ApplyEdits`, `DiffEdits`, `LineEnding`, `Tree.Indentation`, `Tree.IndentLines`, `Tree.BuildInsertion` |

## The tree

A `Tree` is the parse of one text. Its `Statements` are one flat slice in document order, and a block's children sit
right after it. Statement 0 is the file itself, and the top-level statements are its children. A `Statement` knows its
`Parent` and `Last`, one past its last descendant, so its children are the statements between it and `Last`.
`Children` yields them, skipping each child's own descendants.

Every part of a statement is a `Span` into the source, never a copied string: its `Key`, `Op`, `Tag`, `Value` and
`Block`, and its whole `Span`. A scalar's `Value` keeps its quotes. A block's `Block` runs from its opening brace to its
closing one. A bare value in a list has a `Value` and no key, which `IsListed` reports.

The tree also keeps the comments as spans, the parse errors, and the line index. `Lines` turns a byte offset into a
`Position` and back, and gives the span of a line without its line ending. A line ends at `\n`, `\r\n` or a lone `\r`.
Every offset is a UTF-8 byte offset, and a `Position` counts its column in bytes too. Monaco's lines and UTF-16 columns
are `lsp`'s, which converts at its edge.

`FindStatement` returns the innermost statement that covers an offset. `LookupStatement` returns the one that starts at
an offset, as an edit names a block by where it starts. `FindChild` and `FindChildValue` read a child by its key, and
where several children have the key, the last one wins.

## The parse

`NewTree` gives the tree of a text before any script is read from it: the text, its lines and the file's own
statement, which holds nothing, as a localization file's tree is. `Parse` never fails. A malformed text gives the
statements it could parse, and each problem is an `Error` on the tree with a code: `unclosed-block`,
`unexpected-brace`, `unclosed-string` or `missing-value`. A block that is never closed runs to the end of the text.
A closing brace at the file's level closes nothing and is reported.

The lexer splits the text into scalars, operators, braces and comments. A quoted string is one scalar, a backslash
escaping the character after it, and so is a `[ … ]` inside a word, which keeps everything to its `]`. A `#` comment
runs to the end of its line. The operators are `=`, `==`, `!=`, `<`, `>`, `<=`, `>=` and `?=`. A tagged block such as
`rgb { … }` is one value with a `Tag`, and so is `list "x"`.

## The shapes of a word

The predicates in `shapes.go` tell what shape a written key or value has, one predicate each. `IsParameter` finds a
scripted parameter, `$NAME$`, anywhere in a word. `IsConstant` is a script constant, `@name`, or an expression, `@[…]`.
`SplitPrefixed` splits `scope:x`, and `SplitChain` splits a dotted chain of links, keeping a dot inside brackets or
quotes. `ColorParts` reads a block of plain numbers, with its tag. `Tree.ResolveNumber` works out the number a value
stands for: a number, a constant the file declared earlier, or an expression of numbers, constants, the four operators
and brackets. `IsNumberKey` asks whether a key is written as a number or a date, the keys `NumberKey` (`<number>`)
stands for. `MatchKeyPath` asks whether the keys that lead to a statement fit a key path, each segment a glob,
`<number>` or `**` for any depth.

## Colors

`Colors` lists the ways script writes a color, each a `Color`: an `rgb` block, a block with no tag, `hsv`, `hsv360`
(`TagHSV360`) and `hex`. A part of rgb, or of a block with no tag, is out of 255, or out of 1 when red, green and blue
are each at most 1, and a fourth part is the alpha. `ParseColor` turns a tag and its numbers into an `RGBA`, and
`ToColor` reads a statement's color block that way. `EncodeColor` writes a color back the way an existing block wrote
its own: in the same scale, with an alpha only where that block had one. A `ColorForm` is how a color is written that
no block writes yet: a tag and an example's numbers, which give the scale. `RGBA.ToCSS` gives the color as CSS.

## Data calls

A `Call` is one data call as a GUI file or a localization text writes it between brackets, such as
`Root.GetCharacter.GetName`. It is a list of `Step`s, each with its `Arguments` and any cast written before it, and the
`Suffix` a localization call writes after `|`. An `Argument` is a quoted text, a number or a nested call. Every span
is in the file's own offsets, so the parser takes the offset where the text starts. `SubstituteCall` fills in a macro's
parameters with the arguments written for them. Which function a step calls, and what it returns, is the schema's.

## Edits

An `Edit` replaces a span with a text, so an empty span inserts and an empty text deletes. `ApplyEdits` applies a list
of them. `DiffEdits` gives the smallest list that turns one text into another, through `go-diff`, and replaces the whole
text where either text is not valid UTF-8. `Tree.BuildInsertion` adds lines as the last statements of a block, indented
the way the file indents, which `Tree.Indentation` reads from the file itself. A file with no indentation of its own
indents with a tab.

## Terms

- **Tree**: one parse of one text.
- **Statement**: one statement: `key op value`, a bare value inside a list, or a block with or without a tag.
- **Span**: a half-open range of bytes in a text.
- **Lines**: the line index of a text.
- **Position**: a line and a column, both from zero, the column in UTF-8 bytes.
- **Error**: one problem a parse found, with its code and its span.
- **Edit**: a span and the text that replaces it.
- **Date**: a date as the game writes one, `year.month.day`.
- **Color**: one way script writes a color, such as an rgb block or a hex number.
- **RGBA**: a color as red, green, blue and alpha channels, each from 0 to 1.
- **ColorForm**: how a color is written that no block writes yet.
- **Call**: one data call, its steps and its suffix.
- **Step**: one step of a call, with its arguments and its cast.
- **Argument**: one argument of a step: a quoted text, a number or a nested call.
- **Cast**: the type a step is cast to, written in parentheses before it, such as `(int_bool)`.
