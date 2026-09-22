// Package session is one workspace open in the editor: the install's scanned index and schema, the mods' index built over it, the files the editor has open, and the questions the editor asks of all of them together. It decides nothing about words: the reader does that, with the session as its Defined.
//
// session.go is the session and its lock. Readers hold RLock for a whole request; the writers (Did*, Changed, Replace) take Lock themselves and are never called under a read lock. That order is binding (AGENTS.md).
package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// Calendar is the era a mod writes its dates in. Nothing in a mod states the offset, so a person declares it once; with none declared every date reads as written.
type Calendar struct {
	Era         string
	EpochOffset int
}

// Mod is one mod of the workspace, in the playset's order.
type Mod struct {
	ID, Name, Dir string
	Calendar      Calendar
}

// Place is where a path sits: whose file it is, the path from its root, and what may be defined there.
type Place struct {
	Origin   string // game.OriginVanilla, a mod's ID, or "" for a path under no root
	Rel      string
	Kinds    []*schema.Kind
	Format   source.Format
	Calendar Calendar
}

// buffer is a file the editor has open: its text, and the parse of that text, made once when the text arrives, so a reader never writes.
type buffer struct {
	file   source.File
	text   string
	parsed jomini.Result
}

// Session is one workspace: the install, the mods over it, and the open files.
type Session struct {
	mu        sync.RWMutex
	Game      *game.GameInfo
	Install   *index.Install
	Workspace *index.Index
	Schema    *schema.Schema // the install's plus what Relearn added for the workspace
	Mods      []Mod
	Language  string
	watcher   *Watcher
	buffers   map[string]*buffer // by index.PathKey
	built     map[string]string  // by index.PathKey: the text a workspace file's records were built from
}

// New builds the workspace's index over a scanned install.
func New(ctx context.Context, g *game.GameInfo, install *index.Install, mods []Mod, language string) (*Session, error) {
	if language == "" {
		language = "english"
	}
	s := &Session{Game: g, Install: install, Mods: mods, Language: language, buffers: map[string]*buffer{}, built: map[string]string{}}
	return s, s.build(ctx)
}

// build makes the workspace index and schema from the mods' files on disk, then lays the open buffers over them.
func (s *Session) build(ctx context.Context) error {
	var mods []index.Mod
	for _, m := range s.Mods {
		mods = append(mods, index.Mod{ID: m.ID, Dir: m.Dir})
	}
	x, sc, err := index.BuildWorkspace(ctx, mods, s.Install)
	if err != nil {
		return err
	}
	s.Workspace, s.Schema, s.built = x, sc, map[string]string{}
	for _, b := range s.buffers {
		s.harvest(b.file, b.text)
	}
	return nil
}

// Replace puts a rescanned install under the workspace, which is built again; the open buffers stay.
func (s *Session) Replace(ctx context.Context, install *index.Install) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Install = install
	return s.build(ctx)
}

// All is the two indexes in load order: the install, then the workspace's mods over it.
func (s *Session) All() index.All {
	return index.All{s.Install.Index, s.Workspace}
}

// Reader is the one reader, answering from the workspace and the install together.
func (s *Session) Reader() schema.Reader {
	return schema.Reader{Schema: s.Schema, Vocab: s.Install.Vocab, Found: s.All()}
}

// Order is the load order: the game, then each mod as the playset lists them.
func (s *Session) Order() []string {
	order := []string{game.OriginVanilla}
	for _, m := range s.Mods {
		order = append(order, m.ID)
	}
	return order
}

// Defs is every definition of a name the game loads, in load order.
func (s *Session) Defs(name string) []index.Found {
	return s.All().Named(name)
}

// Winner is the definition the game keeps of several: first-wins where the registry says so, the entry modes as the registry reads them.
func (s *Session) Winner(found []index.Found) (index.Found, int) {
	if len(found) == 0 {
		return index.Found{}, -1
	}
	return index.Winner(found, game.IsFIOS(s.Game.ID, found[0].Kind), s.Game.EntryModes, s.Order())
}

// Loc is the entry of a key in a language, and the file it is written in; the last loaded, so a mod's over the install's. With none in that language, an entry of any language, because a key that exists is not a missing key.
func (s *Session) Loc(key, language string) (index.Said, bool) {
	if language == "" {
		language = s.Language
	}
	said := s.All().Entries(key)
	for i := len(said) - 1; i >= 0; i-- {
		if loc.LanguageFromRel(said[i].At.Rel) == language {
			return said[i], true
		}
	}
	if len(said) > 0 {
		return said[len(said)-1], true
	}
	return index.Said{}, false
}

// LocKeys is up to limit distinct keys that start with prefix.
func (s *Session) LocKeys(prefix string, limit int) []string {
	seen, out := map[string]bool{}, []string{}
	for _, x := range s.All() {
		for _, e := range x.Loc {
			if e.File >= 0 && !seen[e.Key] && strings.HasPrefix(e.Key, prefix) {
				seen[e.Key], out = true, append(out, e.Key)
				if len(out) == limit {
					return out
				}
			}
		}
	}
	return out
}

// Where is what a path is to the workspace: the mod or the install that holds it, its rel, and the kinds a file there may define. A file nothing has walked yet is placed by its root.
func (s *Session) Where(p string) Place {
	f, m, ok := s.Located(p)
	if !ok {
		return Place{Format: source.Classify(filepath.ToSlash(p))}
	}
	place := Place{Origin: f.Origin, Rel: f.Rel, Kinds: s.Schema.KindsAt(f.Rel), Format: f.Format}
	if m != nil {
		place.Calendar = m.Calendar
	}
	return place
}

