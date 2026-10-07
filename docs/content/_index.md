---
title: Home
description: Paradox Modding Tools — alpha desktop workspace for CK3, Vic3, and EU5 modders.
weight: 1
---

# Paradox Modding Tools

{{< img src="assets/banner.png" class="pmt-banner" alt="Paradox Modding Tools — Alpha · CK3 · Vic3 · EU5" >}}

A desktop workspace for **Crusader Kings III**, **Victoria 3**, and **Europa Universalis V** (partial) modders. One workspace is one game install plus the mods you are editing. From there you get a script-aware IDE, an event graph, compatibility and localization health, wiki patch notes, and a Steam Workshop / Paradox Mods publish flow.

PMT is **unofficial** and not affiliated with Paradox Interactive. Licensed [GPL-3.0-or-later](https://github.com/idodavis/paradox-modding-tools/blob/master/LICENSE).

{{< callout warning >}}
This is **alpha** software. Expect bugs and incomplete game coverage. Back up your mods.
{{< /callout >}}


## Supported platforms

| OS | Status |
|----|--------|
| **Windows** 10 / 11 | Supported. Windows on ARM runs the amd64 zip under x64 emulation. |
| **Linux** amd64 | Supported on traditional desktop distros Wails v3 supports: Debian/Ubuntu, Fedora/RHEL, Arch, openSUSE, Gentoo (GTK4 + WebKitGTK 6). |
| **macOS** | **Not supported.** No downloads, no “build from source on Mac” path. |

Steam Deck, Flatpak, AppImage, and immutable distros are unsupported.

## Supported games

| Game | Coverage |
|------|----------|
| Crusader Kings III | Script, loc, events, health, wiki, publish |
| Victoria 3 | Same |
| Europa Universalis V | **Partial** — stage roots and `INJECT:` / `REPLACE:` |

## Download

Grab the latest **Windows** or **Linux** zip from [GitHub Releases](https://github.com/idodavis/paradox-modding-tools/releases). Then follow [Getting started]({{< relref "/getting-started" >}}).

Inside the app, the **version button in the status bar** checks for updates against those zips plus a `SHA256SUMS` sidecar.

## What's in the app

| Page | What it does |
|------|----------------|
| [Library]({{< relref "/pages/library" >}}) | Workspaces by game, the active one in full; New workspace, New mod; Reset all PMT data |
| [New workspace]({{< relref "/pages/wizard" >}}) | Game, install, mods in load order, details |
| [IDE]({{< relref "/pages/ide" >}}) | Monaco editors in groups, hover cards, checks, search and replace, git source control, the Wiki pane; game files read only |
| [Event Graph]({{< relref "/pages/event-graph" >}}) | What an event or on_action fires and what fires it |
| [Health]({{< relref "/pages/health" >}}) | Contests, overrides, dependencies, dangling refs, localization and script issues |
| [Patch Notes]({{< relref "/pages/patch-notes" >}}) | The game's patch notes from its wiki |
| [Publish]({{< relref "/pages/publish" >}}) | Listing, description, version and change note; Steam Workshop upload and compare; Paradox Mods copy |
| [Settings]({{< relref "/pages/settings" >}}) | The workspace's name, language, install, mods and load order; the app's appearance |

## Next

1. [Getting started]({{< relref "/getting-started" >}}) — install the zip and create a workspace
2. [Concepts]({{< relref "/concepts" >}}) — workspace, load order, loc, the three “docs”
3. [Limitations]({{< relref "/limitations" >}}) and [roadmap]({{< relref "/roadmap" >}})
4. [Develop]({{< relref "/develop" >}}) — build from source on Windows or Linux
