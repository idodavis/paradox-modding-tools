// tokens.go gives the colors of a text: the color category of every key,
// value and piece of markup, encoded as Monaco wants.

package lsp

import (
	"cmp"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Legend is the list of token types and modifiers that the semantic tokens
// are encoded against.
type Legend struct {
	Types     []string `json:"tokenTypes"`
	Modifiers []string `json:"tokenModifiers"`
}

// The token types that the walk's categories do not cover.
const (
	// tokenNumber is a number, a date, and a yes or no.
	tokenNumber schema.Category = "number"

	// tokenFormat and tokenIcon are the #format and the @icon! of a
	// localization text.
	tokenFormat schema.Category = "format"
	tokenIcon   schema.Category = "icon"

	// tokenComment, tokenString and tokenOperator are what Monaco colors by
	// itself in the editor, so only a text shown outside it gets them.
	tokenComment  schema.Category = "comment"
	tokenString   schema.Category = "string"
	tokenOperator schema.Category = "operator"
)

// legend holds the types a token can have. A token's type is its place here.
var legend = []schema.Category{
	schema.CategoryKeyword, schema.CategoryCommand, schema.CategoryLink, schema.CategoryField, schema.CategoryDefinition,
	schema.CategoryReference, schema.CategoryParameter, schema.CategoryConstant, schema.CategoryCall, schema.CategoryChoice,
	tokenNumber, tokenFormat, tokenIcon, tokenComment, tokenString, tokenOperator,
}

// markupTokens holds the token type of each piece of a localization text's
// markup. Plain text and a line break have none.
var markupTokens = map[loc.Markup]schema.Category{
	loc.MarkupKey: schema.CategoryParameter, loc.MarkupCall: schema.CategoryCall,
	loc.MarkupFormat: tokenFormat, loc.MarkupIcon: tokenIcon,
}

// Tokens returns the semantic tokens of the given range of a file, encoded as
// Monaco wants: the color of every key and value of a script file, and of
// every key and piece of markup of a localization file. It returns none for
// any other file.
func (lsp *Service) Tokens(path string, bounds jomini.Range) ([]uint32, error) {
	_, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree := document.Tree

	return encodeTokens(tree.Src, tree.Lines, listTokens(document, jomini.ToSpan(tree.Src, tree.Lines, bounds), false)), nil
}

// ColorText returns the tokens of a text that is not open in the editor,
// such as a snippet on a page, encoded as Tokens encodes them. Comments,
// quoted texts and operators are tokens too, since no editor colors them.
//
// The line is where the text starts in its file, from 1. The text then takes
// the colors those lines have in the file. With line 0, or if the file does
// not hold the text there, the text is read by itself: as a file at the
// path, or as a file of the given format if the path is "".
func (lsp *Service) ColorText(path string, format files.Format, text string, line int) ([]uint32, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}

	if colors, ok := colorLines(ws, path, text, line); ok {
		return colors, nil
	}

	place := workspace.Place{File: files.File{Format: format}}
	if path != "" {
		place = ws.Locate(ws.Mods.Load(), path)
	}

	document := ws.WalkText(place, text)

	return encodeTokens(text, document.Tree.Lines, listTokens(document, jomini.Span{End: len(text)}, true)), nil
}

// colorLines returns the tokens of a text that starts at a line of a file,
// from 1, with the colors those lines have in the file. A snippet read by
// itself loses what the file says above it, such as an event's namespace.
// It returns false if there is no path or line, the file cannot be read, or
// the file does not hold the text at that line.
func colorLines(ws *workspace.Session, path, text string, line int) ([]uint32, bool) {
	if path == "" || line < 1 {
		return nil, false
	}

	document, err := ws.WalkDocument(path)
	if err != nil {
		return nil, false
	}

	tree, last := document.Tree, line-1+strings.Count(text, "\n")
	if last >= tree.Lines.CountLines() {
		return nil, false
	}

	bounds := jomini.Span{Start: tree.Lines.LineSpan(line - 1).Start, End: tree.Lines.LineSpan(last).End}
	if strings.ReplaceAll(tree.Text(bounds), "\r\n", "\n") != text {
		return nil, false
	}

	colors := encodeTokens(tree.Src, tree.Lines, listTokens(document, bounds, true))
	if len(colors) > 0 {
		colors[0] -= uint32(line - 1)
	}

	return colors, true
}

