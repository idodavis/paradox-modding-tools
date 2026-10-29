// Package app is the service for the app itself, such as its updates, settings and windows. It also holds
// config.json, which every other service reads and writes.
package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
)

// Dirs is the set of folders PMT keeps its files in on this machine.
type Dirs struct {
	App   string // config.json
	Cache string // the caches, wiki pages and Steam snapshots
	Log   string // pmt.log
	Trash string // deleted files, emptied at each start
}

// ResolveDirs finds PMT's folders and creates any that are missing, but the trash. It returns an error if the system
// has no config or cache folder.
func ResolveDirs() (Dirs, error) {
	const folder = "paradox-modding-tools"
	config, err := os.UserConfigDir()
	if err != nil {
		return Dirs{}, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return Dirs{}, err
	}
	// the log is open while the app runs, so it sits beside the cache folder, which a reset empties
	dirs := Dirs{App: filepath.Join(config, folder), Log: filepath.Join(cache, folder)}
	dirs.Cache = filepath.Join(dirs.Log, "cache")
	dirs.Trash = filepath.Join(dirs.Cache, "trash")
	for _, dir := range []string{dirs.App, dirs.Cache, dirs.Log} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Dirs{}, err
		}
	}
	return dirs, nil
}

// LoadJSON reads a JSON file into a value. A missing file gives the zero value and no error. A file that is not valid
// JSON for the value is an error.
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

// SaveJSON writes a value to a file as indented JSON. The file is replaced whole, so a failed write leaves the old one.
func SaveJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return files.WriteAtomic(path, append(content, '\n'))
}

