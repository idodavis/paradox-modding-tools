// Package game is the only game-aware package in the backend: the table of supported games
// and what each one's install, mods and wiki look like. It also finds the installs, through the
// Steam library folders, and each game's user-data folder.
package game

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// OriginVanilla is the session/IDE origin id for game files (not a cache filename).
const OriginVanilla = "vanilla"

// GameInfo is the static identity and convention set for one game. It carries no
// derived data; the engine under lang/ scans the install for that.
type GameInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`

	// WikiAPI is the MediaWiki api.php endpoint for this game's wiki.
	WikiAPI string `json:"wikiApi,omitempty"`

	// ScriptRoot is the install-relative folder holding script (e.g. "game"). EU5's
	// StageRoots sit under it: game/{in_game,main_menu,loading_screen}.
	// Mods omit game/ and put StageRoots at the mod root.
	ScriptRoot string   `json:"scriptRoot,omitempty"`
	StageRoots []string `json:"stageRoots,omitempty"`

	// Descriptor is how a mod root is identified: "mod" (descriptor.mod) or
	// "metadata" (.metadata/metadata.json).
	Descriptor string `json:"descriptor,omitempty"`

	// ThumbnailDir is where a NEW listing image goes, relative to the mod root:
	// "" for the root itself, ".metadata" beside the metadata file. Not implied by
	// Descriptor: Victoria 3 uses metadata.json yet keeps its thumbnail at the root
	// (GAME-SYNTAX.md has the counts). An existing thumbnail keeps its place.
	// Backend-only: the frontend is told the resulting path on the listing.
	ThumbnailDir string `json:"-"`

	// DocsFolderName is the per-game Documents subfolder (e.g. "Crusader Kings III").
	DocsFolderName string `json:"docsFolderName,omitempty"`
	// ScriptDocsDirs are where the console's script_docs writes its dumps, and DataTypesDir where
	// dump_data_types does, from the user-data folder, alike on Windows and Linux: CK3 writes every
	// dump to logs, Vic3 and EU5 event_scopes.log to logs and the rest to docs. Backend-only.
	ScriptDocsDirs []string `json:"-"`
	DataTypesDir   string   `json:"-"`

	// EntryModes are the words a mod may write before a definition's name (REPLACE:name), in
	// the order the game processes them; file order decides only between entries of one mode.
	// Victoria 3 and EU5 have all six words, CK3 none. Backend-only.
	EntryModes []EntryMode `json:"-"`

	SteamAppID int `json:"steamAppId,omitempty"`
	// SteamLibrary is where the game's own copy of Steam's library sits in its install, by
	// GOOS; Workshop publish loads it. A GOOS with no row has no build of the game. Backend-only.
	SteamLibrary map[string]string `json:"-"`

	// FirstWinsDatabases are the databases this game resolves first-in-order; every other
	// database is last-in-order. Backend-only — FirstWins is the same fact as the
	// sentence the Conflicts and Settings pages show.
	FirstWinsDatabases []string `json:"-"`
	FirstWins          string   `json:"firstWins,omitempty"`

	// FileTypes, Folders, Nested and MapTable say where entries are and how they are named; the
	// databases themselves are read from the install. Backend-only.
	FileTypes map[string]FileType `json:"-"`
	Folders   map[string]Folder   `json:"-"`
	Nested    []Nested            `json:"-"`
	MapTable  MapTable            `json:"-"`

	// ModFolders are the folders a new mod gets, from its root; the localization one is the one whose
	// last segment is localization. Backend-only.
	ModFolders []string `json:"-"`
	// DataTypesCommand is the console command that writes the data-type dumps. Backend-only: the
	// frontend reads it from LanguageHealth.
	DataTypesCommand string `json:"-"`
}

// EntryMode is a word written before a definition's name, and what it does to one already loaded.
type EntryMode struct {
	Word     string
	Replaces bool // takes the earlier definition's place; an inject adds to it and never displaces it
	Optional bool // TRY_: does nothing where there is none
	Creates  bool // _OR_CREATE: defines it where there is none
}

// FileType is how an extension's files name entries; an extension with no row defines nothing.
type FileType struct {
	NameKey   string            // the key whose value names a top block's entry: .asset name = x
	Keywords  []string          // the words that declare an entry: type x = button { }
	NameKeyOf map[string]string // a block key at any depth, and the key whose value names its entry
	WidgetKey string            // the key that makes a block below the top an entry of <ext>.widget
}

// Folder is one folder's row, by its path from a root (stage-free for EU5); a path takes the
// longest row at or above it.
type Folder struct {
	History    bool     // a history or setup folder: the start state of entries, defining none of its own
	Of         []string // History: the databases its entries are named after, the first that has the name
	ByFile     bool     // History: each file is one entry, named after one of Of's, or by its stem
	Unnamed    bool     // data whose entries name nothing: generated objects, locators, path-keyed lists
	NameKey    string   // every top block is one entry named by this key's value, up to a (
	Wrappers   []string // keys that only wrap entries: a top key's block holds top statements
	CalledIn   []string // scripted: the blocks that call an entry by its name: effect, trigger, value
	Body       []string // scripted: the block an entry is: effect, trigger, value
	Keyword    string   // scripted: the word that declares an entry inline in any file
	Parameters bool     // scripted: a call passes parameters
	Events     bool     // the whole tree is one database, named by event id
	OnActions  bool     // its entries are on_actions
	Refs       []string // positions in an entry (key/<number>) whose words name this database's entries
	Functions  bool     // its entries are data functions callable in data calls
	Concepts   bool     // its entries are game concepts named in loc text
	Describes  string   // trigger or effect: its entries are named by commands of that kind and give their loc text
}

// Nested is a position inside an entry where keys create entries, or name another database's.
type Nested struct {
	Parent   string // a folder path; the row holds for every database at or below it
	Path     string // "" the entry's own block, * one key, a leading **/ any depth
	Database string // the word for the database the keys create; self for the parent's own entries
}

// MapTable is a table named by its first column.
type MapTable struct {
	Database string
	File     string
}

// entryModes is the six words Victoria 3 and EU5 share, in processing order (both wikis'
// Mod compatibility page).
var entryModes = []EntryMode{
	{Word: "INJECT_OR_CREATE", Creates: true}, {Word: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Word: "TRY_INJECT", Optional: true}, {Word: "TRY_REPLACE", Replaces: true, Optional: true},
	{Word: "INJECT"}, {Word: "REPLACE", Replaces: true},
}

// fileTypes is every game's: how .txt, .asset, .font and .gui files name entries.
var fileTypes = map[string]FileType{
	".txt":   {},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui": {
		NameKey:   "name",
		Keywords:  []string{"type", "local_type", "template", "local_template", "block", "layer"},
		NameKeyOf: map[string]string{"texticon": "icon", "format": "name"},
		WidgetKey: "name",
	},
}

// NameCommands maps each command whose value is a new name (command/key: a key of its block) to the
// name's database, which the link reading the name (scope:x, var:x) names too; one for every game.
var NameCommands = map[string]string{
	"save_scope_as": "scope", "save_temporary_scope_as": "scope",
	"save_scope_value_as/name": "scope", "save_temporary_scope_value_as/name": "scope",
	"save_opinion_value_as/name": "scope", "save_temporary_opinion_value_as/name": "scope",
	"set_variable": "var", "set_variable/name": "var",
	"set_local_variable": "local_var", "set_local_variable/name": "local_var",
	"set_global_variable": "global_var", "set_global_variable/name": "global_var",
	"add_to_variable_list/name": "var", "add_to_local_variable_list/name": "local_var",
	"add_to_global_variable_list/name": "global_var",
}

// sharedFolders are the rows every game has; EU5 has no music_player_categories and its vanilla no
// data_binding (its mods do), so those rows match nothing there.
var sharedFolders = map[string]Folder{
	"common/scripted_effects":   {CalledIn: []string{"effect"}, Body: []string{"effect"}, Keyword: "scripted_effect", Parameters: true},
	"common/scripted_triggers":  {CalledIn: []string{"trigger"}, Body: []string{"trigger"}, Keyword: "scripted_trigger", Parameters: true},
	"common/scripted_modifiers": {CalledIn: []string{"value"}, Body: []string{"value"}, Parameters: true},
	"common/script_values":      {CalledIn: []string{"trigger", "value"}, Body: []string{"value"}},
	"events":                    {Events: true},
	"common/genes": {Wrappers: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes",
		"decal_atlases", "age_presets"}},
	"common/named_colors":              {Wrappers: []string{"colors"}},
	"data_binding":                     {NameKey: "definition", Functions: true},
	"common/game_concepts":             {Concepts: true},
	"common/trigger_localization":      {Describes: "trigger"},
	"common/effect_localization":       {Describes: "effect"},
	"common/achievement_groups.txt":    {NameKey: "name"},
	"common/coat_of_arms/options":      {Unnamed: true},
	"common/coat_of_arms/coat_of_arms": {Wrappers: []string{"template"}},
	"common/coat_of_arms/template_lists": {Wrappers: []string{"coat_of_arms_template_lists",
		"pattern_texture_lists", "colored_emblem_texture_lists", "textured_emblem_texture_lists", "color_lists"}},
	"gui/scripted_widgets":          {Unnamed: true},
	"music/music_player_categories": {NameKey: "id"},
}

// withShared is the shared rows with one game's own rows over them.
func withShared(own map[string]Folder) map[string]Folder {
	rows := maps.Clone(sharedFolders)
	maps.Copy(rows, own)
	return rows
}

// GameList is the Wails payload for ListGames (consts are not exported).
type GameList struct {
	OriginVanilla string      `json:"originVanilla"`
	Games         []*GameInfo `json:"games"`
}

// registry is the supported-game table, keyed by id.
var registry = map[string]*GameInfo{
	"ck3": {
		ID:                 "ck3",
		Name:               "Crusader Kings III",
		ShortName:          "CK3",
		WikiAPI:            "https://ck3.paradoxwikis.com/api.php",
		ScriptRoot:         "game",
		Descriptor:         "mod",
		DocsFolderName:     "Crusader Kings III",
		ScriptDocsDirs:     []string{"logs", "docs"},
		DataTypesDir:       "logs/data_types",
		SteamAppID:         1158310,
		SteamLibrary:       map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FirstWinsDatabases: []string{"gui.type", "gui.template"},
		FirstWins:          "GUI types and templates are first-wins.",
		FileTypes:          fileTypes,
		Folders: withShared(map[string]Folder{
			"common/on_action": {OnActions: true,
				Refs: []string{"on_actions", "random_on_action/<number>", "first_valid_on_action", "fallback"}},
			"gfx/map/map_object_data":  {Unnamed: true},
			"history/titles":           {History: true, Of: []string{"landed_titles"}},
			"history/provinces":        {History: true, Of: []string{"map_data.definition"}},
			"history/province_mapping": {History: true, Of: []string{"map_data.definition"}},
			"history/cultures":         {History: true, Of: []string{"culture/cultures", "culture/pillars"}, ByFile: true},
			"history/faiths":           {History: true, Of: []string{"religion/faith_types"}},
			"history/artifacts":        {History: true},
			"history/situations":       {History: true},
			"history/struggles":        {History: true},
			"history/wars":             {History: true},
		}),
		Nested: []Nested{
			{Parent: "common/landed_titles", Path: "", Database: "self"},
			{Parent: "common/defines", Path: "", Database: "define"},
			{Parent: "common/activities/activity_types", Path: "options", Database: "option_category"},
			{Parent: "common/activities/activity_types", Path: "phases", Database: "phase"},
			{Parent: "common", Path: "**/parameters", Database: "parameter"},
			{Parent: "common", Path: "**/*_parameters", Database: "parameter"},
			{Parent: "common/game_concepts", Path: "alias", Database: "self"},
		},
		MapTable:         MapTable{Database: "map_data.definition", File: "map_data/definition.csv"},
		ModFolders:       []string{"common", "events", "localization"},
		DataTypesCommand: "dump_data_types",
	},
	"vic3": {
		ID:                 "vic3",
		Name:               "Victoria 3",
		ShortName:          "Vic3",
		WikiAPI:            "https://vic3.paradoxwikis.com/api.php",
		ScriptRoot:         "game",
		Descriptor:         "metadata",
		DocsFolderName:     "Victoria 3",
		ScriptDocsDirs:     []string{"logs", "docs"},
		DataTypesDir:       "logs/data_types",
		EntryModes:         entryModes,
		SteamAppID:         529340,
		SteamLibrary:       map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FirstWinsDatabases: []string{"gui.type", "gui.template", "events"},
		FirstWins:          "GUI types, templates, and events are first-wins.",
		FileTypes:          fileTypes,
		Folders: withShared(map[string]Folder{
			"common/on_actions": {OnActions: true,
				Refs: []string{"on_actions", "random_on_actions/<number>", "first_valid_on_action", "fallback"}},
			"gfx/map/map_object_data": {Unnamed: true},
			"gfx/map/post_effects":    {NameKey: "name", Wrappers: []string{"posteffect_volumes"}},
			"common/history":          {History: true},
		}),
		Nested:           []Nested{{Parent: "common/defines", Path: "", Database: "define"}},
		ModFolders:       []string{"common", "events", "localization"},
		DataTypesCommand: "DumpDataTypes",
	},
	"eu5": {
		ID:         "eu5",
		Name:       "Europa Universalis V",
		ShortName:  "EU5",
		WikiAPI:    "https://eu5.paradoxwikis.com/api.php",
		ScriptRoot: "game",
		StageRoots: []string{"in_game", "main_menu", "loading_screen"},
		Descriptor: "metadata",
		// where EU5 mods keep it; GAME-SYNTAX.md has the counts
		ThumbnailDir:       ".metadata",
		DocsFolderName:     "Europa Universalis V",
		ScriptDocsDirs:     []string{"logs", "docs"},
		DataTypesDir:       "logs/data_types",
		EntryModes:         entryModes,
		SteamAppID:         3450310,
		SteamLibrary:       map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build: Proton
		FirstWinsDatabases: []string{"events"},
		FirstWins:          "Events are first-wins; GUI types last-wins.",
		FileTypes:          fileTypes,
		Folders: withShared(map[string]Folder{
			"common/on_action": {OnActions: true,
				Refs: []string{"on_actions", "random_on_action/<number>", "first_valid_on_action", "fallback"}},
			"gfx/map/map_objects":                  {Unnamed: true},
			"gfx/map/city_data":                    {NameKey: "name"},
			"setup/start":                          {History: true},
			"setup/templates":                      {History: true, ByFile: true},
			"map_data/location_templates.txt":      {History: true, Of: []string{"map_data/named_locations"}},
			"map_data/generated_locators_port.txt": {Unnamed: true},
		}),
		Nested: []Nested{
			{Parent: "common/defines", Path: "", Database: "define"},
			{Parent: "gfx/defines", Path: "", Database: "define"},
			{Parent: "common/laws", Path: "", Database: "policy"},
			{Parent: "common/languages", Path: "dialects", Database: "dialect"},
			{Parent: "common/gods", Path: "omens", Database: "omen"},
			{Parent: "common/building_types", Path: "unique_production_methods", Database: "unique_production_method"},
			{Parent: "common/game_concepts", Path: "alias", Database: "self"},
			{Parent: "map_data/definitions.txt", Path: "", Database: "sub_continent"},
			{Parent: "map_data/definitions.txt", Path: "*", Database: "region"},
			{Parent: "map_data/definitions.txt", Path: "*/*", Database: "area"},
			{Parent: "map_data/definitions.txt", Path: "*/*/*", Database: "province"},
			{Parent: "setup/start", Path: "character_db", Database: "character"},
			{Parent: "setup/start", Path: "dynasty_manager", Database: "dynasty"},
			{Parent: "setup/start", Path: "countries/countries"},
			{Parent: "setup/start", Path: "locations"},
			{Parent: "setup/start", Path: "road_network"},
			{Parent: "setup/start", Path: "townrights_manager"},
			{Parent: "setup/start", Path: "building_manager"},
			{Parent: "setup/start", Path: "work_of_art_manager"},
			{Parent: "setup/start", Path: "colony_manager"},
			{Parent: "setup/start", Path: "institution_manager/institutions"},
			{Parent: "setup/start", Path: "situation_manager"},
		},
		ModFolders:       []string{"in_game/common", "in_game/events", "main_menu/localization"},
		DataTypesCommand: "dump_data_types",
	},
}

// Get returns the GameInfo for id, or nil if the game is not supported.
func Get(id string) *GameInfo {
	return registry[id]
}

// All returns supported games in UI order (ck3, eu5, vic3).
func All() []*GameInfo {
	return []*GameInfo{registry["ck3"], registry["eu5"], registry["vic3"]}
}

// FirstWinsAt reports whether database resolves first-in-order for this game.
// For an empty or unknown gameID only GUI types are.
func FirstWinsAt(gameID, database string) bool {
	if info := Get(gameID); info != nil {
		return slices.Contains(info.FirstWinsDatabases, database)
	}
	return database == "gui.type"
}

// DetectedInstall is a game install found on disk.
type DetectedInstall struct {
	GameID  string `json:"gameId"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	AppID   int    `json:"appId"`
}

