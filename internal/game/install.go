// install.go finds a game on this machine: its installs and their versions,
// its user-data and dump folders, its Workshop folders, and the program
// that starts it.

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

// DetectedInstall is one install of a game found in a Steam library.
type DetectedInstall struct {
	Name string `json:"name"`
	Path string `json:"path"`

	// Version is the install's version. It is "" where none was found.
	Version string `json:"version"`
}

// launcherSettings is the part of an install's launcher-settings.json that
// the app reads.
type launcherSettings struct {
	// RawVersion is the version alone, as 1.12.5. Version adds the
	// version's code name, as 1.13.11 (Chamomile).
	RawVersion string `json:"rawVersion"`
	Version    string `json:"version"`

	// GameDataPath is the user-data folder. It may hold %USER_DOCUMENTS%
	// for the user's Documents folder.
	GameDataPath string `json:"gameDataPath"`

	// ExePath is the path of the game's program, from the folder of the
	// settings file. ExeArgs are the arguments it is started with.
	ExePath string   `json:"exePath"`
	ExeArgs []string `json:"exeArgs"`
}

// DLCFolder is the folder that holds each DLC's files, under the game's own
// folder.
const DLCFolder = "dlc"

// LauncherModFolder is the folder the launcher keeps mods in, under the
// user-data folder.
const LauncherModFolder = "mod"

// DebugLogRel is the path of the game's debug log, from the user-data
// folder.
const DebugLogRel = "logs/debug.log"

// installDirRe is the line of a Steam manifest that names the game's
// folder.
var installDirRe = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)

// FindInstalls returns the installs of the game found in Steam's libraries,
// each with its version. A library has none if its manifest for the game
// cannot be read, or names a folder that does not exist.
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

// vdfPathRe is a line of libraryfolders.vdf that names a library's folder.
var vdfPathRe = regexp.MustCompile(`"path"\s*"([^"]+)"`)

