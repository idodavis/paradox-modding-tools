// store.go is the JSON app config (installs, workspaces, settings).

package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/persist"
)

const (
	appConfigDirName    = "Paradox Modding Tools"
	configFileName      = "config.json"
	configFormatVersion = 1
	legacyDBName        = "pmt-workspace.db"
)

// Config is the on-disk app store.
type Config struct {
	FormatVersion int                          `json:"formatVersion"`
	Installs      []GameInstall                `json:"installs"`
	Workspaces    []Workspace                  `json:"workspaces"`
	Settings      map[string]map[string]string `json:"settings"`
}

// GameInstall is a user-configured game installation.
type GameInstall struct {
	ID              string `json:"id"`
	GameID          string `json:"gameId"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	Version         string `json:"version"`
	VersionDetected string `json:"versionDetected"`
	IsBroken        bool   `json:"isBroken"`
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
	IsBroken       bool   `json:"isBroken"`
	CreatedAt      string `json:"createdAt"`
	// Era and EpochOffset declare the calendar this mod's dates are written in.
	// Not derivable — nothing in a mod states the offset — so a person sets it
	// once, and with nothing set every date reads exactly as written.
	Era         string `json:"era,omitempty"`
	EpochOffset int    `json:"epochOffset,omitempty"`
}

// Store is the mutex-guarded JSON config.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
	// loadErr is set when config.json existed but could not be read for a
	// reason that is not a deliberate reset. The store then serves an empty
	// config but refuses to write, so a transient read failure cannot destroy
	// a good file.
	loadErr error
}

func appConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appConfigDirName)
	return dir, os.MkdirAll(dir, 0o755)
}

func dropLegacyDB(dir string) {
	p := filepath.Join(dir, legacyDBName)
	for _, s := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(p + s)
	}
}

func emptyConfig() Config {
	return Config{FormatVersion: configFormatVersion, Settings: map[string]map[string]string{}}
}

// errConfigReset marks the two load failures where starting empty is CORRECT:
// a missing formatVersion and a version mismatch. Both are deliberate — a bump
// wipes rather than migrates, because the config is small and re-enterable.
var errConfigReset = errors.New("config reset")

func newStore(path string) *Store {
	s := &Store{path: path, cfg: emptyConfig()}
	c, err := loadConfig(path)
	switch {
	case err == nil:
		s.cfg = c
		if s.cfg.Settings == nil {
			s.cfg.Settings = map[string]map[string]string{}
		}
	case os.IsNotExist(err), errors.Is(err, errConfigReset):
		// First run, or a deliberate format reset. Empty is right.
	default:
		// The file may be perfectly good and merely unreadable right now — an
		// antivirus hold, a permission blip, OneDrive mid-sync. Starting empty
		// presents as a first run, and the next Mutate would write that empty
		// config over the real one, losing every install, workspace and mod
		// with no backup and no way back. Refuse to write instead.
		s.loadErr = err
	}
	return s
}

func loadConfig(path string) (Config, error) {
	cfg, err := persist.LoadJSON[Config](path, configFormatVersion)
	if err != nil {
		if errors.Is(err, persist.ErrFormat) {
			return Config{}, fmt.Errorf("%w: %w", err, errConfigReset)
		}
		return Config{}, err
	}
	return *cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	return persist.SaveJSON(path, cfg)
}

// OpenStore loads config.json and drops the legacy SQLite files. Missing or a
// version mismatch → empty and writable; any other read failure → empty but
// read-only, so a transient error cannot overwrite a good file.
func OpenStore() (*Store, error) {
	dir, err := appConfigDir()
	if err != nil {
		return nil, err
	}
	dropLegacyDB(dir)
	return newStore(filepath.Join(dir, configFileName)), nil
}

// Read runs fn with the live config. Do not retain pointers after return.
func (s *Store) Read(fn func(*Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
}

// Mutate runs fn then saves. A fn error skips save, and so does an unreadable
// config: writing the empty stand-in over a file that exists and could not be
// read would lose every install, workspace and mod.
func (s *Store) Mutate(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return fmt.Errorf("config.json unreadable, refusing to overwrite it: %w", s.loadErr)
	}
	if err := fn(&s.cfg); err != nil {
		return err
	}
	s.cfg.FormatVersion = configFormatVersion
	if s.cfg.Settings == nil {
		s.cfg.Settings = map[string]map[string]string{}
	}
	return saveConfig(s.path, &s.cfg)
}

func cloneWorkspace(w Workspace) Workspace {
	w.IdeOpenFiles = append([]string(nil), w.IdeOpenFiles...)
	mods := make([]WorkspaceMod, len(w.Mods))
	copy(mods, w.Mods)
	sortWorkspaceMods(mods)
	w.Mods = mods
	return w
}

func sortWorkspaceMods(mods []WorkspaceMod) {
	slices.SortFunc(mods, func(a, b WorkspaceMod) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		return strings.Compare(a.Name, b.Name)
	})
}

func findInstall(c *Config, id string) *GameInstall {
	for i := range c.Installs {
		if c.Installs[i].ID == id {
			return &c.Installs[i]
		}
	}
	return nil
}

func installScriptRoot(c *Config, installID string) (string, error) {
	inst := findInstall(c, installID)
	if inst == nil {
		return "", fmt.Errorf("install not found")
	}
	info := game.Get(inst.GameID)
	if info == nil {
		return "", fmt.Errorf("unknown game: %s", inst.GameID)
	}
	return filepath.Join(inst.Path, info.ScriptRoot), nil
}
func findWorkspace(c *Config, id string) *Workspace {
	for i := range c.Workspaces {
		if c.Workspaces[i].ID == id {
			return &c.Workspaces[i]
		}
	}
	return nil
}
