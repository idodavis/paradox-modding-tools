// session.go is live per-workspace state: open buffers, session maps, and reindex.

package session

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

// buffer is one open file: its current text and parse result.
type buffer struct {
	text   string
	result jomini.Result
}

// Session is the live model of one workspace. All fields are guarded by mu.
type Session struct {
	WorkspaceID string
	GameID      string

	mu               sync.RWMutex
	cache            *catalog.VanillaCache
	vanillaLoc       *catalog.VanillaLoc
	defaultLang      string
	mods             []catalog.ModInput
	order            map[string]int
	locByLang        map[string]map[string]catalog.LocEntry
	defsByKey        map[string][]catalog.Def
	defsByFile       map[string][]catalog.Def
	refsByKey        map[string][]catalog.Ref
	refsByFile       map[string][]catalog.Ref
	locRefs          []catalog.Ref
	edgesByFrom      map[string][]catalog.Edge
	edgesByTo        map[string][]catalog.Edge
	edgesByFile      map[string][]catalog.Edge
	cacheByKey       map[string][]catalog.Def
	cacheRefsByKey   map[string][]catalog.Ref
	cacheEdgesByTo   map[string][]catalog.Edge
	cacheEdgesByFrom map[string][]catalog.Edge
	macroKeys        map[string]bool
	macroDefKinds    map[string]string
	// kindPrefix reverses PrefixKinds (kind -> citation prefix) so hover does
	// not scan the whole table per call.
	kindPrefix map[string]string
	// scopePrefix and scopeKind answer "a token declares it takes a <scope
	// type> — how does the user write one?": as `prefix:id`, or as a bare key
	// of some database. Both reverse a table that is otherwise scanned whole.
	scopePrefix map[string]string
	scopeKind   map[string]string
	modFireKeys map[string]string
	// modMembers are block-member key names per kind from the mods, and
	// memberSets is the lazily built per-kind set the orphan check probes. Only
	// kinds that actually carry a member convention are ever built.
	modMembers       map[string][]string
	memberSets       map[string]map[string]bool
	fieldValueKinds  map[string]string
	modFieldKinds    map[string]string
	modFieldEnums    map[string]map[string][]string
	fieldEnumsByKind map[string]map[string][]string
	// kindsWithDefs is every kind something actually defines, so a reference to a
	// kind with no definitions anywhere can be recognised as an engine concept.
	kindsWithDefs map[string]bool
	viaByTo       map[string][]catalog.Edge
	viaDirty      bool
	buffers       map[string]*buffer
	lastIndexed   map[string]string // path -> last-indexed text, for coalescing
	watcher       *Watcher
	onFS          func(path string, deleted bool)
}

// NewWithLoc is New with loc language and an optional vanilla loc sidecar.
func NewWithLoc(
	workspaceID, gameID, defaultLang string,
	cache *catalog.VanillaCache, vloc *catalog.VanillaLoc, mods []catalog.ModInput,
) *Session {
	if defaultLang == "" {
		defaultLang = "english"
	}
	s := &Session{
		WorkspaceID: workspaceID,
		GameID:      gameID,
		cache:       cache,
		vanillaLoc:  vloc,
		defaultLang: defaultLang,
		mods:        mods,
		buffers:     map[string]*buffer{},
		lastIndexed: map[string]string{},
		defsByKey:   map[string][]catalog.Def{},
		defsByFile:  map[string][]catalog.Def{},
		refsByKey:   map[string][]catalog.Ref{},
		refsByFile:  map[string][]catalog.Ref{},
		edgesByFrom: map[string][]catalog.Edge{},
		edgesByTo:   map[string][]catalog.Edge{},
		edgesByFile: map[string][]catalog.Edge{},
	}
	idx, err := catalog.BuildIndex(context.Background(), gameID, mods, cache)
	if err != nil {
		idx = catalog.Harvest{}
	}
	if cache != nil {
		catalog.PrepareCache(cache)
	}
	s.locByLang = idx.Loc
	if s.locByLang == nil {
		s.locByLang = map[string]map[string]catalog.LocEntry{}
	}
	s.order = catalog.OrderMap(idx.Order)
	s.addDefsLocked(idx.Defs)
	s.addRefsLocked(idx.Refs)
	s.addEdgesLocked(idx.Edges)
	s.modFieldKinds = idx.FieldValueKinds
	s.modFieldEnums = idx.FieldEnumsByKind
	s.modFireKeys = maps.Clone(idx.FireKeys)
	s.modMembers = idx.Structures
	s.rebuildCacheIndexLocked()
	s.rebuildFieldKindsLocked()
	s.rebuildMacroSetsLocked()
	s.rebuildMemberSetsLocked()
	s.viaDirty = true
	return s
}

