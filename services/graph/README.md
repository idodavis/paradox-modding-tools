# graph

The package is the Event Graph page's service. It builds the graph of what fires what from one root, and the detail of
one node.

What fires what is the index's: `index.ListFires` and `index.ListFiredBy` give the firings, and `index.FindKept` the
definition the game keeps of a name. This package adds the reach from a root by steps, the folds, the splice of hidden
nodes, where each firing sits in its definition, the rows a node shows open, an event's picture, and the detail's texts,
rules and copy. The graph has no coordinates; the page lays it out with ELK and draws it with Vue Flow.

| file | holds |
|---|---|
| `graph.go` | the package comment; `Service`; `Node`, `Edge`, `Row`, `Filter`, `Graph`; `BuildGraph` and its `reach`; `FindRoots`, `ListOwnRoots`, `Defaults` |
| `steps.go` | `Step`; the edge of a firing: its site, the statement that writes it, the blocks around it, its delay, the effect that writes it and its title |
| `conditions.go` | `Condition`; a block of triggers in words |
| `pictures.go` | an event's picture, found by a chain of definitions that ends in an image file |
| `detail.go` | `Detail`, `Section`, `Version`, `Rule`, `RuleOption`; `BuildDetail` |

## Nodes and edges

A `Node` is an event, an on_action, a decision, a message or a scripted effect, with where it is written, its title and
its step from the root, a firer's step negative. A firer of any other database is a node too, such as a character
interaction. Nothing fires it, so it has no firers of its own; drawn as the root, it shows what it fires. A node's id is
`database:name`, and a name has one node, which counts once toward the limit.

A node is made of every copy of its name that acts (`index.ListActing`): the one the game keeps and those it merges
into it, or, where no source says which copy the game keeps, every copy in a file the game reads. Its firings are theirs
together. It is shown at the copy the game keeps, or at the first copy the graph meets where no source says which.
Where the game keeps no copy for every file, the node is the copy a file keeps for itself, such as a scripted effect an
events file defines.

