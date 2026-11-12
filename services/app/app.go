// Package app is the service for the app itself: its version and updates, its
// settings and log, and its windows. It also holds PMT's folders and
// config.json, which every other service reads and writes through the one
// ConfigFile that main opens.
//
// It applies these rules:
//
//   - config.json has no format version. A changed record means Reset data,
//     never a migration.
//   - A missing config.json is a first run. Any other read failure gives an
//     empty config that is read-only.
//   - A setting marked Workspace is kept in its workspace's record, any other
//     in the config's own Settings. A setting never set has the catalog's
//     default.
//   - Updates come from the project's GitHub releases, and a download is
//     checked against the release's SHA256SUMS. A dev build is no release, so
//     it never checks and never installs one.
//   - Going back to an older release skips the version running until a newer
//     one is out.
//   - A run that left its running mark behind never shut down cleanly, except
//     in a dev build, which is stopped each time it is rebuilt.
//   - The main window's close waits for the page's answer, and then quits the
//     app. A window opened by OpenWindow gets one window:closing event, and
//     closes when the page closes it again or after five seconds.
//
// # Robustness
//
// An unreadable config.json is never written: Update returns an error and the
// real file stays. A change or a save that fails leaves the config as it was.
// LoadJSON reads a missing file as the zero value. FetchURL refuses any answer
// but 200, and names GitHub's limit of requests when that is the reason.
// InstallRelease refuses the running version and a release with no file for
// this platform or no line in SHA256SUMS. ListReleases leaves out drafts,
// prereleases and releases before the first of this app. A running mark or a
// log that cannot be read counts as a clean shutdown and an empty log.
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Service is the Wails service for the app itself.
type Service struct {
	Config *ConfigFile
	Build  string

	mainClosable atomic.Bool
	source       *releaseSource

	// updating makes checks and installs of updates run one at a time.
	updating sync.Mutex
}

// The events the app sends. Each carries a string, such as the section or the
// window's name.
const (
	EventConfigChanged = "config:changed"
	EventWindowClosing = "window:closing"
	EventWindowClosed  = "window:closed"
)

// DevBuild is the version of a build run from the source.
const DevBuild = "dev"

const mainWindow = "main"

var errRateLimited = errors.New("GitHub takes no more requests from this network for now. Try again within the hour")

func init() {
	application.RegisterEvent[string](EventConfigChanged)
	application.RegisterEvent[string](EventWindowClosing)
	application.RegisterEvent[string](EventWindowClosed)
}

// ServiceStartup sets up the updater, empties the trash and writes the running
// mark.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := s.startUpdater(); err != nil {
		return err
	}

	_ = os.RemoveAll(s.Config.Dirs.Trash)

	// A mark still there means the last run never shut down.
	mark := resolveMark(s.Config.Dirs)
	_, err := os.Stat(mark)

	return os.WriteFile(mark, []byte(strconv.FormatBool(err == nil)), 0o644)
}

// ServiceShutdown removes the running mark.
func (s *Service) ServiceShutdown() error { return os.Remove(resolveMark(s.Config.Dirs)) }

// OpenConfig finds PMT's folders and loads config.json.
func OpenConfig() (*ConfigFile, error) {
	dirs, err := ResolveDirs()
	if err != nil {
		return nil, err
	}

	return openConfig(dirs), nil
}

// OpenLog opens pmt.log and returns the logger the services write through. A
// log over 4 MB starts over.
func OpenLog(config *ConfigFile, version string) (*slog.Logger, error) {
	path := resolveLog(config.Dirs)
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
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
func (s *Service) Version() string { return s.Build }

const userAgent = "paradox-modding-tools (+https://github.com/idodavis/paradox-modding-tools)"

// FetchURL downloads a web address and returns the body. It returns an error
// for any answer but 200.
func FetchURL(address string, timeout time.Duration) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}

	request.Header.Set("User-Agent", userAgent)
	// Some wikis answer a request with no Accept header with a challenge page.
	request.Header.Set("Accept", "*/*")

	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		return nil, err
	}

	defer func() { _ = response.Body.Close() }()

	switch {
	case response.StatusCode == http.StatusOK:
	case response.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, errRateLimited
	default:
		return nil, fmt.Errorf("%s answered %s", request.URL.Host, response.Status)
	}

	return io.ReadAll(response.Body)
}

// ListSettings returns the catalog of settings.
func (s *Service) ListSettings() []Setting { return Catalog }

// ReadSettings returns every setting's value by key, using the catalog's
// default for one never set.
func (s *Service) ReadSettings(workspaceID string) map[string]string {
	values := make(map[string]string, len(Catalog))
	for _, setting := range Catalog {
		value, _ := s.Config.LookupSetting(workspaceID, setting.Key)
		values[setting.Key] = value
	}

	return values
}

