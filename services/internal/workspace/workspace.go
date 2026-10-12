// Package workspace is one workspace open in the editor: the install's schema and index, the mods' index built
// over it, the files the editor has open, the watcher over the mods' folders, and one lock. It decides nothing
// about words: the schema's walker does, and a query resolves names across the two indexes in load order. open.go
// holds the one workspace open at a time, the Slot.
//
// Readers hold RLock for a whole request. The writers (DidChange, DidClose, DidSave, DiskChanged) take the lock
// themselves and are never called under a read lock.
package workspace

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Mod is one mod of the workspace, in the playset's order.
type Mod struct {
	ID, Dir  string
	Calendar Calendar
}

// buffer is a file open in the editor: its text and that text's parse, made once when the text arrives so a
// reader never writes.
type buffer struct {
	file   inventory.File
	text   string
	parsed jomini.Result
}

// changes numbers every build of and change to any session's records, so no value repeats.
var changes atomic.Int64

// Session is one workspace: the install, the mods over it, and the open files.
type Session struct {
	Changes   int64 // the number of the last build of or change to the workspace's records
	mu        sync.RWMutex
	Game      *game.GameInfo
	Install   *index.Install
	Workspace *index.Index // the mods' files, in the playset's order
	Mods      []Mod
	Language  string
	watcher   *Watcher
	buffers   map[string]*buffer // by lower-cased clean path
}

// Open builds the workspace's index over a scanned install.
func Open(ctx context.Context, info *game.GameInfo, install *index.Install, mods []Mod, language string) (*Session, error) {
	var scanned []index.Mod
	for _, mod := range mods {
		scanned = append(scanned, index.Mod{ID: mod.ID, Dir: mod.Dir})
	}
	modsIndex, err := index.ScanMods(ctx, install, scanned)
	if err != nil {
		return nil, err
	}
	return &Session{
		Changes: changes.Add(1), Game: info, Install: install, Workspace: modsIndex, Mods: mods,
		Language: cmp.Or(language, game.BaseLanguage), buffers: map[string]*buffer{},
	}, nil
}

// Indexes are the two indexes in load order: the install, then the workspace's mods over it.
func (ws *Session) Indexes() []*index.Index {
	return []*index.Index{ws.Install.Index, ws.Workspace}
}

// Winner is the definition the game keeps of several by the entry modes, its adds, the INJECT and REPLACE
// definitions with no target, and whether two mods contest the name.
func (ws *Session) Winner(defs []index.At) (winner index.At, adds, orphans []index.At, contested bool) {
	return index.Winner(ws.Game, defs, func(path string) string {
		// a path no mod lists is the install's
		if file, known := ws.Workspace.At(path); known {
			return file.Origin
		}
		return game.OriginVanilla
	})
}

// Text is a file's text: the open buffer's, else the disk's.
func (ws *Session) Text(path string) (string, error) {
	if buffer := ws.buffers[appdata.PathKey(path)]; buffer != nil {
		return buffer.text, nil
	}
	return inventory.File{Path: path}.Text()
}

// Parsed is the parse of a file's text: an open buffer's, made once per text, else the disk's.
func (ws *Session) Parsed(path string) (*jomini.Result, string, error) {
	if buffer := ws.buffers[appdata.PathKey(path)]; buffer != nil {
		return &buffer.parsed, buffer.text, nil
	}
	text, err := ws.Text(path)
	if err != nil {
		return nil, "", err
	}
	parsed := jomini.Parse(text)
	return &parsed, text, nil
}

// DidChange holds the editor's text of an opened or changed file and indexes it from that text, in place of the
// disk's records. The records keep the inventory's spelling of the path.
func (ws *Session) DidChange(path, text string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	key := appdata.PathKey(path)
	open := ws.buffers[key]
	if open == nil {
		file, _, _ := ws.Locate(path)
		open = &buffer{file: file}
		ws.buffers[key] = open
	}
	// the editor sends its text before each completion: the same text is not walked again
	if open.text != text || open.parsed.Root == nil {
		open.text, open.parsed = text, jomini.Parse(text)
		ws.reindex(open.file, text, nil)
	}
}

// DidClose forgets the editor's text; the file is read from disk again.
func (ws *Session) DidClose(path string) {
	text, err := inventory.File{Path: filepath.Clean(path)}.Text()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	key := appdata.PathKey(path)
	open := ws.buffers[key]
	if open == nil {
		return
	}
	delete(ws.buffers, key)
	ws.reindex(open.file, text, err)
}

// DidSave reads the saved file and indexes it again.
func (ws *Session) DidSave(path string) {
	// read before the lock: every editor request waits on this mutex, so disk reads stay outside it
	text, err := inventory.File{Path: path}.Text()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if file, _, known := ws.Locate(path); known {
		ws.reindex(file, text, err)
	}
}

// DiskChanged is a file changed on disk outside the editor: read again (outside the lock) only when no editor
// holds it and the index reads its format; any other file is only listed or forgotten. An unreadable file keeps
// its records, since on Windows a save often hits a sharing violation; only a file that is gone loses them.
func (ws *Session) DiskChanged(path string) {
	key := appdata.PathKey(path)
	ws.mu.RLock()
	file, _, known := ws.Locate(path)
	isOpen := ws.buffers[key] != nil
	ws.mu.RUnlock()
	if !known || isOpen {
		return
	}
	text, err := "", error(nil)
	if indexable(file.Format) {
		text, err = file.Text()
	} else {
		_, err = os.Stat(path)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.buffers[key] == nil {
		ws.reindex(file, text, err)
	}
}

// reindex walks a mod file again from text its caller has already read, under the lock; an install's file, or
// one under no root, is never indexed from the editor.
func (ws *Session) reindex(file inventory.File, text string, err error) {
	if file.Origin == game.OriginVanilla || file.Origin == "" {
		return
	}
	switch {
	case err == nil:
		ws.Workspace.Reindex(ws.Install.Schema, file, text)
	case errors.Is(err, os.ErrNotExist):
		ws.Workspace.Forget(file.Path)
	default:
		return
	}
	ws.Changes = changes.Add(1)
}

// indexable is whether the index reads a file of this format (its text, for records).
func indexable(format inventory.Format) bool {
	switch format {
	case inventory.Script, inventory.Loc, inventory.Table, inventory.List:
		return true
	}
	return false
}

// RLock is held by a reader for its whole request.
func (ws *Session) RLock() { ws.mu.RLock() }

// RUnlock ends a reader's request.
func (ws *Session) RUnlock() { ws.mu.RUnlock() }

// Watch reports every change under the mods' folders to the session, then to onChange, until Close.
func (ws *Session) Watch(onChange func(path string)) error {
	var dirs []string
	for _, mod := range ws.Mods {
		dirs = append(dirs, mod.Dir)
	}
	watcher, err := NewWatcher(dirs, func(path string) {
		ws.DiskChanged(path)
		onChange(path)
	})
	ws.watcher = watcher
	return err
}

// Close stops the session's watcher, if it has one.
func (ws *Session) Close() {
	if ws.watcher != nil {
		_ = ws.watcher.Close()
	}
}
