# wiki

[Architecture](../../docs/ARCHITECTURE.md) › [Services](../../docs/ARCHITECTURE.md#the-services) › wiki

The wiki service reads the community wiki of the open workspace's game. It caches the wiki's pages, lists the patch
notes and finds the pages that belong to a file. The game's row names its wiki. The
[Wiki pane](../../frontend/src/README.md) shows the pages, and the Patch Notes page shows the notes. The Wiki pane's
lexicon is the [lsp](../lsp/README.md) service's.

**Uses** [app](../app/README.md), [files](../../internal/files/README.md), [game](../../internal/game/README.md),
[lang](../../internal/lang/README.md), [workspace](../workspace/README.md) · **Used by** [mcp](../mcp/README.md), and
[steam](../steam/README.md), which reads an element's attribute with `LookupAttribute`.

## The refresh and the cache

```mermaid
flowchart TD
  opened["workspace:opened"] --> refresh["Refresh, in the background"]
  refresh --> listing["Fetch the Modding and Patches categories"]
  listing --> saved[("listing.json")]
  listing --> revisions["Ask each page's latest revision, 50 titles a query"]
  revisions --> stale{"Each page: cached at that revision?"}
  stale -->|no| fetch["Fetch, sanitize, cache with its revision"]
  fetch --> pages[("one JSON file per page")]
  fetch --> progress(["wiki:progress"])
  stale -->|yes| skip["Keep the cached page"]
  fetch --> updated(["wiki:updated"])
  skip --> updated
  read["ReadPage"] --> pages
  read -->|not cached| fetch
```

A `Client` reads one game's wiki through its API, rate limited, with a timeout. It caches each page, its HTML
sanitized, as one JSON file, and the game's listing of pages as another, in the wiki's folder of the app's cache. The
service makes a client when a workspace first needs one, and a new one when a workspace of another game opens.

`Refresh` runs in the background each time a workspace opens. It fetches the listing, which holds the titles of the
wiki's Modding category and the patch versions of its Patches category. Then it asks the wiki for the latest revision of
every listed page, 50 titles to a query, and fetches each page that is not cached or whose cached `Revision` differs.
When every page is current, a refresh makes the listing's requests and one revision query per 50 titles, and fetches no
page. A page cached before revisions were kept has a `Revision` of 0 and is fetched again once. A title the wiki gives
no revision for, such as a page it moved or deleted, is not fetched, and its cached copy stays readable. A page that
cannot be fetched does not stop the others; their errors are joined. `ReadPage` returns a cached page, or fetches it.

## Sanitizing

`Sanitize` strips a page's HTML down to what the article pane shows. The wiki's own chrome, citations and pictures are
dropped, and so are the See also, References, External links and Further reading sections. The scripting tools section
goes last, and a table of more than two rows is folded closed. A link is made absolute against the wiki's address, and
a link to another page of the wiki carries that page's title as `data-page`. A link keeps its address only if it is a
web address.

## The pages of a file

`FindPages` returns the titles of the wiki pages that belong to a file: those whose title holds the name of the file's
database or the topics the game's table gives for its format and folder. The database's own page comes first, then its
plural, then the pages that hold either, then the format's page, which is the page titled with the format's name alone,
then the pages that hold a topic. `SearchPages` searches the modding pages, the titles first and then the text of the
cached pages.

Until a refresh has fetched the listing, both return nil and no error, which the pane reads as "the wiki is being read"
and asks again when `wiki:updated` arrives. A file with no page gets an empty list.

## Patch notes

A `Note` is one patch's notes page. Its title is `Patch 1.20`, or `Patch 1.20.X` for a minor version's hotfix notes.
`ListNotes` lists them newest first, by semver, with a minor version's hotfix notes after all of its numbered patches.
A note's page is read as any other page. Its `Modding` flag says whether it has a Modding section, and it is false for a
note whose page is not cached.

## Files

| file | what it holds |
|---|---|
| `wiki.go` | The service: its events, reading a page, listing the notes and refreshing. |
| `client.go` | The client of one game's wiki, its pages and their cache. |
| `refresh.go` | The listing of the wiki's categories and the refresh of its pages. |
| `sanitize.go` | Cutting a page's HTML down to what the pane shows. |
| `pages.go` | Finding and searching pages, and the facts of a patch note. |

## Events

| event | data | sent when |
|---|---|---|
| `wiki:progress` | a `Progress` | a refresh fetched a page |
| `wiki:updated` | the error's text, "" when it worked | a refresh finished |

## Terms

- **Client**: what fetches one game's wiki, rate limited, and caches its pages.
- **Page**: one wiki page as the cache keeps it, its HTML sanitized, with the wiki's revision it was fetched at.
- **Listing**: what the wiki has for one game: the titles of its modding pages and its patch versions.
- **Note**: one patch's notes page.
- **Progress**: how far a refresh has come.
