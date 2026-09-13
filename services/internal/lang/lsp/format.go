// format.go formats script/loc files and emits folding ranges for CST blocks.

package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

// FormatDocument returns leading-whitespace edits for path, or nil if already
// formatted / empty.
func FormatDocument(s *session.Session, path string) []TextEdit {
	src := s.FileText(path)
	if src == "" || s.IsMetaFile(path) {
		return nil
	}
	if s.KindFor(path) == "loc" {
		return formatLoc(src)
	}
	return formatScript(s, src, path)
}

func lineWSEdit(i, endCol int, text string) TextEdit {
	return TextEdit{
		Range: Range{
			Start: Position{Line: i, Character: 0},
			End:   Position{Line: i, Character: endCol},
		},
		NewText: text,
	}
}

func formatScript(s *session.Session, src, path string) []TextEdit {
	indents := s.Parsed(path).LineIndents()
	var edits []TextEdit
	for i, line := range splitKeep(src) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cur := leadingWS(line)
		want := strings.Repeat("\t", indents[i])
		if cur != want {
			edits = append(edits, lineWSEdit(i, len(cur), want))
		}
	}
	return edits
}

func formatLoc(src string) []TextEdit {
	var edits []TextEdit
	headerDone := false
	for i, line := range splitKeep(src) {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		cur := leadingWS(line)
		want := ""
		if headerDone {
			want = " "
		} else {
			headerDone = true
		}
		if cur != want {
			edits = append(edits, lineWSEdit(i, len(cur), want))
		}
	}
	return edits
}

func leadingWS(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

func splitKeep(src string) []string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	return strings.Split(src, "\n")
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
		start := li.PositionAt(b.OpenBrace).Line
		end := li.PositionAt(b.CloseBrace).Line
		if end > start {
			out = append(out, FoldingRange{StartLine: start, EndLine: end})
		}
		return true
	})
	return out
}
