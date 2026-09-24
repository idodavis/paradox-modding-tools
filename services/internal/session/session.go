// Package session is one workspace open in the editor: the install's scanned index and schema, the
// mods' index built over it, and the files the editor has open. It decides nothing about words; the
// schema reader does that, with the session's indexes as its Defined.
//
// Readers hold RLock for a whole request. The writers (Did*, Changed, Replace) take Lock themselves
// and are never called under a read lock.
package session

import (
	"cmp"
	"context"
	"errors"
	"github.com/fsnotify/fsnotify"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Calendar is the era a mod writes its dates in. A mod never states the offset, so the user
// declares it; with none declared, dates read as written.
type Calendar struct {
	Era         string
	EpochOffset int
}

// Mod is one mod of the workspace, in the playset's order.
type Mod struct {
	ID, Name, Dir string
	Calendar      Calendar
}

// Place is where a path sits: its origin, its path from the root, and the kinds it may define.
type Place struct {
	Origin   string // game.OriginVanilla, a mod's ID, or "" for a path under no root
	Rel      string
	Kinds    []*schema.Kind
	Format   source.Format
	Calendar Calendar
}

// buffer is a file open in the editor: its text and that text's parse, made once when the text
// arrives so a reader never writes.
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
func New(ctx context.Context, info *game.GameInfo, install *index.Install, mods []Mod, language string) (*Session, error) {
	language = cmp.Or(language, "english")
	sess := &Session{Game: info, Install: install, Mods: mods, Language: language, buffers: map[string]*buffer{}, built: map[string]string{}}
	return sess, sess.build(ctx)
}

// build makes the workspace index and schema from the mods' files, then re-indexes open buffers.
func (sess *Session) build(ctx context.Context) error {
	var mods []index.Mod
	for _, mod := range sess.Mods {
		mods = append(mods, index.Mod{ID: mod.ID, Dir: mod.Dir})
	}
	idx, sc, err := index.BuildWorkspace(ctx, mods, sess.Install)
	if err != nil {
		return err
	}
	sess.Workspace, sess.Schema, sess.built = idx, sc, map[string]string{}
	for _, buffer := range sess.buffers {
		sess.harvest(buffer.file, buffer.text)
	}
	return nil
}

// Replace puts a rescanned install under the workspace and rebuilds it; the open buffers stay.
func (sess *Session) Replace(ctx context.Context, install *index.Install) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.Install = install
	return sess.build(ctx)
}

// All is the two indexes in load order: the install, then the workspace's mods over it.
func (sess *Session) All() index.All {
	return index.All{sess.Install.Index, sess.Workspace}
}

// Reader is the one reader, answering from the workspace and the install together.
func (sess *Session) Reader() schema.Reader {
	return schema.Reader{Schema: sess.Schema, Vocab: sess.Install.Vocab, Found: sess.All()}
}

// Order is the load order: the game, then each mod as the playset lists them.
func (sess *Session) Order() []string {
	order := []string{game.OriginVanilla}
	for _, mod := range sess.Mods {
		order = append(order, mod.ID)
	}
	return order
}

// Defs is every definition of a name the game loads, in load order.
func (sess *Session) Defs(name string) []index.Found {
	return sess.All().Named(name)
}

// Winner is the definition the game keeps of several, by the registry's first-wins and entry modes.
func (sess *Session) Winner(found []index.Found) (index.Found, int) {
	if len(found) == 0 {
		return index.Found{}, -1
	}
	return index.Winner(found, game.IsFIOS(sess.Game.ID, found[0].Kind), sess.Game.EntryModes, sess.Order())
}

// Fired reports a kind the game fires: events, the kind whose names are event ids, and on_actions.
func (sess *Session) Fired(kind string) bool {
	named := sess.Schema.Kinds[kind]
	return kind == "on_action" || (named != nil && named.Naming.Form == "event_id")
}

// Fire is the definition at the far end of a fire, with the tokens and macros between.
type Fire struct {
	Name string `json:"name"`
	Via  string `json:"via"`
}

