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
| Crusader Kings III | Script, loc, events, health, Guide wiki, publish |
| Victoria 3 | Same |
| Europa Universalis V | **Partial** — stage roots and `INJECT:` / `REPLACE:` |

## Download

Grab the latest **Windows** or **Linux** zip from [GitHub Releases](https://github.com/idodavis/paradox-modding-tools/releases). Then follow [Getting started]({{< relref "/getting-started" >}}).

Inside the app, the **version button in the footer** runs Check for updates against those zips plus a `SHA256SUMS` sidecar.

## What's in the app

| Page | What it does |
|------|----------------|
| [Library]({{< relref "/pages/library" >}}) | Workspaces by game; open / edit / delete the PMT record; New Workspace / New Mod; Reset all data |
| [Create Workspace]({{< relref "/pages/wizard" >}}) | Game → install → mods + load order → name |
| [Workspace IDE]({{< relref "/pages/ide" >}}) | monaco-vscode workbench; PMT-owned roots; game files read-only; Guide wiki pane |
| [Event Graph]({{< relref "/pages/event-graph" >}}) | Event / on_action neighborhood from the live session |
| [Workspace Health]({{< relref "/pages/health" >}}) | Contests, overrides, dependencies, dangling refs, localization and script issues |
| [Patch Center]({{< relref "/pages/patch-center" >}}) | Cached wiki patch notes |
| [Publish]({{< relref "/pages/publish" >}}) | Descriptor + Markdown/BBCode, Steam Workshop, Paradox Mods copy-paste |
| [Workspace Settings]({{< relref "/pages/settings" >}}) | Name, loc language, install, mods, Workshop ignore |
| [Display]({{< relref "/pages/display" >}}) | Header: themes, scale, fonts, visible tools |

## Next

1. [Getting started]({{< relref "/getting-started" >}}) — install the zip and create a workspace
2. [Concepts]({{< relref "/concepts" >}}) — workspace, load order, loc, the three “docs”
3. [Limitations]({{< relref "/limitations" >}}) and [roadmap]({{< relref "/roadmap" >}})
4. [Develop]({{< relref "/develop" >}}) — build from source on Windows or Linux
