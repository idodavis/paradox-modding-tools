// watcher.go watches folders for changed and deleted files.

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

	"paradox-modding-tools/internal/files"
)

// A Watcher watches folders for files that change or are deleted.
type Watcher struct {
	notifier *fsnotify.Watcher

	// done is closed when run ends.
	done chan struct{}
}

// watchedFolders are the dot folders the watcher watches: the ones coding
// agents and the Workshop listing use.
var watchedFolders = []string{".claude", ".codex", ".github", ".workshop"}

// startWatcher watches folders and any folder made under them, and reports the
// files changed in each burst together.
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

// addFolders makes a notifier watch a folder and the folders under it that
// isWatched accepts.
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

func isWatched(folder string) bool {
	return !files.IsHiddenDir(folder) || slices.Contains(watchedFolders, folder)
}

func (w *Watcher) run(change func(changes []FileChange)) {
	defer close(w.done)

	// batch fires when a burst of events ends, and is nil while none is pending.
	pending := map[string]bool{}
	var batch <-chan time.Time
	for {
		select {
		case event, ok := <-w.notifier.Events:
			if !ok {
				return
			}
			if event.Op == fsnotify.Chmod || (files.IsFolder(event.Name) && !isWatched(filepath.Base(event.Name))) {
				continue
			}

			// On Windows, fsnotify's Add waits for the reader that waits for this
			// goroutine, so a new folder is added off it.
			if event.Has(fsnotify.Create) {
				go func() { _ = addFolders(w.notifier, event.Name) }()
			}
			if len(pending) == 0 {
				batch = time.After(200 * time.Millisecond)
			}

			pending[event.Name] = true
		case _, ok := <-w.notifier.Errors:
			if !ok {
				return
			}
		case <-batch:
			paths := slices.Sorted(maps.Keys(pending))
			changes := make([]FileChange, len(paths))
			for position, path := range paths {
				_, err := os.Stat(path)
				changes[position] = FileChange{Path: path, Deleted: errors.Is(err, fs.ErrNotExist)}
			}

			clear(pending)
			batch = nil
			change(changes)
		}
	}
}

// Close stops the watcher and waits for a report under way.
func (w *Watcher) Close() {
	_ = w.notifier.Close()
	<-w.done
}

// Move runs a move of a file or a folder with the folders it moves unwatched. A
// nil watcher only runs the move.
func (w *Watcher) Move(source string, move func() error) error {
	if w == nil {
		return move()
	}

	// Windows refuses to move a folder while a folder inside it is watched.
	for _, dir := range w.notifier.WatchList() {
		if files.ToKey(dir) == files.ToKey(source) || files.IsUnder(source, dir) {
			_ = w.notifier.Remove(dir)
		}
	}

	err := move()
	if err != nil {
		_ = addFolders(w.notifier, source)
	}

	return err
}
