---
title: New workspace
description: Game, install, mods, details; then the workspace opens in the IDE.
weight: 2
---

# New workspace

The wizard adds a workspace in four steps: **Game**, **Install**, **Mods** and **Details**. **Back** and
**Continue** move between them; every choice can be changed later in [Settings]({{< relref "/pages/settings" >}}).

{{< shot caption="New workspace: the Mods step, with rows to drag into load order." >}}

## Game

Crusader Kings III, Victoria 3 or Europa Universalis V. EU5 is [partial]({{< relref "/games/eu5" >}}).

## Install

Pick an install PMT already knows, or add one: **Found on this computer** lists the installs PMT detected, with
their versions (**Add**), or type an **Install folder** and a **Name**. A game can have several installs (a beta
and the live version, or two drives).

PMT reads the install's version at each scan. After a game patch, the next time the workspace opens it is scanned
again by itself, with a toast that says so.

## Mods

The mods you work on, in load order: drag the rows, and **Leave out** removes one.

- **Add an existing mod**: any mod folder (in Documents' `mod/`, the Workshop's content, a git clone).
- **Create a new mod**: a name, description, folder, localization language and thumbnail. PMT writes the
  descriptor (CK3's `descriptor.mod`, or Vic3's and EU5's `.metadata/metadata.json`), the game's mod
  folders, a localization file with its UTF-8 byte order mark and a Workshop description.

You can leave this step empty, and you do not need every Workshop mod you play with: only those you edit or depend
on.

## Details

**Name** the workspace and pick its **Localization language**. **Create workspace** opens it in the
[IDE]({{< relref "/pages/ide" >}}).
