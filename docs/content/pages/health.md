---
title: Workspace Health
description: Contests, overrides, dependencies, dangling refs, loc gaps and missing files.
weight: 5
---

# Workspace Health

What this workspace's load order decides, and what it lacks.

{{< shot caption="The cards, the table, and a row's definitions beside it." >}}

## The cards

Six cards count what the report found. A card picks the table below it.

| Card | Meaning |
|------|---------|
| **Contests** | A name two or more mods define, with or without the game's own. The one kept follows [FIOS / LIOS]({{< relref "/concepts/load-order" >}}); a contest the game has no part in is marked as a mod overriding another mod's own. |
| **Overrides** | One mod's definition over the game's own of the same name. |
| **Dependencies** | A mod naming something only another mod defines. |
| **Dangling refs** | A name written that nothing defines: a typo, a missing dependency, or a game patch that removed it. The ones the game itself would reject are marked. |
| **Loc gaps** | For each language the mods ship: **missing** (a definition's conventional key is absent), **untranslated** (English has the key, this language does not), **orphaned** (a mod's key that nothing of that mod reads). |
| **Missing files** | A definition without the file its kind always has, such as an icon. |

## The table and the pane

Each column head sorts the table and, where a column's values are few (kind, rule, language, gap, origin), ticks the ones to keep. The filter box searches every column. Pick a row to open it beside the table:

- a contest or override: every definition in load order, each with its first lines, the kept one marked;
- a dependency: each place the mod names what the other defines, with its line;
- a dangling reference: each place it is written;
- a loc gap: the key's text in each language, quoted, and what reads it.

The workspace default loc language is an IDE and hover setting; the report covers every language.

## Load-order preview

Drag the load-order chips to preview the kept definitions **without** writing the workspace. **Save as workspace order** persists the same `SortOrder` as Settings.

After a game patch: reopen the workspace (the install is scanned again by itself), then use Dangling refs and Overrides here. See [roadmap]({{< relref "/roadmap" >}}).
