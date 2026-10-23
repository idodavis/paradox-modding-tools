package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"paradox-modding-tools/internal/files"
)

// A Watcher watches the mods' folders and reports each changed or deleted file once, after a short quiet period.
type Watcher struct {
	watcher *fsnotify.Watcher
	done    chan struct{} // closed when the loop that reports the changes has ended
}

// startWatcher starts watching the given folders and calls the given function for each file that changes or is
// deleted, once per quiet period.
func startWatcher(dirs []string, change func(path string, deleted bool)) (*Watcher, error) {
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := addFolders(notifier, dir); err != nil {
			return nil, errors.Join(err, notifier.Close())
		}
	}
	watcher := &Watcher{watcher: notifier, done: make(chan struct{})}
	go watcher.run(change)
	return watcher, nil
}

// addFolders makes a notifier watch a folder and every folder under it, except the hidden ones. A path that is not a
// folder adds nothing.
func addFolders(notifier *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case !entry.IsDir():
			return nil
		case path != dir && files.IsHiddenDir(entry.Name()):
			return filepath.SkipDir
		}
		return notifier.Add(path)
	})
}

// run takes the notifier's events until the notifier is closed. It holds back each path for 200 ms after its last
// event, so a save that writes twice is reported once, and a file counts as deleted when it no longer exists then.
func (watcher *Watcher) run(change func(path string, deleted bool)) {
	timers := map[string]*time.Timer{}
	quiet := make(chan string)
	defer close(watcher.done)
	defer func() {
		for _, timer := range timers {
			timer.Stop()
		}
	}()
	for {
		select {
		case event, ok := <-watcher.watcher.Events:
			if !ok {
				return
			}
			if event.Op == fsnotify.Chmod || isHiddenFolder(event.Name) {
				continue
			}
			if event.Has(fsnotify.Create) {
				// a folder made later is watched from its first event on; one that cannot be watched is left
				_ = addFolders(watcher.watcher, event.Name)
			}
			if timer, ok := timers[event.Name]; ok {
				timer.Stop()
			}
			timers[event.Name] = time.AfterFunc(200*time.Millisecond, func() {
				select {
				case quiet <- event.Name:
				case <-watcher.done:
				}
			})
		case _, ok := <-watcher.watcher.Errors:
			if !ok {
				return
			}
		case path := <-quiet:
			delete(timers, path)
			_, err := os.Stat(path)
			change(path, errors.Is(err, fs.ErrNotExist))
		}
	}
}

// isHiddenFolder reports whether a path is a folder that nothing lists or watches.
func isHiddenFolder(path string) bool {
	return files.IsFolder(path) && files.IsHiddenDir(filepath.Base(path))
}

// Close stops the watcher.
func (watcher *Watcher) Close() {
	_ = watcher.watcher.Close()
	<-watcher.done
}
