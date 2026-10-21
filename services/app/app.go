package app

import (
	"context"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
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

// Service is the app's own service: the version and the updater, the settings catalogue and values, the data
// reset, the log, and the other windows. The frontend's own log lines go through Wails's log service.
type Service struct {
	Config *ConfigFile // config.json, which carries PMT's folders
	Build  string      // the version the build set
}

// ServiceStartup writes a mark saying the app is running, which CrashedLastRun reads on the next start.
func (app *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	panic("stub")
}

// ServiceShutdown removes the running mark, so the next start knows the shutdown was clean.
func (app *Service) ServiceShutdown() error { panic("stub") }

// An Update says which version was found and whether it is newer than this one. It is the data of the app:update
// event.
type Update struct {
	Version   string `json:"version"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
}

// OpenConfig finds PMT's folders on this machine and loads config.json.
func OpenConfig() (*ConfigFile, error) { panic("stub") }

// OpenLog opens pmt.log in the log folder, truncating it when it has grown too large, and returns the logger that
// Wails and the services write through.
func OpenLog(config *ConfigFile, version string) (*slog.Logger, error) { panic("stub") }

// Version returns the version the build set.
func (app *Service) Version() string { panic("stub") }

// CheckUpdate asks GitHub's releases whether a newer version exists. A release is verified by its SHA256SUMS.
func (app *Service) CheckUpdate() (Update, error) { panic("stub") }

// ApplyUpdate downloads the update beside the app and restarts into it.
func (app *Service) ApplyUpdate() error { panic("stub") }

// ListSettings returns the settings catalogue.
func (app *Service) ListSettings() []Setting { panic("stub") }

// ReadSettings returns every setting's value by key, using the catalogue's default for a setting that was never
// set.
func (app *Service) ReadSettings() map[string]string { panic("stub") }

// WriteSetting writes one setting and emits config:changed for the settings.
func (app *Service) WriteSetting(key, value string) error { panic("stub") }

// ResetData removes every workspace, install record and cache. Mods, game files and the settings stay.
func (app *Service) ResetData() error { panic("stub") }

// CrashedLastRun reports whether the last run ended without a clean shutdown.
func (app *Service) CrashedLastRun() bool { panic("stub") }

// LogPath returns the path of pmt.log.
func (app *Service) LogPath() string { panic("stub") }

// OpenWindow opens a new window with the given name at a URL of the app.
func (app *Service) OpenWindow(name, url string) error { panic("stub") }

// ShowWindow brings a window to the front.
func (app *Service) ShowWindow(name string) error { panic("stub") }

// CloseWindow closes a window. A window holding tabs gets a window:closing event first and closes when the
// frontend answers with ReleaseClose, or after five seconds.
func (app *Service) CloseWindow(name string) error { panic("stub") }

// ReleaseClose lets a close that CloseWindow is holding go ahead.
func (app *Service) ReleaseClose(name string) { panic("stub") }

// ListWindows returns the names of the open windows.
func (app *Service) ListWindows() []string { panic("stub") }
