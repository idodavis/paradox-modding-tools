// game.go holds each game's row: its names, where its script, mods and dumps sit, how it is started, where it logs
// errors, and the facts that are its own beside the shared tables.

// Package game holds every fact about the supported games: which games exist, where each is installed, and what
// its script, GUI and coat of arms files mean. Every fact is a row in a table or a field of a game's row, written
// as a literal. A row's evidence is its trailing comment, and a column whose values are words lists them in its
// comment and names the function that reads them.
package game

import "regexp"

// A Source says where a fact the schema learned came from. A lower value outranks a higher one, and a claim keeps
// its best source. A claim from an inferred source never fails a verdict.
type Source uint8

// The sources, by rank: each outranks the ones after it.
const (
	SourceNone     Source = iota
	SourceRegistry        // this package's rows: they classify and never accuse
	SourceDumps           // the console dumps: what the game declares about its own language
	SourceExe             // a message the game's executable carries
	SourceLog             // a line the game's own logs write
	SourceVanilla         // the install's own files: evidence, never a claim that accuses
	SourceDocs            // the prose the install ships beside its script (.info, .md, readme.txt)
	SourceWiki            // the community wiki, taken where no game source contradicts it
	SourceInferred        // no source states it: written as inferred, it never fails a verdict
)

// A Program is what Launch runs for a game on this OS. A game whose row has no executable cannot be launched.
type Program struct {
	Executable string   // from the install root (binaries/ck3.exe); "" where none is measured, so launching stays off
	DebugArgs  []string // the flags that start it in debug mode, which hot-loads script
	Settings   string   // the launcher settings file whose exePath and debug arguments win; "" none
}

// An ErrorLog says where a game writes its error log and how to read it: the file, its rotated siblings, and the
// two patterns that pull a message and its script location out of a line.
type ErrorLog struct {
	Rel      string         // under the user-data folder (logs/error.log)
	Rotated  string         // the earlier runs' logs (logs/error.%d.log); "" where the game rotates none
	Line     *regexp.Regexp // a message's header: time, an optional [E], the source file and line, the message
	Location *regexp.Regexp // the script path and line a message names; nil where it names none
}

// Events says where an event's root scope is written and how to find its picture.
type Events struct {
	ScopeKey     string  // the key whose value is the event's root scope; "" none
	TypeKey      string  // the key whose value, less TypeSuffix, names the event's root scope; "" none
	TypeSuffix   string  // dropped from TypeKey's value
	DefaultScope string  // the root scope where neither key is written
	Picture      Picture // how the event's picture is found
}

// ModeFolders says which folders take entry modes when a database has no precedence row of its own: the folders
// listed, or everything under the listed roots, except the folders named as exceptions.
type ModeFolders struct {
	Roots  []string // takes modes: its folder's first folder is one of these (read where Paths is empty)
	Paths  []string // takes modes: a database whose folder is one of these or below one
	Except []string // takes none: a database named here, or whose folder is
}

// A Game is one supported game: its identity, where its script, mods and dumps sit, how it starts and logs, and
// its own tables. A reader tries a game's own table before the shared table of the same name.
type Game struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"` // also its folder under Documents
	ShortName     string             `json:"shortName"`
	WikiAPI       string             `json:"-"`                    // the MediaWiki api.php endpoint of its wiki
	StageRoots    []string           `json:"-"`                    // EU5's stages under ScriptRoot, at a mod's root
	ModDescriptor string             `json:"descriptor,omitempty"` // "mod" (descriptor.mod) or MetadataDescriptor
	ThumbnailDir  string             `json:"-"`                    // where a new listing image goes; "" the mod root
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
	EntryModes    []EntryMode           `json:"-"` // the words before a definition's name, in the order the game runs them; nil none
	ModeFolders   ModeFolders           `json:"-"`
	ConceptPrefix string                `json:"-"` // what the loc key of a game concept's name puts before it; "" the name is the key

	GUIHolders     map[string]string `json:"-"` // the window a window made from a type is laid out in, by the type
	GUIUndeclared  []string          `json:"-"` // the widget types it draws though no file of its install declares them
	GUIDropsHidden []string          `json:"-"` // the types, beyond those GUI.Types marks, in which a hidden child takes no room
	CoatFolder     string            `json:"-"` // where it keeps its coats, from the folder holding its stages
	CoatShade      CoatShade         `json:"-"` // how it shades every coat; the zero value shades none
	CoatFrames     []CoatFrame       `json:"-"` // the shapes its coat designer previews a coat in, in its order; none without a designer
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

