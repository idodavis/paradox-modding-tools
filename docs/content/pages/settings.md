---
title: Settings
description: The workspace's name, language, install, mods and load order; the app's appearance.
weight: 8
---

# Settings

The gear at the right of the top bar. The rail on the left has two groups, and **Find a setting** filters them:

- **Workspace** (the open workspace): General, Game install, Mods and load order;
- **App** (every workspace on this computer): Appearance.

Changes are saved as you make them. **Remove this workspace**, at the bottom of the rail, takes the workspace out of
PMT and goes to the [Library]({{< relref "/pages/library" >}}); its mod folders stay on disk.

The editor's own settings (font, text sizes, checks, the explorer, the Wiki pane) are in the IDE's gear: see
[IDE settings]({{< relref "/pages/ide#ide-settings" >}}).

{{< shot caption="Settings: Mods and load order, with a mod's card open." >}}

## General

- **Name**: shown in the Library and the workspace switcher.
- **Localization language**: the language PMT measures localization against, shows on hover cards and writes in
  new mods. Changing it reads the workspace again.
- **Page it opens on**: IDE, Event Graph, Health, Patch Notes or Publish, when you open the workspace from the
  Library or the switcher.
- **IDE**: **Reopen the IDE's tabs** brings back the files that were open, in their groups. **Hide binaries in the
  explorer** hides `.dll`, `.exe`, `.pdb` and the like.

## Game install

The installs PMT knows for this game, each marked **Cache ready** or **Not scanned**; its menu has **Change
folder…** and **Delete install** (refused while a workspace uses it). Beside them, **Found on this computer** lists
the installs PMT detected, each with its version and an **Add** button, or type an **Install folder** and a
**Name**. Changing the install reads the workspace again.

**script_docs** says whether the game's own scripting documentation was found, older than the game, or missing (and
PMT's copy used instead), with the folders it was read from and **How to make script_docs**. See
[three kinds of docs]({{< relref "/concepts/docs" >}}).

## Mods and load order

**Create a new mod** and **Add an existing mod** at the top. The **Load order** list starts with the game, read
only, then the mods: drag to reorder. A mod whose folder is gone says **Folder not found**, with **Locate**. Health
and the Event Graph follow a new order at once; colors stay with the mod.

Select a mod to open its card:

| Part | What it holds |
|------|---------------|
| **Open folder**, **Remove from workspace** | The second leaves the folder on disk |
| **Identity** | Its name, and its folder (**Change**) |
| **Color** | A slot color, or **Custom…** |
| **Thumbnail** | The listing image; **Change** replaces it |
| **Calendar** | Eras and their year offset, so event dates show as in-game years; **Work it out** computes the offset from a date as written and as seen in game |
| **Publishing** | The Workshop item it is linked to (**Unlink**), and where its description comes from (**Change source in Publish**) |
| **Used by and uses** | Which mods of the workspace read this one, and how many game objects it replaces (**Open in Health**) |
| **Upload exclusions** | Patterns, as in `.gitignore`, for files never uploaded; **Import** and **Export** `.workshop-ignore` |

Select the game's row for its color: automatic (a grey of the theme) or picked by hand.

## Appearance

These apply to every workspace and survive **Reset all PMT data**.

- **Theme**: seven families, each dark and light: **PMT**, **Catppuccin** (Mocha and Latte), **One**, **Pierre Soft**,
  **Crusader Kings**, **Europa Universalis** and **Victoria**. **Match the workspace's game** shows a CK3, EU5 or
  Vic3 workspace in its game's family; picking a family turns it off. The pages and the editor change together.
- **Light and dark**: the sun and moon in the top bar switch them; **Follow Windows** follows the system instead.
- **Interface scale**: 80 to 150%, every page. The editor's and the explorer's text sizes are in the IDE settings.
- **Pages**: which pages show in the top bar and the command palette. With none, the IDE stays.
- **Mod colors**: every mod's color; **Reset all to automatic** drops the ones picked by hand.
