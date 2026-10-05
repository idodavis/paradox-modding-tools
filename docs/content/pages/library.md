---
title: Library
description: Browse workspaces by game. Delete removes the PMT record only.
weight: 1
---

# Library

The Library is the list of your modding workspaces, grouped by game.

A **workspace** is one game install plus the mods you are editing.

{{< shot caption="Library with at least one workspace card." >}}

## What you can do

- **Click a card** to open that workspace’s default page (IDE unless you changed it in Settings / visible tools).
- **Edit** opens [Workspace Settings]({{< relref "/pages/settings" >}}).
- **Delete** removes the workspace from PMT only. Mod folders on disk stay.
- **New Workspace** starts the [wizard]({{< relref "/pages/wizard" >}}).
- **New Mod** creates a skeleton in an existing workspace, or starts the wizard when the library is empty.

## Reset all data

At the bottom of the Library. It wipes PMT workspaces, install records, leftover folders under the PMT config tree, and caches. It does **not** delete mods or Steam/game installs on disk. Theme, UI scale, and editor font are kept.

Use this when a bad install pin or leftover cache is worse than setting workspaces up again.

## Switching workspaces

The header dropdown switches workspace. Only one workspace is live at a time; the previous one's session is closed. Unsaved editor buffers follow the “remember tabs” setting.
