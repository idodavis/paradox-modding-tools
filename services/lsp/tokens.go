package lsp

import (
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Legend is Monaco's semantic tokens legend: the token types, one per color category, in index order.
type Legend struct {
	Types     []string `json:"tokenTypes"`
	Modifiers []string `json:"tokenModifiers"`
}

// Tokens returns the semantic tokens of a file: the color category of every key and value the walk decided,
// encoded as Monaco wants, five integers per token (the line and column deltas, the length, the type's index in
// the legend and the modifiers). The grammar in the editor colors shapes; these color meaning on top.
func (lsp *Service) Tokens(path string) ([]uint32, error) { panic("stub") }

// ListLegend returns the legend the tokens are encoded against.
func (lsp *Service) ListLegend() Legend { panic("stub") }

// A tokensVisitor is the visitor behind Tokens. It records the category of each key and value with its span.
type tokensVisitor struct {
	tokens []token
}

// A token is one colored span before encoding.
type token struct {
	span     jomini.Span
	category schema.Category
}

// Enter records the statement's key and value categories.
func (visitor *tokensVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	panic("stub")
}

// Leave does nothing: a token is complete when its statement is entered.
func (visitor *tokensVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// encodeTokens turns the recorded spans and categories into Monaco's delta-encoded stream.
func encodeTokens(text string, lines jomini.Lines, tokens []token) []uint32 { panic("stub") }
