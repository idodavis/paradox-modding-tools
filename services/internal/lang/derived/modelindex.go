// modelindex.go queries the harvested index: defs, refs, edges, fields, structures, loc.

package derived

import (
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

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
	for _, m := range m.mods {
		if m.Origin == origin && m.Name != "" {
			return m.Name
		}
	}
	return origin
}

func (m *Model) ModDefsOf(key string) []Def {
	return cloneIf(m.defsByKey[key])
}

// FindDefs returns non-ephemeral defs whose keys contain query (or start with it if byPrefix).
func (m *Model) FindDefs(query string, limit int, byPrefix, includeVanilla bool) []Def {
	return m.findDefs(query, "", limit, byPrefix, includeVanilla, false)
}

// FindDefsOf is FindDefs restricted to CanonicalKind(kind), including ephemeral.
func (m *Model) FindDefsOf(
	query, kind string, limit int, byPrefix, includeVanilla bool,
) []Def {
	return m.findDefs(query, kind, limit, byPrefix, includeVanilla, true)
}

// FindMacroDefs returns scripted_* macro defs whose keys start with query; the macro test runs before the limit.
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

// LocFile returns a workspace loc file path for origin and lang, if indexed.
func (m *Model) LocFile(origin, lang string) (string, bool) {
	marker := "_l_" + strings.ToLower(lang)
	var found string
	m.EachDef(false, func(d Def) bool {
		if d.Kind == "loc_key" && d.Origin == origin &&
			strings.Contains(strings.ToLower(d.Path), marker) {
			found = d.Path
			return false
		}
		return true
	})
	return found, found != ""
}

// DefCount is the number of workspace definitions.
func (m *Model) DefCount() int {
	n := 0
	m.EachDef(false, func(Def) bool { n++; return true })
	return n
}

func (m *Model) FindParamsOf(owner string, limit int) []string {
	if owner == "" || limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	m.EachDef(true, func(d Def) bool {
		if d.OwnerKey != owner || d.Key == "" ||
			jomini.CanonicalKind(d.Kind) != "script_param" || seen[d.Key] {
			return true
		}
		seen[d.Key] = true
		out = append(out, d.Key)
		return len(out) < limit
	})
	slices.Sort(out)
	return out
}

