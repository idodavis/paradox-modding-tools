// install.go finds where a game is on this machine: its installs through Steam's libraries, their version and age, the
// user-data and dump folders, and the mod descriptor file.

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
)

// DetectedInstall is a game install found on disk.
type DetectedInstall struct {
	GameID  string `json:"gameId"`
	Name    string `json:"name"`
	Path    string `json:"path"`    // the install folder
	Version string `json:"version"` // "" where none is read
}

// launcherSettings is the part of launcher-settings.json the app reads.
type launcherSettings struct {
	RawVersion   string `json:"rawVersion"`   // the version as the game writes it (1.12.5)
	Version      string `json:"version"`      // the version with its codename (1.13.11 (Chamomile))
	GameDataPath string `json:"gameDataPath"` // the user-data folder, %USER_DOCUMENTS% allowed
}

// The patterns that read Steam's library file, an app manifest and its build id.
var (
	vdfPathRe    = regexp.MustCompile(`"path"\s*"([^"]+)"`)
	installDirRe = regexp.MustCompile(`"installdir"\s*"([^"]+)"`)
	buildIDRe    = regexp.MustCompile(`"buildid"\s*"(\d+)"`)
)

// FindInstalls returns the installs of a game that Steam's libraries hold.
func FindInstalls(gameID string) []DetectedInstall {
	game := Lookup(gameID)
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
		if !dirExists(path) {
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

// listDefaultSteamRoots returns the platform's typical Steam install directories.
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

// LoadGameVersion returns an install's version: launcher-settings.json's rawVersion, else its version without the
// codename, else (EU5) "build <id>" from the Steam manifest two folders up; "" for none.
func LoadGameVersion(gameID, installPath string) string {
	settings := loadLauncher(installPath)
	if settings.RawVersion != "" {
		return settings.RawVersion
	}
	version, _, _ := strings.Cut(settings.Version, " (")
	if version = strings.TrimSpace(version); version != "" {
		return version
	}

	game := Lookup(gameID)
	if game == nil {
		return ""
	}
	manifest, err := os.ReadFile(filepath.Join(installPath, "..", "..", "appmanifest_"+strconv.Itoa(game.SteamAppID)+".acf"))
	if match := buildIDRe.FindSubmatch(manifest); err == nil && match != nil {
		return "build " + string(match[1])
	}
	return ""
}

// loadLauncher reads the install's launcher settings; empty where there are none.
func loadLauncher(installPath string) (settings launcherSettings) {
	if installPath == "" {
		return settings
	}
	if data, err := os.ReadFile(filepath.Join(installPath, filepath.FromSlash(launcherFile))); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	return settings
}

// FindDumpDirs returns the folders the game's console dumps land in; none where the game has no user-data folder.
func FindDumpDirs(gameID, installPath string) []string {
	game := Lookup(gameID)
	user := FindUserDataDir(gameID, installPath)
	if game == nil || user == "" {
		return nil
	}
	var dirs []string
	for _, dir := range game.DumpFolders {
		dirs = append(dirs, filepath.Join(user, filepath.FromSlash(dir)))
	}
	return dirs
}

// FindUserDataDir returns the game's user-data folder: the first candidate that exists, else the preferred one.
func FindUserDataDir(gameID, installPath string) string {
	candidates := listUserDataCandidates(gameID, installPath)
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

// listUserDataCandidates returns the folders a game may keep its user data in, most likely first: the launcher's
// setting, then the OS's conventions.
func listUserDataCandidates(gameID, installPath string) []string {
	game := Lookup(gameID)
	if game == nil {
		return nil
	}
	name := game.DocsFolderName
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

// expandGameDataPath returns the launcher's gameDataPath with %USER_DOCUMENTS% filled in; "" for none.
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

// BuildDescriptorPath returns the path of a game's descriptor file under a mod root.
func BuildDescriptorPath(gameID, root string) string {
	return filepath.Join(root, filepath.FromSlash(ResolveDescriptorFile(gameID)))
}

// ResolveDescriptorFile returns the descriptor's path from the mod root, with slashes.
func ResolveDescriptorFile(gameID string) string {
	if game := Lookup(gameID); game != nil && game.ModDescriptor == MetadataDescriptor {
		return MetadataDir + "/metadata.json"
	}
	return "descriptor.mod"
}

// ReadInstallUpdatedAt returns when an install was last updated: the newest file in its binaries, else its top files.
// A dump older than that is stale; zero means unknown, never stale.
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

// findNewestFile returns the newest mtime among the files directly in a folder; the folder's own mtime is no use,
// since replacing a file in place leaves it unchanged.
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

// dirExists reports whether a path is a folder.
func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}