// MetadataDescriptor marks a game whose mods are described by a .metadata/metadata.json file rather than a .mod
// file.
const MetadataDescriptor = "metadata"

// MetadataDir is where such a game keeps the descriptor and, for EU5, the thumbnail.
const MetadataDir = ".metadata"

// The error log's file names and the launcher's settings file.
const (
	errorLogRel     = "logs/error.log"
	errorLogRotated = "logs/error.%d.log"
	launcherFile    = "launcher/launcher-settings.json"
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
		ModDescriptor: "mod",
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
		Precedences:   ck3Precedences,
		References:    ck3References,
		Events:        Events{ScopeKey: "scope", DefaultScope: "character", Picture: ck3Picture}, // _events.info
		ConceptPrefix: "game_concept_",
		GUIUndeclared: []string{"zoomarea", "text_occluder"},
		CoatFolder:    "common/coat_of_arms/coat_of_arms",
		CoatShade:     ck3CoatShade,
		CoatFrames:    ck3CoatFrames,
	},
	vic3: {
		ID:            vic3,
		Name:          "Victoria 3",
		ShortName:     "Vic3",
		WikiAPI:       "https://vic3.paradoxwikis.com/api.php",
		ModDescriptor: MetadataDescriptor,
		SteamAppID:    529340,
		SteamLibrary:  steamLibrary,
		ModFolders:    []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/victoria3.exe", DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
		},
		ErrorLog: ErrorLog{
			Rel: errorLogRel, Rotated: errorLogRotated, Line: errorLogLine,
			Location: regexp.MustCompile(`Script location: (\S+):(\d+)`),
		},
		Databases:      vic3Databases,
		Precedences:    vic3Precedences,
		References:     vic3References,
		Events:         Events{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: vic3Picture}, // the wiki
		EntryModes:     EntryModes,
		ModeFolders:    vic3ModeFolders,
		GUIUndeclared:  []string{"overlappingitembox", "treemapchart", "portrait_button", "axis"},
		GUIDropsHidden: []string{"flowcontainer"}, // observed: the wiki says a flowcontainer keeps a hidden child's room
		CoatFolder:     "common/coat_of_arms/coat_of_arms",
	},
	eu5: {
		ID:            eu5,
		Name:          "Europa Universalis V",
		ShortName:     "EU5",
		WikiAPI:       "https://eu5.paradoxwikis.com/api.php",
		StageRoots:    []string{"in_game", "main_menu", "loading_screen"},
		ModDescriptor: MetadataDescriptor,
		ThumbnailDir:  MetadataDir, // where EU5 mods keep it
		SteamAppID:    3450310,
		SteamLibrary:  map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build: Proton
		ModFolders:    []string{"in_game/common", "in_game/events", "main_menu/localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/eu5.exe", DebugArgs: []string{"-debug_mode"}},
		},
		ErrorLog: ErrorLog{
			Rel: errorLogRel, Rotated: errorLogRotated, Line: errorLogLine,
			Location: regexp.MustCompile(`\bat (\S+):(\d+)`),
		},
		Databases:     eu5Databases,
		Nestings:      eu5Nestings,
		Precedences:   eu5Precedences,
		References:    eu5References,
		Events:        Events{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: eu5Picture}, // _events.info
		EntryModes:    EntryModes,
		ModeFolders:   eu5ModeFolders,
		ConceptPrefix: "game_concept_",
		GUIHolders:    map[string]string{"lateralview": "lateralview_left_layer"},
		GUIUndeclared: []string{"axis", "plotline", "screen_grab", "newsfeed_carousel"},
		CoatFolder:    "main_menu/common/coat_of_arms/coat_of_arms",
	},
}

// CanAccuse reports whether a claim from this source is firm enough to fail a verdict. The dumps, the docs and the
// wiki are; an inferred source is not.
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
