---
title: Publish
description: A mod's listing, description, version and change note, saved in the mod; uploaded to the Steam Workshop or copied for Paradox Mods.
weight: 7
---

# Publish

Edit a mod's listing in its own folder, then upload it to the Steam Workshop or copy its description for Paradox
Mods. Nothing is uploaded until the last step.

{{< shot caption="Publish: the mod rail, the state bar and the listing step." >}}

## The page

**Mod**, on the left, lists the workspace's mods, each with its Steam state: **Not published**, or the version on
Steam (**v2.3.1, on Steam**). Whether an upload changes anything shows on Review and upload. Under it, the three
steps:

1. **Listing and description**: what players see;
2. **Version and change note**: what this upload is;
3. **Review and upload**: check, then send to Steam.

The bar at the top shows the mod in three places: **here, not saved** (your edits), **saved in the workspace**
(the mod's files) and **on Steam**. **Save to workspace** writes your edits into the mod, **Discard changes** drops
them; Review opens once nothing is unsaved. **Compare** shows what an upload would change (see below).

## Listing and description

- **Name** and **Tags**. The tags go into the descriptor only: they are not sent to Steam as Workshop tags.
- **Thumbnail**, shown in the launcher and on Steam: **Change** copies a PNG, GIF or JPEG into the mod as
  `thumbnail.<ext>` (at the mod's root for CK3 and Vic3, in `.metadata/` for EU5) and names it in the descriptor,
  at once.
- **Screenshots and videos**, on Steam only, up to 10: **Add image** (PNG, JPEG, GIF, WebP), **Add a YouTube link**,
  and **Remove**. Once the mod is on Steam, **Import from Steam** downloads the images and videos it has that the mod
  lacks (Steam must be running). They are kept in `workshop/previews/` and saved at once.
- **Description**: where it comes from, and **Change source**:
  - a **Markdown** file (`workshop/mod-description.md` by default), edited in a rich editor and converted to BBCode
    when you upload;
  - a **BBCode** file (`workshop/mod-description.bbcode`, or any `.bbcode` or `.txt` in the mod), edited as text and
    uploaded as written;
  - **Steam's own text**: uploads leave Steam's description as it is.

  With no choice made, PMT uses the Markdown file if there is one, else the BBCode file, else Steam's text. A file
  that is not in the mod yet has **Create**. **Steam preview** shows the description as Steam will, and **BBCode**
  what a Markdown file becomes, with what the conversion drops (tables, task lists, HTML, nested lists flattened,
  images without a public web address).

## Version and change note

- **Version**, with what Steam has, and buttons for the next minor and the next hotfix version;
- **Supported game version**, with the installed game's;
- **Change note**, shown in Steam's change log. **Draft** starts one.

## What Save writes in the mod

- The descriptor: CK3's `descriptor.mod`, or Vic3's and EU5's `.metadata/metadata.json` (its short description, when
  empty, is the Markdown's first paragraph);
- the description file, when it exists;
- `workshop/changelog.bbcode`, the change note until the next one (an empty note deletes it);
- after an upload, the Workshop item's id: `remote_file_id` in `descriptor.mod`, or `workshop/steam-item.txt` for
  Vic3 and EU5.

## Review and upload

Pick **Steam Workshop** or **Paradox Mods**. **Before you upload** checks the mod: its version against Steam's, the
supported game version, the thumbnail's size (Steam takes up to 1 MB), dangling references (**Review in Health**),
how many files go up after exclusions (**Edit upload exclusions**), what happens to Steam's description and to its
screenshots and videos, and how much differs from Steam (**Compare with Steam**).

### Steam Workshop

**Upload** sends the mod as saved, its name, the change note, the thumbnail, and the description unless its source
is Steam's own text. The mod's screenshots and videos replace Steam's; a mod with none leaves Steam's as they are.

- The Steam **client** must be running and signed in to an account that owns the game; Review says so when it is
  not, with **Check again**. While it uploads, Steam's stage and progress show. PMT uses the game's own copy
  of Steam's library; nothing of Steam's ships with PMT. EU5 has no Linux build, so publish EU5 mods from Windows.
- A new item is **private** until you make it public on its Workshop page; later uploads keep the visibility you set.
  If Steam needs you to accept the Workshop agreement first, PMT links to it.
- Left out of the upload: dot-named files and folders (but `.metadata/`), `.gitignore` files, `workshop/previews/`,
  the patterns in the mod's top `.gitignore`, and the mod's **Upload exclusions** in
  [Settings]({{< relref "/pages/settings" >}}) (which can import and export a `.workshop-ignore`). The rest of
  `workshop/` goes up with the mod unless you exclude it.

### Paradox Mods

**Copy the description as BBCode**, then upload the mod through the Paradox Launcher (**Upload Mod**, pick the mod
and Paradox Mods) and paste it. The site often drops line breaks; screenshots and the thumbnail are added there.
**Save the mod as a zip** writes what an upload sends (the exclusions applied), for uploading on the Paradox Mods
site.

## Compare with Steam

**Compare** (in the bar, on the first two steps) and **Compare with Steam** (in Review) show what an upload changes:
the listing's fields (title, description, tags, version, thumbnail, screenshots and video), then every file, added,
modified or deleted, with how many lines. The workspace side is what is saved, not your unsaved edits.

Steam's side is the copy PMT kept at its last upload, or, with none, **Download from Steam** (Steam must be
running). Unchanged lines fold away (**Show all lines** opens them), the arrows step through the changes, and
**Ctrl F** finds in both sides. **Split** draws them side by side, **Unified** in one column.
