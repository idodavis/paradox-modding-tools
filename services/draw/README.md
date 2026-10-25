# draw

The package builds what the Visual view shows. It resolves a GUI window into its widgets and a coat of arms into its
layers, and it produces the text edits the view's panels make.

Go resolves and the browser lays out. A `Window` is a flat list of widgets, each with every property as written, its
built-in type's row, its texts and its pictures, and the page draws them; no coordinate is computed here. The game's
inheritance rules are this package's own, read from `game`'s GUI and coat rows. What a file defines, and which
definition the game keeps, come from the index the file is drawn from (`index.FindWinner`, `index.FindKept`,
`index.FindLoaded`); what a data call reads and returns comes from the schema; every range is `lsp`'s.

**The index a file is drawn from** (`resolveIndex`). A file of the game is drawn from the game's own index
(`Session.Install`), so its types, templates, pictures, colors, fonts and texts are the game's, as the game draws it
with no mod loaded. Any other file is drawn from the mods' index, on top of the game's. Every entry point for a file
picks its index there: `ResolveWindow`, the states, `ResolveCoat`, `ListDrawables` and `ListPictures`.

**Each thing sent once.** A window's properties and the textures laid over its widgets are tables on the `Window`
(`Properties`, `Overlays`); a widget and an overlay hold the positions of their own, as a widget holds its place's.
A widget's row (`game.WidgetType`) leaves its empty fields out.

| file | holds |
|---|---|
| `gui.go` | the package comment; `Service`; `Property`, `Widget`, `Overlay`, `Window`; `ListDrawables`, `ResolveWindow`; the index a file is drawn from and the window the service keeps; the `resolver` of a window: types, templates, labeled blocks, the opening animation, its overlays and its holders, the tables of properties and overlays; the texts and their samples (`library.sampleText`, `library.sampleCall`); the edits `EditProperty` and `AddInstance` |
| `states.go` | `Group`, `States`; `EvaluateStates`, `Reveal`; the `opening` and the `evaluator`: the groups a window's conditions read, the data contexts, the values a window opens with, the data models' sizes, the presses, and the GUI's variable system |
| `choices.go` | `Choice`, `Row`, `PictureList`, `Picture`; `ListPictures`; the groups of picture lists, the choices between two values, the rows of a data model's list, the colors a pick gives |
| `textures.go` | the `library` of what definitions show: the image file of a written path, an icon name or a texture binding; the pictures the defines name; the placeholders; the colors of definitions; `ExportPNG` |
| `coat.go` | `Instance`, `Blend`, `Recolor`, `Shading`, `Layer`, `Coat`, `Image`, `NamedColor`; `ResolveCoat`; `ListColors`, `ListTextures`, `ListFrames`; the edits `AddLayer`, `MoveLayer`, `RemoveBlock`; `CreateCoatFile`, `CopyCoat` |
| `fonts.go` | `Font`: each style of a font a window names, with the file the game reads it from in the session's language |

## Edits, not writes

A method that changes an existing file does not write it. It returns `[]lsp.Edit`, which the editor applies to its own
model, so there is only ever one copy of the text and undo goes through the editor. Each edit sits beside what it
edits: `EditProperty` and `AddInstance` in `gui.go`, the layer edits in `coat.go`. `EditProperty` replaces a value where
the block writes the key, and otherwise adds the key as the block's last statement. A block is named by the range where
it starts. Only `CreateCoatFile` and `CopyCoat` write, since they make a new file, and `ExportPNG` writes a PNG the page
made to a file the user chose.

## Windows

`ListDrawables` lists what a GUI or coat file defines that the view can draw: a window, a type, a template or a coat,
nested ones included. The entry the view opens on comes first (`findFirstDrawn`): the one named as the file, else its
first window (`game.GUI.ShownFirst`), else its first type or template that draws something (`drawsSomething`: a size,
a picture, a text or widgets of its own); the rest follow in document order. A window at the top of a file that writes
no name is named after its file where it is the file's only keyed block, as the walk names it, so the view lists it
and `ResolveWindow` finds it by that name.