// Located is the inventory's file at a path, else the file a path would be under one of the mods' or the install's roots; and the mod it belongs to.
func (s *Session) Located(p string) (source.File, *Mod, bool) {
	mod := func(origin string) *Mod {
		if at := slices.IndexFunc(s.Mods, func(m Mod) bool { return m.ID == origin }); at >= 0 {
			return &s.Mods[at]
		}
		return nil
	}
	if f, ok := s.Workspace.At(p); ok {
		return f, mod(f.Origin), true
	}
	if f, ok := s.Install.Index.At(p); ok {
		return f, nil, true
	}
	under := func(roots []string, origin string) (source.File, bool) {
		for _, root := range roots {
			if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
				rel = filepath.ToSlash(rel)
				return source.File{Path: p, Rel: rel, Origin: origin, Format: source.Classify(rel)}, true
			}
		}
		return source.File{}, false
	}
	for i := range s.Mods {
		if f, ok := under(source.Roots(s.Mods[i].Dir), s.Mods[i].ID); ok {
			return f, &s.Mods[i], true
		}
	}
	if f, ok := under(s.Install.Index.Roots, game.OriginVanilla); ok {
		return f, nil, true
	}
	return source.File{}, nil, false
}

// Text is a file's text: the open buffer's, else the disk's.
func (s *Session) Text(p string) (string, error) {
	if b := s.buffers[index.PathKey(p)]; b != nil {
		return b.text, nil
	}
	return source.File{Path: p}.Text()
}

// Parsed is the parse of a file's text: an open buffer's, made once per text, else the disk's.
func (s *Session) Parsed(p string) (*jomini.Result, string, error) {
	if b := s.buffers[index.PathKey(p)]; b != nil {
		return &b.parsed, b.text, nil
	}
	text, err := s.Text(p)
	if err != nil {
		return nil, "", err
	}
	parsed := jomini.Parse(text)
	return &parsed, text, nil
}

// DidOpen and DidChange hold the editor's text of a file and index it from that text; the disk's records go. The caller's spelling of the path is kept, because the editor keys its tabs by it.
func (s *Session) DidOpen(p, text string) { s.DidChange(p, text) }
func (s *Session) DidChange(p, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := index.PathKey(p)
	b := s.buffers[key]
	if b == nil {
		f, _, ok := s.Located(p)
		if !ok {
			f = source.File{Path: p, Format: source.Classify(filepath.ToSlash(p))}
		}
		f.Path = filepath.Clean(p)
		b = &buffer{file: f}
		s.buffers[key] = b
	}
	if b.text != text || b.parsed.Root == nil {
		b.text, b.parsed = text, jomini.Parse(text)
	}
	s.harvest(b.file, text)
}

// DidClose forgets the editor's text; the file is read from disk again.
func (s *Session) DidClose(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.buffers[index.PathKey(p)]
	if b == nil {
		return
	}
	delete(s.buffers, index.PathKey(p))
	s.changed(b.file.Path)
}

// DidSave reads the saved file: the same text as the buffer indexes nothing again.
func (s *Session) DidSave(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changed(p)
}

// Changed is a file changed on disk outside the editor. A file that cannot be read keeps its records, because on Windows a sharing violation during a save is the ordinary case; only a file that is gone loses them.
func (s *Session) Changed(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffers[index.PathKey(p)] == nil {
		s.changed(p)
	}
}

// changed indexes a path from the disk, under the lock.
func (s *Session) changed(p string) {
	f, m, ok := s.Located(p)
	if !ok || m == nil {
		return
	}
	text, err := source.File{Path: p}.Text()
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.Workspace.Drop(p)
		delete(s.built, index.PathKey(p))
	case err == nil:
		s.harvest(f, text)
	}
}

// harvest indexes one workspace file from a text, once per distinct text; an install's file is never indexed from the editor, so the install stays what the scan found. A name the file defines that nothing defined before is read again in every other open buffer that spells it, because those were read while it did not exist.
func (s *Session) harvest(f source.File, text string) {
	key := index.PathKey(f.Path)
	if f.Origin == game.OriginVanilla || f.Origin == "" || s.built[key] == text {
		return
	}
	s.built[key] = text
	all := s.All()
	x := s.Workspace
	defs, locs := len(x.Defs), len(x.Loc)
	x.IndexFile(f, text, s.Reader())
	var fresh []string
	for _, d := range x.Defs[defs:] {
		if len(all.Named(d.Name)) == 1 {
			fresh = append(fresh, d.Name)
		}
	}
	for _, e := range x.Loc[locs:] {
		if len(all.Entries(e.Key)) == 1 {
			fresh = append(fresh, e.Key)
		}
	}
	for other, b := range s.buffers {
		if other != key && b.file.Origin != game.OriginVanilla && b.file.Origin != "" && slices.ContainsFunc(fresh, func(name string) bool { return strings.Contains(b.text, name) }) {
			s.built[other] = b.text
			x.IndexFile(b.file, b.text, s.Reader())
		}
	}
}

// RLock and RUnlock are held by a reader for its whole request.
func (s *Session) RLock()   { s.mu.RLock() }
func (s *Session) RUnlock() { s.mu.RUnlock() }
