---
title: Publish
description: Descriptor, Markdown/BBCode listing, Steam Workshop, Paradox Mods copy-paste.
weight: 7
---

# Publish

Edit the listing in the **mod folder** and publish to Steam Workshop or copy text for Paradox Mods, in three steps: the listing and description, the version and change note, then review and upload.

The window title is **Publish**; the route name is `release`.

{{< shot caption="Listing editor + thumbnail." >}}

## What PMT writes

In the attached mod:

- Descriptor (CK3 `.mod` / `descriptor.mod`, or Vic3/EU5 `.metadata/metadata.json`)
- `workshop/mod-description.md` or `workshop/mod-description.bbcode` (**Change source** in Publish picks another file)
- `workshop/changelog.bbcode`, the latest change note, replaced on each save
- `workshop/previews/`: the Workshop page's extra screenshots, and `videos.txt`, its YouTube videos
- The Workshop id once the mod is uploaded: `remote_file_id` in CK3's `descriptor.mod`; for Vic3 and EU5, whose `metadata.json` has no such key, `workshop/steam-item.txt`

Description is Markdown in a rich editor. **Save** writes the Markdown file and converts to BBCode. Steam **always** uploads BBCode.

## Steam Workshop

{{< shot caption="Steam publish pane." >}}

- The Steam **client** must be running and signed in as the Workshop **owner**.
- Publishing loads the game's own copy of Steam's library from its install; nothing of Steam's ships with PMT. EU5 has no Linux build, so publish EU5 mods from Windows.
- Staging skips dotfiles (except `.metadata/`), `.gitignore`, `workshop/previews/` (Steam takes those as the page's gallery, not as mod files), patterns in `.gitignore`, and Workshop ignore patterns from [Settings]({{< relref "/pages/settings" >}}). The rest of `workshop/` ships with the mod unless you exclude it there.
- **Upload** works once every change is saved to the workspace, and sends the mod as saved.
- **New items stay private** until you set them Public on Workshop yourself; later uploads keep the visibility you set there.
- Do not run a local copy and a Workshop subscribe of the **same** mod together — the game will see both.

## Compare with Steam

**Compare** (on the listing step, by Steam's state, and the last line of Review's checklist) shows what an upload changes: the listing's fields, then the files, each beside Steam's copy. Steam's copy is the snapshot PMT kept at its last upload, or a download from Steam.

- Unchanged lines fold away; click a fold to open it, or turn on **Show all lines**.
- The arrows step through the changes. **Ctrl F** finds text in both copies; Enter and Shift Enter step through the matching lines.
- **Split** draws the two copies side by side, **Unified** in one column.


## Paradox Mods

**Copy BBCode** for the launcher, or **Copy Markdown**. The site often strips formatting. The launcher still needs its own `.mod` / playset entry the way you already create mods.
