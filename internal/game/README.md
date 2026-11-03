# game

The only game-aware package: the table of supported games, the shared tables of what every game's script, GUI and
coat of arms files mean, and where the games are installed.

Every fact is a row of a table or a field of a game's row, written as a literal: no row is built by a function. A
column whose values are words lists them in its comment and names the one function that reads them, so the count of
readers that switch on a word is the count of word columns, each in one place. A row's evidence (a `.info` file, the
wiki, the executable, a shader) is its trailing comment; nothing reads it at run time. The `Source` type ranks the
sources the schema learns claims from and is the schema's, not the rows'.

**Where to look.** A value every game shares is a package value in its topic's file, read directly (`GUI`, `Coat`,
`Keywords`, `EventDelay`). A value that differs by game is a field of that game's row in `game.go`, in its topic's
part where it has one (`Game.GUI`, `Game.Coat`, `Game.Events`). A table is in its topic's file, and each row names the
games it holds for (`Games`, none meaning every game, as `Game.Matches` reads it).

| file | holds |
|---|---|
| `game.go` | the package comment; `Source` and its constants, `CanAccuse`; `Program`, `Log`; `Game`: a game's names, stages in the order it mounts them (`StageRoots`), descriptor, thumbnail folder, Steam app id and library, new-mod folders, how each OS starts it (`Program`), the forms in which its error log names a file's line (`ErrorLocations`), its `Events` (a `GameEvents`), whether its definitions take entry modes (`TakesModes`), its concept prefix, its forms of the localization replace folder in the order they win (`LocReplaceFolders`), whether it logs a localization key a mod and the game both write outside a replace folder (`LogsRepeatedKey`), the effects that run a decision (`DecisionEffects`), the localization key of a message's title where it writes none (`MessageTitle`), its bookmarks' folder and the key that names a start setup (`BookmarkFolder`, `SetupKey`), its `GUI` (a `GameGUI`), its `Coat` (a `GameCoat`), and its `Guide`; `Guide`; the three rows; `ScriptRoot`, `InstallRoots`, `BaseLanguage`, `OriginVanilla`, `MetadataDir`, `PictureExtensions`, `WikiAPI`; `ErrorLog` and `MountLog`, `DumpFolders`; `LookupGame`, `ListGames`, `Matches`, `SplitStage`, `FindLocReplacement`, `IsUnmounted` |
| `databases.go` | the row types `Database`, `Contents` and its constants, `Nesting`, `Callable`, `FileType`, `Precedence`, `Reference`, `EntryMode`, and the picture types `PictureStep`, `PictureChain`, `Picture` with `GameEvents`; the tables `Databases`, `Nestings`, `Callables`, `FileTypes`, `Precedences`, `References`, `FiringFields`, `EntryModes`, `FireContents`, `ClassDatabases`, `MisspelledFolders`; the databases no definition carries (`Localization`, `File`), `CustomLocalization`, `Constant`, `GlobalType`, `AnyScopeType`, `SelfFunctions`; the event keys (`EventNamespaceKey`, `TitleKey`, `DescriptionKey`, `OptionKey`, `OptionTitleKey`, `LabelKey`, `HiddenKey`), `EventDelay`, `GameRules`, `CustomTexts`, `TableSyntax`, `SetupFolder`; the words for some databases' definitions (`definitionWords`); `ToDatabase`, `FindDatabase`, `ListDatabaseFolders`, `FindNestings`, `ListNestings`, `ListSetupNestings`, `ListReferences`, `ListFiringFields`, `Precedence.KeepsEvery`, `LookupPrecedence`, `SplitMode`, `LookupEntryMode`, `DescribeDefinition` |
| `language.go` | the language: `ShapeRule` and `Shapes` (how a block of each shape reads its keys, by the key rules' words) with the `Shape…` names Go code writes (`ShapeUnknown`, `ShapeFields`, `ShapeArguments`, `ShapeEffectArguments`, `ShapeTriggerArguments`, `ShapeTriggers`, `ShapeEffects`, `ShapeValue`, `ShapeWidget`, `ShapeModifiers`, `ShapeDescription`); `Keyword` and `Keywords` (the keys the language gives a shape to and its operators, each with the sentence a hover card shows), `LimitKey`, `TriggerKey`, `OperatorSays`; `AnyCall`, `AnyKey`, `DocDateKey`, the dumps' `Declared…` words; `Links` and their words (`SavedScopes`, `Variables`, `LocalVariables`, `GlobalVariables`, `Lists`, `Flags`), `ScopeLinks`, `LinkMakers`, `LinkUsers`, `LocStems`, `CommandKeys`; `ConceptFormat`; `DocType` and `DocTypes`; `DumpPhrases`; `Bools`; `LookupKeyword`, `IsOperator`, `MatchListIterator` |
| `gui.go` | `GUIRules` and its one row `GUI`: which words declare a GUI file's databases and what their names start with, which keys name its widgets, the slots, how each built-in `WidgetType` draws, the opening states, the hidden keys, the calls the Visual view answers, the GUI's own variables (`VariableSystem`), the text keys and the keys that let a text run over several lines and resize its box, what a data call in a text shows (`CallKinds`, `DateCalls`, `DateForm`, `PercentCode`, `SignCode`), the data type of a color (`ColorType`), the keys of a mask (`MaskKeys`), how a state leads to others (`Animation`), the functions that compare numbers and ask a data model's size (`Comparisons`, `ModelSize`, `ModelEmpty`, `ModelHas`), the facts of a window's opening and of its sample (`OpeningFacts`, `SampleFacts`, `SampleAbsent`), the font rules (`FontRules`), the picture keys and the defines that say where a picture is, the pictures of a kind of level (`Levels`); `GameGUI`, what one game does beyond them; `TierFrames`, `Illustrations`; `Placeholder`, `Placeholders` and `FindPlaceholder` |
| `coat.go` | the coat types (`CoatStrength`, `CoatLerp`, `CoatRecolor`, `CoatShade`, `CoatSlot`, `CoatPlace`, `CoatPart` and its constants, `CoatFrame`, `CoatOverride`, `CoatTemplate`); `CoatRules` and its one row `Coat`: the databases a coat reads, its texture folders, how each texture kind is colored, the mask regions, the color slots, the placement keys, how a key is edited, what a new coat writes; `GameCoat`, what one game does beyond them: its coat folder, shading, designer frames and the form of a new layer color |
| `install.go` | where a game is on this machine: `DetectedInstall` and `FindInstalls`, its installs through Steam's library folders; `LoadVersion` (the launcher's settings, else the revision log of the game's last start when that came after the install's last update, else Steam's build); `ListInstallRoots`; its user-data and dump folders (`FindUserDataDir`, `FindDumpDirs`); when an install was last updated (`ReadInstallUpdatedAt`) and its build's checksum (`LoadChecksum`); the program that starts it in debug mode (`ResolveProgram`); `DLCFolder`, `LauncherModFolder` |

## Lookups

A table's rows for a game are those whose `Games` name it, or that name none. `FindDatabase(folder)` returns the first
row of `Databases` for the game, a game's own rows written before every game's; `FindNestings(rel)` and
`ListNestings` read `Nestings` the same way, and `ListReferences` gives the rows of `References` for the game, those
of GUI files first, then every game's, then its own. `LookupPrecedence(database, folder)` returns the first row of
`Precedences` that holds for the game and the database, by the database's name (or a pattern `path.Match` reads, as
`setup/*`) or by a folder at or above its own; where none holds, the last copy by file name wins. Each precedence
row's comment cites its source: a file of the install and its line, a message of the executable, or a wiki page and
its section or line. `Precedence.KeepsEvery` says whether a precedence keeps every copy. A database's name is derived
in one place, `ToDatabase`: the path without `common/` and without a file's extension; a nested database is its
parent's name, a dot and its own. `Callables` and `FileTypes` are maps the reader indexes directly. `SplitMode` splits
an entry mode off a name only in a game whose row `TakesModes`.

