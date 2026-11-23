# graph

[Architecture](../../docs/ARCHITECTURE.md) › [Services](../../docs/ARCHITECTURE.md#the-services) › graph

The graph service builds what the Graph page shows. Each type of definition has one view: an event its graph of
firings, a good its chain, a building its line, a technology its tree. It also lists what the page browses, and builds
the detail of one node.

What links two definitions is the [index](../../internal/index/README.md)'s: its firings, its references and the
definition the game keeps of a name. What type a database's definitions are, in each game, is
[lang](../../internal/lang/README.md)'s `Declaration.Type`, and the types the Graph has a view of are its
`Dialect.GraphTypes`. This service adds the reach from an event link by link, the model of how a game's goods,
buildings and technologies link, and the views read from it. A view has no coordinates and the page formats no
number: every amount, range, share and name is worded here (`pages/graph`, see
[the frontend](../../frontend/src/README.md)). The rules the package applies, and what it refuses and reads past, are
in its doc comment (`graph.go`).

**Uses** [files](../../internal/files/README.md), [game](../../internal/game/README.md),
[index](../../internal/index/README.md), [jomini](../../internal/jomini/README.md),
[lang](../../internal/lang/README.md), [loc](../../internal/loc/README.md), [lsp](../lsp/README.md),
[workspace](../workspace/README.md) · **Used by** [mcp](../mcp/README.md).

## The views

```mermaid
flowchart TD
  subject["A subject's id"] --> kept["Find the definition the game keeps"]
  kept --> graph["BuildGraph: an event's firings<br/>(the index's firings)"]
  kept --> chain["BuildChain: a good"]
  kept --> line["BuildLine: a building"]
  kept --> tree["BuildTree: a technology"]
  index["The index's references and statements"] --> model["The model, one per index"]
  model --> chain
  model --> line
  model --> tree
```

| Subject | View | Answer |
|---|---|---|
| An event, on_action, decision, message or scripted effect | `BuildGraph(subject, filter)` | `Graph` |
| A good, and CK3's good category | `BuildChain(id, filter)` | `Chain` |
| A building (a level of one in CK3 and EU5) and Vic3's production method | `BuildLine(id, picked)` | `Line` |
| A technology, an innovation or an advance | `BuildTree(id, filter)` | `TreeGraph` |

`Defaults` is the filter the page starts with. `FindRoots` finds the subjects whose name holds a query, the exact name
first, then the names that start with it, then the rest. `ListCatalogs` gives the page's catalogs (goods, CK3's good
categories, buildings, technologies, events), each with the groups its rows are listed under, and `ListCatalog` the
rows of one, a building line once by its first level and a definition a mod changes once by the kept copy.
`ListOwnRoots` lists what the workspace's mods define, and `ListDefinitions` the definitions of one type in the order
the game's files define them.

## What fires it

`BuildGraph` goes out from an event link by link, forward to what it fires and backward to what fires it. The firings
are the index's (`ListFires` and `ListFiredBy`). Under the filter's depth, fan-out and limit, the reach marks a node
`Deep` where the depth stopped at a node that links further and folds the firings past the fan-out into one fold. It
splices out the nodes the filter hides: the edges into and out of one become an edge through it, with its name in
`Via`. A firer that reaches the root only through a node the root itself fires is `Looped`.

An edge holds where its firing is written, its delay in words, and the blocks around it, each a `Step` with its
conditions in words. The index's scan says which statement is the effect that fires (`index/fires.go`), and nothing
here decides it again.

## The model

Every other view reads the model. `loadModel` builds one per index on the first request and keeps it weakly, as
`candidates` does for pictures. A `model` is a list of links, each an `Edge` with a `Relation`, with the positions of
the links that start and end at each definition. The table `relations` has a row for each way each game writes a
link: in which database, at which key path, as a reference the index holds, as a key named after the target, or as a
nested definition. A row cites the file and line that writes what it reads.

An amount is the number written. Where a script value is written there, its number shows when its definition is one
number, and its name otherwise. A link between two definitions that is written in many places, such as the locations
that hold a raw material, is counted into one, and its amount is how many.

