package workspace

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
)

// A Watcher watches folders for files that change or are deleted. It reports the files of a burst of changes
// together.
type Watcher struct {
	watcher *fsnotify.Watcher
	done    chan struct{} // closed when the loop that reports the changes has ended
}

// startWatcher starts watching the given folders and any folder made under them. It calls the given function with
// the files that change or are deleted in each burst. A file saved twice in a burst is reported once. It returns an
// error if a folder cannot be watched.
func startWatcher(dirs []string, change func(changes []FileChange)) (*Watcher, error) {
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

// run reports the changes the watcher sees to the given function until the watcher is closed.
func (watcher *Watcher) run(change func(changes []FileChange)) {
	defer close(watcher.done)
	// the first event of a burst starts a timer that later events do not reset; when it ends, every path the burst
	// touched is reported together, and a file counts as deleted if it no longer exists then
	pending := map[string]bool{} // the paths the burst touched
	var batch <-chan time.Time   // ends the burst; nil while none is pending, and a nil channel never fires
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
			if len(pending) == 0 {
				batch = time.After(200 * time.Millisecond)
			}
			pending[event.Name] = true
		case _, ok := <-watcher.watcher.Errors:
			if !ok {
				return
			}
		case <-batch:
			changes := lo.Map(slices.Sorted(maps.Keys(pending)), func(path string, _ int) FileChange {
				_, err := os.Stat(path)
				return FileChange{Path: path, Deleted: errors.Is(err, fs.ErrNotExist)}
			})
			clear(pending)
			batch = nil
			change(changes)
		}
	}
}

// isHiddenFolder reports whether a path is a folder that nothing lists or watches.
func isHiddenFolder(path string) bool {
	return files.IsFolder(path) && files.IsHiddenDir(filepath.Base(path))
}

// Close stops the watcher. It returns once no report is under way.
func (watcher *Watcher) Close() {
	_ = watcher.watcher.Close()
	<-watcher.done
}
