// Package game is the only game-aware package in the backend: the table of supported games
// and what each one's install, mods and wiki look like. It also finds the installs, through the
// Steam library folders, and each game's user-data folder.
package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// OriginVanilla is the session/IDE origin id for game files.
const OriginVanilla = "vanilla"

// BaseLanguage is the localization every game ships whole, read beside a workspace's language.
const BaseLanguage = "english"

// GameInfo is the static identity and convention set for one game. It carries no
// derived data; the engine under lang/ scans the install for that.
type GameInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`

	// WikiAPI is the MediaWiki api.php endpoint for this game's wiki. Backend-only.
	WikiAPI string `json:"-"`

	// ScriptRoot is the install-relative folder holding script (e.g. "game"). EU5's
	// StageRoots sit under it: game/{in_game,main_menu,loading_screen}.
	// Mods omit game/ and put StageRoots at the mod root. Backend-only.
	ScriptRoot string   `json:"-"`
	StageRoots []string `json:"-"`

	// Descriptor is how a mod root is identified: "mod" (descriptor.mod) or
	// "metadata" (.metadata/metadata.json).
	Descriptor string `json:"descriptor,omitempty"`

	// ThumbnailDir is where a new listing image goes, from the mod root ("" the root); Descriptor
	// does not imply it (Vic3 keeps it at the root). An existing thumbnail stays. Backend-only.
	ThumbnailDir string `json:"-"`

	// DocsFolderName is the per-game Documents subfolder (e.g. "Crusader Kings III"). Backend-only.
	DocsFolderName string `json:"-"`

	SteamAppID int `json:"-"`
	// SteamLibrary is where the game's own copy of Steam's library sits in its install, by
	// GOOS; Workshop publish loads it. A GOOS with no row has no build of the game. Backend-only.
	SteamLibrary map[string]string `json:"-"`

	// FirstWins is Layout.FirstWinsDatabases as the sentence the Conflicts and Settings pages show.
	FirstWins string `json:"firstWins,omitempty"`

	// Layout is how the game's script files lay out their entries (lang-rules.go). Backend-only.
	Layout `json:"-"`

	// ModFolders are the folders a new mod gets, from its root; the localization one is the one whose
	// last segment is localization. Backend-only.
	ModFolders []string `json:"-"`
	// DataTypesCommand is the console command that writes the data-type dumps. Backend-only: the
	// frontend reads it from LanguageHealth.
	DataTypesCommand string `json:"-"`
}

// GameList is the Wails payload for ListGames (bindings carry no Go constants).
type GameList struct {
	OriginVanilla string      `json:"originVanilla"`
	Games         []*GameInfo `json:"games"`
}

// registry is the supported-game table, keyed by id.
var registry = map[string]*GameInfo{
	"ck3": {
		ID:               "ck3",
		Name:             "Crusader Kings III",
		ShortName:        "CK3",
		WikiAPI:          "https://ck3.paradoxwikis.com/api.php",
		ScriptRoot:       "game",
		Descriptor:       "mod",
		DocsFolderName:   "Crusader Kings III",
		SteamAppID:       1158310,
		SteamLibrary:     map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FirstWins:        "GUI types and templates are first-wins.",
		Layout:           layouts["ck3"],
		ModFolders:       []string{"common", "events", "localization"},
		DataTypesCommand: "dump_data_types",
	},
	"vic3": {
		ID:               "vic3",
		Name:             "Victoria 3",
		ShortName:        "Vic3",
		WikiAPI:          "https://vic3.paradoxwikis.com/api.php",
		ScriptRoot:       "game",
		Descriptor:       "metadata",
		DocsFolderName:   "Victoria 3",
		SteamAppID:       529340,
		SteamLibrary:     map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FirstWins:        "GUI types, templates, and events are first-wins.",
		Layout:           layouts["vic3"],
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
		// where EU5 mods keep it
		ThumbnailDir:     ".metadata",
		DocsFolderName:   "Europa Universalis V",
		SteamAppID:       3450310,
		SteamLibrary:     map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build: Proton
		FirstWins:        "Events are first-wins; GUI types last-wins.",
		Layout:           layouts["eu5"],
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
// else (EU5) the Steam manifest's buildid two folders up, as "build <id>"; "" for none.
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

// dumpDirs are where the console's dumps land in every game, from the user-data folder: CK3 writes
// script_docs to logs, Vic3 and EU5 event_scopes.log to logs and the rest to docs; dump_data_types
// writes to logs/data_types.
var dumpDirs = []string{"logs", "docs", "logs/data_types"}

// DumpDirs is every folder the game's console dumps land in, for the install at installPath; none
// where the game has no user-data folder.
func DumpDirs(gameID, installPath string) []string {
	user := UserDataDir(gameID, installPath)
	if user == "" {
		return nil
	}
	var dirs []string
	for _, dir := range dumpDirs {
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

// InstallUpdatedAt is the newest mtime among the install's binaries, else its top files: an update
// or a branch switch moves it, so a dump older than it is stale. Zero means unknown, never stale.
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

// MetadataDir is the folder a metadata descriptor sits in (.metadata/metadata.json).
const (
	MetadataDir = ".metadata"
)
