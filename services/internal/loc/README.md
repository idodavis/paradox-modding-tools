# loc

Parses the localization dialect the games write (not real YAML): entries with their keys, versions and texts, the
`$key$` interpolations inside a text, and the language of a file. It knows no game.

| file | holds |
|---|---|
| `loc.go` | `Parse`, a line-by-line scanner that never panics and returns entries with typed errors; `Interpolations`; a text's markup read into the stretches that share one formatting (`Segments`; a format's last word may carry an argument after a colon, `#tooltip:[Character.GetTooltipTag],key text#!`); the game concepts a data call may name and whether it marks them (`Concepts`, which the index's references and draw's texts read); the language of a file from its header, name or path (`LanguageFromFilename`, `LanguageFromRel`); `Stub`, a new file's contents |

## Decisions

- **Lines come from `jomini`'s line index,** so an offset here and an offset in script are counted the same way.
- **Interpolations are loc's own reader.** A localization reference is `$key|filter$` with `.`, `-` and `'` allowed in
  the key, which is wider than script's `$NAME$` (`jomini`), so the two readers stay apart.
- **A header names the language** (`l_english:`). A file whose name disagrees with its header is reported by `lsp`, not
  decided here.
