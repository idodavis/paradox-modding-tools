// install.go finds where a game is on this machine: its installs, version, user-data and dump folders, and program.

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

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
)

// A DetectedInstall is an install found under a Steam library, with its version.
type DetectedInstall struct {
	Name    string `json:"name"`
	Path    string `json:"path"`    // the install folder
	Version string `json:"version"` // "" means none was found
}

// launcherSettings is the part of launcher-settings.json the app reads.
type launcherSettings struct {
	RawVersion   string   `json:"rawVersion"`   // the version as the game writes it (1.12.5)
	Version      string   `json:"version"`      // the version with its codename (1.13.11 (Chamomile))
	GameDataPath string   `json:"gameDataPath"` // the user-data folder, %USER_DOCUMENTS% allowed
	ExePath      string   `json:"exePath"`      // the game's executable, from the launcher settings' folder
	ExeArgs      []string `json:"exeArgs"`      // the arguments the launcher starts it with
}

// DLCFolder is the folder under the game folder that holds each DLC's own files, laid out as the game's are.
const DLCFolder = "dlc"

// LauncherModFolder is the folder under a game's user-data folder where its launcher keeps mods.
const LauncherModFolder = "mod"

// The patterns that read Steam's library file, an app manifest and its build id, the day the game was built in its
// revision log and the version it checks in its debug log.
var (
	vdfPathRe       = regexp.MustCompile(`"path"\s*"([^"]+)"`)
	installDirRe    = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)
	buildIDRe       = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
	gameTimestampRe = regexp.MustCompile(`\]: game_timestamp: (\d{4}-\d{2}-\d{2})`)
	gameVersionRe   = regexp.MustCompile(`\]: Feeding game version into checksum: (\S+)`)
)

// FindInstalls returns the installs of the game that Steam's libraries hold.
func (game *Game) FindInstalls() []DetectedInstall {
	var installs []DetectedInstall
	for _, library := range findSteamLibraries() {
		manifest := filepath.Join(library, "steamapps", "appmanifest_"+strconv.Itoa(game.SteamAppID)+".acf")
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
		installs = append(installs, DetectedInstall{
			Name:    game.Name,
			Path:    path,
			Version: game.LoadVersion(path),
		})
	}
	return installs
}

// IsWorkshopFolder reports whether a folder is under the game's Steam Workshop folder in one of Steam's libraries.
func (game *Game) IsWorkshopFolder(dir string) bool {
	return slices.ContainsFunc(game.listWorkshopRoots(), func(root string) bool { return files.IsUnder(root, dir) })
}

// ListWorkshopFolders returns the folder of each Workshop item Steam has downloaded for the game, in each of Steam's
// libraries. It returns none if Steam has none.
func (game *Game) ListWorkshopFolders() []string {
	var folders []string
	for _, root := range game.listWorkshopRoots() {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if entry.IsDir() {
				folders = append(folders, filepath.Join(root, entry.Name()))
			}
		}
	}
	return folders
}

// listWorkshopRoots returns the game's Steam Workshop folder in each of Steam's libraries.
func (game *Game) listWorkshopRoots() []string {
	appID := strconv.Itoa(game.SteamAppID)
	return lo.Map(findSteamLibraries(), func(library string, _ int) string {
		return filepath.Join(library, "steamapps", "workshop", "content", appID)
	})
}

