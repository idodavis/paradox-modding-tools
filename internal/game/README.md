# game

The only game-aware package: the table of supported games, the shared tables of what every game's script, GUI and
coat of arms files mean, and where the games are installed.

Every fact is a row of a table or a field of a game's row, written as a literal: no row is built by a function. A
column whose values are words lists them in its comment and names the one function that reads them, so the count of
readers that switch on a word is the count of word columns, each in one place. A row's evidence (a `.info` file, the
wiki, the executable, a shader) is its trailing comment; nothing reads it at run time. The
`Source` type ranks the sources the schema learns claims from and is the schema's, not the rows'.

| file | holds |
|---|---|
| `game.go` | `Game`: a game's names, wiki, stages in the order it mounts them, descriptor kind, Steam app id and library, new-mod folders, how each OS starts it (`Program`), its error log (`ErrorLog`), and what is its own beside the shared tables: its `Databases`, `Nestings`, `Precedences` and `References`, the top folders whose databases keep every copy (`MergedRoots`), its forms of the localization replace folder in the order they win (`LocReplaceFolders`), whether it logs a localization key a mod and the game both write outside a replace folder (`LogsRepeatedKey`), the fields whose names fire (`FiringFields`) and the effects that run a decision (`DecisionEffects`), its `Events` (the root scope's key and the `Picture`), its `EntryModes` and `ModeFolders`, its concept prefix, its own `Placeholders` (its portrait's silhouette and its coat's shape), the form a coat's layer writes a new color in (`CoatColorForm`), its GUI holders, undeclared types and hidden-room types, its picture suffix stems (`GUISuffixStems`), the define group whose defines say where its icons and pictures are (`GUIDefineGroup`) and title tiers (`GUITiers`), its coat folder, shading and designer frames (each frame's defines named in full); `Source`; the install roots the game mounts (`InstallRoots`); `LookupGame`, `ListGames` |
| `databases.go` | the tables of databases, one per kind of fact: `Databases` (where definitions are written and how named), `Nestings` (databases inside another's definitions), `Callables` (what script and GUI files call by name), `FileTypes` (what an extension says), `Precedences` (how definitions combine), `References` (what key paths name; a game's own beside them), `EntryModes`, the `Picture` chains, the databases no definition carries, `Constant` (the file's own `@name` constants), `ClassDatabases`, the fill-ins, each game's firing fields (`ck3FiringFields`, `vic3FiringFields`), the words for the names script saves and for the player messages of `common/messages` (`definitionWords`); `ToDatabase`, `FindDatabase`, `FindNestings`, `MatchKeyPath` (a key path's segments, globs, `<number>` and `**`), `Precedence.KeepsEvery`, `LookupPrecedence`, `SplitMode`, `LookupEntryMode`, `DescribeDefinition` |
| `language.go` | the language: `Shapes` (how a block of each shape reads its keys, by the `KeyRules` words) and the `Shape…` names Go code writes (`ShapeUnknown`, `ShapeFields`, `ShapeArguments`, `ShapeTriggers`, `ShapeEffects`, `ShapeValue`, `ShapeWidget`), `Keywords` (the keys the language gives a shape to and its operators, each with the sentence a hover card shows), `ScopeLinks` (root, prev, this), `LinkMakers` (for each link of the dumps that reads a name back, the commands that make its names), `DocTypes` (the docs' type words), the dumps' words, `Bools`; `LookupKeyword` (a number or a date fits `<number>`, `IsNumberKey`), `LookupLinkMaker`, `MatchListIterator`, `IsOperator`, `ClassifyKey` |
| `gui.go` | `GUI`, one literal: which words declare a GUI file's databases and what their names start with (`DatabasePrefix`, `WidgetDatabase`), which keys name its widgets, the slots, how each built-in `WidgetType` draws, the opening states, the hidden keys, the calls the Visual view answers, the GUI's own variables (`VariableSystem`), the text keys and the keys that let a text run over several lines and resize its box, what a data call in a text shows by the type its last function returns (`CallKinds`), the calls that write a date (`DateCalls`) and the games' date form (`DateForm`), the format codes that shape a number (`PercentCode`, `SignCode`), the data type of a color (`ColorType`), the keys of a mask (`MaskKeys`), how a state leads to others (`Animation`), the functions that compare numbers and give a data model's size (`Comparisons`, `ModelSize`, `ModelEmpty`), the font rules with the size key and the type of a text (`Fonts`), the picture keys and the defines that say where a picture is (`_PATH`, `_PREFIX`, `_SUFFIX`, a folder's `_default`); `Placeholders`, the shape drawn where a picture has no file by the kind it is of, and `FindPlaceholder`; `TierFrames` |
| `coat.go` | `Coat`, one literal: the databases a coat reads (coats, named colors, template lists), its texture folders, which way depth and rotation turn, how each texture kind is colored (`CoatRecolor`), the mask regions, the color slots, the placement keys, how a key is edited, what a new coat writes, the frame mask |
| `colors.go` | the `Colors` rows, `ParseColor` and `EncodeColor`; `ColorForm`, how a color is written that a block does not write yet |
| `install.go` | where a game is on this machine: its installs through Steam's library folders, its version (the launcher's settings, else the revision log of the game's last start when that came after the install's last update, else Steam's build), its user-data and dump folders, its descriptor file, when an install was last updated, and the program that starts it in debug mode (`ResolveProgram`); `DLCFolder` |

## Lookups

A game's own rows are tried before the shared table of the same name: `FindDatabase(folder)` reads the game's
`Databases` then `Databases`; `FindNestings(rel)` concatenates; `LookupPrecedence(database, folder)` reads the game's
`Precedences`, then `Precedences`, then `mergedPrecedence` for a database under one of the game's `MergedRoots` (CK3's
history, EU5's setup), then `modePrecedence` where the folder takes entry modes (`ModeFolders`), then
`defaultPrecedence`. Each precedence row's comment cites its source: a file of the install and its line, a message of
the executable, or a wiki page and its section or line. `Precedence.KeepsEvery` says whether a precedence keeps every
copy. A database's name is derived in one place, `ToDatabase`: the path without `common/` and without a file's
extension; a nested database is its parent's name, a dot and its own. `Callables` and `FileTypes` are maps the reader
indexes directly.

## The words

Each word column's comment lists its words and its reader. `Shape` (on a `Database`, a `Callable`, a `FileType`, a
`Keyword`, the schema's claims and the walk's context) is a `Shapes` key: fields, triggers, effects, arguments, value,
unknown, setup, widget, description; `enclosing` in a keyword means the holder's. `KeyRule`'s three columns are the
walk's: where the block's fields come from, what decides its shape, how its scope follows. `ShapeRule.Values` and
`ShapeRule.Blocks` are the walk's: the database every value in a block of the shape names, and the shape of a block
whose key gives none. `Precedence.Keeps`, `Modes` and `Injects` (what a mode that adds does: adds, `replaces`, or
`unsettled` where the wiki lists a folder as REPLACE only) are `index.FindWinner`'s; `Precedence.Contested`, the
blocks two merged copies cannot both write (an on_action's trigger and effect), is the Health service's.
`Database.NamedBy` is the walk's and the scan's (`call` names a definition by the first step of the call its
`NameKey` writes), and so are `Database.RunOrder` (the order the walk follows to know the scopes a block starts
with), `Database.AliasKeys` (the keys whose value is a second name of the definition, which the index records as an
alias and the iterators over a geographic region are named by), and `Database.Prefixes` with `Database.Separator`
(the link prefixes that name a definition, and the character between a definition's name and a key nested in it: a
prefixed reference ends at it, and a nested definition is named across it, as the define `NAI|X`). `DocType.Values`
marks the docs' terms that take a script value. `WidgetType.Holding` and `WidgetSlot.Role` are `picture.ts`'s.
`CoatPlace.Part` is `coat.ts`'s. `Keyword.Shows` is the walk's, and so is `Keyword.Preview`, which marks a block shown
in the tooltip and never run. A game's `FiringFields` and `DecisionEffects` are `index`'s: the
key paths of its databases' own fields whose names fire as they would in effect script, and the effects its dumps
say run a decision. `GUI.CallKinds` (number, text) is
draw's `sampleCall`'s, which writes each sample's word in its `loc.Segment.Kind`, date for a call `GUI.DateCalls` knows;
`placeholders.ts` colors a sample by it.

`DescribeDefinition` calls the four databases of names that script saves (`SavedScopes`, `Variables`,
`LocalVariables`, `GlobalVariables`, the keys of `LinkMakers`) by `definitionWords`: saved scope, variable, local
variable and global variable. The same table calls the `messages` database of `common/messages` a player message. The schema keys its `TypeDatabases` by the same words, so the dumps' `Scope` data type
names no database. Any other database is called by the last part of its name with spaces for underscores, and
`plurals` turns its ending between the singular and the plural, the first row that fits winning: `scripted_effects`
gives scripted effect, and `gui.type` gives types.

## The Visual view's facts

`GUI.DateCalls` knows a data call that writes a date by the type its last function returns and the ending of the
function's name: the dumps declare such calls as text, so the name is all that says they write a date. `GUI.DateForm`
is the games' date form (`DATE_FORMATTING_LONG`), the names it fills in for the day, the month and the year, and the
keys of the months' names. A game's `GUITiers` names the function that gives the cell of a title's tier in a sheet of
tiers, and the tiers in the order of the cells (CK3's `GetTierFrame`, unlanded to hegemon). Its `GUISuffixStems` reads
a picture's suffix define under the stem the game names it with (CK3's realm mask,
`GOVERNMENT_TYPE_REALM_FRAME_MASK_SUFFIX`). `GUI.MaskKeys` is the keys whose image cuts a widget's picture to its
shape: a coat of arms' mask and a widget's own. `GUI.Animation` names a state's `next` key, its `on_finish` key, the
alpha key, and the functions that run a state by name on a widget or a child (`TriggerAnimation`, `FindChild`).
`GUI.Comparisons` gives, for each function that compares two numbers, the results of comparing them for which it
holds; `GUI.ModelSize`, `GUI.ModelEmpty` and `GUI.ModelHas` are the functions that ask a data model's size, emptiness
and items. `GUI.OpeningFacts` names the functions that hold as a window opens (`IsValid`, `HasItems`), and
`GUI.SampleFacts` those that hold of the sample it is drawn for, the player's own living, landed ruler (`IsAlive`,
`HasLandedTitles`, `ShouldShowLegitimacy` and the council's), each added where an owner's screenshot shows it. The draw
service opens their groups at every value. `GUI.Levels` says how a kind of level, whose last word is `level`, finds its
pictures: the folder is the kind's `<KIND>_PATH` define, and the picture the first file in it, in name order, that
starts with `icon_` and the kind's other words.
`GUI.Fonts.TextType` is the built-in type of a text, whose own type in the game's files gives a text with no font of
its own its font and size (`Fonts.SizeKey`).
A game's `CoatColorForm` is the form PMT writes a new color of a coat's layer in, the one the game's own coats use:
rgb out of 255 in CK3, hsv360 in Vic3 and EU5. The game reads any color form.

## The map

The map's files are rows like any other. CK3's `map_data/definition.csv` is a table (`NamedBy` column): the first
column of each row, split by `TableSyntax.Separator`, names a province, and a line that opens with
`TableSyntax.Comment` is a comment. Its `history/provinces` and `history/province_mapping` hold the history of those
provinces (`Target`). Vic3's `map_data/province_terrains.txt` matches its names without regard to case
(`FoldsCase`), as its state history writes some of them in lower case. EU5's `map_data/definitions.txt` nests its
geography five deep, each level a database of its own (`eu5Nestings`): continent, sub_continent, region, area and
province. Its
`map_data/location_templates.txt` is history of the locations `map_data/named_locations` names, and the start setup's
locations, roads and town rights set up those locations too. The map objects of `gfx/map` name nothing (CK3 and Vic3
`map_object_data`, EU5 `map_objects`); EU5's `gfx/map/city_data` and Vic3's `gfx/map/post_effects` are named by their
`name` field.

## The GUI's variables

`GUI.Variables` names the functions of the GUI's own variable system, each taking the variable's name first.
`UnsetOrValue`, EU5's `NotExistOrHasValue('x', 'v')`, holds while a variable is unset or set to the given value. The
draw service reads it as `Or(Not(Exists('x')), HasValue('x', 'v'))`, so it asks the same two groups that `Exists` and
`HasValue` ask.