// WriteSetting sets one setting, in the workspace's record if it is kept per
// workspace. It returns an error if the setting or the workspace is unknown.
func (s *Service) WriteSetting(workspaceID, key, value string) error {
	row := slices.IndexFunc(Catalog, func(setting Setting) bool { return setting.Key == key })
	if row < 0 {
		return fmt.Errorf("the catalog has no setting %q", key)
	}
	setting := Catalog[row]

	return s.Config.Update("settings", func(config *Config) error {
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

// CrashedLastRun reports whether the last run ended without a clean shutdown.
// It is always false in a dev build.
func (s *Service) CrashedLastRun() bool {
	if s.Build == DevBuild {
		return false
	}

	mark, err := os.ReadFile(resolveMark(s.Config.Dirs))
	if err != nil {
		return false
	}

	crashed, _ := strconv.ParseBool(string(mark))

	return crashed
}

// ConfigFault returns why config.json could not be read, or "" if it could.
func (s *Service) ConfigFault() string {
	if s.Config.fault == nil {
		return ""
	}

	return s.Config.fault.Error()
}

// BuildDiagnostics returns what a bug report needs: the version, the system,
// the installs and the end of the log.
func (s *Service) BuildDiagnostics() string {
	var report strings.Builder
	system := runtime.GOOS + "/" + runtime.GOARCH
	env := application.Get().Env.Info()
	if env.OSInfo != nil {
		// Windows 11 still names itself Windows 10 in the registry.
		system = fmt.Sprintf("%s %s, %s", cmp.Or(env.OSInfo.Branding, env.OSInfo.Name), env.OSInfo.Version, system)
	}
	if webview, ok := env.PlatformInfo["WebView2"]; ok {
		system += fmt.Sprintf(", WebView2 %v", webview)
	}

	fmt.Fprintf(&report, "Paradox Modding Tools %s\nSystem: %s\n", s.Build, system)
	for _, install := range s.Config.Config().Installs {
		fmt.Fprintf(&report, "Install: %s, version %s\n", install.Game, install.Version)
	}

	text, _ := os.ReadFile(resolveLog(s.Config.Dirs))
	lines := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	fmt.Fprintf(&report, "\npmt.log, its last 200 lines:\n%s\n", strings.Join(lines[max(0, len(lines)-200):], "\n"))

	return report.String()
}

// ShowFolder opens a folder in the file manager, or a file's folder with the
// file selected.
func (s *Service) ShowFolder(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}

	return application.Get().Env.OpenFileManager(path, !stat.IsDir())
}

// OpenMainWindow opens the main window as it was last closed. Closing it waits
// for the page's answer, then saves the window's state and quits the app.
//
//wails:ignore
func (s *Service) OpenMainWindow() {
	pmt := application.Get()
	lastWindow := s.Config.Config().Window
	startState := application.WindowStateNormal
	if lastWindow.Maximized {
		startState = application.WindowStateMaximised
	}

	window := newWindow(application.WebviewWindowOptions{
		Name:       mainWindow,
		Title:      "Paradox Modding Tools",
		Width:      cmp.Or(lastWindow.Width, 1400),
		Height:     cmp.Or(lastWindow.Height, 900),
		StartState: startState,
		URL:        "/",
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !s.mainClosable.Load() {
			event.Cancel()
			pmt.Event.Emit(EventWindowClosing, mainWindow)
		}
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		maximized, bounds := window.IsMaximised(), window.Bounds()
		_ = s.Config.Update("window", func(config *Config) error {
			config.Window.Maximized = maximized
			if !maximized {
				config.Window.Width, config.Window.Height = bounds.Width, bounds.Height
			}

			return nil
		})
		pmt.Quit()
	})
}

// OpenWindow opens a new window on a page of the app. It returns an error if a
// window of that name is already open.
func (s *Service) OpenWindow(name, url string) error {
	pmt := application.Get()
	if _, open := pmt.Window.Get(name); open {
		return fmt.Errorf("a window named %q is already open", name)
	}

	window := newWindow(application.WebviewWindowOptions{Name: name, URL: url, Width: 1200, Height: 800})

	// The first close waits for the frontend, and the next one goes through.
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
func (s *Service) ShowWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}

	window.Focus()

	return nil
}

// CloseWindow closes a window. One from OpenWindow first sends window:closing,
// and the main window's close quits the app.
func (s *Service) CloseWindow(name string) error {
	window, err := lookupWindow(name)
	if err != nil {
		return err
	}

	if name == mainWindow {
		s.mainClosable.Store(true)
	}
	window.Close()

	return nil
}

func resolveMark(dirs Dirs) string { return filepath.Join(dirs.Log, "running") }

func resolveLog(dirs Dirs) string { return filepath.Join(dirs.Log, "pmt.log") }

// newWindow opens a window with no system frame, since the page draws its own
// title bar.
func newWindow(options application.WebviewWindowOptions) *application.WebviewWindow {
	options.Frameless = true

	return application.Get().Window.NewWithOptions(options)
}

func lookupWindow(name string) (application.Window, error) {
	window, open := application.Get().Window.Get(name)
	if !open {
		return nil, fmt.Errorf("no window is named %q", name)
	}

	return window, nil
}
