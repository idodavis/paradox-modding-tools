// lifecycle.go owns the workspaceID -> *Session map and builds each session at most once
// even under concurrent callers (singleflight). It emits a ready event so the
// frontend refreshes without polling; the emitter and builder are injected so
// this package stays free of Wails and the workspace layout.
//
// It also holds the watcher, because both answer the same question: what keeps
// a session alive and current.

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sync/singleflight"

	"paradox-modding-tools/services/internal/source"
)

// Builder constructs a Session for a workspace id (reads mods, loads the cache).
type Builder func(workspaceID string) (*Session, error)

// Emitter publishes a lifecycle event to the frontend (e.g. "lang:ready").
type Emitter func(event string, data any)

// EventReady fires once a workspace session is built and live.
const EventReady = "lang:ready"

// Pool caches live sessions keyed by workspace id.
type Pool struct {
	build Builder
	emit  Emitter

	mu       sync.RWMutex
	sessions map[string]*Session
	sf       singleflight.Group
}

// NewPool creates a pool. emit may be nil (no events).
func NewPool(build Builder, emit Emitter) *Pool {
	return &Pool{build: build, emit: emit, sessions: map[string]*Session{}}
}

// EnsureSession returns the session for id, building it once if absent. Concurrent
// callers for the same id share a single build. Other live sessions are Dropped
// first so only one workspace is live.
func (p *Pool) EnsureSession(id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	p.mu.RLock()
	if s := p.sessions[id]; s != nil {
		p.mu.RUnlock()
		return s, nil
	}
	p.mu.RUnlock()
	p.dropOthers(id)

	v, err, _ := p.sf.Do(id, func() (any, error) {
		p.mu.RLock()
		if s := p.sessions[id]; s != nil {
			p.mu.RUnlock()
			return s, nil
		}
		p.mu.RUnlock()

		s, err := p.build(id)
		if err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.sessions[id] = s
		p.mu.Unlock()
		p.fire(EventReady, id)
		return s, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Session), nil
}

// Drop closes the session for id (if live) and removes it from the pool.
func (p *Pool) Drop(id string) {
	p.mu.Lock()
	s := p.sessions[id]
	delete(p.sessions, id)
	p.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

// DropAll closes every live session.
func (p *Pool) DropAll() {
	p.dropOthers("")
}

func (p *Pool) dropOthers(keep string) {
	p.mu.Lock()
	var drop []*Session
	for k, s := range p.sessions {
		if k != keep {
			drop = append(drop, s)
			delete(p.sessions, k)
		}
	}
	p.mu.Unlock()
	for _, s := range drop {
		s.Close()
	}
}

// Get returns the live session for id, or nil if none is built.
func (p *Pool) Get(id string) *Session {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sessions[id]
}

// fire emits an event if an emitter is configured.
func (p *Pool) fire(event, id string) {
	if p.emit != nil {
		p.emit(event, id)
	}
}

// Watch reports every change under the mods' folders to the session until Close.
func (s *Session) Watch() error {
	var dirs []string
	for _, m := range s.Mods {
		dirs = append(dirs, m.Dir)
	}
	w, err := NewWatcher(dirs, s.Changed)
	s.watcher = w
	return err
}

// Close stops the session's watcher, if it has one.
func (s *Session) Close() {
	if s.watcher != nil {
		s.watcher.Close()
	}
}

// watchDebounce is how long a path must be quiet before its change is reported;
// editors and git often emit several events per logical write.
const watchDebounce = 150 * time.Millisecond

// Watcher reports debounced changes to indexable files under a set of roots.
type Watcher struct {
	fsw      *fsnotify.Watcher
	onChange func(path string)

	mu     sync.Mutex
	timers map[string]*time.Timer
	closed bool
	done   chan struct{}
	// err is the first Add or event failure (inotify limits, overflow); shown on the health strip.
	err error
}

// NewWatcher starts watching roots recursively, calling onChange(absPath) once a
// changed .txt/.gui/.yml file settles. Close stops it.
func NewWatcher(roots []string, onChange func(path string)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{fsw: fsw, onChange: onChange, timers: map[string]*time.Timer{}, done: make(chan struct{})}
	for _, root := range roots {
		w.addTree(root)
	}
	go w.loop()
	return w, nil
}

// Close stops watching and releases resources. Pending debounce timers are
// stopped so onChange cannot fire after teardown.
func (w *Watcher) Close() error {
	close(w.done)
	w.mu.Lock()
	w.closed = true
	for name, t := range w.timers {
		t.Stop()
		delete(w.timers, name)
	}
	w.mu.Unlock()
	return w.fsw.Close()
}

// addTree adds root and every folder under it that holds a file to the watch set (fsnotify is not recursive on its own).
func (w *Watcher) addTree(root string) {
	seen := map[string]bool{}
	w.fail(w.fsw.Add(root))
	source.Walk(root, func(p string) {
		if dir := filepath.Dir(p); !seen[dir] {
			seen[dir] = true
			w.fail(w.fsw.Add(dir))
		}
	})
}

// fail records the first error; later ones add nothing a user can act on.
func (w *Watcher) fail(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.mu.Unlock()
}

// Err is the first error the watch hit, or nil while it is fully live.
func (w *Watcher) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// loop consumes fsnotify events until Close.
func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.fail(err)
		}
	}
}

// handle debounces a relevant event and, for new directories, extends the watch.
func (w *Watcher) handle(ev fsnotify.Event) {
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			w.addTree(ev.Name)
			return
		}
	}
	if !indexable(ev.Name) {
		return
	}
	w.mu.Lock()
	if t := w.timers[ev.Name]; t != nil {
		t.Stop()
	}
	name := ev.Name
	w.timers[name] = time.AfterFunc(watchDebounce, func() {
		w.mu.Lock()
		delete(w.timers, name)
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return
		}
		w.onChange(name)
	})
	w.mu.Unlock()
}

// indexable reports whether a path is a file the index reads.
func indexable(path string) bool {
	switch source.Classify(filepath.ToSlash(path)) {
	case source.Script, source.Loc, source.Table, source.List:
		return true
	}
	return false
}
