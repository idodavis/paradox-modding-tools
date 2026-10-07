---
title: Three kinds of “docs”
description: "The community wiki, the game's script_docs dumps and the install's _*.info files: three sources, kept apart."
weight: 4
---

# Three kinds of “docs”

People say “docs” for three different things. PMT keeps them apart on purpose.

| What | In PMT | Where it comes from | What it gives |
|------|--------|---------------------|---------------|
| Community wiki | The **Wiki** pane, **Patch Notes** | The game's wiki, fetched at start and cached | Pages to read beside the editor; patch notes |
| Console dumps | `script_docs` | Files the game writes in its user-data folder when you ask it to | What every effect, trigger and scope is, takes and returns |
| Install prose | game info | `_*.info` files and readmes **inside the game install** | A field's description on the hover card |

## The wiki

The IDE's [Wiki pane]({{< relref "/pages/ide#wiki" >}}) shows the community wiki's pages for the file you are
editing; [Patch Notes]({{< relref "/pages/patch-notes" >}}) reads the same wiki's patch-note pages. PMT also reads the
script examples on the wiki's pages, after every other source, for the keys an entry can write.

The wiki is for orientation. The vanilla files and `script_docs` are what the game actually does.

## script_docs

The game prints its own scripting documentation from its debug console (start it with `-debug_mode`, open the
console with `` ` `` or `~`): `script_docs`, and `dump_data_types` (Vic3: `DumpDataTypes`). The files land in the
game's folder in Documents: in `logs/` for CK3; in `logs/` and `docs/` for Vic3 and EU5; the data types in
`logs/data_types/`.

PMT reads them when the workspace opens, and checks they belong to the installed version. Without them
definitions, references, conflicts and localization still work; with them PMT knows each effect's and trigger's
description, what it takes (signature help) and the scopes it runs in and hands on, so scope-aware completion,
typed hover cards and scope checks need them. When a dump is missing, PMT uses its own copy for the game if it has
one; a copy for another version than yours helps completion and hover but never reports a problem.

Where you see their state: the Library's **Language** row, Settings' **Game install** (with **How to make
script_docs**), and a one-time toast when none were found. The states are **script_docs loaded**, **script_docs
older than the game**, **Using PMT's copy of script_docs** and **No script_docs**. Dumps older than the game show on
hover cards as **outdated script_docs**: run `script_docs` again after a patch.

## Install `_*.info`

Paradox ships short field descriptions beside the script in the install. PMT reads them for the hover card's
description of a field, by the kind's folders and the field it sits under; not every folder has one, and not every
field is described. A command's description and signature come from the dumps.

## Why this split exists

Calling all three “docs” sent people to the wrong place. If you file a bug, say **wiki**, **script_docs** or **game
info**, not “docs are missing.”
