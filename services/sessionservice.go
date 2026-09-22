// sessionservice.go builds and keeps a workspace's session: the install's cache loaded or scanned, the mods over it, one live session per workspace in a pool. One install scan runs at a time: a manual scan runs now, an automatic one queues behind whatever is running and the queue holds each install once, so a scan asked for is never dropped (three workspaces once wanted a rescan and only the first got one).
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
	"paradox-modding-tools/services/internal/lang/session"
)

// SessionService is the Wails RPC shell for sessions.
type SessionService struct {
	Store *Store
	Scan  func(ctx context.Context, req index.Request) (*index.Install, error) // index.Scan, or a stand-in under test

	once     sync.Once
	sessions *session.Pool
	slot     chan struct{} // one scan at a time
	mu       sync.Mutex    // the queue
	queue    []string
}

// LanguageHealth is what the health strip shows: whether the session is live, what the install's cache holds, and whether the dumps are there.
type LanguageHealth struct {
	WorkspaceID string       `json:"workspaceId"`
	InstallID   string       `json:"installId"`
	GameID      string       `json:"gameId"`
	InstallOk   bool         `json:"installOk"`
	GameVersion string       `json:"gameVersion"`
	CacheStale  bool         `json:"cacheStale"` // no cache for the install, or one of another format
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

// DumpHealth is one folder the game's script_docs dumps may be in, and what the reader found there.
type DumpHealth struct {
	Folder   string `json:"folder"`
	Effects  int    `json:"effects"`
	Triggers int    `json:"triggers"`
	Links    int    `json:"links"`
	Scopes   int    `json:"scopes"` // the scope types: in Vic3 and EU5 event_scopes.log is written to logs/ while the other five dumps go to docs/
}

// pool is the one pool, made on first use.
func (s *SessionService) pool() *session.Pool {
	s.once.Do(func() {
		if s.Scan == nil {
			s.Scan = index.Scan
		}
		s.sessions = session.NewPool(s.build, emitLang)
		s.slot = make(chan struct{}, 1)
	})
	return s.sessions
}

// EnsureSession is the live session for a workspace, built once, and its health.
func (s *SessionService) EnsureSession(workspaceID string) (*LanguageHealth, error) {
	if _, err := s.pool().EnsureSession(workspaceID); err != nil {
		return nil, err
	}
	return s.GetLanguageHealth(workspaceID)
}

// live is the workspace's session, built if it is not yet.
func (s *SessionService) live(workspaceID string) (*session.Session, error) {
	if _, err := s.pool().EnsureSession(workspaceID); err != nil {
		return nil, err
	}
	if sess := s.sessions.Get(workspaceID); sess != nil {
		return sess, nil
	}
	return nil, fmt.Errorf("no session for workspace %s", workspaceID)
}

// build makes a workspace's session: the install from its cache, scanned first where there is none, and the mods over it.
func (s *SessionService) build(workspaceID string) (*session.Session, error) {
	var ws *Workspace
	var inst *GameInstall
	s.Store.Read(func(c *Config) {
		if w := findWorkspace(c, workspaceID); w != nil {
			copied := *w
			ws = &copied
			if i := findInstall(c, w.InstallID); i != nil {
				copied := *i
				inst = &copied
			}
		}
	})
	if ws == nil || inst == nil {
		return nil, fmt.Errorf("workspace %s has no install", workspaceID)
	}
	base, err := index.CachePath(inst.ID, inst.Version)
	if err != nil {
		return nil, err
	}
	install, err := index.Load(base)
	if err != nil || predatesInstall(index.ScannedAt(base), inst.Path) {
		if install, err = s.scan(inst); err != nil {
			return nil, err
		}
	}
	var mods []session.Mod
	for _, m := range ws.Mods {
		mods = append(mods, session.Mod{ID: m.ID, Name: m.Name, Dir: m.Path, Calendar: session.Calendar{Era: m.Era, EpochOffset: m.EpochOffset}})
	}
	sess, err := session.New(context.Background(), game.Get(inst.GameID), install, mods, ws.DefaultLocLang)
	if err != nil {
		return nil, err
	}
	sess.Watch()
	return sess, nil
}

// scan reads an install, saves its cache, and tells the frontend as it goes. Saving is the last fifth.
func (s *SessionService) scan(inst *GameInstall) (*index.Install, error) {
	emitLang(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 0, Stage: "scanning"})
	install, err := s.Scan(context.Background(), index.Request{Game: inst.GameID, Dir: inst.Path, Version: inst.Version, Docs: dumpFolders(inst)})
	if err != nil {
		return nil, err
	}
	emitLang(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 80, Stage: "saving"})
	base, err := index.CachePath(inst.ID, inst.Version)
	if err != nil {
		return nil, err
	}
	if err := index.Save(base, time.Now().UTC().Format(time.RFC3339), install); err != nil {
		return nil, err
	}
	emitLang(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 100, Stage: "done"})
	emitLang(EventLangCacheUpdated, inst.ID)
	return install, nil
}