`ResolveWindow` resolves one window. A widget's properties come from its type's chain, its templates and its own block,
and the last written of each key wins, keys matching without regard to case. A labeled block is read as written, or as
the widget's `blockoverride` replaces it, whether either is written `block "x" { }` or `block = "x" { }`
(`findLead`). Of several definitions of a type or a template, the one the game keeps wins,
as it would if the game read the file being drawn; a file another file at its path replaces is drawn as if the game
read it. A `Property` carries where it is written (`Origin`: widget, template or type) and the type or template whose
block wrote it (`Writer`). A type or template no file defines is a property with the origin `missing`, so the page can
flag it. A constant, an expression or a `GetDefine` call is replaced by what the game reads. A window's `Holders` are
the game windows it is laid out in, each its own widget alone. Its `Conditions` are, by widget, the conditions each is
shown on, as written, and its `TextFont` and `TextSize` the font and size of the game's own type of a text
(`game.GUI.Fonts.TextType`), which a text with none of its own is drawn in.

**Opening animations** (`resolveOpening`). A widget opens as it looks once the animation it opens with has run. From
the states it opens in, and those its holders' opening runs on it by name, each state gives its end values and the calls
it runs as it starts, then leads to the state its `next` names and to those its calls run by name (`TriggerAnimation`
on the widget, or on a child that `FindChild` finds, which opens with it; `game.GUI.Animation`). A state that fades the
widget out and runs calls as it ends takes it off the screen (`isLeaving`), so it is left out with all that would
follow it. Each state runs once. A labeled block in a state is read in its place.

## Texts and their samples

A widget's `Texts` hold, by property key, the plain text and the data calls of each text it shows. A localization key
becomes its message's text in the session's language, each `$key$` inside it replaced by its own message's text.

A data call in a text shows sample text of its kind, in place of data the running game would give (`sampleCall`). The
kind comes from the type the call's last function returns, as the dumps declare it, after the call's macros are
expanded and its data contexts followed. `game.GUI.CallKinds` maps a type to `number` or `text`:

- A number is digits shaped by the call's format codes. A digit among them is how many decimals it has,
  `game.GUI.PercentCode` makes it a percent, and `game.GUI.SignCode` shows its plus sign.
- A call that `game.GUI.DateCalls` knows, a `CString` call whose last function's name ends in `Date` or `DateString`,
  shows a date in the game's own form (`game.GUI.DateForm`), its month in the session's language.
- A text is lorem words, as many as the widget's box holds: two or three on one line, a sentence or two where the box
  has several lines and resizes to fit them, and a paragraph where its size is fixed.
- A call whose last function the dumps do not declare shows two words of text.

The text of the call itself seeds the sample, so the same call always shows the same sample. Each sample's kind,
number, date or text, is written in its `loc.Segment.Kind`, and the page colors a sample by it. A sample that follows
text or another sample with no space between them starts with one, since the game's own functions bring their
spacing; a sample at the start of the text, or after a space or an opening bracket, has none.

## Pictures

The `library` finds the image file a picture leads to, and keeps what it looks up as it first needs it. A texture
written as a path is a file under the game's gfx folders. An icon name is looked up in the icon folders that the
game's defines name, and in a folder's overrides file. A texture binding reads a field of a definition: its last step
reads a field of the object the step before leads to (`[GovernmentType.GetRealmFrame('(int32)62')]` reads
`realm_frame` with the argument 62), or its name's first words name a kind of object (`GetConceptTexture('tax_base')`
reads the texture of the concept `tax_base`). A definition's picture is, in order, the field the binding reads, the
file the defines for its kind and field name (`<KIND>_<FIELD>_PATH`, `_PREFIX` and `_SUFFIX`, with a folder's
`_default`), its picture by the game's picture keys or its name as an icon, and the one image it writes. A suffix
define the game writes under another stem is read there (`game.Game.GUISuffixStems`), as CK3 writes its realm mask's.

