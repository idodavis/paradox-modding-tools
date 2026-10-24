// install.go finds where a game is on this machine: its installs through Steam's libraries, their version and
// age, the user-data and dump folders, the mod descriptor file, and the program that starts the game.

package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

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

// The patterns that read Steam's library file, an app manifest and its build id, and the game's release branch in
// its revision log (release/1.20.1, u26q2/release/1.3.11).
var (
	vdfPathRe    = regexp.MustCompile(`"path"\s*"([^"]+)"`)
	installDirRe = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)
	buildIDRe    = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
	gameBranchRe = regexp.MustCompile(`\]: game_branch: (?:\S+/)?release/(\S+)`)
)

// FindInstalls returns the installs of a game that Steam's libraries hold. It returns nil for an unknown game.
func FindInstalls(gameID string) []DetectedInstall {
	game := LookupGame(gameID)
	if game == nil {
		return nil
	}
	var out []DetectedInstall
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
		out = append(out, DetectedInstall{
			Name:    game.Name,
			Path:    path,
			Version: LoadGameVersion(game.ID, path),
		})
	}
	return out
}

// findSteamLibraries returns the Steam library roots that libraryfolders.vdf names in each default Steam folder.
func findSteamLibraries() []string {
	seen := map[string]bool{}
	var libraries []string
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

// LoadGameVersion returns an install's version. The launcher settings come first, then the game's revision log, then
// the Steam manifest, which gives "build <id>". The revision log counts only when the game started after the
// install's last update. It returns "" if none of them says.
func LoadGameVersion(gameID, installPath string) string {
	settings := loadLauncher(installPath)
	if settings.RawVersion != "" {
		return settings.RawVersion
	}
	version, _, _ := strings.Cut(settings.Version, " (")
	if version = strings.TrimSpace(version); version != "" {
		return version
	}
	if version := readLoggedVersion(gameID, installPath); version != "" {
		return version
	}

	game := LookupGame(gameID)
	if game == nil {
		return ""
	}
	manifest, err := os.ReadFile(filepath.Join(installPath, "..", "..", "appmanifest_"+strconv.Itoa(game.SteamAppID)+".acf"))
	if match := buildIDRe.FindSubmatch(manifest); err == nil && match != nil {
		return "build " + string(match[1])
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

// readLoggedVersion returns the version a game wrote in its revision log at its last start: the part of its release
// branch after release/. It returns "" when there is no log, when the log names no release, or when the install was
// updated after the log was written, since the game has not run as the new version yet.
func readLoggedVersion(gameID, installPath string) string {
	user := FindUserDataDir(gameID, installPath)
	if user == "" {
		return ""
	}
	path := files.Join(user, revisionLogRel)
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Before(ReadInstallUpdatedAt(installPath)) {
		return ""
	}
	data, err := os.ReadFile(path)
	if match := gameBranchRe.FindSubmatch(data); err == nil && match != nil {
		return string(match[1])
	}
	return ""
}

// ListInstallRoots returns the folders of an install that the game reads, in the order it mounts them: the install's
// own folders that exist, then the folder of each DLC. It returns an error if the DLC folder exists and cannot be
// read.
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

// FindDumpDirs returns the folders the game's console dumps land in. It returns nil for an unknown game.
func FindDumpDirs(gameID, installPath string) []string {
	user := FindUserDataDir(gameID, installPath)
	if user == "" {
		return nil
	}
	var dirs []string
	for _, dir := range DumpFolders {
		dirs = append(dirs, files.Join(user, dir))
	}
	return dirs
}

// FindUserDataDir returns the game's user-data folder: the first candidate that exists, or else the most likely
// one. It returns "" for an unknown game.
func FindUserDataDir(gameID, installPath string) string {
	candidates := listUserDataCandidates(gameID, installPath)
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

// listUserDataCandidates returns the folders a game may keep its user data in, most likely first: the launcher's
// setting, then the OS's conventions.
func listUserDataCandidates(gameID, installPath string) []string {
	game := LookupGame(gameID)
	if game == nil {
		return nil
	}
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

// ResolveProgram returns the executable that starts a game on this machine and the arguments to start it in debug
// mode, from an install's folder. Where the game's row names the launcher settings, their executable and arguments
// win over the row's; the row's debug flags always come last. It returns false when the game has no executable on
// this operating system.
func ResolveProgram(gameID, installPath string) (path string, args []string, ok bool) {
	game := LookupGame(gameID)
	if game == nil {
		return "", nil, false
	}
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

// ReadInstallUpdatedAt returns when an install was last updated: the newest file in its binaries folder, else among
// its top-level files. A dump older than that is stale. A zero time means unknown, which never counts as stale.
func ReadInstallUpdatedAt(installPath string) time.Time {
	if installPath == "" {
		return time.Time{}
	}
	for _, dir := range []string{filepath.Join(installPath, "binaries"), installPath} {
		if newest := findNewestFile(dir); !newest.IsZero() {
			return newest
		}
	}
	return time.Time{}
}

// findNewestFile returns the newest modification time among the files directly in a folder. The folder's own
// time is no use, because replacing a file in place leaves it unchanged.
func findNewestFile(dir string) time.Time {
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
