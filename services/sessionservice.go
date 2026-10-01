// sessionservice.go owns the workspaces the editor has open: the pool of live sessions, the scan of
// an install and the one slot that lets only one run at a time, and the health strip a page shows.
// The events the frontend listens for are declared here too: a session became ready, a scan moved,
// a cache was replaced, a mod's calendar or file changed, a wiki was refreshed.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/session"
	"paradox-modding-tools/services/internal/wiki"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"
)

// SessionService is the Wails RPC shell for sessions.
type SessionService struct {
	Config *ConfigService
	Scan   func(ctx context.Context, req index.Request) (*index.Install, error) // index.Scan, or a stand-in under test

	emit func(event string, data any) // emit, or a recorder under test

	once     sync.Once
	smu      sync.RWMutex
	sessions map[string]*session.Session
	sf       singleflight.Group
	slot     chan struct{} // one scan at a time
	mu       sync.Mutex    // the queue
	queue    []string
}

// LanguageHealth is what the health strip shows: whether the session is live, what the install's
// cache holds, and whether the dumps are there.
type LanguageHealth struct {
	WorkspaceID string        `json:"workspaceId"`
	InstallID   string        `json:"installId"`
	GameID      string        `json:"gameId"`
	InstallOk   bool          `json:"installOk"`
	GameVersion string        `json:"gameVersion"`
	CacheStale  bool          `json:"cacheStale"` // no cache for the install, or one older than the game
	ScannedAt   string        `json:"scannedAt"`
	Scanning    bool          `json:"scanning"`
	Ready       bool          `json:"ready"`
	Dumps       []DumpHealth  `json:"dumps"`
	Unread      schema.Unread `json:"unread"`
	WatchError  string        `json:"watchError,omitempty"`

	DumpsFrom        schema.DumpsFrom `json:"dumpsFrom"`              // whose each dump set the live schema read
	DumpsVersion     string           `json:"dumpsVersion,omitempty"` // the shipped dumps' game version, where used
	DataTypesCommand string           `json:"dataTypesCommand"`       // the console command that writes the data types
}

// DumpHealth is one folder the game's script_docs dumps may be in, and what was read there.
type DumpHealth struct {
	Folder       string `json:"folder"`
	Effects      int    `json:"effects"`
	Triggers     int    `json:"triggers"`
	EventTargets int    `json:"eventTargets"`
	Scopes       int    `json:"scopes"` // scope types; Vic3 and EU5 write event_scopes.log to logs/, other dumps to docs/
}

// start makes the scanner, the live sessions, the emitter and the one scan slot on first use.
func (s *SessionService) start() {
	s.once.Do(func() {
		if s.Scan == nil {
			s.Scan = index.Scan
		}
		if s.emit == nil {
			s.emit = emit
		}
		s.sessions = map[string]*session.Session{}
		s.slot = make(chan struct{}, 1)
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
			return nil, err
		}
		s.smu.Lock()
		s.sessions[workspaceID] = built
		s.smu.Unlock()
		s.emit(EventLangReady, workspaceID)
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
	var ws *Workspace
	var inst *GameInstall
	s.Config.read(func(cfg *Config) {
		if listed := findWorkspace(cfg, workspaceID); listed != nil {
			workspace := *listed
			ws = &workspace
			if record := findInstall(cfg, listed.InstallID); record != nil {
				copied := *record
				inst = &copied
			}
		}
	})
	if ws == nil || inst == nil {
		return nil, fmt.Errorf("workspace %s has no install", workspaceID)
	}
	install, err := local.LoadInstall(inst.ID, inst.Version)
	if err != nil || predatesInstall(local.ScannedAt(inst.ID, inst.Version), inst.Path) {
		if install, err = s.scan(inst); err != nil {
			return nil, err
		}
	}
	var mods []session.Mod
	for _, mod := range ws.Mods {
		mods = append(mods, session.Mod{ID: mod.ID, Name: mod.Name, Dir: mod.Path, Calendar: mod.Eras})
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

// scan reads an install, saves its cache, and tells the frontend as it goes.
func (s *SessionService) scan(inst *GameInstall) (*index.Install, error) {
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 0, Stage: "scanning"})
	slog.Info("scan start", "game", inst.GameID, "install", inst.Name, "version", inst.Version)
	started := time.Now()

	// each stage has the part of the bar its share of a CK3 scan's time earns; a move is told once,
	// and a stage's time is logged when the next begins
	spans := map[string][2]int{"reading localization": {0, 2}, "reading the install": {2, 43}, "reading definitions": {43, 78}, "reading references": {78, 95}}
	told, running, since := -1, "", started
	lap := func(next string) {
		if running != "" {
			slog.Info("scan stage", "stage", running, "took", time.Since(since).Round(time.Millisecond))
		}
		running, since = next, time.Now()
	}
	progress := func(stage string, done, total int) {
		if stage != running {
			lap(stage)
		}
		span := spans[stage]
		if percent := span[0] + (span[1]-span[0])*done/max(total, 1); percent != told {
			told = percent
			emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: percent, Stage: stage})
		}
	}
	sidecar, _ := wiki.SidecarPath(inst.GameID)
	request := index.Request{Game: inst.GameID, Dir: inst.Path, Version: inst.Version, Dumps: game.DumpDirs(inst.GameID, inst.Path), Wiki: sidecar, Progress: progress}
	install, err := s.Scan(context.Background(), request)
	if err != nil {
		slog.Error("scan failed", "game", inst.GameID, "stage", running, "error", err)
		return nil, err
	}
	lap("saving")
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 95, Stage: "saving"})
	if err := local.SaveInstall(inst.ID, inst.Version, install); err != nil {
		slog.Error("scan failed", "game", inst.GameID, "stage", running, "error", err)
		return nil, err
	}

	// the user's own dumps of both sets refresh the layers the owner may ship
	if from := install.Schema.DumpsFrom; from.ScriptDocs == "yours" && from.DataTypes == "yours" {
		shipped := schema.Shipped{GameVersion: inst.Version, Dumps: install.Schema.Dumps, Wiki: install.Schema.Wiki}
		if err := local.SaveShipped(inst.GameID, shipped); err != nil {
			slog.Warn("shipped layers not saved", "game", inst.GameID, "error", err)
		}
	}
	lap("")
	slog.Info("scan end", "game", inst.GameID, "took", time.Since(started).Round(time.Millisecond))
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 100, Stage: "done"})
	emit(EventLangCacheUpdated, inst.ID)
	return install, nil
}

