---
title: Workspace, install, user data
description: One workspace is one install plus the mods you attached. User data and Workshop are different trees.
weight: 1
---

# Workspace, install, user data

These are four different trees. Mixing them up is the usual reason a scan looks empty or a mod “isn’t in the workspace.”

## Workspace

A **workspace** is a PMT record: one game, one **install**, a list of **mod folders** with a load order, a default loc language, and UI prefs (remember tabs, default tool).

- Creating or deleting a workspace does **not** create or delete mods on disk (except **New Mod**, which writes a skeleton you asked for).
- [Library]({{< relref "/pages/library" >}}) **Delete** removes the PMT record only.
- **Reset all data** on the Library wipes PMT workspaces, install records, leftover folders under the PMT config tree, and caches. It does **not** delete your mods or Steam/game installs. Theme, UI scale, and editor font stay.

You can have many workspaces. Only **one** is live at a time: opening another drops the previous session. See [limitations]({{< relref "/limitations" >}}).

## Install

The **install** is the Steam or Paradox game folder (vanilla `game/` script, GUI, loc, and shipped `_*.info` files). PMT’s vanilla cache is keyed by install id + version.

Version comes only from `<install>/launcher/launcher-settings.json` (`rawVersion`, then `version` with the `(Codename)` stripped). If that file is missing (common on EU5 right now), PMT stores `""` and **Add game install** defaults the pin to `latest`.

`latest` means “whatever is in the folder now” — useful across patches. A pinned version is a cache key; if you pin `1.16.2` and then the folder updates, rescan or change the pin. **Your pin always wins over auto-detection.**

Two workspaces that share the same Steam install share one vanilla script cache. Different default loc languages load different loc sidecars without rescanning script.

## User data

**User data** is the Paradox Documents tree, not the install:

- Windows: `Documents/Paradox Interactive/<Game>/`
- Linux: `$XDG_DATA_HOME` or `~/.local/share/Paradox Interactive/<Game>/` (Proton: the prefix `Documents/Paradox Interactive/<Game>/`)

That is where the launcher’s default `mod/` parent lives, and where `script_docs` / `error.log` land (CK3: `logs/`; Vic3/EU5: `docs/` and `logs/`). You can attach any folder; new-mod skeletons are usually created next to the others.

## Workshop / attached mods

Workshop downloads live under `steamapps/workshop/content/<appid>/`. **Attach** folders (Workshop, Documents `mod/`, or anywhere) in the wizard or [Workspace Settings]({{< relref "/pages/settings" >}}).

Harvest of those folders is **RAM-only**. It is rebuilt when the workspace opens. If the session looks wrong, close and reopen the workspace or change the install so vanilla is rescanned.

## PMT’s own config

App settings live under your OS user-config directory, folder **Paradox Modding Tools/** (`config.json`). If that file exists but cannot be read, PMT starts without it and will not write over it.
