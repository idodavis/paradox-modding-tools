---
title: Load order
description: LIOS and FIOS, same-path overwrite, and EU5 INJECT/REPLACE.
weight: 2
---

# Load order

In PMT and in this guide, load-order is **LIOS** (last in, only saved / last listed wins) unless a game says otherwise. Do not write **LISO**.

The chips you drag in the [wizard]({{< relref "/pages/wizard" >}}), [Workspace Settings]({{< relref "/pages/settings" >}}), and [Health]({{< relref "/pages/health" >}}) are the same **SortOrder**. Health can preview winners without writing the workspace; **Save as workspace order** persists it.

## Same path, full file

If two origins ship the same relative path (`common/traits/00_traits.txt`), the later playset entry **replaces the whole file**. That is how CK3 and Vic3 overwrite vanilla (and each other). EU5 also does this; it then adds entry modes (below).

Full-file overwrite is the blunt tool. Prefer a **new uniquely named file** for new objects (`mymod_traits.txt`) so you do not own the entire vanilla file through a patch.

## Same key, new file (LIOS)

If two files define the same top-level key (a trait id, a building id), **later ASCII filename wins** for that object on CK3/Vic3. Name override files `zz_mymod_*.txt` so they sort last, and put the mod name in the file so `database_conflicts.log` is readable.

## FIOS (first in)

Some definitions are **FIOS** — the first file that registers the type wins. CK3 **GUI types/templates** are the usual example: prefix `00_` and wrap types in a `types` group. Health labels contests as FIOS or LIOS so you can see which rule applied.

## EU5 `INJECT:` / `REPLACE:`

EU5 can patch **one object** without copying the vanilla file. Usable on most `common/` top-level objects. **Not** on on_actions, not on defines, **not** on events.

```
INJECT:building_example = { possible_production_methods = { foo } }
REPLACE:law_foo = { … }
```

| Mode | If the object is missing |
|------|--------------------------|
| `INJECT:` / `REPLACE:` | Errors |
| `TRY_INJECT:` / `TRY_REPLACE:` | Silent skip |
| `INJECT_OR_CREATE:` / `REPLACE_OR_CREATE:` | Creates |

`INJECT:` **appends a sibling block**. It cannot patch a nested key. `INJECT` on a scripted_effect or scripted_trigger acts like `REPLACE`. Later ASCII filename wins within the same mode.

EU5 **events** are the opposite of LIOS: **first ID wins**. To change a vanilla event, ship a file that sorts *before* vanilla (e.g. `0000_mymod_events.txt`) with the same `namespace` + id.

## On actions

On all three games: **append** your `on_actions` / `events` / `random_events`. Do not replace a vanilla `effect` or `trigger` block unless you intend to own that pulse.

## What Health shows

[Workspace Health]({{< relref "/pages/health" >}}) turns these rules into cards:

- **Contests** — two or more mods define the same name; the one kept follows FIOS/LIOS.
- **Overrides** — one mod's definition over the game's own of the same name.
- **Dependencies** — a mod names something only another mod defines.
- **Dangling refs** — a name written that nothing defines in the live session.
