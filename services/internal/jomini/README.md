# jomini

Parses Paradox script text into a concrete syntax tree and reads the shapes of its words. It knows no game and no file,
and every offset is a UTF-8 byte index (UTF-16 exists only in the frontend's `editor/columns.ts`).

| file | holds |
|---|---|
| `cst.go` | the tree (`Root`, `Statement`, `Value`, `Scalar`, `Block`, `Assignment`), the line index, and every reading of a parse (`Result`: `Flat`, `Attach`, `Walk`, `BlockOf`, `Fields`) |
| `lexer.go` | the tokenizer: one pass over a file's bytes |
| `parse.go` | the error-tolerant recursive-descent parser: it never panics and returns a tree with the structural errors it recovered from |
| `names.go` | one written word read the same in every Jomini game: name shapes, numbers, dates, `prefix:name`, `@constant`, `$NAME$` |
| `datacall.go` | a data call, `[Root.GetCountry.GetName]`, into its steps and arguments |

## Decisions

- **Error-tolerant.** An unclosed brace or a stray close is an error in the `Result`, never a failure; the tree is
  always usable, so the editor works on a file that is half typed.
- **Comments are kept** and attached to the statement they lead or trail (`Result.Attach`), so an edit can move a
  statement with its notes.
- **One grammar.** `.gui` is the same syntax as `.txt`; there is no second parser.
- **A leaf.** It imports nothing of ours. A file's encoding is `inventory`'s (`text.go`), not read here.
