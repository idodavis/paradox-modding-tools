// docs.go reads the prose and declared types the install ships, for the card,
// the completion detail and the signature popup: block-qualified prose first,
// then the kind's, then the global name, then the game's dump. declaredLines
// is the one ladder the card's three declared lines share.

package lsp

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

// PrettyKind is the Kind line: CanonicalKind, then underscores to spaces.
func PrettyKind(kind string) string {
	k := jomini.CanonicalKind(kind)
	if k == "" {
		k = kind
	}
	return strings.ReplaceAll(k, "_", " ")
}

// kindFieldDoc is what KIND's own docs say about a field, under the enclosing block first.
func kindFieldDoc(s *session.Session, key, kind, block string) string {
	fields, lk := s.Install.Docs.Fields, strings.ToLower(key)
	if block != "" {
		if v := fields[strings.ToLower(block)][lk]; v != "" {
			return blurb(v)
		}
	}
	if kind == "" {
		return ""
	}
	return blurb(fields[strings.ToLower(kind)][lk])
}

// fieldDoc tries the block-qualified prose, then the kind's, then the global name, then the game's dump doc, then PMT's own gloss for the block keywords no game documents.
func fieldDoc(s *session.Session, key, kind, block string) string {
	d := s.Install.Docs
	for _, k := range []string{key, portraitSibling(key)} {
		if k == "" {
			continue
		}
		if v := kindFieldDoc(s, k, kind, block); v != "" {
			return v
		}
		if v := d.FieldInfo[strings.ToLower(k)]; v != "" {
			return blurb(v)
		}
		if v := s.Install.Declared.Doc(strings.ToLower(k)); v != "" {
			return blurb(v)
		}
	}
	return jomini.StructuralDoc(key)
}

func tokenUsage(s *session.Session, key string) string {
	if u := s.Install.Declared.Usage(key); u != "" {
		return u
	}
	sib := portraitSibling(key)
	if s.Install.Docs.FieldInfo[strings.ToLower(key)] != "" || sib == "" || s.Install.Docs.FieldInfo[strings.ToLower(sib)] != "" {
		return ""
	}
	return s.Install.Declared.Usage(sib)
}

func tokenTarget(s *session.Session, key string) string {
	if t, _, ok := s.Install.Declared.Token(key); ok {
		return t.Target
	}
	return ""
}

// schemaDoc is the description the game ships for an engine token or link.
func schemaDoc(s *session.Session, word string) string {
	if t, _, ok := s.Install.Declared.Token(word); ok && t.Doc != "" {
		return blurb(t.Doc)
	}
	if l, ok := s.Install.Declared.Link(word); ok {
		return blurb(l.Doc)
	}
	return ""
}

// docsOnly is the prose a name carries plus the signature the game declares for it.
func docsOnly(s *session.Session, word, kind string) string {
	doc := fieldDoc(s, word, kind, "")
	if doc == "" {
		doc = schemaDoc(s, word)
	}
	line, _, _ := declaredLines(s, kind, nil, word)
	if line == "" {
		return doc
	}
	if doc != "" {
		doc += "\n\n"
	}
	return doc + line
}

// vocabRole names an engine token's role: effect, trigger, scope link or modifier.
func vocabRole(s *session.Session, word string) string {
	if _, role, ok := s.Install.Declared.Token(word); ok {
		return role
	}
	if l, ok := s.Install.Declared.Link(word); ok && l.Out != "" {
		return "scope_link"
	}
	if _, ok := s.Install.Declared.Modifier(word); ok {
		return "modifier"
	}
	return ""
}

// declaredFieldType is the kind's declared type for a field, else the global one.
func declaredFieldType(s *session.Session, kind, field string) declared.DeclaredType {
	d, lf := s.Install.Docs, strings.ToLower(field)
	for _, k := range []string{kind, jomini.CanonicalKind(kind)} {
		if t, ok := d.DeclaredFieldTypesByKind[k][lf]; ok && k != "" {
			return t
		}
	}
	return d.DeclaredFieldTypes[lf]
}

