---
title: Europa Universalis V
description: EU5 is partial in PMT — three stage folders and INJECT/REPLACE.
weight: 3
---

# Europa Universalis V

{{< callout warning >}}
EU5 coverage in PMT is **partial**. Stage roots and entry modes are understood; do not expect CK3-level folder and effect coverage.
{{< /callout >}}


Install script is split under `game/`: `loading_screen`, `main_menu`, `in_game`. Local mods: `Documents/Paradox Interactive/Europa Universalis V/mod/<mod>/` — **the same three top folders**, no extra `game/` segment. Steam app `3450310`. `script_docs` writes to Documents `docs/`.

If `launcher/launcher-settings.json` is missing, PMT reads the version from Steam's build id (shown as `build 24187685`), and empty when there is neither; the install is scanned and cached either way.

## Mod layout

```
mod/my_mod/
  .metadata/metadata.json
  .metadata/thumbnail.png     # 512×512 recommended, under about 1MB
  loading_screen/
  main_menu/
  in_game/                    # gameplay (most common/, events, setup)
```

Wrong: `mod/my_mod/game/in_game/...` or `mod/my_mod/events/...` (missing top folder). Misspelled folders are silently ignored.

`in_game/common/on_action/` — not `in_game/on_action/`.

`metadata.json` keys include `name`, `id`, `version`, `supported_game_version` (`1.0.*`), `tags`, `relationships`, and `game_custom_data.replace_paths` (unloads that folder’s **files**, not subfolders).

## Database entry modes

Usable on most `common/` top-level objects. **Not** on on_actions, not defines, **not** events. **INJECT on scripted_effect/trigger acts like REPLACE.**

```
INJECT:building_example = { possible_production_methods = { foo } }
REPLACE:law_foo = { … }
```

See [load order]({{< relref "/concepts/load-order" >}}) for `TRY_*` / `*_OR_CREATE` and process order.

`INJECT:` appends a sibling block. It cannot patch a nested key.

## Other override rules

- Same path+filename: full replace. Lower playset wins vs other mods.
- Files load ASCII order; **subfolder files load after** parent-folder files.
- Events: **first ID wins** (opposite of LIOS). To change a vanilla event, a file that sorts **before** vanilla (e.g. `0000_mymod_events.txt`) with the same `namespace` + id.
- On actions: append `on_actions` / `events` / `random_events` only.
- Loc: `localization/<lang>/replace/` under the relevant top folder (`main_menu` and/or `in_game`).
- GUI types: last **mod in the playlist** wins; filename does not.

## Events

`in_game/events/` (subfolders OK). `namespace.id` with id in `1..9999`. Copy vanilla; do not invent scopes or types.

## Debug

Steam launch: `-debug_mode` (hot reload; not on_actions). Console `~`. `--ignore-disable-mods-on-crash` keeps the playset after a load crash. `error.log` in Documents `logs/`. Non-ASCII Documents paths can fail `fopen()` on mod files.