// Fires is what a definition fires, in name order: events and on_actions only. A fire through a
// macro is followed to what the macro fires, and a macro already met on the way is not entered again.
func (sess *Session) Fires(from string, seen map[string]bool) []Fire {
	var out []Fire
	for _, idx := range sess.All() {
		for _, fired := range idx.Fires(from) {
			if fired.File < 0 {
				continue
			}
			if kind := sess.Schema.Kinds[fired.ToKind]; kind != nil && kind.Macro != 0 {
				if !seen[fired.To] {
					seen[fired.To] = true
					for _, through := range sess.Fires(fired.To, seen) {
						out = append(out, Fire{Name: through.Name, Via: fired.To + " → " + through.Via})
					}
				}
				continue
			}
			if sess.Fired(fired.ToKind) {
				out = append(out, Fire{Name: fired.To, Via: fired.Via})
			}
		}
	}
	slices.SortStableFunc(out, func(first, second Fire) int { return strings.Compare(first.Name, second.Name) })
	return slices.CompactFunc(out, func(first, second Fire) bool { return first.Name == second.Name })
}

// Loc is a key's entry in a language, the last loaded (a mod's over the install's). With none in
// that language it is an entry of any language: a key that exists is not a missing key.
func (sess *Session) Loc(key, language string) (index.Said, bool) {
	language = cmp.Or(language, sess.Language)
	said := sess.All().Entries(key)
	for _, entry := range slices.Backward(said) {
		if loc.LanguageFromRel(entry.At.Rel) == language {
			return entry, true
		}
	}
	if len(said) > 0 {
		return said[len(said)-1], true
	}
	return index.Said{}, false
}

// LocKeys is up to limit distinct keys that start with prefix.
func (sess *Session) LocKeys(prefix string, limit int) []string {
	seen, out := map[string]bool{}, []string{}
	for _, idx := range sess.All() {
		for _, loc := range idx.Loc {
			if loc.File >= 0 && !seen[loc.Key] && strings.HasPrefix(loc.Key, prefix) {
				seen[loc.Key], out = true, append(out, loc.Key)
				if len(out) == limit {
					return out
				}
			}
		}
	}
	return out
}

// Where places a path in the workspace: its origin, rel, and the kinds a file there may define. A
// file not yet walked is placed by its root.
func (sess *Session) Where(path string) Place {
	file, mod, ok := sess.Located(path)
	if !ok {
		return Place{Format: source.Classify(filepath.ToSlash(path))}
	}
	place := Place{Origin: file.Origin, Rel: file.Rel, Kinds: sess.Schema.KindsAt(file.Rel), Format: file.Format}
	if mod != nil {
		place.Calendar = mod.Calendar
	}
	return place
}

// Located is the indexed file at a path, else the file the path would be under a mod's or the
// install's root, and the mod it belongs to.
func (sess *Session) Located(path string) (source.File, *Mod, bool) {
	mod := func(origin string) *Mod {
		if at := slices.IndexFunc(sess.Mods, func(mod Mod) bool { return mod.ID == origin }); at >= 0 {
			return &sess.Mods[at]
		}
		return nil
	}
	if file, ok := sess.Workspace.At(path); ok {
		return file, mod(file.Origin), true
	}
	if file, ok := sess.Install.Index.At(path); ok {
		return file, nil, true
	}
	under := func(roots []string, origin string) (source.File, bool) {
		for _, root := range roots {
			if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				rel = filepath.ToSlash(rel)
				return source.File{Path: path, Rel: rel, Origin: origin, Format: source.Classify(rel)}, true
			}
		}
		return source.File{}, false
	}
	for i := range sess.Mods {
		if file, ok := under(source.Roots(sess.Mods[i].Dir), sess.Mods[i].ID); ok {
			return file, &sess.Mods[i], true
		}
	}
	if file, ok := under(sess.Install.Index.Roots, game.OriginVanilla); ok {
		return file, nil, true
	}
	return source.File{}, nil, false
}

