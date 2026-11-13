// install.go finds a game on this machine: its installs, their versions and
// the folders and program each one uses.

package game

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
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

	// Version is "" where none was found.
	Version string `json:"version"`
}

// launcherSettings is the part of an install's launcher settings the app
// reads.
type launcherSettings struct {
	// RawVersion is the bare version, as 1.12.5. Version may add a code name,
	// as 1.13.11 (Chamomile).
	RawVersion string `json:"rawVersion"`
	Version    string `json:"version"`

	// GameDataPath is the user-data folder. It may hold %USER_DOCUMENTS%.
	GameDataPath string `json:"gameDataPath"`

	// ExePath is the game's program, from the settings file's folder.
	ExePath string   `json:"exePath"`
	ExeArgs []string `json:"exeArgs"`
}

// DLCFolder holds each DLC's files, under the game's own folder.
const DLCFolder = "dlc"

// LauncherModFolder is the launcher's mod folder, under the user-data folder.
const LauncherModFolder = "mod"

// DebugLogRel is the game's debug log, from the user-data folder.
const DebugLogRel = "logs/debug.log"

// installDirRe finds the game's folder in a Steam app manifest.
var installDirRe = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)

// manifestName is the file name of the game's Steam app manifest.
func (g *Game) manifestName() string {
	return "appmanifest_" + strconv.Itoa(g.SteamAppID) + ".acf"
}

// FindInstalls returns the game's installs in Steam's libraries, each with
// its version. A library whose manifest is unreadable or names a missing
// folder is skipped.
func (g *Game) FindInstalls() []DetectedInstall {
	var installs []DetectedInstall
	for _, library := range findSteamLibraries() {
		data, err := os.ReadFile(filepath.Join(library, "steamapps", g.manifestName()))
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

// vdfPathRe finds a library's folder in libraryfolders.vdf.
var vdfPathRe = regexp.MustCompile(`"path"\s*"([^"]+)"`)

// findSteamLibraries returns Steam's own folders and the libraries its
// libraryfolders.vdf names, each once after links are resolved.
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

// listDefaultSteamRoots returns the folders Steam is usually installed in.
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

// revisionLogRel is the log the game writes its build day to, from the
// user-data folder.
const revisionLogRel = "logs/code_revisions.log"

// gameVersionRe finds the version in the debug log, gameTimestampRe the
// build day in the revision log, and buildIDRe the build number in Steam's
// manifest.
var (
	gameVersionRe   = regexp.MustCompile(`\]: Feeding game version into checksum: (\S+)`)
	gameTimestampRe = regexp.MustCompile(`\]: game_timestamp: (\d{4}-\d{2}-\d{2})`)
	buildIDRe       = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
)

// LoadVersion returns an install's version: the launcher's, else the one
// the game logged, else "build" and its build day or Steam build number.
// Logs written before the install's last update are ignored, since they
// belong to the version before. It returns "" if nothing gives a version.
func (g *Game) LoadVersion(installPath string) string {
	settings := loadLauncher(installPath)
	if settings.RawVersion != "" {
		return settings.RawVersion
	}

	version, _, _ := strings.Cut(settings.Version, " (")
	if version = strings.TrimSpace(version); version != "" {
		return version
	}

	if user := g.findUserDataDir(settings.GameDataPath); user != "" {
		updatedAt := ReadInstallUpdatedAt(installPath)
		if match := findCurrentMatch(files.Join(user, DebugLogRel), updatedAt, gameVersionRe); match != "" {
			return match
		}

		if match := findCurrentMatch(files.Join(user, revisionLogRel), updatedAt, gameTimestampRe); match != "" {
			return "build " + match
		}
	}

	manifest, err := os.ReadFile(filepath.Join(installPath, "..", "..", g.manifestName()))
	if match := buildIDRe.FindSubmatch(manifest); err == nil && match != nil {
		return "build " + string(match[1])
	}

	return ""
}

// loadLauncher returns an install's launcher settings, empty if the file is
// missing or cannot be decoded.
func loadLauncher(installPath string) (settings launcherSettings) {
	if installPath == "" {
		return settings
	}

	if data, err := os.ReadFile(files.Join(installPath, launcherFile)); err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	return settings
}

// FindUserDataDir returns the folder the game keeps the user's data in, or
// "" if there is none it could be.
func (g *Game) FindUserDataDir(installPath string) string {
	return g.findUserDataDir(loadLauncher(installPath).GameDataPath)
}

// findUserDataDir returns the first folder that exists of those the user's
// data may be in, else the first of them. They are the launcher's
// gameDataPath, then the usual folders of this system, Proton's prefixes
// among them on Linux.
func (g *Game) findUserDataDir(gameDataPath string) string {
	var candidates []string
	add := func(path string) {
		if path != "" {
			candidates = append(candidates, filepath.Clean(path))
		}
	}

	add(expandGameDataPath(gameDataPath))

	home, homeErr := os.UserHomeDir()
	nativeDocs := ""
	if homeErr == nil {
		nativeDocs = filepath.Join(home, "Documents", "Paradox Interactive", g.Name)
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
			add(filepath.Join(xdg, "Paradox Interactive", g.Name))
		}

		add(nativeDocs)

		appID := strconv.Itoa(g.SteamAppID)
		for _, steam := range findSteamLibraries() {
			add(filepath.Join(
				steam, "steamapps", "compatdata", appID,
				"pfx", "drive_c", "users", "steamuser",
				"Documents", "Paradox Interactive", g.Name,
			))
		}
	}

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

// expandGameDataPath turns the launcher's gameDataPath into a path of this
// system, with %USER_DOCUMENTS% replaced by the user's Documents folder.
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

// findCurrentMatch returns the pattern's first group in a game log. It
// returns "" if the log is missing, was written before the install's last
// update, or has no match.
func findCurrentMatch(path string, updatedAt time.Time, pattern *regexp.Regexp) string {
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Before(updatedAt) {
		return ""
	}

	data, err := os.ReadFile(path)
	if match := pattern.FindSubmatch(data); err == nil && match != nil {
		return string(match[1])
	}

	return ""
}

// ReadInstallUpdatedAt returns when an install was last updated: the time of
// the newest file in its binaries folder, else in the install's own folder.
// It returns the zero time if neither holds a file.
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

// CompareVersions orders two versions part by part, as cmp.Compare does, so
// 1.20.1.2 comes after 1.20.1. A missing part counts as 0, and a part that is
// not a number, as the X of 1.20.X, comes before any number.
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
			return cmp.Compare(firstNumber, secondNumber)
		}
	}

	return 0
}

