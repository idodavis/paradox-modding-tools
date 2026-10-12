// open.go is the one workspace open at a time. A request says what to open; the slot opens it (the install scanned
// or read from its cache, the mods indexed over it, the watcher started), keeps it until a request names another
// workspace or the same one built from other inputs, and closes it. It tells nothing itself: the request's callbacks
// and Closed carry what happened to whoever opened it.

package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
)

// Request is what a workspace is opened from: its id, the install's game, folder and language, the mods in load
// order, the app's version (the cache's key), the fingerprint of these inputs as the caller's records say them
// (another value builds again), and what to call: for a scan's examples, as the scan moves, when the install was
// scanned or read from its cache, and when a file under a mod changed on disk.
type Request struct {
	ID          string
	Game        string // the game's id in the registry
	Dir         string // the install's folder
	Language    string
	Mods        []Mod
	App         string
	Fingerprint string
	Wiki        func(databases []string) (map[string][]string, time.Time, error)
	Progress    func(percent int, stage string)
	Scanned     func()
	Changed     func(path string)
}

// Slot holds the one open workspace; an install's index is most of its memory, so opening another closes it. Scan
// is index.Scan, or a stand-in in tests; Closed, when set, is told the id of every workspace closed.
type Slot struct {
	Scan   func(ctx context.Context, request index.Request) (*index.Install, error)
	Closed func(id string)

	mu          sync.RWMutex
	session     *Session
	id          string
	fingerprint string
	building    singleflight.Group
	scanned     sync.Map // install folder → time.Time: when its install was built, as this run last scanned or loaded it
}

// Open is the workspace the request names, its index built once however many callers ask at once, the one open
// before it closed first so only one is ever open. built is true for the one caller whose call ran the build,
// succeeded or failed, so that caller alone tells of it.
func (s *Slot) Open(ctx context.Context, request Request) (ws *Session, built bool, err error) {
	if request.ID == "" {
		return nil, false, errors.New("workspace id is required")
	}
	if ws := s.Current(request.ID, request.Fingerprint); ws != nil {
		return ws, false, nil
	}
	result, err, _ := s.building.Do(request.ID, func() (any, error) {
		if ws := s.Current(request.ID, request.Fingerprint); ws != nil {
			return ws, nil
		}
		built = true
		s.Close("")
		ws, err := s.build(ctx, request)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.session, s.id, s.fingerprint = ws, request.ID, request.Fingerprint
		s.mu.Unlock()
		return ws, nil
	})
	if err != nil {
		return nil, built, err
	}
	return result.(*Session), built, nil
}

// Current is the open workspace when it is the one named and still built from the inputs the fingerprint names; a
// stale one is closed, and the next Open builds it again.
func (s *Slot) Current(id, fingerprint string) *Session {
	s.mu.RLock()
	ws, open, same := s.session, s.id == id, s.fingerprint == fingerprint
	s.mu.RUnlock()
	if ws == nil || !open {
		return nil
	}
	if !same {
		s.Close(id)
		return nil
	}
	return ws
}

// Close ends the open workspace when it is the one named, or whichever is open when "" is named: its watcher
// stops, and Closed hears its id.
func (s *Slot) Close(id string) {
	s.mu.Lock()
	ws, closing := s.session, s.id
	if ws == nil || (id != "" && closing != id) {
		s.mu.Unlock()
		return
	}
	s.session, s.id, s.fingerprint = nil, "", ""
	s.mu.Unlock()
	ws.Close()
	if s.Closed != nil {
		s.Closed(closing)
	}
}

// ScannedAt is when the request's install was built: as this run built or loaded it, else by the time of its cache
// file in the request's language; zero with neither.
func (s *Slot) ScannedAt(request Request) time.Time {
	if built, ran := s.scanned.Load(request.Dir); ran {
		return built.(time.Time)
	}
	return index.CachedAt(s.indexRequest(request))
}

// build makes the workspace: the install from its cache, scanned if none, and the mods indexed over it, watched.
func (s *Slot) build(ctx context.Context, request Request) (*Session, error) {
	info := game.Get(request.Game)
	if info == nil {
		return nil, fmt.Errorf("unknown game %s", request.Game)
	}
	install, err := s.scanInstall(ctx, request)
	if err != nil {
		return nil, err
	}
	// the mods are read after the install's scan, which ended at 100; the strip names the stage
	request.Progress(100, "reading your mods")
	ws, err := Open(ctx, info, install, request.Mods, request.Language)
	if err != nil {
		return nil, err
	}
	// without a watcher the editor still works; it misses changes made outside it
	if err := ws.Watch(request.Changed); err != nil {
		slog.Warn("file watcher", "error", err)
	}
	return ws, nil
}

// indexRequest is the scan of the request's install in its language: the version read from the install now, so a
// game update is another cache file, the app's own version, and the wiki's examples.
func (s *Slot) indexRequest(request Request) index.Request {
	return index.Request{
		Game: request.Game, Dir: request.Dir, Version: game.LoadGameVersion(request.Game, request.Dir),
		Language: request.Language, App: request.App, Wiki: request.Wiki,
	}
}

// scanInstall gives an install's schema and index, from its cache or else read from the install, telling the
// request as it goes. The cache is the scan's own, keyed by the language too.
func (s *Slot) scanInstall(ctx context.Context, request Request) (*index.Install, error) {
	request.Progress(0, "scanning")
	scan := s.indexRequest(request)
	slog.Info("scan start", "game", request.Game, "install", request.Dir, "version", scan.Version)
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
	last, running, since := -1, "", started
	lap := func(next string) {
		if running != "" {
			slog.Info("scan stage", "stage", running, "took", time.Since(since).Round(time.Millisecond))
		}
		running, since = next, time.Now()
	}
	scan.Progress = func(name string, done, total int) {
		if name != running {
			lap(name)
		}
		stage := stages[name]
		if percent := stage.from + (stage.to-stage.from)*done/max(total, 1); percent != last {
			last = percent
			request.Progress(percent, stage.label)
		}
	}
	install, err := s.Scan(ctx, scan)
	if install == nil {
		slog.Error("scan failed", "game", request.Game, "stage", running, "error", err)
		return nil, err
	}

	// a cache that could not be written still gives the install
	if err != nil {
		slog.Warn("cache not saved", "game", request.Game, "error", err)
	}
	lap("")
	s.scanned.Store(request.Dir, install.Scanned)
	slog.Info("scan end", "game", request.Game, "took", time.Since(started).Round(time.Millisecond))
	request.Progress(100, "done")
	request.Scanned()
	return install, nil
}
