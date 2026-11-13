// Package game holds the games PMT supports, Crusader Kings III, Victoria 3
// and Europa Universalis V: each game's identity, how its mods are laid out
// and how it is started. It also finds a game on this machine: its installs
// and their versions, the folders an install mounts, the user-data and dump
// folders, the Steam Workshop folders and the program that starts the game.
//
// # Installs
//
//   - The installs are found in Steam's own folders and in the libraries its
//     libraryfolders.vdf names, each counted once after links are resolved.
//   - The version of an install is the one in its launcher's settings.
//     Without one, it is the version the game writes to its debug log, else
//     "build" and the day the game was built, from its revision log. A log
//     is read only if the game was started after the install was last
//     updated. Without those, it is "build" and Steam's build number.
//   - An install mounts its clausewitz, jomini and game folders, in that
//     order, and then each DLC under game/dlc.
//   - The user-data folder is the first that exists of the folder the
//     launcher's gameDataPath names and the usual folders: Documents on
//     Windows; the XDG data folder, Documents and each Proton prefix on
//     Linux. Where none exists, it is the first of them.
//   - An install was last updated when the newest file in its binaries
//     folder was written, else the newest file in the install's own folder.
//     A folder's own time is no use, since replacing a file in place leaves
//     it unchanged.
//   - The Workshop folder is the game's folder under steamapps/workshop in
//     each Steam library.
//   - The program that starts a game is the one the launcher's exePath
//     names, else the game's own. Its arguments are the launcher's, then
//     the flags for debug mode.
//
// # Robustness
//
// Nothing here fails over what it reads from disk. A launcher settings file
// that is missing or cannot be decoded is read as empty. A Steam library
// file, a manifest or a log that cannot be read is skipped, and so is an
// install folder that a manifest names and that does not exist.
// ListInstallRoots returns an error only for a DLC folder that exists and
// cannot be read. LookupGame returns nil for an id the app does not
// support.
package game

// Game is one game the app supports: its names, how its mods are laid out
// and how it is started.
type Game struct {
	ID string `json:"id"`

	// Name is the game's full name. Its folder under Documents/Paradox
	// Interactive has the same name.
	Name string `json:"name"`

	ShortName string `json:"shortName"`

	// Stages are the folders the game loads at its different stages, as
	// EU5's main_menu and in_game, in the order it mounts them. The game's
	// own folder and a mod's each hold them. They are none for a game that
	// has no stages.
	Stages []string `json:"-"`

	// ModDescriptor is the path of the file that describes a mod, from the
	// mod's folder.
	ModDescriptor string `json:"descriptor,omitempty"`

	// FixedModFolder is whether the launcher loads a local mod only from its
	// own mod folder.
	FixedModFolder bool `json:"fixedModFolder"`

	// ThumbnailDir is the folder a new thumbnail of a mod is saved in, from
	// the mod's folder. It is "" for the mod's folder itself.
	ThumbnailDir string `json:"-"`

	SteamAppID int `json:"-"`

	// SteamLibrary is the path of the game's own copy of Steam's library,
	// from the install's folder, for each system by its GOOS name. A system
	// that is not listed has none.
	SteamLibrary map[string]string `json:"-"`

	// ModFolders are the folders a new mod is made with, from the mod's
	// folder.
	ModFolders []string `json:"-"`

	// Program is how the game is started on each system, by its GOOS name.
	// The game has no build for a system that is not listed.
	Program map[string]Program `json:"-"`

	Guide   Guide   `json:"guide"`
	Example Example `json:"example"`
}

// Program is how a game is started on one system.
type Program struct {
	// Executable is the path of the game's program, from the install's
	// folder. It is "" where it is not known.
	Executable string

	// DebugArgs are the flags that start the game in debug mode, in which it
	// loads changed script while it runs.
	DebugArgs []string

	// Settings is the path of the launcher's settings file, from the
	// install's folder. The program its exePath names is started in place of
	// Executable, and its arguments come before DebugArgs. It is "" for a
	// game whose launcher has no such file.
	Settings string
}

// Example names three definitions the game ships in one database. The app
// shows them where it explains how a mod changes the game.
type Example struct {
	Database string `json:"database"`

	// Folder is the folder of the database's files, from the game's folder.
	Folder string `json:"folder"`

	Names []string `json:"names"`

	// Modes is whether a mod changes a definition of this game by writing a
	// mode before its name, as REPLACE:brave does.
	Modes bool `json:"modes"`
}

// Guide is the text that tells a modder how a game picks which copy of a
// file or of a definition it keeps.
type Guide struct {
	// Hint says what the load order decides. Files says which of a mod's
	// files the game reads. Kept says which copy of a definition each
	// database keeps. Changes says what moving a mod in the load order
	// changes.
	Hint    string `json:"hint"`
	Files   string `json:"files"`
	Kept    string `json:"kept"`
	Changes string `json:"changes"`
}