// macroUsage is a macro's call signature; one with no parameters gets nothing.
func macroUsage(s *session.Session, d *index.Def) string {
	if d == nil || !jomini.IsMacroKind(d.Kind) {
		return ""
	}
	names := s.Params(d.Key)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(d.Key + " = {\n")
	for _, n := range names {
		b.WriteString("\t" + n + " = <value>\n")
	}
	b.WriteString("}")
	return b.String()
}

// blurb is the first paragraph of a doc, cut at a sentence end past the cap: a card shows what a field is, not the section above it.
func blurb(doc string) string {
	if i := strings.Index(doc, "\n\n"); i >= 0 {
		doc = doc[:i]
	}
	const cap = 320
	if len(doc) <= cap {
		return doc
	}
	if i := strings.LastIndexAny(doc[:cap], ".;"); i > cap/2 {
		return doc[:i+1]
	}
	return doc[:cap] + "…"
}

// clipUsage keeps the first lines of a signature; CK3 lists thirty arguments for create_character.
func clipUsage(u string) string {
	lines := strings.Split(u, "\n")
	const keep = 12
	if len(lines) <= keep {
		return u
	}
	return strings.Join(lines[:keep], "\n") + "\n… (" + strconv.Itoa(len(lines)-keep) + " more lines)"
}

// portraitSibling maps left_foo ↔ right_foo for script_docs pair fallback.
func portraitSibling(key string) string {
	k := strings.ToLower(key)
	switch {
	case strings.HasPrefix(k, "left_"):
		return "right_" + k[len("left_"):]
	case strings.HasPrefix(k, "right_"):
		return "left_" + k[len("right_"):]
	}
	return ""
}

