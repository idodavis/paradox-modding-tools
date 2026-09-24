---
title: Event Graph
description: What an event or on_action fires, and what fires it, from the live session.
weight: 4
---

# Event Graph

See what an event or on_action fires and what fires it, drawn from the **live** workspace.

{{< shot caption="Graph with a node selected and its detail beside it." >}}

## How to use it

1. **Root:** type any part of an id, its namespace or its number, and pick one. The **kind** select narrows the search to one kind (events, on_actions, decisions and the other kinds that fire events), with how many each has.
2. **Depth**, **Fanout** and **Nodes** set how many hops the graph reaches, how many fires one node shows before the rest become a "+N more" stub, and how many nodes it draws. When the node cap cuts the graph, a warning says at what depth; raise **Nodes** to go deeper.
3. The **origins** select limits the graph to the game and the mods you pick.
4. **Show macros** draws each scripted effect or trigger on a path between two events as a node of its own. Off, an edge carries the first one as its label.
5. **Click** a node to open its detail and light its route from the root; everything else dims. **Double-click** a node to draw the graph from it. Click a "+N" stub to open it; **Reset expands** closes them again. **Fit** shows the whole graph.

The graph draws events, on_actions and the definitions that fire them. Scopes are never drawn.

## The detail

A node opens in a drawer beside the graph, or, with **Detail below**, in a panel under it; the choice is kept. Drag the drawer's left edge or the panel's top edge to resize it.

- **Overview:** the definition's one-word properties, its text (each loc key's text, quoted; hover a name for its key), its options, and how many definitions it fires, how many fire it, and how many kinds of name its block uses.
- **Script:** the definition as its file writes it.
- **Links:** what it fires and what fires it, each with the scripted effects and triggers between, as the graph draws them (click one to draw the graph from it); then every name its block uses, by kind (click one to open it in the IDE).

The ⓘ beside each heading says what the section holds.

## From the IDE

Hover an event id in the [IDE]({{< relref "/pages/ide" >}}) (its definition or a `trigger_event`) and choose **Open in Event Graph**. The link shows on any definition the graph can draw.
