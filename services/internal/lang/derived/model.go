// model.go is the vanilla cache plus the mod index, and how it is built.

package derived

import (
	"context"
	"maps"
	"path/filepath"
	"runtime"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
)

type Model struct {
	gameID string
	// lastIndexed is path -> the text this index was last built from. It is the
	// index's own dedup memo, not editor state: an editor save and a watcher
	// event for the same bytes must Reindex once.
	lastIndexed map[string]string
	locByFile   map[string][]string

	cache            *VanillaCache
	vanillaLoc       *VanillaLoc
	defaultLang      string
	mods             []ModInput
	order            map[string]int
	locByLang        map[string]map[string]LocEntry
	defsByKey        map[string][]Def
	defsByFile       map[string][]Def
	refsByKey        map[string][]Ref
	refsByFile       map[string][]Ref
	locRefsByFile    map[string][]Ref
	edgesByFrom      map[string][]Edge
	edgesByTo        map[string][]Edge
	edgesByFile      map[string][]Edge
	cacheByKey       map[string][]Def
	cacheRefsByKey   map[string][]Ref
	cacheEdgesByTo   map[string][]Edge
	cacheEdgesByFrom map[string][]Edge
	macroKeys        map[string]bool
	macroDefKinds    map[string]string
	// kindPrefix reverses PrefixKinds (kind -> citation prefix) so hover does
	// not scan the whole table per call.
	kindPrefix map[string]string
	// scopePrefix and scopeKind answer "a token declares it takes a <scope
	// type> — how does the user write one?": as `prefix:id`, or as a bare key
	// of some database. Both reverse a table that is otherwise scanned whole.
	scopePrefix     map[string]string
	scopeKind       map[string]string
	modFireKeys     map[string]string
	fieldValueKinds map[string]string
	modFieldKinds   map[string]string
	// kindsWithDefs is every kind something actually defines, so a reference to a
	// kind with no definitions anywhere can be recognised as an engine concept.
	kindsWithDefs map[string]bool
}

// CanonPath is for comparison, never for a value anything downstream displays or
// opens. addDefs already gets this right: it keys by CanonPath(d.Path)
// while leaving d.Path alone.
func (m *Model) Reindex(path, rel, text string, parsed jomini.Result) {
	key := CanonPath(path)
	text = jomini.Normalize(text)
	if prev, ok := m.lastIndexed[key]; ok && prev == text {
		return
	}
	origin, locatedRel, ok := m.Locate(path)
	if !ok {
		return
	}
	if rel == "" {
		rel = locatedRel
	}
	m.DropPath(key)
	var ex FileExtract
	if parsed.Root != nil {
		ex = ExtractParsed(m.gameID, path, rel, origin, parsed, false, m.cache)
	} else {
		ex = ExtractFile(m.gameID, path, rel, origin, text, false)
	}
	// Install files stay out of workspace harvest, but live call/$NAME$
	// sites must be queryable (peek/hover) without waiting on a rescan.
	if origin == game.OriginVanilla {
		m.indexVanillaLive(ex)
		m.lastIndexed[key] = text
		return
	}
	m.addDefs(ex.Defs)
	m.addRefs(ex.Refs)
	m.addEdges(ex.Edges)
	m.addMacroDefs(ex.Defs, ex.Cands)
	m.addRefs(ApplyCallRefs(ex.Cands, m.macroDefKinds))
	m.addEdges(ApplyCallEdges(ex.Cands, m.macroKeys))
	m.locByLang = MergeLoc(m.locByLang, ex.Loc)
	m.recordLocFile(ex.Loc)
	m.lastIndexed[key] = text
}

// DropPath removes every index entry sourced from path.
// File maps are keyed by CanonPath; harvest and Monaco may still disagree on
// case until CanonPath folds them, so every SamePath alias is dropped.
func (m *Model) DropPath(path string) {
	path = CanonPath(path)
	m.dropFileMaps(path)
	delete(m.lastIndexed, path)
	delete(m.locRefsByFile, path)
	if m.locByLang == nil {
		return
	}
	// The path check stays: two files may define the same key, and only the one
	// that this path actually contributed may be dropped.
	for _, k := range m.locByFile[path] {
		for lang, byKey := range m.locByLang {
			if v, ok := byKey[k]; ok && SamePath(v.Path, path) {
				delete(byKey, k)
			}
			if len(byKey) == 0 {
				delete(m.locByLang, lang)
			}
		}
	}
	delete(m.locByFile, path)
}

