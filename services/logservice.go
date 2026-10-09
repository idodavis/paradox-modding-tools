// logservice.go is the app's own log, pmt.log in the app's folder: what the app does, what the
// frontend reports, and what a crash prints, so a user can send it with a bug report. A release
// build has no console, so without it a crash leaves nothing behind.

package services

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// logRotateAt is the size past which a start after a clean run moves pmt.log aside; three older
// files are kept.
const logRotateAt = 2 << 20

// LogService owns pmt.log and the logger over it.
type LogService struct {
	App    *AppService
	Logger *slog.Logger

	file    *os.File
	version string
	crashed bool // the run before this one ended without writing its session end
}

// OpenLog opens pmt.log for this run. A log whose last session never ended (a crash, a kill, a
// second launch while the app runs) is not moved aside, so the next report keeps its end. Go's
// crash output and the standard log package write here too.
func OpenLog(version string) (*LogService, error) {
	dir, err := appdata.Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "pmt.log")

	crashed := false
	if raw, err := os.ReadFile(path); err == nil {
		crashed = strings.LastIndex(string(raw), `msg="session start"`) > strings.LastIndex(string(raw), `msg="session end"`)
	}
	if info, err := os.Stat(path); err == nil && !crashed && info.Size() > logRotateAt {
		for i := 3; i > 0; i-- {
			from := path
			if i > 1 {
				from = fmt.Sprintf("%s.%d", path, i-1)
			}
			os.Rename(from, fmt.Sprintf("%s.%d", path, i)) // an older file that is not there is nothing to move
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(file, nil))
	logger.Info("session start", "version", version)
	if err := debug.SetCrashOutput(file, debug.CrashOptions{}); err != nil {
		logger.Warn("crash output", "error", err)
	}
	slog.SetDefault(logger)
	return &LogService{Logger: logger, file: file, version: version, crashed: crashed}, nil
}

// Log writes a line the frontend reports; level is debug, info, warn or error.
func (s *LogService) Log(level, msg string, attrs map[string]string) {
	var severity slog.Level
	if err := severity.UnmarshalText([]byte(level)); err != nil {
		severity = slog.LevelInfo
	}
	args := []any{"from", "frontend"}
	for _, key := range slices.Sorted(maps.Keys(attrs)) {
		args = append(args, key, attrs[key])
	}
	s.Logger.Log(context.Background(), severity, msg, args...)
}

// LastRunCrashed reports whether the run before this one ended without closing.
func (s *LogService) LastRunCrashed() bool {
	return s.crashed
}

// CopyDiagnostics puts the report for a bug report on the clipboard.
func (s *LogService) CopyDiagnostics() error {
	if app := application.Get(); app == nil || !app.Clipboard.SetText(s.diagnostics()) {
		return errors.New("the clipboard took nothing")
	}
	return nil
}

// ServiceShutdown writes the session's end and closes the file.
func (s *LogService) ServiceShutdown() error {
	s.Logger.Info("session end")
	return s.file.Close()
}

// diagnostics is the report: the app's version, the OS, each install with its version, the last
// scan's stage times, and the log's last 200 lines.
func (s *LogService) diagnostics() string {
	var out strings.Builder
	fmt.Fprintf(&out, "Paradox Modding Tools %s\n", s.version)
	system := runtime.GOOS + "/" + runtime.GOARCH
	if app := application.Get(); app != nil {
		env := app.Env.Info()
		if env.OSInfo != nil {
			// Windows 11 still names itself Windows 10 in the registry; its branding says 11
			system = fmt.Sprintf("%s %s, %s", cmp.Or(env.OSInfo.Branding, env.OSInfo.Name), env.OSInfo.Version, system)
		}
		if webview, ok := env.PlatformInfo["WebView2"]; ok {
			system += fmt.Sprintf(", WebView2 %v", webview)
		}
	}
	fmt.Fprintf(&out, "OS: %s\n", system)
	var installs []GameInstall
	if s.App != nil {
		s.App.read(func(cfg *Config) { installs = slices.Clone(cfg.Installs) })
	}
	for _, install := range installs {
		fmt.Fprintf(&out, "Install: %s %s, version %s\n", install.GameID, install.Name, game.ReadGameVersion(install.GameID, install.Path))
	}

	raw, _ := os.ReadFile(s.file.Name())
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for i, line := range slices.Backward(lines) {
		if strings.Contains(line, `msg="scan start"`) {
			out.WriteString("\nLast scan:\n")
			for _, after := range lines[i:] {
				if strings.Contains(after, `msg="scan `) {
					fmt.Fprintf(&out, "%s\n", after)
				}
			}
			break
		}
	}
	fmt.Fprintf(&out, "\npmt.log, its last 200 lines:\n%s\n", strings.Join(lines[max(0, len(lines)-200):], "\n"))
	return out.String()
}
