---
title: Crusader Kings III
description: CK3 mod layout, LIOS/FIOS, events, and debug flags.
weight: 1
---

# Crusader Kings III

Install `game/` is the script root. Local mods usually live under `Documents/Paradox Interactive/Crusader Kings III/mod/`. Steam app `1158310`; Workshop `steamapps/workshop/content/1158310`. `script_docs` writes to Documents `logs/`.

Mods do not disable achievements (since 1.9). Multiplayer needs the same playset order.

## Mod layout

The launcher creates:

```
mod/my_mod.mod          # required (path=…)
mod/my_mod/
  descriptor.mod        # same keys except path
  common/ …
  events/ …
  history/ …
  localization/ …
  gui/ …
```

PMT **New Mod** writes a skeleton (descriptor, empty `common/` and `events/`, loc stub with BOM). You still need the sibling `.mod` with `path=` if you want the official launcher to see it; PMT attaches the **folder**.

```
version="0.1"
tags={ "Events" }
name="My Mod"
supported_version="1.16.*"
path="mod/my_mod"
```

`replace_path="history/characters"` unloads **all** vanilla files in that folder. Use only for total conversions.

Thumbnail: `thumbnail.png` in the mod folder (Workshop, ~1:1, under about 1MB).

## Overrides

- Same path+filename: full file replace. Lower playset mod wins vs other mods.
- Same top-level key in a **new** later-ASCII file: object replace ([LIOS]({{< relref "/concepts/load-order" >}})). Name files `zz_mymod_*.txt`.
- GUI types/templates: **FIOS** — prefix `00_`. Wrap types in a `types` group.
- Loc keys: `localization/replace/<lang>/` (see [localization]({{< relref "/concepts/localization" >}})).
- On actions: append `on_actions` / `events`; do not replace `effect`.
- You cannot override a single event or faith in isolation — copy the whole vanilla file.
- History characters **duplicate** rather than override — replace the file or `replace_path`.

## Events

`events/` — `namespace` + `namespace.id`. Fired from on_actions, decisions, interactions, other events. Copy vanilla structure; do not invent `type` values. Typical loc: `id.t`, `id.desc`, option names.

[Event Graph]({{< relref "/pages/event-graph" >}}) follows `trigger_event` and on_action links from the workspace's index.

## Debug

Launch: `-debug_mode -develop` (hot reload). Console `~`. `error.log` under Documents `logs/`. Explorer: `effect add_gold = 100` / `trigger is_adult = yes` — those names come from vanilla / `script_docs`, not from PMT.
