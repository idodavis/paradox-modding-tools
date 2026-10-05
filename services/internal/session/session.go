// Package session is one workspace open in the editor: the install's scanned schema and index, the
// mods' index built over it, and the files the editor has open. It decides nothing about words; the
// index's walk does that, and a query resolves names across the session's two indexes.
//
// Readers hold RLock for a whole request. The writers (Did*, Changed) take Lock themselves and are
// never called under a read lock.
package session

import (
	"cmp"
	"context"
	"errors"
	"github.com/fsnotify/fsnotify"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Calendar is the eras a mod writes its dates in. A mod never states them, so the user declares
// them; with none declared, dates read as written.
type Calendar []Era

// Era is one era of a mod's calendar: its name and the year before its first (AGOT's A.C., 8000).
type Era struct {
	Name   string `json:"name"`
	Offset int    `json:"offset"`
}

// Read is a year in the named era with the greatest offset below it, and the era's name; false
// where none applies (8082 is 82 A.C.).
func (calendar Calendar) Read(year int) (int, string, bool) {
	best := -1
	for i, era := range calendar {
		if era.Name != "" && year-era.Offset > 0 && (best < 0 || era.Offset > calendar[best].Offset) {
			best = i
		}
	}
	if best < 0 {
		return 0, "", false
	}
	return year - calendar[best].Offset, calendar[best].Name, true
}

// Mod is one mod of the workspace, in the playset's order.
type Mod struct {
	ID, Dir  string
	Calendar Calendar
}

// Place is where a path sits: its file (Origin "" for a path under no root), the database it
// defines and its mod's calendar.
type Place struct {
	source.File
	Database *schema.Database // nil when the folder holds none
	Calendar Calendar
}

// buffer is a file open in the editor: its text and that text's parse, made once when the text
// arrives so a reader never writes.
type buffer struct {
	file   source.File
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

// New builds the workspace's index over a scanned install.
func New(ctx context.Context, info *game.GameInfo, install *index.Install, mods []Mod, language string) (*Session, error) {
	var scanned []index.Mod
	for _, mod := range mods {
		scanned = append(scanned, index.Mod{ID: mod.ID, Dir: mod.Dir})
	}
	workspace, err := index.ScanMods(ctx, install, scanned)
	if err != nil {
		return nil, err
	}
	return &Session{
		Changes: changes.Add(1), Game: info, Install: install, Workspace: workspace, Mods: mods,
		Language: cmp.Or(language, game.BaseLanguage), buffers: map[string]*buffer{},
	}, nil
}

// Indexes are the two indexes in load order: the install, then the workspace's mods over it.
func (sess *Session) Indexes() []*index.Index {
	return []*index.Index{sess.Install.Index, sess.Workspace}
}

// Winner is the definition the game keeps of several by the entry modes, its adds, the INJECT and
// REPLACE definitions with no target, and whether two mods contest the name.
func (sess *Session) Winner(defs []index.Def) (winner index.Def, adds, orphans []index.Def, contested bool) {
	return index.Winner(sess.Game, defs, func(path string) string {
		// a path no mod lists is the install's
		if file, listed := sess.Workspace.At(path); listed {
			return file.Origin
		}
		return game.OriginVanilla
	})
}

// Fire is the definition at the far end of a fire, its database, and the scripted entries between.
type Fire struct {
	Name     string `json:"name"`
	Database string `json:"database"`
	Via      string `json:"via"`
}

// Fires is what a definition fires, in name order: events and on_actions only (Ref.Fires). A
// scripted entry it runs (Ref.Runs) is followed to what it fires, and an entry already met on the
// way is not entered again.
func (sess *Session) Fires(from index.Def) []Fire {
	sch, indexes := sess.Install.Schema, sess.Indexes()
	seen, out := map[index.Def]bool{from: true}, []Fire(nil)

	// follow reads the references written in one definition; via is the entries entered to reach it
	var follow func(def index.Def, via string)
	follow = func(def index.Def, via string) {
		for _, ref := range index.Within(indexes, def) {
			switch {
			case ref.Fires(sch):
				out = append(out, Fire{Name: ref.Name, Database: ref.Database, Via: via})
			case ref.Runs(sch):
				called, _ := index.Resolve(sch, indexes, ref)
				for _, target := range called {
					if !seen[target] {
						seen[target] = true
						follow(target, strings.TrimPrefix(via+" → "+target.Name, " → "))
					}
				}
			}
		}
	}
	follow(from, "")

	slices.SortStableFunc(out, func(first, second Fire) int { return strings.Compare(first.Name, second.Name) })
	return slices.CompactFunc(out, func(first, second Fire) bool { return first.Name == second.Name })
}

// FiredBy is Fires turned around, in name order: each definition holding a reference that fires
// or runs this one, a scripted entry followed to what runs it, with the chain as Fires writes it.
func (sess *Session) FiredBy(target index.Def) []Fire {
	sch, indexes := sess.Install.Schema, sess.Indexes()
	seen := map[index.Def]bool{target: true}

	// follow reads the references to one definition; a local one answers only its own file's
	var follow func(def index.Def) []Fire
	follow = func(def index.Def) (out []Fire) {
		for _, ref := range index.Uses(sch, indexes, def.Database, def.Name) {
			var caller index.Def
			for _, idx := range indexes {
				if records := idx.Records[ref.Path]; records != nil && ref.From >= 0 {
					caller = records.Defs[ref.From]
				}
			}
			switch {
			case !ref.Fires(sch) && !ref.Runs(sch) || caller.Name == "" || def.Local && ref.Path != def.Path:
			case caller.Scripted(sch):
				if !seen[caller] {
					seen[caller] = true
					for _, up := range follow(caller) {
						out = append(out, Fire{Name: up.Name, Database: up.Database, Via: strings.TrimPrefix(up.Via+" → "+caller.Name, " → ")})
					}
				}
			default:
				out = append(out, Fire{Name: caller.Name, Database: caller.Database})
			}
		}
		return out
	}
	out := follow(target)

	slices.SortStableFunc(out, func(first, second Fire) int { return strings.Compare(first.Name, second.Name) })
	return slices.CompactFunc(out, func(first, second Fire) bool { return first.Name == second.Name })
}

// Loc is a key's entry in a language, the last loaded: a mod's over the install's. With none in the
// language it is the english one, so a title shows; whether the key exists is Resolve's to say.
func (sess *Session) Loc(key, language string) (index.Loc, bool) {
	entries := index.LookupLocs(sess.Indexes(), key)
	for _, wanted := range []string{cmp.Or(language, sess.Language), game.BaseLanguage} {
		for _, entry := range slices.Backward(entries) {
			if entry.Language == wanted {
				return entry, true
			}
		}
	}
	return index.Loc{}, false
}

// Entry is a definition's statement in its file's parse: the assignment whose key starts at the
// definition's line and column; nil where its file does not parse or none starts there.
func (sess *Session) Entry(def index.Def) (*jomini.Result, jomini.Statement) {
	parsed, _, err := sess.Parsed(def.Path)
	if def.Path == "" || err != nil {
		return nil, nil
	}
	start, found := parsed.Lines().OffsetAt(int(def.Line), int(def.Col)), jomini.Statement(nil)
	jomini.Walk(parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
		if assign, isAssign := statement.(*jomini.Assignment); isAssign && assign.Key.Range.Start == start {
			found = statement
		}
		return found == nil && statement.NodeRange().Start <= start && start < statement.NodeRange().End
	})
	return parsed, found
}

// Title is a definition's display name: the text of its name as a loc key, else of its first field
// typed as a loc key that has text.
func (sess *Session) Title(def index.Def) string {
	if said, found := sess.Loc(def.Name, ""); found {
		return said.Text
	}
	row := sess.Install.Schema.Databases[def.Database]
	_, entry := sess.Entry(def)
	for i, block := 0, jomini.ChildBlock(entry); row != nil && block != nil && i < len(block.Statements); i++ {
		field, isField := block.Statements[i].(*jomini.Assignment)
		if !isField {
			continue
		}
		position := sess.Install.Schema.Step(sess.Install.Schema.Start(row), field, field.Key.Text, false).Key
		value, isWord := field.Value.(*jomini.Scalar)
		if !isWord || position == nil || position.Value.Value.Type != schema.WordLoc {
			continue
		}
		if said, found := sess.Loc(value.Text, ""); found {
			return said.Text
		}
	}
	return ""
}

// LocKeys is up to limit of the keys that start with prefix, in order.
func (sess *Session) LocKeys(prefix string, limit int) []string {
	keys := map[string]bool{}
	for _, idx := range sess.Indexes() {
		for key := range idx.LocKeys() {
			if strings.HasPrefix(key, prefix) {
				keys[key] = true
			}
		}
	}
	sorted := slices.Sorted(maps.Keys(keys))
	return sorted[:min(limit, len(sorted))]
}

// Where places a path in the workspace: its file, and the database a file there defines. A file
// not yet walked is placed by its root.
func (sess *Session) Where(path string) Place {
	file, mod, ok := sess.Located(path)
	if !ok {
		return Place{File: source.File{Format: source.Classify(path, filepath.ToSlash(path))}}
	}
	place := Place{File: file, Database: sess.Install.Schema.DatabaseAt(file.Rel)}
	if mod != nil {
		place.Calendar = mod.Calendar
	}
	return place
}

// Located is the indexed file at a path, else the file the path would be under a mod's or the
// install's root, and the mod it belongs to.
func (sess *Session) Located(path string) (source.File, *Mod, bool) {
	path = filepath.Clean(path)
	mod := func(origin string) *Mod {
		if at := slices.IndexFunc(sess.Mods, func(mod Mod) bool { return mod.ID == origin }); at >= 0 {
			return &sess.Mods[at]
		}
		return nil
	}
	for _, idx := range slices.Backward(sess.Indexes()) {
		if file, listed := idx.At(path); listed {
			return file, mod(file.Origin), true
		}
	}
	under := func(roots []string, origin string) (source.File, bool) {
		for _, root := range roots {
			if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				rel = filepath.ToSlash(rel)
				return source.File{Path: path, Rel: rel, Origin: origin, Format: source.Classify(path, rel)}, true
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
	if buffer := sess.buffers[strings.ToLower(filepath.Clean(path))]; buffer != nil {
		return buffer.text, nil
	}
	return source.File{Path: path}.Text()
}

// Parsed is the parse of a file's text: an open buffer's, made once per text, else the disk's.
func (sess *Session) Parsed(path string) (*jomini.Result, string, error) {
	if buffer := sess.buffers[strings.ToLower(filepath.Clean(path))]; buffer != nil {
		return &buffer.parsed, buffer.text, nil
	}
	text, err := sess.Text(path)
	if err != nil {
		return nil, "", err
	}
	parsed := jomini.Parse(text)
	return &parsed, text, nil
}

// DidChange holds the editor's text of an opened or changed file and indexes it from that text, in
// place of the disk's records. The records keep the inventory's spelling of the path.
func (sess *Session) DidChange(path, text string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	key := strings.ToLower(filepath.Clean(path))
	opened := sess.buffers[key]
	if opened == nil {
		file, _, ok := sess.Located(path)
		if !ok {
			file = source.File{Path: filepath.Clean(path), Format: source.Classify(path, filepath.ToSlash(path))}
		}
		opened = &buffer{file: file}
		sess.buffers[key] = opened
	}
	if opened.text != text || opened.parsed.Root == nil {
		opened.text, opened.parsed = text, jomini.Parse(text)
	}
	sess.changed(opened.file, text, nil)
}

// DidClose forgets the editor's text; the file is read from disk again.
func (sess *Session) DidClose(path string) {
	text, err := source.File{Path: filepath.Clean(path)}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	key := strings.ToLower(filepath.Clean(path))
	opened := sess.buffers[key]
	if opened == nil {
		return
	}
	delete(sess.buffers, key)
	sess.changed(opened.file, text, err)
}

// DidSave reads the saved file and indexes it again.
func (sess *Session) DidSave(path string) {
	// read before the lock: every editor request waits on this mutex, so disk reads stay outside it
	text, err := source.File{Path: path}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if file, _, ok := sess.Located(path); ok {
		sess.changed(file, text, err)
	}
}

// Changed is a file changed on disk outside the editor. An unreadable file keeps its records, since
// on Windows a save often hits a sharing violation; only a file that is gone loses them.
func (sess *Session) Changed(path string) {
	text, err := source.File{Path: path}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if file, _, ok := sess.Located(path); ok && sess.buffers[strings.ToLower(filepath.Clean(path))] == nil {
		sess.changed(file, text, err)
	}
}

// changed walks a mod file again from text its caller has already read, under the lock; an
// install's file, or one under no root, is never indexed from the editor.
func (sess *Session) changed(file source.File, text string, err error) {
	if file.Origin == game.OriginVanilla || file.Origin == "" {
		return
	}
	switch {
	case err == nil:
		sess.Workspace.Reindex(sess.Install.Schema, file, text)
	case errors.Is(err, os.ErrNotExist):
		sess.Workspace.Forget(file.Path)
	default:
		return
	}
	sess.Changes = changes.Add(1)
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
	watcher, err := NewWatcher(dirs, func(path string) {
		sess.Changed(path)
		after(path)
	})
	sess.watcher = watcher
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

// NewWatcher starts watching roots recursively, calling onChange once a changed indexable file
// settles. Close stops it.
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
		slog.Warn("watcher failed", "error", err)
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
		case event, ok := <-watcher.fsw.Events:
			if !ok {
				return
			}
			watcher.handle(event)
		case err, ok := <-watcher.fsw.Errors:
			if !ok {
				return
			}
			watcher.fail(err)
		}
	}
}

// handle debounces a relevant event and, for new directories, extends the watch.
func (watcher *Watcher) handle(event fsnotify.Event) {
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			watcher.addTree(event.Name)
			return
		}
	}
	if !indexable(event.Name) {
		return
	}
	watcher.mu.Lock()
	if timer := watcher.timers[event.Name]; timer != nil {
		timer.Stop()
	}
	name := event.Name
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
	switch source.Classify(path, filepath.ToSlash(path)) {
	case source.Script, source.Loc, source.Table, source.List:
		return true
	}
	return false
}
