// game.go holds each game's identity: its names, where its script, mods and dumps sit, how it is started, where it
// logs errors, and the rules of its script.

// Package game is the only game-aware package in the backend: the table of supported games with what each one's
// install, mods, databases and wiki look like, the language's own words, and where the games are installed.
package game

import "regexp"

// Source says where a fact came from; a lower value outranks a higher one, and a claim keeps its highest-ranked
// source. One written as inferred never fails a verdict.
type Source uint8

// Program is how a game is started on one OS.
type Program struct {
	Executable string   // from the install root (binaries/ck3.exe); "" where the name is not measured
	DebugArgs  []string // the flags that start it in debug mode, which hot-loads script
	Settings   string   // the launcher settings file whose exePath and debug arguments win; "" none
	Source     Source
}

// ErrorLog is where a game writes the errors in its script and how its lines read.
type ErrorLog struct {
	Rel      string         // under the user-data folder (logs/error.log)
	Rotated  string         // the earlier runs' logs (logs/error.%d.log); "" where the game rotates none
	Line     *regexp.Regexp // a message's header: time, an optional [E], the source file and line, the message
	Location *regexp.Regexp // the script path and line a message names; nil where it names none
	Source   Source
}

// Game is the static identity of one game and the rules of its script; the engine scans the install for the rest.
type Game struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`

	WikiAPI        string             `json:"-"`                    // the MediaWiki api.php endpoint of its wiki
	ScriptRoot     string             `json:"-"`                    // the install folder holding script ("game")
	StageRoots     []string           `json:"-"`                    // EU5's stages under ScriptRoot, at a mod's root
	ModDescriptor  string             `json:"descriptor,omitempty"` // "mod" (descriptor.mod) or MetadataDescriptor
	ThumbnailDir   string             `json:"-"`                    // where a new listing image goes; "" the mod root
	DocsFolderName string             `json:"-"`                    // its folder under Documents ("Crusader Kings III")
	BaseLanguage   string             `json:"-"`                    // the localization language it ships whole
	SteamAppID     int                `json:"-"`
	SteamLibrary   map[string]string  `json:"-"`                   // by GOOS, where its copy of Steam's library sits
	FirstWins      string             `json:"firstWins,omitempty"` // the pages' sentence on its first-wins databases
	ModFolders     []string           `json:"-"`                   // the folders a new mod gets, from its root
	DumpFolders    []string           `json:"-"`                   // where the console's dumps land under user data
	Program        map[string]Program `json:"-"`                   // by GOOS; a GOOS with no row has no build
	ErrorLog       ErrorLog           `json:"-"`

	Rules             []DatabaseRule `json:"-"` // how its script files lay out their entries (databases.go)
	EntryModes        []EntryMode    `json:"-"` // the words before a definition's name, in the order the game runs them
	DefaultPrecedence Precedence     `json:"-"` // how definitions combine where no rule says
	ModeFolders       ModeFolders    `json:"-"` // which databases take an entry mode
}

// GameList is the Wails payload for ListGames (bindings carry no Go constants).
type GameList struct {
	OriginVanilla string  `json:"originVanilla"`
	Games         []*Game `json:"games"`
}

// The sources, by rank: each outranks the ones after it.
const (
	SourceNone     Source = iota
	SourceRegistry        // the game's rows and the language rules: they classify and never accuse
	SourceDumps           // the console dumps: what the game declares about its own language
	SourceExe             // a message the game's executable carries
	SourceLog             // a line the game's own logs write
	SourceVanilla         // the install's own files: evidence, never a claim that accuses
	SourceDocs            // the prose the install ships beside its script (.info, .md, readme.txt)
	SourceWiki            // the community wiki, taken where no game source contradicts it
	SourceInferred        // no source states it: written as inferred, it never fails a verdict
)

// The supported games' ids.
const (
	ck3  = "ck3"
	vic3 = "vic3"
	eu5  = "eu5"
)

// OriginVanilla is the origin id of the game's own files.
const OriginVanilla = "vanilla"

// baseLanguage is the localization every game ships whole.
const baseLanguage = "english"

// MetadataDescriptor is the ModDescriptor of a game whose mods carry .metadata/metadata.json.
const MetadataDescriptor = "metadata"

// MetadataDir is the folder a metadata descriptor sits in.
const MetadataDir = ".metadata"

// The error log's own forms and the launcher's settings file.
const (
	errorLogRel     = "logs/error.log"
	errorLogRotated = "logs/error.%d.log"
	launcherFile    = "launcher/launcher-settings.json"
)

// errorLogLine is the header every game's error log writes: the time, an optional level letter, the source file and
// line, and the message.
var errorLogLine = regexp.MustCompile(`^\[(\d\d):(\d\d):(\d\d)\](?:\[([A-Z])\])?\[([A-Za-z0-9_]+\.(?:cpp|h|hpp|inl)):(\d+)\]: ?(.*)$`)

// dumpFolders are where the console's dumps land in every game: CK3 writes script_docs to logs, Vic3 and EU5
// event_scopes.log to logs and the rest to docs; dump_data_types writes to logs/data_types.
var dumpFolders = []string{"logs", "docs", "logs/data_types"}

// registry is the supported-game table, keyed by id.
var registry = map[string]*Game{
	ck3: {
		ID:             ck3,
		Name:           "Crusader Kings III",
		ShortName:      "CK3",
		WikiAPI:        "https://ck3.paradoxwikis.com/api.php",
		ScriptRoot:     "game",
		ModDescriptor:  "mod",
		DocsFolderName: "Crusader Kings III",
		BaseLanguage:   baseLanguage,
		SteamAppID:     1158310,
		SteamLibrary:   map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FirstWins:      "GUI types and templates are first-wins.",
		ModFolders:     []string{"common", "events", "localization"},
		DumpFolders:    dumpFolders,
		Program: map[string]Program{
			"windows": {
				Executable: "binaries/ck3.exe", DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile, Source: SourceExe,
			},
			"linux": {DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile, Source: SourceExe},
		},
		ErrorLog:          ErrorLog{Rel: errorLogRel, Line: errorLogLine, Source: SourceLog},
		Rules:             ck3Rules,
		DefaultPrecedence: Precedence{Keeps: "last", Source: SourceWiki},
	},
	vic3: {
		ID:             vic3,
		Name:           "Victoria 3",
		ShortName:      "Vic3",
		WikiAPI:        "https://vic3.paradoxwikis.com/api.php",
		ScriptRoot:     "game",
		ModDescriptor:  MetadataDescriptor,
		DocsFolderName: "Victoria 3",
		BaseLanguage:   baseLanguage,
		SteamAppID:     529340,
		SteamLibrary:   map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FirstWins:      "GUI types, templates, and events are first-wins.",
		ModFolders:     []string{"common", "events", "localization"},
		DumpFolders:    dumpFolders,
		Program: map[string]Program{
			"windows": {
				Executable: "binaries/victoria3.exe", DebugArgs: []string{"-debug_mode"}, Settings: launcherFile, Source: SourceExe,
			},
			"linux": {DebugArgs: []string{"-debug_mode"}, Settings: launcherFile, Source: SourceExe},
		},
		ErrorLog: ErrorLog{
			Rel: errorLogRel, Rotated: errorLogRotated, Line: errorLogLine,
			Location: regexp.MustCompile(`Script location: (\S+):(\d+)`), Source: SourceLog,
		},
		Rules:             vic3Rules,
		EntryModes:        entryModes,
		DefaultPrecedence: Precedence{Keeps: "last", Source: SourceWiki},
		ModeFolders:       vic3ModeFolders,
	},
	eu5: {
		ID:             eu5,
		Name:           "Europa Universalis V",
		ShortName:      "EU5",
		WikiAPI:        "https://eu5.paradoxwikis.com/api.php",
		ScriptRoot:     "game",
		StageRoots:     []string{"in_game", "main_menu", "loading_screen"},
		ModDescriptor:  MetadataDescriptor,
		ThumbnailDir:   MetadataDir, // where EU5 mods keep it
		DocsFolderName: "Europa Universalis V",
		BaseLanguage:   baseLanguage,
		SteamAppID:     3450310,
		SteamLibrary:   map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build: Proton
		FirstWins:      "Events are first-wins; GUI types last-wins.",
		ModFolders:     []string{"in_game/common", "in_game/events", "main_menu/localization"},
		DumpFolders:    dumpFolders,
		Program: map[string]Program{
			"windows": {Executable: "binaries/eu5.exe", DebugArgs: []string{"-debug_mode"}, Source: SourceExe},
		},
		ErrorLog: ErrorLog{
			Rel: errorLogRel, Rotated: errorLogRotated, Line: errorLogLine,
			Location: regexp.MustCompile(`\bat (\S+):(\d+)`), Source: SourceLog,
		},
		Rules:             eu5Rules,
		EntryModes:        entryModes,
		DefaultPrecedence: Precedence{Keeps: "last", Source: SourceInferred},
		ModeFolders:       eu5ModeFolders,
	},
}

// Accuses reports whether a source states claims that may accuse: the dumps, the docs or the wiki.
func (source Source) Accuses() bool {
	return source == SourceDumps || source == SourceDocs || source == SourceWiki
}

// Lookup returns the game with an id, or nil if it is not supported.
func Lookup(id string) *Game {
	return registry[id]
}

// ListGames returns the supported games in UI order (ck3, eu5, vic3).
func ListGames() []*Game {
	return []*Game{registry[ck3], registry[eu5], registry[vic3]}
}
