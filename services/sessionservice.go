// sessionservice.go owns the workspaces the editor has open: the pool of live sessions, the scan of
// an install, and the health strip a page shows.
// The events the frontend listens for are declared here too: a session became ready, a scan moved,
// a cache was replaced, a mod's calendar or file changed, a wiki was refreshed.

package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/shipped"
	"paradox-modding-tools/services/internal/session"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"
)

// SessionService is the Wails RPC shell for sessions.
type SessionService struct {
	App  *AppService
	Scan func(ctx context.Context, req index.Request) (*index.Install, error) // index.Scan, or a stand-in under test

	once     sync.Once
	smu      sync.RWMutex
	sessions map[string]*session.Session
	sf       singleflight.Group
	scanned  sync.Map // install id → time.Time: when its install was built, as this run last scanned or loaded it
}

// LanguageHealth is what the health strip shows: whether the session is live, and whose dumps the
// live schema read.
type LanguageHealth struct {
	WorkspaceID string `json:"workspaceId"`
	InstallID   string `json:"installId"`
	GameID      string `json:"gameId"`
	InstallOk   bool   `json:"installOk"`
	GameVersion string `json:"gameVersion"`
	ScannedAt   string `json:"scannedAt,omitempty"`   // when the install was built, else its cache file written
	UpdatedFrom string `json:"updatedFrom,omitempty"` // the version this run's scan replaced, where it differs
	Ready       bool   `json:"ready"`
	WatchError  string `json:"watchError,omitempty"`

	DumpsFrom        string   `json:"dumpsFrom"`        // whose dumps the schema read: yours, shipped or none
	DumpsStale       bool     `json:"dumpsStale"`       // they predate the install, or are another game version's
	DumpFolders      []string `json:"dumpFolders"`      // where the game's script_docs writes them
	DataTypesCommand string   `json:"dataTypesCommand"` // the console command that writes the data types
}

// start makes the scanner and the live sessions on first use.
func (s *SessionService) start() {
	s.once.Do(func() {
		if s.Scan == nil {
			s.Scan = index.Scan
		}
		s.sessions = map[string]*session.Session{}
	})
}

// EnsureSession is the live session for a workspace, built once, and its health.
func (s *SessionService) EnsureSession(workspaceID string) (*LanguageHealth, error) {
	if _, err := s.live(workspaceID); err != nil {
		return nil, err
	}
	return s.GetLanguageHealth(workspaceID)
}

// live is the workspace's session, built once however many callers ask at once, with every other
// workspace's session closed first so only one is ever live.
func (s *SessionService) live(workspaceID string) (*session.Session, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	s.start()
	if built := s.get(workspaceID); built != nil {
		return built, nil
	}
	s.dropAllExcept(workspaceID)

	made, err, _ := s.sf.Do(workspaceID, func() (any, error) {
		if built := s.get(workspaceID); built != nil {
			return built, nil
		}
		built, err := s.build(workspaceID)
		if err != nil {
			emit(EventLangFailed, workspaceID)
			return nil, err
		}
		s.smu.Lock()
		s.sessions[workspaceID] = built
		s.smu.Unlock()
		emit(EventLangReady, workspaceID)
		return built, nil
	})
	if err != nil {
		return nil, err
	}
	return made.(*session.Session), nil
}

// get is a workspace's live session, or nil where none is built.
func (s *SessionService) get(workspaceID string) *session.Session {
	s.smu.RLock()
	defer s.smu.RUnlock()
	return s.sessions[workspaceID]
}

// drop closes a workspace's session, so the next request builds it again with whatever changed.
func (s *SessionService) drop(workspaceID string) {
	s.smu.Lock()
	built := s.sessions[workspaceID]
	delete(s.sessions, workspaceID)
	s.smu.Unlock()
	if built != nil {
		built.Close()
	}
}

// dropAllExcept closes every live session but one; "" keeps none. Only one workspace is live at a
// time, because an install's index is most of a session's memory.
func (s *SessionService) dropAllExcept(keep string) {
	s.smu.Lock()
	var closing []*session.Session
	for id, built := range s.sessions {
		if id != keep {
			closing = append(closing, built)
			delete(s.sessions, id)
		}
	}
	s.smu.Unlock()
	for _, built := range closing {
		built.Close()
	}
}

// build makes a workspace's session: the install from its cache, scanned if none, and its mods.
func (s *SessionService) build(workspaceID string) (*session.Session, error) {
	ws, inst := s.App.workspace(workspaceID)
	if inst == nil {
		return nil, fmt.Errorf("workspace %s has no install", workspaceID)
	}
	install, err := s.scan(inst, ws.DefaultLocLang)
	if err != nil {
		return nil, err
	}

	// the mods are read after the install's scan, which ended at 100; the strip names the stage
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 100, Stage: "reading your mods"})
	var mods []session.Mod
	for _, mod := range ws.Mods {
		mods = append(mods, session.Mod{ID: mod.ID, Dir: mod.Path, Calendar: mod.Eras})
	}
	sess, err := session.New(context.Background(), game.Get(inst.GameID), install, mods, ws.DefaultLocLang)
	if err != nil {
		return nil, err
	}
	sess.Watch(func(path string) {
		_, err := os.Stat(path)
		emit(EventFSChanged, FSChanged{Path: path, Deleted: err != nil})
	})
	return sess, nil
}

// request is the scan of an install in a language: the version read from the install now, so a game
// update is another cache file, and the app's own version.
func (s *SessionService) request(inst *GameInstall, language string) index.Request {
	return index.Request{
		Game: inst.GameID, Dir: inst.Path, Version: game.ReadGameVersion(inst.Path, game.Get(inst.GameID).SteamAppID),
		Language: language, App: s.App.Version,
	}
}