// findSteamLibraries returns each default Steam folder and the library roots its libraryfolders.vdf names.
func findSteamLibraries() []string {
	seen := map[string]bool{}
	var libraries []string
	// links are resolved before dedup: ~/.steam/steam is usually a link to ~/.local/share/Steam
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

// listDefaultSteamRoots returns the folders Steam is usually installed in on this platform.
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

// LoadVersion returns the version of an install of the game, from its launcher settings, else its debug log if the
// game started since the last update, else its revision log in the same case as "build <date>", else its Steam manifest
// as "build <id>". It returns "" if none of them says.
func (game *Game) LoadVersion(installPath string) string {
	settings := loadLauncher(installPath)
	if settings.RawVersion != "" {
		return settings.RawVersion
	}
	version, _, _ := strings.Cut(settings.Version, " (")
	if version = strings.TrimSpace(version); version != "" {
		return version
	}
	if user := game.FindUserDataDir(installPath); user != "" {
		if match := findCurrentMatch(installPath, files.Join(user, debugLogRel), gameVersionRe); match != "" {
			return match
		}
		if match := findCurrentMatch(installPath, files.Join(user, revisionLogRel), gameTimestampRe); match != "" {
			return "build " + match
		}
	}
	manifest, err := os.ReadFile(filepath.Join(installPath, "..", "..", "appmanifest_"+strconv.Itoa(game.SteamAppID)+".acf"))
	if match := buildIDRe.FindSubmatch(manifest); err == nil && match != nil {
		return "build " + string(match[1])
	}
	return ""
}

// findCurrentMatch returns the first group of the pattern's first match in a log of the game, if the game wrote the log
// since the install was last updated. It returns "" if the log is older, missing or unread, or the pattern matches
// nothing in it.
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

// loadLauncher reads an install's launcher settings. It returns the zero value if there are none.
func loadLauncher(installPath string) (settings launcherSettings) {
	if installPath == "" {
		return settings
	}
	if data, err := os.ReadFile(files.Join(installPath, launcherFile)); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	return settings
}

// ListInstallRoots returns the folders of an install that the game reads, in the order it mounts them, with each DLC's
// last. It returns an error if the DLC folder exists and cannot be read.
func ListInstallRoots(installPath string) ([]files.Root, error) {
	var roots []files.Root
	for _, folder := range InstallRoots {
		if dir := filepath.Join(installPath, folder); files.IsFolder(dir) {
			roots = append(roots, files.Root{Dir: dir, Origin: OriginVanilla})
		}
	}
	dlcFolder := filepath.Join(installPath, ScriptRoot, DLCFolder)
	dlcs, err := os.ReadDir(dlcFolder)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, dlc := range dlcs {
		if dlc.IsDir() {
			roots = append(roots, files.Root{Dir: filepath.Join(dlcFolder, dlc.Name()), Origin: OriginVanilla})
		}
	}
	return roots, nil
}

// FindDumpDirs returns the folders the game's console dumps land in.
func (game *Game) FindDumpDirs(installPath string) []string {
	user := game.FindUserDataDir(installPath)
	if user == "" {
		return nil
	}
	var dirs []string
	for _, dir := range DumpFolders {
		dirs = append(dirs, files.Join(user, dir))
	}
	return dirs
}

// FindUserDataDir returns the game's user-data folder, or the most likely one if none exists.
func (game *Game) FindUserDataDir(installPath string) string {
	candidates := game.listUserDataCandidates(installPath)
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

// listUserDataCandidates returns the folders a game may keep its user data in, most likely first.
func (game *Game) listUserDataCandidates(installPath string) []string {
	name := game.Name
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
		appID := strconv.Itoa(game.SteamAppID)
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

// expandGameDataPath returns the launcher's gameDataPath with %USER_DOCUMENTS% filled in, or "" if there is none.
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

// ResolveProgram returns the executable that starts a game from an install and its debug arguments, the row's flags
// last; the launcher settings win over the row. It returns false if the game has no executable on this system.
func (game *Game) ResolveProgram(installPath string) (path string, args []string, ok bool) {
	program := game.Program[runtime.GOOS]
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

// ReadInstallUpdatedAt returns when an install was last updated. A dump older than that is stale. It returns the zero
// time if that is unknown, which never counts as stale.
func ReadInstallUpdatedAt(installPath string) time.Time {
	if installPath == "" {
		return time.Time{}
	}
	// a folder's own time is no use: replacing a file in place leaves it unchanged
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

// LoadChecksum returns the hash an install's build writes beside its executable. It returns "" if the install has none.
func LoadChecksum(installPath string) string {
	data, err := os.ReadFile(filepath.Join(installPath, checksumRel))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
