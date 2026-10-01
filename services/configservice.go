// configservice.go is the app's config, config.json: the game installs, the workspaces and their
// mods, and the settings, held in memory behind one lock and written whole on every change. Its
// RPCs are the settings, the app version and update check, and the reset; the other services read
// and change the config through read and mutate.

package services

import (
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

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/session"
	"paradox-modding-tools/services/internal/wiki"

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
	Version         string `json:"version"`
	VersionDetected string `json:"versionDetected"`
	IsBroken        bool   `json:"isBroken"` // derived by ListGameInstalls; whatever is on disk is ignored
	CreatedAt       string `json:"createdAt"`
	ScannedAt       string `json:"scannedAt,omitempty"`
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
	IdeOpenFiles         []string       `json:"ideOpenFiles,omitempty"`
	IdeActiveFile        string         `json:"ideActiveFile,omitempty"`
	CreatedAt            string         `json:"createdAt"`
	Mods                 []WorkspaceMod `json:"mods"`
}

// WorkspaceMod is a mod attached to a workspace.
type WorkspaceMod struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	SortOrder      int    `json:"sortOrder"`
	Color          string `json:"color,omitempty"`
	Thumbnail      string `json:"thumbnail,omitempty"`
	DescMdRel      string `json:"descMdRel,omitempty"`
	DescBbRel      string `json:"descBbRel,omitempty"`
	WorkshopIgnore string `json:"workshopIgnore,omitempty"`
	IsBroken       bool   `json:"isBroken"` // derived by markBroken; whatever is on disk is ignored
	CreatedAt      string `json:"createdAt"`
	// the eras of the mod's dates; set by hand, as no mod states them, and none leaves every date as written
	Eras session.Calendar `json:"eras,omitempty"`
}

// ConfigService holds config.json and serves the settings.
type ConfigService struct {
	Session *SessionService
	Version string

	mu      sync.Mutex
	path    string
	cfg     Config
	loadErr error // config.json exists but could not be read; the config is then served empty and never written
}

// OpenConfig loads config.json from the app's folder. A missing file is a first run; any other read
// failure leaves the config read-only.
func OpenConfig() (*ConfigService, error) {
	dir, err := local.AppDir()
	if err != nil {
		return nil, err
	}
	return newConfig(filepath.Join(dir, "config.json")), nil
}

func newConfig(path string) *ConfigService {
	config := &ConfigService{path: path, cfg: Config{Settings: map[string]map[string]string{}}}
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
func (s *ConfigService) read(fn func(*Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
}

// mutate runs fn and saves the config; an fn error or an unreadable config saves nothing.
func (s *ConfigService) mutate(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return fmt.Errorf("config.json unreadable, refusing to overwrite it: %w", s.loadErr)
	}
	if err := fn(&s.cfg); err != nil {
		return err
	}
	return local.Write(s.path, func(out io.Writer) error { return json.NewEncoder(out).Encode(&s.cfg) })
}

// GetVersion returns the app version string.
func (s *ConfigService) GetVersion() string {
	return s.Version
}

// CheckForUpdates triggers the Wails updater.
func (s *ConfigService) CheckForUpdates() {
	app := application.Get()
	if app == nil {
		return
	}
	if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
		app.Logger.Error("update", "error", err)
	}
}

// GetSettings loads the settings as a map of "namespace.key" to value.
func (s *ConfigService) GetSettings() (map[string]string, error) {
	out := map[string]string{}
	s.read(func(cfg *Config) {
		for namespace, values := range cfg.Settings {
			for key, value := range values {
				out[namespace+"."+key] = value
			}
		}
	})
	return out, nil
}

// SaveSettings writes settings keyed "namespace.key"; a key with no namespace is skipped.
func (s *ConfigService) SaveSettings(settings map[string]string) error {
	return s.mutate(func(cfg *Config) error {
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
func (s *ConfigService) ResetData() error {
	if err := s.mutate(func(cfg *Config) error {
		cfg.Installs, cfg.Workspaces = nil, nil
		return nil
	}); err != nil {
		return err
	}
	dir, err := local.CacheDir()
	if err != nil {
		return fmt.Errorf("cache dir: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("wipe cache: %w", err)
	}
	wiki.ForgetAll()
	if s.Session != nil {
		s.Session.dropAllExcept("")
	}
	return nil
}

func cloneWorkspace(ws Workspace) Workspace {
	ws.IdeOpenFiles = slices.Clone(ws.IdeOpenFiles)
	ws.Mods = slices.Clone(ws.Mods)
	slices.SortFunc(ws.Mods, func(first, second WorkspaceMod) int {
		if first.SortOrder != second.SortOrder {
			return first.SortOrder - second.SortOrder
		}
		return strings.Compare(first.Name, second.Name)
	})
	return ws
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

func installScriptRoot(cfg *Config, installID string) (string, error) {
	inst := findInstall(cfg, installID)
	if inst == nil {
		return "", fmt.Errorf("install not found")
	}
	info := game.Get(inst.GameID)
	if info == nil {
		return "", fmt.Errorf("unknown game: %s", inst.GameID)
	}
	return filepath.Join(inst.Path, info.ScriptRoot), nil
}
