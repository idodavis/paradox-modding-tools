// config.go holds PMT's folders, the config.json records and ConfigFile.

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
)

// Dirs is the set of folders PMT keeps its files in.
type Dirs struct {
	App   string
	Cache string
	Log   string

	// Trash holds deleted files until the next start empties it.
	Trash string
}

// Install is one game install PMT has recorded.
type Install struct {
	ID        string    `json:"id"`
	Game      string    `json:"game"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Version   string    `json:"version"`
	ScannedAt time.Time `json:"scannedAt"`
}

// Era is one era of a mod's calendar.
type Era struct {
	Name string `json:"name"`
	Year int    `json:"year"`
}

// Calendar is how a mod counts years and names months.
type Calendar struct {
	Eras []Era `json:"eras"`

	// Months are twelve names, or none for the game's.
	Months []string `json:"months,omitempty"`
}

// Mod is one mod that a workspace records.
type Mod struct {
	ID  string `json:"id"`
	Dir string `json:"dir"`

	// Color is a slot `o1` to `o16`, or a CSS color.
	Color string `json:"color"`

	Calendar Calendar `json:"calendar"`

	// WorkshopID is "" for an unpublished mod.
	WorkshopID string `json:"workshopId"`

	// Ignore is the upload exclusions, one pattern per line.
	Ignore string `json:"ignore"`

	// LeftOut keeps the mod out of the compatibility checks.
	LeftOut bool `json:"leftOut"`

	// External is someone else's mod, which the workspace checks its own mods
	// against.
	External bool `json:"external"`
}

// Workspace is one recorded workspace: a game install and its mods.
type Workspace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Install  string `json:"install"`
	Mods     []Mod  `json:"mods"`
	Language string `json:"language"`
	HomePage string `json:"homePage"`

	// GameColor is a slot or a CSS color, or "" for the game's own.
	GameColor string `json:"gameColor"`

	ResetTabs     bool              `json:"resetTabs"`
	HideChecklist bool              `json:"hideChecklist"`
	HideStartPage bool              `json:"hideStartPage"`
	Settings      map[string]string `json:"settings"`

	// IgnoredPairs are sorted pairs of mods whose conflicts are ignored.
	IgnoredPairs [][2]string `json:"ignoredPairs"`
}

// Agent is one coding agent PMT can start for a chat.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Program is the user's own agent, "" for one PMT offers.
	Program string   `json:"program,omitempty"`
	Args    []string `json:"args,omitempty"`

	// Register is how PMT's tools are registered with the agent outside PMT.
	Register string `json:"register,omitempty"`

	// Files are the agent's instruction files and folders, by name in a mod.
	Files []string `json:"files,omitempty"`
}

// Window is how the main window was left.
type Window struct {
	// Width and Height are in device-independent pixels, 0 before the first
	// close.
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
}

// Config is the content of config.json.
type Config struct {
	Installs   []Install         `json:"installs"`
	Workspaces []Workspace       `json:"workspaces"`
	Settings   map[string]string `json:"settings"`

	Agents []Agent `json:"agents"`
	Window Window  `json:"window"`

	// Skipped is the release the user went back from.
	Skipped string `json:"skipped"`
}

// Setting is one row of the catalog of settings.
type Setting struct {
	// Key is `<area>.<noun>`.
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help"`

	// Section is appearance, terminal, or an IDE area such as editor or checks.
	Section string `json:"section"`

	// Workspace is true for a setting kept per workspace.
	Workspace bool `json:"workspace"`

	// Type is switch, choice, number, port (a number written as its digits) or
	// text.
	Type    string   `json:"type"`
	Default string   `json:"default"`
	Choices []string `json:"choices,omitempty"`

	// Min and Max are a number's range, both 0 for none.
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`

	// Words are what the palette's search also matches.
	Words string `json:"words"`
}

// ConfigFile is config.json held in memory for every service. An unreadable
// file gives an empty, read-only config.
type ConfigFile struct {
	Dirs     Dirs
	path     string
	mu       sync.RWMutex
	config   Config
	revision int

	// fault is why config.json could not be read, or nil if it was.
	fault error
}

// Catalog lists every setting.
var Catalog = []Setting{
	{Key: "appearance.simple", Label: "Simple interface",
		Section: "appearance", Type: "switch", Default: "false",
		Help: "Hides Source control, Entries, Git output, Terminal, the minimap, predicted text and signature help, and " +
			"keeps hover cards short. Off shows everything.",
		Words: "beginner novice minimal clutter hide advanced everything"},
	{Key: "appearance.theme", Label: "Theme",
		Section: "appearance", Type: "choice", Default: "pmt",
		Choices: []string{"pmt", "catppuccin", "one", "pierre", "jetbrains", "ck3", "eu5", "vic3"},
		Help:    "Each theme comes dark and light and colors every page and the editor.",
		Words:   "colors palette skin"},
	{Key: "appearance.dark", Label: "Dark",
		Section: "appearance", Type: "switch", Default: "true",
		Help:  "Draws the theme in its dark colors rather than its light ones.",
		Words: "light mode night"},
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
		Help:  "The pages the top bar shows, in order.",
		Words: "sidebar navigation hide"},
	{Key: "guidance.newModders", Label: "Guidance for new modders",
		Section: "guidance", Type: "switch", Default: "true",
		Help:  "Shows How a mod works on Make a new mod and the first-mod checklist.",
		Words: "tutorial onboarding beginner learn checklist"},
	{Key: "terminal.shell", Label: "Default shell",
		Section: "terminal", Type: "text", Default: "",
		Help: "The shell New shell starts; the arrow beside it starts another. Found by PMT unless you pick one. On " +
			"Windows: pwsh on the PATH, then PowerShell 7, then Windows PowerShell. On Linux: your $SHELL, else sh.",
		Words: "terminal shell pwsh powershell bash zsh wsl program console default"},
	{Key: "agents.port", Label: "Port",
		Section: "agents", Type: "port", Default: "47820", Min: 1024, Max: 65535,
		Help: "Fixed, so an agent registers PMT once. PMT's tools listen on 127.0.0.1 only. A new port takes " +
			"effect when PMT starts again, and each agent is registered again.",
		Words: "mcp agents server port claude copilot codex"},
	{Key: "agents.allowed", Label: "Allowed without asking",
		Section: "agents", Type: "text", Default: "",
		Help: "The tools an agent may use without asking, after you chose Always allow for them. Each is the agent and " +
			"the tool, as the agent names it.",
		Words: "permissions always allow tools agents approve prompt"},
	{Key: "shortcuts.bindings", Label: "Keyboard shortcuts",
		Section: "shortcuts", Type: "text", Default: "",
		Help:  "The shortcuts changed from PMT's own, by command.",
		Words: "keys keybindings hotkeys rebind"},
	{Key: "ide.editorTextSize", Label: "Editor text size",
		Section: "editor", Type: "number", Default: "14", Min: 10, Max: 24,
		Help:  "The size of the editor's text in pixels.",
		Words: "font zoom"},
	{Key: "ide.panelTextSize", Label: "Explorer and panel text size",
		Section: "explorer", Type: "number", Default: "13", Min: 10, Max: 20,
		Help:  "The size of the text in the explorer and the panels in pixels.",
		Words: "font sidebar"},
	{Key: "ide.font", Label: "Editor font",
		Section: "editor", Type: "text", Default: "Geist Mono",
		Help:  "Any font installed on this computer, by name.",
		Words: "typeface monospace"},
	{Key: "ide.lineHeight", Label: "Line height",
		Section: "editor", Type: "choice", Default: "compact", Choices: []string{"compact", "roomy"},
		Help:  "The room between the editor's lines.",
		Words: "spacing lines"},
	{Key: "ide.density", Label: "Row density",
		Section: "explorer", Type: "choice", Default: "default", Choices: []string{"compact", "default", "relaxed"},
		Help:  "Compact fits more rows; relaxed is easier to hit.",
		Words: "explorer tree rows spacing"},
	{Key: "ide.indentWith", Label: "Indent with",
		Section: "editor", Type: "choice", Default: "tabs", Choices: []string{"tabs", "spaces"},
		Help:  "What a new line is indented with where a file has no indentation of its own.",
		Words: "tab spaces indent"},
	{Key: "ide.indentation", Label: "Indent size",
		Section: "editor", Type: "number", Default: "4", Min: 1, Max: 8,
		Help:  "The spaces an indent takes, and the width of a tab.",
		Words: "tab spaces indent"},
	{Key: "ide.checks", Label: "Health checks",
		Section: "checks", Workspace: true, Type: "text", Default: "",
		Help:  "The checks the editor shows. Turning one off hides it in the editor; Health still counts it.",
		Words: "diagnostics problems lint ignore"},
	{Key: "ide.wordWrap", Label: "Word wrap",
		Section: "editor", Type: "switch", Default: "false",
		Help:  "Long lines wrap at the editor's edge.",
		Words: "lines wrapping"},
	{Key: "ide.closeBrackets", Label: "Close brackets and quotes",
		Section: "editor", Type: "switch", Default: "true",
		Help:  "Typing { adds the }.",
		Words: "braces pairs auto"},
	{Key: "ide.minimap", Label: "Minimap",
		Section: "editor", Type: "switch", Default: "true",
		Help:  "The whole file in small at the editor's right edge.",
		Words: "overview scroll"},
	{Key: "ide.indentGuides", Label: "Indent guides",
		Section: "editor", Type: "switch", Default: "true",
		Help:  "A line down each block's indentation.",
		Words: "indentation lines"},
	{Key: "ide.bracketColors", Label: "Bracket pair colors",
		Section: "editor", Type: "switch", Default: "true",
		Help:  "Each depth of { } in its own color.",
		Words: "braces rainbow"},
	{Key: "ide.whitespace", Label: "Show spaces and tabs",
		Section: "editor", Type: "switch", Default: "false",
		Help:  "Draws every space and tab. Off draws only those in the selection.",
		Words: "whitespace invisible characters"},
	{Key: "ide.folding", Label: "Folding",
		Section: "editor", Type: "switch", Default: "true",
		Help:  "A block's arrow in the gutter hides its lines.",
		Words: "collapse fold gutter"},
	{Key: "ide.hover", Label: "Hover cards",
		Section: "language", Type: "switch", Default: "true",
		Help:  "Shows a card on the word under the pointer: what it is, where it is defined and its problems.",
		Words: "tooltip documentation"},
	{Key: "ide.complete", Label: "Completion list while typing",
		Section: "language", Type: "switch", Default: "true",
		Help:  "Shows the list of suggestions as you type. Off, only Ctrl Space opens it.",
		Words: "autocomplete suggestions intellisense"},
	{Key: "ide.predict", Label: "Predicted text",
		Section: "language", Type: "switch", Default: "true",
		Help:  "Shows the likeliest suggestion in grey ahead of the cursor. Tab takes it.",
		Words: "inline ghost suggestion"},
	{Key: "ide.signature", Label: "Signature help",
		Section: "language", Type: "switch", Default: "true",
		Help:  "Shows what an effect or scripted macro takes while you fill in its arguments.",
		Words: "parameters arguments"},
	{Key: "ide.textHints", Label: "Text after localization keys",
		Section: "language", Type: "switch", Default: "true",
		Help: "Shows a localization key's text in grey after the key, in the workspace's language. A key no language " +
			"has text for shows no text yet.",
		Words: "localization loc yml inline hint preview"},
	{Key: "ide.completionPictures", Label: "Pictures in the completion list",
		Section: "language", Type: "switch", Default: "true",
		Help:  "Shows the picture of each image file beside the suggestions while you type a path.",
		Words: "image texture dds icon preview thumbnail"},
	{Key: "ide.stickyFolders", Label: "Sticky folders",
		Section: "explorer", Type: "switch", Default: "true",
		Help:  "A folder's name stays on top while you scroll its files.",
		Words: "explorer scroll"},
	{Key: "ide.gameFiles", Label: "Game files section",
		Section: "explorer", Type: "switch", Default: "true",
		Help:  "The game's files, read only, under the mods.",
		Words: "explorer vanilla"},
	{Key: "ide.hideBinaries", Label: "Hide binaries",
		Section: "explorer", Type: "switch", Default: "true",
		Help:  "Leaves images, fonts and other files that are not text out of the explorer.",
		Words: "explorer images fonts dds files"},
	{Key: "ide.hideDotFolders", Label: "Hide dot folders",
		Section: "explorer", Type: "switch", Default: "false",
		Help:  "Leaves folders whose name starts with a dot, such as .workshop, out of the explorer. .git is always left out.",
		Words: "explorer hidden dot folders workshop github"},
	{Key: "ide.rootColors", Label: "Color each top folder",
		Section: "explorer", Type: "switch", Default: "true",
		Help:  "A mod's or the game's folder in its color, as well as its bar.",
		Words: "explorer mod colors"},
	{Key: "ide.followFile", Label: "Follow the open file",
		Section: "explorer", Type: "switch", Default: "true",
		Help:  "The explorer selects the file you switch to.",
		Words: "explorer reveal select"},
	{Key: "ide.wikiFollow", Label: "Wiki follows the open file",
		Section: "wiki", Type: "switch", Default: "true",
		Help:  "Suggests pages for the file you are editing.",
		Words: "wiki pages suggestions"},
	{Key: "draw.rows", Label: "Rows per list",
		Section: "visual", Type: "number", Default: "3", Min: 1, Max: 20,
		Help:  "How many times the Visual view draws the item of a data model.",
		Words: "gui preview list repeat items"},
	// The default is the size each game designs for: CK3 and Vic3 scale their
	// 1920 by 1080 menu art to the screen. EU5's files declare no size.
	{Key: "draw.resolution", Label: "Resolution",
		Section: "visual", Type: "choice", Default: "1920x1080",
		Choices: []string{"1920x1080", "2560x1080", "1920x1200"},
		Help:    "The game's interface size: the screen divided by the GUI scale. 2560x1080 is ultrawide, 1920x1200 is 16:10.",
		Words:   "gui preview screen size ultrawide aspect"},
	{Key: "draw.openVisual", Label: "Open drawable files in Visual",
		Section: "visual", Type: "switch", Default: "false",
		Help:  "A GUI or coat of arms file you open shows drawn, not as code.",
		Words: "gui preview mode code coat"},
}

// ResolveDirs finds PMT's folders and creates those that are missing, the trash
// aside.
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

	// The log is open while the app runs, so it stays out of the cache a reset
	// empties.
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

// LoadJSON reads a JSON file into a value. A missing file gives the zero value
// and no error.
func LoadJSON[T any](path string) (T, error) {
	var value, zero T

	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return zero, nil
	}
	if err != nil {
		return zero, err
	}

	if err := json.Unmarshal(content, &value); err != nil {
		return zero, fmt.Errorf("%s: %w", path, err)
	}

	return value, nil
}

// SaveJSON writes a value as indented JSON. A failed write leaves the old file.
func SaveJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	return files.WriteAtomic(path, append(content, '\n'))
}

// openConfig loads config.json, read-only if it cannot be read.
func openConfig(dirs Dirs) *ConfigFile {
	file := &ConfigFile{Dirs: dirs, path: filepath.Join(dirs.App, "config.json")}
	file.config, file.fault = LoadJSON[Config](file.path)

	return file
}

// Config returns the config as held. The caller must not change it.
func (c *ConfigFile) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.config
}

// Revision returns how many times Update has changed the config.
func (c *ConfigFile) Revision() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.revision
}

// Update runs the change on a copy of the config, saves it and emits
// config:changed with the section ("" for all). It returns an error, and keeps
// the config as it was, if the config is read-only or the change or save fails.
func (c *ConfigFile) Update(section string, change func(config *Config) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fault != nil {
		return errors.New("config.json could not be read when the app started, so it is not written")
	}

	content, err := json.Marshal(c.config)
	if err != nil {
		return err
	}

	var next Config
	if err := json.Unmarshal(content, &next); err != nil {
		return err
	}

	if err := change(&next); err != nil {
		return err
	}

	if err := SaveJSON(c.path, next); err != nil {
		return err
	}

	c.config = next
	c.revision++
	application.Get().Event.Emit(EventConfigChanged, section)

	return nil
}

// LookupSetting returns a setting's value in a workspace, or the catalog's
// default. It returns false if the catalog has no such setting.
func (c *ConfigFile) LookupSetting(workspaceID, key string) (string, bool) {
	row := slices.IndexFunc(Catalog, func(setting Setting) bool { return setting.Key == key })
	if row < 0 {
		return "", false
	}
	setting := Catalog[row]

	c.mu.RLock()
	defer c.mu.RUnlock()

	values := c.config.Settings
	if setting.Workspace {
		values = nil
		position := slices.IndexFunc(c.config.Workspaces, func(record Workspace) bool { return record.ID == workspaceID })
		if position >= 0 {
			values = c.config.Workspaces[position].Settings
		}
	}

	if value, ok := values[key]; ok {
		return value, true
	}

	return setting.Default, true
}
