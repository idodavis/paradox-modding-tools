// edits.go is what the editor draws or applies; nothing here writes to disk. Formatting changes
// leading whitespace only, by brace depth, and keeps a file's line endings and its BOM or lack of
// one: a Vic3 .asset breaks on CRLF, and EU5's setup/start must have no BOM while its other folders
// need one. Folding is one range per block. The quick-fixes add a BOM or a missing loc key. Inlay
// hints give a date's era where the mod declares a calendar. A color block gets a swatch, and is
// written again in its own form with a color picked.

package lsp

import (
	"bytes"
	"fmt"
	"os"
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

// Swatch is a color a file writes: the value that writes it and the color it makes.
type Swatch struct {
	Range Range     `json:"range"`
	Color game.RGBA `json:"color"`
}

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
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// InlayHint is text the editor draws after a position.
type InlayHint struct {
	Position Position `json:"position"`
	Label    string   `json:"label"`
}

// Format is the edits that indent a script file with tabs by brace depth, or a localization file's
// entries with one space. Only leading whitespace changes; other formats are left alone.
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

// Folding is one range per block of a script file, by the lines of its braces.
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

// CodeActions is the quick-fixes at a position: a BOM for a localization file without one, and a
// loc key no file defines for a word at a loc-key position, appended to the mod's file of the
// language.
func CodeActions(ws *workspace.Session, path string, line, col int) ([]CodeAction, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	var out []CodeAction
	if file.place.Format == inventory.Loc {
		if raw, err := os.ReadFile(path); err == nil && !bytes.HasPrefix(raw, []byte(jomini.BOM)) && !strings.HasPrefix(file.text, jomini.BOM) {
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

// keyFile is the mod's localization file in the workspace's language a new key is appended to, the
// position at its end, and whether its text lacks a final newline; "" where the mod has none.
func (file *openFile) keyFile() (string, Position, bool) {
	ws := file.ws
	_, mod, ok := ws.Locate(file.path)
	if !ok || mod == nil {
		return "", Position{}, false
	}
	for _, at := range ws.Workspace.Files {
		if at.Origin == mod.ID && at.Format == inventory.Loc && loc.LanguageFromRel(at.Rel) == ws.Language {
			if text, err := ws.Text(at.Path); err == nil {
				return at.Path, jomini.NewLineIndex(text).PositionAt(len(text)), text != "" && !strings.HasSuffix(text, "\n")
			}
		}
	}
	return "", Position{}, false
}

// InlayHints is the era's date after every date of a file, where the mod declares a calendar and
// one of its eras applies.
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
var geneDatabase = game.NameOf("common/genes")

// Colors is every color a file writes (blockColor), each a swatch before its block. The file's walk over the schema
// gives each block its position; every block is tried, a file the schema reads nothing of too.
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

// blockColor is the color an assignment's block makes, the one rule hover and the swatches share: a tagged color block
// (rgb { }, hsv { }) under any key; an untagged one at a position where vanilla writes only colors or words that name
// them (a named color's entry, a coat of arms' color1: Vanilla.WritesColors), and under a key ending as its file type's
// color keys do (color, fontcolor), but a key that names one of the install's genes (a DNA's hair_color holds two
// pairs into a color map). The position is the block's in the schema; nil where it has none.
func blockColor(file *openFile, assign *jomini.Assignment, position *schema.Position) (game.RGBA, bool) {
	tag, parts, isBlock := jomini.ColorParts(assign.Value)
	color, known := game.ColorOf(tag, parts)
	if !isBlock || !known {
		return color, false
	}
	if tag != "" || position != nil && position.Vanilla.WritesColors() {
		return color, true
	}
	suffix := game.FileTypes[strings.ToLower(filepath.Ext(file.path))].ColorSuffix
	colorKey := suffix != "" && strings.HasSuffix(strings.ToLower(assign.Key.Text), suffix)
	return color, colorKey && len(index.Lookup(file.ws.Indexes(), schema.Defines, geneDatabase, assign.Key.Text)) == 0
}

// Recolor is a color block written again with another color in its own form: its tag, then the parts game.PartsOf
// writes.
func Recolor(block string, color game.RGBA) (string, error) {
	var value jomini.Value
	if statements := jomini.Parse("color = " + block).Root.Statements; len(statements) == 1 {
		if assign, isAssign := statements[0].(*jomini.Assignment); isAssign {
			value = assign.Value
		}
	}
	tag, parts, isBlock := jomini.ColorParts(value)
	texts, known := game.PartsOf(tag, parts, color)
	if !isBlock || !known {
		return "", fmt.Errorf("%q is no color block", block)
	}
	text := "{ " + strings.Join(texts, " ") + " }"
	if tag != "" {
		text = tag + " " + text
	}
	return text, nil
}
