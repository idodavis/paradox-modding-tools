# wiki

Fetches, sanitizes, stores and looks up the community wiki's guides and patch notes, and hands the engine the script
examples a page holds. It imports `game` and `appdata` and nothing above them.

| file | holds |
|---|---|
| `client.go` | `Client` (`New`): the HTTP client the wikis accept, the pause between calls, and the MediaWiki API calls a refresh makes |
| `refresh.go` | `Refresh`: fetch a game's pages again, reusing every page whose revision has not changed, into one sidecar file per game; the title filter |
| `lookup.go` | the queries over a sidecar: `PagesFor`, `NamedPages`, `GuidePage`, `Patches`, `PatchByVersion`, `ScriptExamples`; the payload types |
| `sanitize.go` | a fetched page reduced to the allowed tags and attributes, without the wiki's chrome, and cut into sections |

## Decisions

- **One `Client` holds the state** (the pause, the cache folder, the sidecars read into memory, the refresh lock), so a
  test makes its own and nothing is a package global.
- **A page is stored sanitized,** once, at refresh; a read never touches the wiki's HTML.
- **The engine takes examples as text.** `schema` is given `ScriptExamples` through `index.Request.Wiki`; it parses no
  HTML and imports no wiki.
