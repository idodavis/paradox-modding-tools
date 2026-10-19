# loc

Reads and writes the localization dialect the games use (not real YAML): a file's entries with their keys, versions and
texts, the language a file is in, and the markup inside a text. It knows no game, and every offset is a UTF-8 byte
index.

| file | holds |
|---|---|
| `parse.go` | the file: `Parse`, a line-by-line scanner that never panics and returns the header's language, the entries and typed errors, with the line index it built (`Result.Lines`); `Language`, the one rule for the language a file is in; `File`, the bytes of a new file; `LooksLikeKey`, the one test of whether a word may be a key |
| `markup.go` | the text: `Segments`, the one reader of a value's markup, and what is written over it or beside it: `Plain` (what a player reads), `Interpolations` (the keys a value reuses) and `Concepts` (the game concepts a data call may name, and whether it marks them) |

## Decisions

- **One line index per text.** `Parse` builds `jomini`'s line index and keeps it on the `Result`; whoever turns an
  offset into a line asks the result, so an offset here and an offset in script are counted the same way.
- **One rule for the language.** `Language(header, rel)` is the header's, else the one the file name (the last part of
  `rel`) ends with (`events_l_english.yml`, `.yml` only: no game reads another extension), else the one the folder
  `rel` names (`localization/english/`), in lower case. A header is "" where a caller has none, and `lsp` asks for the
  name's language alone by passing the bare name, which has no folder. A header that disagrees with its file's name is
  reported by `lsp`, not decided here.
- **One key rule.** A key is a run of letters, digits and `_ . - '` (`keyExtra`, wider than script's `$NAME$` in
  `jomini`): `Parse` reads an entry's key by it, `markupAt` a `$key$`, and `LooksLikeKey` tests a word by it, rejecting
  the keywords (`yes`, `root`, ...) a script value may also be.
- **One reader of the markup.** `Segments` reads `$key$`, `[call]`, `#format` (its last word may carry an argument
  after a colon, `#tooltip:[Character.GetTooltipTag],key text#!`), `@icon!` and `\n`, through `markupAt`; every segment
  keeps its span of the value. `Plain` is written over the segments, and `Concepts` holds the `|E` rule once.
  `Interpolations` is not: a key is a key wherever it is written, inside a call's argument and a format's argument
  too, which the segments hold whole, so it asks `markupAt` at each `$`.
- **A `[` no `]` closes is text,** as the game reads it, and the markup after it is read as ever; it is marked
  `IsOpen`, because completion types inside such a call (`lsp`'s `partialAt` takes the last one).
- **Errors are `jomini`'s type.** The codes `loc` raises (`ErrNoHeader`, `ErrBadEntry`, `ErrTabIndent`,
  `ErrUnterminatedValue`, `ErrContentBeforeHeader`) are prefixed by `lsp` where it reports them.
