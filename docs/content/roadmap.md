---
title: Roadmap
description: Loose direction — Health-first patching, GUI preview, Event Graph pictures, CK3 map editor, IDE undock.
weight: 7
---

# Roadmap

This is **direction**, not a calendar. Order can change. File issues against a [roadmap bucket](https://github.com/idodavis/paradox-modding-tools/issues/new/choose) if you want to push on one of these.

## Done

- The language engine is rebuilt: each install's schema is learned from its own files and the game's `script_docs`, and the IDE, the Event Graph and Workspace Health all read one index.
- The [Event Graph]({{< relref "/pages/event-graph" >}}) and [Workspace Health]({{< relref "/pages/health" >}}) are redesigned: a graph with its detail beside or below it, macros shown on request; six cards over a sortable, filterable table with each row's definitions beside it.

## Automatic game-update patching

After a game patch today:

1. Reopen the workspace: the install is scanned again by itself.
2. Read [Workspace Health]({{< relref "/pages/health" >}}) — dangling refs and overrides.

Later: suggested **file-level retarget in the IDE** (open the mod's copy of a changed game file, copy forward what you still own).

## GUI preview

A future preview using the same Jomini CST already used for `.gui`. Goal is a **structural** HTML/CSS widget tree (`hbox` / `vbox` / `button`) so you can see nesting — not a pixel-perfect launcher clone.

## Event Graph pictures

An event node shows its background picture behind its kind bar. Where the picture comes from differs by game:

- CK3: `override_background`, or `theme` mapped to that background's variant with no trigger.
- EU5: `image`; `illustration_tags` is picked at runtime, so it has no picture to show.
- Vic3: none — its event pictures are Bink videos.

It needs a DDS decoder, which comes with this first use; its formats are surveyed then (CK3's scenes are DXT1 and DXT5).

## Map / title editor

**CK3 political layer first.**

- Read `provinces.png` (color-index) + `definition.csv` + `landed_titles` tree.
- Go builds a raster overlay; Vue is the canvas + inspector.
- Write `zz_pmt_map*.txt` plus loc (UTF-8 BOM) into a **workspace mod**.

The game’s `-mapeditor` still owns heightmap bake, locators, and splines. Vic3 `state_regions` and EU5 locations are later adapters on the same idea.

## Multi-window

Wails v3 can open extra windows. Today `main.go` creates one.

{{< mermaid >}}
flowchart LR
  subgraph now [Now]
    oneWin[One webview]
    oneSess[One live session]
  end
  subgraph later [Later]
    ideWin[IDE window]
    toolWin[Tool pages]
    pool[Pool: N sessions or share one]
  end
  oneWin --> ideWin
  oneSess --> pool
  pool --> toolWin
{{< /mermaid >}}

- **Same workspace, extra windows:** feasible (hash router + Pinia). **IDE undock** is the useful first step — each webview gets its own monaco-vscode workbench talking to the **same** Go session.
- **Two workspaces at once:** needs `EnsureSession` to keep more than one session live.
- Undocking Health / Graph / Publish is the same extra-window work once the pool rule is decided.

Target: **IDE undock first; full-app windows after the one-session rule is relaxed.**

## Game coverage

Deeper EU5 extract rules and Guide/wiki coverage. More loc languages in hover without a full rescan (today the install's cache is kept per language).