A mask, a coat of arms' or the widget's own (`game.GUI.MaskKeys`), written as a path or as a binding, is found as a
picture is, with no placeholder of its own; it is no texture of the widget's, and the page cuts the placeholder to its
outline.

A binding of a kind of level (`game.GUI.Levels`), such as `GetPrestigeLevelTexture`, draws the first file, in name
order, of the folder that the kind's define names (`PRESTIGE_LEVEL_PATH`) whose name starts with `icon_` and the kind's
other words (`icon_prestige_`); where the folder has none, it draws its placeholder.

Where a binding names no definition of its kind, the widget draws the first definition that shows a picture, and its
`Pictures` is a `PictureList` the panel picks among. `ListPictures` lists the pictures a list may draw.

**Placeholders.** A picture that leads to no file draws a shape by what it is of. `describePicture` writes a phrase of
the picture's key, the object its binding reads and the step without its getter (`Location.GetRankIcon` under
`texture` is "texture location rank icon"), or the path's folders and name for a picture written as a path.
`game.Game.FindPlaceholder` picks the first placeholder, the game's own first, one of whose kinds the phrase holds as
whole words, and the plain icon where none does. A placeholder that only the running game can fill, such as a portrait
or a coat of arms, looks for no file at all. A background, which only dresses its holder, gets none. The widget's
`Placeholder` is the shape's name, and the page tints it.

## Colors from definitions

A binding whose last function returns a color (`game.GUI.ColorType`) gets the color of a definition (`findColor`). It
is the color that the field the binding reads (`GetColor` reads `color`) writes in the definition the binding names, or
else in the first definition of its kind that writes one (`findFirstColor`), looked for once per window and kind, and
only where the game's own definitions write the field (`writesField`). A binding that names a definition of its kind
never takes another's color. The field is a color block, or the name of a definition that is one, such as a named
color. The widget's `Colors` hold the CSS color by property key. When a picture group picks a definition of a kind,
the bindings of that kind's color take the picked definition's color too (`pickColors`).

**Rows from definitions.** A data model's list that reads a database draws one `Row` for each of the database's first
definitions, in load order and each name once, as many as the workspace's "Rows per list" setting (`draw.rows`) says,
the same count the page draws a list's sample rows by. `ResolveWindow` finds them once per window (`Window.Rows`), so
a state change does not list them again. The database is that of the first binding among the item's widgets that
reads its object as an element (`readsElement`): the binding starts at a data type that no data context of the window
reaches, as `[Trait.GetIcon]` does inside a list of traits. Each row gives the widgets that read the database the
pictures and colors of its definition, and their texts keep their samples. A list whose item reads no database draws
the page's sample rows.

## States

`EvaluateStates` works out which widgets of a window are hidden, and `Reveal` which picks make a hidden widget show. A
`Group` is one choice the panel lets the user switch: a data call the window's conditions read, a coat's template
list, or a list of pictures. Its value is a pick, the value chosen for its id; `*every` stands for every value at once
and `none` for none. The `evaluator` reads every boolean data function the dumps declare, any arity, through And, Or,
Not and Nor. A call that reads the object of a data context is written from the window down to that object, so the
same call about two objects is two groups.

**Once per window.** The service keeps the window it resolved last as an `opening`: its index, file and name, and its
evaluator at the values it opens with, its groups classified. `ResolveWindow` always resolves anew and replaces it; a
change of states starts from it (`applyPicks`) and resolves the window again only where its index, its file, its
name or the rows setting differs. A group's fixed fields are the `Window`'s (`Groups`), and `States` carries what the
picks change: each group's value (`Values`), the hidden widgets, what each group decides and the choices.