// scan gives an install's schema and index, from its cache or else read from the install, and tells
// the frontend as it goes. The cache is the scan's own, keyed by the language too.
func (s *SessionService) scan(inst *GameInstall, language string) (*index.Install, error) {
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 0, Stage: "scanning"})
	request := s.request(inst, language)
	slog.Info("scan start", "game", inst.GameID, "install", inst.Name, "version", request.Version)
	started := time.Now()

	// each stage has the part of the bar its share of a scan's time earns, and the name the bar
	// shows; a move is told once, and a stage's time is logged when the next begins
	stages := map[string]struct {
		label    string
		from, to int
	}{
		"inventory": {"listing the install", 0, 6}, "localization": {"reading localization", 6, 7},
		"schema": {"reading the game's rules", 7, 44}, "index": {"reading definitions", 44, 56},
		"saving": {"saving", 56, 100},
	}
	told, running, since := -1, "", started
	lap := func(next string) {
		if running != "" {
			slog.Info("scan stage", "stage", running, "took", time.Since(since).Round(time.Millisecond))
		}
		running, since = next, time.Now()
	}
	progress := func(name string, done, total int) {
		if name != running {
			lap(name)
		}
		stage := stages[name]
		if percent := stage.from + (stage.to-stage.from)*done/max(total, 1); percent != told {
			told = percent
			emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: percent, Stage: stage.label})
		}
	}
	request.Progress = progress
	install, err := s.Scan(context.Background(), request)
	if install == nil {
		slog.Error("scan failed", "game", inst.GameID, "stage", running, "error", err)
		return nil, err
	}

	// a cache that could not be written still gives the install
	if err != nil {
		slog.Warn("cache not saved", "game", inst.GameID, "error", err)
	}
	lap("")
	s.scanned.Store(inst.ID, install.Scanned)
	slog.Info("scan end", "game", inst.GameID, "took", time.Since(started).Round(time.Millisecond))
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 100, Stage: "done"})
	emit(EventLangCacheUpdated, inst.ID)
	return install, nil
}

// scannedAt is when an install was built, in RFC 3339: as this run built or loaded it, else by the
// time of its cache file in the language; "" with neither.
func (s *SessionService) scannedAt(inst *GameInstall, language string) string {
	built, _ := s.scanned.Load(inst.ID)
	at, ran := built.(time.Time)
	if !ran {
		at = index.CachedAt(s.request(inst, language))
	}
	if at.IsZero() {
		return ""
	}
	return at.Format(time.RFC3339)
}

// GetLanguageHealth is the health strip for a workspace, from its live session where there is one.
func (s *SessionService) GetLanguageHealth(workspaceID string) (*LanguageHealth, error) {
	health := &LanguageHealth{WorkspaceID: workspaceID}
	ws, inst := s.App.workspace(workspaceID)
	if ws != nil {
		health.InstallID = ws.InstallID
	}
	if inst == nil {
		return health, nil
	}
	health.GameID, health.GameVersion = inst.GameID, inst.VersionDetected
	_, statErr := os.Stat(inst.Path)
	health.InstallOk = statErr == nil
	health.ScannedAt = s.scannedAt(inst, ws.DefaultLocLang)
	s.start()
	health.DataTypesCommand = game.Get(inst.GameID).DataTypesCommand
	health.DumpFolders = game.DumpDirs(inst.GameID, inst.Path)
	if live := s.get(workspaceID); live != nil {
		live.RLock()
		dumps := live.Install.Schema.Sources[schema.SourceDumps]
		health.Ready, health.DumpsFrom, health.DumpsStale = true, dumps.From, dumps.Stale

		// the version the scan read, and the one it replaced where the game was updated since
		health.GameVersion = live.Install.Schema.Version
		if previous := live.Install.Previous; previous != "" && previous != shipped.Safe(health.GameVersion) {
			health.UpdatedFrom = previous
		}
		if err := live.WatchError(); err != nil {
			health.WatchError = err.Error()
		}
		live.RUnlock()
	}
	return health, nil
}

const (
	EventLangReady           = "lang:ready"            // a workspace's session is built: data is its id
	EventLangFailed          = "lang:failed"           // a workspace's session could not be built: data is its id
	EventLangScanProgress    = "lang:scan-progress"    // an install scan moved: data is ScanProgress
	EventLangCacheUpdated    = "lang:cache-updated"    // an install was scanned or loaded from its cache: data is its id
	EventLangCalendarChanged = "lang:calendar-changed" // a mod's calendar changed: data is the workspace id
	EventFSChanged           = "fs:changed"            // a file under a mod changed on disk: data is FSChanged
	EventWikiUpdated         = "wiki:updated"          // a game's wiki pages were refreshed: data is WikiUpdated
	EventSearchHits          = "search:hits"           // a text search is done with a file: data is TextSearchHits
)

// FSChanged is one path under a mod that changed on disk; Deleted when it is gone.
type FSChanged struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// WikiUpdated is one game's wiki refresh, done; Error says why it failed.
type WikiUpdated struct {
	GameID string `json:"gameId"`
	Error  string `json:"error,omitempty"`
}

// ScanProgress is one step of an install scan.
type ScanProgress struct {
	InstallID string `json:"installId"`
	Percent   int    `json:"percent"`
	Stage     string `json:"stage"`
}

// emit sends an event to the frontend when the app is running; the tests run with no app.
func emit(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}
