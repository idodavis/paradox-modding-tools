// watcher.go reports debounced changes to the files under a set of roots, folders made later included.

package workspace

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"paradox-modding-tools/services/internal/inventory"
)

// watchDebounce is how long a path must be quiet before its change is reported;
// editors and git often emit several events per logical write.
const watchDebounce = 150 * time.Millisecond

// Watcher reports debounced changes to indexable files under a set of roots.
type Watcher struct {
	notifier *fsnotify.Watcher
	onChange func(path string)

	mu     sync.Mutex
	timers map[string]*time.Timer
	closed bool
	done   chan struct{}
}

// NewWatcher starts watching roots recursively, calling onChange once a changed indexable file
// settles. Close stops it.
func NewWatcher(roots []string, onChange func(path string)) (*Watcher, error) {
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watcher := &Watcher{notifier: notifier, onChange: onChange, timers: map[string]*time.Timer{}, done: make(chan struct{})}
	for _, root := range roots {
		watcher.addTree(root, nil)
	}
	go watcher.loop()
	return watcher, nil
}

// Close stops watching and releases resources. Pending debounce timers are
// stopped so onChange cannot fire after teardown.
func (watcher *Watcher) Close() error {
	close(watcher.done)
	watcher.mu.Lock()
	watcher.closed = true
	for name, timer := range watcher.timers {
		timer.Stop()
		delete(watcher.timers, name)
	}
	watcher.mu.Unlock()
	return watcher.notifier.Close()
}

// addTree watches root and every folder under it that holds a file, fsnotify not being recursive,
// and gives each file to onFile when it is not nil.
func (watcher *Watcher) addTree(root string, onFile func(path string)) {
	seen := map[string]bool{}
	warn(watcher.notifier.Add(root))
	_ = inventory.Walk(root, func(path string) error {
		if dir := filepath.Dir(path); !seen[dir] {
			seen[dir] = true
			warn(watcher.notifier.Add(dir))
		}
		if onFile != nil {
			onFile(path)
		}
		return nil
	})
}

// warn logs a watch failure (inotify limits, overflow): the editor works on, missing outside changes.
func warn(err error) {
	if err != nil {
		slog.Warn("watcher failed", "error", err)
	}
}

// loop consumes fsnotify events until Close.
func (watcher *Watcher) loop() {
	for {
		select {
		case <-watcher.done:
			return
		case event, ok := <-watcher.notifier.Events:
			if !ok {
				return
			}
			watcher.handle(event)
		case err, ok := <-watcher.notifier.Errors:
			if !ok {
				return
			}
			warn(err)
		}
	}
}

// handle reports an event: a file of any kind (the explorer lists them all), and a folder made or moved in is
// watched and each of its files reported, as no event tells of them; a path removed or renamed away may be a folder
// whose files went with it.
func (watcher *Watcher) handle(event fsnotify.Event) {
	if event.Op == fsnotify.Chmod {
		return // only its attributes: nothing the editor or the explorer shows
	}
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			watcher.addTree(event.Name, watcher.report)
			return
		}
	}
	watcher.report(event.Name)
}

// report tells onChange of name once it has been quiet for watchDebounce.
func (watcher *Watcher) report(name string) {
	watcher.mu.Lock()
	if timer := watcher.timers[name]; timer != nil {
		timer.Stop()
	}
	watcher.timers[name] = time.AfterFunc(watchDebounce, func() {
		watcher.mu.Lock()
		delete(watcher.timers, name)
		closed := watcher.closed
		watcher.mu.Unlock()
		if closed {
			return
		}
		watcher.onChange(name)
	})
	watcher.mu.Unlock()
}
