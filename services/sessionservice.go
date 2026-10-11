// sessionservice.go owns the workspaces the editor has open: the live session, the scan of an install, and the
// health strip a page shows. The events about them are declared here too: a session became ready, failed or was
// dropped, a scan moved, a cache was replaced, a file under a mod changed.

package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"
)

// SessionService owns the workspaces the editor has open: the one live session (opening a workspace closes the
// others, an install's index being most of a session's memory), the scan of an install, and the health strip.
type SessionService struct {
	config  *ConfigFile
	version string
	// scan is index.Scan; the tests set a stand-in
	scan func(ctx context.Context, request index.Request) (*index.Install, error)

	once         sync.Once
	mu           sync.RWMutex
	sessions     map[string]*workspace.Session
	fingerprints map[string]string // workspace id → what its live session was built from (fingerprintOf)
	building     singleflight.Group
	scanned      sync.Map // install id → time.Time: when its install was built, as this run last scanned or loaded it
}

// LanguageHealth is what the health strip shows: whether the session is live, and whose dumps the
// live schema read.
type LanguageHealth struct {
	InstallID   string `json:"installId"`
	InstallOk   bool   `json:"installOk"`
	GameVersion string `json:"gameVersion"`
	ScannedAt   string `json:"scannedAt,omitempty"`   // when the install was built, else its cache file written
	UpdatedFrom string `json:"updatedFrom,omitempty"` // the version this run's scan replaced, where it differs
	Ready       bool   `json:"ready"`

	DumpsFrom   string   `json:"dumpsFrom"`   // whose dumps the schema read: yours, shipped or none
	DumpsStale  bool     `json:"dumpsStale"`  // they predate the install, or are another game version's
	DumpFolders []string `json:"dumpFolders"` // where the game's script_docs writes them
}

// The events the frontend listens for about the sessions.
const (
	EventLangReady        = "lang:ready"         // a workspace's session is built: data is its id
	EventLangFailed       = "lang:failed"        // a workspace's session could not be built: data is its id
	EventLangDropped      = "lang:dropped"       // a workspace's session was closed: data is its id
	EventLangScanProgress = "lang:scan-progress" // an install scan moved: data is ScanProgress
	EventLangCacheUpdated = "lang:cache-updated" // an install was scanned or loaded from its cache: data is its id
	EventFSChanged        = "fs:changed"         // a file under a mod changed on disk: data is FSChanged
)

// registers every event so the bindings generator types them
func init() {
	application.RegisterEvent[string](EventLangReady)
	application.RegisterEvent[string](EventLangFailed)
	application.RegisterEvent[string](EventLangDropped)
	application.RegisterEvent[ScanProgress](EventLangScanProgress)
	application.RegisterEvent[string](EventLangCacheUpdated)
	application.RegisterEvent[FSChanged](EventFSChanged)
}

// ScanProgress is one step of an install scan.
type ScanProgress struct {
	InstallID string `json:"installId"`
	Percent   int    `json:"percent"`
	Stage     string `json:"stage"`
}

// FSChanged is one path under a mod that changed on disk; Deleted when it is gone.
type FSChanged struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// NewSessionService is the session service over the config, scanning with index.Scan.
func NewSessionService(config *ConfigFile, version string) *SessionService {
	return &SessionService{config: config, version: version, scan: index.Scan}
}

// start makes the live sessions' maps on first use.
func (s *SessionService) start() {
	s.once.Do(func() {
		s.sessions, s.fingerprints = map[string]*workspace.Session{}, map[string]string{}
	})
}

// EnsureSession is the live session for a workspace, built once, and its health.
func (s *SessionService) EnsureSession(workspaceID string) (*LanguageHealth, error) {
	if _, err := s.openSession(workspaceID); err != nil {
		return nil, err
	}
	return s.GetLanguageHealth(workspaceID), nil
}

