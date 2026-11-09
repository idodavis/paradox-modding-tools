// install.go finds where a game is on this machine: its installs and their
// versions, the folders it mounts, its user-data and dump folders, its
// Workshop folders and the program that starts it.

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

	"paradox-modding-tools/internal/files"
)

// DetectedInstall is an install found under a Steam library, with its version.
type DetectedInstall struct {
	Name string `json:"name"`

	// Path is the install folder.
	Path string `json:"path"`

	// Version is the install's version. It is "" where none was found.
	Version string `json:"version"`
}

// launcherSettings is the part of launcher-settings.json the app reads.
type launcherSettings struct {
	// RawVersion is the version as the game writes it, as 1.12.5, and Version
	// the version with its codename, as 1.13.11 (Chamomile).
	RawVersion string `json:"rawVersion"`
	Version    string `json:"version"`

	// GameDataPath is the user-data folder. It may hold %USER_DOCUMENTS%.
	GameDataPath string `json:"gameDataPath"`

	// ExePath is the game's executable, from the launcher settings' folder,
	// and ExeArgs the arguments the launcher starts it with.
	ExePath string   `json:"exePath"`
	ExeArgs []string `json:"exeArgs"`
}

// DLCFolder is the folder under the game folder that holds each DLC's own
// files, laid out as the game's are.
const DLCFolder = "dlc"

// LauncherModFolder is the folder under a game's user-data folder where its
// launcher keeps mods.
const LauncherModFolder = "mod"

// installDirRe reads the install folder that a Steam app manifest names.
var installDirRe = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)

// FindInstalls returns the installs of the game that Steam's libraries hold.
func (g *Game) FindInstalls() []DetectedInstall {
	var installs []DetectedInstall
	for _, library := range findSteamLibraries() {
		manifest := filepath.Join(library, "steamapps", "appmanifest_"+strconv.Itoa(g.SteamAppID)+".acf")
		data, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}

		match := installDirRe.FindSubmatch(data)
		if match == nil {
			continue
		}

		path := filepath.Join(library, "steamapps", "common", string(match[1]))
		if !files.IsFolder(path) {
			continue
		}

		installs = append(installs, DetectedInstall{Name: g.Name, Path: path, Version: g.LoadVersion(path)})
	}

	return installs
}

// vdfPathRe reads each library root that Steam's libraryfolders.vdf names.
var vdfPathRe = regexp.MustCompile(`"path"\s*"([^"]+)"`)

// findSteamLibraries returns each default Steam folder and the library roots
// its libraryfolders.vdf names, each once.
func findSteamLibraries() []string {
	seen := map[string]bool{}
	var libraries []string

	// Links are resolved before duplicates are dropped: ~/.steam/steam is
	// usually a link to ~/.local/share/Steam.
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
		libraries = append(libraries, path)
	}

	for _, root := range listDefaultSteamRoots() {
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

	return libraries
}

// listDefaultSteamRoots returns the folders Steam is usually installed in on
// this platform.
func listDefaultSteamRoots() []string {
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

// The debug log and the revision log every start writes, from the user-data
// folder.
const (
	debugLogRel    = "logs/debug.log"
	revisionLogRel = "logs/code_revisions.log"
)

// The patterns that read the version the game checks from its debug log, the
// day it was built from its revision log, and the build id from its Steam
// manifest.
var (
	gameVersionRe   = regexp.MustCompile(`\]: Feeding game version into checksum: (\S+)`)
	gameTimestampRe = regexp.MustCompile(`\]: game_timestamp: (\d{4}-\d{2}-\d{2})`)
	buildIDRe       = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
)

// LoadVersion returns the version of an install of the game: from its
// launcher settings, else from its debug log if the game started since its
// last update, else from its revision log in the same case as "build <date>",
// else from its Steam manifest as "build <id>". It returns "" if none of them
// says.
func (g *Game) LoadVersion(installPath string) string {
	settings := loadLauncher(installPath)
	if settings.RawVersion != "" {
		return settings.RawVersion
	}

	version, _, _ := strings.Cut(settings.Version, " (")
	if version = strings.TrimSpace(version); version != "" {
		return version
	}

	if user := g.FindUserDataDir(installPath); user != "" {
		if match := findCurrentMatch(installPath, files.Join(user, debugLogRel), gameVersionRe); match != "" {
			return match
		}

		if match := findCurrentMatch(installPath, files.Join(user, revisionLogRel), gameTimestampRe); match != "" {
			return "build " + match
		}
	}

	manifest, err := os.ReadFile(filepath.Join(installPath, "..", "..", "appmanifest_"+strconv.Itoa(g.SteamAppID)+".acf"))
	if match := buildIDRe.FindSubmatch(manifest); err == nil && match != nil {
		return "build " + string(match[1])
	}

	return ""
}

// loadLauncher reads an install's launcher settings. It returns the zero
// value if there are none or they do not decode.
func loadLauncher(installPath string) (settings launcherSettings) {
	if installPath == "" {
		return settings
	}

	if data, err := os.ReadFile(files.Join(installPath, launcherFile)); err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	return settings
}

// FindUserDataDir returns the game's user-data folder, or the most likely one
// if none exists.
func (g *Game) FindUserDataDir(installPath string) string {
	candidates := g.listUserDataCandidates(installPath)
	for _, candidate := range candidates {
		if files.IsFolder(candidate) {
			return candidate
		}
	}

	if len(candidates) > 0 {
		return candidates[0]
	}

	return ""
}

// listUserDataCandidates returns the folders a game may keep its user data
// in, most likely first.
func (g *Game) listUserDataCandidates(installPath string) []string {
	name := g.Name
	var candidates []string
	add := func(path string) {
		if path != "" {
			candidates = append(candidates, filepath.Clean(path))
		}
	}

	add(expandGameDataPath(loadLauncher(installPath).GameDataPath))
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
		appID := strconv.Itoa(g.SteamAppID)
		for _, steam := range findSteamLibraries() {
			add(filepath.Join(
				steam, "steamapps", "compatdata", appID,
				"pfx", "drive_c", "users", "steamuser",
				"Documents", "Paradox Interactive", name,
			))
		}
	}

	return candidates
}

