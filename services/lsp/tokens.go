// tokens.go gives the semantic tokens of a file: the color category of every key and value, encoded as Monaco wants.

package lsp

import (
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Legend is the list of token types and modifiers that the semantic tokens are encoded against.
type Legend struct {
	Types     []string `json:"tokenTypes"`
	Modifiers []string `json:"tokenModifiers"`
}

// legend holds the color categories a token can have. A token's type is its category's place here.
var legend = []schema.Category{
	schema.CategoryKeyword, schema.CategoryCommand, schema.CategoryLink, schema.CategoryField, schema.CategoryDefinition,
	schema.CategoryReference, schema.CategoryParameter, schema.CategoryConstant, schema.CategoryCall, schema.CategoryChoice,
}

// Tokens returns the semantic tokens of the given range of a file: the color category of every key and value in it,
// encoded as Monaco wants. It returns none for a file that is not script.
func (lsp *Service) Tokens(path string, bounds Range) ([]uint32, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree := document.Tree
	span := ToSpan(tree.Src, tree.Lines, bounds)
	var tokens []token
	add := func(colored token) {
		if colored.span.End > span.Start && colored.span.Start < span.End {
			tokens = append(tokens, colored)
		}
	}

	for position := range document.Contexts {
		current, context := tree.Statements[position], &document.Contexts[position]
		add(makeToken(ws.Schema, tree, current.Key, context.Category))
		add(makeToken(ws.Schema, tree, current.Value, context.ValueGroup))
	}

	return encodeTokens(tree.Src, tree.Lines, tokens), nil
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

// makeToken returns the token of a span with the given category. A span the walk gave none is a link if its text is a
// chain of links, as scope:actor or root.liege.
func makeToken(language *schema.Schema, tree *jomini.Tree, span jomini.Span, category schema.Category) token {
	if category == schema.CategoryNone && !span.IsEmpty() && language.IsLink(tree.Unquoted(span)) {
		category = schema.CategoryLink
	}
	return token{span: span, category: category}
}

// encodeTokens returns the given tokens as five integers each: the line and column deltas, the length, the type's place
// in the legend and the modifiers. It leaves out a token Monaco cannot draw.
func encodeTokens(text string, lines jomini.Lines, tokens []token) []uint32 {
	tokens = lo.Filter(tokens, func(current token, _ int) bool {
		return !current.span.IsEmpty() && slices.Contains(legend, current.category)
	})
	slices.SortFunc(tokens, func(first, second token) int { return first.span.Start - second.span.Start })
	encoded := make([]uint32, 0, len(tokens)*5)
	previousLine, previousColumn := 0, 0
	for _, current := range tokens {
		span := ToRange(text, lines, current.span)
		line, column := span.StartLine-1, span.StartColumn-1
		// a span on more than one line is left out, which Monaco cannot draw, and so is one that overlaps the last
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
