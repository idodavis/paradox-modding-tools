// edits.go is what the editor draws or applies; nothing here writes to disk. Formatting changes
// leading whitespace only, by brace depth, and keeps a file's line endings and its BOM or lack of
// one: a Vic3 .asset breaks on CRLF, and EU5's setup/start must have no BOM while its other folders
// need one. Folding is one range per block. The quick-fixes add a BOM or a missing loc key. Inlay
// hints give a date's era where the mod declares a calendar. A color block gets a swatch, and is
// written again in its own form with a color picked.

package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// Swatch is a color a file writes.
type Swatch struct {
	Range Range     `json:"range"` // the value that writes it
	Color game.RGBA `json:"color"` // the color it makes
}

// TextEdit replaces a range of a file with new text.
type TextEdit struct {
	Range   Range  `json:"range"`   // the text replaced
	NewText string `json:"newText"` // what replaces it
}

// WorkspaceEdit is a set of edits.
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"` // by the path of the file each applies to
}

// CodeAction is one quick-fix.
type CodeAction struct {
	Title string        `json:"title"`
	Edit  WorkspaceEdit `json:"edit"` // what applying it changes
}

// FoldingRange is the lines of one block.
type FoldingRange struct {
	StartLine int `json:"startLine"` // 0-based
	EndLine   int `json:"endLine"`   // 0-based
}

// InlayHint is text the editor draws after a position.
type InlayHint struct {
	Position Position `json:"position"` // where it is drawn
	Label    string   `json:"label"`    // the text
}

// Format returns the edits that indent a script file with tabs by brace depth, or a localization file's entries with
// one space; only leading whitespace changes, and other formats are left alone.
func Format(ws *workspace.Session, path, unit string) ([]TextEdit, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	// lines as the parse breaks them, a lone CR included, so they match LineIndents one to one
	var edits []TextEdit
	lines := file.parsed.Lines()
	line := func(i int) string {
		return strings.TrimRight(file.text[lines.OffsetAt(i, 0):lines.OffsetAt(i+1, 0)], "\r\n")
	}
	fix := func(i int, want string) {
		text := line(i)
		have := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
		if have != want && strings.TrimSpace(text) != "" {
			edits = append(edits, TextEdit{Range: Range{Start: Position{Line: i}, End: Position{Line: i, Character: len(have)}}, NewText: want})
		}
	}
	switch file.place.Format {
	case inventory.Script:
		for i, depth := range file.parsed.LineIndents() {
			fix(i, strings.Repeat(unit, depth))
		}
	case inventory.Loc:
		for i := 1; i < lines.LineCount(); i++ {
			if !strings.HasPrefix(strings.TrimSpace(line(i)), "l_") {
				fix(i, " ")
			}
		}
	default:
		return nil, nil
	}
	return edits, nil
}

// Folding returns one range per block of a script file, by the lines of its braces.
func Folding(ws *workspace.Session, path string) ([]FoldingRange, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	if file.place.Format != inventory.Script {
		return nil, nil
	}
	var out []FoldingRange
	lines := file.parsed.Lines()
	jomini.Walk(file.parsed.Root, func(statement jomini.Statement, depth int, parent *jomini.Block) bool {
		if block := jomini.ChildBlock(statement); block != nil && block.CloseBrace >= 0 {
			if from, to := lines.PositionAt(block.OpenBrace).Line, lines.PositionAt(block.CloseBrace).Line; to > from {
				out = append(out, FoldingRange{StartLine: from, EndLine: to})
			}
		}
		return true
	})
	return out, nil
}

// CodeActions returns the quick-fixes at a position: a byte order mark for a localization file without one, and a
// loc key no file defines, appended to the mod's file of the language.
func CodeActions(ws *workspace.Session, path string, line, col int) ([]CodeAction, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	var out []CodeAction
	if file.place.Format == inventory.Loc {
		if disk, err := inventory.LoadText(path); err == nil && !disk.BOM && !strings.HasPrefix(file.text, jomini.BOM) {
			out = append(out, CodeAction{Title: "Add UTF-8 BOM", Edit: WorkspaceEdit{Changes: map[string][]TextEdit{path: {{NewText: jomini.BOM}}}}})
		}
		return out, nil
	}
	ref := file.target(file.parsed.Lines().OffsetAt(line, col))
	if ref == nil || ref.Database != game.Localization || !loc.LooksLikeKey(ref.ID) {
		return out, nil
	}
	if _, known := index.Resolve(ws.Install.Schema, ws.Indexes(), *ref, path); known {
		return out, nil
	}
	target, at, unended := file.keyFile()
	if target == "" {
		return out, nil
	}
	entry := fmt.Sprintf(" %s:0 \"\"\n", ref.ID)
	if unended {
		entry = "\n" + entry
	}
	out = append(out, CodeAction{Title: fmt.Sprintf("Create localization key %q", ref.ID), Edit: WorkspaceEdit{Changes: map[string][]TextEdit{target: {{Range: Range{Start: at, End: at}, NewText: entry}}}}})
	return out, nil
}