// tokens are the declared names of one role, sorted.
func tokens(s *session.Session, role string) []string {
	m := s.Install.Declared.Effects
	if role == "trigger" {
		m = s.Install.Declared.Triggers
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// childOf is the learned node a key addresses below a position.
func childOf(s *session.Session, hang *index.Node, key string) *index.Node {
	if hang == nil {
		return nil
	}
	return s.Install.Tree.Child(hang, key, false, false, false)
}

// isMember reports a key the structure at this position holds, in either spelling.
func isMember(s *session.Session, hang *index.Node, key string) bool {
	if hang == nil {
		return false
	}
	at := s.Install.Tree.Structs[hang.Struct]
	if len(at) == 0 {
		at = s.Work.Tree.Structs[hang.Struct]
	}
	if _, ok := slices.BinarySearch(at, key); ok {
		return true
	}
	_, ok := slices.BinarySearch(at, strings.ToLower(key))
	return ok
}

// declaredLines is the one ordered ladder the card's three declared lines share: the signature the game states for a name, what the value at that position may be, and what the name's kind is. One pass, one order (declared before derived, specific before general), three lines.
func declaredLines(s *session.Session, kind string, hang *index.Node, name string) (sig, accepts, hint string) {
	// 1. The declared signature of the name itself: token, then scope link, then modifier, then the raw scopes text of a game with no type system.
	if t, role, ok := s.Install.Declared.Token(name); ok {
		parts := []string{strings.ToUpper(role[:1]) + role[1:]}
		if in := scopeList(t.In); in != "" {
			parts = append(parts, "runs on "+in)
		}
		if t.Target != "" {
			parts = append(parts, "takes a "+PrettyKind(t.Target))
		}
		sig = strings.Join(parts, " · ")
	} else if l, ok := s.Install.Declared.Link(name); ok && l.Out != "" {
		var parts []string
		if in := scopeList(l.In); in != "" {
			parts = append(parts, "from "+in)
		} else if l.Global {
			parts = append(parts, "Global link")
		}
		parts = append(parts, "yields "+PrettyKind(l.Out))
		if l.Data {
			parts = append(parts, "cite `"+name+":id`")
		}
		sig = strings.Join(parts, " · ")
	} else if m, ok := s.Install.Declared.Modifier(name); ok {
		// EU5 declares a category for all its modifiers and prose for none, so the category list is the whole card.
		if areas := modifierAreas(m.Area); len(areas) > 0 {
			sig = "Modifier · applies to " + strings.Join(areas, " or ")
		}
	} else if sc := s.Install.Declared.ScopeText(name); sc != "" {
		sig = "Scope here: " + sc
	}

	// 2. What the value may be: the declared target and, when a database is bound to it, the citation form; below it script_docs' own slot, then the game's written documentation, then the learned cell, which is only ever a sample of how vanilla happens to use the field.
	r, isSlot := jomini.ScriptName(name)
	dt := declaredFieldType(s, kind, name)
	cellKind := childOf(s, hang, name).Database()
	if target := tokenTarget(s, name); target != "" {
		cite, k := s.Install.Tree.CiteForm(target)
		switch {
		case k != "" && cite != "":
			accepts = "accepts a " + PrettyKind(k) + ", cited `" + cite + ":<id>`"
		case k != "":
			accepts = "accepts a " + PrettyKind(k)
		default:
			accepts = "accepts a " + PrettyKind(target)
		}
	} else if isSlot && r.Kind != "" {
		accepts = "accepts a " + PrettyKind(r.Kind)
	} else if dt.Known() {
		switch {
		case len(dt.Values) > 0:
			accepts = "one of: " + strings.Join(dt.Values, ", ")
		case dt.Array:
			accepts = "accepts several " + dt.Prim + " values"
		default:
			accepts = "accepts a " + dt.Prim
		}
	} else if cellKind != "" && !jomini.IsEphemeral(cellKind) {
		accepts = "accepts a " + PrettyKind(cellKind)
	} else if loc.Classify(name) != loc.PropNone {
		// A field holding a localization key takes any key, not one of a set.
		accepts = "accepts a localization key"
	}

	// 3. What the kind is, for a card whose subject has no prose of its own: the declared scope type first, then how the object is cited and nested.
	ck := jomini.CanonicalKind(kind)
	if ck == "" {
		ck = kind
	}
	if languageKind(ck) {
		return sig, accepts, ""
	}
	var parts []string
	if scope := s.Install.Tree.ScopeOf(ck); scope != "" {
		parts = append(parts, PrettyKind(scope)+" scope")
	}
	if p := s.Install.Docs.KindInfo[ck]; p != "" {
		parts = append(parts, firstProse(p))
	} else if p := s.Install.Docs.KindInfo[kind]; p != "" {
		parts = append(parts, firstProse(p))
	}
	if prefix := s.Install.Tree.Prefix(ck); prefix != "" {
		parts = append(parts, "cite `"+prefix+":id`")
	}
	for _, sh := range s.Install.Tree.Nested {
		if jomini.CanonicalKind(sh.ChildKind) == ck {
			msg := "nested in " + PrettyKind(sh.ParentKind)
			if sh.GroupKey != "" {
				msg += " `" + sh.GroupKey + "`"
			}
			parts = append(parts, msg)
			break
		}
	}

	// The real signature of a macro with parameters is macroUsage, shown as a code block; this is the shape note for one that takes none.
	if jomini.IsMacroKind(ck) {
		parts = append(parts, "invoked by name")
	}
	if game.IsFIOS(s.GameID, ck) || game.IsFIOS(s.GameID, kind) {
		parts = append(parts, "FIOS")
	}
	return sig, accepts, strings.Join(parts, " · ")
}

// modifierAreas splits a declared category list into the scope types it names; the three games phrase it differently, so the separators of all three are accepted, and "all" is dropped because EU5 stamps it on every modifier.
func modifierAreas(area string) []string {
	if area == "" {
		return nil
	}
	fields := strings.FieldsFunc(area, func(r rune) bool { return r == ',' || r == ';' || r == '/' || r == '|' })
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		for part := range strings.SplitSeq(strings.TrimSpace(f), " and ") {
			p := strings.ToLower(strings.TrimSpace(part))
			if p == "" || p == "all" || p == "none" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, PrettyKind(p))
		}
	}
	return out
}

// scopeList renders declared input scopes; "none" means unrestricted and carries nothing worth showing.
func scopeList(in []string) string {
	keep := make([]string, 0, len(in))
	for _, sc := range in {
		if sc != "none" && sc != "" {
			keep = append(keep, PrettyKind(sc))
		}
	}
	return strings.Join(keep, " or ")
}

func languageKind(k string) bool {
	switch k {
	case "effect", "trigger", "scope", "field", "loc_key", "loc_value", "script_param", "saved_scope", "data_function", "mod_descriptor":
		return true
	}
	return false
}

func firstProse(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