A `draft` is one request: the workspace, the model, and what was worked out once (each node, each group, each script
value's number). `lookupNode` is the one maker of a node from the model: its picture, its painting, the group it is
listed under and a technology's era.

## Groups

`findGroupID` gives the group a definition is listed under, which the catalogs, the chain's piles and the trees all
read. A group is a definition the links give (`groupTypes`: a building's building group, district or category, a
production method's group, a good's category), or a value the definition writes (`writtenGroups`: a Vic3 good's
`category`, a CK3 district's `type`, a technology's tree), titled with the game's own name for it. CK3 lists a good
under the one category that writes `need = yes` (`groupMarks`), and an innovation that does not write
`flag = global_regular` under Cultural and Regional. A technology whose game writes no tree is under its era, as EU5's
ages are its trees.

## A good's chain

`BuildChain` gives where the good is found (`Source`: how it is gathered, how many places), the lines that make it
(`Maker`, with a `Recipe` for each level or method that writes it: what it takes and makes, the era that unlocks it,
the need that gives it), the goods the makers take (`Input`), and the lines that take it, piled under their widest
group with the largest first (`Pile`, `User`). An `Amount` says whether every production method of a level takes a
good, only some, or one way of several (`Among`). `describeAmounts` is the one place an amount is worded: the number
as the game writes it, two decimals in EU5, a share as a signed percentage, a range from least to most. The filter's
`Origins`, `Changed` and `Era` decide which lines are listed.

## A building's line

`BuildLine` gives the levels as columns (`LineLevel`, with the technology and era that unlock each) and the facts as
rows (`LineRow`): what a level makes, boosts, takes or needs, employs and costs, and what its modifiers give, each
with the condition its block writes. A fact every level says alike is in `Same`. A `Cell` holds its text and its share
of the row's largest number, for a bar. A production method as the subject is its building's line.

Where the game has production methods the line also has its groups (`MethodGroup`, each `Method` with what it takes,
makes, employs and gives) and `Totals`: what the picked methods come to together, and the same with each group's first
method. `picked` names a method of a group, and a group none names runs its first.

`facts.go` puts a modifier into the game's words. `modifierTexts` holds the localization keys each game names a
modifier by; a modifier the dumps list and no text names shows its key. A data call in a name reads as the concept it
links to, or as the definition it names. A statement of a modifier block whose value is a plain word is a condition
of the block, worded by the game's text for it or by the tradition that grants the parameter it names.

## A technology's tree

`BuildTree` gives the technologies of the subject's tree (`Technology`: its node with its era, what its modifiers
give, what it unlocks), what each requires, the eras in file order, the game's trees (`Tree`, each with its painting,
its count and the technology it opens on), and the ids the subject needs and leads to at any distance.

