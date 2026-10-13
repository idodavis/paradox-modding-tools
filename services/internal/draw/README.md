# draw

**A structural preview of a mod's GUI entries, with the game's own textures, fonts and localization, where nothing
about the game's state is simulated and nothing is asked.** Every widget is drawn unless its file says `visible = no`;
what only the game would know, a binding, is drawn as written. A vanilla window is a stress test of the layout, never a
promise of fidelity.

What the visual editors draw: a GUI file's entry resolved from the index and the file's own text into one flat list of
elements, parts and fonts, which the page draws and computes nothing over. It decides nothing about script (the
schema's walker did), and it writes nothing.

Read `gui.go` first: `GUI` is the whole of a render, `resolve` then `place`.

| file | holds |
|---|---|
| `draw.go` | the list and what it holds: `List`, `Element`, `Texture`, `Text`, `Line`, `Run`, `Part`, `Font`, `Box`, `Property` |
| `gui.go` | `Request`, `Entries`, `GUI`, `Properties`; the `widget` and `statement` the two passes share and the block reader every property goes through (`last`, `numbers`, `choices`, `scalar`); the blend and fit words and the sentences of what is not found |
| `gui_resolve.go` | pass 1: the entry found, each widget's type chain followed to its built-in kind through the one `lookup` of a name, its templates and blocks expanded in the order they apply, the opening state, the data model's rows, textures; every widget listed as a part |
| `gui_text.go` | a widget's text: the loc entry or literal, its markup into runs with the formats' colors and the icons' textures, measured with the real font files and broken into lines |
| `gui_layout.go` | pass 2: the data model's rows drawn, each widget sized where it declares no size, each container's children placed by its layout class, the elements in paint order |

## The list

`List` is one render at a resolution and UI scale. `Elements` are rectangles in paint order at absolute boxes in scene
pixels: one per texture, or one bare where a widget has none (the page picks and outlines by it), then one for its
text. `Parts` are the widgets, pre-order, so a part's parent is always before it; a part is listed whether it is drawn
or not, with its location in the file. `Fonts` are the faces the texts use, one row a name and style, with the route's
URL; `Missing` is what nothing found, each a sentence the status line shows ("Template not found: x"). The scene
(`Width`, `Height`) is a window's resolution; an entry drawn alone (a type, template, text icon or format) is drawn at
the origin on a scene of its own size, and one with no size and no texture of its own is listed. A `Property` is a
part's key as written, with who wrote it.

## Pass 1: resolve

An entry's name picks a statement of the file: a window is its own, a type is drawn by naming it and a template by a
widget that uses it, a text icon by an icon of its texture and size, and a format by a text written in it. The entry
drawn by name is drawn whatever its own `visible` says. A widget's key follows the types to a built-in kind (`chain`):
each type's block, base first; a type based on its own name ends the chain with the kind's defaults; a base no type
defines is a kind the install implies (`Schema.WidgetKinds`). One `lookup` answers every name a caller resolves, a
definition of the file before the load order's winner. The statements that apply are the chain's blocks, each `using`
at its point, the widget's own last; a `blockoverride` replaces the `block` of its name wherever the block is written
below the widget that wrote the override, and a child sees the overrides of its parent. The last statement of a key
wins everywhere (`widget.last`). A window runs in its opening state: the state named `_show`, and every state with
`trigger_on_create = yes`, writes its end values as the widget's own. A data model draws three rows; its item's widgets
are resolved once and drawn once a row. A text with no `fontsize` takes the font and size of the install's `textbox`
type.

A binding (`[GetPlayer.GetName]`) is the game's to answer, so nothing answers it: a `visible` that is one draws the
widget, a `text` that is one is drawn as written, a `texture` or a number that is one is none.

## Pass 2: place

A container places its children by its layout class (`game.LayoutClasses`). A row or column takes its children's
minimum, preferred and maximum sizes and the room along it, and the room decides one of three branches: below the
minimums' sum each child keeps its minimum; below the preferred sizes' sum each gives up what it can in proportion;
above it the extra goes to the stretch factors, else to the expanding children, each held at its maximum. A grid puts
children in cells, a flow wraps along its direction, a scroll area places its content at its top left and clips it, any
other widget places each child by its `parentanchor`, `widgetanchor` (the `parentanchor` where it names none) and
`position`.

A widget that declares no size on an axis finds it by its kind's row in `game.Sizings`: a row or column by its children
summed and the widest, a container by the bounds of its children, a flow or grid by its cells, a background by the room
it is given, a text by its lines, an icon by its texture; any other kind is zero. A size written in pixels is
multiplied by the UI scale once; a percent is a fraction of what the parent hands out where its children are placed,
and of nothing where a parent's size is measured from them, so a child's `100%` does not make its parent as wide as the
room; a number written negative is unset on its axis, as vanilla writes `size = { -1 32 }` under an expanding policy
and `maximumsize = { 400 -1 }` for no maximum. A kind that fills its parent (`background`) takes no place among its
siblings: it is laid over the parent's whole box, and the parent's children are placed and measured without it. A flow
or grid measures its cells with the limit it will be placed at, the room held to its own size and maximum. A text's
words are measured once (`pieces`) and its lines broken where it is placed: at its `max_width`, else its declared
width, else, where it is `multiline`, at the width the layout gives it; measured with no room, such a text is its
longest word wide. A row or column under a parent that does not lay it out (a window, a widget, a container, a scroll
area) takes the parent's room on an axis it declares no size for where one of its children expands or takes a percent
on it, and its content on any other axis (`spanned`): a window's main column fills the window, a footer row of fixed
buttons keeps its size and its anchor.

## Decisions

- **No row per game.** Each fact comes from the install (the types and defaults, the fonts and their files, the
  formats, the text icons) or from a language row in `game/lang-rules.go`; a row exists only where no file of the
  install could state it.
- **One parse per request.** `workspace.Parses` holds the files a render reads; nothing is kept past it.
- **Fonts are measured, not guessed.** A text is measured glyph by glyph in the first file of the language's group
  that has the glyph, with the font file's own advances and kerning; a font nothing opens is listed and its text not
  drawn.
- **Nothing is answered for the game.** No evaluator, no sample data, no default for what a binding returns: a widget
  whose visibility is a binding is drawn, and a bound text shows the binding, so a modder sees which binding sits where.
