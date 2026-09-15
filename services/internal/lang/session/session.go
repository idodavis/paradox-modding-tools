// Package session holds the live state of one open workspace: the
// derived.Model built from the vanilla cache and the mods, plus the editor
// buffers layered over it. lsp and views read a Session under its RWMutex,
// one lock per request, and call the embedded Model directly — no query layer.
//
// session.go is the Session type, its buffers, and the write methods.
// lifecycle.go is the Pool (one live session per workspace) and the Watcher.
package session

import (
	"errors"
	"os"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// buffer is one open file: its current text and parse result.
type buffer struct {
	text   string
	result jomini.Result
}

// Session is one workspace's Model plus its editor buffers. Readers hold RLock for a whole request; the Did*/Replace*/Set* writers take Lock themselves.
type Session struct {
	WorkspaceID string
	GameID      string

	sync.RWMutex
	*derived.Model
	buffers map[string]*buffer
	watcher *Watcher
	onFS    func(path string, deleted bool)
}

// NewWithLoc is New with loc language and an optional vanilla loc sidecar.
func NewWithLoc(
	workspaceID, gameID, defaultLang string,
	cache *derived.VanillaCache, vloc *derived.VanillaLoc, mods []derived.ModInput,
) *Session {
	if defaultLang == "" {
		defaultLang = "english"
	}
	return &Session{
		WorkspaceID: workspaceID,
		GameID:      gameID,
		buffers:     map[string]*buffer{},
		Model:       derived.NewModel(gameID, defaultLang, cache, vloc, mods),
	}
}

// ReplaceCache swaps the vanilla Cache after a Rescan without dropping the session.
func (s *Session) ReplaceCache(c *derived.VanillaCache) {
	s.Lock()
	s.Model.ReplaceCache(c)
	s.Unlock()
}

// ReplaceVanillaLoc swaps the vanilla loc sidecar after a loc-only harvest.
func (s *Session) ReplaceVanillaLoc(loc *derived.VanillaLoc) {
	s.Lock()
	s.Model.ReplaceVanillaLoc(loc)
	s.Unlock()
}

// SetDefaultLang sets the workspace default loc language (empty → english).
func (s *Session) SetDefaultLang(lang string) {
	if lang == "" {
		lang = "english"
	}
	s.Lock()
	s.Model.SetDefaultLang(lang)
	s.Unlock()
}

// Result returns the parse result for an open file, or ok=false if not open.
func (s *Session) Result(path string) (jomini.Result, bool) {
	if b := s.buffers[derived.CanonPath(path)]; b != nil {
		return b.result, true
	}
	return jomini.Result{}, false
}

// FileText returns the buffer text if open, otherwise the on-disk decoded text.
func (s *Session) FileText(path string) string {
	if b := s.buffers[derived.CanonPath(path)]; b != nil {
		return b.text
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text, _ := jomini.Decode(raw)
	return text
}

// Parsed returns the buffer Result if open, else parses FileText.
func (s *Session) Parsed(path string) jomini.Result {
	src := s.FileText(path)
	if src == "" {
		return jomini.Result{}
	}
	if r, ok := s.Result(path); ok && r.Src == src {
		return r
	}
	if _, rel, ok := s.Locate(path); ok && game.MatchExtract(s.GameID, rel).Mode == game.ModeLocKey {
		return jomini.Result{Src: src}
	}
	return jomini.Parse(src)
}

// DidOpen records a freshly opened file's text and indexes it.
func (s *Session) DidOpen(path, text string) { s.edit(path, text) }

// DidChange records an in-editor edit and reindexes the file.
func (s *Session) DidChange(path, text string) { s.edit(path, text) }

// DidClose drops the open buffer; the last-indexed on-disk state is kept.
func (s *Session) DidClose(path string) {
	s.Lock()
	defer s.Unlock()
	delete(s.buffers, derived.CanonPath(path))
}

// DidSave reindexes from the open buffer if present, else from disk.
func (s *Session) DidSave(path string) {
	s.Lock()
	if b := s.buffers[derived.CanonPath(path)]; b != nil {
		s.Reindex(path, "", b.text, jomini.Result{})
		s.Unlock()
		return
	}
	s.Unlock()
	_ = s.reindexFromDisk(path)
}

// externalChange reindexes a file touched outside the editor (watcher-driven).
func (s *Session) externalChange(path string) {
	if s.reindexFromDisk(path) {
		s.Lock()
		s.DropPath(derived.CanonPath(path))
		s.Unlock()
		s.emitFS(path, true)
		return
	}
	s.emitFS(path, false)
}

func (s *Session) reindexFromDisk(path string) (deleted bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	text, _ := jomini.Decode(raw)
	s.Lock()
	s.Reindex(path, "", text, jomini.Result{})
	s.Unlock()
	return false
}

// edit updates the buffer (parsing once) and reindexes the file.
// The editor's spelling of the path is kept and only the buffer key is
// canonicalised. Everything harvested here carries the path through to
// Def.Path and out to go-to-definition, and the workbench keys its tabs by
// exact URI.
func (s *Session) edit(path, text string) {
	key := derived.CanonPath(path)
	text = jomini.Normalize(text)
	s.Lock()
	defer s.Unlock()
	_, rel, ok := s.Locate(path)
	if !ok {
		return
	}
	if game.MatchExtract(s.GameID, rel).Mode == game.ModeLocKey {
		s.buffers[key] = &buffer{text: text}
		s.Reindex(path, rel, text, jomini.Result{Src: text})
		return
	}
	result := jomini.Parse(text)
	s.buffers[key] = &buffer{text: text, result: result}
	s.Reindex(path, rel, text, result)
}

// SetFileNotify registers a callback fired after each external file change.
func (s *Session) SetFileNotify(fn func(path string, deleted bool)) {
	s.Lock()
	s.onFS = fn
	s.Unlock()
}

func (s *Session) emitFS(path string, deleted bool) {
	s.RLock()
	fn := s.onFS
	s.RUnlock()
	if fn != nil {
		fn(path, deleted)
	}
}

// StartWatch begins watching mod roots for external changes.
func (s *Session) StartWatch() error {
	s.RLock()
	roots := s.ModRoots()
	s.RUnlock()
	w, err := NewWatcher(roots, s.externalChange)
	if err != nil {
		return err
	}
	s.Lock()
	s.watcher = w
	s.Unlock()
	return nil
}

// Close stops the file watcher. Safe to call more than once.
func (s *Session) Close() {
	s.Lock()
	w := s.watcher
	s.watcher = nil
	s.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

// WatchError is the watcher's first failure, or "" while every mod root is watched.
func (s *Session) WatchError() string {
	if s.watcher == nil {
		return ""
	}
	if err := s.watcher.Err(); err != nil {
		return err.Error()
	}
	return ""
}
