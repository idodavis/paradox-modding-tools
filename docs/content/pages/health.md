---
title: Workspace Health
description: Contests, overrides, dependencies, dangling refs, localization issues and script issues.
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
| **Dangling Refs** | A name, event or file a mod uses that nothing defines: a typo, a missing dependency, or a game patch that removed it. One row per name, however many places use it. |
| **Localization Issues** | A key left **untranslated** (the mod has it in English and ships another language without it), and localization files the game reads wrong: **syntax**, a missing byte order mark, a header naming another language, a misspelled folder. One row per place to fix. |
| **Script Issues** | What the editor's checks find in script files: syntax, scopes, values, duplicates. One row per place to fix. |

## The table and the pane

Each column head sorts the table and, where a column's values are few (type, kind, rule, language, origin), ticks the ones to keep. A problem's **Type** is colored by what it is: red the game cannot read it, orange something missing, blue written wrong, teal a file or a duplicate to tidy. The filter box searches every column. Pick a row to open it beside the table:

- a contest or override: every definition in load order, each with its first lines, the kept one marked;
- a dependency: each place the mod names what the other defines, with its line;
- a dangling reference: each place it is written;
- an untranslated key: its English line, the file in that language it goes in, and what reads it;
- any other issue: its line.

The workspace default loc language is an IDE and hover setting; the report covers every language.

## Load-order preview

Drag the load-order chips to preview the kept definitions **without** writing the workspace. **Save as workspace order** persists the same `SortOrder` as Settings.

After a game patch: reopen the workspace (the install is scanned again by itself), then use Dangling refs and Overrides here. See [roadmap]({{< relref "/roadmap" >}}).
