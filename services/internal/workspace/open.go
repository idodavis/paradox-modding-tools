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

// Request is what a workspace is opened from.
type Request struct {
	ID          string // the workspace's id
	Game        string // the game's id in the registry
	Dir         string // the install's folder
	Language    string // the localization language beside english
	Mods        []Mod  // in load order
	App         string // the app's version, which keys the cache
	Fingerprint string // of these inputs as the caller's records say them; another value builds again

	// Wiki returns the wiki's script examples by database and its sidecar's time.
	Wiki func(databases []string) (map[string][]string, time.Time, error)
	// Progress hears the scan move.
	Progress func(percent int, stage string)
	// Scanned is called when the install was scanned or read from its cache.
	Scanned func()
	// Changed is called when a file under a mod changed on disk.
	Changed func(path string)
}

// Slot holds the one open workspace; an install's index is most of its memory, so opening another closes it.
type Slot struct {
	// Scan is index.Scan, or a stand-in in tests.
	Scan func(ctx context.Context, request index.Request) (*index.Install, error)
	// Closed is told the id of every workspace closed; may be nil.
	Closed func(id string)

	mu          sync.RWMutex
	session     *Session
	id          string             // the open workspace's id
	fingerprint string             // of the request it was opened from
	building    singleflight.Group // one build per workspace however many callers ask
	scanned     sync.Map           // install folder → when this run last scanned or loaded its install
}

// Open returns the workspace the request names, building its index once however many callers ask, after closing the
// one open before it; built is true for the one caller whose call ran the build, so it alone tells of it.
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
		// another id may have finished meanwhile: only one is ever open, so it closes now
		s.mu.Lock()
		before, beforeID := s.session, s.id
		s.session, s.id, s.fingerprint = ws, request.ID, request.Fingerprint
		s.mu.Unlock()
		s.release(before, beforeID)
		return ws, nil
	})
	if err != nil {
		return nil, built, err
	}
	return result.(*Session), built, nil
}

// Current returns the open workspace when it is the one named and still built from the inputs the fingerprint names; a
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
	s.release(ws, closing)
}

// release stops a workspace taken out of the slot and tells Closed its id; nil is no workspace.
func (s *Slot) release(ws *Session, id string) {
	if ws == nil {
		return
	}
	ws.Close()
	if s.Closed != nil {
		s.Closed(id)
	}
}

// ScannedAt returns when the request's install was built: as this run built or loaded it, else by its cache file's time
// in the request's language; zero with neither.
func (s *Slot) ScannedAt(request Request) time.Time {
	if scanTime, ok := s.scanned.Load(request.Dir); ok {
		return scanTime.(time.Time)
	}
	return index.CachedAt(s.indexRequest(request))
}

// build makes the workspace: the install from its cache, scanned if none, and the mods indexed over it, watched.
func (s *Slot) build(ctx context.Context, request Request) (*Session, error) {
	info := game.Lookup(request.Game)
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

// indexRequest returns the scan of the request's install in its language: the version read from the install now (so a
// game update is another cache file), the app's version and the wiki's examples.
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