// predatesInstall reports a scan older than the game's binaries: the game was patched since, whatever its version string says (EU5 ships none). Nothing known either way is not stale, because guessing would rescan on every poll.
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

// RebuildInstallSemantics scans an install now, then drops every session over it so each is built again from the new cache. A scan already running is waited for.
func (s *SessionService) RebuildInstallSemantics(installID string) error {
	inst := s.install(installID)
	if inst == nil {
		return fmt.Errorf("install %s not found", installID)
	}
	s.pool()
	s.slot <- struct{}{}
	_, err := s.scan(inst)
	s.sessions.DropAll()
	go s.drain(s.finished(installID))
	return err
}

// QueueScan asks for a scan that nobody is waiting for: it runs now, or after the one running, once per install; its error is swallowed, because it was not asked for and the health strip already says the cache is stale.
func (s *SessionService) QueueScan(installID string) {
	s.mu.Lock()
	if !slices.Contains(s.queue, installID) {
		s.queue = append(s.queue, installID)
	}
	s.mu.Unlock()
	s.pool()
	select {
	case s.slot <- struct{}{}:
		go s.drain(installID)
	default:
	}
}

// drain runs a queued scan holding the slot, then the next the queue holds, in order, and frees the slot when the queue is empty.
func (s *SessionService) drain(installID string) {
	for installID != "" {
		if inst := s.install(installID); inst != nil {
			s.scan(inst)
		}
		installID = s.finished(installID)
	}
}

// finished takes a scanned install off the queue and hands back the next, "" for none, freeing the slot then.
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
	s.pool()
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.queue), len(s.slot) > 0
}

// install is a copy of one install's record.
func (s *SessionService) install(id string) *GameInstall {
	var inst *GameInstall
	s.Store.Read(func(c *Config) {
		if i := findInstall(c, id); i != nil {
			copied := *i
			inst = &copied
		}
	})
	return inst
}

// GetLanguageHealth is the health strip for a workspace, from its live session where there is one.
func (s *SessionService) GetLanguageHealth(workspaceID string) (*LanguageHealth, error) {
	h := &LanguageHealth{WorkspaceID: workspaceID}
	var inst *GameInstall
	s.Store.Read(func(c *Config) {
		if w := findWorkspace(c, workspaceID); w != nil {
			h.InstallID = w.InstallID
			if i := findInstall(c, w.InstallID); i != nil {
				copied := *i
				inst = &copied
			}
		}
	})
	if inst == nil {
		return h, nil
	}
	h.GameID, h.GameVersion = inst.GameID, inst.Version
	_, statErr := os.Stat(inst.Path)
	h.InstallOk = statErr == nil
	base, _ := index.CachePath(inst.ID, inst.Version)
	h.ScannedAt = index.ScannedAt(base)
	h.CacheStale = h.ScannedAt == "" || predatesInstall(h.ScannedAt, inst.Path)
	_, h.Scanning = s.Queued()
	for _, folder := range dumpFolders(inst) {
		v := schema.ReadVocab([]string{folder})
		h.Dumps = append(h.Dumps, DumpHealth{Folder: folder, Effects: len(v.Effects), Triggers: len(v.Triggers), Links: len(v.Links), Scopes: len(v.ScopeTypes)})
	}
	if live := s.pool().Get(workspaceID); live != nil {
		live.RLock()
		h.Ready, h.Defs, h.Refs, h.Kinds, h.Drift = true, len(live.Install.Index.Defs), len(live.Install.Index.Refs), len(live.Schema.Kinds), live.Schema.Drift
		if err := live.WatchError(); err != nil {
			h.WatchError = err.Error()
		}
		live.RUnlock()
	}
	return h, nil
}
