// codeactions.go offers create-loc-key and add-BOM quick fixes.
package lsp

import (
	"path/filepath"
	"strings"

	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

const bom = "\uFEFF"

// CodeActions returns quick-fixes for path.
func CodeActions(s *session.Session, path string) []CodeAction {
	var out []CodeAction
	if s.KindFor(path) == "loc" {
		src := s.FileText(path)
		if src != "" && !locHasBOM(path, src) {
			out = append(out, CodeAction{
				Title: "Add UTF-8 BOM",
				Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{
					path: {{Range: Range{}, NewText: bom}},
				}},
			})
		}
	}
	seen := map[string]bool{}
	for _, r := range s.RefsInFile(path) {
		if r.Kind != "loc" || s.LocDefined(r.Key) || seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		if edit := locCreateEdit(s, path, r.Key); edit != nil {
			out = append(out, CodeAction{
				Title: "Create localization key \"" + r.Key + "\"",
				Edit:  edit,
			})
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
		return &WorkspaceEdit{Changes: map[string][]TextEdit{
			target: {{
				Range:   Range{Start: Position{Line: line, Character: 0}, End: Position{Line: line, Character: 0}},
				NewText: entry,
			}},
		}}
	}
	body := bom + "l_" + lang + ":\n" + entry
	return &WorkspaceEdit{
		Create: []string{target},
		Changes: map[string][]TextEdit{
			target: {{Range: Range{}, NewText: body}},
		},
	}
}

func locTarget(s *session.Session, root, lang string) (path string, exists bool) {
	preferred := filepath.Join(root, "localization", lang, "zzz_pmt_edits_l_"+lang+".yml")
	if fileExists(preferred) {
		return preferred, true
	}
	origin := ""
	for _, m := range s.Mods() {
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
