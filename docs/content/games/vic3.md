---
title: Victoria 3
description: Vic3's metadata.json layout, its overrides and entry modes, and events that never fire by themselves.
weight: 2
---

# Victoria 3

Install `game/` is the script root. Local mods: `Documents/Paradox Interactive/Victoria 3/mod/<mod>/` — that folder **is** `game/` (no extra `game/` segment). Steam app `529340`. `script_docs` writes to Documents `docs/` (`event_scopes.log` to `logs/`), `DumpDataTypes` to `logs/data_types/`.

Mods do not disable achievements (separate game rule).

## Mod layout

```
mod/my_mod/
  .metadata/metadata.json    # required or the game will not load the mod
  thumbnail.png              # at the mod's root
  common/
  events/
  localization/
  gfx/
  gui/
```

Do not edit the install. Vic3 has no CK3-style `.mod` file; a folder the mod replaces whole is listed in `metadata.json`'s `game_custom_data.replace_paths`, which PMT reads too. Overlay by path; unique filenames for new objects.

`metadata.json` (launcher): `name`, `id`, `version`, `supported_game_version`, `tags` (max 5), `relationships` (`dependency`, `incompatible_with`, `load_before`, `load_after`).

PMT's [Publish]({{< relref "/pages/publish" >}}) edits this metadata and the listing's Markdown or BBCode.

## Overrides

- Same path+filename: full file replace. Last ASCII filename wins for the same object key ([LIOS]({{< relref "/concepts/load-order" >}})), taking priority over playset order.
- One object without copying its file: an entry mode before its name, `INJECT:`, `REPLACE:` and their `TRY_` and `_OR_CREATE` forms ([entry modes]({{< relref "/concepts/load-order" >}})).
- **Events, GUI types and templates are first-wins**: the first file to define the id keeps it.
- New objects: new file (`mymod_buildings.txt`) in the vanilla folder.
- Loc: `localization/<lang>/replace/`.
- On actions: append; do not replace vanilla `effect`.
- History states/buildings/pops: naive annex in `history/states` leaves buildings without workers — follow the Vic3 wiki state-modding guide (remove-then-recreate under `region_state:<TAG>`).

Non-ASCII user paths can fail to lexer-load files in the game.

## Events

`events/` (subfolders OK). UTF-8 BOM. **Events never fire by themselves** — they need `trigger_event` from a journal entry, decision, on_action, or another event.

```
namespace = mymod_events

mymod_events.1 = {
    type = country_event
    title = mymod_events.1.t
    desc = mymod_events.1.d
    flavor = mymod_events.1.f
    immediate = { }
    option = { name = mymod_events.1.a default_option = yes }
}
```

Need `namespace`, at least one `option` with `default_option = yes` (unless `hidden = yes`). `type` is `country_event`, `state_event`, or `character_event` — copy vanilla.

## Debug

Launch: `-debug_mode`. `error.log` in Documents `logs/`.
