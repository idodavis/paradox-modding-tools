// parse.go parses a text into a Tree.

package jomini

import "strings"

// token is a scalar, an operator, a brace or a comment.
type token struct {
	class tokenClass
	span  Span
}

// tokenClass is the kind of a token. Zero is no token.
type tokenClass uint8

const (
	tokenClassScalar tokenClass = iota + 1
	tokenClassOperator
	tokenClassOpen
	tokenClassClose
	tokenClassComment
)

// parser reads tokens into a tree.
type parser struct {
	tree   *Tree
	tokens []token
	cursor int
	depth  int
}

// Parse parses a text into a tree. A text that breaks the syntax gives the
// statements that could be read, with its problems in the tree's Problems.
func Parse(text string) *Tree {
	tree := NewTree(text)

	(&parser{tree: tree, tokens: scan(tree)}).parseStatements(0)
	tree.Statements[0].Last = int32(len(tree.Statements))

	return tree
}

// NewTree returns the tree of a text before any statement is read.
func NewTree(text string) *Tree {
	return &Tree{Src: text, Lines: NewLines(text), Statements: []Statement{{Span: Span{End: len(text)}, Last: 1}}}
}

// scan splits the text into tokens. Comments go to the tree. It reports each
// unclosed string.
func scan(tree *Tree) []token {
	text := tree.Src
	var tokens []token

	// A byte order mark is no scalar.
	for i := len(text) - len(strings.TrimPrefix(text, "\xEF\xBB\xBF")); i < len(text); {
		ch := text[i]
		if isSeparator(ch) {
			i++

			continue
		}

		class, length := tokenClassOperator, countOperator(text, i)
		switch {
		case ch == '{':
			class, length = tokenClassOpen, 1
		case ch == '}':
			class, length = tokenClassClose, 1
		case ch == '#':
			class, length = tokenClassComment, countLine(text, i)
		case ch == '"':
			class, length = tokenClassScalar, countQuoted(text, i)
		case length == 0:
			class, length = tokenClassScalar, countScalar(text, i)
		}

		span := Span{Start: i, End: i + length}
		i = span.End

		if class == tokenClassComment {
			tree.Comments = append(tree.Comments, span)

			continue
		}

		if ch == '"' && (length < 2 || text[span.End-1] != '"') {
			tree.report("unclosed-string", "string is not closed; it runs to the end of the text", span)
		}

		tokens = append(tokens, token{class: class, span: span})
	}

	return tokens
}

func isSeparator(ch byte) bool {
	return ch <= ' ' || ch == ',' || ch == ';'
}

// countOperator returns the length of the operator at the given offset, or 0.
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

// countLine returns the length to the line's end, without the line ending.
func countLine(text string, offset int) int {
	if end := strings.IndexAny(text[offset:], "\r\n"); end >= 0 {
		return end
	}

	return len(text) - offset
}

// countQuoted returns the length of the quoted scalar. Without a closing quote
// it runs to the text's end.
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

// countScalar returns the length of the scalar. A [ … ] inside it is part of
// it.
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

// isScalarEnd reports whether a scalar ends at the given offset: at a
// separator, a brace, a # or an operator. A lone < or > ends nothing inside a
// scalar.
func isScalarEnd(text string, offset int) bool {
	ch := text[offset]

	return isSeparator(ch) || ch == '{' || ch == '}' || ch == '#' || ch == '=' || countOperator(text, offset) == 2
}

// CountBrackets returns the length of the bracketed text that opens at the
// given [, to its matching ] or the line's end. Quotes are not read: the games'
// translations write unmatched ones, as ('recipient’) does.
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

func (t *Tree) report(code, message string, span Span) {
	t.Problems = append(t.Problems, Problem{Code: code, Message: message, Span: span})
}

// parseStatements parses the children of a statement. A closing brace at the
// top is reported and skipped.
func (p *parser) parseStatements(parent int32) {
	for p.cursor < len(p.tokens) {
		next := p.tokens[p.cursor]

		switch {
		case next.class == tokenClassClose && parent > 0:
			return

		case next.class == tokenClassClose:
			p.tree.report("unexpected-brace", "} has no { to close; it is skipped", next.span)
			p.cursor++

		// An operator with no key before it is dropped.
		case next.class == tokenClassOperator:
			p.cursor++

		default:
			p.parseStatement(parent)
		}
	}
}

