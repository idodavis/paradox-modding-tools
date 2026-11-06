// tokens.go gives the semantic tokens of a file: the color category of every key and value, encoded as Monaco wants.

package lsp

import (
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Legend is the list of token types and modifiers that the semantic tokens are encoded against.
type Legend struct {
	Types     []string `json:"tokenTypes"`
	Modifiers []string `json:"tokenModifiers"`
}

// legend holds the color categories a token can have. A token's type is its category's place here.
var legend = []schema.Category{
	schema.CategoryKeyword, schema.CategoryCommand, schema.CategoryLink, schema.CategoryField, schema.CategoryDefinition,
	schema.CategoryReference, schema.CategoryParameter, schema.CategoryConstant, schema.CategoryCall,
}

// Tokens returns the semantic tokens of the given range of a file: the color category of every key and value in it,
// encoded as Monaco wants. It returns none for a file that is not script.
func (lsp *Service) Tokens(path string, bounds Range) ([]uint32, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	span := ToSpan(tree.Src, tree.Lines, bounds)
	rangeTokens := lo.Filter(lsp.listTokens(ws, tree, path), func(current token, _ int) bool {
		return current.span.End > span.Start && current.span.Start < span.End
	})
	return encodeTokens(tree.Src, tree.Lines, rangeTokens), nil
}

// listTokens returns the colored spans of a parsed file. The last file's are kept, so a request for more of the same
// text, as each scroll makes, does not walk it again; a new text or a new mods' index walks it anew.
func (lsp *Service) listTokens(ws *workspace.Session, tree *jomini.Tree, path string) []token {
	mods := ws.Mods.Load()
	if last := lsp.coloring.Load(); last != nil && last.tree == tree && last.mods == mods {
		return last.tokens
	}
	visitor, place := &tokensVisitor{schema: ws.Schema}, ws.Locate(mods, path)
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	lsp.coloring.Store(&fileColors{tree: tree, mods: mods, tokens: visitor.tokens})
	return visitor.tokens
}

// ListLegend returns the legend the tokens are encoded against.
func (lsp *Service) ListLegend() Legend {
	types := lo.Map(legend, func(category schema.Category, _ int) string { return string(category) })
	return Legend{Types: types, Modifiers: []string{}}
}

// A tokensVisitor collects the colored spans of a file.
type tokensVisitor struct {
	schema *schema.Schema
	tokens []token
}

// A token is one colored span before encoding.
type token struct {
	span     jomini.Span
	category schema.Category
}

// A fileColors is the colored spans of one parsed file, under the mods' index that colors them.
type fileColors struct {
	tree   *jomini.Tree
	mods   *index.Index
	tokens []token
}

// Enter records the statement's key and value categories.
func (visitor *tokensVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	current := tree.Statements[statement]
	visitor.tokens = append(visitor.tokens,
		visitor.makeToken(tree, current.Key, context.Category), visitor.makeToken(tree, current.Value, context.ValueGroup))
	return true
}

// makeToken returns the token of a span with the given category. A span the walk gave none is a link if its text is a
// chain of links, as scope:actor or root.liege.
func (visitor *tokensVisitor) makeToken(tree *jomini.Tree, span jomini.Span, category schema.Category) token {
	if category == schema.CategoryNone && !span.IsEmpty() && visitor.schema.IsLink(tree.Unquoted(span)) {
		category = schema.CategoryLink
	}
	return token{span: span, category: category}
}

// Leave does nothing.
func (visitor *tokensVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

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
