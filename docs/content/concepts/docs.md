---
title: Three kinds of “docs”
description: Wiki/Guide vs script_docs dumps vs install _*.info — three pipelines.
weight: 4
---

# Three kinds of “docs”

People say “docs” for three different things. PMT keeps them separate on purpose.

| What | In PMT | Where it comes from | What it is |
|------|--------|---------------------|------------|
| Community wiki | **Guide** | Cached MediaWiki HTML | The pane beside the IDE. Search, contents, related pages. |
| Console dumps | `script_docs` | User-data `logs/` (CK3) or `docs/` (Vic3/EU5) | Optional overlay. In-game `script_docs` / `dump_data_types`. The health strip shows whose dumps were read (yours, PMT's, or **No script_docs** when none were) and opens the guide to writing them when you click it. |
| Install prose | game info | `_*.info`, readmes, `*.md` **inside the game install** | Read for the hover's description of a field. |

## Guide (wiki)

The [IDE]({{< relref "/pages/ide" >}}) **Guide** pull-tab is the community wiki. [Patch Center]({{< relref "/pages/patch-center" >}}) is the same wiki pipeline, scoped to patch-note pages.

Wiki is orientation. Vanilla files and `script_docs` are the API when you write script.

## script_docs

From the game’s debug console (`~` with `-debug_mode`):

- CK3: writes under Documents `Crusader Kings III/logs/`
- Vic3 / EU5: Documents `<Game>/docs/` (Vic3 also `DumpDataTypes`; EU5 `dump_data_types` under `logs/data_types`)

Dumps are optional: definitions, references, conflicts and localization work without them. Dumps add each effect's and trigger's description, its usage (the signature help) and the scopes it runs in and hands on, so scope-aware completion, typed hover and scope diagnostics need them. If the health strip shows **No script_docs**, run `script_docs` in-game once; PMT reads the files when the workspace next opens.

## Install `_*.info`

Paradox ships short field descriptions next to script in the install. PMT reads them for the hover's description of a field, by the kind's folders and the field it sits under; not every folder has one, and not every field is described.

A token's description and signature come from the dumps.

## Why this split exists

Collapsing “docs” into one word caused the wrong pane to open and the wrong cache to update. If you file a bug, say **Guide**, **script_docs**, or **game info** — not “docs are missing.”
