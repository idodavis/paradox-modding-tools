// tokens.go gives the colors of a text, encoded as Monaco wants.

package lsp

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// Legend is what tokens are encoded against.
type Legend struct {
	Types     []string `json:"tokenTypes"`
	Modifiers []string `json:"tokenModifiers"`
}

// The token types that the walk's categories do not cover.
const (
	tokenNumber lang.Category = "number"

	tokenFormat lang.Category = "format"
	tokenIcon   lang.Category = "icon"

	// tokenComment, tokenString and tokenOperator are what Monaco colors itself
	// in the editor, so only a text shown outside it gets them.
	tokenComment  lang.Category = "comment"
	tokenString   lang.Category = "string"
	tokenOperator lang.Category = "operator"
)

// legend lists the token types, by place.
var legend = []lang.Category{
	lang.CategoryKeyword, lang.CategoryCommand, lang.CategoryLink, lang.CategoryField, lang.CategoryDefinition,
	lang.CategoryReference, lang.CategoryParameter, lang.CategoryConstant, lang.CategoryCall, lang.CategoryChoice,
	tokenNumber, tokenFormat, tokenIcon, tokenComment, tokenString, tokenOperator,
}

// markupTokens is the type of each markup. Plain text and a line break have
// none.
var markupTokens = map[loc.Markup]lang.Category{
	loc.MarkupKey: lang.CategoryParameter, loc.MarkupCall: lang.CategoryCall,
	loc.MarkupFormat: tokenFormat, loc.MarkupIcon: tokenIcon,
}

// Tokens returns the semantic tokens of a range of a file, encoded as Monaco
// wants. It returns none for a file that is not script or localization.
func (s *Service) Tokens(path string, bounds jomini.Range) ([]uint32, error) {
	_, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree := document.Tree

	return encodeTokens(tree.Src, tree.Lines, listTokens(document, jomini.ToSpan(tree.Src, tree.Lines, bounds), false)), nil
}

// ColorText returns the tokens of a text outside the editor, such as a snippet
// on a page, with comments, quotes and operators too. The line is where the
// text starts in its file, from 1, so it takes that file's colors. Otherwise
// it is read alone, as the file or, with no path, the given format.
func (s *Service) ColorText(path string, format files.Format, text string, line int) ([]uint32, error) {
	ws, err := s.Workspace.Session()
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
// colored as the file has it, so it keeps what the file says above it, such as
// an event's namespace. It returns false if the file does not hold the text.
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
func (s *Service) ListLegend() Legend {
	types := make([]string, len(legend))
	for position, category := range legend {
		types[position] = string(category)
	}

	return Legend{Types: types, Modifiers: []string{}}
}

type token struct {
	span     jomini.Span
	category lang.Category
}

// listTokens returns the tokens of a document that overlap the span. A key or
// value has the category the walk gave it, else a number, a date or a yes or no
// is a number and any other key a field. A localization key is a definition. A
// text outside the editor also gets comments, operators and quoted texts.
func listTokens(document lang.Document, bounds jomini.Span, whole bool) []token {
	tree := document.Tree
	var tokens []token
	add := func(span jomini.Span, category lang.Category) {
		if whole && category == "" && tree.UnquotedSpan(span) != span {
			category = tokenString
		}

		if category != "" && span.End > bounds.Start && span.Start < bounds.End {
			tokens = append(tokens, token{span: span, category: category})
		}
	}

	for position := range document.Contexts {
		current, context := tree.Statements[position], &document.Contexts[position]
		add(current.Key, cmp.Or(classifyKey(context), classifyToken(tree, current.Key, current.KeyClass), lang.CategoryField))
		add(current.Value, cmp.Or(context.ValueGroup, classifyToken(tree, current.Value, current.ValueClass)))
		if whole {
			add(current.Op, tokenOperator)
		}
	}

	if document.Messages != nil {
		var plain lang.Category
		if whole {
			plain = tokenString
		}

		for _, message := range document.Messages.Messages {
			add(message.KeySpan, lang.CategoryDefinition)
			for _, segment := range message.Segments {
				add(segment.Span, cmp.Or(markupTokens[segment.Markup], plain))
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

// classifyKey returns a key's token type: keyword for a list iterator such as
// every_vassal, which the walk also holds as a command, else the walk's
// category, "" if it gave none.
func classifyKey(context *lang.Context) lang.Category {
	if context.Iterates {
		return lang.CategoryKeyword
	}

	return context.Category
}

func classifyToken(tree *jomini.Tree, span jomini.Span, class jomini.Class) lang.Category {
	if class == jomini.ClassNumber || class == jomini.ClassDate || slices.Contains(lang.Bools, tree.Text(span)) {
		return tokenNumber
	}

	return ""
}

// encodeTokens returns the tokens as five integers each: line and column
// deltas, length, legend place and modifiers. A token Monaco cannot draw is
// left out.
func encodeTokens(text string, lines jomini.Lines, tokens []token) []uint32 {
	tokens = slices.DeleteFunc(tokens, func(current token) bool {
		return current.span.IsEmpty() || !slices.Contains(legend, current.category)
	})
	slices.SortFunc(tokens, func(first, second token) int { return first.span.Start - second.span.Start })

	encoded := make([]uint32, 0, len(tokens)*5)
	previousLine, previousColumn := 0, 0
	for _, current := range tokens {
		span := jomini.ToRange(text, lines, current.span)
		line, column := span.StartLine-1, span.StartColumn-1
		// Monaco draws no span over lines, or overlapping the last.
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