// An Install is one game install that PMT has recorded.
type Install struct {
	ID        string    `json:"id"`
	Game      string    `json:"game"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Version   string    `json:"version"`
	ScannedAt time.Time `json:"scannedAt"`
}

// An Era is one era of a mod's calendar.
type Era struct {
	Name string `json:"name"`
	Year int    `json:"year"`
}

// A Calendar is how a mod counts years and names months.
type Calendar struct {
	Eras   []Era    `json:"eras"`
	Months []string `json:"months,omitempty"` // twelve names, or none for the game's
}

// A Mod is one mod that a workspace records.
type Mod struct {
	ID         string   `json:"id"`
	Dir        string   `json:"dir"`
	Color      string   `json:"color"` // a slot `o1`…`o16`, or a CSS color
	Calendar   Calendar `json:"calendar"`
	WorkshopID string   `json:"workshopId"` // "" unpublished
	Ignore     string   `json:"ignore"`     // the upload exclusions, one pattern per line
	LeftOut    bool     `json:"leftOut"`    // left out of the workspace's compatibility checks
}

// A Workspace is one recorded workspace, a game install and the mods loaded with it.
type Workspace struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Install      string `json:"install"`
	Mods         []Mod  `json:"mods"`
	Language     string `json:"language"`     // the localization language beside the game's base language
	HomePage     string `json:"homePage"`     // the page it opens on
	GameColor    string `json:"gameColor"`    // the color of the game's files, as a slot or a CSS color; "" for its own
	ResetTabs    bool   `json:"resetTabs"`    // the IDE opens with no tabs rather than those left open
	ShowBinaries bool   `json:"showBinaries"` // the explorer lists images, fonts and other binary files
	// Settings holds the user's values of the per-workspace settings, by key.
	Settings map[string]string `json:"settings"`
	// IgnoredPairs holds the sorted pairs of mods whose conflicts the workspace ignores.
	IgnoredPairs [][2]string `json:"ignoredPairs"`
}

// An Agent is one coding agent PMT can start for a chat: one PMT offers, which the ACP registry describes, or one the
// user adds as a program of their own.
type Agent struct {
	ID       string   `json:"id"` // the registry's id of one PMT offers
	Name     string   `json:"name"`
	Program  string   `json:"program,omitempty"` // the program the user's agent runs; "" for one PMT offers
	Args     []string `json:"args,omitempty"`
	Register string   `json:"register,omitempty"` // how PMT's tools are registered with it for use outside PMT
	Files    []string `json:"files,omitempty"`    // its instruction files and folders, by their names in a mod
}

// Config is the content of config.json.
type Config struct {
	Installs   []Install         `json:"installs"`
	Workspaces []Workspace       `json:"workspaces"`
	Settings   map[string]string `json:"settings"`
	Agents     []Agent           `json:"agents"` // the agents the user adds
}

// A Setting is one row of the catalog of settings.
type Setting struct {
	Key       string   `json:"key"` // `<area>.<noun>`
	Label     string   `json:"label"`
	Help      string   `json:"help"`
	Section   string   `json:"section"`   // appearance, terminal, or an IDE area (editor, language, checks, …)
	Workspace bool     `json:"workspace"` // it is kept per workspace
	Type      string   `json:"type"`      // switch, choice, number, text
	Default   string   `json:"default"`
	Choices   []string `json:"choices,omitempty"`
	Min       int      `json:"min,omitempty"` // a number's range; both 0 when it has none
	Max       int      `json:"max,omitempty"`
	Words     string   `json:"words"` // what the palette's search also matches
}

// Catalog lists every setting.
var Catalog = []Setting{
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
		Section: "appearance", Type: "text", Default: "workspace-ide,event-graph,health,patch-notes,release,agents",
		Help:  "The pages the sidebar offers, in order, separated by commas.",
		Words: "sidebar navigation hide"},
	{Key: "terminal.shell", Label: "Default shell",
		Section: "terminal", Type: "text", Default: "",
		Help: "The shell New shell starts; the arrow beside it starts another. Found by PMT unless you pick one. On " +
			"Windows: pwsh on the PATH, then PowerShell 7, then Windows PowerShell. On Linux: your $SHELL, else sh.",
		Words: "terminal shell pwsh powershell bash zsh wsl program console default"},
	{Key: "agents.port", Label: "Port",
		Section: "agents", Type: "number", Default: "47820", Min: 1024, Max: 65535,
		Help: "Fixed, so an agent registers PMT once. PMT's tools listen on 127.0.0.1 only. A new port takes " +
			"effect when PMT starts again, and each agent is registered again.",
		Words: "mcp agents server port claude copilot codex"},
	{Key: "shortcuts.bindings", Label: "Keyboard shortcuts",
		Section: "shortcuts", Type: "text", Default: "",
		Help:  "The shortcuts changed from PMT's own, by command.",
		Words: "keys keybindings hotkeys rebind"},
	{Key: "ide.editorTextSize", Label: "Editor text size",
		Section: "editor", Workspace: true, Type: "number", Default: "14", Min: 10, Max: 24,
		Help:  "The size of the editor's text in pixels.",
		Words: "font zoom"},
	{Key: "ide.panelTextSize", Label: "Explorer and panel text size",
		Section: "explorer", Workspace: true, Type: "number", Default: "13", Min: 10, Max: 20,
		Help:  "The size of the text in the explorer and the panels in pixels.",
		Words: "font sidebar"},
	{Key: "ide.font", Label: "Editor font",
		Section: "editor", Workspace: true, Type: "text", Default: "Geist Mono",
		Help:  "Any font installed on this computer, by name.",
		Words: "typeface monospace"},
	{Key: "ide.lineHeight", Label: "Line height",
		Section: "editor", Workspace: true, Type: "choice", Default: "compact", Choices: []string{"compact", "roomy"},
		Help:  "The room between the editor's lines.",
		Words: "spacing lines"},
	{Key: "ide.density", Label: "Row density",
		Section: "explorer", Workspace: true, Type: "choice", Default: "default", Choices: []string{"compact", "default", "relaxed"},
		Help:  "Compact fits more rows; relaxed is easier to hit.",
		Words: "explorer tree rows spacing"},
	{Key: "ide.indentWith", Label: "Indent with",
		Section: "editor", Workspace: true, Type: "choice", Default: "tabs", Choices: []string{"tabs", "spaces"},
		Help:  "What a new line is indented with where a file has no indentation of its own.",
		Words: "tab spaces indent"},
	{Key: "ide.indentation", Label: "Indent size",
		Section: "editor", Workspace: true, Type: "number", Default: "4", Min: 1, Max: 8,
		Help:  "The spaces an indent takes, and the width of a tab.",
		Words: "tab spaces indent"},
	{Key: "ide.checks", Label: "Checks",
		Section: "checks", Workspace: true, Type: "text", Default: "",
		Help:  "The checks the editor runs, kept as the ids of those turned off. Health's counts do not change.",
		Words: "diagnostics problems lint ignore"},
	{Key: "ide.wordWrap", Label: "Word wrap",
		Section: "editor", Workspace: true, Type: "switch", Default: "false",
		Help:  "Long lines wrap at the editor's edge.",
		Words: "lines wrapping"},
	{Key: "ide.closeBrackets", Label: "Close brackets and quotes",
		Section: "editor", Workspace: true, Type: "switch", Default: "true",
		Help:  "Typing { adds the }.",
		Words: "braces pairs auto"},
	{Key: "ide.minimap", Label: "Minimap",
		Section: "editor", Workspace: true, Type: "switch", Default: "true",
		Help:  "The whole file in small at the editor's right edge.",
		Words: "overview scroll"},
	{Key: "ide.indentGuides", Label: "Indent guides",
		Section: "editor", Workspace: true, Type: "switch", Default: "true",
		Help:  "A line down each block's indentation.",
		Words: "indentation lines"},
	{Key: "ide.bracketColors", Label: "Bracket pair colors",
		Section: "editor", Workspace: true, Type: "switch", Default: "true",
		Help:  "Each depth of { } in its own color.",
		Words: "braces rainbow"},
	{Key: "ide.whitespace", Label: "Show spaces and tabs",
		Section: "editor", Workspace: true, Type: "switch", Default: "false",
		Help:  "Off: only in the selection.",
		Words: "whitespace invisible characters"},
	{Key: "ide.folding", Label: "Folding",
		Section: "editor", Workspace: true, Type: "switch", Default: "true",
		Help:  "A block's arrow in the gutter hides its lines.",
		Words: "collapse fold gutter"},
	{Key: "ide.hover", Label: "Hover cards",
		Section: "language", Workspace: true, Type: "switch", Default: "true",
		Help:  "What a word is, when the pointer rests on it.",
		Words: "tooltip documentation"},
	{Key: "ide.complete", Label: "Completion list while typing",
		Section: "language", Workspace: true, Type: "switch", Default: "true",
		Help:  "Off: only Ctrl Space opens it.",
		Words: "autocomplete suggestions intellisense"},
	{Key: "ide.predict", Label: "Predicted text",
		Section: "language", Workspace: true, Type: "switch", Default: "true",
		Help:  "The likeliest completion in grey; Tab takes it.",
		Words: "inline ghost suggestion"},
	{Key: "ide.signature", Label: "Signature help",
		Section: "language", Workspace: true, Type: "switch", Default: "true",
		Help:  "What an effect or macro takes, while you fill it in.",
		Words: "parameters arguments"},
	{Key: "ide.stickyFolders", Label: "Sticky folders",
		Section: "explorer", Workspace: true, Type: "switch", Default: "true",
		Help:  "A folder's name stays on top while you scroll its files.",
		Words: "explorer scroll"},
	{Key: "ide.gameFiles", Label: "Game files section",
		Section: "explorer", Workspace: true, Type: "switch", Default: "true",
		Help:  "The game's files, read only, under the mods.",
		Words: "explorer vanilla"},
	{Key: "ide.rootColors", Label: "Color each top folder",
		Section: "explorer", Workspace: true, Type: "switch", Default: "true",
		Help:  "A mod's or the game's folder in its color, as well as its bar.",
		Words: "explorer mod colors"},
	{Key: "ide.followFile", Label: "Follow the open file",
		Section: "explorer", Workspace: true, Type: "switch", Default: "true",
		Help:  "The explorer selects the file you switch to.",
		Words: "explorer reveal select"},
	{Key: "ide.wikiFollow", Label: "Wiki follows the open file",
		Section: "wiki", Workspace: true, Type: "switch", Default: "true",
		Help:  "Suggests pages for the file you are editing.",
		Words: "wiki pages suggestions"},
	{Key: "draw.rows", Label: "Rows per list",
		Section: "visual", Workspace: true, Type: "number", Default: "3", Min: 1, Max: 20,
		Help:  "How many times the Visual view draws the item of a data model.",
		Words: "gui preview list repeat items"},
	// The default is the size each game designs for. CK3 and Vic3 scale their 1920 by 1080 main menu art to the
	// screen (frontend_main.gui). EU5's files declare no size.
	{Key: "draw.resolution", Label: "Resolution",
		Section: "visual", Workspace: true, Type: "choice", Default: "1920x1080",
		Choices: []string{"1920x1080", "2560x1080", "1920x1200"},
		Help:    "The game's interface size: the screen divided by the GUI scale. 2560x1080 is ultrawide, 1920x1200 is 16:10.",
		Words:   "gui preview screen size ultrawide aspect"},
}

// A ConfigFile is config.json held in memory for every service. A missing file means a first run. A file that cannot be
// read gives an empty, read-only config, so the real file is never overwritten.
type ConfigFile struct {
	Dirs     Dirs // PMT's folders
	path     string
	mu       sync.RWMutex
	config   Config
	readOnly bool
}

// openConfig loads config.json from the app folder. A file that cannot be read gives an empty, read-only config.
func openConfig(dirs Dirs) *ConfigFile {
	file := &ConfigFile{Dirs: dirs, path: filepath.Join(dirs.App, "config.json")}
	config, err := LoadJSON[Config](file.path)
	file.config, file.readOnly = config, err != nil
	return file
}

// Config returns a copy of the config. The copy shares no slice or map with the file's own.
func (file *ConfigFile) Config() Config {
	file.mu.RLock()
	defer file.mu.RUnlock()
	config := file.config
	config.Installs = slices.Clone(config.Installs)
	config.Workspaces = lo.Map(config.Workspaces, func(workspace Workspace, _ int) Workspace {
		workspace.Mods = lo.Map(workspace.Mods, func(mod Mod, _ int) Mod {
			mod.Calendar.Eras = slices.Clone(mod.Calendar.Eras)
			mod.Calendar.Months = slices.Clone(mod.Calendar.Months)
			return mod
		})
		workspace.IgnoredPairs = slices.Clone(workspace.IgnoredPairs)
		return workspace
	})
	config.Settings = maps.Clone(config.Settings)
	config.Agents = lo.Map(config.Agents, func(agent Agent, _ int) Agent {
		agent.Args = slices.Clone(agent.Args)
		return agent
	})
	return config
}

// Update changes the config, saves it and emits config:changed with a section, or "" for all. If the change fails,
// nothing is saved and its error is returned. It also returns an error if the config is read-only or cannot be saved.
func (file *ConfigFile) Update(section string, change func(config *Config) error) error {
	file.mu.Lock()
	defer file.mu.Unlock()
	if file.readOnly {
		return errors.New("config.json could not be read when the app started, so it is not written")
	}
	if err := change(&file.config); err != nil {
		return err
	}
	if err := SaveJSON(file.path, file.config); err != nil {
		return err
	}
	application.Get().Event.Emit(EventConfigChanged, section)
	return nil
}

// LookupSetting returns a setting's value in a workspace, or the catalog's default if it was never set. It returns
// false if the catalog does not have the setting.
func (file *ConfigFile) LookupSetting(workspaceID, key string) (string, bool) {
	setting, ok := lo.Find(Catalog, func(setting Setting) bool { return setting.Key == key })
	if !ok {
		return "", false
	}
	file.mu.RLock()
	defer file.mu.RUnlock()
	values := file.config.Settings
	if setting.Workspace {
		record, _ := lo.Find(file.config.Workspaces, func(record Workspace) bool { return record.ID == workspaceID })
		values = record.Settings
	}
	if value, ok := values[key]; ok {
		return value, true
	}
	return setting.Default, true
}
