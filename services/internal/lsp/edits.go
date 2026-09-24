// edits.go is what the editor draws or applies; nothing here writes to disk. Formatting changes
// leading whitespace only, by brace depth, and keeps a file's line endings and its BOM or lack of
// one: a Vic3 .asset breaks on CRLF, and EU5's setup/start must have no BOM while its other folders
// need one. Folding is one range per block. The quick-fixes add a BOM or a missing loc key. Inlay
// hints give a date's era where the mod declares a calendar.
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
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
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

// Format is the edits that indent a script file with tabs by brace depth, or a localization file's
// entries with one space. Only leading whitespace changes; other formats are left alone.
func Format(sess *session.Session, path string) ([]TextEdit, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	var edits []TextEdit
	lines := strings.Split(file.text, "\n")
	fix := func(i int, want string) {
		line := strings.TrimRight(lines[i], "\r")
		have := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if have != want && strings.TrimSpace(line) != "" {
			edits = append(edits, TextEdit{Range: Range{Start: Position{Line: i}, End: Position{Line: i, Character: len(have)}}, NewText: want})
		}
	}
	switch file.place.Format {
	case source.Script:
		for i, depth := range file.parsed.LineIndents() {
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
func Folding(sess *session.Session, path string) ([]FoldingRange, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	if file.place.Format != source.Script {
		return nil, nil
	}
	var out []FoldingRange
	lines := file.parsed.Lines()
	jomini.Walk(file.parsed.Root, func(st jomini.Statement, depth int, parent *jomini.Block) bool {
		if block := jomini.ChildBlock(st); block != nil && block.CloseBrace >= 0 {
			if from, to := lines.PositionAt(block.OpenBrace).Line, lines.PositionAt(block.CloseBrace).Line; to > from {
				out = append(out, FoldingRange{StartLine: from, EndLine: to})
			}
		}
		return true
	})
	return out, nil
}

// CodeActions is the quick-fixes at a position: a BOM for a localization file without one, and a
// loc key no file defines for a word in a loc-key slot, appended to the mod's file of the language.
func CodeActions(sess *session.Session, path string, line, col int) ([]CodeAction, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	var out []CodeAction
	if file.place.Format == source.Loc {
		if raw, err := os.ReadFile(path); err == nil && !jomini.HasUTF8BOM(raw) && !strings.HasPrefix(file.text, "\xEF\xBB\xBF") {
			out = append(out, CodeAction{Title: "Add UTF-8 BOM", Edit: WorkspaceEdit{Changes: map[string][]TextEdit{path: {{NewText: "\xEF\xBB\xBF"}}}}})
		}
		return out, nil
	}
	word := file.at(file.offset(line, col))
	if word == nil || word.IsKey || word.Level.Below == nil || sess.All().Loc(word.Text) {
		return out, nil
	}
	if !slices.ContainsFunc(word.Level.Below.Accepts, func(accept schema.Accept) bool { return accept.Type == schema.TypeLoc }) || !loc.LooksLikeKey(word.Text) {
		return out, nil
	}
	target, at := file.locFile()
	if target == "" {
		return out, nil
	}
	out = append(out, CodeAction{Title: fmt.Sprintf("Create localization key %q", word.Text), Edit: WorkspaceEdit{Changes: map[string][]TextEdit{target: {{Range: Range{Start: at, End: at}, NewText: fmt.Sprintf(" %s:0 \"\"\n", word.Text)}}}}})
	return out, nil
}

// locFile is the mod's localization file in the workspace's language to append a key to, and the
// position after its last line; a new file's path where the mod has none, and "" outside a mod.
func (file *file) locFile() (string, Position) {
	sess := file.sess
	_, mod, ok := sess.Located(file.path)
	if !ok || mod == nil {
		return "", Position{}
	}
	for _, at := range sess.Workspace.Files {
		if at.Origin == mod.ID && at.Format == source.Loc && loc.LanguageFromRel(at.Rel) == sess.Language {
			text, err := sess.Text(at.Path)
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
	return filepath.Join(mod.Dir, "localization", sess.Language, mod.ID+"_l_"+sess.Language+".yml"), Position{}
}

// InlayHints is the era's date after every date of a file, where the mod declares a calendar and
// the year stays positive.
func InlayHints(sess *session.Session, path string) ([]InlayHint, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	calendar := file.place.Calendar
	if calendar.Era == "" || file.place.Format != source.Script {
		return nil, nil
	}
	var out []InlayHint
	lines := file.parsed.Lines()
	hint := func(scalar *jomini.Scalar) {
		if year, month, day, _, ok := jomini.ParseDate(scalar.Text); ok && year-calendar.EpochOffset > 0 {
			out = append(out, InlayHint{Position: lines.PositionAt(scalar.Range.End), Label: fmt.Sprintf(" %d.%d.%d %s", year-calendar.EpochOffset, month, day, calendar.Era)})
		}
	}
	jomini.Walk(file.parsed.Root, func(st jomini.Statement, depth int, parent *jomini.Block) bool {
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