// toVersionPart returns one part of a version as a number, 0 for a part the
// version does not have, and an error for a part that is not a number.
func toVersionPart(parts []string, position int) (int, error) {
	if position >= len(parts) {
		return 0, nil
	}

	return strconv.Atoi(parts[position])
}

// IsWorkshopFolder reports whether a folder is inside one of the Workshop
// roots ListWorkshopRoots returns. A caller asking about many folders lists
// the roots once, since listing them reads Steam's library file.
func IsWorkshopFolder(roots []string, dir string) bool {
	return slices.ContainsFunc(roots, func(root string) bool { return files.IsUnder(root, dir) })
}

// ListWorkshopFolders returns the folder of each of the game's Workshop items
// that Steam has downloaded.
func (g *Game) ListWorkshopFolders() []string {
	var folders []string
	for _, root := range g.ListWorkshopRoots() {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if entry.IsDir() {
				folders = append(folders, filepath.Join(root, entry.Name()))
			}
		}
	}

	return folders
}

// ListWorkshopRoots returns the game's Workshop folder in each Steam library.
// It reads Steam's library file on each call.
func (g *Game) ListWorkshopRoots() []string {
	appID := strconv.Itoa(g.SteamAppID)

	var roots []string
	for _, library := range findSteamLibraries() {
		roots = append(roots, filepath.Join(library, "steamapps", "workshop", "content", appID))
	}

	return roots
}

// installRoots are the folders of an install the game mounts, in order.
var installRoots = []string{"clausewitz", "jomini", ScriptRoot}

// ListInstallRoots returns the folders of an install the game mounts, in
// order: the engine's two, the game's own, then each DLC's. It fails only if
// the DLC folder exists and cannot be read.
func (g *Game) ListInstallRoots(installPath string) ([]files.Root, error) {
	var roots []files.Root
	for _, folder := range installRoots {
		if dir := filepath.Join(installPath, folder); files.IsFolder(dir) {
			roots = append(roots, files.Root{Dir: dir, Origin: files.OriginVanilla, Stages: g.Stages})
		}
	}

	dlcFolder := filepath.Join(installPath, ScriptRoot, DLCFolder)
	dlcs, err := os.ReadDir(dlcFolder)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
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

// dumpFolders are where the game writes its dump files, under the user-data
// folder.
var dumpFolders = []string{"logs", "docs", "logs/data_types"}

// FindDumpDirs returns the folders of an install's dump files, or none if
// the game has no user-data folder.
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

// ResolveProgram returns the program that starts the game and its arguments.
// Where the game has launcher settings, their program wins and their
// arguments come before the debug flags. It returns false if no program is
// known.
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

const checksumRel = "binaries/checksum.txt"

// LoadChecksum returns the checksum written beside an install's program, or
// "" if there is none.
func LoadChecksum(installPath string) string {
	data, err := os.ReadFile(filepath.Join(installPath, checksumRel))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
