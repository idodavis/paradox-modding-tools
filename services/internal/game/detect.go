// detect.go finds game installs (via Steam library folders) and reads the
// installed version from launcher-settings.json. It avoids the Windows registry
// so it cross-compiles; it scans well-known Steam locations.

package game

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
)

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
	if err := sonic.Unmarshal(data, &ls); err != nil {
		return ""
	}
	if ls.RawVersion != "" {
		return ls.RawVersion
	}
	v := ls.Version
	if i := strings.Index(v, " ("); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// UserDataDir is the OS user-data folder for gameID (script_docs, default mod/).
// First existing candidate wins; if none exist, the preferred conventional path.
func UserDataDir(gameID, installPath string) string {
	cands := userDataCandidates(gameID, installPath)
	for _, p := range cands {
		if dirExists(p) {
			return p
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
	add := func(p string) {
		if p != "" {
			cands = append(cands, filepath.Clean(p))
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
	if sonic.Unmarshal(data, &ls) != nil {
		return ""
	}
	return expandGameDataPath(ls.GameDataPath)
}

func expandGameDataPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.Contains(p, "%USER_DOCUMENTS%") {
		home, err := os.UserHomeDir()
		docs := ""
		if err == nil {
			docs = filepath.Join(home, "Documents")
		}
		p = strings.ReplaceAll(p, "%USER_DOCUMENTS%", docs)
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// DescriptorPath is the expected descriptor file for gameID under a mod root.
func DescriptorPath(gameID, root string) string {
	return filepath.Join(root, filepath.FromSlash(DescriptorFile(gameID)))
}

// DescriptorFile is the mod-root descriptor's path relative to the mod, in
// slash form. One statement of the fact: DescriptorPath builds the absolute
// path from it and the LSP names it in the missing-descriptor message, which
// until G1 re-derived it with the last game-id branch outside this package.
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
		m := installDirRe.FindSubmatch(data)
		if m == nil {
			continue
		}
		path := filepath.Join(lib, "steamapps", "common", string(m[1]))
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
	add := func(p string) {
		if p == "" {
			return
		}
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		if seen[p] {
			return
		}
		seen[p] = true
		libs = append(libs, p)
	}
	for _, root := range defaultSteamRoots() {
		add(root)
		vdf := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		data, err := os.ReadFile(vdf)
		if err != nil {
			continue
		}
		for _, m := range vdfPathRe.FindAllSubmatch(data, -1) {
			add(filepath.FromSlash(strings.ReplaceAll(string(m[1]), `\\`, `/`)))
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
// The binaries are the signal, for three reasons:
//
//   - `launcher/launcher-settings.json` is not usable. EU5 ships no launcher
//     directory at all, and on CK3 the file was 11 days older than the actual
//     update, so it is wrong even where it exists.
//   - Steam's appmanifest LastUpdated is exact but only for Steam copies, and
//     PMT supports installs it did not get from Steam.
//   - Executable mtimes tracked Steam's own LastUpdated to within seconds on
//     all three games, and need nothing outside the install folder.
//
// "Last changed" is deliberately not "highest version", which matters for the
// common downgrade route: picking an older build in Steam's game-version (beta
// branch) settings. Steam stamps files as it writes them rather than carrying
// the build's own timestamps — measured on all three games, the executable
// mtime lands 2–7 seconds *before* the appmanifest's LastUpdated — so switching
// branches rewrites the binaries with a fresh mtime. Dumps generated against the
// other build are then correctly reported as no longer describing this install,
// which comparing version strings would miss entirely, and which PMT could not
// do for EU5 at all since it publishes no version anywhere.
//
// Copying an install preserves mtimes, which can only make this quieter, never
// falsely loud.
//
// Zero time means unknown; callers must treat that as "cannot tell", never stale.
func InstallUpdatedAt(installPath string) time.Time {
	if installPath == "" {
		return time.Time{}
	}
	for _, dir := range []string{filepath.Join(installPath, "binaries"), installPath} {
		if t := newestFileIn(dir); !t.IsZero() {
			return t
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
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

// MetadataDir and MetadataFile name the metadata descriptor's location. They
// are constants rather than registry fields because the layout is a Paradox
// convention rather than a per-title fact — what varies is WHICH descriptor a
// game uses, and GameInfo.Descriptor already holds that. Before G2 the two
// strings were spelled out in seven places across five packages.
const (
	DescriptorModFile = "descriptor.mod"
	MetadataDir       = ".metadata"
	MetadataFile      = "metadata.json"
)
