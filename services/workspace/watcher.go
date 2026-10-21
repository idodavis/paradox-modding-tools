package workspace

import "github.com/fsnotify/fsnotify"

// A Watcher watches the mods' folders and reports each changed or deleted file once, after a short quiet period.
type Watcher struct {
	watcher *fsnotify.Watcher
	done    chan struct{}
}

// startWatcher starts watching the given folders and calls the given function for each file that changes or is
// deleted, once per quiet period.
func startWatcher(dirs []string, change func(path string, deleted bool)) (*Watcher, error) {
	panic("stub")
}

// Close stops the watcher.
func (watcher *Watcher) Close() { panic("stub") }
