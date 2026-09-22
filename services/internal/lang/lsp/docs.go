// docs.go is the prose: what the game declares about a token, a link or a field, and the comment a modder wrote above a definition, read from the file when a card is built and never stored.
package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
)

// tokenDocs is a declared token's prose and usage, from whichever vocabulary declares it.
func tokenDocs(v *schema.Vocab, name string) (docs, usage string, in []string, out string) {
	if t, ok := v.Effects[name]; ok {
		return t.Doc, t.Usage, t.In, t.Out
	}
	if t, ok := v.Triggers[name]; ok {
		return t.Doc, t.Usage, t.In, t.Out
	}
	if m, ok := v.Modifiers[name]; ok {
		return m.Doc, "", m.Categories, ""
	}
	return "", "", nil, ""
}

// fieldDocs is what the game's info file says of a key under its parent key, in the folders of the kind the file defines.
func fieldDocs(s *session.Session, level index.Level, key string) string {
	if level.Kind == nil {
		return ""
	}
	parent := ""
	if level.Field != nil {
		parent = level.Field.Name
	}
	return s.Install.Info.Field(level.Kind.Folders, parent, key).Prose
}

// comment is the comment written on the lines just above a line of a file, joined; "" where there is none.
func comment(text string, line int) string {
	lines := strings.Split(text, "\n")
	var above []string
	for i := line - 1; i >= 0 && i < len(lines); i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		above = append([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))}, above...)
	}
	return strings.Join(above, "\n")
}

// spell writes a field's accepts as the schema file spells them.
func spell(f *schema.Field) []string {
	if f == nil {
		return nil
	}
	var out []string
	for _, a := range f.Accepts {
		switch a.Type {
		case schema.TypeRef:
			out = append(out, "ref:"+a.Kind)
		case schema.TypeEnum:
			out = append(out, "enum:"+strings.Join(a.Enum, "|"))
		default:
			out = append(out, a.Type.String())
		}
	}
	return out
}
