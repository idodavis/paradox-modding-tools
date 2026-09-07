// query.go is the read-only session API. lsp and views reach the model only
// through these methods: they take the read lock, copy what they return, and
// hold no state of their own. Anything that mutates session maps belongs in
// session.go.

package session

import (
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

// DisplayRel is origin-relative, else install-relative, else the path with slashes.
func (s *Session) DisplayRel(path string) string {
	if path == "" {
		return ""
	}
	if _, rel, ok := s.Locate(path); ok {
		return filepath.ToSlash(rel)
	}
	if inst, _, _, _ := s.CacheInfo(); inst != "" {
		if rel, err := filepath.Rel(inst, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// OriginName is the mod display name, or the game title for vanilla.
func (s *Session) OriginName(origin string) string {
	if origin == "" || origin == game.OriginVanilla {
		if g := game.Get(s.GameID); g != nil && g.Name != "" {
			return g.Name
		}
		return "Game"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.mods {
		if m.Origin == origin && m.Name != "" {
			return m.Name
		}
	}
	return origin
}

// ModDefsOf returns workspace-only declarations of key.
func (s *Session) ModDefsOf(key string) []catalog.Def {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneIf(s.defsByKey[key])
}

// eachDef walks workspace defs, then vanilla if includeVanilla. fn false stops.
func (s *Session) eachDef(includeVanilla bool, fn func(catalog.Def) bool) {
	for _, defs := range s.defsByFile {
		for _, d := range defs {
			if !fn(d) {
				return
			}
		}
	}
	if includeVanilla && s.cache != nil {
		for _, d := range s.cache.Defs {
			if !fn(d) {
				return
			}
		}
	}
}

// FindDefs returns defs whose keys contain query (or, if byPrefix, start with it).
// Ephemeral kinds are omitted (outline / Ctrl+T); use FindDefsOf for those.
func (s *Session) FindDefs(query string, limit int, byPrefix, includeVanilla bool) []catalog.Def {
	return s.findDefs(query, "", limit, byPrefix, includeVanilla, false)
}

// FindDefsOf is FindDefs restricted to CanonicalKind(kind), including ephemeral.
func (s *Session) FindDefsOf(
	query, kind string, limit int, byPrefix, includeVanilla bool,
) []catalog.Def {
	return s.findDefs(query, kind, limit, byPrefix, includeVanilla, true)
}

// FindParamsOf returns the $PARAM$ names one scripted macro declares, sorted.
//
// The owner test happens before the limit, for the same reason FindMacroDefs
// tests macro-ness before its own: filtering after a limit returns whichever
// entries map iteration reached first, and with CK3 harvesting script params
// from 2,536 different macros that is almost never the one asked about.
func (s *Session) FindParamsOf(owner string, limit int) []string {
	if owner == "" || limit <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	s.eachDef(true, func(d catalog.Def) bool {
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

// FindMacroDefs returns scripted_* macro defs whose keys start with query.
// The macro test happens before the limit, so the result does not depend on
// map iteration order the way a find-then-filter would.
func (s *Session) FindMacroDefs(query string, limit int) []catalog.Def {
	if limit <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(query)
	var out []catalog.Def
	s.eachDef(true, func(d catalog.Def) bool {
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

func (s *Session) findDefs(
	query, kind string, limit int, byPrefix, includeVanilla, allowEphemeral bool,
) []catalog.Def {
	if byPrefix && limit <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(query)
	want := ""
	if kind != "" {
		want = jomini.CanonicalKind(kind)
	}
	var out []catalog.Def
	s.eachDef(includeVanilla, func(d catalog.Def) bool {
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

// GraphCatalog returns fire-edge endpoints, their kinds, and macro def keys.
func (s *Session) GraphCatalog(origins []string) (
	picker map[string]string, kinds map[string]string, effects map[string]bool,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	picker, kinds = map[string]string{}, map[string]string{}
	effects = make(map[string]bool, len(s.macroKeys))
	for k, v := range s.macroKeys {
		effects[k] = v
	}
	endpoints := map[string]bool{}
	addEnd := func(e catalog.Edge) {
		if e.Kind == catalog.EdgeKindCall {
			return
		}
		if e.From != "" {
			endpoints[e.From] = true
		}
		if e.To != "" {
			endpoints[e.To] = true
		}
	}
	for _, edges := range s.edgesByFrom {
		for _, e := range edges {
			addEnd(e)
		}
	}
	if s.cache != nil {
		for _, e := range s.cache.Edges {
			addEnd(e)
		}
	}
	s.eachDef(true, func(d catalog.Def) bool {
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
func (s *Session) Contests(order []string) []catalog.Contest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var mods []catalog.Def
	s.eachDef(false, func(d catalog.Def) bool {
		mods = append(mods, d)
		return true
	})
	var vanilla []catalog.Def
	if s.cache != nil {
		vanilla = s.cache.Defs
	}
	return catalog.Contests(s.GameID, mods, vanilla, order)
}

// LocFile returns a workspace loc file path for origin and lang, if indexed.
func (s *Session) LocFile(origin, lang string) (string, bool) {
	marker := "_l_" + strings.ToLower(lang)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var found string
	s.eachDef(false, func(d catalog.Def) bool {
		if d.Kind == "loc_key" && d.Origin == origin &&
			strings.Contains(strings.ToLower(d.Path), marker) {
			found = d.Path
			return false
		}
		return true
	})
	return found, found != ""
}

// MemberSets returns pre-built vanilla structure/effect/trigger maps for kind.
func (s *Session) MemberSets(kind string) (structKeys, effects, triggers map[string]bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil, nil, nil
	}
	return s.cache.MemberSets(kind)
}

// Structures returns vanilla structure keys for kind (frequency order).
func (s *Session) Structures(kind string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil
	}
	return s.cache.Structures[kind]
}

// StructureBlock reports whether key is usually a block field under kind.
func (s *Session) StructureBlock(kind, key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return false
	}
	return s.cache.StructureBlock(kind, key)
}

// Vocab returns a vanilla membership slice: effect, trigger, vocabulary,
// gui_type, gui_prop, or meta.
func (s *Session) Vocab(kind string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil
	}
	switch kind {
	case "effect", "trigger", "vocabulary":
		return s.cache.TokenNames(kind)
	case "gui_type":
		return s.cache.GUITypes
	case "gui_prop":
		return s.cache.GUIProps
	case "meta":
		return s.cache.MetaKeys
	case "datafunction":
		return s.cache.DataFunctions
	default:
		return nil
	}
}

// RefsTo returns workspace refs whose key matches. Empty key returns none.
func (s *Session) RefsTo(key string) []catalog.Ref {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if key == "" {
		return nil
	}
	out := cloneIf(s.refsByKey[key])
	if extra := s.cacheRefsByKey[key]; len(extra) > 0 {
		out = append(out, extra...)
	}
	return out
}

// RefsInFile returns workspace refs sourced from path.
func (s *Session) RefsInFile(path string) []catalog.Ref {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneIf(s.refsByFile[CanonPath(path)])
}

// WorkspaceRefs returns harvested workspace refs (not vanilla cache refs).
func (s *Session) WorkspaceRefs() []catalog.Ref {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, refs := range s.refsByKey {
		n += len(refs)
	}
	out := make([]catalog.Ref, 0, n)
	for _, refs := range s.refsByKey {
		out = append(out, refs...)
	}
	return out
}

// DefaultLoc returns the workspace-default-language localization string for key.
func (s *Session) DefaultLoc(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lang := s.defaultLang
	if lang == "" {
		lang = "english"
	}
	if m := s.locByLang[lang]; m != nil {
		if v, ok := m[key]; ok {
			return v.Value, true
		}
	}
	if s.vanillaLoc != nil {
		if site, ok := s.vanillaLoc.Sites[key]; ok && site.Value != "" {
			return site.Value, true
		}
	}
	return "", false
}

// DefsInFile returns workspace defs declared in path.
func (s *Session) DefsInFile(path string) []catalog.Def {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneIf(s.defsByFile[CanonPath(path)])
}

// KindFieldDoc returns FieldInfoByKind[kind] only (no global FieldInfo/TokenDoc).
func (s *Session) kindFieldDoc(key, kind string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil || kind == "" {
		return ""
	}
	lk := strings.ToLower(key)
	if m := s.cache.FieldInfoByKind[kind]; m != nil {
		return m[lk]
	}
	return ""
}

// FieldDoc returns kind-scoped field-info, then global field-info, then TokenDoc.
func (s *Session) FieldDoc(key, kind string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return ""
	}
	if v := s.fieldDocLocked(key, kind); v != "" {
		return v
	}
	if sib := portraitSibling(key); sib != "" {
		if v := s.fieldDocLocked(sib, kind); v != "" {
			return v
		}
	}
	// Last resort: PMT's own gloss for the block keywords no game documents at
	// all (`limit`, `desc`, `option`, `weight`, …). Every real source above
	// wins — including the portrait-pair fallback — so this only fills a hover
	// that would otherwise be empty.
	return jomini.StructuralDoc(key)
}

func (s *Session) fieldDocLocked(key, kind string) string {
	lk := strings.ToLower(key)
	if kind != "" {
		if m := s.cache.FieldInfoByKind[kind]; m != nil && m[lk] != "" {
			return m[lk]
		}
	}
	if v := s.cache.FieldInfo[lk]; v != "" {
		return v
	}
	// Install prose wins over the game's dump prose, so the schema is consulted last.
	return s.cache.Schema.Doc(lk)
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

// TokenUsage returns the declared signature for an engine token. A token whose
// prose PMT already shows from the install falls back to its portrait sibling
// (left_/right_) rather than repeating that prose as a signature.
func (s *Session) TokenUsage(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return ""
	}
	if u := s.cache.Schema.Usage(key); u != "" {
		return u
	}
	if s.fieldDocLocked(key, "") != "" {
		return ""
	}
	sib := portraitSibling(key)
	if sib == "" || s.fieldDocLocked(sib, "") != "" {
		return ""
	}
	return s.cache.Schema.Usage(sib)
}

// TokenScopes renders what an engine token runs against, for display only.
// Scope filtering goes through TokensInScope, which reads the typed lists.
func (s *Session) tokenScopes(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return ""
	}
	return s.cache.Schema.ScopeText(key)
}

// PrefixKind returns the harvested kind bound to a typed cite prefix by
// BindKinds (`culture:` → `cultures`). Empty without a schema.
func (s *Session) PrefixKind(prefix string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return ""
	}
	if k := s.cache.PrefixKinds[prefix]; k != "" {
		return k
	}
	return s.cache.PrefixKinds[strings.ToLower(prefix)]
}

// FieldValueKind returns the harvested unique def type for an assignment field.
// TokenTarget is the scope type an effect or trigger takes as its argument, as
// the game declares it in Supported Targets. Empty when the token is unknown or
// takes no typed argument.
func (s *Session) TokenTarget(key string) string {
	t, _, ok := s.engineToken(key)
	if !ok {
		return ""
	}
	return t.Target
}

// CiteFormOfScope answers "the user must write a <scope type> here — how?".
//
// prefix is the citation form when the type has one (`culture_group`, so the
// value is written `culture_group:turkic_group`); kind is the database whose
// keys are written bare. A type can have both, one, or neither: EU5 cites a
// culture group by prefix, but a country tag is written plain.
func (s *Session) CiteFormOfScope(scope string) (prefix, kind string) {
	if scope == "" {
		return "", ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := strings.ToLower(scope)
	return s.scopePrefix[sc], s.scopeKind[sc]
}

func (s *Session) FieldValueKind(field string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v := s.fieldValueKinds[field]; v != "" {
		return v
	}
	return s.fieldValueKinds[strings.ToLower(field)]
}

// FieldEnums returns unique harvested scalar values for a kind+field, or nil.
func (s *Session) FieldEnums(kind, field string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lk := strings.ToLower(field)
	try := func(k string) []string {
		if k == "" || s.fieldEnumsByKind == nil {
			return nil
		}
		if m := s.fieldEnumsByKind[k]; m != nil {
			return m[lk]
		}
		return nil
	}
	if v := try(kind); len(v) > 0 {
		return v
	}
	return try(jomini.CanonicalKind(kind))
}

// CacheInfo returns vanilla install path, scan timestamp, game version, and install id.
func (s *Session) CacheInfo() (installPath, scannedAt, gameVersion, installID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return "", "", "", ""
	}
	return s.cache.InstallPath, s.cache.ScannedAt, s.cache.GameVersion, s.cache.InstallID
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

// DefCount is the number of workspace definitions.
func (s *Session) DefCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	s.eachDef(false, func(d catalog.Def) bool {
		n++
		return true
	})
	return n
}

// LocRefs returns workspace loc and loc-broad refs.
func (s *Session) LocRefs() []catalog.Ref {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneIf(s.locRefs)
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

// LocKeys returns up to limit workspace and vanilla loc keys starting with
// prefix. The filter runs here rather than in the caller because a CK3 workspace
// holds ~10^5 loc keys and completion keeps 80 of them: materializing the whole
// set on every keystroke was the most expensive thing hover and completion did.
func (s *Session) LocKeys(prefix string, limit int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	for _, m := range s.locByLang {
		for k := range m {
			if !add(k) {
				return out
			}
		}
	}
	if s.vanillaLoc != nil {
		for k := range s.vanillaLoc.Sites {
			if !add(k) {
				return out
			}
		}
	}
	s.eachDef(false, func(d catalog.Def) bool {
		if d.Kind != "loc_key" {
			return true
		}
		return add(d.Key)
	})
	return out
}

// InheritedLocKeys returns vanilla loc keys for lang (sidecar + vanilla loc_key defs).
func (s *Session) InheritedLocKeys(lang string) []string {
	s.mu.RLock()
	defLang := s.defaultLang
	// The default-language sidecar is inherited by every language, not only its
	// own. The games fall back to the source language for any key a translation
	// omits, so a vanilla key present there is available in french too — and
	// gating this on lang == defLang meant a mod that ships eight translations
	// had every vanilla key it referenced reported missing eight times, once
	// per language and never in english. A per-language sidecar, when one has
	// been harvested, is unioned with it below rather than replacing it.
	var sites map[string]catalog.LocEntry
	if s.vanillaLoc != nil {
		sites = s.vanillaLoc.Sites
	}
	installID, version := "", ""
	if s.cache != nil {
		installID, version = s.cache.InstallID, s.cache.GameVersion
	}
	s.mu.RUnlock()

	seen := map[string]bool{}
	var out []string
	for k := range sites {
		addLocKeys(seen, &out, k)
	}
	// A harvested sidecar for this language too, when there is one: it can hold
	// keys the source language does not. Both are inherited, so this unions
	// rather than replaces.
	if lang != "" && lang != defLang && installID != "" {
		if version == "" {
			version = "latest"
		}
		if vl, err := catalog.LoadVanillaLoc(installID, version, lang); err == nil && vl != nil {
			for k := range vl.Sites {
				addLocKeys(seen, &out, k)
			}
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.eachDef(true, func(d catalog.Def) bool {
		if d.Kind != "loc_key" || d.Origin != "" {
			return true
		}
		addLocKeys(seen, &out, d.Key)
		return true
	})
	return out
}

// UsedLocKeys is loc / loc-broad / loc-convention keys from workspace and vanilla.
func (s *Session) UsedLocKeys() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	add := func(refs []catalog.Ref) {
		for _, r := range refs {
			switch r.Kind {
			case "loc", "loc-broad", "loc-convention":
				out[r.Key] = true
			}
		}
	}
	add(s.locRefs)
	if s.cache != nil {
		add(s.cache.LocRefs)
	}
	return out
}

// EachLoc calls fn for every workspace loc entry, under the read lock.
//
// Prefer this to LocByLang when the caller is only going to read or reshape the
// entries. LocByLang deep-copies every language, and a caller that then builds
// its own map holds three copies of the whole loc corpus at once — which on a
// total conversion is most of the memory Workspace Health uses.
//
// fn must not call back into the session.
func (s *Session) EachLoc(fn func(lang, key string, e catalog.LocEntry)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for lang, m := range s.locByLang {
		for k, e := range m {
			fn(lang, k, e)
		}
	}
}

// EdgesFrom returns stored call+fire edges leaving id. Empty id returns every edge.
func (s *Session) EdgesFrom(id string) []catalog.Edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return s.allEdgesLocked()
	}
	out := append(append([]catalog.Edge{}, s.edgesByFrom[id]...), s.cacheEdgesByFrom[id]...)
	if len(out) == 0 {
		return nil
	}
	return out
}

// EdgesTo returns stored call+fire/via edges arriving at id.
func (s *Session) EdgesTo(id string) []catalog.Edge {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return nil
	}
	s.ensureViaLocked()
	out := append(append(append([]catalog.Edge{}, s.edgesByTo[id]...),
		s.cacheEdgesByTo[id]...), s.viaByTo[id]...)
	if len(out) == 0 {
		return nil
	}
	return out
}

// KindHasDefs reports whether anything in the workspace or the install defines
// an object of this kind. A reference to a kind with no definitions anywhere is
// an engine concept rather than a missing object — Vic3's `relations_threshold:`
// is a declared scope link with no database behind it.
func (s *Session) KindHasDefs(kind string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.kindsWithDefs[jomini.CanonicalKind(kind)]
}

// IsEnumValue reports that a kind's own definitions use this word as a scalar
// value. Vic3 role archetypes (`admiral`, `general`) appear only as values of
// `type` inside character_roles, so `role = admiral` names a member of that
// kind's vocabulary, not a row that has gone missing.
func (s *Session) IsEnumValue(kind, key string) bool {
	if kind == "" || key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range []map[string]map[string][]string{s.fieldEnumsByKind, s.modFieldEnums} {
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
func (s *Session) Snippet(kind string) []catalog.SnippetMember {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil
	}
	if m := s.cache.Snippets[kind]; len(m) > 0 {
		return m
	}
	return s.cache.Snippets[jomini.CanonicalKind(kind)]
}

// CalendarFor returns the era a path's mod declares, or an undeclared calendar.
//
// Keyed by origin because a workspace can hold several mods and only one of them
// may be a total conversion; a file's dates mean what its own mod says they mean.
func (s *Session) CalendarFor(path string) catalog.ModCalendar {
	origin, _, ok := s.Locate(path)
	if !ok {
		return catalog.ModCalendar{}
	}
	for _, m := range s.Mods() {
		if m.Origin == origin {
			return m.Calendar
		}
	}
	return catalog.ModCalendar{}
}
