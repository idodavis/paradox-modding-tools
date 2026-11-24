# atlas

[Architecture](../../docs/ARCHITECTURE.md) › [Services](../../docs/ARCHITECTURE.md#the-services) › atlas

The atlas service answers the Atlas page: what a definition of the game is tied to, drawn as one canvas of nodes and
edges, with the detail of a node and the lists the page offers. It keeps nothing between calls.

It reads no rule of the games on its own. What fires what, which copy of a name the game keeps and which definitions
a key links are the [index](../../internal/index/README.md)'s. What a statement means is
[lang](../../internal/lang/README.md)'s walk. A definition's title, picture and painting, a text as one line and a
script value as a number are the [workspace](../workspace/README.md) session's. Atlas chooses what to draw of a
subject, words its amounts and conditions, and sends the page an answer it renders as it comes. The rules every
answer keeps are in the package's doc.

**Uses** [files](../../internal/files/README.md), [game](../../internal/game/README.md),
[index](../../internal/index/README.md), [jomini](../../internal/jomini/README.md),
[lang](../../internal/lang/README.md), [loc](../../internal/loc/README.md), [lsp](../lsp/README.md),
[workspace](../workspace/README.md) · **Used by** [mcp](../mcp/README.md).

## One answer for every canvas

```mermaid
flowchart TD
  subject["A node's id, database:name"] --> kept["The copy the game keeps"]
  kept --> what{"What it is"}
  what -->|"event, on_action, decision,<br/>message, scripted effect"| reach["reach.go: what fires it<br/>and what it fires"]
  what -->|"good, category"| good["reach.go: who makes it<br/>and who takes it"]
  what -->|"building, production method"| building["building.go: the line,<br/>its methods, its goods"]
  what -->|"technology"| tree["tree.go: its tree"]
  reach & good & building & tree --> graph(["Graph"])
```

`BuildGraph` answers every subject with a `Graph`: the nodes, each with its distance from the subject and the drawn
node one step nearer, and the edges between them. The page lays it out; the answer has no coordinates.

- **An event's graph** follows the index's firings both ways, a step at a time, to the filter's depth and limit.
  A node past the fanout folds into a `more:` node, firers that come round a loop into a `loop:` node, and a firing
  back into its own chain is a `back:` marker. Each edge carries its delay and the blocks the firing sits in, with
  their conditions in words.
- **A good's canvas** has the building lines that make it on the left, with the goods they take one step further
  out, and the lines and needs that take it on the right. A side of more than twelve draws a node for each widest
  group, which the filter's `Expand` opens. A heading over each column comes as a `Column`. A category of goods
  has its goods as its `Members`.
- **A building's canvas** is the first level of its line, with the line as a table (`Line`): a column for each
  level, a row for each thing the levels say, the production method groups and what the picked methods come to.
  The goods it takes and makes stand beside it as pills, and the technologies that unlock it in a column of their
  own, one step further out.
- **A technology's canvas** is its tree: every technology with what it requires, gives and unlocks, the game's
  trees and eras, and how many technologies the subject needs and leads to. CK3's innovations require nothing of
  each other, so its trees come together as one grid. An EU5 age draws the advances every country can have, those
  of the subject's own audience and those of the audience the filter picks.

`BuildDetail` gives one node's detail: the facts under its name, what it gives, its texts by the conditions that
choose them, what it fires and what fires it, every link it has, the game rules it reads, the copy it replaces and
its script. A building's detail also carries the rows and the production methods of that one level, and keeps
among what it gives only what holds under a condition. A description the definition does not write is the one the
game builds from its name.

## Lists

`ListCatalogs` names the catalogs the open game has, and `ListCatalog` lists one's rows under their groups, or the
rows a search matches. `ListOwnRoots` lists what the workspace's mods define. `FindRoots` is the agent's search over
everything Atlas draws. `Defaults` is the filter a canvas starts with.

## Words and numbers

A canvas says amounts and conditions the way the game does, and each is worded once, in Go.

- A text is the one line a player reads (`workspace.Session.FlattenText`). A part the game fills in at run time is
  an ellipsis.
- A number is written with a comma between each three digits and two decimals at most. Several amounts of one
  fact are a range, as "60–465"; a share of what is produced is a signed percentage. A time in days is said in
  years, months or days.
- A Vic3 production method's amount below zero takes away from what the other groups give, so it lowers the least
  of a range by the most its group takes away.
- A modifier is named and formatted as the game's own format for it says.
- A condition is its trigger in words, with the operator as the language's word and a negating block said as
  "not". A tooltip the script gives stands for the block it explains.

## What the games write, measured

The rows Atlas reads beside its functions were measured on the installs of the three games:

- A plain fact is a key at the top of a definition, said by its key in words. `factRows` holds the keys the games'
  own notes document for a good, a building, an era and a group.
- A group written as a plain value has no definition: Vic3's and EU5's `category` of a good, CK3's `type` of a
  district and `group` of an innovation, Vic3's `category` of a technology (`writtenGroups`).
- Only EU5's advances are for an audience. An advance's `potential` names a country, a culture or a religion,
  itself or through a scripted trigger; one that names nobody stands under "Other conditions".
- Where a good is found: CK3 lists the counties under each good, and for the other two games the places that name
  the good are counted (`foundPlaces`).

Whether a later game version adds a key these tables lack is not known; a key no row names is not shown.

## Files

| file | what it holds |
|---|---|
| `atlas.go` | The service, the canvas's types, `BuildGraph` and `Defaults`, and what a node says of its definition. |
| `reach.go` | The walk from a subject: an event's firings with their folds and loops, and a good's makers and users. |
| `building.go` | A building's canvas: its line as a table, its production methods and what the picked ones come to. |
| `tree.go` | A technology's tree: the trees and eras, what each technology requires, and whom an EU5 advance is for. |
| `conditions.go` | A trigger in words, and a firing's blocks, delay and weight. |
| `amounts.go` | What a definition gives, and how a number, a range, a modifier and a time are worded. |
| `detail.go` | `BuildDetail`: a node's facts, texts, links, game rules and script. |
| `catalogs.go` | The catalogs and their groups, the search, and what the mods define. |

## Terms

- **Subject**: the definition a canvas is about, by its node's id.
- **Node**: one definition on a canvas or in a list, or a node that stands for several: a fold, a group or a
  loop's marker.
- **Edge**: one firing or one link between two nodes.
- **Filter**: what a canvas draws: the databases, origins, depth and limit of an event's graph, the folds and
  groups opened, the era things must be available by, an EU5 audience, and the production methods picked.
- **Graph**: the answer of every canvas: the nodes and edges around a subject.
- **Column**: the heading over the nodes one distance from the subject on a good's or a building's canvas.
- **Line**: a building's levels, first to last, as a table.
- **Tie**: what one building line has to do with a good: how it makes it or how much it takes.
- **Fact**: one thing a definition says of itself: a label, its text and, where it has them, where and when it
  holds.
- **Condition**: one trigger in words, with the conditions inside it.
- **Step**: one block a firing sits in, with its conditions.
- **Tree**: one of a game's technology trees.
- **Audience**: whom an EU5 advance is for: a country, a culture, a religion or one of their groups.
- **Catalog**: one list the page offers, of every definition of some types.
- **Detail**: everything the detail pane shows of one node.
