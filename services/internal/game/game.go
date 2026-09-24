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
	// ScriptDocsSubdir is where in-game console dumps land ("logs" or "docs").
	ScriptDocsSubdir string `json:"scriptDocsSubdir,omitempty"`

	// EntryModes are the words a mod may write before a definition's name (REPLACE:name), in
	// the order the game processes them; file order decides only between entries of one mode.
	// Victoria 3 and EU5 have all six words, CK3 none. Backend-only.
	EntryModes []EntryMode `json:"-"`

	SteamAppID int `json:"steamAppId,omitempty"`
	// SteamLibrary is where the game's own copy of Steam's library sits in its install, by
	// GOOS; Workshop publish loads it. A GOOS with no row has no build of the game. Backend-only.
	SteamLibrary map[string]string `json:"-"`

	// FIOSKinds are the kinds this game resolves first-in-order; every other
	// kind is last-in-order. Backend-only — FirstWins is the same fact as the
	// sentence the Conflicts and Settings pages show.
	FIOSKinds []string `json:"-"`
	FirstWins string   `json:"firstWins,omitempty"`

	// SubjectDirs are the folders whose definitions are named after other kinds' definitions:
	// history, coats of arms, flags. The learner makes a kind under one a subject kind of the
	// kind that defines its names, with no prefix. They are parent folders, so a folder the
	// game adds below one needs no row. Backend-only.
	SubjectDirs []string `json:"-"`
}

// EntryMode is a word written before a definition's name, and what it does to one already loaded.
type EntryMode struct {
	Word     string
	Replaces bool // takes the earlier definition's place; an inject adds to it and never displaces it
	Optional bool // TRY_: does nothing where there is none
	Creates  bool // _OR_CREATE: defines it where there is none
}

// entryModes is the six words Victoria 3 and EU5 share, in processing order (both wikis'
// Mod compatibility page).
var entryModes = []EntryMode{
	{Word: "INJECT_OR_CREATE", Creates: true}, {Word: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Word: "TRY_INJECT", Optional: true}, {Word: "TRY_REPLACE", Replaces: true, Optional: true},
	{Word: "INJECT"}, {Word: "REPLACE", Replaces: true},
}

// GameList is the Wails payload for ListGames (consts are not exported).
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
		ScriptDocsSubdir: "logs",
		SteamAppID:       1158310,
		SteamLibrary:     map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FIOSKinds:        []string{"gui.type", "gui.template"},
		FirstWins:        "GUI types and templates are first-wins.",
		SubjectDirs:      []string{"history", "common/coat_of_arms"},
	},
	"vic3": {
		ID:               "vic3",
		Name:             "Victoria 3",
		ShortName:        "Vic3",
		WikiAPI:          "https://vic3.paradoxwikis.com/api.php",
		ScriptRoot:       "game",
		Descriptor:       "metadata",
		DocsFolderName:   "Victoria 3",
		ScriptDocsSubdir: "docs",
		EntryModes:       entryModes,
		SteamAppID:       529340,
		SteamLibrary:     map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"},
		FIOSKinds:        []string{"gui.type", "gui.template", "events"},
		FirstWins:        "GUI types, templates, and events are first-wins.",
		SubjectDirs:      []string{"common/history", "common/coat_of_arms", "common/flag_definitions"},
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
		ThumbnailDir:     ".metadata",
		DocsFolderName:   "Europa Universalis V",
		ScriptDocsSubdir: "docs",
		EntryModes:       entryModes,
		SteamAppID:       3450310,
		SteamLibrary:     map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build; it runs under Proton
		FIOSKinds:        []string{"events"},
		FirstWins:        "Events are first-wins; GUI types last-wins.",
		SubjectDirs:      []string{"setup/start", "common/coat_of_arms", "common/flag_definitions"},
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

// IsFIOS reports whether kind resolves first-in-order for this game.
// For an empty or unknown gameID only GUI types are.
func IsFIOS(gameID, kind string) bool {
	if info := Get(gameID); info != nil {
		return slices.Contains(info.FIOSKinds, kind)
	}
	return kind == "gui.type"
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
)

type launcherSettings struct {
	RawVersion   string `json:"rawVersion"`
	Version      string `json:"version"`
	GameDataPath string `json:"gameDataPath"`
}

// ReadGameVersion reads rawVersion (then version, stripping a parenthetical
// codename) from launcher/launcher-settings.json. Missing file → "".
func ReadGameVersion(installPath string) string {
	data, err := os.ReadFile(filepath.Join(installPath, "launcher", "launcher-settings.json"))
	if err != nil {
		return ""
	}
	var ls launcherSettings
	if err := json.Unmarshal(data, &ls); err != nil {
		return ""
	}
	if ls.RawVersion != "" {
		return ls.RawVersion
	}
	value := ls.Version
	if i := strings.Index(value, " ("); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

// UserDataDir is the OS user-data folder for gameID (script_docs, default mod/).
// First existing candidate wins; if none exist, the preferred conventional path.
func UserDataDir(gameID, installPath string) string {
	cands := userDataCandidates(gameID, installPath)
	for _, cand := range cands {
		if dirExists(cand) {
			return cand
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

func userDataCandidates(gameID, installPath string) []string {
	info := Get(gameID)
	if info == nil {
		return nil
	}
	name := info.DocsFolderName
	var cands []string
	add := func(path string) {
		if path != "" {
			cands = append(cands, filepath.Clean(path))
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
	return cands
}

func launcherGameDataPath(installPath string) string {
	if installPath == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(installPath, "launcher", "launcher-settings.json"))
	if err != nil {
		return ""
	}
	var ls launcherSettings
	if json.Unmarshal(data, &ls) != nil {
		return ""
	}
	return expandGameDataPath(ls.GameDataPath)
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
			Version: ReadGameVersion(path),
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
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
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
	ents, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		fi, err := ent.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
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
