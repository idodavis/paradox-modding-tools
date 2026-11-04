# search

The package is the IDE's search service: text search over the open workspace's files, replace, and the file search
behind Ctrl P.

It lists the files from the mods' index (`index.Index`), reads each one as the editor holds it where it is open
(`workspace.Session.LookupBuffer`) and from disk otherwise, and matches the include and exclude globs as a
`files.Ignore`. Each hit's range is `lsp`'s.

| file | holds |
|---|---|
| `search.go` | the service, `Query` and `Hits`; the text search, the replace and the file search; the globs and the matches |

## Text search

A `Query` is one text search: the text, as a plain text or a regular expression, whether case and whole words count,
the include and exclude globs, and the origins searched, the mods' when it names none. `SearchText` searches the text
files the query names in parallel on `files.Workers` goroutines, and sends each file's hits as a `search:hits` event.
A match never spans two lines. Each hit is an `lsp.Site`: the file's place, the range of the match and the text of its
line. A search stops at `maxHits` and says so.

The service runs one search at a time. Starting a new one, `StopSearch` or `ServiceShutdown` stops the last.

`CollectText`, which PMT's tools for coding agents call, runs the same search but returns the hits, up to its
caller's limit, and stops when its caller's channel closes.

## Replace

`ReplaceText` replaces every match of a query in the mods' files and returns how many it replaced. Each file keeps its
encoding. A file open in the editor is skipped, since its text belongs to the editor's model, and the error says how
many were.

## File search

`SearchFiles` returns the files whose path holds the query's characters in order, the mods' files first, up to a limit,
each as its `workspace.Place`.

## Events

| event | data | sent when |
|---|---|---|
| `search:hits` | a `Hits` | a text search found one file's hits |

## Terms

- **Query**: one text search: what to find and where.
- **Hits**: the matches in one file, each with its range and its line's text.
