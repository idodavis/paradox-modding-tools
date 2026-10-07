---
title: Workspace, install, user data
description: One workspace is one install plus the mods you attached. User data and Workshop are different trees.
weight: 1
---

# Workspace, install, user data

These are four different trees. Mixing them up is the usual reason a scan looks empty or a mod “isn’t in the workspace.”

## Workspace

A **workspace** is a PMT record: one game, one **install**, a list of **mod folders** with a load order, a localization language, and a few preferences (the page it opens on, reopening the IDE's tabs).

- Creating or deleting a workspace does **not** create or delete mods on disk (except **New Mod**, which writes a skeleton you asked for).
- **Remove from PMT…** in the [Library]({{< relref "/pages/library" >}}) removes the PMT record only.
- **Reset all PMT data**, in the Library's status bar, removes every workspace, install record and cache, and leftover folders under PMT's config folder. It does **not** delete your mods or the game. Your settings stay.

You can have many workspaces. Only **one** is live at a time: opening another drops the previous session. See [limitations]({{< relref "/limitations" >}}).

## Install

The **install** is the Steam or Paradox game folder (vanilla `game/` script, GUI, loc, and shipped `_*.info` files). PMT reads it as it is in the folder now.

The version comes from `<install>/launcher/launcher-settings.json` (`rawVersion`, then `version` with the `(Codename)` stripped); where that file is missing (common on EU5 right now), from Steam's build id. PMT’s vanilla cache is keyed by the version, the loc language, the install's last update and your dump files, so after a game patch the next session scans the install again by itself.

Two workspaces that share the same install and localization language share one vanilla cache. The cache is kept per language, so the first open in another localization language scans the install again.

## User data

**User data** is the Paradox Documents tree, not the install:

- Windows: `Documents/Paradox Interactive/<Game>/`
- Linux: `$XDG_DATA_HOME` or `~/.local/share/Paradox Interactive/<Game>/` (Proton: the prefix `Documents/Paradox Interactive/<Game>/`)

That is where the launcher’s default `mod/` parent lives, and where `script_docs` / `error.log` land (CK3: `logs/`; Vic3/EU5: `docs/` and `logs/`). You can attach any folder; new-mod skeletons are usually created next to the others.

## Workshop / attached mods

Workshop downloads live under `steamapps/workshop/content/<appid>/`. **Attach** folders (Workshop, Documents `mod/`, or anywhere) in the wizard or [Settings]({{< relref "/pages/settings" >}}).

PMT's index of those folders lives **in memory**: it is built when the workspace opens, and while it is open PMT watches the mod folders, so a file changed, added or deleted outside PMT is read again at once.

## PMT’s own config

App settings live under your OS user-config directory, folder **Paradox Modding Tools/** (`config.json`). If that file exists but cannot be read, PMT starts without it and will not write over it.