// openSession is the workspace's session, built once however many callers ask at once, with every other
// workspace's session closed first so only one is ever live.
func (s *SessionService) openSession(workspaceID string) (*workspace.Session, error) {
	if workspaceID == "" {
		return nil, errors.New("workspace id is required")
	}
	s.start()
	if ws := s.cachedSession(workspaceID); ws != nil {
		return ws, nil
	}
	s.dropAllExcept(workspaceID)

	result, err, _ := s.building.Do(workspaceID, func() (any, error) {
		if ws := s.cachedSession(workspaceID); ws != nil {
			return ws, nil
		}
		fingerprint := s.fingerprintOf(workspaceID)
		ws, err := s.build(workspaceID)
		if err != nil {
			emit(EventLangFailed, workspaceID)
			return nil, err
		}
		s.mu.Lock()
		s.sessions[workspaceID], s.fingerprints[workspaceID] = ws, fingerprint
		s.mu.Unlock()
		emit(EventLangReady, workspaceID)
		return ws, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*workspace.Session), nil
}

// cachedSession is a workspace's live session, or nil where none is built or the config no longer says what it
// was built from (then it is closed, and the next request builds it again).
func (s *SessionService) cachedSession(workspaceID string) *workspace.Session {
	s.mu.RLock()
	ws, fingerprint, recorded := s.sessions[workspaceID], s.fingerprints[workspaceID], s.fingerprints[workspaceID] != ""
	s.mu.RUnlock()
	if ws != nil && recorded && fingerprint != s.fingerprintOf(workspaceID) {
		s.drop(workspaceID)
		return nil
	}
	return ws
}

// fingerprintOf is what a workspace's session is built from, as the config says now: its install and that
// install's folder, its language, and each mod's id, folder and calendar, in load order. A workspace edit that
// changes any of them needs no drop of its own: the next request finds the session stale and builds it again.
func (s *SessionService) fingerprintOf(workspaceID string) string {
	ws, inst, _ := s.config.workspace(workspaceID)
	if inst == nil {
		return ""
	}
	key := fmt.Sprintf("%s|%s|%s", inst.ID, inst.Path, ws.DefaultLocLang)
	for _, mod := range ws.Mods {
		key += fmt.Sprintf("|%s=%s %v", mod.ID, mod.Path, mod.Eras)
	}
	return key
}

// drop closes a workspace's session, so the next request builds it again with whatever changed.
func (s *SessionService) drop(workspaceID string) {
	s.mu.Lock()
	ws := s.sessions[workspaceID]
	delete(s.sessions, workspaceID)
	delete(s.fingerprints, workspaceID)
	s.mu.Unlock()
	if ws != nil {
		ws.Close()
		emit(EventLangDropped, workspaceID)
	}
}

// dropAllExcept closes every live session but one; "" keeps none. Only one workspace is live at a
// time, because an install's index is most of a session's memory.
func (s *SessionService) dropAllExcept(keep string) {
	s.mu.Lock()
	closing := map[string]*workspace.Session{}
	for id, ws := range s.sessions {
		if id != keep {
			closing[id] = ws
			delete(s.sessions, id)
			delete(s.fingerprints, id)
		}
	}
	s.mu.Unlock()
	for id, ws := range closing {
		ws.Close()
		emit(EventLangDropped, id)
	}
}

// build makes a workspace's session: the install from its cache, scanned if none, and its mods.
func (s *SessionService) build(workspaceID string) (*workspace.Session, error) {
	entry, inst, err := s.config.workspace(workspaceID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("workspace %s has no install", workspaceID)
	}
	info := game.Get(inst.GameID)
	if info == nil {
		return nil, fmt.Errorf("unknown game %s", inst.GameID)
	}
	install, err := s.scanInstall(inst, entry.DefaultLocLang)
	if err != nil {
		return nil, err
	}

	// the mods are read after the install's scan, which ended at 100; the strip names the stage
	emit(EventLangScanProgress, ScanProgress{InstallID: inst.ID, Percent: 100, Stage: "reading your mods"})
	var mods []workspace.Mod
	for _, mod := range entry.Mods {
		mods = append(mods, workspace.Mod{ID: mod.ID, Dir: mod.Path, Calendar: mod.Eras})
	}
	ws, err := workspace.Open(context.Background(), info, install, mods, entry.DefaultLocLang)
	if err != nil {
		return nil, err
	}
	// without a watcher the editor still works; it misses changes made outside it
	if err := ws.Watch(func(path string) {
		_, err := os.Stat(path)
		emit(EventFSChanged, FSChanged{Path: path, Deleted: err != nil})
	}); err != nil {
		slog.Warn("file watcher", "error", err)
	}
	return ws, nil
}

// request is the scan of an install in a language: the version read from the install now, so a game
// update is another cache file, and the app's own version.
func (s *SessionService) request(inst *GameInstall, language string) index.Request {
	return index.Request{
		Game: inst.GameID, Dir: inst.Path, Version: game.LoadGameVersion(inst.GameID, inst.Path),
		Language: language, App: s.version,
	}
}

// scanInstall gives an install's schema and index, from its cache or else read from the install, and tells
// the frontend as it goes. The cache is the scan's own, keyed by the language too.
func (s *SessionService) scanInstall(inst *GameInstall, language string) (*index.Install, error) {
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
	install, err := s.scan(context.Background(), request)
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
	scanTime, ran := built.(time.Time)
	if !ran {
		scanTime = index.CachedAt(s.request(inst, language))
	}
	if scanTime.IsZero() {
		return ""
	}
	return scanTime.Format(time.RFC3339)
}

// GetLanguageHealth is the health strip for a workspace, from its live session where there is one.
func (s *SessionService) GetLanguageHealth(workspaceID string) *LanguageHealth {
	health := &LanguageHealth{}
	entry, inst, _ := s.config.workspace(workspaceID)
	if entry != nil {
		health.InstallID = entry.InstallID
	}
	if inst == nil {
		return health
	}
	health.GameVersion = game.LoadGameVersion(inst.GameID, inst.Path)
	_, statErr := os.Stat(inst.Path)
	health.InstallOk = statErr == nil
	health.ScannedAt = s.scannedAt(inst, entry.DefaultLocLang)
	health.DumpFolders = game.DumpDirs(inst.GameID, inst.Path)
	if ws := s.cachedSession(workspaceID); ws != nil {
		ws.RLock()
		dumps := ws.Install.Schema.Sources[schema.SourceDumps]
		health.Ready, health.DumpsFrom, health.DumpsStale = true, dumps.Origin, dumps.Stale

		// the version the scan read, and the one it replaced where the game was updated since
		health.GameVersion = ws.Install.Schema.Version
		if previous := ws.Install.Previous; previous != "" && previous != appdata.SafeName(health.GameVersion) {
			health.UpdatedFrom = previous
		}
		ws.RUnlock()
	}
	return health
}
