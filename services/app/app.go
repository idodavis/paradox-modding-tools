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
	EventConfigChanged = "config:changed" // config.json was written; the data is the section
	EventAppUpdate     = "app:update"     // a newer version exists; the data is the Update
	EventWindowClosing = "window:closing" // a window waits to close for the frontend's answer; the data is its name
	EventWindowClosed  = "window:closed"  // a window closed; the data is its name
)

// init registers the app events with the application.
func init() {
	application.RegisterEvent[string](EventConfigChanged)
	application.RegisterEvent[Update](EventAppUpdate)
	application.RegisterEvent[string](EventWindowClosing)
	application.RegisterEvent[string](EventWindowClosed)
}

// Service is the Wails service for the app itself: its version and updates, its settings, its log and its windows.
type Service struct {
	Config *ConfigFile // config.json, which carries PMT's folders
	Build  string      // the version the build set
}

// ServiceStartup writes a mark saying the app is running, which CrashedLastRun reads on the next start.
func (app *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	mark := resolveMark(app.Config.Dirs)
	_, err := os.Stat(mark)
	// a mark still there means the last run never shut down
	return os.WriteFile(mark, []byte(strconv.FormatBool(err == nil)), 0o644)
}

// ServiceShutdown removes the running mark, so the next start knows the shutdown was clean.
func (app *Service) ServiceShutdown() error { return os.Remove(resolveMark(app.Config.Dirs)) }

// An Update is the result of a check for a newer version of the app. It is the data of the app:update event.
type Update struct {
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
}

// OpenConfig finds PMT's folders on this machine and loads config.json. It returns an error if a folder cannot be
// made.
func OpenConfig() (*ConfigFile, error) {
	dirs, err := ResolveDirs()
	if err != nil {
		return nil, err
	}
	return openConfig(dirs), nil
}

// OpenLog opens pmt.log in the log folder and returns the logger that the app and the services write through. It
// starts the file over if it has grown too large. It returns an error if the file cannot be opened.
func OpenLog(config *ConfigFile, version string) (*slog.Logger, error) {
	path := resolveLog(config.Dirs)
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	// a log that has grown too large starts over
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

// CheckUpdate asks the project's GitHub releases whether a newer version exists, and emits app:update if one does.
// It returns an empty Update if none does.
func (app *Service) CheckUpdate() (Update, error) {
	release, err := application.Get().Updater.Check(context.Background())
	if err != nil || release == nil {
		return Update{}, err
	}
	update := Update{Version: release.Version, Available: true, Notes: release.Notes}
	application.Get().Event.Emit(EventAppUpdate, update)
	return update, nil
}

// ApplyUpdate downloads the newer version beside the app and restarts into it. It returns an error if there is
// none.
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
	return updater.Restart(context.Background())
}

// ListSettings returns the settings catalog.
func (app *Service) ListSettings() []Setting { return Catalog }

// ReadSettings returns every setting's value by key, using the catalog's default for a setting that was never
// set.
func (app *Service) ReadSettings() map[string]string {
	return lo.SliceToMap(Catalog, func(setting Setting) (string, string) {
		value, _ := app.Config.LookupSetting(setting.Key)
		return setting.Key, value
	})
}

// WriteSetting sets one setting to the given value and emits config:changed for the settings. It returns an error if
// the config is read-only or cannot be saved.
func (app *Service) WriteSetting(key, value string) error {
	return app.Config.Update("settings", func(config *Config) error {
		if config.Settings == nil {
			config.Settings = map[string]string{}
		}
		config.Settings[key] = value
		return nil
	})
}

// CrashedLastRun reports whether the last run ended without a clean shutdown. It is false on the first run.
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

// OpenWindow opens a new window with the given name on a page of the app. It returns an error if a window already
// has the name.
func (app *Service) OpenWindow(name, url string) error {
	pmt := application.Get()
	if _, open := pmt.Window.Get(name); open {
		return fmt.Errorf("a window named %q is already open", name)
	}
	window := pmt.Window.NewWithOptions(application.WebviewWindowOptions{Name: name, URL: url})
	// the first close waits for the frontend; the next one goes through
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

// ShowWindow brings the window with the given name to the front. It returns an error if no window has the name.
func (app *Service) ShowWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}
	window.Focus()
	return nil
}

// CloseWindow closes the window with the given name. A window opened by OpenWindow first gets a window:closing event.
// It closes when the frontend closes it again, or when a timer ends. It returns an error if no window has the name.
func (app *Service) CloseWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}
	window.Close()
	return nil
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
