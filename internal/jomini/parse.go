package jomini

import "strings"

// A token is one lexical unit of the text: a scalar, an operator, a brace or a comment.
type token struct {
	class tokenClass
	span  Span
}

// A tokenClass is the class of a token: scalar, operator, brace or comment.
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
// A tagged block such as `rgb { … }` or `list "x"` is one value with a tag, `?=` is an operator like `=`, and a `#`
// comment runs to the end of its line.
func Parse(text string) *Tree {
	tree := &Tree{Src: text, Lines: NewLines(text), Statements: []Statement{{Span: Span{End: len(text)}}}}
	tokens := scan(text)
	kept := tokens[:0]
	for _, tok := range tokens {
		if tok.class == tokenComment {
			tree.Comments = append(tree.Comments, tok.span)
			continue
		}
		if tok.class == tokenScalar && isUnclosedString(tree.Text(tok.span)) {
			tree.Errors = append(tree.Errors, Error{Code: "unclosed-string", Message: "string is not closed", Span: tok.span})
		}
		kept = append(kept, tok)
	}
	parseStatements(tree, kept, 0, 0)
	tree.Statements[0].Last = int32(len(tree.Statements))
	return tree
}

// scan splits a text into its tokens. A quoted string is one token, quotes included.
func scan(text string) []token {
	var tokens []token
	// A byte order mark at the start of the text is not a scalar.
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

// countQuoted returns how many bytes the quoted scalar starting at the given quote runs, its closing quote
// included, line endings between the quotes kept. Without a closing quote it runs to the text's end.
func countQuoted(text string, offset int) int {
	if end := strings.IndexByte(text[offset+1:], '"'); end >= 0 {
		return end + 2
	}
	return len(text) - offset
}

// countScalar returns how many bytes the scalar starting at the given offset runs. A `[ … ]` inside it keeps
// everything to its `]`.
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

// isUnclosedString reports whether the scalar opens with a quote and does not close with one.
func isUnclosedString(scalar string) bool {
	return scalar[0] == '"' && (len(scalar) < 2 || scalar[len(scalar)-1] != '"')
}

// parseStatements parses statements into the tree as children of the given parent until the tokens end or a
// closing brace closes the parent. It returns the index of the token it stopped at. At the file's level, a closing
// brace closes nothing: it is reported and skipped.
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

// parseStatement parses one statement starting at the given token: `key op value`, a tagged block, a bare value or
// a bare block. It appends the statement and its descendants to the tree and returns the index of the token after
// it. A key and an operator with no value are reported at the operator.
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

// parseValue parses the value of a statement at the given token. A value is a block, a scalar, a scalar that tags a
// block, the tag `list` with its quoted name, or an operator such as `<=`. It fills the statement's Tag, Value and
// Block and returns the index of the token after the value. The statement gets no value before a closing brace, at the
// end of the tokens or before a scalar that an operator follows. That scalar starts the next statement.
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

// parseBlock parses the block at the given opening brace, its statements as children of the given statement, and
// returns the index of the token after the closing brace. A block never closed runs to the text's end and is
// reported at its opening brace.
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
