# jomini

Parses Paradox script text into a concrete syntax tree and reads the shapes of its words. It knows no game and no file,
and every offset is a UTF-8 byte index (UTF-16 exists only in the frontend's `editor/columns.ts`).

| file | holds |
|---|---|
| `cst.go` | the tree (`Root`, `Statement`, `Value`, `Scalar`, `Block`, `TaggedBlock`, `Assignment`), the line index (`NewLineIndex`, `LineText`), a parse's `Result` and every reading of it (`Lines`, `Flat`, `LineIndents`, `TokenAt`, `AssignmentAt`, `Attach`), and the lookups over the tree (`Walk`, `BlockOf`, `Fields`, `KeyValue`, `ColorParts`) |
| `lexer.go` | the tokenizer: one pass over a file's bytes |
| `parse.go` | the error-tolerant recursive-descent parser: it never panics and returns a tree with the structural errors it recovered from |
| `shapes.go` | one function for each shape a written word has: a name (`IsIdentifier`), an event id, a number or a date (`Literal`, `ParseDate`), `prefix:name` (`SplitPrefixed`), a `$PARAMETER$` (`Parameters`), an `@constant` (`Constant`; `ConstantAt`, the nearest declaration around a place; `Evaluated`, a word with its constants and `@[ … ]` arithmetic put in) |
| `datacall.go` | a data call, `[Root.GetCountry.GetName]`, into its steps, their arguments and casts (`DataCalls` for a text's calls, `ParseCall` for one call, `Calls` for their spans, `Cast` for a cast literal, `'(int32)3'`) |

## Decisions

- **Error-tolerant.** An unclosed brace or a stray close is an error in the `Result`, never a failure; the tree is
  always usable, so the editor works on a file that is half typed.
- **Comments are kept** and attached to the statement they lead or trail (`Result.Attach`), so the hover card can
  show the note a mod's author wrote above a definition. The docs reader in `schema` attaches its own, by its own rule.
- **One grammar.** `.gui` is the same syntax as `.txt`; there is no second parser.
- **A leaf.** It imports nothing of ours. A file's encoding is `inventory`'s (`text.go`), not read here.
- **One lex, one parse, one line index.** `Parse` keeps the tokens and the line index in its `Result`; `TokenAt` and
  `LineIndents` read them and never tokenize the text again. A line ends at `\n`, `\r\n` and a lone `\r`, and
  `LineIndex.LineText` is how a reader gets a line of the text without its ending.
- **A tag may carry a text, not only a block.** `rgb { 255 0 0 }` and `list "x"` are each one `TaggedBlock`, one value;
  the second has no braces (its `Block`'s `OpenBrace` is -1, and the text is its one statement), and `BlockOf` gives
  nil for it. The text rule applies in value position only, after an operator: a bare word, a quoted text no operator
  follows, and the two are one value (`key = list "x"`); listed in a block, `a "b"` stay two values.
- **Which value a key gives is the caller's rule.** `KeyValue` reads the first assignment of a key that writes a word,
  or the last, with or without case, as the `Match` a caller passes says.
- **A shape is one function.** Every name, in script or in a data call, is made of the letters and digits of any
  script and the underscore (`identifierEnd`); a shape adds the characters it allows (a dot or a hyphen in a name, a
  hyphen in a namespace).
- **A cast literal is a step.** `(int32)3` read by `ParseCall` is one `CallStep`: its `Name` is the value and its
  `Cast` the type; an argument written as one reads as it is, and a format after a `|` is no part of it.
