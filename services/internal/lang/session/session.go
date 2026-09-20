// Package session holds the live state of one open workspace: the install
// index shared with every other workspace on the same game, the workspace's
// own index built over it, the mods in load order, and the editor buffers.
// lsp and views read a Session under its RWMutex, one lock per request, and
// read the two indexes as fields; the methods here are the questions that read
// both halves or compute. lifecycle.go is the Pool and the Watcher.
package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

type buffer struct {
	text   string
	result jomini.Result
}

// Session is one workspace. Readers hold RLock for a whole request; the Did*/Replace*/Set* writers take Lock themselves.
type Session struct {
	WorkspaceID string
	GameID      string

	sync.RWMutex
	Install, Work *index.Index
	Mods          []index.ModInput
	order         map[string]int
	lang          string
	last          map[string]string // path → the text its records were built from
	buffers       map[string]*buffer
	watcher       *Watcher
	onFS          func(path string, deleted bool)
}

// New builds a session over an install index and the workspace mods.
func New(workspaceID, gameID, defaultLang string, cache *index.Index, mods []index.ModInput) *Session {
	if cache == nil {
		cache = index.NewIndex(gameID, nil)
	}
	work, err := index.BuildWorkspace(context.Background(), gameID, mods, cache)
	if err != nil || work == nil {
		work = index.NewIndex(gameID, cache.Declared)
	}
	return &Session{
		WorkspaceID: workspaceID, GameID: gameID, Install: cache, Work: work, Mods: mods,
		order: index.OrderMap(work.Order), lang: defaultLang,
		last: map[string]string{}, buffers: map[string]*buffer{},
	}
}

// ReplaceCache swaps the install index after a rescan and rebuilds the workspace over it.
func (s *Session) ReplaceCache(c *index.Index) {
	s.Lock()
	defer s.Unlock()
	s.Install = c
	if work, err := index.BuildWorkspace(context.Background(), s.GameID, s.Mods, c); err == nil {
		s.Work = work
	}
	s.last = map[string]string{}
}

// MergeVanillaLoc folds a freshly harvested language into the install index.
func (s *Session) MergeVanillaLoc(x *index.Index) {
	s.Lock()
	s.Install.MergeLoc(x)
	s.Unlock()
}

func (s *Session) SetDefaultLang(lang string) {
	s.Lock()
	s.lang = lang
	s.Unlock()
}

// DefaultLang is the workspace default loc language, english when unset.
func (s *Session) DefaultLang() string {
	if s.lang == "" {
		return "english"
	}
	return s.lang
}

func (s *Session) ModRoots() []string {
	roots := make([]string, 0, len(s.Mods))
	for _, m := range s.Mods {
		roots = append(roots, m.Root)
	}
	return roots
}

// ---------------------------------------------------------------- buffers

// Result returns the parse result for an open file, or ok=false if not open.
func (s *Session) Result(path string) (jomini.Result, bool) {
	if b := s.buffers[index.CanonPath(path)]; b != nil {
		return b.result, true
	}
	return jomini.Result{}, false
}