// The ids of the supported games.
const (
	CK3  = "ck3"
	Vic3 = "vic3"
	EU5  = "eu5"
)

// ScriptRoot is the folder of an install that holds the game's own files.
const ScriptRoot = "game"

// MetadataDir is the folder of a mod that holds its metadata.json and, for
// EU5, its thumbnail.
const MetadataDir = ".metadata"

// PictureExtensions are the file extensions a mod's thumbnail and the
// pictures of its gallery may have.
var PictureExtensions = []string{".png", ".gif", ".jpg", ".jpeg", ".webp"}

// launcherFile is the path of the launcher's settings file, from the
// install's folder.
const launcherFile = "launcher/launcher-settings.json"

// steamLibrary is where CK3 and Vic3 keep their copy of Steam's library, by
// GOOS name.
var steamLibrary = map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"}

// games holds the supported games, by id.
var games = map[string]*Game{
	CK3: {
		ID:            CK3,
		Name:          "Crusader Kings III",
		ShortName:     "CK3",
		ModDescriptor: "descriptor.mod",
		SteamAppID:    1158310,
		SteamLibrary:  steamLibrary,
		ModFolders:    []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/ck3.exe", DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path.",
			Files:   "A mod's replace_path makes the game skip its own files in that folder; whether it also skips the folder's subfolders is not documented.",
			Kept:    "Most things keep the copy read last; events keep the highest override priority, and on a tie the game logs an error and keeps one copy without saying which; a character takes its name, traits, skills, parents and birth from the copy with the highest override priority and combines the rest, and the game calls a tie an error without saying what it keeps; on_actions combine their lists.",
			Changes: "Only files at the same path.",
		},
		// common/traits/00_traits.txt
		Example: Example{Database: "traits", Folder: "common/traits", Names: []string{"gregarious", "shy", "brave"}},
	},
	Vic3: {
		ID:             Vic3,
		Name:           "Victoria 3",
		ShortName:      "Vic3",
		ModDescriptor:  MetadataDir + "/metadata.json",
		FixedModFolder: true,
		SteamAppID:     529340,
		SteamLibrary:   steamLibrary,
		ModFolders:     []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/victoria3.exe", DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path.",
			Files:   "A mod's replace_path makes the game skip its own files in that folder. This is documented for CK3 and EU5, whose engine Vic3 shares; whether it also skips the folder's subfolders is not documented.",
			Kept:    "Most things keep the copy read last; events and GUI types keep the first; where a folder takes INJECT: and REPLACE:, the first plain copy stays and a later one is refused, so mods change it with those keywords, which act after every plain copy whatever their file's name, by mode (INJECT_OR_CREATE first, REPLACE last) and then by file name; character DNA copies all exist.",
			Changes: "Only files at the same path.",
		},
		// common/buildings/01_industry.txt
		Example: Example{
			Database: "buildings", Folder: "common/buildings",
			Names: []string{"building_food_industry", "building_textile_mill", "building_furniture_manufactory"},
		},
	},
	EU5: {
		ID:        EU5,
		Name:      "Europa Universalis V",
		ShortName: "EU5",
		// debug.log:5372-5394 mounts each root's stages in this order, and only
		// them; the wiki's Mod structure, l.46-57
		Stages:         []string{"loading_screen", "main_menu", "in_game"},
		ModDescriptor:  MetadataDir + "/metadata.json",
		FixedModFolder: true,
		ThumbnailDir:   MetadataDir,
		SteamAppID:     3450310,
		// EU5 has no Linux build. On Linux it runs under Proton, with only
		// the Windows library.
		SteamLibrary: map[string]string{"windows": "binaries/steam_api64.dll"},
		ModFolders:   []string{"in_game/common", "in_game/events", "main_menu/localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/eu5.exe", DebugArgs: []string{"-debug_mode"}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path, and GUI types and templates.",
			Files:   "A mod's files count only inside its in_game, main_menu or loading_screen folder. A mod's replace_path makes the game skip its own files in that folder, not in its subfolders.",
			Kept:    "Under common/ the first plain copy stays and mods change it with INJECT: and REPLACE:, which act after every plain copy whatever their file's name, by mode (INJECT_OR_CREATE first, REPLACE last) and then by file name; events keep the first; GUI types and templates follow the load order; coats of arms and defines keep the last.",
			Changes: "Only files at the same path and GUI types and templates.",
		},
		// in_game/common/traits/00_ruler.txt
		Example: Example{Database: "traits", Folder: "in_game/common/traits", Names: []string{"righteous", "cruel", "tolerant"}},
	},
}

// LookupGame returns the game with the given id. It returns nil for an id
// the app does not support.
func LookupGame(id string) *Game {
	return games[id]
}

// ListGames returns the supported games in the order the app shows them.
// Modes of each game's example is false here, since the language's own rows
// say which games take modes.
func ListGames() []Game {
	return []Game{*games[CK3], *games[EU5], *games[Vic3]}
}
