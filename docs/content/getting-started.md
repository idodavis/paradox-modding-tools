---
title: Getting started
description: Install the Windows or Linux zip, create a workspace, and open the IDE.
weight: 2
---

# Getting started

{{< callout warning >}}
**macOS is not supported.** There is no Mac download and no documented Mac build path.
{{< /callout >}}


## 1. Download a zip

From [Releases](https://github.com/idodavis/paradox-modding-tools/releases) pick one file:

| File | Who it is for |
|------|----------------|
| `paradox-modding-tools-windows-amd64.zip` | Windows PCs, including Windows on ARM (Snapdragon, etc.), which runs it under x64 emulation |
| `paradox-modding-tools-linux-amd64.zip` | Linux desktops (x86_64) |

Extract anywhere you can write and run the binary. Steam Workshop publish uses the game's own copy of Steam's library.

## 2. Runtime on Windows

The app uses the WebView2 runtime. Windows 11 usually has it. On Windows 10, install [Microsoft Edge WebView2](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) if the window fails to open.

The zip is **unsigned**. SmartScreen or your AV may warn the first time — that is expected for this alpha.

## 3. Runtime on Linux

You need a traditional desktop session with **GTK 4** and **WebKitGTK 6** (the libraries Wails v3 links against). Package names vary:

| Distro family | Typical runtime packages |
|---------------|--------------------------|
| Debian / Ubuntu | `libgtk-4-1`, `libwebkitgtk-6.0-4` |
| Fedora / RHEL | `gtk4`, `webkitgtk6.0` |
| Arch | `gtk4`, `webkitgtk-6.0` |
| openSUSE | `gtk4`, `webkit2gtk-4.1` or the distro’s WebKitGTK 6 package |
| Gentoo | `gtk4`, `webkit-gtk` |

Steam Deck, Flatpak-only, and immutable images are unsupported. After extract:

```bash
chmod +x paradox-modding-tools
./paradox-modding-tools
```

If the window is blank, the WebKitGTK 6 runtime is the first thing to check.

## 4. Create a workspace

On first launch the [wizard]({{< relref "/pages/wizard" >}}) walks you through:

1. **Game** — CK3, Vic3, or EU5.
2. **Install** — detect a Steam/Paradox folder or browse. Pin a version from `launcher/launcher-settings.json`, or use `latest` (the live folder across patches).
3. **Mods** — attach existing folders and/or **Create new mod** (descriptor, empty `common/` and `events/`, readmes, a localization stub with UTF-8 BOM). Drag to set load order.
4. **Name** — how the workspace appears in the Library.

You land in the [Workspace IDE]({{< relref "/pages/ide" >}}).

{{< shot caption="Wizard Install step (detected path + version)." >}}

## 5. First scan

PMT indexes **vanilla** script into a cache keyed by install + version, then harvests your mods into RAM when the workspace opens. Large installs take a while the first time. Later opens reuse the vanilla cache.

The language strip (defs count / “index ready”) tells you the [live session]({{< relref "/concepts/workspace" >}}) is up. Tool pages wait for that.

Game files in the explorer are **read-only**. Edit in a mod folder.

## 6. Open the IDE and Guide

- Explorer roots are this workspace only: your mods, then the game.
- Search and Ctrl+P index every root.
- The **Guide** pull-tab on the editor’s right edge is the community wiki (search, contents, related pages). It stays closed until you open it.

{{< shot caption="IDE: explorer + script editor + Guide open." >}}

## After that

- [Workspace Health]({{< relref "/pages/health" >}}) — who wins FIOS/LIOS, missing loc, dangling refs.
- [Event Graph]({{< relref "/pages/event-graph" >}}) — hover an event id in the IDE and open the neighborhood.
- [Publish]({{< relref "/pages/publish" >}}) — listing + Steam (client must be running) or Paradox Mods copy-paste.

Later zips: click the **version number in the footer** to check for updates (needs `SHA256SUMS` on the GitHub Release).

Read [concepts]({{< relref "/concepts" >}}) if “workspace”, “install”, and “user data” are getting mixed up. [FAQ]({{< relref "/faq" >}}) covers scan stale, loc ignored, and Workshop staying private.
