# lang

The language engine: it reads a game install, its dump folders and a workspace's mods, and answers
what every written word is. Each package under it has its design in its own README; this file is
the overview and the map. The spec is `.claude/plans/engine.md`; its decisions are cited here as
D-n.

## What it must do

- **Walk once, parse once.** Every install and workspace file PMT wants is listed (script, GUI,
  gfx, assets, coats of arms, history and setup, docs, sounds; binaries are listed, not read), and
  each is read by its kind: script, localization, docs, dumps. A file is parsed once and walked once
  per stage, as close as possible.
- **Derive each fact once, from sources in trust order: dumps, vanilla, docs, wiki.** Dumps declare
  commands, scopes, links, modifiers and data types. Vanilla states no claim; it is evidence: which
  positions exist, which keys sit under each, which forms and values are written. Docs say what is
  optional, required or defaulted. The wiki backs up where docs and dumps are missing.
- **Nothing is guessed.** A claim accuses a user's code only when a declared source states it, the
  source is fresh and no vanilla evidence contradicts it (`schema.Speaks`). What nothing declares
  stays unknown, and unknown is quiet. No tuned numbers, no check hand-coded for one case.
- **Mods are never authorities.** They are scanned with the install but give only definitions,
  references and hints. A saved mod file updates the index alone, with no rescan.
- **Durable.** A game update, a stale dump or a stale wiki never breaks it or brings false findings;
  features grow quieter, and the app says when it uses stale data. A game update is a rescan.
- **The schema comes from the game**, not hand-written tables. The one hand-declared per-game table
  is the registry in `game`.

## The pipeline

```
SCHEMA STAGE (runs when the game version or a dump file changes; the generator runs the same code)
  install ── walk ── parse (CST) ────────┐
  docs ── normalize ── parse ────────────┤
  wiki <pre> ── normalize ── parse ──────┼──► written into one position tree, in trust order
  dumps ── label table; usages normalized┘         (dumps, vanilla, docs, wiki)
                                                   ▼
                                Schema ──► cache (with the install index) ──► shipped copy (generator)

INDEX STAGE (every launch for mods; the install's index comes from the cache; one mod file on save)
  file ── parse (CST) ── one classify walk reading the Schema ──► Index (per file: definitions, references)

EDITOR AND PAGES
  query the Index and the Schema; an open file: parse + the same walk, with a visit (cursor or diagnose)
```

- **Schema stage** (`schema.Build`): one position tree that every source writes into. A position
  is a place in a database entry or in a command's arguments; a claim keeps the source that stated
  it, and a later source only fills what is empty. Docs, wiki templates and dump usages go through
  one normalizer to parseable script first (prose becomes comments, never dropped).
- **Index stage** (`index.Walk`): one walk of a file's CST with the schema classifies each key by
  its slot (a fields block, a triggers or effects block, a block nothing declares) and records the
  file's definitions, references and localization entries. Names resolve at query time, in the
  game's load order. A saved mod file is dropped, walked again and added back.
- **Cache key:** game, game version, app version, the install's folder, the workspace language,
  when the install last changed and the dump files' names and dates (D-28); the game's version is
  `game.ReadGameVersion` (D-11). A game update, an app update, a moved install or a new dump file is
  another cache file, so a rescan. The cache holds the schema and the install's index.
- **Dev builds** (`main.version` `dev` or empty) never read or write the cache, so every launch
  rebuilds from the current code. Release builds rebuild once per app update.
- **Dumps and the shipped backup:** local dump files always win when all are present, whatever
  their age (D-5). When any is missing, the scan takes the dump part of the schema from the shipped
  file for the game and version and reads everything else from the user's own install (D-27).
- **Walks per file:** one parse and one walk on a launch; install files are parsed and walked twice
  (once per stage) only when the schema regenerates.

## The packages

Imports run one way: `jomini` ← `loc`, `source` ← `schema` ← `shipped` ← `index` ← `session` ←
`lsp`. Nothing under `lang` imports `session`, `lsp` or `steam`. `schema` reads `game` and `wiki`;
`index` reads `game`, `wiki` and `local` (the cache folder and the atomic write).

| Package | Is | Design |
|---|---|---|
| `jomini` | The CST parser, the one parse form: error-tolerant, comments kept and attached to statements by go/parser's rules, UTF-8 byte offsets, the shapes of names, dates, numbers and `prefix:name` words. A leaf: it never imports `game` | the headers of `parse.go`, `cst.go`, `names.go` |
| `loc` | The localization dialect: entries, `$key$` interpolations, the language from a file's header or name, whether it has a BOM, and the format's own parse errors | the package comment of `parse.go` |
| `source` | Every file of an install, a mod or a dump folder: roots that mirror the game layout, the inventory in load order, the one format table, a file's text, image headers. It interprets nothing | the package comment of `source.go` |
| `schema` | The schema stage: the position tree, the dump reader, the doc normalizer and phrase catalogue, the entry detector with vanilla's observation, `Build`, and the one rule for when a claim may accuse | [schema/README.md](schema/README.md) |
| `index` | The index stage: the classify walk, the records and per-name lists, resolution, the entry-mode function, and the scan with its cache | [index/README.md](index/README.md) |
| `shipped` | The embedded backup schemas, one per game version, and the generator that writes them | [shipped/README.md](shipped/README.md) |

### `game`, the registry

`game` is outside `lang` and the only game-aware package. Its rows say what a folder cannot say:
how entries are named (name key, file name, event id), wrapper keys, history and setup folders,
scripted keywords, what each file extension's keywords mean (`.gui`, `.gfx`, `.asset`), nested
databases, the map table, first-wins databases, the entry-mode words and the positions whose words
name a database's own entries (an on_action's `on_actions` list, D-35). It also holds where each
game's dumps land, where its user data and Steam library are, and the version rules. The databases
themselves are discovered from the install, one per script folder, so a new folder registers itself.
A row field is kept only while code reads it, and a row the new sources make redundant is deleted.

### What reads the engine

`session` is one workspace open in the editor: the install's schema and index, the mods' index
over them, the open files, and one lock. `lsp` answers the editor's requests (hover, completion,
definition, references, diagnostics, edits) from one walk of the open file, stopped at the cursor or
run whole. The event graph, health and language-health services in `services/` answer the pages from
the same index and schema.
