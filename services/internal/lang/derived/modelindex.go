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
	"paradox-modding-tools/services/internal/lang/loc"
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
		if want != "" {
			if jomini.CanonicalKind(d.Kind) != want {
				return true
			}
		} else if kind == "" && jomini.IsEphemeral(d.Kind) {
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
	m.EachDef(includeVanilla, visit)
	if limit > 0 && len(out) >= limit {
		return out
	}
	// The tier is out of EachDef so a kindless search never pays for it, and walked by sorted key so a limit cuts the same way twice.
	if includeVanilla && want != "" && game.IsDataKind(m.gameID, want) {
		m.ensureData()
		for _, key := range m.dataKeys {
			for _, d := range m.dataByKey[key] {
				if !visit(d) {
					return out
				}
			}
		}
	}
	return out
}

// MemberSets returns the pre-built structure/effect/trigger maps LSP ranks with.
func (m *Model) MemberSets(kind string) (structKeys, effects, triggers map[string]bool) {
	if m.cache == nil {
		return nil, nil, nil
	}
	return m.cache.structureSets[kind], m.cache.effectSet, m.cache.triggerSet
}

// Vocab is the name list for a completion slot: the declared API for effect/trigger/vocabulary, the harvested corpus for the rest.
func (m *Model) Vocab(kind string) []string {
	if m.cache == nil {
		return nil
	}
	switch kind {
	case "effect":
		return m.cache.effectNames
	case "trigger":
		return m.cache.triggerNames
	case "vocabulary":
		return m.cache.vocabNames
	case "gui_type":
		return m.cache.GUITypes
	case "gui_prop":
		return m.cache.GUIProps
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

// KindFieldDoc is kind-only prose (chain[len-1]+"."+key, else bare key), with no global or dump fallback -- for callers gating on "does this kind document it" rather than "what does PMT know about this word everywhere".
func (m *Model) KindFieldDoc(key, kind string, chain []string) string {
	if m.cache == nil || kind == "" {
		return ""
	}
	byKind := m.cache.FieldInfoByKind[kind]
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
	if m.cache == nil {
		return ""
	}
	if v := m.KindFieldDoc(key, kind, chain); v != "" {
		return v
	}
	lk := strings.ToLower(key)
	if v := m.cache.FieldInfo[lk]; v != "" {
		return v
	}
	if v := m.cache.Schema.Doc(lk); v != "" {
		return v
	}
	if sib := portraitSibling(key); sib != "" {
		if v := m.KindFieldDoc(sib, kind, chain); v != "" {
			return v
		}
		slk := strings.ToLower(sib)
		if v := m.cache.FieldInfo[slk]; v != "" {
			return v
		}
		if v := m.cache.Schema.Doc(slk); v != "" {
			return v
		}
	}
	return jomini.StructuralDoc(key)
}

func (m *Model) TokenUsage(key string) string {
	if m.cache == nil {
		return ""
	}
	if u := m.cache.Schema.Usage(key); u != "" {
		return u
	}
	if m.cache.FieldInfo[strings.ToLower(key)] != "" {
		return ""
	}
	sib := portraitSibling(key)
	if sib == "" || m.cache.FieldInfo[strings.ToLower(sib)] != "" {
		return ""
	}
	return m.cache.Schema.Usage(sib)
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

// declaredKindType is what KIND's own docs say a field holds, with no global fallback: the global map holds whatever the first file to document a name happened to say about it, and `type` is `<diplomacy/subject/union>` inside a scripted relation and an event presentation kind inside an event.
func (m *Model) declaredKindType(kind, field string) string {
	if m.cache == nil {
		return ""
	}
	lf := strings.ToLower(field)
	for _, k := range []string{kind, jomini.CanonicalKind(kind)} {
		if k == "" {
			continue
		}
		if byKind := m.cache.DeclaredFieldTypesByKind[k]; byKind != nil {
			if raw := byKind[lf]; raw != "" {
				return raw
			}
		}
	}
	return ""
}

// DeclaredFieldType is the kind's declared type, else the global one. A hover can afford the global map's ambiguity.
func (m *Model) DeclaredFieldType(kind, field string) declared.DeclaredType {
	if m.cache == nil {
		return declared.DeclaredType{}
	}
	raw := m.declaredKindType(kind, field)
	if raw == "" {
		raw = m.cache.DeclaredFieldTypes[strings.ToLower(field)]
	}
	if raw == "" {
		return declared.DeclaredType{}
	}
	return declared.ClassifyDeclaredType(raw)
}

// DeclaredValueSet is per-kind only: a check that accuses correct script cannot afford the global map's ambiguity.
func (m *Model) DeclaredValueSet(kind, field string) []string {
	raw := m.declaredKindType(kind, field)
	if raw == "" {
		return nil
	}
	return declared.ClassifyDeclaredType(raw).Values
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
	if block == "" || m.cache == nil {
		return nil
	}
	return m.cache.NestedMembers[strings.ToLower(block)]
}

func (m *Model) IsNestedMember(block, key string) bool {
	if block == "" || key == "" {
		return false
	}
	return slices.Contains(m.NestedMembers(block), strings.ToLower(key))
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
	vanilla := m.cacheByKey[key]
	if len(vanilla) == 0 {
		// As in defsOf: only a name the script cache cannot place pays for the tier.
		m.ensureData()
		vanilla = m.dataByKey[key]
	}
	for _, d := range vanilla {
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
	// The default language's file is the site; other languages only when it has none.
	var locDefs, other []Def
	for _, d := range m.defsByKey[key] {
		if d.Kind != "loc_key" {
			continue
		}
		if loc.LanguageFromRel(d.Path) == m.DefaultLang() {
			locDefs = append(locDefs, d)
		} else {
			other = append(other, d)
		}
	}
	if len(locDefs) == 0 {
		locDefs = other
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

// maxViaHops caps the macro chain a via edge may cross.
const maxViaHops = 3

// EdgesFrom are the edges leaving id, mods then vanilla, then the via hops.
func (m *Model) EdgesFrom(id string) []Edge {
	if id == "" {
		return nil
	}
	out := append(append([]Edge{}, m.edgesByFrom[id]...), m.cacheEdgesByFrom[id]...)
	type step struct {
		macro string
		chain []string
	}
	var q []step
	seen, visited := map[string]bool{id: true}, map[string]bool{}
	// A macro is a hop, not a node: nothing is synthesized when id is one.
	if !m.macroKeys[id] {
		for _, e := range out {
			seen[e.To] = true
			if e.Kind == EdgeKindCall && m.macroKeys[e.To] && !visited[e.To] {
				visited[e.To] = true
				q = append(q, step{e.To, []string{e.To}})
			}
		}
	}
	for len(q) > 0 {
		st := q[0]
		q = q[1:]
		for _, e := range append(append([]Edge{}, m.edgesByFrom[st.macro]...), m.cacheEdgesByFrom[st.macro]...) {
			if e.Kind == EdgeKindCall {
				if m.macroKeys[e.To] && !visited[e.To] && len(st.chain) < maxViaHops {
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
	out := append(append([]Edge{}, m.edgesByTo[id]...), m.cacheEdgesByTo[id]...)
	type step struct {
		chain []string
		fire  Edge
	}
	var q []step
	seen, visited := map[string]bool{id: true}, map[string]bool{}
	for _, e := range out {
		seen[e.From] = true
		if e.Kind != EdgeKindCall && m.macroKeys[e.From] && !visited[e.From] {
			visited[e.From] = true
			q = append(q, step{[]string{e.From}, e})
		}
	}
	for len(q) > 0 {
		st := q[0]
		q = q[1:]
		for _, caller := range m.callersOf(st.chain[0]) {
			if m.macroKeys[caller] {
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

// callersOf lists the defs whose call edges reach macro, mods then vanilla.
func (m *Model) callersOf(macro string) []string {
	var out []string
	for _, e := range append(append([]Edge{}, m.edgesByTo[macro]...), m.cacheEdgesByTo[macro]...) {
		if e.Kind == EdgeKindCall {
			out = append(out, e.From)
		}
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

// GraphCatalog returns the kind of every fire-edge endpoint and the macro def keys.
func (m *Model) GraphCatalog() (kinds map[string]string, effects map[string]bool) {
	kinds = map[string]string{}
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
		if endpoints[d.Key] {
			kinds[d.Key] = jomini.CanonicalKind(d.Kind)
		}
		return true
	})
	return kinds, effects
}

// Contests returns contested override keys for the live workspace.
func (m *Model) Contests(order []string) []Contest {
	var mods []Def
	rewritesData := false
	m.EachDef(false, func(d Def) bool {
		mods = append(mods, d)
		rewritesData = rewritesData || game.IsDataKind(m.gameID, d.Kind)
		return true
	})
	var vanilla []Def
	if m.cache != nil {
		vanilla = m.cache.Defs
	}
	// A mod rewriting a character's history contests the install's row, so the tier belongs here once a mod has written one.
	if rewritesData {
		m.ensureData()
		all := make([]Def, 0, len(vanilla))
		all = append(all, vanilla...)
		for _, key := range m.dataKeys {
			all = append(all, m.dataByKey[key]...)
		}
		vanilla = all
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
	if m.cache == nil || key == "" {
		return false
	}
	set := m.cache.structureBlockSets[kind]
	return set[key] || set[strings.ToLower(key)]
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

// LocKeys returns up to limit workspace and vanilla loc keys starting with prefix. The filter runs here because a CK3 workspace holds ~10^5 keys and completion keeps 80: materializing the whole set per keystroke was the most expensive thing hover and completion did.
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

// EachLoc walks every indexed workspace loc entry; fn must not call back into the session.
func (m *Model) EachLoc(fn func(lang, key string, e LocEntry)) {
	for lang, m := range m.locByLang {
		for k, e := range m {
			fn(lang, k, e)
		}
	}
}

// KindHasDefs reports whether anything in the workspace or the install defines an object of this kind. A reference to a kind with no definitions anywhere is an engine concept rather than a missing object.
func (m *Model) KindHasDefs(kind string) bool {
	return m.kindsWithDefs[jomini.CanonicalKind(kind)]
}

// BlockIsJudgeable reports a block whose members can be judged rather than merely suggested: a name exactly one kind uses.
func (m *Model) BlockIsJudgeable(block string) bool {
	if block == "" || m.cache == nil {
		return false
	}
	return m.cache.UnambiguousBlocks[strings.ToLower(block)]
}

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
