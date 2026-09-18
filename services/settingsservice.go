// settingsservice.go persists app settings via the JSON store.

package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/wiki"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SettingsService provides persisted app settings.
type SettingsService struct {
	Store   *Store
	Session *SessionService
	Version string
}

// GetVersion returns the app version string.
func (s *SettingsService) GetVersion() string {
	return s.Version
}

// CheckForUpdates triggers the Wails updater.
func (s *SettingsService) CheckForUpdates() {
	app := application.Get()
	if app == nil {
		return
	}
	if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
		app.Logger.Error("update", "error", err)
	}
}

// GetSettings loads settings as a map "game.key" -> value.
func (s *SettingsService) GetSettings() (map[string]string, error) {
	out := map[string]string{}
	s.Store.Read(func(c *Config) {
		for game, kv := range c.Settings {
			for k, v := range kv {
				out[game+"."+k] = v
			}
		}
	})
	return out, nil
}

// SaveSettings writes user settings keyed as "game.key".
func (s *SettingsService) SaveSettings(settings map[string]string) error {
	return s.Store.Mutate(func(c *Config) error {
		for k, v := range settings {
			parts := strings.SplitN(k, ".", 2)
			if len(parts) != 2 {
				continue
			}
			if c.Settings[parts[0]] == nil {
				c.Settings[parts[0]] = map[string]string{}
			}
			c.Settings[parts[0]][parts[1]] = v
		}
		return nil
	})
}

// ResetData wipes workspaces, mods, installs, leftover PMT workspace dirs,
// the semantic cache, and in-memory wiki sidecars. Mod and game folders stay.
func (s *SettingsService) ResetData() error {
	root, rootErr := pmtDataRoot()
	var wipe []string
	s.Store.Read(func(c *Config) {
		for _, ws := range c.Workspaces {
			if rootErr == nil && ws.ID != "" {
				wipe = append(wipe, filepath.Join(root, "workspaces", ws.ID))
			}
		}
	})
	if rootErr == nil {
		wipe = append(wipe, filepath.Join(root, "patch_runs"))
		removeOwnedDirs(root, wipe)
	}
	if err := s.Store.Mutate(func(c *Config) error {
		c.Installs, c.Workspaces = nil, nil
		return nil
	}); err != nil {
		return err
	}
	dir, err := index.CacheDir()
	if err != nil {
		return fmt.Errorf("cache dir: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("wipe cache: %w", err)
	}
	wiki.ForgetAll()
	if s.Session != nil {
		s.Session.pool().DropAll()
	}
	return nil
}

func pmtDataRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appConfigDirName), nil
}

// ownedByPMT reports whether path is a subdirectory of root (not root itself).
func ownedByPMT(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

func resetLog(msg string, args ...any) {
	if app := application.Get(); app != nil {
		app.Logger.Warn(msg, args...)
	}
}

// removeOwnedDirs deletes PMT-owned dirs. Paths outside root are skipped.
func removeOwnedDirs(root string, dirs []string) {
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		if !ownedByPMT(root, dir) {
			resetLog("reset-data: skip non-PMT path", "path", dir)
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			resetLog("reset-data: wipe failed", "path", dir, "error", err)
		}
	}
}