An EU5 age holds advances for different countries. An advance with no `potential` is for every country and is always
drawn. One whose `potential` names a country, religion or culture, itself or through a scripted trigger, is in the
narrowest of those groups; the answer lists the groups' rows in `Audiences`, and draws the rows the filter's
`Potential` names, with what is written for what they belong to (a country's culture and religion, a culture's
language and groups, a religion's group). Any other `potential` is under other conditions. A name inside `NOT` says
who cannot have the advance. A technology's `Root` is the technology its own tree inside the age starts from.

## Pictures

`findPicture` gives a definition's picture. An event's is the window background its game's `Events.Picture` chain
finds. Any other type reads its row in `pictureRows`: a key's value (Vic3 `icon` and `texture`, CK3 `type_icon` and an
innovation's `icon` through its constant), or a file named after the definition, in a folder of the game's art. A row
marks CK3's building icons as silhouettes, which the game tints, so only their shape counts. `findPainting` gives the
painting behind a head: a Vic3 building's `background`, a technology tree's illustration, a district's first background
with no trigger, an era's or age's. Each path is found by `index.ResolveTexture`.

## Conditions in words

A condition in words is a block's limit, else its trigger, each statement a `Condition`. Where some of a trigger's
conditions are negated, `groupConditions` sets them under "must be true" and "must be false".

- A key with words of its own reads them from `phrases`, and one that `Covers` what it holds reads its text as the
  player does (a `custom_tooltip`).
- A key that names a scope, or a parameter that stands for one, stays as written as the condition's `Subject`. Any
  other key is its words, a yes after it dropped and a no making it `Negated`.
- Each operator reads its words from `operatorSays`, but an = reads its words only between a scope and a value, and a
  ?= before a block reads "if it exists".
- A value stays as written, with `Title`, the in-game name of the definition a `prefix:name` value names. A key marked
  `ValueFirst` makes the value the subject (`exists = X` reads "X exists").
- A block that only wraps one condition folds into it. A negation around several stays a group, since moving its
  lines would change what they claim.

## The detail

`BuildDetail` reads the definition the game keeps of a node. A `Detail` holds the node with its trigger, its source
and a `Section` for each key at its top, the options apart; a block's value is left to the source.

- `About` are the facts listed under the node's name: each group it is listed under, its era, how many levels its
  line has, where it is found and what it costs (`detailRows` names the keys, `costLabels` what the page calls them).
  An event has none.
- A section that shows a text has its `Version`s: each text it can show in the workspace's language, else the base
  language, with the trigger that decides it in words and its pieces (`loc.Segments`), each data call made its words.
  A good, a building and a technology keep only their title and description.
- A call that names a customizable localization reads as the texts it can show, joined by slashes ("daughter/son").
- A version whose key has no message in the detail's language is `Missing`, and shows the base language's text. Its
  `Message` is the line the text came from: the detail's language's, else the base language's, and the zero site
  where neither has one.
- `Links` is every model link from and to the node. A technology also has a `trigger` link from each definition that
  tests it, and an event a link to each good, building and technology its script names. `Linked` holds the node at
  the far end of each.

The detail also holds what fires the node, the node at the other end of each firing, and the game rules it reads with
their settings' texts. Where the definition hides a copy of its name, the detail holds that copy's node.

## What was measured

Read from the three installs with no mods when each part was built.

The model holds 17,223 links in CK3, 3,479 in Vic3 and 12,343 in EU5. It builds in 232, 66 and 175 ms the first time
and in none when asked again; each view below then takes under 300 ms.

| | CK3 | Vic3 | EU5 |
|---|---|---|---|
| Catalogs | 44 goods in 20 categories; 980 building lines in 360 districts and district types; 133 innovations in 3 groups | 54 goods in 4 categories; 122 buildings in 49 building groups; 179 technologies in 3 trees | 75 goods in 2 categories; 425 building lines in 16 categories; 3,592 advances in 6 ages |
| A chain | Wood: found in 2,276 counties, made by 5 lines, taken by 127 lines in five district types | Tools: made by Tooling Workshops, 30 to 110, taken by 31 lines | Books: made by the Scriptorium line (0.3, 1, 2, 4), taken by 90 lines and 18 needs |
| A line | Forestry: 10 levels, Wood 60 to 465, a need for Iron from level II | Tooling Workshops: 2 groups of 4 methods; the first methods come to 5,000 jobs | The Scriptorium line: 4 buildings, each with the advance and age that unlock it |
| A tree | Military 28, Civic 46, Cultural and Regional 59, in 4 eras | Production 57 with 68 requirements; Mechanical Tools needs 6, leads to 12, unlocks 6 | Discovery draws the 141 advances of every country, and 5 more with Catholicism |

EU5's lead is found in 109 locations, written as one counted link. Pictures were found for these shares:

| | CK3 | Vic3 | EU5 |
|---|---|---|---|
| Goods | 44 of 44 | 54 of 54 | 75 of 75 |
| Building lines | 898 of 904 | 103 of 122 buildings | 425 of 425 |
| Technologies | 133 of 133 | 179 of 179 | 2,700 of 3,592 |
| Paintings | every district (118 files); a holding writes none, its paintings are its districts' | a building's `background` (11 files) and 3 trees | 6 ages |

CK3 also has 4 eras with a painting, 917 building icons that are all silhouettes, and 33 of 41 good categories with an
icon. A CK3 building writes its `type_icon` in the first level of a line only, so the line shows one icon.

What is not known, and what the code does:

- **A Vic3 pop need's `default`** is not read as a link. Only its `entry/goods` is.
- **A modifier no text names** shows its key. In CK3's innovations that is `accolades` alone.
- **A condition with no text**, as CK3's `is_coastal = yes`, is shown as written.
- **A name that is an alias**, such as a Vic3 building's plural `building_tooling_workshops`, is no definition. The
  subject's id is the definition's own name, and a view says the game keeps none for an alias.
- **A script value** that is not one number shows its name where an amount is written, and is left out of a range.

## Files

| file | what it holds |
|---|---|
| `graph.go` | The service, the node, edge, filter and graph, an event's graph, the default filter. |
| `roots.go` | What the page can open: a search, the catalogs and their rows, the mods' own, one type's. |
| `events.go` | The reach from an event link by link, its folds, loops and splice. |
| `steps.go` | An edge: where a firing is written, the blocks around it, its delay, its effect and its title. |
| `conditions.go` | A block of triggers put into words. |
| `model.go` | The relations each game writes, and the reader that builds the model of an index from them. |
| `draft.go` | One request: the workspace, the model and what was worked out once. |
| `nodes.go` | A definition as a node: its picture, painting, group and era, and the lists of one type. |
| `groups.go` | The group a definition is listed under, by a link or by a value it writes. |
| `lines.go` | A building line read from the model: the lines that link to a good, a level's links, where a good is found. |
| `chain.go` | A good's chain: its makers, their recipes, the goods they take, its users in piles, and how an amount is worded. |
| `line.go` | A building's line: the table of its levels, its production method groups and their totals. |
| `facts.go` | What a definition's modifiers give, and the conditions of their blocks, in the game's words. |
| `tree.go` | A technology's tree: its technologies, what each requires, gives and unlocks, and the game's trees. |
| `audiences.go` | Whom an advance of an EU5 age is for. |
| `pictures.go` | The picture and painting of a definition: an event's chain, and the rows of the other types. |
| `detail.go` | The detail of one node: what it is listed under and costs, its links, rules and the copy it hides. |
| `texts.go` | The texts of a detail: its sections, their versions, and the data calls put into words. |

## Terms

- **Subject**: the definition a view is about, by its node id, `database:name`.
- **View**: what the page shows of one subject: an event's graph, a good's chain, a building's line, a technology's
  tree.
- **Type**: what the definitions of a database are in a game, as `lang.Type` says: `good`, `good_category`,
  `building`, `building_group`, `production_method_group`, `production_method`, `technology`, `era` or `demand`.
- **Node**: one definition in a view, or a fold of an event's graph.
- **Line**: a building through its levels. Its first level stands for it.
- **Level**: one building of a line. In Vic3 a line has one, and its methods stand for what changes.
- **Member**: a method group or a method that is a part of a level.
- **Link**: one edge between two goods, buildings or technologies, with a `Relation`, as against a firing.
- **Relation**: how a link says its two ends relate. `level`: one level leads to the next. `group`: a definition is a
  part of another, a building in its group or a method in its slot. `input` and `output`: a definition uses up a good,
  or produces it. `boost`: it raises what is made of a good by a share. `gives`: what meeting a need produces.
  `employment`: a method hires a pop type. `unlock`: a technology opens a definition. `requires`: a technology needs
  another first. `era`: a technology is in an era or age. `resource`: a good is found in a place. `trigger`: a
  definition tests whether a technology is researched; no row reads it, and a technology's detail lists it.
- **Group**: what a definition is listed under: a district, a building group, a category, a tree.
- **Model**: every link between a game's goods, buildings and technologies, built once from the index.
- **Draft**: one request about them, with what it worked out once.
- **Chain**: a good with who makes it, from what, and who takes it.
- **Maker**, **Recipe**, **Input**, **Pile**, **User**: a line that makes the good; one level or method of it; a good
  the makers take; the lines that take the good under one group; one of those lines.
- **Amount**: a good with how much, as shown. **Among**: which of a level's production methods take it.
- **Source**: where a good is found: counties, state regions or locations, counted.
- **Fact**: one thing a modifier gives, or one labeled fact about a node, with its condition.
- **Tree**: the technologies the game lists together: a CK3 group, a Vic3 category, an EU5 age.
- **Audience**: a religion, culture or country that advances of an EU5 age are written for.
- **Picture**: the icon of a definition, or an event's scene. **Painting**: the picture behind a head.
- **Silhouette**: a picture the game tints, drawn as a mask in the text color.
- **Edge**: one firing, from a firer to what it fires, or one link.
- **Step**: one block a firing is written inside, from the top of its definition.
- **Distance**: how many links a node of an event's graph is from the subject, negative for one that leads to it.
- **Condition**: one statement of a block of triggers in words, with the statements of its block under it.
- **Fold**: a node that stands for the firings of one node past the fan-out.
- **Deep**: what a node is when the depth stopped at it and it links further.
- **Via**: the scripted effects a firing passes through that are not drawn.
- **Filter**: what a view shows: an event graph's reach, the origins drawn, the era a chain is available by, the
  audiences of a tree.
- **Graph**: an event's firings, without coordinates.
- **Display**: how the player sees an event or a message: a window, hidden, or a message.
- **Copy**: the latest-loaded copy of a name that the definition the game keeps hides.
- **Detail**: what the page shows about one node.
- **Section**: one key at the top of a definition, as the page shows it.
- **Version**: one text a key can show, with the trigger that decides it.
- **Rule**: one game rule a definition reads, with its settings.
