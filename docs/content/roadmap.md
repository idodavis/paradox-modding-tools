---
title: Roadmap
description: Loose direction — Health-first patching, Event Graph pictures, CK3 map editor, IDE undock, a terminal and AI agents.
weight: 7
---

# Roadmap

This is **direction**, not a calendar. Order can change. File issues against a [roadmap bucket](https://github.com/idodavis/paradox-modding-tools/issues/new/choose) if you want to push on one of these.

## Done

- The language engine is rebuilt: each install's schema is learned from its own files and the game's `script_docs`, and the IDE, the Event Graph and Health all read one index.
- A new look across the app: seven theme families, each dark and light, that the pages and the editor share.
- The [IDE]({{< relref "/pages/ide" >}}) is rebuilt on Monaco: editor groups and splits, hover cards, search and replace, the Wiki pane, and git source control with a graph and merge conflicts resolved in the editor.
- The [Event Graph]({{< relref "/pages/event-graph" >}}) and [Health]({{< relref "/pages/health" >}}) are redesigned: a graph with its detail beside or below it; six cards over a sortable, filterable table with each row's definitions beside it.
- A `.gui` file has a Visual view in the [IDE]({{< relref "/pages/ide" >}}): its window drawn from the game's own types, templates, textures and fonts, with a Widgets panel beside it.
- [Patch Notes]({{< relref "/pages/patch-notes" >}}) reads the game's patch notes from its wiki.
- [Publish]({{< relref "/pages/publish" >}}) is a three-step flow: the description from Markdown, BBCode or Steam's own text, screenshots and videos imported from Steam, and a compare with what Steam has.

## Automatic game-update patching

After a game patch today:

1. Reopen the workspace: the install is scanned again by itself.
2. Read [Health]({{< relref "/pages/health" >}}): dangling refs and overrides.

Later: suggested **file-level retarget in the IDE** (open the mod's copy of a changed game file, copy forward what you still own).

## Event Graph pictures

An event node shows its background picture behind its kind bar. Where the picture comes from differs by game:

- CK3: `override_background`, or `theme` mapped to that background's variant with no trigger.
- EU5: `image`; `illustration_tags` is picked at runtime, so it has no picture to show.
- Vic3: none — its event pictures are Bink videos.

It needs a DDS decoder, which comes with this first use; its formats are surveyed then (CK3's scenes are DXT1 and DXT5).

## The window's own state in the Visual view

A window keeps state of its own while it is open, its variables and a panel's selected tab, which its GUI writes on a
click and reads in a `visible`: a tab shows its page, an expand button opens its panel. Today the Visual view draws
every one of those widgets at once. Later it plays that state, a click pressing what it lands on as in the game, built
as part of one foundation for what a window decides while it runs (its state, hovers, tooltips and animations) rather
than as a feature of its own, so that it covers every window that keeps state.

## Entries of any file

Today the Visual view picks the entry it draws from a select in its toolbar. Next, an Entries tab beside Source
control in the [IDE]({{< relref "/pages/ide" >}}) lists a GUI or coat of arms file's entries, searchable, each coat
with a picture of it; a click draws it. Later the same tab lists the entries of any script file, its events,
decisions, traits and the rest, grouped by kind, each with its name in your language from its localization; a click
opens it in the code.

## Map / title editor

**CK3 political layer first.**

- Read `provinces.png` (color-index) + `definition.csv` + `landed_titles` tree.
- Go builds a raster overlay; Vue is the canvas + inspector.
- Write `zz_pmt_map*.txt` plus loc (UTF-8 BOM) into a **workspace mod**.

The game’s `-mapeditor` still owns heightmap bake, locators, and splines. Vic3 `state_regions` and EU5 locations are later adapters on the same idea.

## Multi-window

Same-workspace windows are in (see [Windows]({{< relref "/pages/ide#windows" >}})): a page, an editor, the game's
files or the panel in a window of its own, all on the one live session. What is left is two workspaces at once.

{{< mermaid >}}
flowchart LR
  subgraph now [Now]
    wins[Several windows]
    oneSess[One live session]
  end
  subgraph later [Later]
    pool[Pool: N sessions]
    twoWs[Two workspaces at once]
  end
  wins --> oneSess
  oneSess --> pool
  pool --> twoWs
{{< /mermaid >}}

- **Same workspace, extra windows:** done. Each window has its own Monaco editors talking to the **same** Go session.
- **Two workspaces at once:** needs `EnsureSession` to keep more than one session live.

Target: **two workspaces at once, after the one-session rule is relaxed.**

## Terminal and AI agents

**First release: a terminal in the IDE's panel.** Run git, or a command-line agent such as Claude Code or GitHub
Copilot CLI, beside the editor. The editor follows what the agent does: a file it changes reloads in its tab, a
`path:line` it prints opens at that line, a selection can be sent to it as a file reference, and source control
shows its changes as diffs to keep or discard. A change on disk to a tab with unsaved text is flagged with a
compare, never lost.

Later, in this order, as the pieces mature:

1. **PMT's knowledge for agents.** An MCP server so an agent can ask PMT the problems in a file, where a name is
   defined, what is valid at a position and what Health finds, and check the Paradox script it writes against the
   game's own schema. MCP is the one interface both Claude Code and Copilot CLI support for outside tools.
2. **The agents' editor bridges.** The selection shared live and the agent's edits opened as diffs in the editor,
   as Claude Code and Copilot CLI do in VS Code. Both bridges are undocumented today; this waits on their
   documentation.
3. **Agent chat and tab completion in the editor.** Copilot and Claude in one chat panel through the Agent Client
   Protocol (ACP), the open standard Zed, JetBrains and Neovim use: plans, edits as diffs you accept, commands run
   in PMT's terminal; and Copilot's tab completions alongside. Waiting on ACP's second version, which changes how
   edits and permissions are described.

## Game coverage

Deeper EU5 extract rules and wiki coverage. More loc languages in hover without a full rescan (today the install's cache is kept per language).

## Game Log Reader

A new page or extension of Health that allows you toimport your game log files and it gives a dashboard/report on what errors were found, which mods are likely culprit, and a detailed map/grouping of relevant information output by the logs outside of errors.