// parseStatement parses one statement. A key and operator with no value are
// reported at the operator.
func (p *parser) parseStatement(parent int32) {
	statement := int32(len(p.tree.Statements))
	first, second := p.peek(0), p.peek(1)
	p.tree.Statements = append(p.tree.Statements, Statement{Parent: parent, Span: Span{Start: first.span.Start}})

	keyed := first.class == tokenClassScalar && second.class == tokenClassOperator
	if keyed {
		p.tree.Statements[statement].Key, p.tree.Statements[statement].Op = first.span, second.span
		p.cursor += 2
	}

	p.parseValue(statement)

	current := &p.tree.Statements[statement]
	if keyed && current.ResolveValueSpan().IsEmpty() {
		p.tree.report("missing-value", "a key with an operator has no value; the statement is kept without one", current.Op)
	}

	current.KeyClass = Classify(p.tree.Unquoted(current.Key))
	current.TagClass = Classify(p.tree.Unquoted(current.Tag))
	current.ValueClass = Classify(p.tree.Unquoted(current.Value))

	current.Last = int32(len(p.tree.Statements))
	current.Span.End = p.tokens[p.cursor-1].span.End

	// A block never closed ends at the text's end.
	if current.IsBlock() {
		current.Span.End = current.Block.End
	}
}

// peek returns the token ahead of the cursor, or the zero token.
func (p *parser) peek(ahead int) token {
	if p.cursor+ahead >= len(p.tokens) {
		return token{}
	}

	return p.tokens[p.cursor+ahead]
}

// parseValue parses the value of a statement from the cursor.
func (p *parser) parseValue(statement int32) {
	first, second := p.peek(0), p.peek(1)
	current := &p.tree.Statements[statement]

	switch {
	// The end or a closing brace leaves the value empty.
	case first.class == 0, first.class == tokenClassClose:

	case first.class == tokenClassOpen:
		p.parseBlock(statement)

	// An operator written where a value goes is the value.
	case first.class == tokenClassOperator:
		current.Value = first.span
		p.cursor++

	// A scalar before an operator is the next key.
	case second.class == tokenClassOperator:

	case second.class == tokenClassOpen:
		current.Tag = first.span
		p.cursor++
		p.parseBlock(statement)

	case second.class == tokenClassScalar && p.tree.Text(first.span) == "list" && p.tree.Src[second.span.Start] == '"':
		current.Tag, current.Value = first.span, second.span
		p.cursor += 2

	default:
		current.Value = first.span
		p.cursor++
	}
}

// maxDepth is how deep the parser reads: it recurses per level, and a Go stack
// overflow is fatal.
const maxDepth = 1000

// parseBlock parses the block at the cursor. A block deeper than maxDepth is
// reported and skipped. One never closed is reported at its opening brace and
// runs to the text's end.
func (p *parser) parseBlock(statement int32) {
	open := p.tokens[p.cursor].span
	p.cursor++
	p.depth++

	if p.depth > maxDepth {
		p.tree.report("too-deep", "{ opens more than 1,000 levels deep; its block is skipped", open)
		p.skipBlock()
	} else {
		p.parseStatements(statement)
	}

	p.depth--

	if p.cursor < len(p.tokens) {
		p.tree.Statements[statement].Block = Span{Start: open.Start, End: p.tokens[p.cursor].span.End}
		p.cursor++

		return
	}

	p.tree.Statements[statement].Block = Span{Start: open.Start, End: len(p.tree.Src)}
	p.tree.report("unclosed-block", "{ is not closed; its block runs to the end of the text", open)
}

// skipBlock moves the cursor to the closing brace of the open block, or to the
// end.
func (p *parser) skipBlock() {
	for open := 1; p.cursor < len(p.tokens); p.cursor++ {
		switch p.tokens[p.cursor].class {
		case tokenClassOpen:
			open++
		case tokenClassClose:
			open--
			if open == 0 {
				return
			}
		}
	}
}
