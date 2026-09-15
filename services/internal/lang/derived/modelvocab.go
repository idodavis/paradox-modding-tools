// modelvocab.go answers what the game says about a word: tokens, docs, vocabulary, loc conventions.

package derived

import (
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// IsMacroDef reports that key is a harvested scripted-macro definition.
func (m *Model) IsMacroDef(key string) bool {
	_, ok := m.macroDefKinds[key]
	return ok
}

// FireKind returns the derived fire target kind for an assignment key.
// Loc properties (title, desc, …) never fire.
func (m *Model) FireKind(key string) string {
	if loc.Classify(key) != loc.PropNone {
		return ""
	}
	lower := strings.ToLower(key)
	if m.cache != nil {
		if k := firstIn(m.cache.FireKeys, key, lower); k != "" {
			return k
		}
	}
	return firstIn(m.modFireKeys, key, lower)
}

// LocConventionsOf is a kind's derived conventions, falling back to the canonical spelling's.
func (m *Model) LocConventionsOf(kind string) []loc.Convention {
	if m.cache == nil {
		return nil
	}
	if cs := m.cache.LocConventions[kind]; len(cs) > 0 {
		return cs
	}
	return m.cache.LocConventions[jomini.CanonicalKind(kind)]
}

// ConventionLocKeys is every key a kind's conventions name for one def.
func (m *Model) ConventionLocKeys(kind, id string) []string {
	return loc.Expand(id, m.LocConventionsOf(kind))
}

// LocKeyPrefixes are the derived prefixes that build one loc key out of another.
func (m *Model) LocKeyPrefixes() []string {
	if m.cache == nil {
		return nil
	}
	return m.cache.LocKeyPrefixes
}

// A macro with no parameters gets nothing here; `invoked by name` in the hint
// already says all there is to say about it.
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

// A namespace declares an id prefix for events; nothing points at one. But it is
// a definition like any other in the index, so load order decided the tie: a mod
// writing `namespace = conqueror` outranked vanilla's `conqueror` trait, and
// `add_trait = conqueror` hovered as the namespace. What kind of thing a name
// denotes is not something override order gets to decide. A namespace still
// resolves when nothing else of that name does, so hovering one still works.
func (m *Model) ResolveNonLoc(word string) *Def {
	if d := m.ResolveMatching(word, func(d Def) bool {
		return jomini.IsObjectKind(d.Kind)
	}); d != nil {
		return d
	}
	// Second pass drops only the namespace exclusion, so a namespace still
	// resolves when nothing else of that name does.
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
		if owner != "" && d.OwnerKey != "" && d.OwnerKey != owner {
			return false
		}
		return true
	})
}

func (m *Model) LocDefined(key string) bool {
	if _, ok := m.DefaultLoc(key); ok {
		return true
	}
	_, _, _, _, ok := m.LocSite(key)
	return ok
}

func (m *Model) IsStructKey(kind, key string) bool {
	sk, _, _ := m.MemberSets(kind)
	return sk[key] || sk[strings.ToLower(key)]
}

// VocabRole names an engine token's role. The declared type system wins over
// the harvested vocabulary: it distinguishes effects, triggers and scope links,
// which a flat name list cannot.
func (m *Model) VocabRole(word string) string {
	if _, role, ok := m.Schema().Token(word); ok {
		return role
	}
	if l, ok := m.Schema().Link(word); ok && l.Out != "" {
		return "scope_link"
	}
	// A modifier is declared too. Without this it only hovered where the install
	// happened to also define it as a row (EU5's modifier_type_definitions), so
	// the same token in a mod's own file produced no card at all.
	if _, ok := m.Schema().Modifier(word); ok {
		return "modifier"
	}
	_, ef, tr := m.MemberSets("")
	if ef[word] || ef[strings.ToLower(word)] {
		return "effect"
	}
	if tr[word] || tr[strings.ToLower(word)] {
		return "trigger"
	}
	return ""
}

// Schema returns the declared type system, or nil when the user has not
// run script_docs. Callers must degrade rather than guess.
func (m *Model) Schema() *declared.Schema {
	if m.cache == nil {
		return nil
	}
	return m.cache.Schema
}

func (m *Model) ScopeOfKind(kind string) string {
	if m.cache == nil || len(m.cache.KindScope) == 0 {
		return ""
	}
	if sc := m.cache.KindScope[kind]; sc != "" {
		return sc
	}
	return m.cache.KindScope[jomini.CanonicalKind(kind)]
}

// Edges come from the session, not the cache, so a mod's own on_action firing a
// mod's own event counts the same as vanilla's.
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

const maxMacroParams = 32

// base is the default-language sidecar, which every language falls back to;
// byLang holds only the extra keys a translated sidecar adds. CK3's base is
// 287,896 keys, so materialising it twelve times for a twelve-language mod was
// never affordable.
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

func (m *Model) InheritedLocKeys(langs []string) *InheritedLoc {
	defLang := m.defaultLang
	// The default-language sidecar is inherited by every language, not only its
	// own. The games fall back to the source language for any key a translation
	// omits, so a vanilla key present there is available in french too — and
	// gating this on lang == defLang meant a mod that ships eight translations
	// had every vanilla key it referenced reported missing eight times, once
	// per language and never in english.
	var sites map[string]LocEntry
	if m.vanillaLoc != nil {
		sites = m.vanillaLoc.Sites
	}
	installID, version := "", ""
	if m.cache != nil {
		installID, version = m.cache.InstallID, m.cache.GameVersion
	}

	out := &InheritedLoc{base: map[string]bool{}, byLang: map[string]map[string]bool{}}
	var flat []string
	for k := range sites {
		addLocKeys(out.base, &flat, k)
	}
	if installID == "" {
		return out
	}
	version = CacheVersion(version)
	for _, lang := range langs {
		if lang == "" || lang == defLang || out.byLang[lang] != nil {
			continue
		}
		vl, err := LoadVanillaLoc(installID, version, lang)
		if err != nil || vl == nil {
			continue
		}
		extra := map[string]bool{}
		var each []string
		for k := range vl.Sites {
			if !out.base[k] {
				addLocKeys(extra, &each, k)
			}
		}
		out.byLang[lang] = extra
	}
	return out
}