// ListLegend returns the legend the tokens are encoded against.
func (lsp *Service) ListLegend() Legend {
	types := lo.Map(legend, func(category schema.Category, _ int) string { return string(category) })
	return Legend{Types: types, Modifiers: []string{}}
}

// A token is one colored span before encoding.
type token struct {
	span     jomini.Span
	category schema.Category
}

// listTokens returns the tokens of a document that overlap the given span. A
// key or a value has the category the walk gave it. Where the walk gave
// none, a number, a date and a yes or no are numbers, and any other key is a
// field. A localization file's keys are definitions, and its texts' markup
// has the types of markupTokens. With the flag set, comments, operators,
// quoted texts and a localization text's plain pieces are tokens too.
func listTokens(document schema.Document, bounds jomini.Span, whole bool) []token {
	tree := document.Tree
	var tokens []token
	add := func(span jomini.Span, category schema.Category) {
		if whole && category == "" && tree.UnquotedSpan(span) != span {
			category = tokenString
		}

		if category != "" && span.End > bounds.Start && span.Start < bounds.End {
			tokens = append(tokens, token{span: span, category: category})
		}
	}

	for position := range document.Contexts {
		current, context := tree.Statements[position], &document.Contexts[position]
		add(current.Key, cmp.Or(classifyKey(context), classifyToken(tree, current.Key, current.KeyClass), schema.CategoryField))
		add(current.Value, cmp.Or(context.ValueGroup, classifyToken(tree, current.Value, current.ValueClass)))
		if whole {
			add(current.Op, tokenOperator)
		}
	}

	if document.Messages != nil {
		for _, message := range document.Messages.Messages {
			add(message.KeySpan, schema.CategoryDefinition)
			for _, segment := range message.Segments {
				add(segment.Span, cmp.Or(markupTokens[segment.Markup], lo.Ternary(whole, tokenString, "")))
			}
		}
	}

	if whole {
		for _, comment := range tree.Comments {
			add(comment, tokenComment)
		}
	}

	return tokens
}

// classifyKey returns the token type the walk's answer gives a key. A list
// iterator such as every_vassal is a keyword, since it is a loop of the
// language, though the walk holds it as the command it also is. Any other
// key has the walk's own category, which is "" where the walk gave none.
func classifyKey(context *schema.Context) schema.Category {
	if context.Iterates {
		return schema.CategoryKeyword
	}

	return context.Category
}

// classifyToken returns the token type that a key or a value has by the way
// it is written: a number for a number, a date, and a yes or no. It returns
// "" for anything else.
func classifyToken(tree *jomini.Tree, span jomini.Span, class jomini.Class) schema.Category {
	if class == jomini.ClassNumber || class == jomini.ClassDate || slices.Contains(game.Bools, tree.Text(span)) {
		return tokenNumber
	}

	return ""
}

// encodeTokens returns the given tokens as five integers each: the line and
// column deltas, the length, the type's place in the legend and the
// modifiers. It leaves out a token Monaco cannot draw.
func encodeTokens(text string, lines jomini.Lines, tokens []token) []uint32 {
	tokens = lo.Filter(tokens, func(current token, _ int) bool {
		return !current.span.IsEmpty() && slices.Contains(legend, current.category)
	})
	slices.SortFunc(tokens, func(first, second token) int { return first.span.Start - second.span.Start })
	encoded := make([]uint32, 0, len(tokens)*5)
	previousLine, previousColumn := 0, 0
	for _, current := range tokens {
		span := jomini.ToRange(text, lines, current.span)
		line, column := span.StartLine-1, span.StartColumn-1
		// a span on more than one line is left out, which Monaco cannot draw,
		// and so is one that overlaps the last
		if span.StartLine != span.EndLine || line < previousLine || line == previousLine && column < previousColumn {
			continue
		}
		deltaColumn := column
		if line == previousLine {
			deltaColumn = column - previousColumn
		}
		encoded = append(encoded, uint32(line-previousLine), uint32(deltaColumn), uint32(span.EndColumn-span.StartColumn),
			uint32(slices.Index(legend, current.category)), 0)
		previousLine, previousColumn = line, column
	}
	return encoded
}
