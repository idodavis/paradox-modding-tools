// game.go holds each game's row: the facts that are its own beside the shared tables.

// Package game holds every fact about the supported games: which games exist, where each is installed, and what
// its script, GUI and coat of arms files mean. Every fact is a row of a table or a field of a game's row.
package game

import "regexp"

// A Source is where a fact the schema learned came from. A lower value outranks a higher one, and a claim keeps its
// best source.
type Source uint8

// The sources, by rank: each outranks the ones after it.
const (
	SourceNone     Source = iota // no source
	SourceRegistry               // this package's rows: they classify and never accuse
	SourceDumps                  // the console dumps: what the game declares about its own language
	SourceExe                    // a message the game's executable carries
	SourceLog                    // a line the game's own logs write
	SourceDocs                   // the prose the install ships beside its script (.info, .md, readme.txt)
	SourceWiki                   // the community wiki, taken where no game source contradicts it
	SourceVanilla                // the install's own files: evidence, never a claim that accuses
	SourceInferred               // no source states it: written as inferred, it never fails a verdict
)

// A Program is how a game is started on one operating system: its executable and the flags for debug mode. A game
// whose row has no executable cannot be started.
type Program struct {
	Executable string   // from the install root (binaries/ck3.exe); "" means none is known, so the game is not started
	DebugArgs  []string // the flags that start it in debug mode, which hot-loads script
	Settings   string   // the launcher settings file whose exePath and debug arguments win; "" means none
}

// An ErrorLog is a game's error log: where it is written and how its lines are taken apart.
type ErrorLog struct {
	Rel      string         // under the user-data folder (logs/error.log)
	Line     *regexp.Regexp // a message's header: time, an optional [E], the source file and line, the message
	Location *regexp.Regexp // the script path and line a message names; nil where it names none
}

// Events holds a game's rules for events: where an event writes its root scope and how its picture is found.
type Events struct {
	ScopeKey     string  // the key whose value is the event's root scope; "" means none
	TypeKey      string  // the key whose value, less TypeSuffix, names the event's root scope; "" means none
	TypeSuffix   string  // dropped from TypeKey's value
	DefaultScope string  // the root scope where neither key is written
	Picture      Picture // how the event's picture is found
}

// ModeFolders names the folders whose databases take entry modes when a database has no precedence row of its own:
// the folders listed, or everything under the listed roots, except the folders named as exceptions.
type ModeFolders struct {
	Roots  []string // takes modes: its folder's first folder is one of these (read where Paths is empty)
	Paths  []string // takes modes: a database whose folder is one of these or below one
	Except []string // takes none: a database named here, or whose folder is
}

// A Game is one supported game with every fact that is its own. A game's own table is tried before the shared table
// of the same name.
type Game struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"` // also its folder under Documents
	ShortName     string             `json:"shortName"`
	WikiAPI       string             `json:"-"`                    // the MediaWiki api.php endpoint of its wiki
	StageRoots    []string           `json:"-"`                    // EU5's stages under ScriptRoot, at a mod's root
	ModDescriptor string             `json:"descriptor,omitempty"` // the descriptor file's path from the mod's root
	ThumbnailDir  string             `json:"-"`                    // where a new listing image goes; "" means the root
	SteamAppID    int                `json:"-"`
	SteamLibrary  map[string]string  `json:"-"` // by GOOS, where its copy of Steam's library sits
	ModFolders    []string           `json:"-"` // the folders a new mod gets, from its root
	Program       map[string]Program `json:"-"` // by GOOS; a GOOS with no row has no build
	ErrorLog      ErrorLog           `json:"-"`

	Databases     map[string]Database   `json:"-"` // its own databases by path, tried before Databases
	Nestings      []Nesting             `json:"-"` // its own nested databases, tried before Nestings
	Precedences   map[string]Precedence `json:"-"` // its own precedences by database, tried before Precedences
	References    []Reference           `json:"-"` // what the key paths of its own databases name
	Events        Events                `json:"-"`
	EntryModes    []EntryMode           `json:"-"` // the entry modes, in the order the game runs them
	ModeFolders   ModeFolders           `json:"-"`
	ConceptPrefix string                `json:"-"` // a concept's loc key prefix; "" means none
	// Placeholders is its own shapes, tried before Placeholders: a character's silhouette from its period, and its
	// coat of arms as a shield or a flag.
	Placeholders  []Placeholder `json:"-"`
	Illustrations Illustrations `json:"-"`
	// CoatColorForm is the form PMT writes a new color of a coat of arms' layer in, the one the game's own coats use.
	// The game reads any color form.
	CoatColorForm ColorForm `json:"-"`
	// FiringFields is the key paths of its databases' own fields where a name fires the event, on_action or decision
	// it names, as it would in effect script.
	FiringFields []Reference `json:"-"`
	// DecisionEffects is the effects that run the decision they name, as its dumps declare them. With none, no effect
	// fires a decision.
	DecisionEffects []string `json:"-"`

	GUIHolders     map[string]string `json:"-"` // the window a window made from a type is laid out in, by the type
	GUIUndeclared  []string          `json:"-"` // the widget types it draws though no file of its install declares them
	GUIDropsHidden []string          `json:"-"` // the types beyond GUI.Types in which a hidden child takes no room
	// GUISuffixStems is, by the stem of the defines for a kind and a field of its pictures (GUI.FolderDefineSuffix), the
	// stem its suffix define is written under instead, where the game names it so.
	GUISuffixStems map[string]string `json:"-"`
	GUITiers       TierFrames        `json:"-"` // how it draws a title's tier from a sheet of cells; zero for none
	CoatFolder     string            `json:"-"` // where it keeps its coats, from the folder holding its stages
	CoatShade      CoatShade         `json:"-"` // how it shades every coat; the zero value shades none
	CoatFrames     []CoatFrame       `json:"-"` // the frames its coat designer previews, in order
}

