---
title: Games
description: CK3, Vic3, and EU5 (partial) — folder layout and override rules PMT understands.
weight: 5
---

# Games

PMT shares one Clausewitz/Jomini parser. **Effects, triggers, folders, and descriptors are per game.** Do not copy a CK3 event into Vic3 and expect it to load.

| Game | Descriptor | Script root in the mod |
|------|------------|------------------------|
| [Crusader Kings III]({{< relref "/games/ck3" >}}) | `.mod` + `descriptor.mod` | same layout as install `game/` |
| [Victoria 3]({{< relref "/games/vic3" >}}) | `.metadata/metadata.json` | that folder **is** `game/` (no extra `game/` segment) |
| [Europa Universalis V]({{< relref "/games/eu5" >}}) | `.metadata/metadata.json` | `loading_screen/`, `main_menu/`, `in_game/` — **partial** support |

Shared script syntax (comments, `yes`/`no`, `@foo`, dates, colors) matches vanilla. When in doubt, copy a vanilla file of the same type, and read the [Wiki pane]({{< relref "/pages/ide#wiki" >}}) and `script_docs`.
