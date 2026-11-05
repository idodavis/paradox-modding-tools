// parse.go splits a text into tokens and parses them into a Tree. A malformed text never fails: the problems in it are
// kept in the tree.

package jomini

import "strings"

// A token is one lexical unit of the text: a scalar, an operator, a brace or a comment.
type token struct {
	class tokenClass
	span  Span
}

// A tokenClass is the class of a token.
type tokenClass uint8

const (
	tokenScalar tokenClass = iota
	tokenOperator
	tokenOpen
	tokenClose
	tokenComment
)

// Parse parses a text into its tree. Statement 0 is the file itself, and the top-level statements are its children.
// It never fails: a malformed text gives the statements it could parse, and the problems go into the tree's Errors.
func Parse(text string) *Tree {
	tree := NewTree(text)
	tokens := scan(text)
	kept := tokens[:0]
	for _, tok := range tokens {
		if tok.class == tokenComment {
			tree.Comments = append(tree.Comments, tok.span)
			continue
		}
		scalar := tree.Text(tok.span)
		if tok.class == tokenScalar && scalar[0] == '"' && (len(scalar) < 2 || scalar[len(scalar)-1] != '"') {
			tree.Errors = append(tree.Errors, Error{Code: "unclosed-string", Message: "string is not closed", Span: tok.span})
		}
		kept = append(kept, tok)
	}
	parseStatements(tree, kept, 0, 0)
	tree.Statements[0].Last = int32(len(tree.Statements))
	return tree
}

// NewTree returns the tree of a text before any script is read from it: the text, its lines and the file's own
// statement, which holds nothing.
func NewTree(text string) *Tree {
	return &Tree{Src: text, Lines: NewLines(text), Statements: []Statement{{Span: Span{End: len(text)}, Last: 1}}}
}

// scan splits a text into its tokens.
func scan(text string) []token {
	var tokens []token
	// The scan starts after a byte order mark, which is not a scalar.
	for i := len(text) - len(strings.TrimPrefix(text, "\xEF\xBB\xBF")); i < len(text); {
		ch := text[i]
		if ch <= ' ' {
			i++
			continue
		}
		class, length := tokenOperator, countOperator(text, i)
		switch {
		case ch == '{':
			class, length = tokenOpen, 1
		case ch == '}':
			class, length = tokenClose, 1
		case ch == '#':
			class, length = tokenComment, countLine(text, i)
		case ch == '"':
			class, length = tokenScalar, countQuoted(text, i)
		case length == 0:
			class, length = tokenScalar, countScalar(text, i)
		}
		tokens = append(tokens, token{class: class, span: Span{Start: i, End: i + length}})
		i += length
	}
	return tokens
}

// countOperator returns how many bytes the operator starting at the given offset has, or 0 if none starts there.
func countOperator(text string, offset int) int {
	ch, next := text[offset], byte(0)
	if offset+1 < len(text) {
		next = text[offset+1]
	}
	switch {
	case next == '=' && strings.IndexByte("=<>!?", ch) >= 0:
		return 2
	case ch == '=' || ch == '<' || ch == '>':
		return 1
	}
	return 0
}

// countLine returns how many bytes run from the given offset to the end of its line, the line ending excluded.
func countLine(text string, offset int) int {
	if end := strings.IndexAny(text[offset:], "\r\n"); end >= 0 {
		return end
	}
	return len(text) - offset
}

// countQuoted returns how many bytes the quoted scalar starting at the given quote has, its closing quote included.
// Without a closing quote it runs to the end of the text.
func countQuoted(text string, offset int) int {
	for i := offset + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i + 1 - offset
		}
	}
	return len(text) - offset
}

// countScalar returns how many bytes the scalar starting at the given offset has. A `[ … ]` inside it is part of the
// scalar, spaces and all.
func countScalar(text string, offset int) int {
	end := offset
	for end < len(text) && !isScalarEnd(text, end) {
		if text[end] == '[' {
			end += CountBrackets(text, end)
			continue
		}
		end++
	}
	return end - offset
}

// isScalarEnd reports whether a scalar ends before the given offset: at a space, a brace, a `#` or an operator.
// A lone `<` or `>` is an operator only at the start of a token, so inside a scalar it ends nothing.
func isScalarEnd(text string, offset int) bool {
	ch := text[offset]
	return ch <= ' ' || ch == '{' || ch == '}' || ch == '#' || ch == '=' || countOperator(text, offset) == 2
}

