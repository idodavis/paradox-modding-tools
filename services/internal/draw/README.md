# draw

**A structural preview of a mod's GUI entries, with the game's own textures, fonts and localization, where nothing
about the game's state is simulated and nothing is asked.** Every widget is drawn unless its file says `visible = no`;
what only the game would know, a binding, is drawn as written. A vanilla window is a stress test of the layout, never a
promise of fidelity.

What the visual editors draw: a GUI file's entry resolved from the index and the file's own text into one list of
elements, each with the words it is laid out by as written, its textures and its text. The page nests them as HTML
elements and the browser lays them out (`frontend/src/draw/widgets.ts`): nothing here measures or places. It decides
nothing about script (the schema's walker did), and it writes nothing.

Read `gui.go` first: `GUI` is the whole of a render.

| file | holds |
|---|---|
| `draw.go` | the list and what it holds: `List`, `Element`, `Texture`, `Text`, `Run`, `Font`, `Box`, `Property` |
| `gui.go` | `Request`, `Entries`, `GUI`, `Properties`; the `widget` and `statement` the resolver builds and the block reader every property goes through (`last`, `numbers`, `choices`, `scalar`); the layout words handed to the page (`layoutKeys`), the blend and fit words, and the sentences of what is not found |
| `gui_resolve.go` | the entry found, each widget's type chain followed to its built-in kind through the one `lookup` of a name, its templates and blocks expanded in the order they apply, the opening state, the roles of the blocks it writes, its data model's rows, its siblings shown one at a time, its textures; every widget described as an element |
| `gui_text.go` | a widget's text: the loc entry or literal, its markup into runs with the formats' colors and the icons' textures, and the font files the page loads |

## The list

`List` is one render. `Elements` are the widgets, pre-order, so an element's parent is always before it; an element
is listed whether it is drawn or not, with its location in the file. Each carries its `Role` (a child among its
siblings, or the skin or chrome of its parent), whether it is `Hidden`, the `Rows` a data model item is drawn, the
sibling whose place it shares (`Pair`), its `Layout` words as written by key (`size = { 100 20 }` is `"100 20"`), its
`Textures` and its `Text`. `Fonts` are the faces the texts use, one row a name and style, with the route's URL;
`Missing` is what nothing found, each a sentence the status line shows ("Template not found: x"). The scene (`Width`,
`Height`) is a window's resolution; an entry drawn alone (a type, template, text icon or format) is its own size, and
one with no size, texture, text or child of its own is listed. A `Property` is an element's key as written, with who
wrote it.

## Resolving

An entry's name picks a statement of the file: a window is its own, a type is drawn by naming it and a template by a
widget that uses it, a text icon by an icon of its texture and size, and a format by a text written in it. The entry
drawn by name is drawn whatever its own `visible` says. A widget's key follows the types to a built-in kind (`chain`):
each type's block, base first; a type based on its own name ends the chain with the kind's defaults; a base no type
defines is a kind the install implies (`Schema.WidgetKinds`). One `lookup` answers every name a caller resolves, a
definition of the file before the load order's winner. The statements that apply are the chain's blocks, each `using`
at its point, the widget's own last; a `blockoverride` replaces the `block` of its name wherever the block is written
below the widget that wrote the override, and a child sees the overrides of its parent. The last statement of a key
wins everywhere (`widget.last`). A window runs in its opening state: the state named `_show`, and every state with
`trigger_on_create = yes`, writes its end values as the widget's own. A data model's item widgets are resolved once
and drawn three rows. A text with no `fontsize` takes the font and size of the install's `textbox` type.

A block whose key names a widget is a child, laid out among its siblings and drawn in written order, but for the keys
of `game.WidgetSlots`, which the game treats apart: a `background` is its widget's skin, filling it and drawn behind
its children; a `scrollbar_vertical` or `scrollbar_horizontal` is chrome at the edge, a `scrollbar`; a `tooltipwidget`
is shown on an event the preview has not, and is left out.

A widget's textures are its `texture` and then each `modify_texture` in order, the latter's blocks expanded and
overridden as the widget's own are, so a color overlay's `block "master_color_frame" { frame = 7 }` picks its frame.
An overlay is masked by the widget's texture (`Texture.Mask`), since the game keeps the texture's alpha, so a color
added to a flat icon shows in the glyph's shape; a `mask` or `alphamultiply` overlay masks the texture instead. A
binding (`[GetPlayer.GetName]`) is the game's to answer, so nothing answers it: a `visible` that is one draws the
widget, a `text` that is one is drawn as written, a `texture` that is one is written in the widget's box as its text,
and a number that is one is none. One thing the text
alone says: two siblings whose `visible` bindings are `[X]` and `[Not(X)]` are shown one at a time, so the later shares
the earlier's place (`Pair`) instead of taking one of its own.

## Laying out

The page's table (`widgets.ts`) maps each kind and each layout word to the CSS that means the same thing, and the
browser does the rest: a row or column is a flex box whose items' policies are their grow and shrink, a flow wraps,
a grid has cells, a scroll area clips, and every other kind is one grid cell its children sit in by their anchors,
moved by their position. A word with no row is not carried, and the browser lays the rest out as if it were not
written. Go hands over only the words (`layoutKeys`), as written, scaled by nothing: the UI scale is a zoom on the
window.

## Decisions

- **No row per game.** Each fact comes from the install (the types and defaults, the fonts and their files, the
  formats, the text icons) or from a language row in `game/lang-rules.go`; a row exists only where no file of the
  install could state it.
- **One parse per request.** `workspace.Parses` holds the files a render reads; nothing is kept past it.
- **The browser is the layout engine.** The game's layout is a box model with size hints, policies, stretch factors and
  anchors, the model flexbox and grid implement; Go computes no size and no box, so nothing here can disagree with the
  browser about where a word puts a widget. The fonts are the game's files, loaded by the page, and the browser breaks
  the lines.
- **Nothing is answered for the game.** No evaluator, no sample data, no default for what a binding returns: a widget
  whose visibility is a binding is drawn, and a bound text shows the binding, so a modder sees which binding sits where.
