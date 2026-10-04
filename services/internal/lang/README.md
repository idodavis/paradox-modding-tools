# lang

The language engine: it reads a game install, its dump folders and a workspace's mods, and answers
what every written word is. Each package under it has its design in its own README; this file is
the overview and the map.

## What it must do

- **Walk once, parse once.** Every install and workspace file is listed and read by its kind:
  script, localization, docs, dumps. A file is parsed once and walked once per stage.
- **Each fact once, from sources in trust order: dumps, vanilla, docs, wiki.** The dumps declare
  commands, scopes, links, modifiers and data types. Vanilla is evidence: which positions exist and
  what is written there. The docs say what is optional, required or defaulted. The wiki backs up
  where the docs and dumps are missing.
- **Nothing is guessed.** A claim accuses a user's code only when a declared source states it, the
  source is fresh and vanilla does not contradict it (`schema.Speaks`). What nothing declares stays
  unknown, and unknown is quiet. No tuned numbers, no check written for one case.
- **Mods are never authorities.** They give definitions and references only. A saved mod file
  updates the index alone.
- **Durable.** A game update or a stale dump never brings false findings; features grow quieter and
  the app says when its data is stale.
- **The rules are data.** What Paradox script is lives in `game/lang-rules.go`; what one game's
  files look like is that file's `Layout` rows. The walker reads those tables and nothing else.

## The pipeline

```
LOCALIZATION (english and the workspace language)
  loc files ── parse ──► loc records (to the index) and the install's loc keys (to the schema)

SCHEMA (when the game version or a dump file changes; the generator runs the same code)
  install ── parse ── walk (observe) ─┐
  docs, wiki <pre> ── normalize ── parse ── walk (join) ──► one position tree, in trust order
  dumps ── label table; usages ── walk (usage) ───┘

INDEX (the install's from the cache; the mods on every launch; one mod file on save)
  file ── parse ── walk (records) ──► Index: each file's definitions and references

EDITOR AND PAGES
  query the Index and the Schema; an open file: parse + the same walk, stopped at the cursor
```

One walker (`schema.Read`) does every walk above, reading each key by the block's rules: what the
key is, what its block holds, and the scope the block runs in. Names resolve at query time, in the
game's load order; `index.Winner` applies the entry modes. The cache holds the schema and the
install's index, keyed by the game and app versions, the install folder, the workspace language, the
install's update time and the dump files. Local dump files win when all are present, whatever their
age; when any is missing, the shipped dump part for the game version stands in.

## The packages

Imports run one way: `jomini` ← `loc`, `source` ← `schema` ← `shipped` ← `index` ← `session` ←
`lsp`. `schema` reads `game` and `wiki`; `index` reads `game`, `wiki` and `local`.

| Package | Is | Design |
|---|---|---|
| `jomini` | The parser: error-tolerant, comments kept and attached, UTF-8 byte offsets, the shapes of names, dates, numbers and `prefix:name` words. It knows no game | the headers of `parse.go`, `cst.go`, `names.go` |
| `loc` | The localization dialect: entries, `$key$`, the language from a header or name, the BOM | the package comment of `parse.go` |
| `source` | Every file of an install, a mod or a dump folder, in load order, by format. It interprets nothing | the package comment of `source.go` |
| `schema` | The position tree, the walker, the dump and doc readers, the entry detector and vanilla's evidence | [schema/README.md](schema/README.md) |
| `index` | The records, resolution, entry modes, and the scan with its cache | [index/README.md](index/README.md) |
| `shipped` | The embedded backup dump schemas and their generator | [shipped/README.md](shipped/README.md) |

### `game`

`game` is outside `lang` and the only game-aware package. `lang-rules.go` holds the language rules
(block kinds and how a key reads in each, command contents, structural keys, the scope table, the
name rules, the written forms) and each game's `Layout`: folder rows, nested entries, the map table,
entry modes and first-wins databases. `game.go` holds each game's identity, install and Steam
discovery, user-data folders and wiki. The databases themselves are discovered from the install, one
per script folder, so a new folder registers itself.

### What reads the engine

`session` is one workspace open in the editor: the install's schema and index, the mods' index over
them, the open files, and one lock. `lsp` answers the editor (hover, completion, definition,
references, diagnostics, edits) from one walk of the open file. The event graph, health and
language-health services in `services/` answer the pages from the same index and schema.
