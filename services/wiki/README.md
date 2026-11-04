# wiki

The package reads the community wiki for the open workspace's game. It caches the pages, lists the patch notes and
builds the lexicon of the game's commands.

The wiki is read for the Wiki pane and the Patch Notes page only; no wiki text reaches a hover card or a verdict. The
game's row names its wiki. The lexicon reads the schema's commands, scopes and data functions, with the places the
game's own files use them, and the workspace places each one.

| file | holds |
|---|---|
| `wiki.go` | the events, `Progress`; `Service` with `ServiceStartup`; `ReadPage`, `ListNotes`, `Refresh`; the client of the open workspace's game |
| `client.go` | the package comment; `Page`, `Client`, `NewClient`; `FetchPage`, `LoadPage`; the cache of one sanitized page per file |
| `refresh.go` | `Summary`, `Listing`; `Client.Refresh`, `LoadListing`; the wiki's Modding and Patches categories, and the pages that are missing or stale |
| `sanitize.go` | `Sanitize`: a page's HTML cut to what the pane shows, over `x/net/html`; `LookupAttribute`, an element's attribute, which the steam service reads too |
| `pages.go` | `Note`; `FindPages`, `SearchPages`; the order of patch versions and a note's release date |
| `lexicon.go` | `LexiconRow`; `ListClasses`, `BuildLexicon`; the rows built for each class, kept for the index they were built from |

## The client and the cache

A `Client` reads one game's wiki through its API, rate limited by `x/time/rate`, with a timeout (`app.FetchURL`). It
caches each page, its HTML sanitized, as one JSON file, and the game's listing of pages as another, in the wiki's
folder of the app's cache. The service makes a client when a workspace first needs one, and a new one when a workspace
of another game opens. `ServiceStartup` refreshes the open game's wiki in the background each time a workspace opens.

`Refresh` fetches the listing, the titles of the wiki's Modding and Patches categories, and then every page that is
missing or stale. A page that cannot be fetched does not stop the others.

## Sanitizing

`Sanitize` strips a page's HTML down to what the article pane shows. The wiki's chrome, citations and pictures are
dropped, and so are the sections of links to elsewhere. The scripting tools go last, and a long table is folded. A link
is made absolute against the wiki's address, and a link to another page of the wiki carries that page's title as
`data-page`.

## The pages of a file

`FindPages` returns the wiki pages whose title holds the name of the file's database or format. The database's own page
comes first, then its plural, then the pages that hold either, then the format's page. `SearchPages` searches the
modding pages, the titles first and then the text of the cached pages.

## Patch notes

A `Note` is one patch's notes page. Its title is `Patch 1.20`, or `Patch 1.20.X` for a minor version's hotfix notes.
`ListNotes` lists them newest first, by semver, with a minor version's hotfix notes after all of its numbered patches.
A note's page is read as any other page, by `ReadPage`.

## The lexicon

The lexicon's classes are `lsp.Classes`, the one table the hover cards name commands by; `ListClasses` returns it in
the order the Lexicon tab shows them. `BuildLexicon` builds the rows of one class. A `LexiconRow` carries the dump's
blurb and usage, the scopes it runs in, the scope it leads to, and an `lsp.Site` for each place the game's own files
use it. Effects and triggers also take in the scripted ones the game and the workspace's mods define, one row for each
name the game keeps a copy of, with `Defined` where that copy is. The rows are sorted by name, letter case aside. The
service keeps the rows of each class for the mods' index they were built from, and builds them again for another index.

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
- **LexiconRow**: one effect, trigger, link, scope or data function the dumps declare, or one scripted effect or
  trigger.
