---
title: Limitations
description: Alpha limits — platforms, one session, EU5 coverage, unsigned zips.
weight: 6
---

# Limitations

PMT is **alpha**. Expect bugs. Back up your mods. A green Health table is not a guarantee the playset loads in-game.

## Operating systems

- **Windows** 10/11 (amd64; Windows on ARM runs it under x64 emulation) and **Linux** amd64 on traditional desktops (Debian/Ubuntu, Fedora/RHEL, Arch, openSUSE, Gentoo — GTK4 + WebKitGTK 6).
- **macOS is not supported.**
- Linux **arm64** is not a release target.
- Workshop publish loads the game's own copy of Steam's library from its install, so it needs the game installed; EU5 has no Linux build, so EU5 mods publish from Windows only.
- Steam Deck, Flatpak, AppImage, and immutable distros are unsupported.

## One live workspace

Opening a workspace drops every other live session. [Roadmap]({{< relref "/roadmap" >}}) multi-window depends on relaxing that.

## Game coverage

- CK3 and Vic3 are the well-trodden paths.
- **EU5 is partial**: its stage folders and entry modes (`INJECT:`, `REPLACE:`) are read; folder and effect coverage is thinner than CK3's.

## Binaries and updater

- Zips are **unsigned**. Windows SmartScreen will warn.
- Windows needs **WebView2** (Win11 usually has it).
- Linux needs the **WebKitGTK 6** runtime Wails uses — a missing `.so` looks like a blank window.
- The status bar's **version button** verifies against a Release asset named `SHA256SUMS` plus the platform zip (`paradox-modding-tools-windows-amd64.zip`, `…-linux-amd64.zip`).

## Source control

The IDE's source control runs the **git installed on your computer**; without git on the PATH it is off. Git never asks for a password inside PMT: set up a credential helper or an SSH key for pulls and pushes that need one.

## Steam Workshop

Publish runs a second copy of PMT, which loads the game's own copy of Steam's library from its install; the Steam client must be running. New items stay private. See [Publish]({{< relref "/pages/publish" >}}).