// findSteamLibraries returns Steam's own folders and the libraries its
// libraryfolders.vdf names. A folder reached through a link is listed once,
// under the folder the link leads to.
func findSteamLibraries() []string {
	seen := map[string]bool{}
	var libraries []string

	// ~/.steam/steam is usually a link to ~/.local/share/Steam
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

// listDefaultSteamRoots returns the folders Steam is usually installed in
// on this system.
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

// revisionLogRel is the path of the log the game writes the day it was
// built to, from the user-data folder.
const revisionLogRel = "logs/code_revisions.log"

// gameVersionRe is the line of the debug log that gives the game's version.
// gameTimestampRe is the line of the revision log that gives the day the
// game was built. buildIDRe is the line of Steam's manifest that gives the
// build's number.
var (
	gameVersionRe   = regexp.MustCompile(`\]: Feeding game version into checksum: (\S+)`)
	gameTimestampRe = regexp.MustCompile(`\]: game_timestamp: (\d{4}-\d{2}-\d{2})`)
	buildIDRe       = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
)

// LoadVersion returns the version of an install. It is the version in the
// launcher's settings. Without one, it is the version in the game's debug
// log, else "build" and the day the game was built, from its revision log.
// A log is read only if the game was started after the install was last
// updated. Without those, it is "build" and Steam's build number. It
// returns "" if nothing gives a version.
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
		if match := findCurrentMatch(installPath, files.Join(user, DebugLogRel), gameVersionRe); match != "" {
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

// loadLauncher returns the launcher settings of an install. It returns
// empty settings if the file is missing or cannot be decoded.
func loadLauncher(installPath string) (settings launcherSettings) {
	if installPath == "" {
		return settings
	}

	if data, err := os.ReadFile(files.Join(installPath, launcherFile)); err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	return settings
}

// FindUserDataDir returns the folder the game keeps the user's data in: the
// first that exists of the folders it may be, else the first of them. It
// returns "" if there are none.
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

// listUserDataCandidates returns the folders the user's data may be in, the
// likeliest first: the one the launcher's settings name, then the usual
// ones for this system.
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

// expandGameDataPath returns the launcher's gameDataPath as a path of this
// system, with %USER_DOCUMENTS% replaced by the user's Documents folder. It
// returns "" for an empty path.
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

// findCurrentMatch returns what the pattern's first group matches in one of
// the game's logs. It returns "" if the log is missing, was written before
// the install was last updated, or has no match.
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

// ReadInstallUpdatedAt returns when an install was last updated: the time
// of the newest file in its binaries folder, else of the newest file in the
// install's own folder. It returns the zero time if neither holds a file.
func ReadInstallUpdatedAt(installPath string) time.Time {
	if installPath == "" {
		return time.Time{}
	}

	// a folder's own time is no use: replacing a file in place leaves it
	// unchanged
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

// CompareVersions orders two versions by their numbers, part by part:
// 1.20.1.2 comes after 1.20.1. A missing part counts as 0. A part that is
// no number, as the X of 1.20.X, comes before any number. It returns what
// cmp.Compare does.
func CompareVersions(first, second string) int {
	firstParts, secondParts := strings.Split(first, "."), strings.Split(second, ".")
	for position := range max(len(firstParts), len(secondParts)) {
		firstNumber, firstErr := toVersionPart(firstParts, position)
		secondNumber, secondErr := toVersionPart(secondParts, position)
		switch {
		case firstErr != nil && secondErr != nil:
			if order := strings.Compare(firstParts[position], secondParts[position]); order != 0 {
				return order
			}

		case firstErr != nil:
			return -1

		case secondErr != nil:
			return 1

		case firstNumber != secondNumber:
			return firstNumber - secondNumber
		}
	}

	return 0
}

// toVersionPart returns one part of a version as a number, and 0 for a part
// the version does not have. It returns an error for a part that is no
// number.
func toVersionPart(parts []string, position int) (int, error) {
	if position >= len(parts) {
		return 0, nil
	}

	return strconv.Atoi(parts[position])
}

// IsWorkshopFolder reports whether a folder is inside one of the game's
// Workshop folders.
func (g *Game) IsWorkshopFolder(dir string) bool {
	return slices.ContainsFunc(g.listWorkshopRoots(), func(root string) bool { return files.IsUnder(root, dir) })
}

// ListWorkshopFolders returns the folder of each Workshop item of the game
// that Steam has downloaded.
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

// listWorkshopRoots returns the game's Workshop folder in each Steam
// library.
func (g *Game) listWorkshopRoots() []string {
	appID := strconv.Itoa(g.SteamAppID)

	var roots []string
	for _, library := range findSteamLibraries() {
		roots = append(roots, filepath.Join(library, "steamapps", "workshop", "content", appID))
	}

	return roots
}

// installRoots are the folders of an install that the game mounts, in the
// order it mounts them.
var installRoots = []string{"clausewitz", "jomini", ScriptRoot}

// ListInstallRoots returns the folders of an install that the game mounts,
// in order: the engine's two, the game's own, and then each DLC's. It
// returns an error if the DLC folder exists and cannot be read.
func (g *Game) ListInstallRoots(installPath string) ([]files.Root, error) {
	var roots []files.Root
	for _, folder := range installRoots {
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

// dumpFolders are the folders the game writes its dump files to, under the
// user-data folder.
var dumpFolders = []string{"logs", "docs", "logs/data_types"}

// FindDumpDirs returns the folders an install's dump files are in. It
// returns none if the game has no user-data folder.
func (g *Game) FindDumpDirs(installPath string) []string {
	user := g.FindUserDataDir(installPath)
	if user == "" {
		return nil
	}

	var dirs []string
	for _, dir := range dumpFolders {
		dirs = append(dirs, files.Join(user, dir))
	}

	return dirs
}

// ResolveProgram returns the program that starts the game, and its
// arguments. The program is the one the launcher's settings name, else the
// game's own. The arguments are the launcher's, then the flags for debug
// mode. It returns false if no program is known.
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

// checksumRel is the path of the file that holds an install's checksum,
// from the install's folder.
const checksumRel = "binaries/checksum.txt"

// LoadChecksum returns the checksum written beside an install's program. It
// returns "" if the file is missing.
func LoadChecksum(installPath string) string {
	data, err := os.ReadFile(filepath.Join(installPath, checksumRel))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
