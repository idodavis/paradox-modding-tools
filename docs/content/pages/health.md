---
title: Health
description: What the load order decides (contests, overrides, dependencies) and the problems in the mods.
weight: 5
---

# Health

What this workspace's load order decides, and what its mods get wrong.

{{< shot caption="Health: the cards, the table, and a row's definitions beside it." >}}

## The cards

Six cards, in two groups, each with its count. A card picks the table below it.

**Load order** (changes when you reorder mods):

| Card | Lists |
|------|-------|
| **Contests** | A name two or more mods define, with or without the game's own, and a whole file two mods ship at the same path. Its note counts how many follow [LIOS and FIOS]({{< relref "/concepts/load-order" >}}). |
| **Overrides** | A game object a mod replaces or adds to: its own definition, an `INJECT` or `REPLACE`, or a whole file at the game's path. |
| **Dependencies** | A mod that **depends on** another (uses what only the other defines) or is **affected by** it (the other's definition changes, adds to or removes what it uses). |

**Problems in the mods** (fixed in the files):

| Card | Lists |
|------|-------|
| **Dangling Refs** | A name, event or file a mod uses that nothing defines: a typo, a missing dependency, or a game patch that removed it. One row per name, however many places use it. |
| **Localization Issues** | A key left **untranslated** (a mod has it in the workspace's language and ships another language without it), and files the game reads wrong: syntax, a missing byte order mark, a header naming another language, a misspelled folder. One row per place to fix. |
| **Script Issues** | What the editor's checks find in script files: syntax, scopes, values, duplicates. One row per place to fix. |

A problem's **Type** is colored by what it is: red the game cannot read it (Syntax); orange something missing
(Undefined, Unknown event, Missing file, No target, Missing key, Untranslated); blue written wrong (Scope, Value);
teal a file or a duplicate to tidy (Duplicate, No BOM, Wrong header, Wrong folder).

## The table

Each column head sorts the table and, where the column's values are few (Kind, Rule, Kept, Mod, Relation, Other
mod, Type, Language, Origin), ticks the ones to show. The filter box searches every column. The edge between the
table and the pane drags to resize.

## The pane

Pick a row to open it beside the table:

- **a contest or override**: why that definition is kept ("For this kind the last mod in load order wins…"), then
  each definition with its first lines, the kept one first, each marked Kept, Injected, Replaced or Removed. A mod
  replacing another mod's own is marked so. **Compare the two** sets the kept file beside the one it replaces. A
  whole-file contest says how many names the file defines and how they differ;
- **a dependency**: each place the mod uses what the other defines;
- **a dangling reference**: each place it is written;
- **an untranslated key**: its line in the workspace's language, the file it goes in for the missing language,
  and what reads it;
- **any other problem**: its line.

Every place is a link that opens the file in the [IDE]({{< relref "/pages/ide" >}}) at that line. In the IDE, **Show
in Health** (right-click) opens the matching row here.

## Previewing another load order

The **Load order** bar at the top lists the game and the mods. Drag the mods to see what another order would keep,
without changing the workspace; **Save as workspace order** keeps it, as
[Settings]({{< relref "/pages/settings" >}}) would.

After a game patch, open the workspace (the install is scanned again by itself), then read Dangling Refs and
Overrides here.
