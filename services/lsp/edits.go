package lsp

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
	"github.com/sergi/go-diff/diffmatchpatch"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Fix is one quick fix for a problem. It carries the edits that apply it, grouped by file.
type Fix struct {
	Title string            `json:"title"`
	Code  string            `json:"code"` // the diagnostic it fixes
	Edits map[string][]Edit `json:"edits"`
}

// fixDistance is how many edits away a name may be and still be offered as the fix for an unresolved reference.
const fixDistance = 3

// Format returns the edits that format a file's text. It keeps the file's own indentation, or else indents with the
// given tab size, in spaces if the flag is set. It returns no edits for a file that is not script, does not parse, or
// would change in more than its spacing.
func (lsp *Service) Format(path string, tabSize int, insertSpaces bool) ([]Edit, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	// a file that does not parse is left as it is
	if !ws.Locate(ws.Mods.Load(), path).Format.IsScript() || len(tree.Errors) > 0 {
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

// DescribeIndentation returns what a file indents one level with, in words: "Tabs" or "4 spaces". It returns "" if the
// file indents nothing.
func (lsp *Service) DescribeIndentation(path string) (string, error) {
	_, tree, err := lsp.loadTree(path)
	if err != nil {
		return "", err
	}
	switch unit := tree.Indentation(); {
	case unit == "":
		return "", nil
	case unit[0] == '\t':
		return "Tabs", nil
	default:
		return strconv.Itoa(len(unit)) + " spaces", nil
	}
}

// A piece is one run of a file's text that formatting keeps whole, such as a key, an operator, a brace or a comment.
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

// listPieces returns the pieces of a tree in the order they are written, each with its depth in blocks.
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
	// a closing brace is outside the block it closes
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

// encodeTree returns the text of a tree written again from its pieces. It keeps every line break and indents each line
// with the given unit for each block it is inside. Two pieces on a line are one space apart, and an operator is spaced
// as most of the file's operators are.
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

// separatePieces returns the space between two pieces on one line: the given spacing next to an operator, else one
// space.
func separatePieces(before, current piece, gap string) string {
	if current.kind != pieceComment && (before.kind == pieceOperator || current.kind == pieceOperator) {
		return gap
	}
	return " "
}

// ListFixes returns the quick fixes for the given problems of a file, one for each problem that has a fix.
func (lsp *Service) ListFixes(path string, diagnostics []Diagnostic) ([]Fix, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	walk := func(diagnostic Diagnostic) *hoverVisitor {
		offset := ToOffset(tree.Src, tree.Lines, diagnostic.Range.StartLine, diagnostic.Range.StartColumn)
		visitor := &hoverVisitor{offset: offset}
		ws.Schema.WalkTo(tree, place.File, offset, visitor)
		return visitor
	}
	fixes := []Fix{}
	for _, diagnostic := range diagnostics {
		var title string
		var edits []Edit
		switch diagnostic.Code {
		case "unresolved-reference":
			title, edits = buildReferenceFix(mods, walk(diagnostic), diagnostic)
		case "missing-key":
			title, edits = buildMissingFix(ws.Schema, tree, walk(diagnostic), diagnostic)
		case "misplaced-mode":
			title, edits = buildModeFix(diagnostic)
		}
		if len(edits) > 0 {
			fixes = append(fixes, Fix{Title: title, Code: diagnostic.Code, Edits: map[string][]Edit{path: edits}})
		}
	}
	return fixes, nil
}

// buildReferenceFix returns the fix for a reference nothing defines: a change to the nearest name its database defines.
// The edits are empty if no name is near enough.
func buildReferenceFix(mods *index.Index, visitor *hoverVisitor, diagnostic Diagnostic) (string, []Edit) {
	best, nearest := fixDistance, ""
	for _, reference := range visitor.context.References {
		if reference.Span.Start != visitor.offset {
			continue
		}
		name := mods.ToCaseless(reference.Database, reference.Name)
		length := utf8.RuneCountInString(name)
		keys := lo.Map(mods.ListDefinitions(reference.Database), func(site index.Site, _ int) string {
			return mods.ToReference(site).Name
		})
		for _, key := range lo.Uniq(keys) {
			// names that differ in length by the best distance so far or more are that far apart
			if max(utf8.RuneCountInString(key)-length, length-utf8.RuneCountInString(key)) >= best {
				continue
			}
			if distance := countEdits(name, key); distance > 0 && distance < best {
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
// default. The edits are empty if the key has no default.
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

// buildModeFix returns the fix for an entry mode that is out of place: it removes the mode and its colon.
func buildModeFix(diagnostic Diagnostic) (string, []Edit) {
	return "Remove mode", []Edit{{Range: diagnostic.Range, Text: ""}}
}
