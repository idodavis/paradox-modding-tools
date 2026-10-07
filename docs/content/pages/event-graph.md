---
title: Event Graph
description: What an event or on_action fires, and what fires it, drawn from the open workspace.
weight: 4
---

# Event Graph

What an event or on_action fires and what fires it, drawn from the open workspace: the game's files and your mods.

{{< shot caption="Event Graph: a node selected, its route lit, and its detail beside the graph." >}}

## Picking the root

Type any part of an id, a namespace or a number in **Root**. The hits are grouped by kind (events, on_actions,
decisions and the other kinds that fire events), each with its origin; ids that start with what you typed come
first. Pick one to draw the graph from it.

From the [IDE]({{< relref "/pages/ide" >}}): **Ctrl Shift G**, or right-click and **Open in Event Graph**, on an
event or anything else the graph can draw; the command palette has **Open the event under the cursor**.

## The toolbar

- **Filters** (its badge counts what differs from the defaults):
  - **Kinds**: which kinds of object become nodes. With **scripted effect** off, a scripted effect between two
    nodes is the label of the edge between them instead of a node;
  - **Show nodes from**: the game and the mods; an origin left out hides the nodes only it defines;
  - **Depth** (1 to 10, 3 by default) and **Node limit** (10 to 400, 120 by default). Past six fires from one node,
    the rest fold into a **+N more** group, which opens when you click it.
- **Follow macros** (on by default) walks into scripted effects and triggers to find the events they fire; off,
  only direct fires are drawn.
- How many nodes are shown, **Fit to view**, **Collapse all** (closes every group you opened) and **Full screen**
  (**Esc** leaves it).
- **Detail below** or **Detail beside**: where a node's detail opens. Full screen always puts it beside.

## The graph

Laid out left to right by step from the root, with a head over each column ("Fired by", "1 step", "2 steps" and so
on). Events are cards with their title; on_actions are filled pills, other kinds outlined pills; each has its
origin's color. Zoomed out below 70%, cards show only their ids.

- **Click** a node to open its detail: the routes from the root to it and its neighbors stay lit, the rest fades,
  and its edges are labeled with the scripted effects they pass through. Click empty space to clear it.
- **Double-click** a node to draw the graph from it.
- **Click** a **+N** group to open it.

The status bar counts the nodes, says where the node limit stopped the graph, and how many groups are folded.

## The detail

A header with the node's kind, origin, file and line, **Set as root** and **Open in IDE**. Drag its edge to resize
it. Three tabs:

- **The kind** (Event, On action, …): its type and one-word properties, its text in the workspace's language
  (with each version when there are several, and when each shows), portraits, and its options with their text and
  conditions;
- **Links**: what it **fires** and what it is **fired by**, each directly or through scripted effects (click a name
  to draw the graph from it); then what it **reads**, by kind, each name opening the IDE at its first use;
- **Script**: the definition as its file writes it, with **Copy the definition** and **Open in IDE**.