// Text is a file's text: the open buffer's, else the disk's.
func (sess *Session) Text(path string) (string, error) {
	if buffer := sess.buffers[index.PathKey(path)]; buffer != nil {
		return buffer.text, nil
	}
	return source.File{Path: path}.Text()
}

// Parsed is the parse of a file's text: an open buffer's, made once per text, else the disk's.
func (sess *Session) Parsed(path string) (*jomini.Result, string, error) {
	if buffer := sess.buffers[index.PathKey(path)]; buffer != nil {
		return &buffer.parsed, buffer.text, nil
	}
	text, err := sess.Text(path)
	if err != nil {
		return nil, "", err
	}
	parsed := jomini.Parse(text)
	return &parsed, text, nil
}

// DidOpen and DidChange hold the editor's text of a file and index it from that text, in place of
// the disk's records. The caller's spelling of the path is kept: the editor keys its tabs by it.
func (sess *Session) DidOpen(path, text string) { sess.DidChange(path, text) }
func (sess *Session) DidChange(path, text string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	key := index.PathKey(path)
	buf := sess.buffers[key]
	if buf == nil {
		file, _, ok := sess.Located(path)
		if !ok {
			file = source.File{Path: path, Format: source.Classify(filepath.ToSlash(path))}
		}
		file.Path = filepath.Clean(path)
		buf = &buffer{file: file}
		sess.buffers[key] = buf
	}
	if buf.text != text || buf.parsed.Root == nil {
		buf.text, buf.parsed = text, jomini.Parse(text)
	}
	sess.harvest(buf.file, text)
}

// DidClose forgets the editor's text; the file is read from disk again.
func (sess *Session) DidClose(path string) {
	text, err := source.File{Path: filepath.Clean(path)}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	buf := sess.buffers[index.PathKey(path)]
	if buf == nil {
		return
	}
	delete(sess.buffers, index.PathKey(path))
	sess.changed(buf.file.Path, text, err)
}

// DidSave reads the saved file: the same text as the buffer indexes nothing again.
func (sess *Session) DidSave(path string) {
	// read before the lock: every editor request waits on this mutex, so disk reads stay outside it
	text, err := source.File{Path: path}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.changed(path, text, err)
}

// Changed is a file changed on disk outside the editor. An unreadable file keeps its records, since
// on Windows a save often hits a sharing violation; only a file that is gone loses them.
func (sess *Session) Changed(path string) {
	text, err := source.File{Path: path}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.buffers[index.PathKey(path)] == nil {
		sess.changed(path, text, err)
	}
}

// changed indexes a path from text its caller has already read, under the lock.
func (sess *Session) changed(path, text string, err error) {
	file, mod, ok := sess.Located(path)
	if !ok || mod == nil {
		return
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		sess.Workspace.Drop(path)
		delete(sess.built, index.PathKey(path))
	case err == nil:
		sess.harvest(file, text)
	}
}

// harvest indexes a workspace file from a text, once per distinct text; an install's file is never
// indexed from the editor. When the file defines a new name, every other open buffer that spells it
// is indexed again, since it was read while the name did not exist.
func (sess *Session) harvest(file source.File, text string) {
	key := index.PathKey(file.Path)
	if file.Origin == game.OriginVanilla || file.Origin == "" || sess.built[key] == text {
		return
	}
	sess.built[key] = text
	all := sess.All()
	idx := sess.Workspace
	defs, locs := len(idx.Defs), len(idx.Loc)
	idx.IndexFile(file, text, sess.Reader())
	var fresh []string
	for _, def := range idx.Defs[defs:] {
		if len(all.Named(def.Name)) == 1 {
			fresh = append(fresh, def.Name)
		}
	}
	for _, loc := range idx.Loc[locs:] {
		if len(all.Entries(loc.Key)) == 1 {
			fresh = append(fresh, loc.Key)
		}
	}
	for other, buffer := range sess.buffers {
		if other != key && buffer.file.Origin != game.OriginVanilla && buffer.file.Origin != "" && slices.ContainsFunc(fresh, func(name string) bool { return strings.Contains(buffer.text, name) }) {
			sess.built[other] = buffer.text
			idx.IndexFile(buffer.file, buffer.text, sess.Reader())
		}
	}
}

