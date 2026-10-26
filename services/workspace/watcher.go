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

// A Watcher watches folders for files that change or are deleted.
type Watcher struct {
	watcher *fsnotify.Watcher
	done    chan struct{} // closed when run ends
}

// startWatcher watches folders and any folder made under them. It reports the files changed in each burst together.
// It returns an error if a folder cannot be watched.
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

// addFolders makes a notifier watch a folder and every folder under it but the hidden ones. A file adds nothing.
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

// run reports the changes the watcher sees until it is closed.
func (watcher *Watcher) run(change func(changes []FileChange)) {
	defer close(watcher.done)
	// a burst's first event starts a timer that later events do not reset. A file gone when it fires counts as deleted
	pending := map[string]bool{} // the paths the burst touched
	var batch <-chan time.Time   // fires when the burst ends; nil while none is pending
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
				// a folder made later is watched from its first event; one that cannot be watched is skipped
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

// Move runs a function that moves a file or a folder, and returns its error. It stops watching the folders being
// moved while the move runs, and watches them again if the move fails. A nil watcher only runs the move.
func (watcher *Watcher) Move(source string, move func() error) error {
	if watcher == nil {
		return move()
	}
	// Windows refuses to move a folder while a folder inside it is watched
	for _, dir := range watcher.watcher.WatchList() {
		if files.ToKey(dir) == files.ToKey(source) || files.IsUnder(source, dir) {
			_ = watcher.watcher.Remove(dir)
		}
	}
	err := move()
	if err != nil {
		// a folder that cannot be watched again is skipped
		_ = addFolders(watcher.watcher, source)
	}
	return err
}
