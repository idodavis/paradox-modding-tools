# graph

The package is the Event Graph page's service. It builds the graph of what fires what from one root, and the detail of
one node.

What fires what is the index's: `index.ListFires` and `index.ListFiredBy` give the firings, and `index.FindKept` the
definition the game keeps of a name. This package adds the reach from a root by steps, the folds, the nodes the filter
hides spliced out, the delay written beside a firing, and the detail's texts and pictures. The graph has no
coordinates; the page lays it out with dagre and draws it with Vue Flow.

| file | holds |
|---|---|
| `graph.go` | the package comment; `Service`; `Node`, `Edge`, `Filter`, `Graph`; `BuildGraph` and its `reach`: the steps out from the root, the folds, the deep marks, the splice of hidden nodes; the delay of a firing; `FindRoots`, `Defaults` |
| `detail.go` | `Detail`, `Section`, `Version`; `BuildDetail`: a node's text, its sections and options, its firings, its references, its portraits and its background |

## Nodes and edges

A `Node` is the definition the game keeps of an event, an on_action, a decision or a scripted effect, with where it is
written, its title and its step from the root, a firer's step negative. A firer of any other database is a node too,
such as a character interaction, labeled by its database's word as the others are. Nothing fires it, so it has no
firers of its own; drawn as the root, it shows what it fires. Its id is `database:name`. A node is made of every
definition the game merges under its name, so its firings are theirs together. A fold is a node too, with the id
`more:<id>` or `more-in:<id>`: it stands for the firings of one node past the filter's fan-out, and `Count` says how
many. An `Edge` is one firing the graph draws, from a firer to what it fires, with the delay written for it (`days`,
`months` or `years`, beside the firing or in a `delay` block before it) and where the firing is written.

## The reach

`BuildGraph` starts at the root and goes out by steps, forward through what each node fires and backward through what
fires it, under a `Filter`:

- **Depth** stops the reach at a number of steps. A node the depth stops at that links further is marked `Deep`, and
  the filter's `Expand` opens it to go on.
- **Fan-out** draws a node's first firings and folds the rest into one fold, which `Expand` opens.
- **Limit** caps the nodes. A graph the limit cuts is `Truncated`, and `Total` says how many the reach found.
- **Databases** and **Origins** say which nodes are drawn. Databases picks among the kinds `Defaults` lists, so a firer
  of any other database is drawn whatever it holds; Origins applies to every node. A node the filter hides is spliced
  out: the edges into and out of it become one edge through it, with its name in `Via`, and a node whose parent is
  hidden takes the nearest drawn one. A deep mark on a hidden node passes to the nearest drawn node before it. The
  root and the folds are never hidden.
- **Follow** follows the scripted effects a firing passes through, so a firing through an effect counts.

`Defaults` is the filter of a new graph: the databases that are fired (`schema.Schema.FireDatabases`) and the scripted
effects, from the game and every mod, with every fold closed. `FindRoots` finds the nodes whose name holds a query,
the exact name first, then the names that start with it, then the rest.

## The detail

`BuildDetail` reads the definition the game keeps of a node. A `Detail` holds its description in the workspace's
language, else the base language, its source, and its `Section`s, one for each key at its top, the options apart. A
section that shows a text has its `Version`s: each text it can show with the trigger that decides it, and whether one
shows at random. A version whose key has no message in the detail's language is `Missing`, and shows the base
language's text. The detail also holds the node's firings both ways with the node at their other end, the nodes its
definitions refer to, the characters its portraits show with the placeholder each is drawn as, and its background
picture. The background is found by the `Picture` chain of the game's `Events` row, a chain of definitions that ends
in an image file.

## Terms

- **Node**: one node of the event graph: the kept definition of an event, an on_action, a decision, a scripted effect
  or another entry that fires one, or a fold.
- **Fold**: a node that stands for the firings of one node past the fan-out.
- **Edge**: one firing the graph draws.
- **Filter**: what the graph shows and how far it reaches from its root.
- **Graph**: the event graph from one root, without coordinates.
- **Step**: a node's distance from the root, negative for a firer.
- **Deep**: what a node is when the depth stopped at it and it links further.
- **Detail**: what the page shows about one node.
- **Section**: one key at the top of a definition, as the page shows it.
- **Version**: one text a key can show, with the trigger that decides it.