// ReplaceCache swaps the vanilla Cache after a Rescan without dropping the session.
func (s *Session) ReplaceCache(c *catalog.VanillaCache) {
	s.mu.Lock()
	if c != nil {
		catalog.PrepareCache(c)
	}
	s.cache = c
	s.rebuildCacheIndexLocked()
	s.rebuildFieldKindsLocked()
	s.rebuildMacroSetsLocked()
	s.rebuildMemberSetsLocked()
	s.viaDirty = true
	s.mu.Unlock()
}

// ReplaceVanillaLoc swaps the vanilla loc sidecar after a loc-only harvest.
func (s *Session) ReplaceVanillaLoc(loc *catalog.VanillaLoc) {
	s.mu.Lock()
	s.vanillaLoc = loc
	s.mu.Unlock()
}

// SetDefaultLang sets the workspace default loc language (empty → english).
func (s *Session) SetDefaultLang(lang string) {
	if lang == "" {
		lang = "english"
	}
	s.mu.Lock()
	s.defaultLang = lang
	s.mu.Unlock()
}

// DefaultLang returns the workspace default loc language.
func (s *Session) DefaultLang() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.defaultLang == "" {
		return "english"
	}
	return s.defaultLang
}

// Result returns the parse result for an open file, or ok=false if not open.
func (s *Session) Result(path string) (jomini.Result, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if b := s.buffers[CanonPath(path)]; b != nil {
		return b.result, true
	}
	return jomini.Result{}, false
}

// DidOpen records a freshly opened file's text and indexes it.
func (s *Session) DidOpen(path, text string) { s.edit(path, text) }

// DidChange records an in-editor edit and reindexes the file.
func (s *Session) DidChange(path, text string) { s.edit(path, text) }

// DidClose drops the open buffer; the last-indexed on-disk state is kept.
func (s *Session) DidClose(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buffers, CanonPath(path))
}

