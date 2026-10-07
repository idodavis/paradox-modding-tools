---
title: Library
description: Your workspaces by game, the active one in full, new workspaces and mods, and Reset all PMT data.
weight: 1
---

# Library

The Library lists your workspaces, grouped by game. **PMT** at the left of the top bar brings you back here.

A **workspace** is one game install plus the mods you load with it.

{{< shot caption="Library: the active workspace's panel and the cards by game." >}}

## The active workspace

At the top, the workspace you last opened: its install and version, and when it was scanned. **Open IDE**,
**Health** and **Settings** open those pages, and its **…** menu has **Remove from PMT…**. Below:

- **Load order**: the game, then the mods in order;
- **Language**: whether the game's `script_docs` were found (click it for how to make them);
- **Health**: contests (objects more than one mod defines), dangling references and localization issues, from
  the last scan.

## The cards

Click a card to open that workspace on its **Page it opens on** (set in
[Settings]({{< relref "/pages/settings" >}})). Each card's **…** menu has **Settings** and **Remove from PMT…**.
Removing a workspace takes it out of PMT only: its mod folders stay on disk.

Each game ends with a **New workspace for this game** tile, which starts the
[wizard]({{< relref "/pages/wizard" >}}) at its Install step.

## New workspace, new mod

**New workspace** starts the wizard. **New mod** opens **Create a mod** for the active workspace: a name,
description, folder, localization language and thumbnail. PMT writes the descriptor, the game's mod folders, a
localization file with its byte order mark and a Workshop description, and adds the mod to the workspace. With no
workspace yet, it starts the wizard instead.

## Switching workspaces

The top bar's workspace menu switches workspace and keeps you on the same page. One workspace is open at a time:
opening another closes the first one's session. Its tabs are saved first and come back next time (unless **Reopen
the IDE's tabs** is off). Files with unsaved changes are asked about first: **Save all**, **Don't save** (the
changes are dropped) or **Cancel**.

## Reset all PMT data

In the status bar at the bottom. It removes every workspace and install record, clears the language cache and
deletes leftover folders. Your mods, the game files and your settings stay. Type `RESET` to confirm.

Use it when a bad install record or a broken cache is worse than setting your workspaces up again.
