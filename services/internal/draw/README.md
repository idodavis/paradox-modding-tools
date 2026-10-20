# draw

**A preview of a mod's GUI entries and coats of arms with the game's own textures, fonts and localization, where
nothing about the game's state is simulated and nothing is asked.** Every widget is drawn unless its file says
`visible = no` or `visible_at_creation = no` (`game.GUIHiddenKeys`); what only
the game would know, a binding, is drawn as written, but for a texture whose file the scan finds. A vanilla window is a
stress test of the preview, never a promise of fidelity.

The preview translates; three engines think. The index resolves the script. The game's shaders say how a sprite is
cut, scaled and masked. The browser lays out, breaks lines, slices, masks and blends. Everything the preview decides
that none of the three does is a row in a table with its source beside it: the CK3 wiki's Interface page, a shader
file, or vanilla observed in the app. The rows are `game/gui.go` (how each built-in type draws, the roles of a
widget's blocks, where a window is laid out, the state it opens in, the calls the preview answers), `game/coat.go`
(what a coat reads, where each game keeps its coats and textures, its field, which way depth orders it, its shading
and the frames its designer previews), `frontend/src/draw/picture.ts` (what a holding and a property mean in CSS)
and `frontend/src/draw/coat.ts` (how a coat's shaders color its textures and where its keys place a layer).

Read `draw.go` first: what both kinds share, and the calls that answer each.

## Files

| file | the one job |
|---|---|
| `draw.go` | what both kinds share: the request, the element, a property, what is missing; the calls; the resolve both start from |
| `properties.go` | the reader every property goes through: a statement with the file and writer it comes from, the properties statements write, read by key, the last written winning |
| `gui.go` | a GUI entry's payload, and the entry resolved into a tree of widgets: the type chain, templates and blocks under the overrides in force, the opening state, the children, the holders |
| `text.go` | a widget's text as runs, and the fonts that draw them |
| `texture.go` | a widget's textures, read as the sprite shader reads them, and the file a texture binding draws |
| `coat.go` | a coat's payload, the coat resolved into layers on its field, the lists its designer picks from, and the file a mod's new coat is written in |

The tests are the package's one test file: the fixture tests, and the gate over the three installs
(`PMT_VANILLA_GATE`).

## Two payloads over one element

An `Element` is what every drawn thing has, a widget's or a coat layer's: its parent, key, type, name, label, path
and ancestors, and where it is written (`Location`); the page selects, marks and links the code by these alone. A
GUI entry's payload is a `List` of `Widget`s, a coat's a `Coat` of `Layer`s, each embedding `Element`, which the
bindings flatten, so the page reads the same keys from both. The `resolver` is what a resolve of either kind shares:
the request's entry (`entry`), the definitions and routes read once, the element being resolved and what is missing;
`guiResolver` and `coatResolver` embed it.

`Render.List` resolves a GUI request (a file, an entry's id and database) into one `List`: the entry's widgets in
pre-order, the widgets the game lays the entry out in (`Holders`), the textures they draw and the fonts they use, each
listed once, the calls they are shown on (`Groups`), and what nothing found (`Missing`). An entry is a window drawn as
written, or a type, template, text icon or format drawn alone (`IsAlone`) as the script `entryRows` names it by. A
`Widget` is its element, its role among its siblings (`WidgetSlots`: a skin, chrome, a label, a data model's item),
its type's row stamped for the workspace's game (`Type`, `Holding`, `Clips`, `Fills`, `CentersText`, `DropsHidden`),
every property as written (`Properties`, the last written of each key, a call `GUIAnswered` names replaced by the
entry's value), its textures and its text, and what it is shown on (`IsHidden`, `Condition`). `Render` keeps the last
GUI resolve for the panel.

`CoatOf` resolves a coat, `CoatsOf` several of one file over one resolve's parses and definitions (the Entries feed's
pictures). `Render.Properties` answers the panel for either kind: a widget's from the GUI resolve kept, a coat
layer's from its coat resolved anew; each property's last value, who wrote it, where, and where its value is, which
an edit replaces. `Colors` and `Textures` list what the designer picks from (each named color's CSS, a texture
folder's images by file name across the load order), `Frames` the shapes the game's designer previews a coat in, and
`CoatFile` the file a mod's new coat goes into, with its path in the mod and whether writing it makes the file and
its folder.

## Resolving

A widget's statements come from its type chain (base first), each template in place where it is used, its own block,
then the end values of the states that run as the window opens (`GUIOpeningStates`). A `block` applies its statements,
or those of the `blockoverride` in force for its name where another writer wrote one; the overrides are scanned before
any block is applied, a later one over an earlier, so the widget's own win. A block whose key is a slot, a type or a
built-in is a child; an `item` block's widgets are a data model's row; a `buttontext` block's widget is the label that
draws the button's text; a tooltip is left out. A child written by the widget or an ancestor is left out, since it would
nest without end.

Each property is read through `properties`: a statement's scalars with its file's constants put in, as numbers, as
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

## Coats

A coat is drawn alone on its field (`Coat.Width`, `Height`): the first atlas' tile of `options/atlases.txt` (230 × 230
in CK3, 3:2 in Vic3 and EU5). Its layers, in pre-order: the coat (`coat`), its pattern (`pattern`), then each emblem's
instances in paint order: by `depth`, a larger one behind (`CoatDepthInFront`), the written order among equals;
consecutive instances of one emblem share one `emblem` layer, so an emblem whose instances interleave with another's
is listed once a run. Each `instance` paints the emblem's file; each `sub` is a coat under the root, its parent's
statements under its own (a parent's instances left out, its emblems too where the sub writes any), once for each of
its instances. A `Layer` carries the file it paints and how its shader colors it (`URL`, `Recolor`, `Colors`), an
emblem's mask (`Mask`, the coat's pattern file, and the `Regions` its `mask` keeps) and every property as written
(`Properties`); the page places an instance or a sub from those (coat.ts' `PLACEMENT`) as the coat shader moves an
emblem's quad. An entry written as another coat's id draws that coat, read-only. A color slot is a named color, a
color block, or the coat's slot an emblem names (`color1 = color2`); a slot an emblem does not write is its coat's. A
value a list picks is listed.

The pixels are the page's one computation (`coat.ts`): a pattern blends from a transparent fallback toward its slots
by red, green and blue; a colored emblem starts at slot 1, blends to slots 2 and 3 by green and red and takes a full
overlay of its blue, the coat shaders' GetOverlay, which chooses its formula by the blue (CSS's hard-light, not the
sprite's overlay); a mask keeps the pattern's regions, each region's channel less the later ones, in the field's
coordinates; CK3's cloth (`Coat.Shading`) is overlaid on each coat and sub at a fifth and thins each emblem by its
green. The frames are the shapes CK3's coat designer previews a coat in (`CoatFrames`): a dynasty and a house in the
frames the defines name, a title and a realm in their masks; each mask cuts the field by its alpha.

A coat is edited through its file's model, never through Go: the panel writes a value over its `ValueLocation` or
adds the key to the layer's block, adds an instance or an emblem after the last of its kind, removes one, and a drop
in its list moves an emblem's or a sub's block before or after another's; a drag writes an instance's position; Paste
replaces the coat with the game designer's copy. Each edit is one step of the file's history, which Ctrl Z and Ctrl Y
step in the picture and the panel as in the code. Only the coat's own entry in a mod's file is written (`coat.ts`'
`isEditable`: never a parent's statements or an alias's target), and each edit waits until the file is drawn again
before the next is taken (`stores/draw.ts`' `edit`), since an edit moves every range after it.

## Accepted as different from the game

- Shrinking under pressure: flexbox shrinks in proportion to size; the game shrinks evenly to each minimum.
- A percent height inside a parent sized by its content: the browser resolves the width, the height sometimes.
- A 9-slice in a box smaller than its borders: the browser squeezes the slices; the shader samples fewer pixels.
- A negative margin on a widget that is no skin: the game pulls its children outward; CSS has no negative padding.
- A scroll bar is drawn unless its policy says `always_off`; the game shows it only when the content overflows.
- `wrap_count` and `setitemsizefromcell`: known, not drawn.

Each gets a row, never a function, if one is ever worth fixing.