// DidSave reindexes from the open buffer if present, else from disk.
func (s *Session) DidSave(path string) {
	s.mu.Lock()
	if b := s.buffers[CanonPath(path)]; b != nil {
		s.reindexLocked(path, "", b.text, jomini.Result{})
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	_ = s.reindexFromDisk(path)
}

// ExternalChange reindexes a file touched outside the editor (watcher-driven).
func (s *Session) externalChange(path string) {
	if s.reindexFromDisk(path) {
		s.mu.Lock()
		s.dropPathLocked(CanonPath(path))
		s.mu.Unlock()
		s.emitFS(path, true)
		return
	}
	s.emitFS(path, false)
}

// reindexFromDisk re-reads path into the model. deleted reports that the file is
// GONE — the only read failure that may drop its definitions.
//
// Every error used to count as deletion, and externalChange acts on it, so one
// transient failure erased every definition in the file. On Windows that is the
// common case rather than the rare one: the watcher fires on the write, the
// writer still holds an exclusive handle, and the sharing violation arrives
// before the file is readable. A permission error or a full disk did the same.
// Those now leave the model alone — stale beats empty, and the next write
// reindexes anyway.
func (s *Session) reindexFromDisk(path string) (deleted bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	text, _ := jomini.Decode(raw)
	s.mu.Lock()
	s.reindexLocked(path, "", text, jomini.Result{})
	s.mu.Unlock()
	return false
}

// edit updates the buffer (parsing once) and reindexes the file.
// The editor's spelling of the path is kept and only the buffer key is
// canonicalised. Everything harvested here carries the path through to
// Def.Path and out to go-to-definition, and the workbench keys its tabs by
// exact URI.
func (s *Session) edit(path, text string) {
	key := CanonPath(path)
	text = jomini.Normalize(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, rel, ok := s.locate(path)
	if !ok {
		return
	}
	if game.MatchExtract(s.GameID, rel).Mode == game.ModeLocKey {
		s.buffers[key] = &buffer{text: text}
		s.reindexLocked(path, rel, text, jomini.Result{Src: text})
		return
	}
	result := jomini.Parse(text)
	s.buffers[key] = &buffer{text: text, result: result}
	s.reindexLocked(path, rel, text, result)
}

// reindexLocked patches indexes for one path. Identical bytes are a no-op.
// reindexLocked harvests one file. path is kept as the caller spelled it and
// canonicalised only where a map key is needed.
//
// Overwriting path with CanonPath here put a lower-cased path into every
// definition harvested from an open file, because CanonPath lower-cases on
// Windows. That spelling then travelled all the way to the editor, which keys
// its tabs by exact URI: go-to-definition on a symbol in the file you were
// already looking at opened a second tab onto `file:///c:/users/...` beside the
// original `file:///C:/Users/...`. The same mismatch made the workbench's
// reveal-in-explorer a no-op, since it first asks whether the resource is inside
// a workspace folder and the folders are registered with their real casing.
//
// CanonPath is for comparison, never for a value anything downstream displays or
// opens. addDefsLocked already gets this right: it keys by CanonPath(d.Path)
// while leaving d.Path alone.
func (s *Session) reindexLocked(path, rel, text string, parsed jomini.Result) {
	key := CanonPath(path)
	text = jomini.Normalize(text)
	if prev, ok := s.lastIndexed[key]; ok && prev == text {
		return
	}
	origin, locatedRel, ok := s.locate(path)
	if !ok {
		return
	}
	if rel == "" {
		rel = locatedRel
	}
	s.dropPathLocked(key)
	var ex catalog.FileExtract
	if parsed.Root != nil {
		ex = catalog.ExtractParsed(s.GameID, path, rel, origin, parsed, false, s.cache)
	} else {
		ex = catalog.ExtractFile(s.GameID, path, rel, origin, text, false)
	}
	// Install files stay out of workspace harvest, but live call/$NAME$
	// sites must be queryable (peek/hover) without waiting on a rescan.
	if origin == game.OriginVanilla {
		s.indexVanillaLiveLocked(ex)
		s.lastIndexed[key] = text
		return
	}
	s.addDefsLocked(ex.Defs)
	s.addRefsLocked(ex.Refs)
	s.addEdgesLocked(ex.Edges)
	s.addMacroDefsLocked(ex.Defs, ex.Cands)
	s.addRefsLocked(catalog.ApplyCallRefs(ex.Cands, s.macroDefKinds))
	s.addEdgesLocked(catalog.ApplyCallEdges(ex.Cands, s.macroKeys))
	s.locByLang = catalog.MergeLoc(s.locByLang, ex.Loc)
	s.lastIndexed[key] = text
	s.viaDirty = true
}

// mapKeysMatching returns map keys that SamePath-match path.
func mapKeysMatching[V any](m map[string]V, path string) []string {
	var keys []string
	for k := range m {
		if SamePath(k, path) {
			keys = append(keys, k)
		}
	}
	return keys
}

// dropPathLocked removes every index entry sourced from path.
// File maps are keyed by CanonPath; harvest and Monaco may still disagree on
// case until CanonPath folds them, so every SamePath alias is dropped.
func (s *Session) dropPathLocked(path string) {
	path = CanonPath(path)
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	for _, k := range mapKeysMatching(s.defsByFile, path) {
		add(k)
	}
	for _, k := range mapKeysMatching(s.refsByFile, path) {
		add(k)
	}
	for _, k := range mapKeysMatching(s.edgesByFile, path) {
		add(k)
	}
	add(path)
	for _, key := range keys {
		s.dropFileMapsLocked(key)
	}
	s.locRefs = removeRefsPath(s.locRefs, path)
	for _, k := range mapKeysMatching(s.lastIndexed, path) {
		delete(s.lastIndexed, k)
	}
	if s.locByLang == nil {
		return
	}
	for lang, m := range s.locByLang {
		for k, v := range m {
			if SamePath(v.Path, path) {
				delete(m, k)
			}
		}
		if len(m) == 0 {
			delete(s.locByLang, lang)
		}
	}
}

// dropFileMapsLocked removes defs/refs/edges stored under one file-map key.
func (s *Session) dropFileMapsLocked(path string) {
	defs := s.defsByFile[path]
	for _, d := range defs {
		s.defsByKey[d.Key] = removeDefsPath(s.defsByKey[d.Key], path)
		if len(s.defsByKey[d.Key]) == 0 {
			delete(s.defsByKey, d.Key)
		}
		delete(s.macroKeys, d.Key)
		delete(s.macroDefKinds, d.Key)
	}
	delete(s.defsByFile, path)
	for _, r := range s.refsByFile[path] {
		s.refsByKey[r.Key] = removeRefsPath(s.refsByKey[r.Key], path)
		if len(s.refsByKey[r.Key]) == 0 {
			delete(s.refsByKey, r.Key)
		}
	}
	delete(s.refsByFile, path)
	for _, e := range s.edgesByFile[path] {
		s.edgesByFrom[e.From] = removeEdgesPath(s.edgesByFrom[e.From], path)
		if len(s.edgesByFrom[e.From]) == 0 {
			delete(s.edgesByFrom, e.From)
		}
		s.edgesByTo[e.To] = removeEdgesPath(s.edgesByTo[e.To], path)
		if len(s.edgesByTo[e.To]) == 0 {
			delete(s.edgesByTo, e.To)
		}
	}
	delete(s.edgesByFile, path)
}

func (s *Session) defsOf(key string) []catalog.Def {
	n := len(s.defsByKey[key]) + len(s.cacheByKey[key])
	if n == 0 {
		return nil
	}
	return append(append(make([]catalog.Def, 0, n), s.defsByKey[key]...), s.cacheByKey[key]...)
}

func cloneIf[T any](in []T) []T {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}

// Resolve returns the effective definition of key (winner across mods + vanilla).
func (s *Session) Resolve(key string) *catalog.Def {
	return s.ResolveMatching(key, nil)
}

// VanillaDefs returns install definitions of key (script cache, then loc sidecar).
func (s *Session) VanillaDefs(key string) []catalog.Def {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []catalog.Def
	seen := map[string]bool{}
	add := func(d catalog.Def) {
		if d.Key != key {
			return
		}
		if jomini.IsEphemeral(d.Kind) && jomini.CanonicalKind(d.Kind) != "script_param" {
			return
		}
		id := d.Kind + "\x00" + CanonPath(d.Path) + "\x00" + strconv.Itoa(d.Line)
		if seen[id] {
			return
		}
		seen[id] = true
		if d.Origin == "" {
			d.Origin = game.OriginVanilla
		}
		out = append(out, d)
	}
	for _, d := range s.cacheByKey[key] {
		add(d)
	}
	if s.vanillaLoc != nil {
		if site, ok := s.vanillaLoc.Sites[key]; ok {
			add(catalog.Def{
				Kind: "loc_key", Key: key, Path: site.Path, Line: site.Line,
				Origin: game.OriginVanilla,
			})
		}
	}
	return out
}

// ResolveMatching is Resolve after keep filters defsOf(key). keep nil keeps
// non-ephemeral defs only (flags / vars / scopes need a kind filter).
func (s *Session) ResolveMatching(key string, keep func(catalog.Def) bool) *catalog.Def {
	s.mu.RLock()
	defer s.mu.RUnlock()
	defs := s.defsOf(key)
	if keep == nil {
		keep = func(d catalog.Def) bool { return !jomini.IsEphemeral(d.Kind) }
	}
	n := 0
	for _, d := range defs {
		if keep(d) {
			defs[n] = d
			n++
		}
	}
	defs = defs[:n]
	return catalog.Winner(s.GameID, defs, s.order)
}

// LocSite returns the winning localization definition site for key.
// LocSite locates a localization key's definition.
//
// start is the byte offset of the key within the file, or 0 when it is not
// known. A workspace def carries one; the vanilla sidecar does not, because
// catalog.LocEntry stores only Value/Path/Line. Callers that navigate must
// treat 0 as "line only" rather than as column zero — see attachSite.
func (s *Session) LocSite(key string) (file string, line, start int, origin string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var locDefs []catalog.Def
	for _, d := range s.defsByKey[key] {
		if d.Kind == "loc_key" {
			locDefs = append(locDefs, d)
		}
	}
	if w := catalog.Winner(s.GameID, locDefs, s.order); w != nil {
		return w.Path, w.Line, w.Start, w.Origin, true
	}
	if s.vanillaLoc != nil {
		if site, hit := s.vanillaLoc.Sites[key]; hit {
			return site.Path, site.Line, 0, game.OriginVanilla, true
		}
	}
	return "", 0, 0, "", false
}

// Mods returns the workspace mods in the order they were supplied.
func (s *Session) Mods() []catalog.ModInput {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mods
}

// Locate finds the origin and root-relative path for an absolute file path.
// Mods win; vanilla is the install script root (InstallPath + ScriptRoot).
func (s *Session) Locate(path string) (origin, rel string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locate(path)
}

// FileText returns the buffer text if open, otherwise the on-disk decoded text.
func (s *Session) FileText(path string) string {
	s.mu.RLock()
	b := s.buffers[CanonPath(path)]
	s.mu.RUnlock()
	if b != nil {
		return b.text
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text, _ := jomini.Decode(raw)
	return text
}

// SetFileNotify registers a callback fired after each external file change.
func (s *Session) SetFileNotify(fn func(path string, deleted bool)) {
	s.mu.Lock()
	s.onFS = fn
	s.mu.Unlock()
}

func (s *Session) emitFS(path string, deleted bool) {
	s.mu.RLock()
	fn := s.onFS
	s.mu.RUnlock()
	if fn != nil {
		fn(path, deleted)
	}
}

// StartWatch begins watching mod roots for external changes.
func (s *Session) StartWatch() error {
	s.mu.Lock()
	roots := make([]string, 0, len(s.mods))
	for _, m := range s.mods {
		roots = append(roots, m.Root)
	}
	s.mu.Unlock()
	w, err := NewWatcher(roots, s.externalChange)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.watcher = w
	s.mu.Unlock()
	return nil
}

// Close stops the file watcher. Safe to call more than once.
func (s *Session) Close() {
	s.mu.Lock()
	w := s.watcher
	s.watcher = nil
	s.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

func (s *Session) locate(path string) (origin, rel string, ok bool) {
	path = CanonPath(path)
	for _, m := range s.mods {
		if r, inside := RelPath(m.Root, path); inside {
			return m.Origin, r, true
		}
	}
	for _, root := range s.vanillaRoots() {
		if r, inside := RelPath(root, path); inside {
			return game.OriginVanilla, r, true
		}
	}
	return "", "", false
}

func (s *Session) vanillaRoots() []string {
	if s.cache == nil || s.cache.InstallPath == "" {
		return nil
	}
	inst := CanonPath(s.cache.InstallPath)
	info := game.Get(s.GameID)
	if info != nil && info.ScriptRoot != "" {
		return []string{filepath.Join(inst, info.ScriptRoot), inst}
	}
	return []string{inst}
}

func removeByPath[T any](in []T, drop string, pathOf func(T) string) []T {
	out := in[:0]
	for _, x := range in {
		if !SamePath(pathOf(x), drop) {
			out = append(out, x)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func removeDefsPath(in []catalog.Def, drop string) []catalog.Def {
	return removeByPath(in, drop, func(d catalog.Def) string { return d.Path })
}
func removeRefsPath(in []catalog.Ref, drop string) []catalog.Ref {
	return removeByPath(in, drop, func(r catalog.Ref) string { return r.Path })
}
func removeEdgesPath(in []catalog.Edge, drop string) []catalog.Edge {
	return removeByPath(in, drop, func(e catalog.Edge) string { return e.Path })
}

func (s *Session) rebuildCacheIndexLocked() {
	s.cacheByKey = map[string][]catalog.Def{}
	s.cacheRefsByKey = map[string][]catalog.Ref{}
	s.cacheEdgesByTo = map[string][]catalog.Edge{}
	s.cacheEdgesByFrom = map[string][]catalog.Edge{}
	s.kindPrefix = map[string]string{}
	s.scopePrefix = map[string]string{}
	s.scopeKind = map[string]string{}
	s.kindsWithDefs = map[string]bool{}
	for _, defs := range s.defsByFile {
		for _, d := range defs {
			s.kindsWithDefs[jomini.CanonicalKind(d.Kind)] = true
		}
	}
	if s.cache == nil {
		return
	}
	for _, d := range s.cache.Defs {
		s.kindsWithDefs[jomini.CanonicalKind(d.Kind)] = true
	}
	// Reverse the prefix table once. Hover used to scan every entry per call to
	// find the citation form for a kind.
	for prefix, kind := range s.cache.PrefixKinds {
		k := jomini.CanonicalKind(kind)
		if cur, ok := s.kindPrefix[k]; !ok || len(prefix) < len(cur) {
			s.kindPrefix[k] = prefix
		}
	}
	// scope type -> the database holding its rows, and -> the prefix that cites
	// one. Completion needs both to turn "Supported Targets: culture_group"
	// into an offer of `culture_group:turkic_group`.
	for kind, scope := range s.cache.KindScope {
		if cur, ok := s.scopeKind[scope]; !ok || len(kind) < len(cur) {
			s.scopeKind[scope] = jomini.CanonicalKind(kind)
		}
	}
	for prefix, scope := range s.cache.Schema.Prefixes() {
		if cur, ok := s.scopePrefix[scope]; !ok || len(prefix) < len(cur) {
			s.scopePrefix[scope] = prefix
		}
	}
	for _, d := range s.cache.Defs {
		s.cacheByKey[d.Key] = append(s.cacheByKey[d.Key], d)
	}
	for _, r := range s.cache.LocRefs {
		s.cacheRefsByKey[r.Key] = append(s.cacheRefsByKey[r.Key], r)
	}
	for _, r := range s.cache.CallRefs {
		s.cacheRefsByKey[r.Key] = append(s.cacheRefsByKey[r.Key], r)
	}
	for _, e := range s.cache.Edges {
		s.cacheEdgesByTo[e.To] = append(s.cacheEdgesByTo[e.To], e)
		s.cacheEdgesByFrom[e.From] = append(s.cacheEdgesByFrom[e.From], e)
	}
}

func (s *Session) rebuildFieldKindsLocked() {
	var vanilla map[string]string
	if s.cache != nil {
		vanilla = s.cache.FieldValueKinds
	}
	s.fieldValueKinds = catalog.MergeFieldValueKinds(vanilla, s.modFieldKinds)
	var vanEnums map[string]map[string][]string
	if s.cache != nil {
		vanEnums = s.cache.FieldEnumsByKind
	}
	s.fieldEnumsByKind = catalog.MergeFieldEnums(vanEnums, s.modFieldEnums)
}

func (s *Session) addDefsLocked(defs []catalog.Def) {
	for _, d := range defs {
		s.defsByKey[d.Key] = append(s.defsByKey[d.Key], d)
		p := CanonPath(d.Path)
		s.defsByFile[p] = append(s.defsByFile[p], d)
	}
}

func (s *Session) addRefsLocked(refs []catalog.Ref) {
	for _, r := range refs {
		s.refsByKey[r.Key] = append(s.refsByKey[r.Key], r)
		p := CanonPath(r.Path)
		s.refsByFile[p] = append(s.refsByFile[p], r)
		if r.Kind == "loc" || r.Kind == "loc-broad" || r.Kind == "loc-convention" {
			s.locRefs = append(s.locRefs, r)
		}
	}
}

func (s *Session) addEdgesLocked(edges []catalog.Edge) {
	for _, e := range edges {
		s.edgesByFrom[e.From] = append(s.edgesByFrom[e.From], e)
		s.edgesByTo[e.To] = append(s.edgesByTo[e.To], e)
		p := CanonPath(e.Path)
		s.edgesByFile[p] = append(s.edgesByFile[p], e)
	}
}

// indexVanillaLiveLocked records macro-call and  from an open
// install file without merging defs/edges into workspace harvest.
func (s *Session) indexVanillaLiveLocked(ex catalog.FileExtract) {
	s.addRefsLocked(catalog.ApplyCallRefs(ex.Cands, s.macroDefKinds))
	var prefs []catalog.Ref
	for _, r := range ex.Refs {
		if r.Kind == "script_param" {
			prefs = append(prefs, r)
		}
	}
	s.addRefsLocked(prefs)
}

func (s *Session) addMacroDefsLocked(defs []catalog.Def, _ []catalog.CallCandidate) {
	if s.macroDefKinds == nil {
		s.macroDefKinds = map[string]string{}
	}
	if s.macroKeys == nil {
		s.macroKeys = map[string]bool{}
	}
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if !jomini.IsMacroKind(k) {
			continue
		}
		s.macroDefKinds[d.Key] = k
		s.macroKeys[d.Key] = true
	}
}

func (s *Session) rebuildMacroSetsLocked() {
	s.macroKeys = map[string]bool{}
	s.macroDefKinds = map[string]string{}
	s.eachDef(true, func(d catalog.Def) bool {
		k := jomini.CanonicalKind(d.Kind)
		if !jomini.IsMacroKind(k) {
			return true
		}
		s.macroDefKinds[d.Key] = k
		s.macroKeys[d.Key] = true
		return true
	})
}

func (s *Session) ensureViaLocked() {
	if !s.viaDirty {
		return
	}
	var stored []catalog.Edge
	for _, edges := range s.edgesByFile {
		stored = append(stored, edges...)
	}
	s.viaByTo = map[string][]catalog.Edge{}
	for _, e := range catalog.DeriveVia(stored, s.cache, s.macroKeys) {
		s.viaByTo[e.To] = append(s.viaByTo[e.To], e)
	}
	s.viaDirty = false
}

func (s *Session) allEdgesLocked() []catalog.Edge {
	s.ensureViaLocked()
	var out []catalog.Edge
	for _, edges := range s.edgesByFrom {
		out = append(out, edges...)
	}
	if s.cache != nil {
		out = append(out, s.cache.Edges...)
	}
	for _, edges := range s.viaByTo {
		out = append(out, edges...)
	}
	return out
}
