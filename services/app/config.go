// Package app is the app's own service and its files on this machine: PMT's folders, config.json with the
// catalog of settings, and the JSON and gob files.
package app

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
)

// Dirs says where PMT keeps its files on this machine.
type Dirs struct {
	App   string // config.json
	Cache string // the schema and index caches, the wiki's pages, the Steam snapshots
	Log   string // pmt.log
}

// ResolveDirs finds PMT's folders by the platform's conventions and creates any that are missing.
func ResolveDirs() (Dirs, error) {
	const folder = "paradox-modding-tools"
	dirs := Dirs{
		App:   filepath.Join(xdg.ConfigHome, folder),
		Cache: filepath.Join(xdg.CacheHome, folder),
		Log:   filepath.Join(xdg.StateHome, folder),
	}
	for _, dir := range []string{dirs.App, dirs.Cache, dirs.Log} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Dirs{}, err
		}
	}
	return dirs, nil
}

// LoadJSON reads a JSON file into a value. A missing file gives the zero value and no error.
func LoadJSON[T any](path string) (T, error) {
	var value T
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(content, &value); err != nil {
		return *new(T), err
	}
	return value, nil
}

// SaveJSON writes a value as indented JSON, atomically.
func SaveJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return files.WriteAtomic(path, append(content, '\n'))
}

// LoadGob reads a gob cache into a value. It returns false if the cache is missing or cannot be read.
func LoadGob[T any](path string) (T, bool) {
	var value T
	content, err := os.ReadFile(path)
	if err != nil {
		return value, false
	}
	if err := gob.NewDecoder(bytes.NewReader(content)).Decode(&value); err != nil {
		return *new(T), false
	}
	return value, true
}

// SaveGob writes a value as a gob cache, atomically.
func SaveGob(path string, value any) error {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(value); err != nil {
		return err
	}
	return files.WriteAtomic(path, buffer.Bytes())
}

// An Install is one game install PMT reads: its id, game and folder, and what the last scan found.
type Install struct {
	ID        string    `json:"id"`
	Game      string    `json:"game"`
	Path      string    `json:"path"`
	Version   string    `json:"version"`
	ScannedAt time.Time `json:"scannedAt"`
}

// An Era is one era of a mod's calendar: its name and the year it starts.
type Era struct {
	Name string `json:"name"`
	Year int    `json:"year"`
}

// A Calendar is how a mod counts years and names months.
type Calendar struct {
	Eras   []Era    `json:"eras"`
	Months []string `json:"months,omitempty"` // twelve names, or none for the game's
}

// A Mod is one mod of a workspace: its folder and what the user set about it.
type Mod struct {
	ID         string   `json:"id"`
	Dir        string   `json:"dir"`
	Color      string   `json:"color"` // a slot `o1`…`o16`, or a CSS color
	Calendar   Calendar `json:"calendar"`
	WorkshopID string   `json:"workshopId"` // "" unpublished
	Ignore     string   `json:"ignore"`     // the upload exclusions, one pattern per line
}

// A Workspace is one workspace: an install and the mods loaded with it, in load order.
type Workspace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Install  string `json:"install"`
	Mods     []Mod  `json:"mods"`
	Language string `json:"language"` // the localization language beside the game's base language
	HomePage string `json:"homePage"` // the page it opens on
}

// Config is the content of config.json: the installs, the workspaces and the settings' values by key.
type Config struct {
	Installs   []Install         `json:"installs"`
	Workspaces []Workspace       `json:"workspaces"`
	Settings   map[string]string `json:"settings"`
}

// A Setting is one row of the catalog: what a setting is, where it is shown and what it takes.
type Setting struct {
	Key     string   `json:"key"` // `<area>.<noun>`
	Label   string   `json:"label"`
	Help    string   `json:"help"`
	Section string   `json:"section"` // general, install, mods, appearance, ide
	Type    string   `json:"type"`    // switch, choice, number, text
	Default string   `json:"default"`
	Choices []string `json:"choices,omitempty"`
	Min     int      `json:"min,omitempty"` // a number's range; both 0 when it has none
	Max     int      `json:"max,omitempty"`
	Words   string   `json:"words"` // what the palette's search also matches
}