// The supported games' ids.
const (
	ck3  = "ck3"
	vic3 = "vic3"
	eu5  = "eu5"
)

// ScriptRoot is the folder under an install where the game's files start. Every scan and every relative path
// begins there.
const ScriptRoot = "game"

// InstallRoots are the folders under an install that the game mounts, in the order it mounts them. A relative
// path begins under one of them.
var InstallRoots = []string{"clausewitz", "jomini", ScriptRoot}

// BaseLanguage is the language that every other language's coverage is measured against.
const BaseLanguage = "english"

// OriginVanilla is the origin of the game's own files. A mod's files have the mod's id as their origin.
const OriginVanilla = "vanilla"

// MetadataDir is where a game whose mods are described by a metadata.json file keeps it and, for EU5, the thumbnail.
const MetadataDir = ".metadata"

// PictureExtensions are the extensions a mod's pictures may have: its thumbnail and the pictures of its gallery.
var PictureExtensions = []string{".png", ".gif", ".jpg", ".jpeg", ".webp"}

// The error log's file names, the revision log every start writes, and the launcher's settings file.
const (
	errorLogRel    = "logs/error.log"
	revisionLogRel = "logs/code_revisions.log"
	launcherFile   = "launcher/launcher-settings.json"
)

// errorLogLine matches the start of every error log line: the time, an optional level letter, the source file and
// line, and the message.
var errorLogLine = regexp.MustCompile(`^\[(\d\d):(\d\d):(\d\d)\](?:\[([A-Z])\])?\[([A-Za-z0-9_]+\.(?:cpp|h|hpp|inl)):(\d+)\]: ?(.*)$`)

// DumpFolders are the folders under a game's user data where the console's dumps land. CK3 writes script_docs to
// logs; Vic3 and EU5 write event_scopes.log to logs and the rest to docs; dump_data_types writes to
// logs/data_types in every game.
var DumpFolders = []string{"logs", "docs", "logs/data_types"}

var steamLibrary = map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"}