// dropFileMaps removes defs/refs/edges stored under one file-map key.
// recordLocFile notes which keys a loc file contributed, so DropPath can find
// them again without scanning the corpus.
func (m *Model) recordLocFile(locd LocDelta) {
	for k, v := range locd.Vals {
		p := CanonPath(v.Path)
		if p == "" {
			continue
		}
		m.locByFile[p] = append(m.locByFile[p], k)
	}
}

// rebuildLocByFile indexes the whole loc corpus by path once, at build time.
func (m *Model) rebuildLocByFile() {
	m.locByFile = map[string][]string{}
	for _, byKey := range m.locByLang {
		for k, v := range byKey {
			if p := CanonPath(v.Path); p != "" {
				m.locByFile[p] = append(m.locByFile[p], k)
			}
		}
	}
}

func (m *Model) dropFileMaps(path string) {

	defs := m.defsByFile[path]
	for _, d := range defs {
		m.defsByKey[d.Key] = removeDefsPath(m.defsByKey[d.Key], path)
		if len(m.defsByKey[d.Key]) == 0 {
			delete(m.defsByKey, d.Key)
		}
		delete(m.macroKeys, d.Key)
		delete(m.macroDefKinds, d.Key)
	}
	delete(m.defsByFile, path)
	for _, r := range m.refsByFile[path] {
		m.refsByKey[r.Key] = removeRefsPath(m.refsByKey[r.Key], path)
		if len(m.refsByKey[r.Key]) == 0 {
			delete(m.refsByKey, r.Key)
		}
	}
	delete(m.refsByFile, path)
	for _, e := range m.edgesByFile[path] {
		m.edgesByFrom[e.From] = removeEdgesPath(m.edgesByFrom[e.From], path)
		if len(m.edgesByFrom[e.From]) == 0 {
			delete(m.edgesByFrom, e.From)
		}
		m.edgesByTo[e.To] = removeEdgesPath(m.edgesByTo[e.To], path)
		if len(m.edgesByTo[e.To]) == 0 {
			delete(m.edgesByTo, e.To)
		}
	}
	delete(m.edgesByFile, path)
}

func (m *Model) defsOf(key string) []Def {
	n := len(m.defsByKey[key]) + len(m.cacheByKey[key])
	if n == 0 {
		return nil
	}
	return append(append(make([]Def, 0, n), m.defsByKey[key]...), m.cacheByKey[key]...)
}

func (m *Model) vanillaRoots() []string {
	if m.cache == nil || m.cache.InstallPath == "" {
		return nil
	}
	inst := CanonPath(m.cache.InstallPath)
	info := game.Get(m.gameID)
	if info != nil && info.ScriptRoot != "" {
		return []string{filepath.Join(inst, info.ScriptRoot), inst}
	}
	return []string{inst}
}

func (m *Model) rebuildCacheIndex() {
	m.cacheByKey = map[string][]Def{}
	m.cacheRefsByKey = map[string][]Ref{}
	m.cacheEdgesByTo = map[string][]Edge{}
	m.cacheEdgesByFrom = map[string][]Edge{}
	m.kindPrefix = map[string]string{}
	m.scopePrefix = map[string]string{}
	m.scopeKind = map[string]string{}
	m.kindsWithDefs = map[string]bool{}
	for _, defs := range m.defsByFile {
		for _, d := range defs {
			m.kindsWithDefs[jomini.CanonicalKind(d.Kind)] = true
		}
	}
	if m.cache == nil {
		return
	}
	for prefix, kind := range m.cache.PrefixKinds {
		k := jomini.CanonicalKind(kind)
		if cur, ok := m.kindPrefix[k]; !ok || len(prefix) < len(cur) {
			m.kindPrefix[k] = prefix
		}
	}
	// scope type -> the database holding its rows, and -> the prefix that cites
	// one. Completion needs both to turn "Supported Targets: culture_group"
	// into an offer of `culture_group:turkic_group`.
	for kind, scope := range m.cache.KindScope {
		if cur, ok := m.scopeKind[scope]; !ok || len(kind) < len(cur) {
			m.scopeKind[scope] = jomini.CanonicalKind(kind)
		}
	}
	for prefix, scope := range m.cache.Schema.Prefixes() {
		if cur, ok := m.scopePrefix[scope]; !ok || len(prefix) < len(cur) {
			m.scopePrefix[scope] = prefix
		}
	}
	for _, d := range m.cache.Defs {
		m.kindsWithDefs[jomini.CanonicalKind(d.Kind)] = true
		m.cacheByKey[d.Key] = append(m.cacheByKey[d.Key], d)
	}
	for _, r := range m.cache.LocRefs {
		m.cacheRefsByKey[r.Key] = append(m.cacheRefsByKey[r.Key], r)
	}
	for _, r := range m.cache.CallRefs {
		m.cacheRefsByKey[r.Key] = append(m.cacheRefsByKey[r.Key], r)
	}
	for _, e := range m.cache.Edges {
		m.cacheEdgesByTo[e.To] = append(m.cacheEdgesByTo[e.To], e)
		m.cacheEdgesByFrom[e.From] = append(m.cacheEdgesByFrom[e.From], e)
	}
}

