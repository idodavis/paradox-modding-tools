# draw

**A preview of a mod's GUI entries with the game's own textures, fonts and localization, where nothing about the
game's state is simulated and nothing is asked.** Every widget is drawn unless its file says `visible = no` or
`visible_at_creation = no` (`game.GUIHiddenKeys`); what only
the game would know, a binding, is drawn as written, but for a texture whose file the scan finds. A vanilla window is a
stress test of the preview, never a promise of fidelity.

The preview translates; three engines think. The index resolves the script. The game's shaders say how a sprite is
cut, scaled and masked. The browser lays out, breaks lines, slices, masks and blends. Everything the preview decides
that none of the three does is a row in a table with its source beside it: the CK3 wiki's Interface page, a shader
file, or vanilla observed in the app. The rows are `game/gui-rules.go` (how each built-in type draws, the roles of a
widget's blocks, where a window is laid out, the state it opens in, the calls the preview answers) and
`frontend/src/draw/picture.ts` (what a holding and a property mean in CSS).

Read `draw.go` first: `Render.List` is the whole of a render.

## Files

| file | the one job |
|---|---|
| `draw.go` | the payload types and the two calls, the last resolve kept between them |
| `block.go` | the reader every property goes through: a statement with the file and writer it comes from, a block of them read by key, the last written winning |
| `resolve.go` | an entry resolved into a tree of widgets, each an element: the type chain, templates and blocks under the overrides in force, the opening state, the children, the holders |
| `text.go` | a widget's text as runs, and the fonts that draw them |
| `texture.go` | a widget's textures, read as the sprite shader reads them, and the file a texture binding draws |
| `draw_test.go` | the fixture tests and the three-install gate (`PMT_VANILLA_GATE`) |

## The list

`Render.List` resolves a request (a file, an entry's id and database) into one `List`: the entry's
elements in pre-order, the widgets the game lays the entry out in (`Holders`), the textures they draw and the fonts
they use, each listed once, the calls they are shown on (`Groups`), and what nothing found (`Missing`). An entry is a
window drawn as written, or a type, template, text icon or format drawn alone (`IsAlone`) as the script `entryRows`
names it by.

An `Element` is one widget: where it is written (`Location`), how it is named (`Label`, `Path`, `Ancestors`, which the
pane, the panel and the code read as they are), its role among its siblings (`WidgetSlots`: a skin, chrome, a label, a
data model's item), its type's row stamped for the workspace's game (`Type`, `Holds`, `Clips`, `Fills`, `CentersText`,
`DropsHidden`), every property as written (`Properties`, the last written of each key, a call `GUIAnswered` names
replaced by the entry's value), its textures and its text, and what it is shown on (`IsHidden`, `Condition`).

`Render.Properties` answers the panel from the same resolve: each property's last value, who wrote it and where.

## Resolving

A widget's statements come from its type chain (base first), each template in place where it is used, its own block,
then the end values of the states that run as the window opens (`GUIOpeningStates`). A `block` applies its statements,
or those of the `blockoverride` in force for its name where another writer wrote one; the overrides are scanned before
any block is applied, a later one over an earlier, so the widget's own win. A block whose key is a slot, a type or a
built-in is a child; an `item` block's widgets are a data model's row; a `buttontext` block's widget is the label that
draws the button's text; a tooltip is left out. A child written by the widget or an ancestor is left out, since it would
nest without end.

Each property is read through `block`: a statement's words with its file's constants put in, as numbers, as
lower-cased choices, or as a color. A definition is found through the index (`index.Resolve`, the load order's winner,
a local type or template of the file first) and read once a render.

## Textures and text

A texture is its file's size at its density, its frame of a sheet (`framesize` in the screen's pixels or
`frame_grid`, which the file route cuts), its border, tiling, mirror, fit, tint and blend, as `pdxgui_sprite_base.fxh`
reads them. Each `modify_texture` is an overlay at its alpha; an `alphamultiply` masks the widget's texture, a `mask`
scales every overlay after it. A texture that is a binding draws the file the scan finds for the entry a quoted key
names (`GUIPictureKeys`, the `_ICON_PATH` defines), else nothing, and the page draws a placeholder.

A text is its loc entry in the workspace's language (else as written), its markup read into runs: a format's color, bold
and italic through the `gui.format` entries, a text icon's texture, a `$key$`'s text, a concept link's name, a `[call]`
as `[…]`. Its font is the face the `font.font` entry names for the style and language, listed for the page to load.

## Holders

A window is drawn on the screen. A window made from a type `GUIHolders` names is drawn in that window. A type or
template drawn alone is drawn in the widgets around the one place the install writes it (`index.RefsTo`), each an
element of its type row and properties alone; none where it is written nowhere, in several places, or in a block.

## Accepted as different from the game

- Shrinking under pressure: flexbox shrinks in proportion to size; the game shrinks evenly to each minimum.
- A percent height inside a parent sized by its content: the browser resolves the width, the height sometimes.
- A 9-slice in a box smaller than its borders: the browser squeezes the slices; the shader samples fewer pixels.
- A negative margin on a widget that is no skin: the game pulls its children outward; CSS has no negative padding.
- A scroll bar is drawn unless its policy says `always_off`; the game shows it only when the content overflows.
- `wrap_count` and `setitemsizefromcell`: known, not drawn.

Each gets a row, never a function, if one is ever worth fixing.
