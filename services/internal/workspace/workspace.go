// Package workspace is one workspace open in the editor: the install's scanned schema and index, the
// mods' index built over it, and the files the editor has open. It decides nothing about words; the
// index's walk does that, and a query resolves names across the session's two indexes.
//
// Readers hold RLock for a whole request. The writers (Did*, Changed) take Lock themselves and are
// never called under a read lock.
package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
)

// Calendar is the eras a mod writes its dates in. A mod never states them, so the user declares
// them; with none declared, dates read as written.
type Calendar []Era

// Era is one era of a mod's calendar: its name and the year before its first (AGOT's A.C., 8000).
type Era struct {
	Name   string `json:"name"`
	Offset int    `json:"offset"`
}

// Date is a date in the named era with the greatest offset below its year ("82.1.1 A.C." for
// 8082.1.1); false where none applies.
func (calendar Calendar) Date(year, month, day int) (string, bool) {
	best := -1
	for i, era := range calendar {
		if era.Name != "" && year-era.Offset > 0 && (best < 0 || era.Offset > calendar[best].Offset) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return fmt.Sprintf("%d.%d.%d %s", year-calendar[best].Offset, month, day, calendar[best].Name), true
}

// Mod is one mod of the workspace, in the playset's order.
type Mod struct {
	ID, Dir  string
	Calendar Calendar
}

// Place is where a path sits: its file (Origin "" for a path under no root), the database it
// defines and its mod's calendar.
type Place struct {
	inventory.File
	Database *schema.Database // nil when the folder holds none
	Calendar Calendar
}

// buffer is a file open in the editor: its text and that text's parse, made once when the text
// arrives so a reader never writes.
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
func (sess *Session) Winner(defs []index.At) (winner index.At, adds, orphans []index.At, contested bool) {
	return index.Winner(sess.Game, defs, func(path string) string {
		// a path no mod lists is the install's
		if file, listed := sess.Workspace.At(path); listed {
			return file.Origin
		}
		return game.OriginVanilla
	})
}

// Fire is the definition at the far end of a fire, its database, and the scripted entries between; where it starts
// in the definition (the fire, or the scripted call that leads to it) and the definition's block it sits in there
// (option a, immediate); and the statement that fires, as written, with its other fields (trigger_event; days = 30).
type Fire struct {
	Name     string `json:"name"`
	Database string `json:"database"`
	Kind     string `json:"kind"` // what one of its database is called (game.KindName)
	Via      string `json:"via"`
	Through  int    `json:"through,omitempty"` // how many scripted entries Via names
	Path     string `json:"path"`
	Rel      string `json:"rel"`
	Origin   string `json:"origin"`
	Line     int32  `json:"line"` // 0-based
	Block    string `json:"block,omitempty"`
	Command  string `json:"command,omitempty"`
	Args     string `json:"args,omitempty"`
}

// newFire is a fire at name through the chain via, written as Fires writes it.
func newFire(name, database, via string) Fire {
	fire := Fire{Name: name, Database: database, Kind: game.KindName(database), Via: via}
	if via != "" {
		fire.Through = strings.Count(via, " → ") + 1
	}
	return fire
}

// placed is a fire placed: where start sits in from (its site and blocks), and what fire, a reference in def, fires
// with. The files are parsed once per call, in parsed.
func (sess *Session) placed(fire Fire, from, start, def, ref index.At, parsed map[string]*jomini.Result) Fire {
	file := start.File()
	fire.Path, fire.Rel, fire.Origin, fire.Line = file.Path, file.Rel, file.Origin, start.Occurrence().Line
	fire.Block, _, _ = sess.context(from, start, parsed)
	_, fire.Command, fire.Args = sess.context(def, ref, parsed)
	return fire
}

// context is where a reference sits in its definition: the definition's block it is in (with its name's last part
// where it has one: option a), the statement that holds it, and that statement's other fields as written. A list's
// weight (random_events' 100 = x) is an argument, and a block's id is its statement's.
func (sess *Session) context(def, ref index.At, parsed map[string]*jomini.Result) (block, command, args string) {
	in, start := ref.File().Path, ref.Occurrence().Range.Start
	result := parsed[in]
	if result == nil {
		if result, _, _ = sess.Parsed(in); result == nil {
			return "", "", ""
		}
		parsed[in] = result
	}
	top := result.AssignmentAt(int(def.Occurrence().Line), int(def.Occurrence().Col))
	if top == nil {
		return "", "", ""
	}
	var path []jomini.Statement
	for block := jomini.ChildBlock(top); block != nil; {
		at := slices.IndexFunc(block.Statements, func(st jomini.Statement) bool {
			return st.NodeRange().Start <= start && start < st.NodeRange().End
		})
		if at < 0 {
			break
		}
		path = append(path, block.Statements[at])
		block = jomini.ChildBlock(block.Statements[at])
	}
	if len(path) == 0 {
		return "", "", ""
	}
	key := func(st jomini.Statement) string {
		if assign, ok := st.(*jomini.Assignment); ok {
			return assign.Key.Text
		}
		return ""
	}
	leaf, last := path[len(path)-1], len(path)-1
	if name := key(leaf); name == "" || name == "id" || jomini.IsDigits(name) {
		if jomini.IsDigits(name) {
			args = name
		}
		last--
	}
	if last > 0 {
		block = key(path[0])
		for _, field := range blockFields(jomini.ChildBlock(path[0])) {
			if value, ok := field.Value.(*jomini.Scalar); ok && field.Key.Text == "name" {
				block += " " + value.Text[strings.LastIndex(value.Text, ".")+1:]
			}
		}
	}
	if last < 0 {
		return block, "", args
	}
	command = key(path[last])
	for _, field := range blockFields(jomini.ChildBlock(path[last])) {
		if jomini.Statement(field) != leaf && field.Value != nil {
			at := field.Value.NodeRange()
			args = strings.TrimPrefix(args+", "+field.Key.Text+" = "+result.Flat(at), ", ")
		}
	}
	return block, command, args
}

// blockFields is a block's assignments; none for no block.
func blockFields(block *jomini.Block) []*jomini.Assignment {
	if block == nil {
		return nil
	}
	var fields []*jomini.Assignment
	for _, st := range block.Statements {
		if assign, ok := st.(*jomini.Assignment); ok {
			fields = append(fields, assign)
		}
	}
	return fields
}

// Fires is what a definition fires, in name order: events and on_actions only (index.Fires). A
// scripted entry it runs (index.Runs) is followed to what it fires, and an entry already met on the
// way is not entered again.
func (sess *Session) Fires(from index.At) []Fire {
	s, indexes := sess.Install.Schema, sess.Indexes()
	seen, out, parsed := map[index.At]bool{from: true}, []Fire(nil), map[string]*jomini.Result{}

	// follow reads the references written in one definition; via is the entries entered to reach it, start the
	// reference in from that leads there (the zero At: the reference itself)
	var follow func(def index.At, via string, start index.At)
	follow = func(def index.At, via string, start index.At) {
		for _, ref := range index.RefsInside(def) {
			first, found := cmp.Or(start, ref), ref.Occurrence()
			switch {
			case index.Fires(s, found):
				out = append(out, sess.placed(newFire(found.ID, found.Database, via), from, first, def, ref, parsed))
			case index.Runs(s, found):
				called, _ := index.Resolve(s, indexes, found, ref.File().Path)
				for _, target := range called {
					if !seen[target] {
						seen[target] = true
						follow(target, strings.TrimPrefix(via+" → "+target.Occurrence().ID, " → "), first)
					}
				}
			}
		}
	}
	follow(from, "", index.At{})
	return byName(out)
}

// byName is fires in name order, each name once.
func byName(fires []Fire) []Fire {
	slices.SortStableFunc(fires, func(first, second Fire) int { return strings.Compare(first.Name, second.Name) })
	return slices.CompactFunc(fires, func(first, second Fire) bool { return first.Name == second.Name })
}

// FiredBy is Fires turned around, in name order: each definition holding a reference that fires
// or runs this one, a scripted entry followed to what runs it, with the chain as Fires writes it; with place, each
// placed where it starts in its definition, with the statement that fires this one (which parses its file).
func (sess *Session) FiredBy(target index.At, place bool) []Fire {
	s, indexes := sess.Install.Schema, sess.Indexes()
	seen, parsed := map[index.At]bool{target: true}, map[string]*jomini.Result{}

	// follow reads the references to one definition; a local one answers only its own file's; fired is the
	// statement that fires the target, met on the first step
	type step struct {
		def, ref index.At
	}
	var follow func(def index.At, fired *step) []Fire
	follow = func(def index.At, fired *step) (out []Fire) {
		defined := def.Occurrence()
		for _, ref := range index.RefsTo(s, indexes, defined.Database, defined.ID) {
			caller, _ := ref.Entry()
			calling, found := caller.Occurrence(), ref.Occurrence()
			inner := cmp.Or(fired, &step{caller, ref})
			switch {
			case !index.Fires(s, found) && !index.Runs(s, found) || calling.ID == "" || defined.Local && ref.File().Path != def.File().Path:
			case index.Scripted(s, calling):
				if !seen[caller] {
					seen[caller] = true
					for _, up := range follow(caller, inner) {
						up.Via = strings.TrimPrefix(up.Via+" → "+calling.ID, " → ")
						up.Through = strings.Count(up.Via, " → ") + 1
						out = append(out, up)
					}
				}
			case place:
				out = append(out, sess.placed(newFire(calling.ID, calling.Database, ""), caller, ref, inner.def, inner.ref, parsed))
			default:
				out = append(out, newFire(calling.ID, calling.Database, ""))
			}
		}
		return out
	}
	return byName(follow(target, nil))
}

// Loc is a key's entry in a language, the last loaded: a mod's over the install's. With none in the
// language it is the english one, so a title shows; whether the key exists is Resolve's to say.
func (sess *Session) Loc(key, language string) (index.At, bool) {
	entries := index.Lookup(sess.Indexes(), schema.Localizes, schema.Localization, key)
	for _, wanted := range []string{cmp.Or(language, sess.Language), game.BaseLanguage} {
		for _, entry := range slices.Backward(entries) {
			if entry.Language() == wanted {
				return entry, true
			}
		}
	}
	return index.At{}, false
}

// Entry is a definition's statement in its file's parse: the assignment whose key starts at the
// definition's line and column; nil where its file does not parse or none starts there.
func (sess *Session) Entry(def index.At) (*jomini.Result, jomini.Statement) {
	if def == (index.At{}) {
		return nil, nil
	}
	parsed, _, err := sess.Parsed(def.File().Path)
	if err != nil {
		return nil, nil
	}
	return parsed, parsed.AssignmentAt(int(def.Occurrence().Line), int(def.Occurrence().Col))
}

// Title is the display name of a definition of id: the text of the id as a loc key, else of the definition's
// first field typed as a loc key that has text (the zero At: the loc key alone).
func (sess *Session) Title(id string, def index.At) string {
	if said, found := sess.Loc(id, ""); found {
		return said.Text()
	}
	row := sess.Install.Schema.Databases[def.Occurrence().Database]
	_, entry := sess.Entry(def)
	for i, block := 0, jomini.ChildBlock(entry); row != nil && block != nil && i < len(block.Statements); i++ {
		field, isField := block.Statements[i].(*jomini.Assignment)
		if !isField {
			continue
		}
		position := sess.Install.Schema.PlaceKey(sess.Install.Schema.EntryContext(row), field, field.Key.Text, false).Position
		value, isWord := field.Value.(*jomini.Scalar)
		if !isWord || position == nil || position.Value.Value.Kind != schema.ExpectsLoc {
			continue
		}
		if said, found := sess.Loc(value.Text, ""); found {
			return said.Text()
		}
	}
	return ""
}

// LocKeys is up to limit of the keys that start with prefix, lower-cased, in order.
func (sess *Session) LocKeys(prefix string, limit int) []string {
	keys := map[string]bool{}
	for _, index := range sess.Indexes() {
		for key := range index.LocKeys() {
			if strings.HasPrefix(strings.ToLower(key), prefix) {
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
		return Place{File: file}
	}
	place := Place{File: file, Database: sess.Install.Schema.DatabaseAt(file.Rel)}
	if mod != nil {
		place.Calendar = mod.Calendar
	}
	return place
}

// Located is the indexed file at a path, else the file the path would be under a mod's or the
// install's root, and the mod it belongs to; under no root, the path alone and false.
func (sess *Session) Located(path string) (inventory.File, *Mod, bool) {
	if path == "" {
		return inventory.File{}, nil, false
	}
	path = filepath.Clean(path)
	mod := func(origin string) *Mod {
		if at := slices.IndexFunc(sess.Mods, func(mod Mod) bool { return mod.ID == origin }); at >= 0 {
			return &sess.Mods[at]
		}
		return nil
	}
	for _, index := range slices.Backward(sess.Indexes()) {
		if file, listed := index.At(path); listed {
			return file, mod(file.Origin), true
		}
	}
	under := func(roots []string, origin string) (inventory.File, bool) {
		for _, root := range roots {
			if rel, err := filepath.Rel(root, path); err == nil && filepath.IsLocal(rel) {
				rel = filepath.ToSlash(rel)
				return inventory.File{Path: path, Rel: rel, Origin: origin, Format: inventory.Classify(path, rel)}, true
			}
		}
		return inventory.File{}, false
	}
	for i := range sess.Mods {
		if file, ok := under(inventory.Roots(sess.Mods[i].Dir), sess.Mods[i].ID); ok {
			return file, &sess.Mods[i], true
		}
	}
	if file, ok := under(sess.Install.Index.Roots, game.OriginVanilla); ok {
		return file, nil, true
	}
	return inventory.File{Path: path, Format: inventory.Classify(path, filepath.ToSlash(path))}, nil, false
}

// Text is a file's text: the open buffer's, else the disk's.
func (sess *Session) Text(path string) (string, error) {
	if buffer := sess.buffers[appdata.PathKey(path)]; buffer != nil {
		return buffer.text, nil
	}
	return inventory.File{Path: path}.Text()
}

// Parsed is the parse of a file's text: an open buffer's, made once per text, else the disk's.
func (sess *Session) Parsed(path string) (*jomini.Result, string, error) {
	if buffer := sess.buffers[appdata.PathKey(path)]; buffer != nil {
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
	key := appdata.PathKey(path)
	opened := sess.buffers[key]
	if opened == nil {
		file, _, _ := sess.Located(path)
		opened = &buffer{file: file}
		sess.buffers[key] = opened
	}
	// the editor sends its text before each completion: the same text is not walked again
	if opened.text != text || opened.parsed.Root == nil {
		opened.text, opened.parsed = text, jomini.Parse(text)
		sess.changed(opened.file, text, nil)
	}
}

// DidClose forgets the editor's text; the file is read from disk again.
func (sess *Session) DidClose(path string) {
	text, err := inventory.File{Path: filepath.Clean(path)}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	key := appdata.PathKey(path)
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
	text, err := inventory.File{Path: path}.Text()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if file, _, ok := sess.Located(path); ok {
		sess.changed(file, text, err)
	}
}

// Changed is a file changed on disk outside the editor: read again (outside the lock) only when no editor holds it
// and the index reads its format; any other file is only listed or forgotten. An unreadable file keeps its records,
// since on Windows a save often hits a sharing violation; only a file that is gone loses them.
func (sess *Session) Changed(path string) {
	key := appdata.PathKey(path)
	sess.mu.RLock()
	file, _, ok := sess.Located(path)
	held := sess.buffers[key] != nil
	sess.mu.RUnlock()
	if !ok || held {
		return
	}
	text, err := "", error(nil)
	if indexable(file.Format) {
		text, err = file.Text()
	} else {
		_, err = os.Stat(path)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.buffers[key] == nil {
		sess.changed(file, text, err)
	}
}

// changed walks a mod file again from text its caller has already read, under the lock; an
// install's file, or one under no root, is never indexed from the editor.
func (sess *Session) changed(file inventory.File, text string, err error) {
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
		watcher.addTree(root, nil)
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

// addTree watches root and every folder under it that holds a file, fsnotify not being recursive,
// and gives each file to found when found is not nil.
func (watcher *Watcher) addTree(root string, found func(path string)) {
	seen := map[string]bool{}
	warn(watcher.fsw.Add(root))
	_ = inventory.Walk(root, func(path string) error {
		if dir := filepath.Dir(path); !seen[dir] {
			seen[dir] = true
			warn(watcher.fsw.Add(dir))
		}
		if found != nil {
			found(path)
		}
		return nil
	})
}

// warn logs a watch failure (inotify limits, overflow): the editor works on, missing outside changes.
func warn(err error) {
	if err != nil {
		slog.Warn("watcher failed", "error", err)
	}
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
			warn(err)
		}
	}
}

// handle reports an event: a file of any kind (the explorer lists them all), and a folder made or moved in is
// watched and each of its files reported, as no event tells of them; a path removed or renamed away may be a folder
// whose files went with it.
func (watcher *Watcher) handle(event fsnotify.Event) {
	if event.Op == fsnotify.Chmod {
		return // only its attributes: nothing the editor or the explorer shows
	}
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			watcher.addTree(event.Name, watcher.report)
			return
		}
	}
	watcher.report(event.Name)
}

// report tells onChange of name once it has been quiet for watchDebounce.
func (watcher *Watcher) report(name string) {
	watcher.mu.Lock()
	if timer := watcher.timers[name]; timer != nil {
		timer.Stop()
	}
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

// indexable is whether the index reads a file of this format (its text, for records).
func indexable(format inventory.Format) bool {
	switch format {
	case inventory.Script, inventory.Loc, inventory.Table, inventory.List:
		return true
	}
	return false
}
