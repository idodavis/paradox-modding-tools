---
title: FAQ
description: Scan stale, loc ignored, Workshop private, one live workspace, macOS.
weight: 8
---

# FAQ

## Is there a Mac build?

**No.** macOS is not supported. Windows and Linux (amd64 desktop distros) only. See [getting started]({{< relref "/getting-started" >}}).

## The language strip never says the session is ready

Vanilla scan of a full install takes time the first time. Leave the window open. If it stays empty, the install path is probably wrong: it must be the game's root folder, the one that holds `game/`. Fix the install in [Workspace Settings]({{< relref "/pages/settings" >}}) and reopen.

Two workspaces sharing one install and default loc language share the vanilla cache. The cache is kept per language, so the first open in another loc language scans the install again.

## I changed vanilla / subscribed a Workshop item and PMT looks stale

- **Vanilla:** after a game patch the install is scanned again by itself; reopen the workspace to use it.
- **Mods:** the mods' index lives in memory. Close and reopen the workspace (or switch away and back) after adding files outside the IDE.
- Attach a new folder in Settings or the wizard after you subscribe on Workshop.

## Localization is “ignored” in-game

Almost always one of: missing **UTF-8 BOM**, filename not `*_l_<lang>.yml`, first line not `l_<lang>:`, folder spelled `localisation`, or an override sitting outside `replace/`. See [localization]({{< relref "/concepts/localization" >}}). The IDE diagnostics flag BOM/header problems on **mod** files.

## Workshop item stays private / upload fails

New Steam items stay **private** until you set them Public on the Workshop website. Steam client must be running and signed in as the owner, with the game installed. EU5 has no Linux build, so publish EU5 mods from Windows. See [Publish]({{< relref "/pages/publish" >}}).

## Why did the other workspace’s graph go blank?

Only **one** workspace is live. Opening another drops the previous session. That is [intentional for this alpha]({{< relref "/limitations" >}}).

## What does `# pmt:ignore` do?

It suppresses IDE diagnostics on that line or the next (`# pmt:ignore-next-line`). Optional codes limit which warnings. The game still loads the file as written.

## How do I check a game update?

Reopen the workspace (the install is scanned again by itself) and read [Workspace Health]({{< relref "/pages/health" >}}) (Dangling refs and Overrides). [Patch Center]({{< relref "/pages/patch-center" >}}) has the wiki notes for that version. Suggested file-level retarget is on the [roadmap]({{< relref "/roadmap" >}}).

## SmartScreen / my antivirus blocked the zip

The Windows zip is **unsigned**. That is expected. Prefer the GitHub Release asset and the in-app updater (checksums via `SHA256SUMS`).

## Can I run two copies of the same mod (local + Workshop)?

Do not. The game will load both. Detach or unsubscribe one.

## Check for updates says it cannot verify

The footer version button needs a Release that includes both the platform zip **and** a sibling `SHA256SUMS`.