var games = map[string]*Game{
	ck3: {
		ID:            ck3,
		Name:          "Crusader Kings III",
		ShortName:     "CK3",
		WikiAPI:       "https://ck3.paradoxwikis.com/api.php",
		ModDescriptor: "descriptor.mod",
		SteamAppID:    1158310,
		SteamLibrary:  steamLibrary,
		ModFolders:    []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/ck3.exe", DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile},
		},
		ErrorLog:      ErrorLog{Rel: errorLogRel, Line: errorLogLine},
		Databases:     ck3Databases,
		Nestings:      ck3Nestings,
		References:    ck3References,
		Precedences:   ck3Precedences,
		Events:        Events{ScopeKey: "scope", DefaultScope: "character", Picture: ck3Picture}, // _events.info
		ConceptPrefix: "game_concept_",
		// effects.log: "execute_decision - Execute the specified decision for the scoped character"
		DecisionEffects: []string{"execute_decision"},
		Placeholders: []Placeholder{
			{Shape: "crowned-ruler", Kinds: []string{"portrait texture"}, Runtime: true},
			{Shape: "shield", Kinds: []string{"coat of arms"}, Runtime: true},
		},
		// gfx/interface/illustrations/scripted_illustrations/_scripted_illustrations.info
		Illustrations: Illustrations{FolderKey: "folder", Folder: "gfx/interface/illustrations", TriggerKey: "trigger"},
		CoatColorForm: ColorForm{Tag: "rgb", Example: []float64{255, 255, 255}},
		FiringFields:  ck3FiringFields,
		GUIUndeclared: []string{"zoomarea", "text_occluder"},
		// common/defines/graphic/00_graphics.txt:812-813: GOVERNMENT_TYPE_REALM_MASK_PATH with
		// GOVERNMENT_TYPE_REALM_FRAME_MASK_SUFFIX
		GUISuffixStems: map[string]string{"GOVERNMENT_TYPE_REALM_MASK": "GOVERNMENT_TYPE_REALM_FRAME_MASK"},
		// gui/map_icon_layer.gui:4820-4856 asks cell 3 for a count, 4 for a duke and from 5 for a king or an emperor;
		// common/defines/00_defines.txt:358 counts seven tiers, Unlanded to Hegemon, and gfx/interface/coat_of_arms'
		// topframe_62.dds and crown_strip_62.dds are seven cells wide
		GUITiers: TierFrames{
			Function: "GetTierFrame", Tiers: []string{"unlanded", "baron", "count", "duke", "king", "emperor", "hegemon"},
		},
		CoatFolder: "common/coat_of_arms/coat_of_arms",
		CoatShade:  ck3CoatShade,
		CoatFrames: ck3CoatFrames,
	},
	vic3: {
		ID:            vic3,
		Name:          "Victoria 3",
		ShortName:     "Vic3",
		WikiAPI:       "https://vic3.paradoxwikis.com/api.php",
		ModDescriptor: MetadataDir + "/metadata.json",
		SteamAppID:    529340,
		SteamLibrary:  steamLibrary,
		ModFolders:    []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/victoria3.exe", DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
		},
		ErrorLog: ErrorLog{
			Rel: errorLogRel, Line: errorLogLine,
			Location: regexp.MustCompile(`Script location: (\S+):(\d+)`),
		},
		Databases:   vic3Databases,
		Precedences: vic3Precedences,
		References:  vic3References,
		Events:      Events{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: vic3Picture},
		EntryModes:  EntryModes,
		ModeFolders: vic3ModeFolders,
		Placeholders: []Placeholder{
			{Shape: "top-hat", Kinds: []string{"portrait texture"}, Runtime: true},
			{Shape: "flag", Kinds: []string{"coat of arms"}, Runtime: true},
		},
		CoatColorForm:  ColorForm{Tag: tagHSV360, Example: []float64{360, 100, 100}},
		FiringFields:   vic3FiringFields,
		GUIUndeclared:  []string{"overlappingitembox", "treemapchart", "portrait_button", "axis"},
		GUIDropsHidden: []string{"flowcontainer"},
		CoatFolder:     "common/coat_of_arms/coat_of_arms",
	},
	eu5: {
		ID:            eu5,
		Name:          "Europa Universalis V",
		ShortName:     "EU5",
		WikiAPI:       "https://eu5.paradoxwikis.com/api.php",
		StageRoots:    []string{"in_game", "main_menu", "loading_screen"},
		ModDescriptor: MetadataDir + "/metadata.json",
		ThumbnailDir:  MetadataDir, // where EU5 mods keep it
		SteamAppID:    3450310,
		SteamLibrary:  map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build: Proton
		ModFolders:    []string{"in_game/common", "in_game/events", "main_menu/localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/eu5.exe", DebugArgs: []string{"-debug_mode"}},
		},
		ErrorLog: ErrorLog{
			Rel: errorLogRel, Line: errorLogLine,
			Location: regexp.MustCompile(`\bat (\S+):(\d+)`),
		},
		Databases:     eu5Databases,
		Nestings:      eu5Nestings,
		Precedences:   eu5Precedences,
		References:    eu5References,
		Events:        Events{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: eu5Picture},
		EntryModes:    EntryModes,
		ModeFolders:   eu5ModeFolders,
		ConceptPrefix: "game_concept_",
		Placeholders: []Placeholder{
			{Shape: "plumed-hat", Kinds: []string{"portrait texture"}, Runtime: true},
			{Shape: "flag", Kinds: []string{"coat of arms"}, Runtime: true},
		},
		GUIHolders:    map[string]string{"lateralview": "lateralview_left_layer"},
		CoatColorForm: ColorForm{Tag: tagHSV360, Example: []float64{360, 100, 100}},
		GUIUndeclared: []string{"axis", "plotline", "screen_grab", "newsfeed_carousel"},
		CoatFolder:    "main_menu/common/coat_of_arms/coat_of_arms",
	},
}

// CanAccuse reports whether a claim from this source is firm enough to fail a verdict. Only the dumps, the docs and
// the wiki are.
func (source Source) CanAccuse() bool {
	return source == SourceDumps || source == SourceDocs || source == SourceWiki
}

// LookupGame returns the game with the given id, or nil if this build does not support it.
func LookupGame(id string) *Game {
	return games[id]
}

// ListGames returns the supported games in the order the app shows them.
func ListGames() []*Game {
	return []*Game{games[ck3], games[eu5], games[vic3]}
}