// FileText returns the buffer text if open, otherwise the on-disk decoded text.
func (s *Session) FileText(path string) string {
	if b := s.buffers[index.CanonPath(path)]; b != nil {
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

func (s *Session) DidOpen(path, text string)   { s.edit(path, text) }
func (s *Session) DidChange(path, text string) { s.edit(path, text) }

// DidClose drops the open buffer; the last-indexed on-disk state is kept.
func (s *Session) DidClose(path string) {
	s.Lock()
	delete(s.buffers, index.CanonPath(path))
	s.Unlock()
}

// DidSave reindexes from the open buffer if present, else from disk.
func (s *Session) DidSave(path string) {
	s.Lock()
	if b := s.buffers[index.CanonPath(path)]; b != nil {
		s.reindex(path, "", b.text, jomini.Result{})
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
		s.DropPath(path)
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
	s.reindex(path, "", text, jomini.Result{})
	s.Unlock()
	return false
}

// edit updates the buffer (parsing once) and reindexes the file. The editor's spelling of the path is kept: it carries through to Def.Path and go-to-definition, and the workbench keys its tabs by exact URI.
func (s *Session) edit(path, text string) {
	key := index.CanonPath(path)
	text = jomini.Normalize(text)
	s.Lock()
	defer s.Unlock()
	_, rel, ok := s.Locate(path)
	if !ok {
		return
	}
	if game.MatchExtract(s.GameID, rel).Mode == game.ModeLocKey {
		s.buffers[key] = &buffer{text: text}
		s.reindex(path, rel, text, jomini.Result{Src: text})
		return
	}
	result := jomini.Parse(text)
	s.buffers[key] = &buffer{text: text, result: result}
	s.reindex(path, rel, text, result)
}

// reindex rebuilds one file's records, once per distinct text.
func (s *Session) reindex(path, rel, text string, parsed jomini.Result) {
	key := index.CanonPath(path)
	text = jomini.Normalize(text)
	if prev, ok := s.last[key]; ok && prev == text {
		return
	}
	origin, locatedRel, ok := s.Locate(path)
	if !ok {
		return
	}
	if rel == "" {
		rel = locatedRel
	}
	if origin == game.OriginVanilla {
		origin = ""
	}
	s.DropPath(path)
	f := index.NewFile(s.GameID, path, rel, origin, 0)
	if f.Rule.Mode == game.ModeLocKey {
		f.Src = text
	} else {
		f.Parse(text, parsed)
	}
	s.Work.IndexFile(f, s.Install)
	s.Work.SortLocKeys()
	s.last[key] = text
}

// DropPath removes every workspace record sourced from path.
func (s *Session) DropPath(path string) {
	if id := s.Work.PathID(path); id >= 0 {
		s.Work.Drop(id)
		s.Work.SortLocKeys()
	}
	delete(s.last, index.CanonPath(path))
}

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

// ------------------------------------------------------------------- files

// Locate is what a path is: the index answers for a file it has seen, the mod roots for one the editor has just opened.
func (s *Session) Locate(path string) (origin, rel string, ok bool) {
	for _, x := range []*index.Index{s.Work, s.Install} {
		if id := x.PathID(path); id >= 0 {
			if f := x.FileOf(id); f.Rel != "" {
				return originOf(f.Origin), f.Rel, true
			}
		}
	}
	for _, mi := range s.Mods {
		if r, inside := index.RelPath(mi.Root, path); inside {
			return mi.Origin, r, true
		}
	}
	if inst := s.Install.InstallPath; inst != "" {
		roots := []string{inst}
		if info := game.Get(s.GameID); info != nil && info.ScriptRoot != "" {
			roots = []string{filepath.Join(inst, info.ScriptRoot), inst}
		}
		for _, root := range roots {
			if r, inside := index.RelPath(root, path); inside {
				return game.OriginVanilla, r, true
			}
		}
	}
	return "", "", false
}

func originOf(origin string) string {
	if origin == "" {
		return game.OriginVanilla
	}
	return origin
}

func (s *Session) KindFor(path string) string {
	_, rel, ok := s.Locate(path)
	if !ok {
		rel = path
	}
	return index.ClassifyRel(strings.ReplaceAll(rel, "\\", "/"), filepath.Base(path))
}

func (s *Session) DisplayRel(path string) string {
	if path == "" {
		return ""
	}
	if _, rel, ok := s.Locate(path); ok {
		return filepath.ToSlash(rel)
	}
	if inst := s.Install.InstallPath; inst != "" {
		if rel, err := filepath.Rel(inst, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func (s *Session) OriginName(origin string) string {
	if origin == "" || origin == game.OriginVanilla {
		if g := game.Get(s.GameID); g != nil && g.Name != "" {
			return g.Name
		}
		return "Game"
	}
	for _, mi := range s.Mods {
		if mi.Origin == origin && mi.Name != "" {
			return mi.Name
		}
	}
	return origin
}

func (s *Session) CalendarFor(path string) index.ModCalendar {
	if origin, _, ok := s.Locate(path); ok {
		for _, mi := range s.Mods {
			if mi.Origin == origin {
				return mi.Calendar
			}
		}
	}
	return index.ModCalendar{}
}

func (s *Session) IsMetaFile(path string) bool {
	l := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	return s.KindFor(path) == "meta" && strings.HasSuffix(l, "/"+game.MetadataDir+"/"+game.MetadataFile)
}

func (s *Session) IsLocalDefFile(path, extractKind string) bool {
	return extractKind == "mod_descriptor" || s.IsMetaFile(path)
}

// ------------------------------------------------------------- definitions

// Defs is every definition of a key: the workspace's, the install's, and the data tier's when neither has one.
func (s *Session) Defs(key string) []index.Def {
	out := append(s.Work.DefsAt(s.Work.DefsByKey[key]), s.Install.DefsAt(s.Install.DefsByKey[key])...)
	if len(out) == 0 {
		if d := s.Install.DataTier(); d != nil {
			return d.DefsAt(d.DefsByKey[key])
		}
	}
	return out
}

func (s *Session) Resolve(key string) *index.Def { return s.ResolveMatching(key, nil) }

// ResolveMatching is the winning definition of a key among those keep admits; the default drops ephemerals.
func (s *Session) ResolveMatching(key string, keep func(index.Def) bool) *index.Def {
	if keep == nil {
		keep = func(d index.Def) bool { return !jomini.IsEphemeral(d.Kind) }
	}
	defs := slices.DeleteFunc(s.Defs(key), func(d index.Def) bool { return !keep(d) })
	return index.Winner(s.GameID, defs, s.order)
}

// ResolveNonLoc resolves a word to an object, never to a namespace unless nothing else of that name resolves.
func (s *Session) ResolveNonLoc(word string) *index.Def {
	if d := s.ResolveMatching(word, func(d index.Def) bool { return jomini.IsObjectKind(d.Kind) }); d != nil {
		return d
	}
	return s.ResolveMatching(word, index.IsCitableDef)
}

// ResolveOfKind is the winning definition of one kind, confined to a script_param owner when one is given.
func (s *Session) ResolveOfKind(word, kind, owner string) *index.Def {
	if word == "" || kind == "" {
		return nil
	}
	ck := jomini.CanonicalKind(kind)
	return s.ResolveMatching(word, func(d index.Def) bool {
		return jomini.CanonicalKind(d.Kind) == ck && (owner == "" || d.OwnerKey == "" || d.OwnerKey == owner)
	})
}

// ModDefs is every workspace definition of a key.
func (s *Session) ModDefs(key string) []index.Def { return s.Work.DefsAt(s.Work.DefsByKey[key]) }

// VanillaDefs is every install definition of a key, the data tier included, plus the install's own localization of it.
func (s *Session) VanillaDefs(key string) []index.Def {
	defs := s.Install.DefsAt(s.Install.DefsByKey[key])
	if len(defs) == 0 {
		if d := s.Install.DataTier(); d != nil {
			defs = d.DefsAt(d.DefsByKey[key])
		}
	}
	if e, ok := s.Install.LocAt(s.DefaultLang(), key); ok {
		defs = append(defs, index.Def{Kind: jomini.KindLocKey, Key: key, Path: e.Path, Line: e.Line})
	}
	var out []index.Def
	seen := map[string]bool{}
	for _, d := range defs {
		if d.Key != key || jomini.IsEphemeral(d.Kind) && jomini.CanonicalKind(d.Kind) != jomini.KindScriptParam {
			continue
		}
		id := d.Kind + "\x00" + index.CanonPath(d.Path) + "\x00" + strconv.Itoa(d.Line)
		if !seen[id] {
			seen[id] = true
			d.Origin = game.OriginVanilla
			out = append(out, d)
		}
	}
	return out
}

func (s *Session) DefsInFile(path string) []index.Def {
	return s.Work.DefsAt(s.Work.DefsByFile[s.Work.PathID(path)])
}

func (s *Session) RefsInFile(path string) []index.Ref {
	return s.Work.RefsAt(s.Work.RefsByFile[s.Work.PathID(path)])
}

func (s *Session) RefsTo(key string) []index.Ref {
	if key == "" {
		return nil
	}
	return append(s.Work.RefsAt(s.Work.RefsByKey[key]), s.Install.RefsAt(s.Install.RefsByKey[key])...)
}

// WorkRefs is every live workspace reference.
func (s *Session) WorkRefs() []index.Ref {
	out := make([]index.Ref, 0, len(s.Work.Refs))
	for i := range s.Work.Refs {
		if s.Work.Refs[i].PathID >= 0 {
			out = append(out, s.Work.RefsAt([]int32{int32(i)})...)
		}
	}
	return out
}

// Kinds is every database a name is a row of, which is what the classifier asks of a session.
func (s *Session) Kinds(name string) []string {
	var out []string
	for _, d := range s.Defs(name) {
		if k := jomini.CanonicalKind(d.Kind); index.IsCitableDef(d) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// IsLoc reports a localization key, for the classifier.
func (s *Session) IsLoc(name string) bool { return s.LocDefined(name) }

// KindHasDefs reports whether anything defines an object of this kind; a reference to a kind with no definitions anywhere names an engine concept.
func (s *Session) KindHasDefs(kind string) bool {
	ck := jomini.CanonicalKind(kind)
	return len(s.Work.DefsByKind[ck]) > 0 || len(s.Install.DefsByKind[ck]) > 0 || s.Install.Tree.IsData(ck)
}

// FindDefsOf returns defs whose keys contain query, or start with it if byPrefix. An empty kind searches every kind and drops ephemerals.
func (s *Session) FindDefsOf(query, kind string, limit int, byPrefix, includeVanilla bool) []index.Def {
	if byPrefix && limit <= 0 {
		return nil
	}
	q, want := strings.ToLower(query), ""
	if kind != "" {
		want = jomini.CanonicalKind(kind)
	}
	var out []index.Def
	visit := func(d index.Def) bool {
		k := strings.ToLower(d.Key)
		if want == "" && jomini.IsEphemeral(d.Kind) || q != "" && !(byPrefix && strings.HasPrefix(k, q) || !byPrefix && strings.Contains(k, q)) {
			return true
		}
		out = append(out, d)
		return limit <= 0 || len(out) < limit
	}
	indexes := []*index.Index{s.Work}
	if includeVanilla {
		indexes = append(indexes, s.Install)
		if want != "" && s.Install.Tree.IsData(want) {
			if d := s.Install.DataTier(); d != nil {
				indexes = append(indexes, d)
			}
		}
	}
	for _, x := range indexes {
		if want == "" {
			x.EachDef(visit)
		} else {
			for _, d := range x.DefsAt(x.DefsByKind[want]) {
				if !visit(d) {
					return out
				}
			}
		}
		if limit > 0 && len(out) >= limit {
			return out
		}
	}
	return out
}

// Macros are the scripted_* definitions whose key starts with query, as key and kind.
func (s *Session) Macros(query string, limit int) []index.Def {
	q := strings.ToLower(query)
	var out []index.Def
	for key, kind := range s.Work.Tree.Macros {
		if strings.HasPrefix(strings.ToLower(key), q) {
			out = append(out, index.Def{Kind: kind, Key: key})
		}
	}
	slices.SortFunc(out, func(a, b index.Def) int { return strings.Compare(a.Key, b.Key) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Params are a macro's parameters, sorted.
func (s *Session) Params(owner string) []string {
	if owner == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, x := range []*index.Index{s.Work, s.Install} {
		for _, d := range x.DefsAt(x.DefsByKind[jomini.KindScriptParam]) {
			if d.OwnerKey == owner && d.Key != "" {
				seen[d.Key] = true
			}
		}
	}
	return slices.Sorted(func(yield func(string) bool) {
		for k := range seen {
			if !yield(k) {
				return
			}
		}
	})
}

// ValuesOf is how often each value was written for one ephemeral name, under the named owner or, when its calls say nothing, across every owner.
func (s *Session) ValuesOf(kind, owner, name string) map[string]int {
	ck := jomini.CanonicalKind(kind)
	out := map[string]int{}
	for _, at := range []string{owner, ""} {
		k := ck + "\x00" + at + "\x00" + name
		for _, x := range []*index.Index{s.Work, s.Install} {
			for v, n := range x.Values[k] {
				out[v] += int(n)
			}
		}
		if len(out) > 0 || owner == "" {
			return out
		}
	}
	return out
}

// Contests returns contested override keys for the live workspace.
func (s *Session) Contests(order []string) []index.Contest {
	var mods, vanilla []index.Def
	rewritesData := false
	s.Work.EachDef(func(d index.Def) bool {
		mods = append(mods, d)
		rewritesData = rewritesData || s.Install.Tree.IsData(d.Kind)
		return true
	})
	s.Install.EachDef(func(d index.Def) bool { vanilla = append(vanilla, d); return true })
	if rewritesData {
		if d := s.Install.DataTier(); d != nil {
			d.EachDef(func(def index.Def) bool { vanilla = append(vanilla, def); return true })
		}
	}
	return index.Contests(s.GameID, mods, vanilla, order)
}

// ------------------------------------------------------------- localization

func (s *Session) DefaultLoc(key string) (string, bool) {
	lang := s.DefaultLang()
	if e, ok := s.Work.LocAt(lang, key); ok {
		return e.Value, true
	}
	if e, ok := s.Install.LocAt(lang, key); ok && e.Value != "" {
		return e.Value, true
	}
	return "", false
}

// LocSite is where a key is written: the default language's file, and another language's only when it has none.
func (s *Session) LocSite(key string) (file string, line, start int, origin string, ok bool) {
	lang := s.DefaultLang()
	for _, x := range []*index.Index{s.Work, s.Install} {
		if e, hit := x.LocAt(lang, key); hit {
			return e.Path, e.Line, e.Start, originOf(e.Origin), true
		}
	}
	for _, x := range []*index.Index{s.Work, s.Install} {
		for other := range x.Loc {
			if e, hit := x.LocAt(other, key); hit && other != lang {
				return e.Path, e.Line, e.Start, originOf(e.Origin), true
			}
		}
	}
	return "", 0, 0, "", false
}

func (s *Session) LocDefined(key string) bool {
	if _, ok := s.DefaultLoc(key); ok {
		return true
	}
	_, _, _, _, ok := s.LocSite(key)
	return ok
}

// LocKeys returns up to limit workspace and install loc keys starting with prefix.
func (s *Session) LocKeys(prefix string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := make(map[string]bool, limit)
	out := s.Work.LocKeys(prefix, limit, seen, make([]string, 0, limit))
	return s.Install.LocKeys(prefix, limit, seen, out)
}

// LocFile returns a workspace loc file path for origin and lang, if indexed.
func (s *Session) LocFile(origin, lang string) (string, bool) {
	marker := "_l_" + strings.ToLower(lang)
	for id, meta := range s.Work.Files {
		if p := s.Work.Paths[id]; meta.Origin == origin && meta.Kind == "loc" && strings.Contains(strings.ToLower(p), marker) {
			return p, true
		}
	}
	return "", false
}

// LocConventions are the loc key templates a kind's definitions carry.
func (s *Session) LocConventions(kind string) []loc.Convention {
	if r := s.Install.Tree.Root(kind, false); r != nil && len(r.Loc) > 0 {
		return r.Loc
	}
	if r := s.Install.Tree.Root(jomini.CanonicalKind(kind), false); r != nil {
		return r.Loc
	}
	return nil
}

// InheritedLoc is the vanilla keys a workspace inherits: the default-language table every language falls back to, plus the extra keys a translated table adds.
type InheritedLoc struct {
	base   map[string]bool
	byLang map[string]map[string]bool
}

func (i *InheritedLoc) Has(lang, key string) bool {
	return i != nil && (i.base[key] || i.byLang[lang][key])
}

func (i *InheritedLoc) Each(lang string, fn func(key string)) {
	if i == nil {
		return
	}
	for k := range i.base {
		fn(k)
	}
	for k := range i.byLang[lang] {
		fn(k)
	}
}

func (s *Session) InheritedLocKeys(langs []string) *InheritedLoc {
	out := &InheritedLoc{base: map[string]bool{}, byLang: map[string]map[string]bool{}}
	for k := range s.Install.Loc[s.DefaultLang()] {
		if k != "" {
			out.base[k] = true
		}
	}
	for _, lang := range langs {
		if lang == "" || lang == s.DefaultLang() || out.byLang[lang] != nil {
			continue
		}
		extra := map[string]bool{}
		for k := range s.Install.Loc[lang] {
			if k != "" && !out.base[k] {
				extra[k] = true
			}
		}
		out.byLang[lang] = extra
	}
	return out
}

// ------------------------------------------------------------------- schema

// Node is the learned position a chain of block keys addresses under a kind: the install's answer, then the workspace's where the install never wrote there.
func (s *Session) Node(kind string, chain ...string) *index.Node {
	if n := s.Install.Tree.At(kind, chain...); n != nil {
		return n
	}
	return s.Work.Tree.At(kind, chain...)
}

// ------------------------------------------------------------------- graph

const maxViaHops = 3

func (s *Session) edges(id string, out bool) []index.Edge {
	if out {
		return append(s.Work.EdgesAt(s.Work.EdgesFrom[id]), s.Install.EdgesAt(s.Install.EdgesFrom[id])...)
	}
	return append(s.Work.EdgesAt(s.Work.EdgesTo[id]), s.Install.EdgesAt(s.Install.EdgesTo[id])...)
}

// Edges are the fire edges leaving id (out) or arriving at it, mods then vanilla, then the hops synthesized through chains of macro calls. A macro is a hop, not a node.
func (s *Session) Edges(id string, out bool) []index.Edge {
	if id == "" {
		return nil
	}
	macros := s.Work.Tree.Macros
	edges := s.edges(id, out)
	type step struct {
		at    string
		chain []string
		fire  index.Edge
	}
	var q []step
	seen, visited := map[string]bool{id: true}, map[string]bool{}
	for _, e := range edges {
		far, hop := e.To, e.Kind == index.EdgeKindCall
		if !out {
			far, hop = e.From, e.Kind != index.EdgeKindCall
		}
		seen[far] = true
		if hop && macros[far] != "" && !visited[far] && (!out || macros[id] == "") {
			visited[far] = true
			q = append(q, step{far, []string{far}, e})
		}
	}
	for len(q) > 0 {
		st := q[0]
		q = q[1:]
		for _, e := range s.edges(st.at, out) {
			far, hop := e.To, e.Kind == index.EdgeKindCall
			if !out {
				far, hop = e.From, true
				if e.Kind != index.EdgeKindCall {
					continue
				}
			}
			if hop && macros[far] != "" {
				if !visited[far] && len(st.chain) < maxViaHops {
					visited[far] = true
					chain := append(append([]string{}, st.chain...), far)
					if !out {
						chain = append([]string{far}, st.chain...)
					}
					q = append(q, step{far, chain, st.fire})
				}
				continue
			}
			if out && hop || seen[far] {
				continue
			}
			seen[far] = true
			via := index.Edge{Via: strings.Join(st.chain, " → "), Kind: index.EdgeKindVia}
			if out {
				via.From, via.To, via.Path, via.Line, via.Rescoped = id, e.To, e.Path, e.Line, e.Rescoped
			} else {
				via.From, via.To, via.Path, via.Line, via.Rescoped = far, id, st.fire.Path, st.fire.Line, st.fire.Rescoped
			}
			edges = append(edges, via)
		}
	}
	return edges
}
