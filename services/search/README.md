# search

The package is the IDE's search service: text search over the open workspace's files, replace, and the file search
behind Ctrl P.

It lists the files from the mods' index (`index.Index`), reads each one as the editor holds it where it is open
(`workspace.Session.LookupBuffer`) and from disk otherwise, and matches the include and exclude globs as a
`files.Ignore`. Each hit's range is `lsp`'s.

| file | holds |
|---|---|
| `search.go` | the package comment; `EventHits`, `maxHits`; `Service` with `ServiceShutdown`; `Query`, `Hit`, `Hits`; `SearchText`, `CollectText`, `StopSearch`, `ReplaceText`, `SearchFiles`; the run, the quick test before a file is read line by line, the globs and the matches |

## Text search

A `Query` is one text search: the text, as a plain text or a regular expression, whether case and whole words count,
the include and exclude globs, and the origins searched, the mods' when it names none. `SearchText` searches the text
files the query names in parallel on `files.Workers` goroutines, and sends each file's hits as a `search:hits` event
as it finds them. A file is first tested whole for a plain query's text before it is read line by line. A match never
spans two lines. A search stops at `maxHits` and says so.

The service runs one search at a time. Starting a new one stops the last, and `StopSearch` stops it. `ServiceShutdown`
stops a running search.

The run gives each file's hits to a receiver, up to a limit. `SearchText` sends them as events, up to `maxHits`;
`CollectText`, which PMT's tools for coding agents call, collects and returns them, up to its caller's limit, and
stops when its caller's channel closes.

## Replace

`ReplaceText` replaces every match of a query in the mods' files and returns how many it replaced. Each file keeps its
encoding, and a file with no match is not written. A file open in the editor is skipped, and the error says how many
were, since its text belongs to the editor's model.

## File search

`SearchFiles` returns the files whose path holds the query's characters in order, the mods' files first, up to a limit,
each as its `workspace.Place`.

## Events

| event | data | sent when |
|---|---|---|
| `search:hits` | a `Hits` | a text search found one file's hits |

## Terms

- **Query**: one text search: what to find and where.
- **Hit**: one match of a text search, with its line's text.
- **Hits**: the matches in one file.
