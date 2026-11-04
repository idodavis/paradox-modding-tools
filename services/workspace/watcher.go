// watcher.go watches folders and reports the files that change or are deleted in them.

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
	notifier *fsnotify.Watcher
	done     chan struct{} // closed when run ends
}

// watchedFolders holds the dot folders the watcher watches: the ones coding agents keep their instructions in, and the
// Workshop listing's page.
var watchedFolders = []string{".claude", ".codex", ".github", ".workshop"}

// startWatcher watches folders and any folder made under them. It reports the files changed in each burst together. It
// returns an error if a folder cannot be watched.
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
	watcher := &Watcher{notifier: notifier, done: make(chan struct{})}
	go watcher.run(change)
	return watcher, nil
}

// addFolders makes a notifier watch a folder and every folder under it that isWatched accepts.
func addFolders(notifier *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case !entry.IsDir():
			return nil
		case path != dir && !isWatched(entry.Name()):
			return filepath.SkipDir
		}
		return notifier.Add(path)
	})
}

// isWatched reports whether the watcher watches a folder of the given name.
func isWatched(folder string) bool {
	return !files.IsHiddenDir(folder) || slices.Contains(watchedFolders, folder)
}

// run reports the changes the watcher sees until it is closed.
func (watcher *Watcher) run(change func(changes []FileChange)) {
	defer close(watcher.done)
	pending := map[string]bool{} // the paths the burst touched
	var batch <-chan time.Time   // fires when the burst ends; nil while none is pending
	for {
		select {
		case event, ok := <-watcher.notifier.Events:
			if !ok {
				return
			}
			if event.Op == fsnotify.Chmod || (files.IsFolder(event.Name) && !isWatched(filepath.Base(event.Name))) {
				continue
			}
			if event.Has(fsnotify.Create) {
				_ = addFolders(watcher.notifier, event.Name)
			}
			if len(pending) == 0 {
				batch = time.After(200 * time.Millisecond)
			}
			pending[event.Name] = true
		case _, ok := <-watcher.notifier.Errors:
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

// Close stops the watcher. It returns once no report is under way.
func (watcher *Watcher) Close() {
	_ = watcher.notifier.Close()
	<-watcher.done
}

// Move runs a function that moves a file or a folder, and returns its error. The folders being moved are not watched
// while it runs. A nil watcher only runs the move.
func (watcher *Watcher) Move(source string, move func() error) error {
	if watcher == nil {
		return move()
	}
	// Windows refuses to move a folder while a folder inside it is watched
	for _, dir := range watcher.notifier.WatchList() {
		if files.ToKey(dir) == files.ToKey(source) || files.IsUnder(source, dir) {
			_ = watcher.notifier.Remove(dir)
		}
	}
	err := move()
	if err != nil {
		_ = addFolders(watcher.notifier, source)
	}
	return err
}