// expandGameDataPath returns the launcher's gameDataPath with
// %USER_DOCUMENTS% filled in, or "" if there is none.
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

// findCurrentMatch returns the first group of the pattern's first match in a
// log of the game, if the game wrote the log since the install was last
// updated. It returns "" if the log is older, missing or unread, or the
// pattern matches nothing in it.
func findCurrentMatch(installPath, path string, pattern *regexp.Regexp) string {
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Before(ReadInstallUpdatedAt(installPath)) {
		return ""
	}

	data, err := os.ReadFile(path)
	if match := pattern.FindSubmatch(data); err == nil && match != nil {
		return string(match[1])
	}

	return ""
}

// ReadInstallUpdatedAt returns when an install was last updated. A dump older
// than that is stale. It returns the zero time if that is not known, which
// never counts as stale.
func ReadInstallUpdatedAt(installPath string) time.Time {
	if installPath == "" {
		return time.Time{}
	}

	// A folder's own time is no use: replacing a file in place leaves it
	// unchanged.
	for _, dir := range []string{filepath.Join(installPath, "binaries"), installPath} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
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

		if !newest.IsZero() {
			return newest
		}
	}

	return time.Time{}
}

// IsWorkshopFolder reports whether a folder is under the game's Steam Workshop
// folder in one of Steam's libraries.
func (g *Game) IsWorkshopFolder(dir string) bool {
	return slices.ContainsFunc(g.listWorkshopRoots(), func(root string) bool { return files.IsUnder(root, dir) })
}

// ListWorkshopFolders returns the folder of each Workshop item Steam has
// downloaded for the game, in each of Steam's libraries. It returns none if
// Steam has none.
func (g *Game) ListWorkshopFolders() []string {
	var folders []string
	for _, root := range g.listWorkshopRoots() {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if entry.IsDir() {
				folders = append(folders, filepath.Join(root, entry.Name()))
			}
		}
	}

	return folders
}

// listWorkshopRoots returns the game's Steam Workshop folder in each of
// Steam's libraries.
func (g *Game) listWorkshopRoots() []string {
	appID := strconv.Itoa(g.SteamAppID)

	var roots []string
	for _, library := range findSteamLibraries() {
		roots = append(roots, filepath.Join(library, "steamapps", "workshop", "content", appID))
	}

	return roots
}

// InstallRoots are the folders under an install that the game mounts, in the
// order it mounts them. A relative path begins under one of them.
var InstallRoots = []string{"clausewitz", "jomini", ScriptRoot}

// ListInstallRoots returns the folders of an install that the game reads, in
// the order it mounts them, with each DLC's last. Each carries the game's
// stage folders. It returns an error if the DLC folder exists and cannot be
// read.
func (g *Game) ListInstallRoots(installPath string) ([]files.Root, error) {
	var roots []files.Root
	for _, folder := range InstallRoots {
		if dir := filepath.Join(installPath, folder); files.IsFolder(dir) {
			roots = append(roots, files.Root{Dir: dir, Origin: files.OriginVanilla, Stages: g.Stages})
		}
	}

	dlcFolder := filepath.Join(installPath, ScriptRoot, DLCFolder)
	dlcs, err := os.ReadDir(dlcFolder)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	for _, dlc := range dlcs {
		if dlc.IsDir() {
			roots = append(roots, files.Root{
				Dir: filepath.Join(dlcFolder, dlc.Name()), Origin: files.OriginVanilla, Stages: g.Stages,
			})
		}
	}

	return roots, nil
}

// DumpFolders are the folders under a game's user data where the console's
// dumps land.
var DumpFolders = []string{"logs", "docs", "logs/data_types"}

// FindDumpDirs returns the folders the game's console dumps land in. It
// returns none if the game has no user-data folder.
func (g *Game) FindDumpDirs(installPath string) []string {
	user := g.FindUserDataDir(installPath)
	if user == "" {
		return nil
	}

	var dirs []string
	for _, dir := range DumpFolders {
		dirs = append(dirs, files.Join(user, dir))
	}

	return dirs
}

// ResolveProgram returns the executable that starts the game from an install,
// and its debug arguments with the row's flags last. The launcher settings
// win over the row. It returns false if the game has no executable on this
// system.
func (g *Game) ResolveProgram(installPath string) (path string, args []string, ok bool) {
	program := g.Program[runtime.GOOS]
	if program.Executable != "" {
		path = files.Join(installPath, program.Executable)
	}

	if program.Settings != "" {
		settings := loadLauncher(installPath)
		if settings.ExePath != "" {
			folder := filepath.Dir(files.Join(installPath, program.Settings))
			path = files.Join(folder, settings.ExePath)
		}

		args = settings.ExeArgs
	}

	return path, append(args, program.DebugArgs...), path != ""
}

// checksumRel is the file in which an install's build writes its hash.
const checksumRel = "binaries/checksum.txt"

// LoadChecksum returns the hash an install's build writes beside its
// executable. It returns "" if the install has none.
func LoadChecksum(installPath string) string {
	data, err := os.ReadFile(filepath.Join(installPath, checksumRel))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