// keyFile returns the mod's localization file in the workspace's language that a new key is appended to, the position
// at its end, and whether its text lacks a final newline; "" where the mod has none.
func (file *openFile) keyFile() (string, Position, bool) {
	ws := file.ws
	_, mod, ok := ws.Locate(file.path)
	if !ok || mod == nil {
		return "", Position{}, false
	}
	for _, at := range ws.Workspace.Files {
		if at.Origin == mod.ID && at.Format == inventory.Loc && loc.Language("", at.Rel) == ws.Language {
			if text, err := ws.Text(at.Path); err == nil {
				return at.Path, jomini.NewLineIndex(text).PositionAt(len(text)), text != "" && !strings.HasSuffix(text, "\n")
			}
		}
	}
	return "", Position{}, false
}

// InlayHints returns the era's date after every date of a file, where the mod declares a calendar and one of its eras
// applies.
func InlayHints(ws *workspace.Session, path string) ([]InlayHint, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	calendar := file.place.Calendar
	if len(calendar) == 0 || file.place.Format != inventory.Script {
		return nil, nil
	}
	var out []InlayHint
	lines := file.parsed.Lines()
	hint := func(scalar *jomini.Scalar) {
		year, month, day, _, ok := jomini.ParseDate(scalar.Text)
		if inEra, applies := calendar.Date(year, month, day); ok && applies {
			out = append(out, InlayHint{Position: lines.PositionAt(scalar.Range.End), Label: " " + inEra})
		}
	}
	jomini.Walk(file.parsed.Root, func(statement jomini.Statement, depth int, parent *jomini.Block) bool {
		switch statement := statement.(type) {
		case *jomini.Assignment:
			hint(&statement.Key)
			if scalar, ok := statement.Value.(*jomini.Scalar); ok {
				hint(scalar)
			}
		case *jomini.ValueStmt:
			if scalar, ok := statement.Value.(*jomini.Scalar); ok {
				hint(scalar)
			}
		}
		return true
	})
	return out, nil
}

// geneDatabase is the database of the install's portrait genes, whose names a DNA writes as keys.
var geneDatabase = game.DeriveDatabaseName("common/genes")

// Colors returns every color a file writes (blockColor), each a swatch before its block; every block is tried, a file
// the schema reads nothing of too.
func Colors(ws *workspace.Session, path string) ([]Swatch, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	positions := map[*jomini.Assignment]*schema.Position{}
	index.Collect(ws.Install.Schema, file.place.File, file.parsed,
		func(statement jomini.Statement, context schema.Context, inner *schema.Context, _ []schema.Occurrence) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign {
				positions[assign] = schema.ValuePosition(statement, context, *inner)
			}
			return true
		})
	var swatches []Swatch
	jomini.Walk(file.parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
		if assign, isAssign := statement.(*jomini.Assignment); isAssign {
			if color, isColor := blockColor(file, assign, positions[assign]); isColor {
				swatches = append(swatches, Swatch{Range: file.span(assign.Value.NodeRange()), Color: color})
			}
		}
		return true
	})
	return swatches, nil
}

// blockColor returns the color an assignment's block makes, by the rule hover and swatches share: a tagged block, an
// untagged one where vanilla writes only colors, or under a color key unless it names a gene (hair_color).
func blockColor(file *openFile, assign *jomini.Assignment, position *schema.Position) (game.RGBA, bool) {
	tag, parts, isBlock := jomini.ColorParts(assign.Value)
	color, known := game.ParseColor(tag, parts)
	if !isBlock || !known {
		return color, false
	}
	if tag != "" || position != nil && position.Vanilla.WritesColors() {
		return color, true
	}
	suffix := game.FindRuleByExtension(strings.ToLower(filepath.Ext(file.path))).ColorSuffix
	colorKey := suffix != "" && strings.HasSuffix(strings.ToLower(assign.Key.Text), suffix)
	return color, colorKey && len(index.Lookup(file.ws.Indexes(), schema.Defines, geneDatabase, assign.Key.Text)) == 0
}

// Recolor returns a color block written again with another color in its own form: its tag, then the parts
// game.FormatColor writes.
func Recolor(block string, color game.RGBA) (string, error) {
	var value jomini.Value
	if statements := jomini.Parse("color = " + block).Root.Statements; len(statements) == 1 {
		if assign, isAssign := statements[0].(*jomini.Assignment); isAssign {
			value = assign.Value
		}
	}
	tag, parts, isBlock := jomini.ColorParts(value)
	texts, known := game.FormatColor(tag, parts, color)
	if !isBlock || !known {
		return "", fmt.Errorf("%q is no color block", block)
	}
	text := "{ " + strings.Join(texts, " ") + " }"
	if tag != "" {
		text = tag + " " + text
	}
	return text, nil
}