func (m *Model) rebuildFieldKinds() {
	var vanilla map[string]string
	if m.cache != nil {
		vanilla = m.cache.FieldValueKinds
	}
	m.fieldValueKinds = MergeFieldValueKinds(vanilla, m.modFieldKinds)
}

func (m *Model) addDefs(defs []Def) {
	for _, d := range defs {
		m.defsByKey[d.Key] = append(m.defsByKey[d.Key], d)
		p := CanonPath(d.Path)
		m.defsByFile[p] = append(m.defsByFile[p], d)
	}
}

func (m *Model) addRefs(refs []Ref) {
	for _, r := range refs {
		m.refsByKey[r.Key] = append(m.refsByKey[r.Key], r)
		p := CanonPath(r.Path)
		m.refsByFile[p] = append(m.refsByFile[p], r)
		if r.Kind == "loc" || r.Kind == "loc-broad" {
			m.locRefsByFile[p] = append(m.locRefsByFile[p], r)
		}
	}
}

// indexVanillaLive records macro-call and  from an open
// install file without merging defs/edges into workspace harvest.
func (m *Model) indexVanillaLive(ex FileExtract) {
	m.addRefs(ApplyCallRefs(ex.Cands, m.macroDefKinds))
	var prefs []Ref
	for _, r := range ex.Refs {
		if r.Kind == "script_param" {
			prefs = append(prefs, r)
		}
	}
	m.addRefs(prefs)
}

func (m *Model) addMacroDefs(defs []Def, _ []CallCandidate) {
	if m.macroDefKinds == nil {
		m.macroDefKinds = map[string]string{}
	}
	if m.macroKeys == nil {
		m.macroKeys = map[string]bool{}
	}
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if !jomini.IsMacroKind(k) {
			continue
		}
		m.macroDefKinds[d.Key] = k
		m.macroKeys[d.Key] = true
	}
}

func (m *Model) rebuildMacroSets() {
	m.macroKeys = map[string]bool{}
	m.macroDefKinds = map[string]string{}
	m.EachDef(true, func(d Def) bool {
		k := jomini.CanonicalKind(d.Kind)
		if !jomini.IsMacroKind(k) {
			return true
		}
		m.macroDefKinds[d.Key] = k
		m.macroKeys[d.Key] = true
		return true
	})
}

func NewModel(
	gameID, defaultLang string,
	cache *VanillaCache, vloc *VanillaLoc, mods []ModInput,
) *Model {
	m := &Model{
		gameID:        gameID,
		lastIndexed:   map[string]string{},
		locByFile:     map[string][]string{},
		locRefsByFile: map[string][]Ref{},
		cache:         cache,
		vanillaLoc:    vloc,
		defaultLang:   defaultLang,
		mods:          mods,
		defsByKey:     map[string][]Def{},
		defsByFile:    map[string][]Def{},
		refsByKey:     map[string][]Ref{},
		refsByFile:    map[string][]Ref{},
		edgesByFrom:   map[string][]Edge{},
		edgesByTo:     map[string][]Edge{},
		edgesByFile:   map[string][]Edge{},
	}
	idx, err := BuildIndex(context.Background(), gameID, mods, cache)
	if err != nil {
		idx = Harvest{}
	}
	if cache != nil {
		PrepareCache(cache)
	}
	m.locByLang = idx.Loc
	if m.locByLang == nil {
		m.locByLang = map[string]map[string]LocEntry{}
	}
	m.order = OrderMap(idx.Order)
	m.addDefs(idx.Defs)
	m.addRefs(idx.Refs)
	m.addEdges(idx.Edges)
	m.modFieldKinds = idx.FieldValueKinds
	m.modFireKeys = maps.Clone(idx.FireKeys)
	m.rebuildLocByFile()
	m.rebuildProjections()
	return m
}