## The words

Each word column's comment lists its words and its reader. `Shape` (on a `Database`, a `Callable`, a `FileType`, a
`Keyword`, the schema's claims and the walk's context) is a `Shapes` key: fields, triggers, effects, arguments, effect
arguments, trigger arguments, value, unknown, setup, widget, modifiers, description; `enclosing` in a keyword means the
holder's. `ShapeRule.Rules` are the key rules a block of the shape tries in order, the first that fits winning, read
by the walk's `matchRule`: `known-call`, `declared`, `documented`, `command`, `operator`, `link`, `modifier`,
`modifier-type`, `observed`, `field`, `field-or-call` and `call`. The two argument shapes are the block of an effect
and of a trigger: what the command takes, and the commands of its own kind it runs. `ShapeRule.Values` and
`ShapeRule.Blocks` are the walk's: the database every value in a block of the shape names, and the shape of a block
whose key gives none. `Precedence.Keeps`, `Modes` and `Injects` (what a mode that adds does: adds, `replaces`, or
`unsettled` where the wiki lists a folder as REPLACE only) are `index.FindWinner`'s; `Precedence.Contested`, the
blocks two merged copies cannot both write (an on_action's trigger and effect), is the Health service's.
`Database.NamedBy` is the walk's and the scan's (`call` names a definition by the first step of the call its
`NameKey` writes), and so are `Database.RunOrder` (the order the walk follows to know the scopes a block starts
with), `Database.RootKey` (the key whose value names the scope a definition's script starts in, as CK3's events write
`scope`), `Database.AliasKeys` with `AliasTarget` (the keys whose value is a second name of the definition, or of the
database `AliasTarget` names, which the index records as an alias), and `Database.Prefixes` with `Database.Separator`
(the link prefixes that name a definition, and the character between a definition's name and a key nested in it: a
prefixed reference ends at it, and a nested definition is named across it, as the define `NAI|X`).
`Database.Contents` (`Contents`) is what a database holds beyond definitions: the schema finds the one database of
each such kind by it, and `FireContents` names the kinds that fire and are fired. `Database.BoolKey` is the schema's:
the key a definition writes as yes to say that the name it defines takes yes or no where it is written, and a number
otherwise, as the modifier types' `boolean` does; such a claim's source is `SourceDefinitions`, which can accuse.
`DocType.Values` marks the docs' terms that take a script value. `WidgetType.Holding` and `WidgetSlot.Role` are
`picture.ts`'s. `CoatPlace.Part` is draw's `setPlacement`'s. `Keyword.Shows` is the graph detail's. `Keyword.Preview`
is the walk's, and marks a block shown in the tooltip and never run; so are `Keyword.Values`, the database of the text
a keyword shows (`custom_tooltip`'s localization key), and `Keyword.Entries`, the database of the entry its text key
names by what declares the keyword, which the game does not need defined (`custom_description`'s effect and trigger
localizations). `Keyword.Follows` is lsp's. `Keyword.Weighted` and `Keyword.Modifies` are the graph's steps.
`Keyword.Says` and `Keyword.Covers` are the graph's conditions in words: what a keyword reads, a `*` standing for the
rest of a prefix key ("any * where"), and a keyword whose own text the game shows in place of what it holds
(`custom_tooltip`); so are `Keyword.ValueFirst`, a keyword whose value reads first ("X exists"), and
`Keyword.Negates`, a keyword true when what it holds is false, which around one condition reads as that condition
negated (`not`, `nor`, `nand`, `all_false`, `any_false`; the docs' "not - negates content of trigger"). A game's
`FiringFields` and `DecisionEffects` are `index`'s: the key paths of its databases' own fields whose names fire as
they would in effect script, and the effects its dumps say run a decision. `GUI.CallKinds` (number, text) is draw's
`sampleCall`'s, which writes each sample's word in its `loc.Segment.Kind`, date for a call `GUI.DateCalls` knows;
`placeholders.ts` colors a sample by it.

`LocStems` and `CommandKeys` are the walk's and the dumps reader's. A `LocStems` row is an argument whose value is the
stem of the localization key it names, as CK3's `add_activity_log_entry`'s `key` names `<key>_title`. A `CommandKeys`
row is a command whose block's keys, beside the arguments the dumps declare, are names of a database, as
`stress_impact`'s are traits. `DumpPhrases` are the dumps' own words the engine decides by: a blurb that opens with
`Iterates` and names no target walks a list, `NoScope` names no scope, and `Literal` is the scope of a link whose name
is a literal (`flag:the_boss`), whose names the vote for a prefix's database leaves out. `AnyScopeType` is the data
type of a scope of any type, whose getters give each type of scope its own data type, as `Scope.GetCountry` gives
`Country`. `CustomLocalization` is the database of the customizable localizations, each a block for each text it can
show (`CustomTexts`).

`DescribeDefinition` calls the six databases of names that script saves and reads back (`Links`) and the `messages`
database of `common/messages` by `definitionWords`: saved scope, variable, local variable, global variable, list, flag
and player message. The schema keys its `TypeDatabases` by the same words, so the dumps' `Scope` data type names no
database. Any other database is called by the last part of its name with spaces for underscores, and `plurals` turns
its ending between the singular and the plural, the first row that fits winning: `scripted_effects` gives scripted
effect, and `gui.type` gives types.

`Game.Guide` is how a game picks which copy of a file or a definition it keeps, in the sentences where the games
differ: what the load order decides, which of a mod's files the game reads, which copy each kind of definition keeps,
and what the load order changes. Health's compatibility page shows it.

## The Visual view's facts

`GUI.DateCalls` knows a data call that writes a date by the type its last function returns and the ending of the
function's name: the dumps declare such calls as text, so the name is all that says they write a date. `GUI.DateForm`
is the games' date form (`DATE_FORMATTING_LONG`), the names it fills in for the day, the month and the year, and the
keys of the months' names. A game's `GUI.Tiers` names the function that gives the cell of a title's tier in a sheet of
tiers, and the tiers in the order of the cells (CK3's `GetTierFrame`, unlanded to hegemon). Its `GUI.SuffixStems`
reads a picture's suffix define under the stem the game names it with (CK3's realm mask,
`GOVERNMENT_TYPE_REALM_FRAME_MASK_SUFFIX`), and its `GUI.DefineGroup` is the define group whose defines say where its
icons and pictures are (CK3's and EU5's `NGameIcons`). Its `GUI.Illustrations` is the key that names the folder of the
pictures a definition writes by a path below it, and the folder where it names none. `GUI.MaskKeys` is the keys whose
image cuts a widget's picture to its shape: a coat of arms' mask and a widget's own. `GUI.Animation` names a state's
`next` key, its `on_finish` key, the alpha key, and the functions that run a state by name on a widget or a child
(`TriggerAnimation`, `FindChild`). `GUI.Comparisons` gives, for each function that compares two numbers, the results
of comparing them for which it holds; `GUI.ModelSize`, `GUI.ModelEmpty` and `GUI.ModelHas` are the functions that ask
a data model's size, emptiness and items. `GUI.OpeningFacts` names the functions that hold as a window opens
(`IsValid`, `HasItems`), and `GUI.SampleFacts` those that hold of the sample it is drawn for, the player's own living,
landed ruler (`IsAlive`, `HasLandedTitles`, `ShouldShowLegitimacy` and the council's), each added where an owner's
screenshot shows it; `GUI.SampleAbsent` names the functions that reach an object the sample lacks
(`GetNextForcedCouncillor`). The draw service opens their groups at every value. `GUI.Levels` says how a kind of
level, whose last word is `level`, finds its pictures: the folder is the kind's `<KIND>_PATH` define, and the picture
the first file in it, in name order, that starts with `icon_` and the kind's other words. `GUI.Fonts.TextType` is the
built-in type of a text, whose own type in the game's files gives a text with no font of its own its font and size
(`Fonts.SizeKey`). A game's `Coat.ColorForm` is the form PMT writes a new color of a coat's layer in, the one the
game's own coats use: rgb out of 255 in CK3, hsv360 in Vic3 and EU5. The game reads any color form.

## The map

The map's files are rows like any other. CK3's `map_data/definition.csv` is a table (`NamedBy` column): the first
column of each row, split by `TableSyntax.Separator`, names a province, and a line that opens with
`TableSyntax.Comment` is a comment. Its `history/provinces` and `history/province_mapping` hold the history of those
provinces (`Target`). Vic3's `map_data/province_terrains.txt` matches its names without regard to case
(`FoldsCase`), as its state history writes some of them in lower case. EU5's `map_data/definitions.txt` nests its
geography five deep, each level a database of its own (EU5's rows of `Nestings`): continent, sub_continent, region,
area and province. Its `map_data/location_templates.txt` is history of the locations `map_data/named_locations` names,
and the start setup's locations, roads and town rights set up those locations too. The map objects of `gfx/map` name
nothing (CK3 and Vic3 `map_object_data`, EU5 `map_objects`); EU5's `gfx/map/city_data` and Vic3's
`gfx/map/post_effects` are named by their `name` field.

## The GUI's variables

`GUI.Variables` names the functions of the GUI's own variable system, each taking the variable's name first.
`UnsetOrValue`, EU5's `NotExistOrHasValue('x', 'v')`, holds while a variable is unset or set to the given value. The
draw service reads it as `Or(Not(Exists('x')), HasValue('x', 'v'))`, so it asks the same two groups that `Exists` and
`HasValue` ask.
