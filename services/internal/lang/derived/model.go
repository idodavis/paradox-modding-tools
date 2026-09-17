// model.go is the query surface: three indexes (the install, its data-row tier
// and the workspace), the mods and their load order, and one method per
// question the editor asks. Every lookup reads the workspace index first and
// falls through to the install's; nothing here is a projection, so nothing can
// go stale after an edit.

package derived

import (
	"context"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// Model is one workspace's view of the world.
type Model struct {
	gameID  string
	vanilla *Index
	work    *Index
	// lastIndexed is path -> the text this index was last built from: the index's own dedup memo, so an editor save and a watcher event for the same bytes Reindex once.
	lastIndexed map[string]string
	mods        []ModInput
	order       map[string]int
	defaultLang string
}

// NewModel builds the workspace index over an install index and returns the model.
func NewModel(gameID, defaultLang string, vanilla *Index, mods []ModInput) *Model {
	if vanilla == nil {
		// An unscanned install is an empty index, not a nil one: every query reads both.
		vanilla = NewIndex(gameID)
	}
	work, err := BuildWorkspace(context.Background(), gameID, mods, vanilla)
	if err != nil || work == nil {
		work = NewIndex(gameID)
	}
	return &Model{
		gameID: gameID, vanilla: vanilla, work: work,
		lastIndexed: map[string]string{}, mods: mods,
		order: OrderMap(work.Order), defaultLang: defaultLang,
	}
}

// cells is the learned schema every workspace query reads: the install's, with the workspace's own macro definitions folded in.
func (m *Model) cells() *Schema { return &m.work.Schema }

// ReplaceCache swaps the install index after a rescan, keeping the workspace one.
func (m *Model) ReplaceCache(x *Index) {
	m.vanilla = x
	if x != nil {
		macros := m.work.Schema.MacroDefs
		m.work.Schema = x.Schema
		m.work.Schema.MacroDefs = maps.Clone(x.Schema.MacroDefs)
		if m.work.Schema.MacroDefs == nil {
			m.work.Schema.MacroDefs = map[string]string{}
		}
		for k, v := range macros {
			if m.work.Schema.MacroDefs[k] == "" {
				m.work.Schema.MacroDefs[k] = v
			}
		}
		m.work.Declared, m.work.Docs = x.Declared, x.Docs
	}
}

// MergeVanillaLoc folds a freshly harvested language into the install index, which is how a loc-language switch avoids a rescan.
func (m *Model) MergeVanillaLoc(src *Index) { m.vanilla.MergeLoc(src) }

// SetDefaultLang sets the workspace default loc language.
func (m *Model) SetDefaultLang(lang string) { m.defaultLang = lang }

// DefaultLang is the workspace default loc language, english when unset.
func (m *Model) DefaultLang() string {
	if m.defaultLang == "" {
		return "english"
	}
	return m.defaultLang
}

// Mods are the workspace's mods, and ModRoots their directories in load order.
func (m *Model) Mods() []ModInput { return m.mods }

func (m *Model) ModRoots() []string {
	roots := make([]string, 0, len(m.mods))
	for _, mi := range m.mods {
		roots = append(roots, mi.Root)
	}
	return roots
}

func (m *Model) CacheInfo() (installPath, scannedAt, gameVersion, installID string) {
	if m.vanilla == nil {
		return "", "", "", ""
	}
	return m.vanilla.InstallPath, m.vanilla.ScannedAt, m.vanilla.GameVersion, m.vanilla.InstallID
}

// DataError reports why the data tier could not be read, empty when it could.
func (m *Model) DataError() string { return m.vanilla.DataError() }

// Schema is the declared type system, or nil when the user has not run script_docs. Callers must degrade rather than guess.
func (m *Model) Schema() *declared.Schema {
	if m.vanilla == nil {
		return nil
	}
	return m.vanilla.Declared
}

func (m *Model) docs() Docs {
	if m.vanilla == nil {
		return Docs{}
	}
	return m.vanilla.Docs
}

// KindInfo is the install's own prose about a database folder.
func (m *Model) KindInfo(kind string) string { return m.docs().KindInfo[kind] }

// NestedShapes are the nested databases the corpus found.
func (m *Model) NestedShapes() []NestedShape { return m.cells().Nested }

// ------------------------------------------------------------------ lookups

// defsOf is every definition of a key, workspace first, falling through to the install and finally to the data-row tier.
func (m *Model) defsOf(key string) []Def {
	out := m.work.defsAt(m.work.DefsByKey[key])
	out = append(out, m.vanilla.defsAt(m.vanilla.DefsByKey[key])...)
	if len(out) == 0 {
		// A data row's id is the only name that reaches here unplaced, so the miss is what pays for the tier.
		if d := m.vanilla.dataTier(); d != nil {
			return d.defsAt(d.DefsByKey[key])
		}
	}
	return out
}

func (m *Model) Resolve(key string) *Def { return m.ResolveMatching(key, nil) }

func (m *Model) ResolveMatching(key string, keep func(Def) bool) *Def {
	defs := m.defsOf(key)
	if keep == nil {
		keep = func(d Def) bool { return !jomini.IsEphemeral(d.Kind) }
	}
	n := 0
	for _, d := range defs {
		if keep(d) {
			defs[n] = d
			n++
		}
	}
	return Winner(m.gameID, defs[:n], m.order)
}

// ResolveNonLoc resolves a word to an object, never to a namespace unless nothing else of that name resolves. What kind of thing a name denotes is not something load order gets to decide: a mod writing `namespace = conqueror` outranked vanilla's `conqueror` trait.
func (m *Model) ResolveNonLoc(word string) *Def {
	if d := m.ResolveMatching(word, func(d Def) bool {
		return jomini.IsObjectKind(d.Kind)
	}); d != nil {
		return d
	}
	return m.ResolveMatching(word, IsCitableDef)
}

func (m *Model) ResolveOfKind(word, kind string) *Def {
	return m.ResolveOfKindOwner(word, kind, "")
}

func (m *Model) ResolveOfKindOwner(word, kind, owner string) *Def {
	if word == "" || kind == "" {
		return nil
	}
	ck := jomini.CanonicalKind(kind)
	return m.ResolveMatching(word, func(d Def) bool {
		if jomini.CanonicalKind(d.Kind) != ck {
			return false
		}
		return owner == "" || d.OwnerKey == "" || d.OwnerKey == owner
	})
}

// ModDefsOf is every workspace definition of a key.
func (m *Model) ModDefsOf(key string) []Def { return m.work.defsAt(m.work.DefsByKey[key]) }

// VanillaDefs is every install definition of a key, the data tier included, plus the install's own localization of it.
func (m *Model) VanillaDefs(key string) []Def {
	defs := m.vanilla.defsAt(m.vanilla.DefsByKey[key])
	if len(defs) == 0 {
		if d := m.vanilla.dataTier(); d != nil {
			defs = d.defsAt(d.DefsByKey[key])
		}
	}
	var out []Def
	seen := map[string]bool{}
	add := func(d Def) {
		if d.Key != key ||
			jomini.IsEphemeral(d.Kind) && jomini.CanonicalKind(d.Kind) != jomini.KindScriptParam {
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
	for _, d := range defs {
		add(d)
	}
	if e, ok := m.vanilla.locAt(m.DefaultLang(), key); ok {
		add(Def{Kind: jomini.KindLocKey, Key: key, Path: e.Path, Line: e.Line,
			Origin: game.OriginVanilla})
	}
	return out
}

func (m *Model) RefsTo(key string) []Ref {
	if key == "" {
		return nil
	}
	return append(m.work.refsAt(m.work.RefsByKey[key]),
		m.vanilla.refsAt(m.vanilla.RefsByKey[key])...)
}

// RefsInFile returns workspace refs sourced from path.
func (m *Model) RefsInFile(path string) []Ref {
	return m.work.refsAt(m.work.RefsByFile[m.work.PathID(path)])
}

// DefsInFile returns workspace defs declared in path.
func (m *Model) DefsInFile(path string) []Def {
	return m.work.defsAt(m.work.DefsByFile[m.work.PathID(path)])
}

// WorkspaceRefs returns harvested workspace refs (never the install's).
func (m *Model) WorkspaceRefs() []Ref {
	out := make([]Ref, 0, len(m.work.Refs))
	for _, r := range m.work.Refs {
		if r.PathID >= 0 {
			r.Path, r.Kind = m.work.Paths[r.PathID], m.work.KindOf(r.KindID)
			out = append(out, r)
		}
	}
	return out
}

// LocRefs returns workspace loc and loc-broad refs.
func (m *Model) LocRefs() []Ref {
	var out []Ref
	for _, r := range m.work.Refs {
		if k := m.work.KindOf(r.KindID); r.PathID >= 0 && isLocKind(k) {
			r.Path, r.Kind = m.work.Paths[r.PathID], k
			out = append(out, r)
		}
	}
	return out
}

// EachDef walks workspace defs, then the install's if includeVanilla. fn false stops.
func (m *Model) EachDef(includeVanilla bool, fn func(Def) bool) {
	stopped := false
	m.work.eachDef(func(d Def) bool {
		if !fn(d) {
			stopped = true
			return false
		}
		return true
	})
	if includeVanilla && !stopped {
		m.vanilla.eachDef(fn)
	}
}

// DefCount is the number of workspace definitions.
func (m *Model) DefCount() int {
	n := 0
	for _, d := range m.work.Defs {
		if d.PathID >= 0 {
			n++
		}
	}
	return n
}

// KindHasDefs reports whether anything in the workspace or the install defines an object of this kind. A reference to a kind with no definitions anywhere is an engine concept rather than a missing object.
func (m *Model) KindHasDefs(kind string) bool {
	ck := jomini.CanonicalKind(kind)
	if len(m.work.DefsByKind[ck]) > 0 || len(m.vanilla.DefsByKind[ck]) > 0 {
		return true
	}
	// A tier kind is still defined by the install, only kept elsewhere.
	return game.IsDataKind(m.gameID, ck)
}

// FindDefsOf returns defs whose keys contain query, or start with it if byPrefix. An empty kind searches every kind and drops ephemeral defs; a named kind narrows to CanonicalKind(kind) and keeps them, because asking for `saved_scope` by name is asking for the ephemeral ones.
func (m *Model) FindDefsOf(
	query, kind string, limit int, byPrefix, includeVanilla bool,
) []Def {
	if byPrefix && limit <= 0 {
		return nil
	}
	q := strings.ToLower(query)
	want := ""
	if kind != "" {
		want = jomini.CanonicalKind(kind)
	}
	var out []Def
	visit := func(d Def) bool {
		if want == "" && jomini.IsEphemeral(d.Kind) {
			return true
		}
		if q != "" {
			k := strings.ToLower(d.Key)
			hit := strings.Contains(k, q)
			if byPrefix {
				hit = strings.HasPrefix(k, q)
			}
			if !hit {
				return true
			}
		}
		out = append(out, d)
		return limit <= 0 || len(out) < limit
	}
	if want != "" {
		for _, x := range m.indexes(includeVanilla) {
			for _, d := range x.defsAt(x.DefsByKind[want]) {
				if !visit(d) {
					return out
				}
			}
		}
	} else {
		m.EachDef(includeVanilla, visit)
	}
	if limit > 0 && len(out) >= limit {
		return out
	}
	// The tier is out of the main indexes so a kindless search never pays for it.
	if includeVanilla && want != "" && game.IsDataKind(m.gameID, want) {
		if d := m.vanilla.dataTier(); d != nil {
			for _, def := range d.defsAt(d.DefsByKind[want]) {
				if !visit(def) {
					return out
				}
			}
		}
	}
	return out
}

func (m *Model) indexes(includeVanilla bool) []*Index {
	if includeVanilla {
		return []*Index{m.work, m.vanilla}
	}
	return []*Index{m.work}
}

// FindMacroDefs returns scripted_* macro defs whose keys start with query.
func (m *Model) FindMacroDefs(query string, limit int) []Def {
	if limit <= 0 {
		return nil
	}
	q := strings.ToLower(query)
	var out []Def
	m.EachDef(true, func(d Def) bool {
		if !jomini.IsMacroKind(d.Kind) {
			return true
		}
		if q != "" && !strings.HasPrefix(strings.ToLower(d.Key), q) {
			return true
		}
		out = append(out, d)
		return len(out) < limit
	})
	return out
}

func (m *Model) FindParamsOf(owner string, limit int) []string {
	if owner == "" || limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, x := range m.indexes(true) {
		for _, d := range x.defsAt(x.DefsByKind[jomini.KindScriptParam]) {
			if d.OwnerKey != owner || d.Key == "" || seen[d.Key] {
				continue
			}
			seen[d.Key] = true
			out = append(out, d.Key)
			if len(out) >= limit {
				slices.Sort(out)
				return out
			}
		}
	}
	slices.Sort(out)
	return out
}

const maxMacroParams = 32

// MacroUsage is a macro's call signature. A macro with no parameters gets nothing: `invoked by name` already says all there is to say about it.
func (m *Model) MacroUsage(d *Def) string {
	if d == nil || d.Key == "" || !jomini.IsMacroKind(d.Kind) {
		return ""
	}
	names := m.FindParamsOf(d.Key, maxMacroParams)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(d.Key)
	b.WriteString(" = {\n")
	for _, n := range names {
		b.WriteString("\t")
		b.WriteString(n)
		b.WriteString(" = <value>\n")
	}
	b.WriteString("}")
	return b.String()
}

// IsMacroDef reports that key is a harvested scripted-macro definition.
func (m *Model) IsMacroDef(key string) bool { return m.cells().MacroDefs[key] != "" }

// ------------------------------------------------------------- localization

func (m *Model) DefaultLoc(key string) (string, bool) {
	lang := m.DefaultLang()
	if e, ok := m.work.locAt(lang, key); ok {
		return e.Value, true
	}
	if e, ok := m.vanilla.locAt(lang, key); ok && e.Value != "" {
		return e.Value, true
	}
	return "", false
}

// LocSite is where a key is written: the default language's file, and another language's only when it has none.
func (m *Model) LocSite(key string) (file string, line, start int, origin string, ok bool) {
	lang := m.DefaultLang()
	for _, x := range []*Index{m.work, m.vanilla} {
		if e, hit := x.locAt(lang, key); hit {
			return e.Path, e.Line, e.Start, originOf(e.Origin), true
		}
	}
	for _, x := range []*Index{m.work, m.vanilla} {
		for other := range x.Loc {
			if other == lang {
				continue
			}
			if e, hit := x.locAt(other, key); hit {
				return e.Path, e.Line, e.Start, originOf(e.Origin), true
			}
		}
	}
	return "", 0, 0, "", false
}

func originOf(origin string) string {
	if origin == "" {
		return game.OriginVanilla
	}
	return origin
}

func (m *Model) LocDefined(key string) bool {
	if _, ok := m.DefaultLoc(key); ok {
		return true
	}
	_, _, _, _, ok := m.LocSite(key)
	return ok
}

// LocKeys returns up to limit workspace and install loc keys starting with prefix, by binary search over each language's sorted key list.
func (m *Model) LocKeys(prefix string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	lp := strings.ToLower(prefix)
	seen := make(map[string]bool, limit)
	out := make([]string, 0, limit)
	for _, x := range []*Index{m.work, m.vanilla} {
		if x == nil {
			continue
		}
		for _, keys := range x.LocKeysSorted {
			for i := sort.SearchStrings(keys, prefix); i < len(keys); i++ {
				k := keys[i]
				if !strings.HasPrefix(strings.ToLower(k), lp) {
					if !strings.HasPrefix(k, prefix) {
						break
					}
					continue
				}
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, k)
				if len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

// EachLoc walks every indexed workspace loc entry; fn must not call back into the session.
func (m *Model) EachLoc(fn func(lang, key string, e LocEntry)) {
	for lang, byKey := range m.work.Loc {
		for k, e := range byKey {
			e.Path = m.work.PathOf(e.PathID)
			fn(lang, k, e)
		}
	}
}

// LocFile returns a workspace loc file path for origin and lang, if indexed.
func (m *Model) LocFile(origin, lang string) (string, bool) {
	marker := "_l_" + strings.ToLower(lang)
	for id, meta := range m.work.Files {
		if meta.Origin != origin || meta.Kind != "loc" {
			continue
		}
		if p := m.work.Paths[id]; strings.Contains(strings.ToLower(p), marker) {
			return p, true
		}
	}
	return "", false
}

// InheritedLoc is the vanilla keys a workspace inherits. base is the default-language table, which every language falls back to; byLang holds only the extra keys a translated table adds. CK3's base is 287,896 keys, so materialising it twelve times for a twelve-language mod was never affordable.
type InheritedLoc struct {
	base   map[string]bool
	byLang map[string]map[string]bool
}

// Has reports that lang inherits key from vanilla.
func (i *InheritedLoc) Has(lang, key string) bool {
	if i == nil {
		return false
	}
	return i.base[key] || i.byLang[lang][key]
}

// Each calls fn once for every key lang inherits, base first.
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

// InheritedLocKeys is what a workspace falls back to. The default-language table is inherited by EVERY language -- gating it on lang == defLang reported each vanilla key missing once per translation a mod ships.
func (m *Model) InheritedLocKeys(langs []string) *InheritedLoc {
	out := &InheritedLoc{base: map[string]bool{}, byLang: map[string]map[string]bool{}}
	if m.vanilla == nil {
		return out
	}
	for k := range m.vanilla.Loc[m.DefaultLang()] {
		if k != "" {
			out.base[k] = true
		}
	}
	for _, lang := range langs {
		if lang == "" || lang == m.DefaultLang() || out.byLang[lang] != nil {
			continue
		}
		extra := map[string]bool{}
		for k := range m.vanilla.Loc[lang] {
			if k != "" && !out.base[k] {
				extra[k] = true
			}
		}
		out.byLang[lang] = extra
	}
	return out
}

// ------------------------------------------------------------------- files

func (m *Model) vanillaRoots() []string {
	if m.vanilla == nil || m.vanilla.InstallPath == "" {
		return nil
	}
	inst := CanonPath(m.vanilla.InstallPath)
	if info := game.Get(m.gameID); info != nil && info.ScriptRoot != "" {
		return []string{filepath.Join(inst, info.ScriptRoot), inst}
	}
	return []string{inst}
}

// Locate is what a path is: the index answers for a file it has seen, and the mod roots for one the editor has just opened.
func (m *Model) Locate(path string) (origin, rel string, ok bool) {
	for _, x := range []*Index{m.work, m.vanilla} {
		if id := x.PathID(path); id >= 0 {
			if f := x.FileOf(id); f.Rel != "" {
				return originOf(f.Origin), f.Rel, true
			}
		}
	}
	path = CanonPath(path)
	for _, mi := range m.mods {
		if r, inside := RelPath(mi.Root, path); inside {
			return mi.Origin, r, true
		}
	}
	for _, root := range m.vanillaRoots() {
		if r, inside := RelPath(root, path); inside {
			return game.OriginVanilla, r, true
		}
	}
	return "", "", false
}

func (m *Model) KindFor(path string) string {
	_, rel, ok := m.Locate(path)
	if !ok {
		rel = path
	}
	return ClassifyRel(strings.ReplaceAll(rel, "\\", "/"), filepath.Base(path))
}

func (m *Model) DisplayRel(path string) string {
	if path == "" {
		return ""
	}
	if _, rel, ok := m.Locate(path); ok {
		return filepath.ToSlash(rel)
	}
	if inst, _, _, _ := m.CacheInfo(); inst != "" {
		if rel, err := filepath.Rel(inst, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func (m *Model) OriginName(origin string) string {
	if origin == "" || origin == game.OriginVanilla {
		if g := game.Get(m.gameID); g != nil && g.Name != "" {
			return g.Name
		}
		return "Game"
	}
	for _, mi := range m.mods {
		if mi.Origin == origin && mi.Name != "" {
			return mi.Name
		}
	}
	return origin
}

func (m *Model) CalendarFor(path string) ModCalendar {
	origin, _, ok := m.Locate(path)
	if !ok {
		return ModCalendar{}
	}
	for _, mi := range m.mods {
		if mi.Origin == origin {
			return mi.Calendar
		}
	}
	return ModCalendar{}
}

func (m *Model) IsMetaFile(path string) bool {
	if m.KindFor(path) != "meta" {
		return false
	}
	l := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	return strings.HasSuffix(l, "/"+game.MetadataDir+"/"+game.MetadataFile)
}

func (m *Model) IsLocalDefFile(path, extractKind string) bool {
	return extractKind == "mod_descriptor" || m.IsMetaFile(path)
}

// CanonPath returns a cleaned path for index and buffer identity, lowercased on Windows so harvest and Monaco fsPath share a key. Never for a value anything downstream displays or opens.
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

// SamePath reports whether a and b name the same file, case-insensitively on Windows.
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

// ------------------------------------------------------------------- edits

// Reindex rebuilds one file's entries.
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
	m.DropPath(path)
	id := fileID{GameID: m.gameID, Path: filepath.Clean(path), Origin: origin,
		Rule: game.MatchExtract(m.gameID, rel)}
	if origin == game.OriginVanilla {
		id.Origin = ""
	}
	var h *FileHarvest
	if id.Rule.Mode == game.ModeLocKey {
		h = ApplyLoc(id, text)
	} else {
		res := parsed
		if res.Root == nil {
			res = jomini.Parse(text)
		}
		ff := FactsOf(m.gameID, path, rel, id.Origin, res)
		h = Apply(ff, m.Schema())
		m.applyOne(h, rel)
	}
	// Install files stay out of the workspace harvest, but live call and `$NAME$` sites must be queryable without waiting on a rescan.
	if origin == game.OriginVanilla {
		kept := h.Refs[:0]
		for _, r := range h.Refs {
			if r.Kind == jomini.KindScriptParam || jomini.IsMacroKind(r.Kind) {
				kept = append(kept, r)
			}
		}
		h.Defs, h.Refs, h.Edges, h.Loc = nil, kept, nil, LocDelta{}
	}
	m.work.Add(h.Path, rel, h.Origin,
		ClassifyRel(strings.ReplaceAll(rel, "\\", "/"), filepath.Base(path)), h)
	m.work.SortLocKeys()
	m.lastIndexed[key] = text
}

// applyOne is Phase B for one edited file: the install's schema applied as it stands, against the install's definitions, the workspace's and the file's own.
func (m *Model) applyOne(h *FileHarvest, rel string) {
	one := NewIndex(m.gameID)
	one.Schema, one.Declared = *m.cells(), m.Schema()
	one.Schema.Cells = nil
	one.Add(h.Path, rel, h.Origin, "", &FileHarvest{fileID: h.fileID, Defs: h.Defs})
	one.learn(context.Background(), []*fileRecs{h.recs}, "", m.vanilla, m.work)
	h.Defs, h.Edges, h.recs = nil, one.Edges, nil
	for _, d := range one.Defs {
		if d.PathID >= 0 {
			h.Defs = append(h.Defs, d)
		}
	}
	for _, r := range one.Refs {
		// The scratch index interned the kinds into a table of its own; the workspace's Add interns them again from the name.
		r.Kind = one.KindOf(r.KindID)
		h.Refs = append(h.Refs, r)
	}
	m.adoptCells(one, h.Path)
}

// adoptCells keeps what one file learned at positions nothing else has written, and files them under it so Drop retracts them with its other records.
func (m *Model) adoptCells(one *Index, path string) {
	at := m.work.intern(path, "", "", "")
	var own []Cell
	for kind, byPath := range one.Schema.Cells {
		for p, byKey := range byPath {
			for key, cell := range byKey {
				if m.work.Schema.TypeOf(Cell{kind, p, key}) != nil {
					continue
				}
				m.work.Schema.at(kind, p, key)
				m.work.Schema.Cells[kind][p][key] = cell
				own = append(own, Cell{Kind: kind, Path: p, Key: key})
			}
		}
	}
	if m.work.CellsByFile == nil {
		m.work.CellsByFile = map[int32][]Cell{}
	}
	if len(own) > 0 {
		m.work.CellsByFile[at] = own
	} else {
		delete(m.work.CellsByFile, at)
	}
}

// DropPath removes every index entry sourced from path.
func (m *Model) DropPath(path string) {
	if id := m.work.PathID(path); id >= 0 {
		m.work.Drop(id)
		m.work.SortLocKeys()
	}
	delete(m.lastIndexed, CanonPath(path))
}

// ------------------------------------------------------------------- graph

// maxViaHops caps the macro chain a via edge may cross.
const maxViaHops = 3

func (m *Model) edgesFromKey(id string) []Edge {
	return append(m.work.edgesAt(m.work.EdgesFrom[id]),
		m.vanilla.edgesAt(m.vanilla.EdgesFrom[id])...)
}

func (m *Model) edgesToKey(id string) []Edge {
	return append(m.work.edgesAt(m.work.EdgesTo[id]),
		m.vanilla.edgesAt(m.vanilla.EdgesTo[id])...)
}

// EdgesFrom are the edges leaving id, mods then vanilla, then the via hops.
func (m *Model) EdgesFrom(id string) []Edge {
	if id == "" {
		return nil
	}
	out := m.edgesFromKey(id)
	type step struct {
		macro string
		chain []string
	}
	var q []step
	seen, visited := map[string]bool{id: true}, map[string]bool{}
	// A macro is a hop, not a node: nothing is synthesized when id is one.
	if !m.IsMacroDef(id) {
		for _, e := range out {
			seen[e.To] = true
			if e.Kind == EdgeKindCall && m.IsMacroDef(e.To) && !visited[e.To] {
				visited[e.To] = true
				q = append(q, step{e.To, []string{e.To}})
			}
		}
	}
	for len(q) > 0 {
		st := q[0]
		q = q[1:]
		for _, e := range m.edgesFromKey(st.macro) {
			if e.Kind == EdgeKindCall {
				if m.IsMacroDef(e.To) && !visited[e.To] && len(st.chain) < maxViaHops {
					visited[e.To] = true
					q = append(q, step{e.To, append(append([]string{}, st.chain...), e.To)})
				}
				continue
			}
			if seen[e.To] {
				continue
			}
			seen[e.To] = true
			out = append(out, Edge{
				From: id, To: e.To, Via: strings.Join(st.chain, " → "),
				Path: e.Path, Line: e.Line, Kind: EdgeKindVia, Rescoped: e.Rescoped,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EdgesTo are the edges arriving at id, mods then vanilla, then the via hops.
func (m *Model) EdgesTo(id string) []Edge {
	if id == "" {
		return nil
	}
	out := m.edgesToKey(id)
	type step struct {
		chain []string
		fire  Edge
	}
	var q []step
	seen, visited := map[string]bool{id: true}, map[string]bool{}
	for _, e := range out {
		seen[e.From] = true
		if e.Kind != EdgeKindCall && m.IsMacroDef(e.From) && !visited[e.From] {
			visited[e.From] = true
			q = append(q, step{[]string{e.From}, e})
		}
	}
	for len(q) > 0 {
		st := q[0]
		q = q[1:]
		for _, e := range m.edgesToKey(st.chain[0]) {
			if e.Kind != EdgeKindCall {
				continue
			}
			caller := e.From
			if m.IsMacroDef(caller) {
				if !visited[caller] && len(st.chain) < maxViaHops {
					visited[caller] = true
					q = append(q, step{append([]string{caller}, st.chain...), st.fire})
				}
				continue
			}
			if seen[caller] {
				continue
			}
			seen[caller] = true
			out = append(out, Edge{
				From: caller, To: id, Via: strings.Join(st.chain, " → "),
				Path: st.fire.Path, Line: st.fire.Line, Kind: EdgeKindVia,
				Rescoped: st.fire.Rescoped,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EventRootScope is the scope an event runs in, read from the on_actions that fire it.
func (m *Model) EventRootScope(sc *declared.Schema, event string) string {
	found := ""
	for _, e := range m.EdgesTo(event) {
		if e.Rescoped {
			continue
		}
		scope := sc.OnActions[strings.ToLower(e.From)]
		if scope == "" || scope == "none" {
			continue
		}
		if found == "" {
			found = scope
			continue
		}
		if found != scope {
			return ""
		}
	}
	return found
}

// GraphCatalog returns the kind of every fire-edge endpoint and the macro def keys.
func (m *Model) GraphCatalog() (kinds map[string]string, effects map[string]bool) {
	kinds = map[string]string{}
	effects = map[string]bool{}
	for k := range m.cells().MacroDefs {
		effects[k] = true
	}
	endpoints := map[string]bool{}
	for _, x := range []*Index{m.work, m.vanilla} {
		if x == nil {
			continue
		}
		for _, e := range x.Edges {
			if e.PathID < 0 || e.Kind == EdgeKindCall {
				continue
			}
			if e.From != "" {
				endpoints[e.From] = true
			}
			if e.To != "" {
				endpoints[e.To] = true
			}
		}
	}
	for key := range endpoints {
		for _, x := range []*Index{m.work, m.vanilla} {
			if list := x.DefsByKey[key]; len(list) > 0 {
				kinds[key] = jomini.CanonicalKind(x.Defs[list[0]].Kind)
				break
			}
		}
	}
	return kinds, effects
}

// Contests returns contested override keys for the live workspace.
func (m *Model) Contests(order []string) []Contest {
	var mods []Def
	rewritesData := false
	m.work.eachDef(func(d Def) bool {
		mods = append(mods, d)
		rewritesData = rewritesData || game.IsDataKind(m.gameID, d.Kind)
		return true
	})
	var vanilla []Def
	if m.vanilla != nil {
		vanilla = m.vanilla.defsAt(nil)
		vanilla = make([]Def, 0, len(m.vanilla.Defs))
		m.vanilla.eachDef(func(d Def) bool { vanilla = append(vanilla, d); return true })
	}
	// A mod rewriting a character's history contests the install's row, so the tier belongs here once a mod has written one.
	if rewritesData {
		if d := m.vanilla.dataTier(); d != nil {
			d.eachDef(func(def Def) bool { vanilla = append(vanilla, def); return true })
		}
	}
	return Contests(m.gameID, mods, vanilla, order)
}

// ---------------------------------------------------------- the schema reads

// Vocab is the name list for a completion slot: the declared API for effect/trigger/vocabulary, the harvested corpus for the rest.
func (m *Model) Vocab(kind string) []string {
	l := m.cells()
	switch kind {
	case "effect":
		return l.Effects
	case "trigger":
		return l.Triggers
	case "vocabulary":
		return l.VocabNames
	case "gui_type":
		return l.GUITypes
	case "gui_prop":
		return l.GUIProps
	case "datafunction":
		return l.DataFunctions
	}
	return nil
}

// installDef is the definition set the scan folded with: the install's citable script definitions, since the data tier is not loaded to answer a keystroke.
func (m *Model) installDef(name string) bool {
	for _, i := range m.vanilla.DefsByKey[name] {
		if d := m.vanilla.Defs[i]; d.PathID >= 0 && IsCitableDef(d) {
			return true
		}
	}
	return false
}

// cellKeys is how one key may be spelled in the table: a query does not know whether the statement was a block, so it asks both ways.
func (m *Model) cellKeys(key string) []string {
	if at := foldKey(m.gameID, key, false, false, m.installDef); at != key {
		return []string{at}
	}
	// <kind> is always the second spelling, because a block key folds to it at any position where one of its siblings is a definition.
	return []string{key, segKind}
}

// CellPath folds a chain of enclosing block keys into the path a cell is filed under, by the same rule the scan used.
func (m *Model) CellPath(segs []string) string {
	out := ""
	for i, seg := range segs {
		f := foldSeg(m.gameID, seg, false, m.installDef)
		if i == 0 {
			out = f
			continue
		}
		out += "." + f
	}
	return out
}

// TypeOf is what the corpus saw at one cell: the labels its values wore, and the databases every resolvable one of them named.
func (m *Model) TypeOf(kind, path, key string) *CellType {
	for _, k := range m.cellKeys(key) {
		c := Cell{Kind: kind, Path: path, Key: k}
		if at := m.vanilla.Schema.TypeOf(c); at != nil {
			return at
		}
		if at := m.work.Schema.TypeOf(c); at != nil {
			return at
		}
	}
	return nil
}

// InstallTypeOf is the install's answer alone: a mod's own mistake must not silence the check that would have caught it.
func (m *Model) InstallTypeOf(kind, path, key string) *CellType {
	for _, k := range m.cellKeys(key) {
		if at := m.vanilla.Schema.TypeOf(Cell{Kind: kind, Path: path, Key: k}); at != nil {
			return at
		}
	}
	return nil
}

// InstallMembers are the install's keys at one position, the pair of InstallTypeOf: a reporting surface counts the evidence the install left, never the mod's own.
func (m *Model) InstallMembers(kind, path string) []string {
	return m.vanilla.Schema.Members(kind, path)
}

// Members are the keys written at one position, most-used first -- a kind's own structure at the empty path, a block's members below it.
func (m *Model) Members(kind, path string) []string {
	if out := m.vanilla.Schema.Members(kind, path); len(out) > 0 {
		return out
	}
	return m.work.Schema.Members(kind, path)
}

// IsMember reports a key the corpus writes at this position.
func (m *Model) IsMember(kind, path, key string) bool {
	return m.TypeOf(kind, path, key) != nil ||
		m.TypeOf(kind, path, strings.ToLower(key)) != nil
}

// MemberSets returns the effect/trigger maps LSP ranks with.
func (m *Model) MemberSets() (effects, triggers map[string]bool) {
	l := m.cells()
	return l.EffectSet, l.TriggerSet
}

// StructureBlock reports whether key is usually written as a block at this position.
func (m *Model) StructureBlock(kind, path, key string) bool {
	return m.TypeOf(kind, path, key).IsBlock()
}

func (m *Model) PrefixKind(prefix string) string {
	return firstIn(m.cells().Prefix, prefix, strings.ToLower(prefix))
}

// KindPrefix is the shortest citation prefix for a kind, or empty.
func (m *Model) KindPrefix(kind string) string { return m.cells().KindPrefix[kind] }

func (m *Model) CiteFormOfScope(scope string) (prefix, kind string) {
	if scope == "" {
		return "", ""
	}
	sc := strings.ToLower(scope)
	l := m.cells()
	return l.ScopePrefix[sc], l.ScopeKind[sc]
}

func (m *Model) ScopeOfKind(kind string) string {
	l := m.cells()
	if sc := l.KindScope[kind]; sc != "" {
		return sc
	}
	return l.KindScope[jomini.CanonicalKind(kind)]
}

// FieldValueKind is the one database a cell's values all named, or empty where the corpus disagreed.
func (m *Model) FieldValueKind(kind, path, key string) string {
	ks := m.TypeOf(kind, path, key).kindsOf()
	if len(ks) == 1 && ks[0] != LabelLoc {
		return ks[0]
	}
	return ""
}

func (t *CellType) kindsOf() []string {
	if t == nil {
		return nil
	}
	return t.Kinds
}

// FireKind reports a cell whose values name events or on_actions, which is what makes an assignment a fire site.
func (m *Model) FireKind(kind, path, key string) string {
	for _, k := range m.TypeOf(kind, path, key).kindsOf() {
		if k == jomini.KindEvent || k == "on_action" {
			return k
		}
	}
	return ""
}

// LocConventionsOf is a kind's derived conventions, falling back to the canonical spelling's.
func (m *Model) LocConventionsOf(kind string) []loc.Convention {
	l := m.cells()
	if cs := l.Kinds[kind].Loc; len(cs) > 0 {
		return cs
	}
	return l.Kinds[jomini.CanonicalKind(kind)].Loc
}

// ConventionLocKeys is every key a kind's conventions name for one def.
func (m *Model) ConventionLocKeys(kind, id string) []string {
	return loc.Expand(id, m.LocConventionsOf(kind))
}

// LocKeyPrefixes are the derived prefixes that build one loc key out of another.
func (m *Model) LocKeyPrefixes() []string { return m.cells().LocKeyPrefixes }

// ------------------------------------------------------------- the doc reads

// KindFieldDoc is kind-only prose (chain[len-1]+"."+key, else bare key), with no global or dump fallback -- for callers gating on "does this kind document it" rather than "what does PMT know about this word everywhere".
func (m *Model) KindFieldDoc(key, kind string, chain []string) string {
	if kind == "" {
		return ""
	}
	byKind := m.docs().FieldInfoByKind[kind]
	if byKind == nil {
		return ""
	}
	lk := strings.ToLower(key)
	if len(chain) > 0 {
		if v := byKind[strings.ToLower(chain[len(chain)-1])+"."+lk]; v != "" {
			return v
		}
	}
	return byKind[lk]
}

// FieldDoc tries the block-qualified prose, then the kind's bare prose, then the global name, then the game's dump doc, then PMT's own gloss for the block keywords no game documents.
func (m *Model) FieldDoc(key, kind string, chain []string) string {
	d := m.docs()
	if v := m.KindFieldDoc(key, kind, chain); v != "" {
		return v
	}
	lk := strings.ToLower(key)
	if v := d.FieldInfo[lk]; v != "" {
		return v
	}
	if v := m.Schema().Doc(lk); v != "" {
		return v
	}
	if sib := portraitSibling(key); sib != "" {
		if v := m.KindFieldDoc(sib, kind, chain); v != "" {
			return v
		}
		slk := strings.ToLower(sib)
		if v := d.FieldInfo[slk]; v != "" {
			return v
		}
		if v := m.Schema().Doc(slk); v != "" {
			return v
		}
	}
	return jomini.StructuralDoc(key)
}

func (m *Model) TokenUsage(key string) string {
	d := m.docs()
	if u := m.Schema().Usage(key); u != "" {
		return u
	}
	if d.FieldInfo[strings.ToLower(key)] != "" {
		return ""
	}
	sib := portraitSibling(key)
	if sib == "" || d.FieldInfo[strings.ToLower(sib)] != "" {
		return ""
	}
	return m.Schema().Usage(sib)
}

func (m *Model) TokenTarget(key string) string {
	t, _, ok := m.Schema().Token(key)
	if !ok {
		return ""
	}
	return t.Target
}

// SchemaDoc returns the description the game ships for an engine token or link.
func (m *Model) SchemaDoc(word string) string {
	if t, _, ok := m.Schema().Token(word); ok && t.Doc != "" {
		return t.Doc
	}
	if l, ok := m.Schema().Link(word); ok {
		return l.Doc
	}
	return ""
}

// VocabRole names an engine token's role. The declared type system wins over the harvested vocabulary: it distinguishes effects, triggers and scope links, which a flat name list cannot.
func (m *Model) VocabRole(word string) string {
	if _, role, ok := m.Schema().Token(word); ok {
		return role
	}
	if l, ok := m.Schema().Link(word); ok && l.Out != "" {
		return "scope_link"
	}
	// A modifier is declared too; without this it only hovered where the install happened to also define it as a row.
	if _, ok := m.Schema().Modifier(word); ok {
		return "modifier"
	}
	l := m.cells()
	if l.EffectSet[word] || l.EffectSet[strings.ToLower(word)] {
		return "effect"
	}
	if l.TriggerSet[word] || l.TriggerSet[strings.ToLower(word)] {
		return "trigger"
	}
	return ""
}

// declaredKindType is what KIND's own docs say a field holds, with no global fallback: the global map holds whatever the first file to document a name happened to say about it, and `type` is `<diplomacy/subject/union>` inside a scripted relation and an event presentation kind inside an event.
func (m *Model) declaredKindType(kind, field string) (declared.DeclaredType, bool) {
	d := m.docs()
	lf := strings.ToLower(field)
	for _, k := range []string{kind, jomini.CanonicalKind(kind)} {
		if k == "" {
			continue
		}
		if byKind := d.DeclaredFieldTypesByKind[k]; byKind != nil {
			if t, ok := byKind[lf]; ok {
				return t, true
			}
		}
	}
	return declared.DeclaredType{}, false
}

// DeclaredFieldType is the kind's declared type, else the global one. A hover can afford the global map's ambiguity.
func (m *Model) DeclaredFieldType(kind, field string) declared.DeclaredType {
	if t, ok := m.declaredKindType(kind, field); ok {
		return t
	}
	return m.docs().DeclaredFieldTypes[strings.ToLower(field)]
}

// DeclaredValueSet is per-kind only: a check that accuses correct script cannot afford the global map's ambiguity.
func (m *Model) DeclaredValueSet(kind, field string) []string {
	t, _ := m.declaredKindType(kind, field)
	return t.Values
}

// firstIn makes the lookup policies comparable without merging them: each caller still states its own key order on one line.
func firstIn[V comparable](m map[string]V, keys ...string) V {
	var zero V
	for _, k := range keys {
		if v, ok := m[k]; ok && v != zero {
			return v
		}
	}
	return zero
}

// portraitSibling maps left_foo ↔ right_foo for script_docs pair fallback.
func portraitSibling(key string) string {
	k := strings.ToLower(key)
	switch {
	case strings.HasPrefix(k, "left_"):
		return "right_" + k[len("left_"):]
	case strings.HasPrefix(k, "right_"):
		return "left_" + k[len("right_"):]
	default:
		return ""
	}
}
