---
title: Workspace IDE
description: monaco-vscode workbench with PMT-owned roots, read-only game files, and Guide.
weight: 3
---

# Workspace IDE

The IDE is a monaco-vscode-api workbench (Monaco + VS Code services) inside PMT chrome. Themes and scale live in the PMT header.

{{< shot caption="IDE: explorer + script editor + Guide open." >}}

## Folders

Roots come from **this workspace**: your mods, then the game files. PMT owns the folder set.

- **Game files** (CK3, Vic3, or EU5) are **read-only**. Edit in a mod folder.
- Search and Ctrl+P index every root.
- Deleting a **mod’s root folder** in the explorer also detaches it from this workspace. Files inside a mod delete normally.

## Tabs

Open editor tabs persist across workspace switch and app close unless you turn that off in [Workspace Settings]({{< relref "/pages/settings" >}}).

## Language features

On Paradox script, loc, and descriptors in **mod** folders:

| Feature | Notes |
|---------|--------|
| Hover | Kind, where it is defined (the kept definition and those it replaced; a saved scope, where its own script saves it), its label, its comment or the game's description, the scopes it runs in and hands on, what a field accepts; loc text; dates, colours and random-list weights |
| Complete / signature | The keys and names the session knows; an effect's or trigger's usage from `script_docs`, inside its own block |
| Go to definition / references | Across attached mods + vanilla |
| Symbols | The file's definitions, and a search over the workspace |
| Format / folding | Jomini CST |
| Diagnostics | Unresolved references, wrong value types, wrong scopes, duplicate definitions, required loc, loc BOM, header and folder, and missing files (off by default); each can be turned off in [Workspace Settings]({{< relref "/pages/settings" >}}) → Editor |
| Code actions | Add a missing UTF-8 BOM; create a missing localization key |
| Inlay hints | A date in the mod's own calendar era, where the mod has one |

Game files stay read-only, so diagnostics run on mods.

### `# pmt:ignore`

In a script comment:

```
# pmt:ignore
# pmt:ignore required-loc, missing-bom
```

That turns diagnostics off on the comment's own line and the next one. With codes, only those.

## Guide

The **Guide** pull-tab on the editor’s right edge opens a resizable **wiki** pane (search, contents, related pages). It stays closed until you open it.

This is the community wiki, not `script_docs` and not install `_*.info`. See [three kinds of docs]({{< relref "/concepts/docs" >}}).

Snooze or turn off the Guide toast under [Display]({{< relref "/pages/display" >}}).

{{< shot caption="Hover card on a def (kind, docs, origin)." >}}

## Event Graph from hover

Hover a definition the Event Graph can draw (an event, an on_action, or a definition that fires one) for **Open in Event Graph**.
