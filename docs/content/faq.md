---
title: FAQ
description: An empty scan, localization the game ignores, a private Workshop item, one live workspace, macOS.
weight: 8
---

# FAQ

## Is there a Mac build?

**No.** macOS is not supported. Windows and Linux (amd64 desktop distros) only. See [getting started]({{< relref "/getting-started" >}}).

## The scan never finishes, or finds nothing

The first scan of a full install takes a while; the top bar shows its stage and progress. Leave the window open. If it says **Install not found**, or the explorer's game files stay empty, the install path is probably wrong: it must be the game's root folder, the one that holds `game/`. Fix the install in [Settings]({{< relref "/pages/settings" >}}) and reopen the workspace.

Two workspaces sharing one install and localization language share the cache. The cache is kept per language, so the first open in another language scans the install again.

## I changed vanilla / subscribed a Workshop item and PMT looks stale

- **Vanilla:** after a game patch the install is scanned again by itself the next time the workspace opens, and a toast says the game was updated.
- **Mods:** PMT watches the open workspace's mod folders, so files changed outside PMT are read again at once.
- Attach a new folder in Settings or the wizard after you subscribe on Workshop.

## Localization is “ignored” in-game

Almost always one of: missing **UTF-8 BOM**, filename not `*_l_<lang>.yml`, first line not `l_<lang>:`, folder spelled `localisation`, or an override sitting outside `replace/`. See [localization]({{< relref "/concepts/localization" >}}). The IDE diagnostics flag BOM/header problems on **mod** files.

## Workshop item stays private / upload fails

New Steam items stay **private** until you set them Public on the Workshop website. The Steam client must be running and signed in to an account that owns the game, with the game installed. EU5 has no Linux build, so publish EU5 mods from Windows. See [Publish]({{< relref "/pages/publish" >}}).

## Why did the other workspace’s graph go blank?

Only **one** workspace is live. Opening another drops the previous session. That is [intentional for this alpha]({{< relref "/limitations" >}}).

## What does `# pmt:ignore` do?

A comment `# pmt:ignore` turns the IDE's checks off on its own line and the next one; with codes (`# pmt:ignore unresolved-ref, missing-bom`), only those. To hide a check everywhere in the editor, turn it off in the IDE settings' **Checks** tab. Neither changes what the game loads.

## How do I check a game update?

Reopen the workspace (the install is scanned again by itself) and read [Health]({{< relref "/pages/health" >}}) (Dangling Refs and Overrides). [Patch Notes]({{< relref "/pages/patch-notes" >}}) has the wiki's notes for that version. Suggested file-level retarget is on the [roadmap]({{< relref "/roadmap" >}}).

## SmartScreen / my antivirus blocked the zip

The Windows zip is **unsigned**. That is expected. Prefer the GitHub Release asset and the in-app updater (checksums via `SHA256SUMS`).

## Can I run two copies of the same mod (local + Workshop)?

Do not. The game will load both. Remove one from the playset or unsubscribe.

## Check for updates says it cannot verify

The status bar's version button needs a Release that includes both the platform zip **and** a sibling `SHA256SUMS`.
