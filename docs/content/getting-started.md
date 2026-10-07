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

1. **Game**: CK3, Vic3 or EU5.
2. **Install**: one PMT found on this computer, or a folder you pick (the game's root, the one that holds
   `game/`).
3. **Mods**: **Add an existing mod** and **Create a new mod**, dragged into load order. You can leave it empty.
4. **Details**: the workspace's name and localization language.

The workspace opens in the [IDE]({{< relref "/pages/ide" >}}).

{{< shot caption="New workspace: the Install step." >}}

## 5. First scan

PMT indexes the game's script into a cache kept per install and version, then indexes your mods in memory when
the workspace opens. A large install takes a while the first time; the top bar shows the scan's stage and progress,
then the version and when it was scanned. Later opens reuse the cache.

Game files in the explorer are **read only**: edit in a mod folder.

## 6. Find your way in the IDE

- The explorer shows this workspace only: your mods, then the game's files.
- **Ctrl P** opens a file by name; **Ctrl Shift F** searches the files' text, and can replace.
- Rest the pointer on a word for its hover card; problems show as squiggles and in the Problems panel.
- The **Wiki** button at the end of the editor's tabs (**Ctrl Shift W**) opens the community wiki's pages for the file.
- **Source control** (the sidebar's third view) works on mods that are git repositories. It needs
  [git](https://git-scm.com/) installed.

{{< shot caption="IDE: explorer, a script file and the Wiki pane." >}}

## After that

- [Health]({{< relref "/pages/health" >}}): what the load order keeps (FIOS/LIOS), dangling references, localization
  and script problems.
- [Event Graph]({{< relref "/pages/event-graph" >}}): **Ctrl Shift G** on an event in the IDE draws what it fires
  and what fires it.
- [Publish]({{< relref "/pages/publish" >}}): the listing, then the Steam Workshop (the client must be running) or a
  BBCode copy for Paradox Mods.

Later versions: click the **version number in the status bar** to check for updates (it needs `SHA256SUMS` on the
GitHub Release).

Read [concepts]({{< relref "/concepts" >}}) if “workspace”, “install” and “user data” are getting mixed up. The
[FAQ]({{< relref "/faq" >}}) covers a scan that finds nothing, localization the game ignores, and a Workshop item
that stays private.
