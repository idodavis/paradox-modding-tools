// sessionservice.go owns the workspaces the editor has open: the pool of live sessions, the scan of
// an install and the one slot that lets only one run at a time, and the health strip a page shows.
// The events the frontend listens for are declared here too, because this is what emits them: a
// session became ready, a scan moved, a cache was replaced, a mod's calendar changed.
package services

import (
	"context"
	"fmt"
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

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"
)

// SessionService is the Wails RPC shell for sessions.
type SessionService struct {
	Config *ConfigService
	Scan   func(ctx context.Context, req index.Request) (*index.Install, error) // index.Scan, or a stand-in under test

	emit func(event string, data any) // emitLang, or a recorder under test

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
	WorkspaceID string       `json:"workspaceId"`
	InstallID   string       `json:"installId"`
	GameID      string       `json:"gameId"`
	InstallOk   bool         `json:"installOk"`
	GameVersion string       `json:"gameVersion"`
	CacheStale  bool         `json:"cacheStale"` // no cache for the install, or one older than the game
	ScannedAt   string       `json:"scannedAt"`
	Scanning    bool         `json:"scanning"`
	Ready       bool         `json:"ready"`
	Defs        int          `json:"defs"`
	Refs        int          `json:"refs"`
	Kinds       int          `json:"kinds"`
	Dumps       []DumpHealth `json:"dumps"`
	Drift       schema.Drift `json:"drift"`
	WatchError  string       `json:"watchError,omitempty"`
}

// DumpHealth is one folder the game's script_docs dumps may be in, and what was read there.
type DumpHealth struct {
	Folder   string `json:"folder"`
	Effects  int    `json:"effects"`
	Triggers int    `json:"triggers"`
	Links    int    `json:"links"`
	Scopes   int    `json:"scopes"` // scope types; Vic3 and EU5 write event_scopes.log to logs/, other dumps to docs/
}

// start makes the scanner, the live sessions, the emitter and the one scan slot on first use.
func (s *SessionService) start() {
	s.once.Do(func() {
		if s.Scan == nil {
			s.Scan = index.Scan
		}
		if s.emit == nil {
			s.emit = emitLang
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
		mods = append(mods, session.Mod{ID: mod.ID, Name: mod.Name, Dir: mod.Path, Calendar: session.Calendar{Era: mod.Era, EpochOffset: mod.EpochOffset}})
	}
	sess, err := session.New(context.Background(), game.Get(inst.GameID), install, mods, ws.DefaultLocLang)
	if err != nil {
		return nil, err
	}
	sess.Watch()
	return sess, nil
}

// scan reads an install, saves its cache, and tells the frontend as it goes.
func (s *SessionService) scan(inst *GameInstall) (*index.Install, error) {
	emitLang(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 0, Stage: "scanning"})
	install, err := s.Scan(context.Background(), index.Request{Game: inst.GameID, Dir: inst.Path, Version: inst.Version, Docs: dumpFolders(inst)})
	if err != nil {
		return nil, err
	}
	emitLang(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 80, Stage: "saving"})
	if err := local.SaveInstall(inst.ID, inst.Version, install); err != nil {
		return nil, err
	}
	emitLang(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 100, Stage: "done"})
	emitLang(EventLangCacheUpdated, inst.ID)
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

// dumpFolders is where a game's script_docs dumps land.
func dumpFolders(inst *GameInstall) []string {
	user := game.UserDataDir(inst.GameID, inst.Path)
	return []string{filepath.Join(user, "logs"), filepath.Join(user, "docs")}
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
	for _, folder := range dumpFolders(inst) {
		vocab := schema.ReadVocab([]string{folder})
		health.Dumps = append(health.Dumps, DumpHealth{Folder: folder, Effects: len(vocab.Effects), Triggers: len(vocab.Triggers), Links: len(vocab.Links), Scopes: len(vocab.ScopeTypes)})
	}
	if live := s.get(workspaceID); live != nil {
		live.RLock()
		health.Ready, health.Defs, health.Refs, health.Kinds, health.Drift = true, len(live.Install.Index.Defs), len(live.Install.Index.Refs), len(live.Schema.Kinds), live.Schema.Drift
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
)

// ScanProgress is one step of an install scan.
type ScanProgress struct {
	InstallID string `json:"installId"`
	Percent   int    `json:"percent"`
	Stage     string `json:"stage"`
}

// emitLang sends an event to the frontend when the app is running; the tests run with no app.
func emitLang(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}