// rebuildProjections re-derives everything computed FROM the cache and the mod
// index. Three calls in a fixed order, and they were written out at all three
// sites that change the model — so a fourth projection added later would have
// been missed at one of them.
func (m *Model) rebuildProjections() {
	m.rebuildCacheIndex()
	m.rebuildFieldKinds()
	m.rebuildMacroSets()
}

// ReplaceCache swaps the vanilla cache after a rescan, keeping the mod index.
func (m *Model) ReplaceCache(c *VanillaCache) {
	if c != nil {
		PrepareCache(c)
	}
	m.cache = c
	m.rebuildProjections()
}

// ReplaceVanillaLoc swaps the vanilla loc sidecar after a loc-only harvest.
func (m *Model) ReplaceVanillaLoc(vloc *VanillaLoc) { m.vanillaLoc = vloc }

// SetDefaultLang sets the workspace default loc language.
func (m *Model) SetDefaultLang(lang string) { m.defaultLang = lang }

// DefaultLang is the workspace default loc language, english when unset.
func (m *Model) DefaultLang() string {
	if m.defaultLang == "" {
		return "english"
	}
	return m.defaultLang
}

// ModRoots are the mod directories, in load order.
func (m *Model) ModRoots() []string {
	roots := make([]string, 0, len(m.mods))
	for _, mi := range m.mods {
		roots = append(roots, mi.Root)
	}
	return roots
}

// CanonPath returns a cleaned path for index and buffer identity.
// On Windows the result is lowercased so harvest and Monaco fsPath share a key.
func CanonPath(p string) string {
	if p == "" {
		return p
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// SamePath reports whether a and b name the same file. On Windows the
// comparison is case-insensitive after Clean.
func SamePath(a, b string) bool {
	a, b = CanonPath(a), CanonPath(b)
	if a == b {
		return true
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return false
}

// RelPath reports whether file is inside root and returns the relative path.
func RelPath(root, file string) (rel string, ok bool) {
	root, file = CanonPath(root), CanonPath(file)
	r, err := filepath.Rel(root, file)
	if err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return r, true
	}
	if runtime.GOOS != "windows" {
		return "", false
	}
	rl, fl := strings.ToLower(root), strings.ToLower(file)
	sep := string(filepath.Separator)
	if fl == rl {
		return ".", true
	}
	if !strings.HasPrefix(fl, rl+sep) {
		return "", false
	}
	return file[len(root)+1:], true
}

// This makes the fourteen policies comparable; it does not merge them. Each
// caller still states its own key order on one line, and two callers differing
// is now a visible difference rather than a hidden one.
func firstIn[V comparable](m map[string]V, keys ...string) V {
	var zero V
	for _, k := range keys {
		if v, ok := m[k]; ok && v != zero {
			return v
		}
	}
	return zero
}
func (m *Model) addEdges(edges []Edge) {
	for _, e := range edges {
		m.edgesByFrom[e.From] = append(m.edgesByFrom[e.From], e)
		m.edgesByTo[e.To] = append(m.edgesByTo[e.To], e)
		p := CanonPath(e.Path)
		m.edgesByFile[p] = append(m.edgesByFile[p], e)
	}
}

func removeRefsPath(in []Ref, drop string) []Ref {
	return removeByPath(in, drop, func(r Ref) string { return r.Path })
}

func removeDefsPath(in []Def, drop string) []Def {
	return removeByPath(in, drop, func(d Def) string { return d.Path })
}

func removeEdgesPath(in []Edge, drop string) []Edge {
	return removeByPath(in, drop, func(e Edge) string { return e.Path })
}

// Cache is the vanilla model this workspace sits on, or nil before a scan.
func (m *Model) Cache() *VanillaCache { return m.cache }

// GameID is the game this model was built for.
func (m *Model) KindPrefix(kind string) string { return m.kindPrefix[kind] }

func (m *Model) DropLocIndex() { m.locByLang = map[string]map[string]LocEntry{} }
