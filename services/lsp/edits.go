package lsp

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Fix is one quick fix offered at a problem: its title and the edits that apply it, by file path.
type Fix struct {
	Title string            `json:"title"`
	Code  string            `json:"code"` // the diagnostic it fixes
	Edits map[string][]Edit `json:"edits"`
}

// fixDistance is how many edits away a defined name is too far off to be offered as the fix for an unresolved
// reference.
const fixDistance = 3

// Format formats a file and returns the edits that turn the old text into the new. It keeps the file's own
// indentation, or uses the editor's settings when the file has none.
func (lsp *Service) Format(path string, tabSize int, insertSpaces bool) ([]Edit, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	// a file that does not parse is left as it is
	if !ws.Locate(path).Format.IsScript() || len(tree.Errors) > 0 {
		return []Edit{}, nil
	}
	unit := tree.Indentation()
	if unit == "" {
		unit = "\t"
		if insertSpaces && tabSize > 0 {
			unit = strings.Repeat(" ", tabSize)
		}
	}
	text := encodeTree(tree, listPieces(tree), unit)
	// a file whose new text would lose any character but a space is left as it is
	if strings.Join(strings.Fields(text), "") != strings.Join(strings.Fields(tree.Src), "") {
		return []Edit{}, nil
	}
	return ToEdits(tree.Src, tree.Lines, jomini.DiffEdits(tree.Src, text)), nil
}

// A piece is one run of a file's text that formatting keeps whole: a key, a tag or a value, an operator, a brace or a
// comment. Its depth is how many blocks it is inside; a closing brace is outside the block it closes.
type piece struct {
	span  jomini.Span
	kind  pieceKind
	depth int
}

// A pieceKind says what a piece is, which decides the spacing around it.
type pieceKind int

// The kinds of piece.
const (
	pieceText pieceKind = iota // a key, a tag or a value
	pieceOperator
	pieceOpen // the brace that opens a block
	pieceClose
	pieceComment
)

// listPieces returns the pieces of a tree in the order they are written: what each statement is made of, and the
// comments.
func listPieces(tree *jomini.Tree) []piece {
	var pieces []piece
	for _, statement := range tree.Statements {
		pieces = append(pieces, piece{statement.Key, pieceText, 0}, piece{statement.Op, pieceOperator, 0},
			piece{statement.Tag, pieceText, 0}, piece{statement.Value, pieceText, 0})
		if statement.IsBlock() {
			pieces = append(pieces, piece{jomini.Span{Start: statement.Block.Start, End: statement.Block.Start + 1}, pieceOpen, 0},
				piece{jomini.Span{Start: statement.Block.End - 1, End: statement.Block.End}, pieceClose, 0})
		}
	}
	for _, comment := range tree.Comments {
		pieces = append(pieces, piece{comment, pieceComment, 0})
	}
	pieces = slices.DeleteFunc(pieces, func(current piece) bool { return current.span.IsEmpty() })
	slices.SortStableFunc(pieces, func(first, second piece) int { return first.span.Start - second.span.Start })
	depth := 0
	for position := range pieces {
		if pieces[position].kind == pieceClose {
			depth--
		}
		pieces[position].depth = max(depth, 0)
		if pieces[position].kind == pieceOpen {
			depth++
		}
	}
	return pieces
}

// encodeTree returns the text of a tree's pieces written again. Every line break stays where it is. Each line is
// indented one unit for each block it is inside, and a piece on a line follows the one before it by a single space,
// or by the spacing most of the file's operators have where an operator is next to it. Text between two pieces that
// is not space stays as written.
func encodeTree(tree *jomini.Tree, pieces []piece, unit string) string {
	gap := " "
	tight, operators := 0, 0
	for _, statement := range tree.Statements {
		if !statement.Op.IsEmpty() {
			operators++
			if statement.Key.End == statement.Op.Start {
				tight++
			}
		}
	}
	if tight > operators-tight {
		gap = ""
	}
	eol := jomini.LineEnding(tree.Src)
	var text strings.Builder
	var before piece
	end := 0
	for position, current := range pieces {
		between := tree.Src[end:current.span.Start]
		switch {
		case strings.TrimSpace(between) != "":
			text.WriteString(between)
		case strings.Contains(between, "\n"):
			text.WriteString(strings.Repeat(eol, strings.Count(between, "\n")) + strings.Repeat(unit, current.depth))
		case position > 0:
			text.WriteString(separatePieces(before, current, gap))
		}
		source := tree.Text(current.span)
		if current.kind == pieceComment {
			source = strings.TrimRightFunc(source, unicode.IsSpace)
		}
		text.WriteString(source)
		before, end = current, current.span.End
	}
	if rest := tree.Src[end:]; strings.TrimSpace(rest) != "" {
		text.WriteString(rest)
	} else {
		text.WriteString(strings.Repeat(eol, strings.Count(rest, "\n")))
	}
	return text.String()
}

