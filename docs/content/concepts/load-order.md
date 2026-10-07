---
title: Load order
description: LIOS and FIOS, same-path overwrite, and the INJECT/REPLACE entry modes of Vic3 and EU5.
weight: 2
---

# Load order

In PMT and in this guide, load-order is **LIOS** (last in, only saved / last listed wins) unless a game says otherwise. Do not write **LISO**.

The rows you drag in the [wizard]({{< relref "/pages/wizard" >}}) and [Settings]({{< relref "/pages/settings" >}}), and the chips in [Health]({{< relref "/pages/health" >}}), are the same load order. Health can preview what another order keeps without changing the workspace; **Save as workspace order** keeps it.

## Same path, full file

If two origins ship the same relative path (`common/traits/00_traits.txt`), the later playset entry **replaces the whole file**. That is how all three games overwrite vanilla (and each other). Vic3 and EU5 also have entry modes (below).

Full-file overwrite is the blunt tool. Prefer a **new uniquely named file** for new objects (`mymod_traits.txt`) so you do not own the entire vanilla file through a patch.

## Same key, new file (LIOS)

If two files define the same top-level key (a trait id, a building id), **later ASCII filename wins** for that object. Name override files `zz_mymod_*.txt` so they sort last, and put the mod name in the file so `database_conflicts.log` is readable.

## FIOS (first in)

Some definitions are **FIOS**: the first file that registers the name wins. Per game:

- CK3: GUI types and templates;
- Vic3: GUI types, templates and events;
- EU5: events (its GUI types are last-wins).

For a GUI type, prefix the file `00_` and wrap types in a `types` group. Health labels each contest FIOS or LIOS so you can see which rule applied.

## Entry modes: `INJECT:` / `REPLACE:` (Vic3, EU5)

Victoria 3 and EU5 can patch **one object** without copying the vanilla file, by a word before its name. In EU5 they work on most `common/` top-level objects; **not** on on_actions, defines or events.

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

Vic3 and EU5 **events** are the opposite of LIOS: **first ID wins**. To change a vanilla event, ship a file that sorts *before* vanilla (e.g. `0000_mymod_events.txt`) with the same `namespace` + id.

## On actions

On all three games: **append** your `on_actions` / `events` / `random_events`. Do not replace a vanilla `effect` or `trigger` block unless you intend to own that pulse.

## What Health shows

[Health]({{< relref "/pages/health" >}}) turns these rules into its load-order cards:

- **Contests**: two or more mods define the same name, or ship the same file; the one kept follows FIOS, LIOS or the
  entry mode it is written with.
- **Overrides**: a mod replacing or adding to a game object, or a whole game file.
- **Dependencies**: a mod using what only another mod defines, or what another mod changes.

Its other cards (Dangling Refs, Localization Issues, Script Issues) are problems in the mods, whatever the order.
