---
title: Publish
description: Descriptor, Markdown/BBCode listing, Steam Workshop, Paradox Mods copy-paste.
weight: 7
---

# Publish

Edit the listing in the **mod folder** and publish to Steam Workshop or copy text for Paradox Mods.

The window title is **Publish**; the route name is `release`.

{{< shot caption="Listing editor + thumbnail." >}}

## What PMT writes

In the attached mod:

- Descriptor (CK3 `.mod` / `descriptor.mod`, or Vic3/EU5 `.metadata/metadata.json`)
- `mod-description.md` and `mod-description.bbcode` (paths overridable in Settings)
- `changelog/<version>.bbcode`

Description is Markdown in a rich editor. **Save** writes the Markdown file and converts to BBCode. Steam **always** uploads BBCode.

## Steam Workshop

{{< shot caption="Steam publish pane." >}}

- The Steam **client** must be running and signed in as the Workshop **owner**.
- Publishing loads the game's own copy of Steam's library from its install; nothing of Steam's ships with PMT. EU5 has no Linux build, so publish EU5 mods from Windows.
- Staging skips dotfiles (except `.metadata/`), `.gitignore`, patterns in `.gitignore`, and Workshop ignore patterns from [Settings]({{< relref "/pages/settings" >}}).
- **New items stay private** until you set them Public on Workshop yourself.
- Do not run a local copy and a Workshop subscribe of the **same** mod together — the game will see both.


## Paradox Mods

**Copy BBCode** for the launcher, or **Copy Markdown**. The site often strips formatting. The launcher still needs its own `.mod` / playset entry the way you already create mods.
