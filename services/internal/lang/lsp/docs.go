// docs.go reads the prose and declared types the install ships, for the card,
// the completion detail and the signature popup: block-qualified prose first,
// then the kind's, then the global name, then the game's dump.

package lsp

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

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

// childOf is the learned node a key addresses below a position: the install's, then the workspace's.
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