**Data models' sizes** (`computeFact`). As the window opens, each data model's list draws "Rows per list" rows, so
`GetDataModelSize` is that number, `IsDataModelEmpty` is false, and a comparison of numbers (`game.GUI.Comparisons`) is
worked out from them. Such a fact is no group.

A window opens with every group at none, except what holds as a window opens (`holdsOnOpening`) at every value, however
far the object is reached: an object exists or a list has items (`game.GUI.OpeningFacts`), and the facts of the sample
(`game.GUI.SampleFacts`), the player's own living, landed ruler as in the owner's `character-window.png`, who shows
their legitimacy and sits on a council they may change as in `council-window.png`. A name joins the sample only where a
screenshot shows that it holds. A test of what the window shows holds and the test of its absence fails, so the two
arms of a yes/no pair never draw together. Then what its opening states' calls set, and the first widget of each strip
of tabs pressed. A `DataModelHasItems` call (`game.GUI.ModelHas`) is worked out from the rows, as `IsDataModelEmpty` is. A press runs the widget's clicks: a call such as `SelectTab('x')`
selects `x` of the group on the same object, and a call to the GUI's variable system sets, clears or toggles a
variable. A property written as a choice between two values, a function named with `game.GUI.SelectPrefix` such as
`Select_float(cond, a, b)`, takes the value its condition gives, as a `Choice`.

**The variable query.** `game.GUI.Variables` names the functions of the GUI's own variable system, each taking the
variable's name first, and a variable's name stays part of its group. `UnsetOrValue`, EU5's
`NotExistOrHasValue('x', 'v')`, holds while a variable is unset or set to the given value. `expandVariableQuery`
reads it as `Or(Not(Exists('x')), HasValue('x', 'v'))`, so it asks the same two groups that `Exists` and `HasValue` ask.

**Tiers.** Where the game draws a title's tier from a sheet of cells (`game.Game.GUITiers`, CK3's `GetTierFrame`),
each call of that function is a group whose options are the game's tiers, with the widgets that write it as its
readers (`listTierGroups`, found once per window). A tier picked gives each reader's property its cell, counted from
one (`pickTiers`).

## Coats of arms

`ResolveCoat` resolves a coat into its layers on its field, from the `game.Coat` rows: the pattern, each emblem and each
sub-coat, a parent coat's statements under the coat's own. A `Layer` carries its texture's file, its colors as CSS with
their names, how the game recolors its texture (`Recolor`), its mask and regions, and its `Instance`s, each a placement
with the game's defaults where the file sets none and its rank in the order the coat draws them. A value drawn from a
template list draws the option picked for that list, else its heaviest option. The `Coat` carries its field's size,
the game's `Shading` and its color slots. `ListColors`, `ListTextures` and `ListFrames` give the panel the named colors,
the textures of a role and the designer's frames.

## Terms

- **Window**: one GUI window resolved for drawing.
- **Widget**: one element of a resolved window, with everything needed to draw it.
- **Property**: one property of a widget, with where it is written.
- **Overlay**: one texture laid over a widget's own, as a `modify_texture` block writes it.
- **Group**: one choice the page lets the user switch.
- **Pick**: the value chosen for one group, by its id.
- **States**: one way of drawing a window: as it opens, changed by the picks.
- **Choice**: the value the picks give one property of a widget, with what it leads to.
- **Row**: one row a data model's list draws for one definition.
- **PictureList**: where the pictures are listed that a picture binding may draw.
- **Picture**: one picture a picture binding may draw.
- **Placeholder**: the shape drawn where a picture has no file.
- **Coat**: a coat of arms resolved for drawing, as its layers.
- **Layer**: one layer of a coat: the pattern, an emblem or a sub-coat.
- **Instance**: one placement of a layer on the field.
- **NamedColor**: one of the game's named colors, for the color picker.
- **Font**: one style of a font a window's texts are written in, with its file.
