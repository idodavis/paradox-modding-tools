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
- **EU5 is partial** — stage roots and `INJECT:` / `REPLACE:` exist; folder and effect coverage is thinner than CK3.

## Binaries and updater

- Zips are **unsigned**. Windows SmartScreen will warn.
- Windows needs **WebView2** (Win11 usually has it).
- Linux needs the **WebKitGTK 6** runtime Wails uses — a missing `.so` looks like a blank window.
- The footer **version button** verifies against a Release asset named `SHA256SUMS` plus the platform zip (`paradox-modding-tools-windows-amd64.zip`, `…-linux-amd64.zip`).

## Steam Workshop

The helper is embedded and extracted to user-config `bin/`. Steam client must be running. New items stay private. See [Publish]({{< relref "/pages/publish" >}}).
