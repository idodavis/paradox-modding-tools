---
title: Workspace Settings
description: Name, loc language, install, mods, Workshop ignore.
weight: 8
---

# Workspace Settings

Configure **this** workspace. Appearance is [Display]({{< relref "/pages/display" >}}) in the header.

{{< shot caption="Workspace Settings mods/order/colors." >}}

## Overview

- Display **name**
- **Default loc language** (IDE / hover)
- Whether the IDE **remembers open files**
- Which page the Library should open (**default tool**)

## Game

Pick or add an install; its version is read from `launcher/launcher-settings.json`. Changing install rebuilds language intelligence.

**Scripting documentation** shows where the game's `script_docs` output was read and how many effects, triggers and scope types it declares, with how to generate it. See [three kinds of docs]({{< relref "/concepts/docs" >}}).

## Mods

Attach an existing folder or create a new skeleton, then **color**, **reorder**, or **detach**. Detach does not delete the folder on disk.

Per-mod **description file paths** and **Workshop ignore** patterns live here.

## Editor

**Diagnostics** turns each check on or off, with **Reset to defaults**. These are IDE settings: they apply to every workspace.

## Danger

**Remove workspace** deletes the PMT record only. **Reset all data** lives on the [Library]({{< relref "/pages/library" >}}).