// CountBrackets returns how many bytes the bracketed text starting at the given `[` runs, its matching `]`
// included. Without one on its line it runs to the line's end.
func CountBrackets(text string, offset int) int {
	depth := 0
	for i := offset; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i + 1 - offset
			}
		case '\n', '\r':
			return i - offset
		}
	}
	return len(text) - offset
}

// parseStatements parses the children of the given parent from the given token on, until a closing brace or the end of
// the tokens. It returns the index of the token it stopped at. A closing brace in the file itself is reported and
// skipped.
func parseStatements(tree *Tree, tokens []token, cursor int, parent int32) int {
	for cursor < len(tokens) {
		switch tokens[cursor].class {
		case tokenClose:
			if parent > 0 {
				return cursor
			}
			tree.Errors = append(tree.Errors, Error{Code: "unexpected-brace", Message: "} has no { to close", Span: tokens[cursor].span})
			cursor++
		case tokenOperator:
			// An operator with no key before it is dropped.
			cursor++
		default:
			cursor = parseStatement(tree, tokens, cursor, parent)
		}
	}
	return cursor
}

// parseStatement parses one statement from the given token and returns the index of the token after it. A key and
// operator with no value are reported at the operator.
func parseStatement(tree *Tree, tokens []token, cursor int, parent int32) int {
	index := int32(len(tree.Statements))
	tree.Statements = append(tree.Statements, Statement{Parent: parent, Span: Span{Start: tokens[cursor].span.Start}})
	keyed := tokens[cursor].class == tokenScalar && cursor+1 < len(tokens) && tokens[cursor+1].class == tokenOperator
	if keyed {
		tree.Statements[index].Key = tokens[cursor].span
		tree.Statements[index].Op = tokens[cursor+1].span
		cursor += 2
	}
	cursor = parseValue(tree, tokens, cursor, index)
	statement := &tree.Statements[index]
	if keyed && statement.ResolveValueSpan().IsEmpty() {
		tree.Errors = append(tree.Errors, Error{Code: "missing-value", Message: "a key with an operator has no value", Span: statement.Op})
	}
	statement.Last = int32(len(tree.Statements))
	statement.Span.End = tokens[cursor-1].span.End
	if statement.IsBlock() {
		// A block never closed runs past its last token to the text's end.
		statement.Span.End = statement.Block.End
	}
	return cursor
}

// parseValue parses the value of the given statement from the given token and returns the index of the token after it.
// It sets nothing and returns the same token when a closing brace or the end of the tokens comes first, or when the
// scalar is followed by an operator, which makes it the key of the next statement.
func parseValue(tree *Tree, tokens []token, cursor int, statement int32) int {
	if cursor >= len(tokens) {
		return cursor
	}
	switch tokens[cursor].class {
	case tokenClose:
		return cursor
	case tokenOperator:
		tree.Statements[statement].Value = tokens[cursor].span
		return cursor + 1
	case tokenOpen:
		return parseBlock(tree, tokens, cursor, statement)
	}
	scalar := tokens[cursor].span
	if cursor+1 < len(tokens) {
		switch next := tokens[cursor+1]; {
		case next.class == tokenOperator:
			return cursor
		case next.class == tokenOpen:
			tree.Statements[statement].Tag = scalar
			return parseBlock(tree, tokens, cursor+1, statement)
		case next.class == tokenScalar && tree.Text(scalar) == "list" && tree.Src[next.span.Start] == '"':
			tree.Statements[statement].Tag = scalar
			tree.Statements[statement].Value = next.span
			return cursor + 2
		}
	}
	tree.Statements[statement].Value = scalar
	return cursor + 1
}

// parseBlock parses the block that opens at the given token as the block of the given statement and returns the index
// of the token after its closing brace. A block never closed is reported at its opening brace.
func parseBlock(tree *Tree, tokens []token, cursor int, statement int32) int {
	open := tokens[cursor].span
	cursor = parseStatements(tree, tokens, cursor+1, statement)
	if cursor < len(tokens) {
		tree.Statements[statement].Block = Span{Start: open.Start, End: tokens[cursor].span.End}
		return cursor + 1
	}
	tree.Statements[statement].Block = Span{Start: open.Start, End: len(tree.Src)}
	tree.Errors = append(tree.Errors, Error{Code: "unclosed-block", Message: "{ is not closed", Span: open})
	return cursor
}