// separatePieces returns the space between two pieces on one line: the given operator spacing next to an operator,
// else one space.
func separatePieces(before, current piece, gap string) string {
	if current.kind != pieceComment && (before.kind == pieceOperator || current.kind == pieceOperator) {
		return gap
	}
	return " "
}

// ListFixes returns the quick fixes for the problems inside a range of a file, one per problem that has a fix.
func (lsp *Service) ListFixes(path string, within Range) ([]Fix, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods, place := ws.Mods.Load(), ws.Locate(path)
	diagnostics, err := DiagnoseFile(ws, mods, path)
	if err != nil {
		return nil, err
	}
	fixes := []Fix{}
	for _, diagnostic := range lsp.listEnabled(diagnostics) {
		if !isOverlapping(diagnostic.Range, within) {
			continue
		}
		offset := ToOffset(tree.Src, tree.Lines, diagnostic.Range.StartLine, diagnostic.Range.StartColumn)
		visitor := &hoverVisitor{offset: offset}
		ws.Schema.WalkTo(tree, place.File, offset, visitor)
		var title string
		var edits []Edit
		switch diagnostic.Code {
		case "unresolved-reference":
			title, edits = buildReferenceFix(mods, visitor, diagnostic)
		case "missing-key":
			title, edits = buildMissingFix(ws.Schema, tree, visitor, diagnostic)
		case "misplaced-mode":
			title, edits = buildModeFix(tree, offset, diagnostic)
		}
		if len(edits) > 0 {
			fixes = append(fixes, Fix{Title: title, Code: diagnostic.Code, Edits: map[string][]Edit{path: edits}})
		}
	}
	return fixes, nil
}

// isOverlapping reports whether two ranges share a place, or touch.
func isOverlapping(first, second Range) bool {
	return cmp.Or(cmp.Compare(first.StartLine, second.EndLine), cmp.Compare(first.StartColumn, second.EndColumn)) <= 0 &&
		cmp.Or(cmp.Compare(second.StartLine, first.EndLine), cmp.Compare(second.StartColumn, first.EndColumn)) <= 0
}

// buildReferenceFix returns the fix for a reference nothing defines: a change to the nearest name its database
// defines, when one is fewer than fixDistance edits away. The edits are empty when there is none.
func buildReferenceFix(mods *index.Index, visitor *hoverVisitor, diagnostic Diagnostic) (string, []Edit) {
	best, nearest := fixDistance, ""
	for _, reference := range visitor.context.References {
		if reference.Span.Start != visitor.offset {
			continue
		}
		for _, site := range mods.ListDefinitions(reference.Database) {
			key := mods.ToReference(site).Name
			// names that differ in length by the best distance so far or more are that far apart
			if max(utf8.RuneCountInString(key)-utf8.RuneCountInString(reference.Name),
				utf8.RuneCountInString(reference.Name)-utf8.RuneCountInString(key)) >= best {
				continue
			}
			if distance := countEdits(reference.Name, key); distance > 0 && distance < best {
				best, nearest = distance, key
			}
		}
	}
	if nearest == "" {
		return "", nil
	}
	return "Change to " + nearest, []Edit{{Range: diagnostic.Range, Text: nearest}}
}

// countEdits returns how many single-character edits turn one text into another.
func countEdits(first, second string) int {
	diff := diffmatchpatch.New()
	return diff.DiffLevenshtein(diff.DiffMain(first, second, false))
}

// buildMissingFix returns the fix for a definition that lacks a key it must write: a line that adds the key with its
// default, at the end of the definition's block. The edits are empty where the key's claim states no default.
func buildMissingFix(schema *schema.Schema, tree *jomini.Tree, visitor *hoverVisitor, diagnostic Diagnostic) (string, []Edit) {
	definition, key := visitor.context.Definition, diagnostic.Facts["key"]
	if definition == nil || !tree.Statements[definition.Statement].IsBlock() {
		return "", nil
	}
	database := schema.Databases[definition.Database]
	if database == nil || database.Definition.Children[key] == nil || !database.Definition.Children[key].Default.IsStated() {
		return "", nil
	}
	statement := key + " = " + database.Definition.Children[key].Default.Value
	insertion := tree.BuildInsertion(definition.Statement, []string{statement})
	return "Add " + statement, ToEdits(tree.Src, tree.Lines, []jomini.Edit{insertion})
}

// buildModeFix returns the fix for an entry mode that is out of place: the mode and its colon taken off the key. The
// given offset is where the key starts.
func buildModeFix(tree *jomini.Tree, offset int, diagnostic Diagnostic) (string, []Edit) {
	span := jomini.Span{Start: offset, End: offset + len(diagnostic.Facts["mode"]) + 1}
	return "Remove mode", []Edit{{Range: ToRange(tree.Src, tree.Lines, span), Text: ""}}
}
