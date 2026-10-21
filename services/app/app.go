package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The events the app sends.
const (
	EventConfigChanged = "config:changed" // config.json was written; the data names its section: installs, workspaces, settings
	EventAppUpdate     = "app:update"     // an update is available or was applied; the data is the Update
	EventWindowClosing = "window:closing" // a window is about to close, held until the frontend answers; the data is its name
	EventWindowClosed  = "window:closed"  // a window closed; the data is its name
)

func init() {
	application.RegisterEvent[string](EventConfigChanged)
	application.RegisterEvent[Update](EventAppUpdate)
	application.RegisterEvent[string](EventWindowClosing)
	application.RegisterEvent[string](EventWindowClosed)
}

// Service is the app's own service: the version and the updater, the settings catalog and values, the data
// reset, the log, and the other windows. The frontend's own log lines go through Wails's log service.
type Service struct {
	Config *ConfigFile // config.json, which carries PMT's folders
	Build  string      // the version the build set
}

// ServiceStartup writes a mark saying the app is running, which CrashedLastRun reads on the next start.
func (app *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	mark := resolveMark(app.Config.Dirs)
	_, err := os.Stat(mark)
	// a mark still there was left by a run that never shut down; the new mark records that for CrashedLastRun
	return os.WriteFile(mark, []byte(strconv.FormatBool(err == nil)), 0o644)
}

// ServiceShutdown removes the running mark, so the next start knows the shutdown was clean.
func (app *Service) ServiceShutdown() error { return os.Remove(resolveMark(app.Config.Dirs)) }

// An Update says which version was found and whether it is newer than this one. It is the data of the app:update
// event.
type Update struct {
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
}

// OpenConfig finds PMT's folders on this machine and loads config.json.
func OpenConfig() (*ConfigFile, error) {
	dirs, err := ResolveDirs()
	if err != nil {
		return nil, err
	}
	return openConfig(dirs)
}

// OpenLog opens pmt.log in the log folder, truncating it when it has grown too large, and returns the logger that
// Wails and the services write through.
func OpenLog(config *ConfigFile, version string) (*slog.Logger, error) {
	path := resolveLog(config.Dirs)
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	// past 4 MB the log starts over
	if stat, err := os.Stat(path); err == nil && stat.Size() > 4<<20 {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(file, nil))
	logger.Info("started", "version", version)
	return logger, nil
}

// Version returns the version the build set.
func (app *Service) Version() string { return app.Build }

// CheckUpdate asks GitHub's releases whether a newer version exists. A release is verified by its SHA256SUMS.
func (app *Service) CheckUpdate() (Update, error) {
	release, err := application.Get().Updater.Check(context.Background())
	if err != nil || release == nil {
		return Update{}, err
	}
	update := Update{Version: release.Version, Available: true, Notes: release.Notes}
	application.Get().Event.Emit(EventAppUpdate, update)
	return update, nil
}

// ApplyUpdate downloads the update beside the app and restarts into it.
func (app *Service) ApplyUpdate() error {
	update, err := app.CheckUpdate()
	if err != nil {
		return err
	}
	if !update.Available {
		return errors.New("no update is available")
	}
	updater := application.Get().Updater
	if err := updater.DownloadAndInstall(context.Background()); err != nil {
		return err
	}
	application.Get().Event.Emit(EventAppUpdate, update)
	return updater.Restart(context.Background())
}

// ListSettings returns the settings catalog.
func (app *Service) ListSettings() []Setting { return Catalog }

// ReadSettings returns every setting's value by key, using the catalog's default for a setting that was never
// set.
func (app *Service) ReadSettings() map[string]string {
	settings := app.Config.Config().Settings
	return lo.SliceToMap(Catalog, func(setting Setting) (string, string) {
		if value, ok := settings[setting.Key]; ok {
			return setting.Key, value
		}
		return setting.Key, setting.Default
	})
}

// WriteSetting writes one setting and emits config:changed for the settings.
func (app *Service) WriteSetting(key, value string) error {
	err := app.Config.Update(func(config *Config) {
		if config.Settings == nil {
			config.Settings = map[string]string{}
		}
		config.Settings[key] = value
	})
	if err != nil {
		return err
	}
	application.Get().Event.Emit(EventConfigChanged, "settings")
	return nil
}

// ResetData removes every workspace, install record and cache. Mods, game files and the settings stay.
func (app *Service) ResetData() error {
	err := app.Config.Update(func(config *Config) {
		config.Installs, config.Workspaces = nil, nil
	})
	if err != nil {
		return err
	}
	// the records go first, so a failing sweep never leaves the frontend showing workspaces that are gone
	application.Get().Event.Emit(EventConfigChanged, "")
	cache := app.Config.Dirs.Cache
	entries, err := os.ReadDir(cache)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cache, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// CrashedLastRun reports whether the last run ended without a clean shutdown.
func (app *Service) CrashedLastRun() bool {
	mark, err := os.ReadFile(resolveMark(app.Config.Dirs))
	if err != nil {
		return false
	}
	crashed, _ := strconv.ParseBool(string(mark))
	return crashed
}

// LogPath returns the path of pmt.log.
func (app *Service) LogPath() string { return resolveLog(app.Config.Dirs) }

// OpenWindow opens a new window with the given name at a URL of the app.
func (app *Service) OpenWindow(name, url string) error {
	pmt := application.Get()
	if _, open := pmt.Window.Get(name); open {
		return fmt.Errorf("a window named %q is already open", name)
	}
	window := pmt.Window.NewWithOptions(application.WebviewWindowOptions{Name: name, URL: url})
	// the first close is held for the frontend's answer; the next one, ReleaseClose's or the timer's, goes through
	var unhook func()
	unhook = window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		unhook()
		pmt.Event.Emit(EventWindowClosing, name)
		time.AfterFunc(5*time.Second, window.Close)
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		pmt.Event.Emit(EventWindowClosed, name)
	})
	return nil
}

// ShowWindow brings a window to the front.
func (app *Service) ShowWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}
	window.Focus()
	return nil
}

// CloseWindow closes a window. A window holding tabs gets a window:closing event first and closes when the
// frontend answers with ReleaseClose, or after five seconds.
func (app *Service) CloseWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}
	window.Close()
	return nil
}

// ReleaseClose lets a close that CloseWindow is holding go ahead.
func (app *Service) ReleaseClose(name string) {
	if window, err := lookupWindow(name); err == nil {
		window.Close()
	}
}

// ListWindows returns the names of the open windows.
func (app *Service) ListWindows() []string {
	return lo.Map(application.Get().Window.GetAll(), func(window application.Window, _ int) string {
		return window.Name()
	})
}

// resolveMark returns the path of the running mark, which sits beside the log.
func resolveMark(dirs Dirs) string { return filepath.Join(dirs.Log, "running") }

// resolveLog returns the path of pmt.log.
func resolveLog(dirs Dirs) string { return filepath.Join(dirs.Log, "pmt.log") }

// lookupWindow returns the open window with the given name. It returns an error when no window has it.
func lookupWindow(name string) (application.Window, error) {
	window, open := application.Get().Window.Get(name)
	if !open {
		return nil, fmt.Errorf("no window is named %q", name)
	}
	return window, nil
}