var (
	vdfPathRe    = regexp.MustCompile(`"path"\s*"([^"]+)"`)
	installDirRe = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)
	buildIDRe    = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
)

type launcherSettings struct {
	RawVersion   string `json:"rawVersion"`
	Version      string `json:"version"`
	GameDataPath string `json:"gameDataPath"`
}

// ReadGameVersion is launcher-settings.json's rawVersion, else its version without the codename,
// else (EU5) the Steam manifest's buildid two folders up, as "build <id>"; "" for none (D-11).
func ReadGameVersion(installPath string, steamAppID int) string {
	var settings launcherSettings
	if data, err := os.ReadFile(filepath.Join(installPath, "launcher", "launcher-settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings.RawVersion != "" {
		return settings.RawVersion
	}
	version, _, _ := strings.Cut(settings.Version, " (")
	if version = strings.TrimSpace(version); version != "" {
		return version
	}

	manifest, err := os.ReadFile(filepath.Join(installPath, "..", "..", "appmanifest_"+strconv.Itoa(steamAppID)+".acf"))
	if match := buildIDRe.FindSubmatch(manifest); err == nil && match != nil {
		return "build " + string(match[1])
	}
	return ""
}

// DumpDirs is every folder the game's console dumps land in, for the install at installPath:
// script_docs's, then dump_data_types's; none where the game has no user-data folder.
func DumpDirs(gameID, installPath string) []string {
	user := UserDataDir(gameID, installPath)
	if user == "" {
		return nil
	}
	info := Get(gameID)
	var dirs []string
	for _, dir := range append(slices.Clone(info.ScriptDocsDirs), info.DataTypesDir) {
		dirs = append(dirs, filepath.Join(user, filepath.FromSlash(dir)))
	}
	return dirs
}

// UserDataDir is the OS user-data folder for gameID (script_docs, default mod/).
// First existing candidate wins; if none exist, the preferred conventional path.
func UserDataDir(gameID, installPath string) string {
	candidates := userDataCandidates(gameID, installPath)
	for _, candidate := range candidates {
		if dirExists(candidate) {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func userDataCandidates(gameID, installPath string) []string {
	info := Get(gameID)
	if info == nil {
		return nil
	}
	name := info.DocsFolderName
	var candidates []string
	add := func(path string) {
		if path != "" {
			candidates = append(candidates, filepath.Clean(path))
		}
	}
	add(launcherGameDataPath(installPath))
	home, homeErr := os.UserHomeDir()
	nativeDocs := ""
	if homeErr == nil {
		nativeDocs = filepath.Join(home, "Documents", "Paradox Interactive", name)
	}
	switch runtime.GOOS {
	case "windows":
		add(nativeDocs)
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" && homeErr == nil {
			xdg = filepath.Join(home, ".local", "share")
		}
		if xdg != "" {
			add(filepath.Join(xdg, "Paradox Interactive", name))
		}
		add(nativeDocs)
		appID := strconv.Itoa(info.SteamAppID)
		for _, steam := range steamLibraries() {
			add(filepath.Join(
				steam, "steamapps", "compatdata", appID,
				"pfx", "drive_c", "users", "steamuser",
				"Documents", "Paradox Interactive", name,
			))
		}
	}
	return candidates
}

func launcherGameDataPath(installPath string) string {
	if installPath == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(installPath, "launcher", "launcher-settings.json"))
	if err != nil {
		return ""
	}
	var settings launcherSettings
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	return expandGameDataPath(settings.GameDataPath)
}

func expandGameDataPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.Contains(path, "%USER_DOCUMENTS%") {
		home, err := os.UserHomeDir()
		docs := ""
		if err == nil {
			docs = filepath.Join(home, "Documents")
		}
		path = strings.ReplaceAll(path, "%USER_DOCUMENTS%", docs)
	}
	return filepath.Clean(filepath.FromSlash(path))
}

// DescriptorPath is the expected descriptor file for gameID under a mod root.
func DescriptorPath(gameID, root string) string {
	return filepath.Join(root, filepath.FromSlash(DescriptorFile(gameID)))
}

// DescriptorFile is the mod-root descriptor's path relative to the mod, in slash form.
func DescriptorFile(gameID string) string {
	if info := Get(gameID); info != nil && info.Descriptor == "metadata" {
		return ".metadata/metadata.json"
	}
	return "descriptor.mod"
}

// FindInstalls returns Steam-detected installs for a game.
func FindInstalls(gameID string) []DetectedInstall {
	info := Get(gameID)
	if info == nil {
		return nil
	}
	var out []DetectedInstall
	for _, lib := range steamLibraries() {
		manifest := filepath.Join(lib, "steamapps", "appmanifest_"+strconv.Itoa(info.SteamAppID)+".acf")
		data, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}
		match := installDirRe.FindSubmatch(data)
		if match == nil {
			continue
		}
		path := filepath.Join(lib, "steamapps", "common", string(match[1]))
		if !dirExists(path) {
			continue
		}
		out = append(out, DetectedInstall{
			GameID:  gameID,
			Name:    info.Name,
			Path:    path,
			Version: ReadGameVersion(path, info.SteamAppID),
			AppID:   info.SteamAppID,
		})
	}
	return out
}

