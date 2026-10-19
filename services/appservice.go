// appservice.go is the app-wide service: the version, the update check, the display settings and the reset. It also
// holds config.json (the game installs, the workspaces and their mods, the settings) as ConfigFile, in memory behind
// one lock, written whole on every change; every other service reads and changes it through read and mutate.

package services

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/wiki"
	"paradox-modding-tools/services/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventConfigChanged is config.json being written: data is the section (workspaces, installs, settings; "" all).
const EventConfigChanged = "config:changed"

// registers the event so the bindings generator types it
func init() {
	application.RegisterEvent[string](EventConfigChanged)
}

// Config is config.json.
type Config struct {
	Installs   []GameInstall                `json:"installs"`
	Workspaces []Workspace                  `json:"workspaces"`
	Settings   map[string]map[string]string `json:"settings"`
}

// GameInstall is a user-configured game installation.
type GameInstall struct {
	ID              string `json:"id"`
	GameID          string `json:"gameId"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	VersionDetected string `json:"versionDetected,omitempty"` // derived: read from the install each time
	ScannedAt       string `json:"scannedAt,omitempty"`       // derived: its install built, else its cache written
}

// Workspace is a user-defined mod working environment tied to a game.
type Workspace struct {
	ID                   string         `json:"id"`
	GameID               string         `json:"gameId"`
	Name                 string         `json:"name"`
	InstallID            string         `json:"installId"`
	DefaultLocLang       string         `json:"defaultLocLang"`
	GameColor            string         `json:"gameColor,omitempty"`
	ResetIdeOnOpen       bool           `json:"resetIdeOnOpen"`
	HideExplorerBinaries bool           `json:"hideExplorerBinaries"`
	DefaultTool          string         `json:"defaultTool,omitempty"`
	Mods                 []WorkspaceMod `json:"mods"`
}

// WorkspaceMod is a mod attached to a workspace.
type WorkspaceMod struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	SortOrder      int    `json:"sortOrder"`
	Color          string `json:"color,omitempty"` // a hand-picked hex; it wins over the slot
	Slot           int    `json:"slot"`            // the mod's color slot, 1 to 16; a reorder keeps it
	Thumbnail      string `json:"thumbnail,omitempty"`
	DescSource     string `json:"descSource,omitempty"` // markdown, bbcode or steam; "" for the file the mod has
	DescMdRel      string `json:"descMdRel,omitempty"`
	DescBbRel      string `json:"descBbRel,omitempty"`
	WorkshopIgnore string `json:"workshopIgnore,omitempty"`
	IsBroken       bool   `json:"isBroken"` // derived by markDisk; a value saved in config.json is ignored
	// the folder as shown: under the game's user folder relative to it (mod\sir), else absolute; derived
	// by markDisk
	Folder string `json:"folder"`
	// the eras of the mod's dates, set by hand (no mod states them); none leaves dates as written
	Eras workspace.Calendar `json:"eras,omitempty"`
}

// ConfigFile is config.json held in memory behind one lock and written whole on every change; every service reads
// and changes it through read and mutate. A file that exists but cannot be read is served empty and never written,
// so a transient failure never overwrites the real file.
type ConfigFile struct {
	mu      sync.Mutex
	path    string
	cfg     Config
	loadErr error // config.json exists but could not be read; the config is then served empty and never written
}

// AppService serves the app-wide calls: the version, the update check, the settings and the reset.
type AppService struct {
	config     *ConfigFile
	workspaces *WorkspaceService
	wikis      *wiki.Client
	version    string
}

// NewAppService is the app-wide service over the config, the workspace service and wiki client it resets, and the
// app's version.
func NewAppService(config *ConfigFile, workspaces *WorkspaceService, wikis *wiki.Client, version string) *AppService {
	return &AppService{config: config, workspaces: workspaces, wikis: wikis, version: version}
}

// OpenConfig loads config.json from the app's folder. A missing file is a first run; any other read failure
// leaves the config read-only.
func OpenConfig() (*ConfigFile, error) {
	dir, err := appdata.Dir()
	if err != nil {
		return nil, err
	}
	return openConfigFile(filepath.Join(dir, "config.json")), nil
}

// openConfigFile loads one config.json; the tests give it a temporary path.
func openConfigFile(path string) *ConfigFile {
	config := &ConfigFile{path: path, cfg: Config{Settings: map[string]map[string]string{}}}
	err := appdata.LoadJSON(path, &config.cfg)
	if errors.Is(err, os.ErrNotExist) {
		return config
	}
	if err != nil {
		// an antivirus hold, a permission blip or OneDrive mid-sync: starting empty would read as a
		// first run, and the next change would write that over every install and workspace
		config.loadErr = err
		config.cfg = Config{}
	}
	if config.cfg.Settings == nil {
		config.cfg.Settings = map[string]map[string]string{}
	}
	return config
}

// read runs fn with the live config under the lock; fn must not keep pointers into it.
func (config *ConfigFile) read(fn func(*Config)) {
	config.mu.Lock()
	defer config.mu.Unlock()
	fn(&config.cfg)
}

// mutate runs fn and saves the config, then tells every window which section changed (settings, installs,
// workspaces; "" all); an fn error or an unreadable config saves nothing.
func (config *ConfigFile) mutate(section string, fn func(*Config) error) error {
	config.mu.Lock()
	defer config.mu.Unlock()
	if config.loadErr != nil {
		return fmt.Errorf("config.json unreadable, refusing to overwrite it: %w", config.loadErr)
	}
	if err := fn(&config.cfg); err != nil {
		return err
	}
	if err := config.save(); err != nil {
		return err
	}
	emit(EventConfigChanged, section)
	return nil
}

// save writes config.json; the caller holds the lock, or has the ConfigFile to itself.
func (config *ConfigFile) save() error {
	return appdata.SaveJSON(config.path, &config.cfg)
}

// workspace is a copy of a workspace and of the install it is on (nil where none is listed), from one read;
// errNoWorkspace where the workspace is not listed.
func (config *ConfigFile) workspace(id string) (ws *Workspace, inst *GameInstall, err error) {
	config.read(func(cfg *Config) {
		entry := findWorkspace(cfg, id)
		if entry == nil {
			return
		}
		copied := cloneWorkspace(*entry)
		ws = &copied
		if install := findInstall(cfg, entry.InstallID); install != nil {
			record := *install
			inst = &record
		}
	})
	if ws == nil {
		return nil, nil, errNoWorkspace
	}
	return ws, inst, nil
}

// GetVersion returns the app version string.
func (app *AppService) GetVersion() string {
	return app.version
}

// CheckForUpdates triggers the Wails updater.
func (*AppService) CheckForUpdates() {
	app := application.Get()
	if app == nil {
		return
	}
	if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
		slog.Error("update", "error", err)
	}
}

// GetSettings loads the settings as a map of "namespace.key" to value.
func (app *AppService) GetSettings() map[string]string {
	out := map[string]string{}
	app.config.read(func(cfg *Config) {
		for namespace, values := range cfg.Settings {
			for key, value := range values {
				out[namespace+"."+key] = value
			}
		}
	})
	return out
}

// SaveSettings writes settings keyed "namespace.key"; a key with no namespace is skipped.
func (app *AppService) SaveSettings(settings map[string]string) error {
	return app.config.mutate("settings", func(cfg *Config) error {
		for qualified, value := range settings {
			namespace, key, ok := strings.Cut(qualified, ".")
			if !ok {
				continue
			}
			if cfg.Settings[namespace] == nil {
				cfg.Settings[namespace] = map[string]string{}
			}
			cfg.Settings[namespace][key] = value
		}
		return nil
	})
}

// ResetData forgets every install and workspace, deletes the caches, and closes the open workspace.
// Mod and game folders stay.
func (app *AppService) ResetData() error {
	if err := app.config.mutate("", func(cfg *Config) error {
		cfg.Installs, cfg.Workspaces = nil, nil
		return nil
	}); err != nil {
		return err
	}
	dir, err := appdata.CacheDir()
	if err != nil {
		return fmt.Errorf("cache dir: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("wipe cache: %w", err)
	}
	app.wikis.ForgetAll()
	app.workspaces.slot.Close("")
	return nil
}

// emit sends an event to the frontend when the app is running; the tests run with no app.
func emit(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}

// cloneWorkspace is a copy of a workspace with its own mods, in load order.
func cloneWorkspace(ws Workspace) Workspace {
	ws.Mods = slices.Clone(ws.Mods)
	slices.SortFunc(ws.Mods, loadOrder)
	return ws
}

// loadOrder compares two mods by their place in the workspace's load order.
func loadOrder(first, second WorkspaceMod) int {
	return cmp.Or(first.SortOrder-second.SortOrder, strings.Compare(first.Name, second.Name))
}

// errNoWorkspace and errNoMod are what an id no workspace or mod has is told.
var (
	errNoWorkspace = errors.New("workspace not found")
	errNoMod       = errors.New("mod not found")
)

// mod is one of the workspace's mods, by its id.
func (ws *Workspace) mod(id string) (*WorkspaceMod, error) {
	for i := range ws.Mods {
		if ws.Mods[i].ID == id {
			return &ws.Mods[i], nil
		}
	}
	return nil, errNoMod
}

// findMod is one of a workspace's mods and the workspace, by their ids.
func findMod(config *ConfigFile, workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	ws, _, err := config.workspace(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	mod, err := ws.mod(modID)
	return ws, mod, err
}

// findInstall is the config's install of an id, nil where none has it.
func findInstall(cfg *Config, id string) *GameInstall {
	for i := range cfg.Installs {
		if cfg.Installs[i].ID == id {
			return &cfg.Installs[i]
		}
	}
	return nil
}

// findWorkspace is the config's workspace of an id, nil where none has it.
func findWorkspace(cfg *Config, id string) *Workspace {
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == id {
			return &cfg.Workspaces[i]
		}
	}
	return nil
}
