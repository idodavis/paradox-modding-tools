// edits.go is what the editor draws or applies and nothing writes to disk: formatting (leading whitespace only, by brace depth; a file keeps its line endings and its byte order mark or lack of one, because a Vic3 .asset breaks on CRLF and EU5's setup/start must have no BOM while its other folders need one), folding (one range per block), the two quick-fixes (a BOM for a localization file, a localization key for a word nothing defines, into the mod's existing file), and the era of a date where the mod declares a calendar.
package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// TextEdit replaces a range of a file with new text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit is edits by the path of the file each applies to.
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

// CodeAction is one quick-fix.
type CodeAction struct {
	Title string        `json:"title"`
	Edit  WorkspaceEdit `json:"edit"`
}

// FoldingRange is one block's lines.
type FoldingRange struct {
	StartLine, EndLine int
}

// InlayHint is text the editor draws after a position.
type InlayHint struct {
	Position Position `json:"position"`
	Label    string   `json:"label"`
}

// Format is the edits that indent a script file with tabs by brace depth, or a localization file's entries with one space. Only leading whitespace changes; a file of another format is left alone.
func Format(s *session.Session, path string) ([]TextEdit, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	var edits []TextEdit
	lines := strings.Split(f.text, "\n")
	fix := func(i int, want string) {
		line := strings.TrimRight(lines[i], "\r")
		have := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if have != want && strings.TrimSpace(line) != "" {
			edits = append(edits, TextEdit{Range: Range{Start: Position{Line: i}, End: Position{Line: i, Character: len(have)}}, NewText: want})
		}
	}
	switch f.place.Format {
	case source.Script:
		for i, depth := range f.parsed.LineIndents() {
			fix(i, strings.Repeat("\t", depth))
		}
	case source.Loc:
		for i, line := range lines {
			if i > 0 && !strings.HasPrefix(strings.TrimSpace(line), "l_") {
				fix(i, " ")
			}
		}
	default:
		return nil, nil
	}
	return edits, nil
}

// Folding is one range per block of a script file, by the lines of its braces.
func Folding(s *session.Session, path string) ([]FoldingRange, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	if f.place.Format != source.Script {
		return nil, nil
	}
	var out []FoldingRange
	lines := f.parsed.Lines()
	jomini.Walk(f.parsed.Root, func(st jomini.Statement, depth int, parent *jomini.Block) bool {
		if block := jomini.ChildBlock(st); block != nil && block.CloseBrace >= 0 {
			if from, to := lines.PositionAt(block.OpenBrace).Line, lines.PositionAt(block.CloseBrace).Line; to > from {
				out = append(out, FoldingRange{StartLine: from, EndLine: to})
			}
		}
		return true
	})
	return out, nil
}

// CodeActions is the quick-fixes at a position: a byte order mark for a localization file without one, and a localization key for a word written where a loc key goes that no file defines, appended to the mod's own file of the language.
func CodeActions(s *session.Session, path string, line, col int) ([]CodeAction, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	var out []CodeAction
	if f.place.Format == source.Loc {
		if raw, err := os.ReadFile(path); err == nil && !jomini.HasUTF8BOM(raw) && !strings.HasPrefix(f.text, "\xEF\xBB\xBF") {
			out = append(out, CodeAction{Title: "Add UTF-8 BOM", Edit: WorkspaceEdit{Changes: map[string][]TextEdit{path: {{NewText: "\xEF\xBB\xBF"}}}}})
		}
		return out, nil
	}
	w := f.at(f.offset(line, col))
	if w == nil || w.IsKey || w.Level.Below == nil || s.All().Loc(w.Text) {
		return out, nil
	}
	if !slices.ContainsFunc(w.Level.Below.Accepts, func(a schema.Accept) bool { return a.Type == schema.TypeLoc }) || !loc.LooksLikeKey(w.Text) {
		return out, nil
	}
	target, at := f.locFile()
	if target == "" {
		return out, nil
	}
	out = append(out, CodeAction{Title: fmt.Sprintf("Create localization key %q", w.Text), Edit: WorkspaceEdit{Changes: map[string][]TextEdit{target: {{Range: Range{Start: at, End: at}, NewText: fmt.Sprintf(" %s:0 \"\"\n", w.Text)}}}}})
	return out, nil
}

// locFile is the mod's localization file of the workspace's language to append a key to, and the position after its last line; "" where the mod has none.
func (f *file) locFile() (string, Position) {
	s := f.s
	_, m, ok := s.Located(f.path)
	if !ok || m == nil {
		return "", Position{}
	}
	for _, at := range s.Workspace.Files {
		if at.Origin == m.ID && at.Format == source.Loc && loc.LanguageFromRel(at.Rel) == s.Language {
			text, err := s.Text(at.Path)
			if err != nil {
				continue
			}
			lines := strings.Count(text, "\n")
			if !strings.HasSuffix(text, "\n") {
				lines++
			}
			return at.Path, Position{Line: lines}
		}
	}
	return filepath.Join(m.Dir, "localization", s.Language, m.ID+"_l_"+s.Language+".yml"), Position{}
}

// InlayHints is the era's date after every date of a file, where the mod declares a calendar and the year stays positive.
func InlayHints(s *session.Session, path string) ([]InlayHint, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	calendar := f.place.Calendar
	if calendar.Era == "" || f.place.Format != source.Script {
		return nil, nil
	}
	var out []InlayHint
	lines := f.parsed.Lines()
	hint := func(scalar *jomini.Scalar) {
		if y, m, d, _, ok := jomini.ParseDate(scalar.Text); ok && y-calendar.EpochOffset > 0 {
			out = append(out, InlayHint{Position: lines.PositionAt(scalar.Range.End), Label: fmt.Sprintf(" %d.%d.%d %s", y-calendar.EpochOffset, m, d, calendar.Era)})
		}
	}
	jomini.Walk(f.parsed.Root, func(st jomini.Statement, depth int, parent *jomini.Block) bool {
		switch st := st.(type) {
		case *jomini.Assignment:
			hint(&st.Key)
			if scalar, ok := st.Value.(*jomini.Scalar); ok {
				hint(scalar)
			}
		case *jomini.ValueStmt:
			if scalar, ok := st.Value.(*jomini.Scalar); ok {
				hint(scalar)
			}
		}
		return true
	})
	return out, nil
}
