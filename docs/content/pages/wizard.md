---
title: Create Workspace
description: Game, install, mods, name — then you land in the IDE.
weight: 2
---

# Create Workspace

The wizard is how you add a workspace. You can change every choice later in [Workspace Settings]({{< relref "/pages/settings" >}}).

Walk through **game → install → mods → name**.

{{< shot caption="Wizard Mods step with load-order drag handles." >}}

## Game

CK3, Vic3, or EU5. EU5 is [partial]({{< relref "/games/eu5" >}}).

## Install

Detect a known Steam/Paradox path or **Add** a folder. Multiple installs per game are allowed (beta vs live, different drives).

- **Version** — read from `launcher/launcher-settings.json` and shown beside the install. After a game patch the next session scans the install again by itself.
- Changing install later rebuilds language intelligence for that install.

{{< shot caption="Wizard Install step (detected path + version)." >}}

## Mods

- **Add existing** — any folder (Documents `mod/`, Workshop content, a git clone).
- **Create new mod** — descriptor (CK3 `.mod` + `descriptor.mod`; Vic3/EU5 `.metadata/metadata.json`), empty `common/` and `events/` (EU5: under the stage folders), readmes, localization stub **with UTF-8 BOM**.
- **Drag** to set load order (`SortOrder`). Same order as Settings and Health.
- A mod whose folder is gone shows **Missing**; you can fix its path later without deleting the workspace.

You do not have to attach every Workshop item — only what you are editing or depending on.

## Name

Display name in the Library and title bar. After create you land in the [IDE]({{< relref "/pages/ide" >}}) so you can open the files you just attached.

**Back** from the IDE returns to the Library, not to this wizard.