// steamLibraries returns candidate Steam library roots by reading
// libraryfolders.vdf from each default Steam install location.
func steamLibraries() []string {
	seen := map[string]bool{}
	var libs []string
	// Resolved before dedup: ~/.steam/steam is usually a link to ~/.local/share/Steam.
	add := func(path string) {
		if path == "" {
			return
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		if seen[path] {
			return
		}
		seen[path] = true
		libs = append(libs, path)
	}
	for _, root := range defaultSteamRoots() {
		add(root)
		vdf := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		data, err := os.ReadFile(vdf)
		if err != nil {
			continue
		}
		for _, match := range vdfPathRe.FindAllSubmatch(data, -1) {
			add(filepath.FromSlash(strings.ReplaceAll(string(match[1]), `\\`, `/`)))
		}
	}
	return libs
}

// defaultSteamRoots returns the platform's typical Steam install directories.
func defaultSteamRoots() []string {
	switch runtime.GOOS {
	case "windows":
		var roots []string
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if base := os.Getenv(env); base != "" {
				roots = append(roots, filepath.Join(base, "Steam"))
			}
		}
		return roots
	default:
		home, _ := os.UserHomeDir()
		return []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		}
	}
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

// InstallUpdatedAt is when the game's files last changed, for judging whether
// generated files (script_docs) still describe the installed build.
//
// The binaries' mtimes are the signal: launcher/launcher-settings.json is missing on
// EU5 and lags the update on CK3, and Steam's appmanifest covers only Steam copies.
// "Last changed" is deliberately not "highest version": Steam stamps files as it
// writes them, so switching to an older build (a beta branch) moves the time forward
// too, which comparing versions would miss, and EU5 publishes no version at all.
// Copying an install preserves mtimes, which can only make this quieter.
//
// Zero time means unknown; callers must treat that as "cannot tell", never stale.
func InstallUpdatedAt(installPath string) time.Time {
	if installPath == "" {
		return time.Time{}
	}
	for _, dir := range []string{filepath.Join(installPath, "binaries"), installPath} {
		if newest := newestFileIn(dir); !newest.IsZero() {
			return newest
		}
	}
	return time.Time{}
}

// newestFileIn is the newest mtime among the files directly in dir. Directory
// mtimes are not used: replacing a file in place leaves the parent untouched.
func newestFileIn(dir string) time.Time {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

// DescriptorModFile, MetadataDir and MetadataFile name the descriptor files. They are
// constants rather than registry fields because the layout is a Paradox convention;
// which descriptor a game uses is GameInfo.Descriptor.
const (
	DescriptorModFile = "descriptor.mod"
	MetadataDir       = ".metadata"
	MetadataFile      = "metadata.json"
)
