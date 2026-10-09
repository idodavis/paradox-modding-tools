// appservice.go is the app-wide service: the version, the update check, the display settings and
// the reset. It also holds config.json (the game installs, the workspaces and their mods, the
// settings) in memory behind one lock, written whole on every change; the other services read and
// change it through read and mutate.

package services

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// AppService serves the app-wide calls and holds config.json for every other service.
type AppService struct {
	Session *SessionService
	Version string

	mu      sync.Mutex
	path    string
	cfg     Config
	loadErr error // config.json exists but could not be read; the config is then served empty and never written
}

// OpenApp loads config.json from the app's folder. A missing file is a first run; any other read
// failure leaves the config read-only.
func OpenApp() (*AppService, error) {
	dir, err := appdata.Dir()
	if err != nil {
		return nil, err
	}
	return newApp(filepath.Join(dir, "config.json")), nil
}

func newApp(path string) *AppService {
	config := &AppService{path: path, cfg: Config{Settings: map[string]map[string]string{}}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config
	}
	if err == nil {
		err = json.Unmarshal(raw, &config.cfg)
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
func (s *AppService) read(fn func(*Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
}

// mutate runs fn and saves the config, then tells every window which section changed (settings,
// installs, workspaces; "" all); an fn error or an unreadable config saves nothing.
func (s *AppService) mutate(section string, fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return fmt.Errorf("config.json unreadable, refusing to overwrite it: %w", s.loadErr)
	}
	if err := fn(&s.cfg); err != nil {
		return err
	}
	if err := s.save(); err != nil {
		return err
	}
	emit(EventConfigChanged, section)
	return nil
}

// save writes config.json; the caller holds the lock, or has the AppService to itself.
func (s *AppService) save() error {
	return appdata.Write(s.path, func(out io.Writer) error { return json.NewEncoder(out).Encode(&s.cfg) })
}

// GetVersion returns the app version string.
func (s *AppService) GetVersion() string {
	return s.Version
}

// CheckForUpdates triggers the Wails updater.
func (s *AppService) CheckForUpdates() {
	app := application.Get()
	if app == nil {
		return
	}
	if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
		app.Logger.Error("update", "error", err)
	}
}

// GetSettings loads the settings as a map of "namespace.key" to value.
func (s *AppService) GetSettings() map[string]string {
	out := map[string]string{}
	s.read(func(cfg *Config) {
		for namespace, values := range cfg.Settings {
			for key, value := range values {
				out[namespace+"."+key] = value
			}
		}
	})
	return out
}

// SaveSettings writes settings keyed "namespace.key"; a key with no namespace is skipped.
func (s *AppService) SaveSettings(settings map[string]string) error {
	return s.mutate("settings", func(cfg *Config) error {
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

// ResetData forgets every install and workspace, deletes the caches, and closes the live session.
// Mod and game folders stay.
func (s *AppService) ResetData() error {
	if err := s.mutate("", func(cfg *Config) error {
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
	wiki.ForgetAll()
	s.Session.dropAllExcept("")
	return nil
}

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

// workspace is a copy of a workspace and of the install it is on (nil where none is listed), from one read;
// errNoWorkspace where the workspace is not listed.
func (s *AppService) workspace(id string) (ws *Workspace, inst *GameInstall, err error) {
	s.read(func(cfg *Config) {
		listed := findWorkspace(cfg, id)
		if listed == nil {
			return
		}
		copied := cloneWorkspace(*listed)
		ws = &copied
		if found := findInstall(cfg, listed.InstallID); found != nil {
			record := *found
			inst = &record
		}
	})
	if ws == nil {
		return nil, nil, errNoWorkspace
	}
	return ws, inst, nil
}

// mod is one of the workspace's mods, by its id.
func (ws *Workspace) mod(id string) (*WorkspaceMod, error) {
	for i := range ws.Mods {
		if ws.Mods[i].ID == id {
			return &ws.Mods[i], nil
		}
	}
	return nil, errNoMod
}

func findInstall(cfg *Config, id string) *GameInstall {
	for i := range cfg.Installs {
		if cfg.Installs[i].ID == id {
			return &cfg.Installs[i]
		}
	}
	return nil
}

func findWorkspace(cfg *Config, id string) *Workspace {
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == id {
			return &cfg.Workspaces[i]
		}
	}
	return nil
}
