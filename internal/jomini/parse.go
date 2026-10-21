package jomini

// Parse reads a text into its tree. It never fails: a malformed text gives the statements it could read, and the
// problems go into Tree.Errors with their spans. A tagged block such as `rgb { … }` or `list "x"` is one value
// with a tag, `?=` is an operator like `=`, and a `#` comment runs to the end of its line.
func Parse(text string) *Tree { panic("stub") }

// A token is one lexical unit of the text: a scalar, an operator, a brace or a comment.
type token struct {
	class tokenClass
	span  Span
}

// tokenClass says which kind of token a token is.
type tokenClass uint8

const (
	tokenScalar tokenClass = iota
	tokenOperator
	tokenOpen
	tokenClose
	tokenComment
)

// scan splits a text into its tokens. A quoted string is one token, quotes included.
func scan(text string) []token { panic("stub") }
