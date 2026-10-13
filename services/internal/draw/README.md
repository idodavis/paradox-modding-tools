# draw

What the visual editors draw: a GUI file's window resolved from the index and the file's own text into one flat list of
elements, parts, fonts and facts, which the page draws and computes nothing over. It decides nothing about script
(the schema's walker did), and it writes nothing to a game's files: an edit is a list of text edits the page applies
through the editor.

Read `gui.go` first: `GUI` is the whole of a render, `resolve` then `place`.

| file | holds |
|---|---|
| `draw.go` | the list and what it holds: `List`, `Element`, `Texture`, `Text`, `Line`, `Run`, `Part`, `Fact`, `Font`, `Box`, `Property`, `Change` |
| `gui.go` | `Request`, `GUI`, `Properties`, `Edit`, `PruneFacts`; the `widget` and `statement` the two passes share and the block reader every property goes through (`last`, `numbers`, `words`); the blend and fit words |
| `gui_resolve.go` | pass 1: the entry found, each widget's type chain followed to its built-in kind, its templates and blocks expanded in the order they apply, the opening state, the data model's rows, visibility, textures; every widget listed as a part |
| `gui_evaluate.go` | the evaluator: a binding answered with no game running, and every question it could not answer kept as a fact |
| `gui_text.go` | a widget's text: the loc entry or literal, its markup into runs with the formats' colors and the icons' textures, measured with the real font files and broken into lines |
| `gui_layout.go` | pass 2: the data model's rows drawn, each container's children placed by its layout class, the elements in paint order |

## The list

`List` is one render at a resolution and UI scale. `Elements` are rectangles in paint order at absolute boxes in scene
pixels: one per texture, or one bare where a widget has none (the page picks and outlines by it), then one for its
text. `Parts` are the tree, pre-order, so a part's parent is always before it; a part is listed whether it is drawn or
not, with its location in the file, its visibility (`Shown`, `Hidden`, `Unknown`) and the fact that decides it.
`Fonts` are the faces the texts use, one row a name and style, with the route's URL; `Facts` are what the bindings
asked, in the order first asked; `Missing` is what nothing found, each string starting with its kind (`template`,
`type`, `texture`, `font`, `format`, `texticon`, `loc`, `data function`, `blend_mode`, `fontsize`, `glyph`).

## Pass 1: resolve

A window's name picks a statement of the file: a window is its own, a type is drawn by naming it and a template by a
widget that uses it. A widget's key follows the types to a built-in kind (`chain`): each type's block, base first; a
type based on its own name ends the chain with the kind's defaults; a base no type defines is a kind the install
implies (`Schema.WidgetKinds`). The statements that apply are the chain's blocks, each `using` at its point, the
widget's own last; a `blockoverride` replaces the `block` of its name wherever the block is written below the widget
that wrote the override, and a child sees the overrides of its parent. The last statement of a key wins everywhere
(`widget.last`). A window runs in its opening state: the state named `_show`, and every state with
`trigger_on_create = yes`, writes its end values as the widget's own. A data model draws three rows where no fact gives
its count; its item's widgets are resolved once and drawn once a row, each row's bindings answered with the row's
index.

## The evaluator

A binding is answered, in this order, by: a literal, a macro (`data_binding` entries are replaced by their text), a fact
the user gave (for the exact call, then for its `Type.Function`), the state a window opens in (`game.UnsetAtOpen`: a
variable, a tab and the pointer are nothing yet), a computed function (`game.ComputedFunctions`: logic, comparison,
arithmetic, parity, list size, a choice between two values, a localization key's text, joined texts, the row's index), else a stand-in that says it is
not known: a bool and a number unknown, a text the function's words, a list of three rows, an object that exists. The
steps lead through the data types the dumps declare (`Schema.CallStep`); a step no type declares is unknown and listed.
An unknown widget is drawn faded by the page, or laid out as hidden at the user's choice. Nothing is guessed: the
answer is the user's, given once per workspace and kept as `Workspace.Facts`.

## Pass 2: place

A container places its children by its layout class (`game.LayoutClasses`). A row or column takes its children's
minimum, preferred and maximum sizes and the room along it, and the room decides one of three branches: below the
minimums' sum each child keeps its minimum; below the preferred sizes' sum each gives up what it can in proportion;
above it the extra goes to the stretch factors, else to the expanding children, each held at its maximum. A grid puts
children in cells, a flow wraps along its direction, a scroll area places its content at its top left and clips it, any
other widget places each child by its `parentanchor`, `widgetanchor` and `position`. A size written in pixels is
multiplied by the UI scale once, a percent is a fraction of what the parent gives.

## Decisions

- **No row per game.** Each fact comes from the install (the types and defaults, the fonts and their files, the
  formats, the text icons, the macros) or from a language row in `game/lang-rules.go`; a row exists only where no file
  of the install could state it.
- **One parse per request.** `workspace.Parses` holds the files a render reads; nothing is kept past it.
- **Fonts are measured, not guessed.** A text is measured glyph by glyph in the first file of the language's group
  that has the glyph, with the font file's own advances and kerning; a font nothing opens is listed and its text not
  drawn.
- **An edit is text.** `Edit` writes into the widget's own block (a key's value replaced, or the key added after the
  block's last statement), removes the part's statement, or moves it before another's; a path under the install is
  refused.
