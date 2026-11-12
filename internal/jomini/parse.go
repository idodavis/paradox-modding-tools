// parse.go splits a text into tokens and parses them into a Tree.

package jomini

import "strings"

// token is one lexical unit of the text: a scalar, an operator, a brace or a
// comment.
type token struct {
	class tokenClass
	span  Span
}

// tokenClass is the class of a token. The zero class is no token, which the
// parser sees past the last one.
type tokenClass uint8

const (
	tokenClassScalar tokenClass = iota + 1
	tokenClassOperator
	tokenClassOpen
	tokenClassClose
	tokenClassComment
)

// parser reads the tokens of a text into its tree, one statement at a time.
type parser struct {
	tree   *Tree
	tokens []token

	// cursor is the token the parser reads next.
	cursor int

	// depth is how many blocks are open at the cursor.
	depth int
}

// Parse parses a text into its tree. Statement 0 is the file itself, and the
// statements at the top of the text are its children. A text that breaks the
// syntax gives the statements Parse could read, and its problems go into the
// tree's Problems.
func Parse(text string) *Tree {
	tree := NewTree(text)

	(&parser{tree: tree, tokens: scan(tree)}).parseStatements(0)
	tree.Statements[0].Last = int32(len(tree.Statements))

	return tree
}

// NewTree returns the tree of a text before any script is read from it: the
// text, its lines and the file's own statement, which holds nothing.
func NewTree(text string) *Tree {
	return &Tree{Src: text, Lines: NewLines(text), Statements: []Statement{{Span: Span{End: len(text)}, Last: 1}}}
}

// scan splits the tree's text into tokens. It keeps the comments on the tree
// and returns the other tokens. It reports each string that is not closed.
func scan(tree *Tree) []token {
	text := tree.Src
	var tokens []token

	// The scan starts after a byte order mark, which is no scalar.
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

// isSeparator reports whether a byte only separates tokens: white space, a
// comma or a semicolon.
func isSeparator(ch byte) bool {
	return ch <= ' ' || ch == ',' || ch == ';'
}

// countOperator returns how many bytes the operator that starts at the given
// offset has, or 0 if none starts there.
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

// countLine returns how many bytes run from the given offset to the end of
// its line, the line ending left out.
func countLine(text string, offset int) int {
	if end := strings.IndexAny(text[offset:], "\r\n"); end >= 0 {
		return end
	}

	return len(text) - offset
}

// countQuoted returns how many bytes the quoted scalar that opens at the
// given quote has, its closing quote included. Without a closing quote it
// runs to the end of the text.
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

// countScalar returns how many bytes the scalar that starts at the given
// offset has. A [ … ] inside it is part of the scalar, spaces and all.
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

// isScalarEnd reports whether a scalar ends before the given offset: at a
// separator, a brace, a # or an operator. A lone < or > is an operator only
// at the start of a token, so inside a scalar it ends nothing.
func isScalarEnd(text string, offset int) bool {
	ch := text[offset]

	return isSeparator(ch) || ch == '{' || ch == '}' || ch == '#' || ch == '=' || countOperator(text, offset) == 2
}

// CountBrackets returns how many bytes the bracketed text that opens at the
// given [ runs, its matching ] included. Without one on its line it runs to
// the line's end.
//
// Quotes are not read. The games' translations write quotes that do not
// match, as ('recipient’) does.
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

// report records a problem the parse found.
func (t *Tree) report(code, message string, span Span) {
	t.Problems = append(t.Problems, Problem{Code: code, Message: message, Span: span})
}

// parseStatements parses the children of a statement from the cursor on,
// until a closing brace or the end of the tokens. A closing brace at the top
// closes nothing, and is reported and skipped.
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

// parseStatement parses one statement from the cursor, and gives its key, its
// tag and its value their classes. A key and an operator with no value after
// them are reported at the operator.
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

	// A block never closed runs past its last token to the text's end.
	if current.IsBlock() {
		current.Span.End = current.Block.End
	}
}

// peek returns the token that many places past the cursor, or the zero token
// past the last one.
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
	// The end of the tokens or a closing brace leaves the value empty.
	case first.class == 0, first.class == tokenClassClose:

	case first.class == tokenClassOpen:
		p.parseBlock(statement)

	// An operator written where a value goes is the value.
	case first.class == tokenClassOperator:
		current.Value = first.span
		p.cursor++

	// A scalar followed by an operator is the key of the next statement.
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

// maxDepth is how many blocks deep the parser reads. It recurses once per
// level, and a Go stack overflow is fatal rather than recoverable.
const maxDepth = 1000

// parseBlock parses the block that opens at the cursor as a statement's block,
// and moves the cursor past its closing brace. A block opened deeper than
// maxDepth is reported, and skipped to its closing brace. A block never
// closed is reported at its opening brace, and runs to the text's end.
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

// skipBlock moves the cursor to the closing brace of the block open before
// it, past every block inside it, or to the end of the tokens if the block is
// never closed.
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