// predatesInstall reports a scan older than the game's binaries: the game was patched since,
// whatever its version string says (EU5 ships none). Nothing known either way is not stale, because
// guessing would rescan on every poll.
func predatesInstall(scannedAt, installPath string) bool {
	scanned, err := time.Parse(time.RFC3339, scannedAt)
	if err != nil {
		return false
	}
	updated := game.InstallUpdatedAt(installPath)
	return !updated.IsZero() && updated.After(scanned)
}

// RebuildInstallSemantics scans an install now, then drops every session so each is built again
// from the new cache. A scan already running is waited for.
func (s *SessionService) RebuildInstallSemantics(installID string) error {
	inst := s.install(installID)
	if inst == nil {
		return fmt.Errorf("install %s not found", installID)
	}
	s.start()
	s.slot <- struct{}{}
	_, err := s.scan(inst)
	s.dropAllExcept("")
	go s.drain(s.finished(installID))
	return err
}

// QueueScan asks for a scan nobody waits for: it runs now, or after the one running, once per
// install. Its error is swallowed; the health strip already says the cache is stale.
func (s *SessionService) QueueScan(installID string) {
	s.mu.Lock()
	if !slices.Contains(s.queue, installID) {
		s.queue = append(s.queue, installID)
	}
	s.mu.Unlock()
	s.start()
	select {
	case s.slot <- struct{}{}:
		go s.drain(installID)
	default:
	}
}

// drain runs queued scans in order holding the slot, and frees it when the queue is empty.
func (s *SessionService) drain(installID string) {
	for installID != "" {
		if inst := s.install(installID); inst != nil {
			s.scan(inst)
		}
		installID = s.finished(installID)
	}
}

// finished takes a scanned install off the queue and returns the next; at "" it frees the slot.
func (s *SessionService) finished(installID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = slices.DeleteFunc(s.queue, func(id string) bool { return id == installID })
	if len(s.queue) == 0 {
		<-s.slot
		return ""
	}
	return s.queue[0]
}

// Queued is the installs waiting for a scan, and whether one runs.
func (s *SessionService) Queued() ([]string, bool) {
	s.start()
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.queue), len(s.slot) > 0
}

// install is a copy of one install's record.
func (s *SessionService) install(id string) *GameInstall {
	var inst *GameInstall
	s.Config.read(func(cfg *Config) {
		if found := findInstall(cfg, id); found != nil {
			copied := *found
			inst = &copied
		}
	})
	return inst
}

// GetLanguageHealth is the health strip for a workspace, from its live session where there is one.
func (s *SessionService) GetLanguageHealth(workspaceID string) (*LanguageHealth, error) {
	health := &LanguageHealth{WorkspaceID: workspaceID}
	var inst *GameInstall
	s.Config.read(func(cfg *Config) {
		if listed := findWorkspace(cfg, workspaceID); listed != nil {
			health.InstallID = listed.InstallID
			if found := findInstall(cfg, listed.InstallID); found != nil {
				copied := *found
				inst = &copied
			}
		}
	})
	if inst == nil {
		return health, nil
	}
	health.GameID, health.GameVersion = inst.GameID, inst.Version
	_, statErr := os.Stat(inst.Path)
	health.InstallOk = statErr == nil
	health.ScannedAt = local.ScannedAt(inst.ID, inst.Version)
	health.CacheStale = health.ScannedAt == "" || predatesInstall(health.ScannedAt, inst.Path)
	_, health.Scanning = s.Queued()
	user, info := game.UserDataDir(inst.GameID, inst.Path), game.Get(inst.GameID)
	health.DataTypesCommand = info.DataTypesCommand
	for _, dir := range info.ScriptDocsDirs {
		folder := filepath.Join(user, dir)
		dumps := schema.ReadDumps([]string{folder})
		health.Dumps = append(health.Dumps, DumpHealth{Folder: folder, Effects: len(dumps.Effects), Triggers: len(dumps.Triggers), EventTargets: len(dumps.EventTargets) + len(dumps.DataTargets), Scopes: len(dumps.Scopes)})
	}
	if live := s.get(workspaceID); live != nil {
		live.RLock()
		health.Ready, health.Unread, health.DumpsFrom, health.DumpsVersion = true, live.Schema.Unread, live.Schema.DumpsFrom, live.Schema.DumpsVersion
		if err := live.WatchError(); err != nil {
			health.WatchError = err.Error()
		}
		live.RUnlock()
	}
	return health, nil
}

const (
	EventLangReady           = "lang:ready"            // a workspace's session is built: data is its id
	EventLangScanProgress    = "lang:scan-progress"    // an install scan moved: data is ScanProgress
	EventLangCacheUpdated    = "lang:cache-updated"    // an install's cache was written: data is the install id
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