A node says how the player sees it: an event shows a window or is hidden (`game.HiddenKey`), and a message is sent (the
database `schema.Schema.Messages` names). An event's node holds its background picture, found by the `Picture` chain of
the game's `Events` row. At each hop of the chain, of several copies of a key the first with no trigger of its own wins,
or the one a `key[field=value]` names (EU5's backdrop, the layer of a scene on `background2_mesh`). An on_action says
whether the game calls it from its own code (the dumps' "From Code: Yes"). Every node holds the comment lines just
above its definition and the origin of the copy of its name it hides. In a graph and a detail, a node holds every firing
it makes, `Fires`, in the order written. A drawn node holds its own trigger in words, `Trigger`.

In a graph, a node also holds the `Row`s it shows open, `Rows`, in the order its firings are written. Firings that
blocks decide (a branch of an if, a weighted branch or a list), written inside the blocks at the same lines, are one row
with the last deciding block as its `Step`. A firing written plainly is a row of its own. A row that a weighted branch
or a list decides inside a branch of an if sits under a header row for that branch (`Nested`, `Header`). Past the
fan-out the rest are one fold row, whose `More` counts them, unless `Expand` opens the node's fold.

A fold is a node too, with the id `more:<id>` or `more-in:<id>`. It stands for the firings of one node past the
filter's fan-out, and `Count` says how many.

An `Edge` is one firing, from a firer to what it fires. It holds where it is written (`Site`), its delay in words
("7 days", "10–30 days"), the effect that writes it and that effect's title. Its `Steps` are the blocks around it from
the top of the definition: a branch of an if's chain, a weighted branch, a list, a link to another scope, an option or
another block. Each has its condition in words and, for an option, its name. A firing written as an entry that an
on_action's `random_events` picks by weight has that entry as its last step.

A condition in words is a block's limit, else its trigger, each statement a `Condition`. A keyword of the language
reads its `game.Keyword.Says` ("any of", "any province where"), and one that `Covers` what it holds reads its text as
the player does (a `custom_tooltip`). A key that names a scope, or a parameter that stands for one, stays as written as
the condition's `Subject`. Any other key is its words, a yes after it dropped and a no making it `Negated`. Each
operator reads its words ("more than", "at least"), but an = reads "is" only between a scope and a value, and a ?=
before a block reads "if it exists". A value stays as written, with `Title`, the in-game name of the definition a
`prefix:name` value names. A keyword marked `ValueFirst` makes the value the subject (`exists = X` reads "X exists").
A block that only wraps one condition folds into it: a negation (`game.Keyword.Negates`) makes it `Negated`, and a scope
makes itself its subject ("culture has culture group …"). A negation around several stays a group, since moving its
lines would change what they claim.

## The reach

`BuildGraph` starts at the root and goes out by steps, forward through what each node fires and backward through what
fires it, under a `Filter`:

- **Depth** stops the reach at a number of steps. A node the depth stops at that links further is marked `Deep`, and
  the filter's `Expand` opens it to go on.
- **Fan-out** draws a node's first firings and folds the rest into one fold, which `Expand` opens.
- **Limit** caps the nodes. A graph the limit cuts is `Truncated`, and `Total` says how many the reach found.
- **Databases** and **Origins** say which nodes are drawn. Databases picks among the kinds `Defaults` lists, so a firer
  of any other database is drawn whatever it holds; Origins applies to every node. A node the filter hides is spliced
  out: the edges into and out of it become one edge through it, with its name in `Via`, keeping the first firing's
  delay and blocks, and a node whose parent is hidden takes the nearest drawn one. A deep mark on a hidden node passes
  to the nearest drawn node before it. The root and the folds are never hidden.
- **Follow** follows the scripted effects a firing passes through, so a firing through an effect counts. Its blocks
  are those inside the last effect.

`Defaults` is the filter of a new graph: the databases that are fired (`schema.Schema.FireDatabases`) and the scripted
effects, from the game and every mod, with every fold closed. `FindRoots` finds the nodes whose name holds a query, the
exact name first, then the names that start with it, then the rest. `ListOwnRoots` lists the events the workspace's
mods define that the game keeps, for the page's start.

## The detail

`BuildDetail` reads the definition the game keeps of a node. A `Detail` holds the node, its source and a `Section` for
each key at its top, the options apart; a block's value is left to the source. A section that shows a text has its
`Version`s: each text it can show in the workspace's language, else the base language, with the trigger that decides
it in words and its pieces (`loc.Segments`), each data call made its words. A call that names a customizable
localization reads as the texts it can show, joined by slashes ("daughter/son", `game.CustomTexts`). A version whose
key has no message in the detail's language is `Missing`, and shows the base language's text.

The detail also holds what fires the node, the node at the other end of each firing, and the game rules it reads with
their settings' texts (`game.GameRules`). Where the definition hides a copy of its name, the detail holds that copy's
node.

## Terms

- **Node**: one node of the event graph: the kept definition of an event, an on_action, a decision, a message, a
  scripted effect or another entry that fires one, or a fold.
- **Fold**: a node that stands for the firings of one node past the fan-out.
- **Edge**: one firing, from a firer to what it fires.
- **Row**: one line of an open node: the firings written in the same blocks, a header for a branch, or the fold.
- **Step**: one block a firing is written inside, from the top of its definition.
- **Condition**: one statement of a block of triggers in words, with the statements of its block under it.
- **Filter**: what the graph shows and how far it reaches from its root.
- **Graph**: the event graph from one root, without coordinates.
- **Deep**: what a node is when the depth stopped at it and it links further.
- **Display**: how the player sees an event or a message: a window, hidden, or a message.
- **Copy**: the latest-loaded copy of a name that the definition the game keeps hides.
- **Detail**: what the page shows about one node.
- **Section**: one key at the top of a definition, as the page shows it.
- **Version**: one text a key can show, with the trigger that decides it.
- **Rule**: one game rule a definition reads, with its settings.
