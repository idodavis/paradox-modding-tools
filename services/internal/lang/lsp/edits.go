// edits.go is what the editor draws or applies without a cursor: whitespace
// formatting, folding ranges, the two quick-fixes, and the era hints beside
// dates. None of it writes to the file.

package lsp

import (
	"path/filepath"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

const bom = string(rune(0xFEFF))

// FormatDocument returns leading-whitespace edits for path, or nil if already formatted / empty.
func FormatDocument(s *session.Session, path string) []TextEdit {
	src := s.FileText(path)
	if src == "" || s.IsMetaFile(path) {
		return nil
	}
	var edits []TextEdit
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n"), "\n")
	if s.KindFor(path) == "loc" {
		// Loc indents with one leading space after the header, never a tab.
		headerDone := false
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			if trim == "" || strings.HasPrefix(trim, "#") {
				continue
			}
			want := " "
			if !headerDone {
				want, headerDone = "", true
			}
			if cur := leadingWS(line); cur != want {
				edits = append(edits, lineWSEdit(i, len(cur), want))
			}
		}
		return edits
	}
	indents := s.Parsed(path).LineIndents()
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cur, want := leadingWS(line), strings.Repeat("\t", indents[i])
		if cur != want {
			edits = append(edits, lineWSEdit(i, len(cur), want))
		}
	}
	return edits
}

func lineWSEdit(i, endCol int, text string) TextEdit {
	return TextEdit{Range: Range{Start: Position{Line: i, Character: 0}, End: Position{Line: i, Character: endCol}}, NewText: text}
}

func leadingWS(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// FoldingRanges returns foldable block ranges for path.
func FoldingRanges(s *session.Session, path string) []FoldingRange {
	src := s.FileText(path)
	if src == "" || s.IsMetaFile(path) || s.KindFor(path) == "loc" {
		return nil
	}
	res := s.Parsed(path)
	li := res.Lines()
	var out []FoldingRange
	jomini.Walk(res.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		b := jomini.ChildBlock(st)
		if b == nil || b.CloseBrace < 0 {
			return true
		}
		start, end := li.PositionAt(b.OpenBrace).Line, li.PositionAt(b.CloseBrace).Line
		if end > start {
			out = append(out, FoldingRange{StartLine: start, EndLine: end})
		}
		return true
	})
	return out
}

// CodeActions returns the quick-fixes for path: add a UTF-8 BOM to a loc file, and create a localization key nothing defines.
func CodeActions(s *session.Session, path string) []CodeAction {
	var out []CodeAction
	if s.KindFor(path) == "loc" {
		if src := s.FileText(path); src != "" && !locHasBOM(path, src) {
			out = append(out, CodeAction{Title: "Add UTF-8 BOM", Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{path: {{Range: Range{}, NewText: bom}}}}})
		}
	}
	seen := map[string]bool{}
	for _, r := range s.RefsInFile(path) {
		if r.Kind != "loc" || s.LocDefined(r.Key) || seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		if edit := locCreateEdit(s, path, r.Key); edit != nil {
			out = append(out, CodeAction{Title: "Create localization key \"" + r.Key + "\"", Edit: edit})
		}
	}
	return out
}

func locCreateEdit(s *session.Session, fromPath, key string) *WorkspaceEdit {
	root := modRootOf(s, fromPath)
	if root == "" {
		return nil
	}
	lang := loc.LanguageFromFilename(fromPath)
	if lang == "" {
		lang = s.DefaultLang()
	}
	target, exists := locTarget(s, root, lang)
	entry := " " + key + ":0 \"\"\n"
	if exists {
		text, _ := strings.CutPrefix(s.FileText(target), bom)
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		line := len(lines)
		if line > 0 && lines[line-1] == "" {
			line--
		}
		at := Position{Line: line, Character: 0}
		return &WorkspaceEdit{Changes: map[string][]TextEdit{target: {{Range: Range{Start: at, End: at}, NewText: entry}}}}
	}
	return &WorkspaceEdit{Create: []string{target}, Changes: map[string][]TextEdit{target: {{Range: Range{}, NewText: bom + "l_" + lang + ":\n" + entry}}}}
}

func locTarget(s *session.Session, root, lang string) (path string, exists bool) {
	preferred := filepath.Join(root, "localization", lang, "zzz_pmt_edits_l_"+lang+".yml")
	if fileExists(preferred) {
		return preferred, true
	}
	origin := ""
	for _, m := range s.Mods {
		if m.Root == root {
			origin = m.Origin
			break
		}
	}
	if path, ok := s.LocFile(origin, lang); ok {
		return path, true
	}
	return preferred, false
}

// InlayHints returns the era form of every date literal in a file: a reading aid drawn by the editor, and nothing at all for a mod that declares no calendar.
func InlayHints(s *session.Session, path string) []InlayHint {
	cal := s.CalendarFor(path)
	if !cal.Declared() {
		return nil
	}
	res := s.Parsed(path)
	if res.Root == nil {
		return nil
	}
	li := res.Lines()
	var out []InlayHint
	add := func(sc jomini.Scalar) {
		if sc.Quoted || jomini.Literal(sc.Text) != jomini.LitDate {
			return
		}
		era := index.EraDate(sc.Text, cal)
		if era == "" {
			return
		}
		out = append(out, InlayHint{Range: byteRange(li, sc.Range.Start, sc.Range.End), Label: " " + era,
			Tooltip: sc.Text + " in this mod's calendar is " + era + "."})
	}
	jomini.Walk(res.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		switch n := st.(type) {
		case *jomini.Assignment:
			// Both sides: history files key their blocks BY a date.
			add(n.Key)
			if v, ok := n.Value.(*jomini.Scalar); ok {
				add(*v)
			}
		case *jomini.ValueStmt:
			if v, ok := n.Value.(*jomini.Scalar); ok {
				add(*v)
			}
		}
		return true
	})
	return out
}