func (m *Model) findDefs(
	query, kind string, limit int, byPrefix, includeVanilla, allowEphemeral bool,
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
	m.EachDef(includeVanilla, func(d Def) bool {
		if want != "" {
			if jomini.CanonicalKind(d.Kind) != want {
				return true
			}
		} else if !allowEphemeral && jomini.IsEphemeral(d.Kind) {
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
	})
	return out
}

func (m *Model) MemberSets(kind string) (structKeys, effects, triggers map[string]bool) {
	if m.cache == nil {
		return nil, nil, nil
	}
	return m.cache.MemberSets(kind)
}

func (m *Model) Vocab(kind string) []string {
	if m.cache == nil {
		return nil
	}
	switch kind {
	case "effect", "trigger", "vocabulary":
		return m.cache.TokenNames(kind)
	case "gui_type":
		return m.cache.GUITypes
	case "gui_prop":
		return m.cache.GUIProps
	case "meta":
		return m.cache.MetaKeys
	case "datafunction":
		return m.cache.DataFunctions
	default:
		return nil
	}
}

func (m *Model) RefsTo(key string) []Ref {
	if key == "" {
		return nil
	}
	out := cloneIf(m.refsByKey[key])
	if extra := m.cacheRefsByKey[key]; len(extra) > 0 {
		out = append(out, extra...)
	}
	return out
}

func (m *Model) DefaultLoc(key string) (string, bool) {
	lang := m.defaultLang
	if lang == "" {
		lang = "english"
	}
	if m := m.locByLang[lang]; m != nil {
		if v, ok := m[key]; ok {
			return v.Value, true
		}
	}
	if m.vanillaLoc != nil {
		if site, ok := m.vanillaLoc.Sites[key]; ok && site.Value != "" {
			return site.Value, true
		}
	}
	return "", false
}

func (m *Model) KindFieldDoc(key, kind string) string {
	if m.cache == nil || kind == "" {
		return ""
	}
	lk := strings.ToLower(key)
	if m := m.cache.FieldInfoByKind[kind]; m != nil {
		return m[lk]
	}
	return ""
}

func (m *Model) FieldDoc(key, kind string) string {
	if m.cache == nil {
		return ""
	}
	if v := m.fieldDoc(key, kind); v != "" {
		return v
	}
	if sib := portraitSibling(key); sib != "" {
		if v := m.fieldDoc(sib, kind); v != "" {
			return v
		}
	}
	// Last resort: PMT's own gloss for the block keywords no game documents at
	// all (`limit`, `desc`, `option`, `weight`, …). Every real source above
	// wins — including the portrait-pair fallback — so this only fills a hover
	// that would otherwise be empty.
	return jomini.StructuralDoc(key)
}

func (m *Model) TokenUsage(key string) string {
	if m.cache == nil {
		return ""
	}
	if u := m.cache.Schema.Usage(key); u != "" {
		return u
	}
	if m.fieldDoc(key, "") != "" {
		return ""
	}
	sib := portraitSibling(key)
	if sib == "" || m.fieldDoc(sib, "") != "" {
		return ""
	}
	return m.cache.Schema.Usage(sib)
}

func (m *Model) TokenScopes(key string) string {
	if m.cache == nil {
		return ""
	}
	return m.cache.Schema.ScopeText(key)
}

func (m *Model) PrefixKind(prefix string) string {
	if m.cache == nil {
		return ""
	}
	return firstIn(m.cache.PrefixKinds, prefix, strings.ToLower(prefix))
}

func (m *Model) TokenTarget(key string) string {
	t, _, ok := m.Schema().Token(key)
	if !ok {
		return ""
	}
	return t.Target
}

func (m *Model) CiteFormOfScope(scope string) (prefix, kind string) {
	if scope == "" {
		return "", ""
	}
	sc := strings.ToLower(scope)
	return m.scopePrefix[sc], m.scopeKind[sc]
}

func (m *Model) FieldValueKind(field string) string {
	if v := m.fieldValueKinds[field]; v != "" {
		return v
	}
	return m.fieldValueKinds[strings.ToLower(field)]
}

func (m *Model) DeclaredFieldType(kind, field string) declared.DeclaredType {
	if m.cache == nil {
		return declared.DeclaredType{}
	}
	lf := strings.ToLower(field)
	for _, k := range []string{kind, jomini.CanonicalKind(kind)} {
		if k == "" {
			continue
		}
		if m := m.cache.DeclaredFieldTypesByKind[k]; m != nil {
			if raw := m[lf]; raw != "" {
				return declared.ClassifyDeclaredType(raw)
			}
		}
	}
	if raw := m.cache.DeclaredFieldTypes[lf]; raw != "" {
		return declared.ClassifyDeclaredType(raw)
	}
	return declared.DeclaredType{}
}

func (m *Model) FieldEnums(kind, field string) []string {
	lk := strings.ToLower(field)
	try := func(k string) []string {
		if k == "" || m.fieldEnumsByKind == nil {
			return nil
		}
		if m := m.fieldEnumsByKind[k]; m != nil {
			return m[lk]
		}
		return nil
	}
	if v := try(kind); len(v) > 0 {
		return v
	}
	return try(jomini.CanonicalKind(kind))
}

func (m *Model) CacheInfo() (installPath, scannedAt, gameVersion, installID string) {
	if m.cache == nil {
		return "", "", "", ""
	}
	return m.cache.InstallPath, m.cache.ScannedAt, m.cache.GameVersion, m.cache.InstallID
}

func (m *Model) CalendarFor(path string) ModCalendar {
	origin, _, ok := m.Locate(path)
	if !ok {
		return ModCalendar{}
	}
	for _, m := range m.Mods() {
		if m.Origin == origin {
			return m.Calendar
		}
	}
	return ModCalendar{}
}

func (m *Model) NestedMembers(block string) []string {
	if block == "" {
		return nil
	}
	if m.cache == nil {
		return nil
	}
	return m.cache.NestedMembers[strings.ToLower(block)]
}

func (m *Model) FieldDocIn(key, kind, block string) string {
	if block != "" && m.FieldDocIsAmbiguous(key) {
		return ""
	}
	return m.FieldDoc(key, kind)
}

func (m *Model) IsNestedMember(block, key string) bool {
	if block == "" || key == "" {
		return false
	}
	lk := strings.ToLower(key)
	return slices.Contains(m.NestedMembers(block), lk)
}

func (m *Model) Resolve(key string) *Def {
	return m.ResolveMatching(key, nil)
}

func (m *Model) VanillaDefs(key string) []Def {
	var out []Def
	seen := map[string]bool{}
	add := func(d Def) {
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
	for _, d := range m.cacheByKey[key] {
		add(d)
	}
	if m.vanillaLoc != nil {
		if site, ok := m.vanillaLoc.Sites[key]; ok {
			add(Def{
				Kind: "loc_key", Key: key, Path: site.Path, Line: site.Line,
				Origin: game.OriginVanilla,
			})
		}
	}
	return out
}

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
	defs = defs[:n]
	return Winner(m.gameID, defs, m.order)
}

func (m *Model) LocSite(key string) (file string, line, start int, origin string, ok bool) {
	var locDefs []Def
	for _, d := range m.defsByKey[key] {
		if d.Kind == "loc_key" {
			locDefs = append(locDefs, d)
		}
	}
	if w := Winner(m.gameID, locDefs, m.order); w != nil {
		return w.Path, w.Line, w.Start, w.Origin, true
	}
	if m.vanillaLoc != nil {
		if site, hit := m.vanillaLoc.Sites[key]; hit {
			return site.Path, site.Line, 0, game.OriginVanilla, true
		}
	}
	return "", 0, 0, "", false
}

func (m *Model) Mods() []ModInput {
	return m.mods
}

func (m *Model) Locate(path string) (origin, rel string, ok bool) {
	path = CanonPath(path)
	for _, m := range m.mods {
		if r, inside := RelPath(m.Root, path); inside {
			return m.Origin, r, true
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

// Schema is the declared type system this model was built against, or nil when
// the user has not run script_docs. Callers must degrade rather than guess.
// EdgesFrom are the graph edges leaving id, mods first then vanilla. Empty id
// means every edge.
func (m *Model) EdgesFrom(id string) []Edge {
	if id == "" {
		return m.allEdges()
	}
	out := append(append([]Edge{}, m.edgesByFrom[id]...), m.cacheEdgesByFrom[id]...)
	if len(out) == 0 {
		return nil
	}
	return out
}

// EachDef walks workspace defs, then vanilla if includeVanilla. fn false stops.
func (m *Model) EachDef(includeVanilla bool, fn func(Def) bool) {
	for _, defs := range m.defsByFile {
		for _, d := range defs {
			if !fn(d) {
				return
			}
		}
	}
	if includeVanilla && m.cache != nil {
		for _, d := range m.cache.Defs {
			if !fn(d) {
				return
			}
		}
	}
}

// GraphCatalog returns fire-edge endpoints, their kinds, and macro def keys.
func (m *Model) GraphCatalog(origins []string) (
	picker map[string]string, kinds map[string]string, effects map[string]bool,
) {
	picker, kinds = map[string]string{}, map[string]string{}
	effects = make(map[string]bool, len(m.macroKeys))
	maps.Copy(effects, m.macroKeys)
	endpoints := map[string]bool{}
	addEnd := func(e Edge) {
		if e.Kind == EdgeKindCall {
			return
		}
		if e.From != "" {
			endpoints[e.From] = true
		}
		if e.To != "" {
			endpoints[e.To] = true
		}
	}
	for _, edges := range m.edgesByFrom {
		for _, e := range edges {
			addEnd(e)
		}
	}
	if m.cache != nil {
		for _, e := range m.cache.Edges {
			addEnd(e)
		}
	}
	m.EachDef(true, func(d Def) bool {
		if !endpoints[d.Key] {
			return true
		}
		ck := jomini.CanonicalKind(d.Kind)
		kinds[d.Key] = ck
		want := d.Origin
		if want == "" {
			want = game.OriginVanilla
		}
		if len(origins) == 0 || slices.Contains(origins, want) {
			if prev, ok := picker[d.Key]; !ok || prev == "" || d.Origin != "" {
				picker[d.Key] = want
			}
		}
		return true
	})
	return picker, kinds, effects
}

// Contests returns contested override keys for the live workspace.
func (m *Model) Contests(order []string) []Contest {
	var mods []Def
	m.EachDef(false, func(d Def) bool {
		mods = append(mods, d)
		return true
	})
	var vanilla []Def
	if m.cache != nil {
		vanilla = m.cache.Defs
	}
	return Contests(m.gameID, mods, vanilla, order)
}

// Structures returns vanilla structure keys for kind (frequency order).
func (m *Model) Structures(kind string) []string {
	if m.cache == nil {
		return nil
	}
	return m.cache.Structures[kind]
}

// StructureBlock reports whether key is usually a block field under kind.
func (m *Model) StructureBlock(kind, key string) bool {
	if m.cache == nil {
		return false
	}
	return m.cache.StructureBlock(kind, key)
}

// RefsInFile returns workspace refs sourced from path.
func (m *Model) RefsInFile(path string) []Ref {
	return cloneIf(m.refsByFile[CanonPath(path)])
}

// WorkspaceRefs returns harvested workspace refs (not vanilla cache refs).
func (m *Model) WorkspaceRefs() []Ref {
	n := 0
	for _, refs := range m.refsByKey {
		n += len(refs)
	}
	out := make([]Ref, 0, n)
	for _, refs := range m.refsByKey {
		out = append(out, refs...)
	}
	return out
}

// DefsInFile returns workspace defs declared in path.
func (m *Model) DefsInFile(path string) []Def {
	return cloneIf(m.defsByFile[CanonPath(path)])
}

func (m *Model) fieldDoc(key, kind string) string {
	lk := strings.ToLower(key)
	if kind != "" {
		if m := m.cache.FieldInfoByKind[kind]; m != nil && m[lk] != "" {
			return m[lk]
		}
	}
	// The global description is per field NAME, not per position. A name used
	// inside several different blocks has several meanings, and showing one of
	// them is a coin flip: `trigger` inside a triggered_desc was explained as
	// controlling "visibility for this portrait". Kind-specific prose above is
	// still trusted; this is only the catch-all.
	if v := m.cache.FieldInfo[lk]; v != "" && !m.cache.AmbiguousFields[lk] {
		return v
	}
	// Install prose wins over the game's dump prose, so the Schema is consulted last.
	return m.cache.Schema.Doc(lk)
}

// Per-kind only, with no global fallback, unlike DeclaredFieldType. The global
// map holds whatever the first file to document a name happened to say about
// it, and `type` is `<diplomacy/subject/union>` inside a scripted relation and
// an event presentation kind inside an event -- GAME-SYNTAX 3f. A hover can
// afford to be wrong about that; a check that accuses correct script cannot.
func (m *Model) DeclaredValueSet(kind, field string) []string {
	if m.cache == nil {
		return nil
	}
	lf := strings.ToLower(field)
	for _, k := range []string{kind, jomini.CanonicalKind(kind)} {
		if k == "" {
			continue
		}
		if m := m.cache.DeclaredFieldTypesByKind[k]; m != nil {
			if raw := m[lf]; raw != "" {
				return declared.ClassifyDeclaredType(raw).Values
			}
		}
	}
	return nil
}

// LocRefs returns workspace loc and loc-broad refs.
func (m *Model) LocRefs() []Ref {
	n := 0
	for _, rs := range m.locRefsByFile {
		n += len(rs)
	}
	if n == 0 {
		return nil
	}
	out := make([]Ref, 0, n)
	for _, rs := range m.locRefsByFile {
		out = append(out, rs...)
	}
	return out
}

// LocKeys returns up to limit workspace and vanilla loc keys starting with
// prefix. The filter runs here rather than in the caller because a CK3 workspace
// holds ~10^5 loc keys and completion keeps 80 of them: materializing the whole
// set on every keystroke was the most expensive thing hover and completion did.
func (m *Model) LocKeys(prefix string, limit int) []string {
	lp := strings.ToLower(prefix)
	seen := make(map[string]bool, limit)
	out := make([]string, 0, limit)
	add := func(k string) bool {
		if k == "" || seen[k] || !strings.HasPrefix(strings.ToLower(k), lp) {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return len(out) < limit
	}
	for _, m := range m.locByLang {
		for k := range m {
			if !add(k) {
				return out
			}
		}
	}
	if m.vanillaLoc != nil {
		for k := range m.vanillaLoc.Sites {
			if !add(k) {
				return out
			}
		}
	}
	m.EachDef(false, func(d Def) bool {
		if d.Kind != "loc_key" {
			return true
		}
		return add(d.Key)
	})
	return out
}

// UsedLocKeys is loc / loc-broad / loc-convention keys from workspace and vanilla.
func (m *Model) UsedLocKeys() map[string]bool {
	out := map[string]bool{}
	add := func(refs []Ref) {
		for _, r := range refs {
			switch r.Kind {
			case "loc", "loc-broad", "loc-convention":
				out[r.Key] = true
			}
		}
	}
	for _, rs := range m.locRefsByFile {
		add(rs)
	}
	if m.cache != nil {
		add(m.cache.LocRefs)
	}
	return out
}

// fn must not call back into the session.
func (m *Model) EachLoc(fn func(lang, key string, e LocEntry)) {
	for lang, m := range m.locByLang {
		for k, e := range m {
			fn(lang, k, e)
		}
	}
}

// EdgesTo returns stored call+fire/via edges arriving at id.
func (m *Model) EdgesTo(id string) []Edge {
	if id == "" {
		return nil
	}
	m.EnsureVia()
	out := append(append(append([]Edge{}, m.edgesByTo[id]...),
		m.cacheEdgesByTo[id]...), m.viaByTo[id]...)
	if len(out) == 0 {
		return nil
	}
	return out
}

// KindHasDefs reports whether anything in the workspace or the install defines
// an object of this kind. A reference to a kind with no definitions anywhere is
// an engine concept rather than a missing object — Vic3's `relations_threshold:`
// is a declared scope link with no database behind it.
func (m *Model) KindHasDefs(kind string) bool {
	return m.kindsWithDefs[jomini.CanonicalKind(kind)]
}

// IsEnumValue reports that a kind's own definitions use this word as a scalar
// value. Vic3 role archetypes (`admiral`, `general`) appear only as values of
// `type` inside character_roles, so `role = admiral` names a member of that
// kind's vocabulary, not a row that has gone missing.
func (m *Model) IsEnumValue(kind, key string) bool {
	if kind == "" || key == "" {
		return false
	}
	for _, m := range []map[string]map[string][]string{m.fieldEnumsByKind, m.modFieldEnums} {
		for _, vals := range m[jomini.CanonicalKind(kind)] {
			if slices.Contains(vals, key) {
				return true
			}
		}
	}
	return false
}

// Snippet returns the derived skeleton members for kind, most-used first, or
// nil when the install did not evidence a shape for it.
func (m *Model) Snippet(kind string) []SnippetMember {
	if m.cache == nil {
		return nil
	}
	if m := m.cache.Snippets[kind]; len(m) > 0 {
		return m
	}
	return m.cache.Snippets[jomini.CanonicalKind(kind)]
}

// BlockIsJudgeable reports a block whose members can be judged rather than
// merely suggested: a name exactly one kind uses.
func (m *Model) BlockIsJudgeable(block string) bool {
	if block == "" {
		return false
	}
	if m.cache == nil {
		return false
	}
	return m.cache.UnambiguousBlocks[strings.ToLower(block)]
}

// Overwriting path with CanonPath here put a lower-cased path into every
// definition harvested from an open file, because CanonPath lower-cases on
// Windows. That spelling then travelled all the way to the editor, which keys
// its tabs by exact URI: go-to-definition on a symbol in the file you were
// already looking at opened a second tab onto `file:///c:/users/...` beside the
// original `file:///C:/Users/...`. The same mismatch made the workbench's
// reveal-in-explorer a no-op, since it first asks whether the resource is inside
// a workspace folder and the folders are registered with their real casing.
func cloneIf[T any](in []T) []T {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
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

// addLocKeys appends k once. Used by the coverage views, which genuinely need
// every key; completion goes through LocKeys, which filters as it walks.
func addLocKeys(seen map[string]bool, out *[]string, k string) {
	if k == "" || seen[k] {
		return
	}
	seen[k] = true
	*out = append(*out, k)
}
func (m *Model) FieldDocIsAmbiguous(field string) bool {
	if field == "" {
		return false
	}
	if m.cache == nil {
		return false
	}
	return m.cache.AmbiguousFields[strings.ToLower(field)]
}