// Catalog lists every setting.
var Catalog = []Setting{
	{Key: "ide.reopenTabs", Label: "Reopen the IDE's tabs",
		Section: "general", Type: "switch", Default: "true",
		Help:  "Opens the tabs that were open when the workspace was last closed.",
		Words: "tabs restore session startup"},
	{Key: "ide.hideBinaries", Label: "Hide binaries in the explorer",
		Section: "general", Type: "switch", Default: "true",
		Help:  "Leaves images, fonts and other binary files out of the explorer's tree.",
		Words: "explorer images dds binary files"},
	{Key: "appearance.theme", Label: "Theme",
		Section: "appearance", Type: "choice", Default: "pmt",
		Choices: []string{"pmt", "catppuccin", "one", "pierre", "ck3", "eu5", "vic3"},
		Help:    "The family of colors the app is drawn in.",
		Words:   "colors palette skin"},
	{Key: "appearance.dark", Label: "Dark",
		Section: "appearance", Type: "switch", Default: "true",
		Help:  "Draws the theme in its dark colors rather than its light ones.",
		Words: "light mode night"},
	{Key: "appearance.followSystem", Label: "Follow the system's light and dark",
		Section: "appearance", Type: "switch", Default: "false",
		Help:  "Switches between light and dark as the system does.",
		Words: "os automatic"},
	{Key: "appearance.matchGame", Label: "Match the workspace's game theme",
		Section: "appearance", Type: "switch", Default: "false",
		Help:  "Draws the app in the theme of the open workspace's game.",
		Words: "ck3 eu5 vic3 game colors"},
	{Key: "appearance.scale", Label: "Interface scale",
		Section: "appearance", Type: "number", Default: "100", Min: 80, Max: 150,
		Help:  "The size of the whole interface, as a percentage.",
		Words: "zoom size percent"},
	{Key: "appearance.pages", Label: "Pages a workspace shows",
		Section: "appearance", Type: "text", Default: "workspace-ide,event-graph,health,patch-notes,release",
		Help:  "The pages the sidebar offers, in order, separated by commas.",
		Words: "sidebar navigation hide"},
	{Key: "ide.editorTextSize", Label: "Editor text size",
		Section: "ide", Type: "number", Default: "13", Min: 10, Max: 24,
		Help:  "The size of the editor's text in pixels.",
		Words: "font zoom"},
	{Key: "ide.panelTextSize", Label: "Explorer and panel text size",
		Section: "ide", Type: "number", Default: "12", Min: 10, Max: 20,
		Help:  "The size of the text in the explorer and the panels in pixels.",
		Words: "font sidebar"},
	{Key: "ide.indentation", Label: "Indentation where a file has none",
		Section: "ide", Type: "number", Default: "4", Min: 1, Max: 8,
		Help:  "The number of spaces an indent takes in a file whose own indentation cannot be told.",
		Words: "tab spaces indent"},
	{Key: "ide.checks", Label: "Checks turned off",
		Section: "ide", Type: "text", Default: "",
		Help:  "The ids of the checks the editor skips, separated by commas.",
		Words: "diagnostics problems lint ignore"},
	{Key: "ide.wikiToast", Label: "Wiki suggestion toast",
		Section: "ide", Type: "switch", Default: "true",
		Help:  "Shows a toast when the wiki has a page for what you are editing.",
		Words: "wiki notification popup"},
	{Key: "draw.resolution.ck3", Label: "Visual view resolution, CK3",
		Section: "ide", Type: "choice", Default: "1920x1080",
		Choices: []string{"1920x1080", "2560x1440", "3840x2160"},
		Help:    "The screen size the Visual view draws CK3's windows at.",
		Words:   "gui preview screen size"},
	{Key: "draw.resolution.vic3", Label: "Visual view resolution, Vic3",
		Section: "ide", Type: "choice", Default: "1920x1080",
		Choices: []string{"1920x1080", "2560x1440", "3840x2160"},
		Help:    "The screen size the Visual view draws Vic3's windows at.",
		Words:   "gui preview screen size"},
	{Key: "draw.resolution.eu5", Label: "Visual view resolution, EU5",
		Section: "ide", Type: "choice", Default: "1920x1080",
		Choices: []string{"1920x1080", "2560x1440", "3840x2160"},
		Help:    "The screen size the Visual view draws EU5's windows at.",
		Words:   "gui preview screen size"},
}

// A ConfigFile holds config.json for every service: one lock, one copy, one writer. A missing file means a first
// run. Any other failure to read it makes the config empty and read-only, so a passing fault never overwrites the
// real file.
type ConfigFile struct {
	Dirs     Dirs // the folders PMT keeps its files in, resolved once with the config
	path     string
	mu       sync.RWMutex
	config   Config
	readOnly bool
}

// openConfig loads config.json from the app folder.
func openConfig(dirs Dirs) (*ConfigFile, error) {
	file := &ConfigFile{Dirs: dirs, path: filepath.Join(dirs.App, "config.json")}
	config, err := LoadJSON[Config](file.path)
	// a config that cannot be read stays empty and is never written back, so the real file survives the fault
	file.config, file.readOnly = config, err != nil
	return file, nil
}

// Config returns a copy of the config as it stands.
func (file *ConfigFile) Config() Config {
	file.mu.RLock()
	defer file.mu.RUnlock()
	// the copy shares no slice or map with the file's own, so an Update never shows through it
	config := file.config
	config.Installs = slices.Clone(config.Installs)
	config.Workspaces = lo.Map(config.Workspaces, func(workspace Workspace, _ int) Workspace {
		workspace.Mods = lo.Map(workspace.Mods, func(mod Mod, _ int) Mod {
			mod.Calendar.Eras = slices.Clone(mod.Calendar.Eras)
			mod.Calendar.Months = slices.Clone(mod.Calendar.Months)
			return mod
		})
		return workspace
	})
	config.Settings = maps.Clone(config.Settings)
	return config
}

// Update applies a change to the config under the lock and saves it. It returns the write's error, or an error
// when the config is read-only.
func (file *ConfigFile) Update(change func(config *Config)) error {
	file.mu.Lock()
	defer file.mu.Unlock()
	if file.readOnly {
		return errors.New("config.json could not be read when the app started, so it is not written")
	}
	change(&file.config)
	return SaveJSON(file.path, file.config)
}

// LookupSetting returns one setting's value: the one the user set, else the catalog's default. It returns false for a
// key the catalog does not have.
func (file *ConfigFile) LookupSetting(key string) (string, bool) {
	setting, ok := lo.Find(Catalog, func(setting Setting) bool { return setting.Key == key })
	if !ok {
		return "", false
	}
	file.mu.RLock()
	defer file.mu.RUnlock()
	if value, ok := file.config.Settings[key]; ok {
		return value, true
	}
	return setting.Default, true
}

// LookupMod returns a mod's record in a workspace as the config holds it now. It returns false if the workspace is
// not recorded or does not have the mod.
func (file *ConfigFile) LookupMod(workspaceID, modID string) (Mod, bool) {
	workspace, _ := lo.Find(file.Config().Workspaces, func(workspace Workspace) bool { return workspace.ID == workspaceID })
	return lo.Find(workspace.Mods, func(mod Mod) bool { return mod.ID == modID })
}
