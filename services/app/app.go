package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
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

// mainWindow is the name of the main window, whose close quits the app.
const mainWindow = "main"

// DevBuild is the version of a build run from the source, which no release has.
const DevBuild = "dev"

// Service is the Wails service for the app itself: its version and updates, its settings, its log and its windows.
type Service struct {
	Config       *ConfigFile // config.json, which carries PMT's folders
	Build        string      // the version the build set
	mainClosable atomic.Bool // the frontend let the main window close
}

// ServiceStartup empties the trash and writes a mark saying the app is running, which CrashedLastRun reads on the
// next start.
func (app *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	_ = os.RemoveAll(app.Config.Dirs.Trash)
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
}

// A Release is one published version of the app, with its notes as the Markdown they are written in.
type Release struct {
	Version string    `json:"version"`
	Date    time.Time `json:"date"`
	Notes   string    `json:"notes"`
}

// releasesURL is where GitHub lists the app's releases, the newest first.
const releasesURL = "https://api.github.com/repos/idodavis/paradox-modding-tools/releases"

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
	update := Update{Version: release.Version, Available: true}
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

// ListReleases returns the app's published releases from the newest down to the one running, each with its notes. A
// build that is no release, such as a dev build, gets the newest only. Drafts and prereleases are left out. It returns
// an error if GitHub cannot be reached or answers with an error.
func (app *Service) ListReleases() ([]Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", response.Status)
	}
	var listed []struct {
		Tag        string    `json:"tag_name"`
		Body       string    `json:"body"`
		Published  time.Time `json:"published_at"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		return nil, err
	}
	running := strings.TrimPrefix(app.Build, "v")
	var releases []Release
	for _, published := range listed {
		if published.Draft || published.Prerelease {
			continue
		}
		version := strings.TrimPrefix(published.Tag, "v")
		releases = append(releases, Release{Version: version, Date: published.Published, Notes: published.Body})
		if version == running {
			return releases, nil
		}
	}
	return releases[:min(len(releases), 1)], nil
}

// ListSettings returns the settings catalog.
func (app *Service) ListSettings() []Setting { return Catalog }

// ReadSettings returns every setting's value by key in the given workspace, using the catalog's default for a setting
// that was never set.
func (app *Service) ReadSettings(workspaceID string) map[string]string {
	return lo.SliceToMap(Catalog, func(setting Setting) (string, string) {
		value, _ := app.Config.LookupSetting(workspaceID, setting.Key)
		return setting.Key, value
	})
}

// WriteSetting sets one setting to the given value, in the given workspace's record for a setting kept per workspace,
// and emits config:changed for the settings. It returns an error if the catalog has no such setting, the workspace is
// not recorded, or the config is read-only or cannot be saved.
func (app *Service) WriteSetting(workspaceID, key, value string) error {
	setting, ok := lo.Find(Catalog, func(setting Setting) bool { return setting.Key == key })
	if !ok {
		return fmt.Errorf("the catalog has no setting %q", key)
	}
	return app.Config.Update("settings", func(config *Config) error {
		values := &config.Settings
		if setting.Workspace {
			position := slices.IndexFunc(config.Workspaces, func(record Workspace) bool { return record.ID == workspaceID })
			if position < 0 {
				return fmt.Errorf("the workspace %s is not recorded", workspaceID)
			}
			values = &config.Workspaces[position].Settings
		}
		if *values == nil {
			*values = map[string]string{}
		}
		(*values)[key] = value
		return nil
	})
}

// CrashedLastRun reports whether the last run ended without a clean shutdown. It is false on the first run, and in a
// dev build, which is stopped without a clean shutdown each time it is rebuilt.
func (app *Service) CrashedLastRun() bool {
	if app.Build == DevBuild {
		return false
	}
	mark, err := os.ReadFile(resolveMark(app.Config.Dirs))
	if err != nil {
		return false
	}
	crashed, _ := strconv.ParseBool(string(mark))
	return crashed
}

// LogPath returns the path of pmt.log.
func (app *Service) LogPath() string { return resolveLog(app.Config.Dirs) }

// BuildDiagnostics returns what a bug report needs: the version, the system, the installs and the end of pmt.log.
func (app *Service) BuildDiagnostics() string {
	var report strings.Builder
	system := runtime.GOOS + "/" + runtime.GOARCH
	env := application.Get().Env.Info()
	if env.OSInfo != nil {
		// Windows 11 still names itself Windows 10 in the registry; its branding says 11
		system = fmt.Sprintf("%s %s, %s", cmp.Or(env.OSInfo.Branding, env.OSInfo.Name), env.OSInfo.Version, system)
	}
	if webview, ok := env.PlatformInfo["WebView2"]; ok {
		system += fmt.Sprintf(", WebView2 %v", webview)
	}
	fmt.Fprintf(&report, "Paradox Modding Tools %s\nSystem: %s\n", app.Build, system)
	for _, install := range app.Config.Config().Installs {
		fmt.Fprintf(&report, "Install: %s, version %s\n", install.Game, install.Version)
	}
	text, _ := os.ReadFile(resolveLog(app.Config.Dirs))
	lines := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	fmt.Fprintf(&report, "\npmt.log, its last 200 lines:\n%s\n", strings.Join(lines[max(0, len(lines)-200):], "\n"))
	return report.String()
}

// ShowFolder opens a folder in the system's file manager, or a file's folder with the file selected. It returns an
// error if the path does not exist.
func (app *Service) ShowFolder(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	return application.Get().Env.OpenFileManager(path, !stat.IsDir())
}

// OpenMainWindow opens the main window. Its close is held and sent to the frontend as window:closing, until
// CloseWindow lets it through and the app quits.
//
//wails:ignore
func (app *Service) OpenMainWindow() {
	pmt := application.Get()
	window := pmt.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   mainWindow,
		Title:  "Paradox Modding Tools",
		Width:  1400,
		Height: 900,
		URL:    "/",
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !app.mainClosable.Load() {
			event.Cancel()
			pmt.Event.Emit(EventWindowClosing, mainWindow)
		}
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { pmt.Quit() })
}

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
// It closes when the frontend closes it again, or when a timer ends. The main window's close quits the app. It returns
// an error if no window has the name.
func (app *Service) CloseWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}
	if name == mainWindow {
		app.mainClosable.Store(true)
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
