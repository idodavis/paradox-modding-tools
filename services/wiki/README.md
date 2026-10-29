# wiki

The package reads the community wiki for the open workspace's game. It fetches and caches the pages, finds the ones
that belong to a file, lists the patch notes and builds the lexicon of the game's commands.

The wiki is read for the Wiki pane and the Patch Notes page only; no wiki text reaches a hover card or a verdict. The
game's row names its wiki. The lexicon reads the schema's commands, scopes and data functions, with the places the
game's own files use them, and the workspace places each one.

| file | holds |
|---|---|
| `wiki.go` | the events, `Progress`; `Service` with `ServiceStartup`; `ReadPage`, `ListNotes`, `ReadNote`, `Refresh`; the client of the open workspace's game |
| `client.go` | the package comment; `Page`, `Client`, `NewClient`; `FetchPage`, `LoadPage`; the cache of one sanitized page per file |
| `refresh.go` | `Summary`, `Listing`; `Client.Refresh`, `LoadListing`; the wiki's Modding and Patches categories, and the pages that are missing or stale |
| `sanitize.go` | `Sanitize`: a page's HTML cut to what the pane shows, over `x/net/html` |
| `pages.go` | `Note`; `FindPages`, `SearchPages`; the order of patch versions and a note's release date |
| `lexicon.go` | `LexiconRow`, `Example`; `BuildLexicon` |

## The client and the cache

A `Client` reads one game's wiki through its API, rate limited by `x/time/rate`, with a timeout and a cap on an
answer's size. It caches each page as one sanitized HTML file, and the game's listing of pages as one JSON file, in
the wiki's folder of the app's cache. The service makes the client for the open workspace's game on first use, and a
new one when a workspace of another game opens. `ServiceStartup` refreshes the open game's wiki in the background each
time a workspace opens.

`Refresh` fetches the listing, the titles of the wiki's Modding and Patches categories, and then every page that is
missing or stale. `wiki:progress` reports each page and `wiki:updated` the end, with the error's text if there was
one. A page that cannot be fetched does not stop the others.

## Sanitizing

`Sanitize` strips a page's HTML down to what the article pane shows. Each kept element keeps only the attributes its
row allows; the wiki's chrome, its citations, its pictures and the sections of links to elsewhere are dropped; the
scripting tools go last; and a long table is folded. A link is made absolute against the wiki's address, and a link to
another page of the wiki carries that page's title as `data-page`.

## The pages of a file

`FindPages` returns the wiki pages that belong to a file, by one rule over the file's database and format. A page
whose title holds the database's name or the format's belongs. The database's own page comes first, then its plural,
then the pages that hold either, then the format's page. `SearchPages` searches the modding pages, the titles first and
then the text of the cached pages.

## Patch notes

A `Note` is one patch's notes page: its version, the release a hotfix belongs to, the release date its opening text
gives, and whether it has a Modding section. Its title is `Patch 1.20`, or
`Patch 1.20.X` for a minor version's hotfix notes. `ListNotes` lists them newest first, by semver, with a minor
version's hotfix notes after all of its numbered patches. `ReadNote` returns one note's page.

## The lexicon

`BuildLexicon` builds the Lexicon tab's rows of one class: effect, trigger, link, scope or data function. A
`LexiconRow` carries the dump's blurb and usage, the scopes it runs in and the scope it leads to, and an `Example` for
each place the game's own files use it, as the schema's vanilla count kept them. Effects and triggers also take in the
scripted effects and triggers the game and the workspace's mods define, one row for each name the game keeps a copy of:
its call written from the parameters its body reads, `Defined` where that copy is, and the first places that use it.
The rows are sorted by name, letter case aside.

## Events

| event | data | sent when |
|---|---|---|
| `wiki:progress` | a `Progress` | a refresh fetched a page |
| `wiki:updated` | the error's text, "" when it worked | a refresh finished |

## Terms

- **Client**: what fetches one game's wiki, rate limited, and caches its pages.
- **Page**: one wiki page as the cache keeps it, its HTML sanitized.
- **Summary**: one page in the listing: its title and its category.
- **Listing**: what the wiki has for one game: its modding pages and its patch versions.
- **Note**: one patch's notes page.
- **Progress**: how far a refresh has come.
- **LexiconRow**: one effect, trigger, link, scope or data function the dumps declare, with the dump's text and its
  examples, or one scripted effect or trigger, with how a call to it is written.
- **Example**: one place in the workspace's files where a lexicon row's name is used, or defined.