// RLock and RUnlock are held by a reader for its whole request.
func (sess *Session) RLock()   { sess.mu.RLock() }
func (sess *Session) RUnlock() { sess.mu.RUnlock() }

// Watch reports every change under the mods' folders to the session, then to after, until Close.
func (sess *Session) Watch(after func(path string)) error {
	var dirs []string
	for _, mod := range sess.Mods {
		dirs = append(dirs, mod.Dir)
	}
	word, err := NewWatcher(dirs, func(path string) {
		sess.Changed(path)
		after(path)
	})
	sess.watcher = word
	return err
}

// WatchError is the first error the session's watcher hit, nil while it is live or there is none.
func (sess *Session) WatchError() error {
	if sess.watcher == nil {
		return nil
	}
	return sess.watcher.Err()
}

// Close stops the session's watcher, if it has one.
func (sess *Session) Close() {
	if sess.watcher != nil {
		sess.watcher.Close()
	}
}

// watchDebounce is how long a path must be quiet before its change is reported;
// editors and git often emit several events per logical write.
const watchDebounce = 150 * time.Millisecond

// Watcher reports debounced changes to indexable files under a set of roots.
type Watcher struct {
	fsw      *fsnotify.Watcher
	onChange func(path string)

	mu     sync.Mutex
	timers map[string]*time.Timer
	closed bool
	done   chan struct{}
	// err is the first Add or event failure (inotify limits, overflow); shown on the health strip.
	err error
}

// NewWatcher starts watching roots recursively, calling onChange(absPath) once a
// changed .txt/.gui/.yml file settles. Close stops it.
func NewWatcher(roots []string, onChange func(path string)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watcher := &Watcher{fsw: fsw, onChange: onChange, timers: map[string]*time.Timer{}, done: make(chan struct{})}
	for _, root := range roots {
		watcher.addTree(root)
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
	return watcher.fsw.Close()
}

// addTree watches root and every folder under it that holds a file; fsnotify is not recursive.
func (watcher *Watcher) addTree(root string) {
	seen := map[string]bool{}
	watcher.fail(watcher.fsw.Add(root))
	source.Walk(root, func(path string) {
		if dir := filepath.Dir(path); !seen[dir] {
			seen[dir] = true
			watcher.fail(watcher.fsw.Add(dir))
		}
	})
}

// fail records the first error; later ones add nothing a user can act on.
func (watcher *Watcher) fail(err error) {
	if err == nil {
		return
	}
	watcher.mu.Lock()
	if watcher.err == nil {
		watcher.err = err
	}
	watcher.mu.Unlock()
}

// Err is the first error the watch hit, or nil while it is fully live.
func (watcher *Watcher) Err() error {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	return watcher.err
}

// loop consumes fsnotify events until Close.
func (watcher *Watcher) loop() {
	for {
		select {
		case <-watcher.done:
			return
		case ev, ok := <-watcher.fsw.Events:
			if !ok {
				return
			}
			watcher.handle(ev)
		case err, ok := <-watcher.fsw.Errors:
			if !ok {
				return
			}
			watcher.fail(err)
		}
	}
}

// handle debounces a relevant event and, for new directories, extends the watch.
func (watcher *Watcher) handle(ev fsnotify.Event) {
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			watcher.addTree(ev.Name)
			return
		}
	}
	if !indexable(ev.Name) {
		return
	}
	watcher.mu.Lock()
	if timer := watcher.timers[ev.Name]; timer != nil {
		timer.Stop()
	}
	name := ev.Name
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

// indexable reports whether a path is a file the index reads.
func indexable(path string) bool {
	switch source.Classify(filepath.ToSlash(path)) {
	case source.Script, source.Loc, source.Table, source.List:
		return true
	}
	return false
}
