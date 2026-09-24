// Package jomini is the game-agnostic Paradox/Jomini script language: syntax
// tree and grammar shared by every game on the engine. It is a leaf — never
// imports game — and all offsets are UTF-8 byte indices (the UTF-16 seam
// lives only at the Monaco boundary in the frontend).
//
// parse.go is the error-tolerant recursive-descent parser: it never panics
// and always returns a usable CST plus any recovered structural errors.
package jomini

// Result is an immutable parse result. Src is the exact text that was parsed;
// all node offsets are byte indices into it.
type Result struct {
	Root     *Root
	Errors   []ParseError
	Comments []Comment
	Src      string
	lines    *LineIndex
}

// Lines returns the LineIndex for Src (built at parse time).
func (result Result) Lines() *LineIndex {
	if result.lines != nil {
		return result.lines
	}
	return NewLineIndex(result.Src)
}

// LineIndents is brace-depth indent per line. It tokenizes Src: a Result does
// not keep its tokens, because only the two editor-time readers here ever want
// them and a scan would otherwise carry one buffer per open file.
func (result Result) LineIndents() []int {
	toks := tokenize(result.Src)
	defer putTokens(toks)
	return lineIndentsFrom(result.Src, toks)
}

// TokenAt returns the identifier covering off (UTF-8 bytes). Comments yield
// an empty word. Src is tokenized on the fly; see LineIndents.
func (result Result) TokenAt(off int) (word string, start, end int) {
	src := result.Src
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	start, end = off, off
	var toks []token
	if src != "" {
		toks = tokenize(src)
		defer putTokens(toks)
	}
	for _, tok := range toks {
		if tok.kind == tokEOF || off < tok.start || off > tok.end {
			continue
		}
		if tok.kind == tokComment {
			return "", off, off
		}
		if off == tok.end && tok.start != tok.end {
			continue
		}
		switch tok.kind {
		case tokWord:
			return src[tok.start:tok.end], tok.start, tok.end
		case tokString:
			return identAt(src, off, tok.start, tok.end)
		}
	}
	return identAt(src, off, 0, len(src))
}

func identAt(src string, off, lo, hi int) (string, int, int) {
	if off < lo {
		off = lo
	}
	if off > hi {
		off = hi
	}
	start, end := off, off
	for start > lo && isIdentByte(src[start-1]) {
		start--
	}
	for end < hi && end < len(src) && isIdentByte(src[end]) {
		end++
	}
	if start == end {
		return "", start, end
	}
	return src[start:end], start, end
}

func isIdentByte(char byte) bool {
	return char == '_' || char == '.' || char == '-' ||
		(char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
}

// Parse parses script text into a CST. It never panics and always returns a
// usable (possibly empty) tree plus any recovered structural errors.
func Parse(src string) Result {
	li := NewLineIndex(src)
	toks := tokenize(src)
	parser := &parser{text: src, tokens: toks}
	root := parser.parse()
	// the tokens are not kept: LineIndents and TokenAt tokenize again
	putTokens(toks)
	return Result{
		Root:     root,
		Errors:   parser.errors,
		Comments: parser.comments,
		Src:      src,
		lines:    li,
	}
}

// parser holds recursive-descent state over a token stream. Comments are skipped
// transparently and recorded as they are passed.
type parser struct {
	text     string
	tokens   []token
	pos      int
	errors   []ParseError
	comments []Comment

	// Node slabs, so a parse does not allocate its nodes one at a time. A full slab is
	// REPLACED, never grown: append would copy to a new array and every pointer already
	// handed out would address the stale copy. Full slabs stay alive through those pointers.
	scalars []Scalar
	assigns []Assignment
	values  []ValueStmt
	blocks  []Block
	tagged  []TaggedBlock
}

// nodeSlab is how many nodes one allocation covers. 512 Scalars is ~20 KB —
// large enough that the allocation count collapses, small enough that a
// one-statement file does not hold a big buffer alive.
const nodeSlab = 512

func slabAlloc[T any](slab *[]T, value T) *T {
	if len(*slab) == cap(*slab) {
		*slab = make([]T, 0, nodeSlab)
	}
	*slab = append(*slab, value)
	return &(*slab)[len(*slab)-1]
}

func (parser *parser) parse() *Root {
	var statements []Statement
	for !parser.atEOF() {
		tok := parser.peek()
		if tok.kind == tokRBrace {
			parser.errors = append(parser.errors, ParseError{
				Code:    ErrStrayClose,
				Message: "Unexpected '}' with no matching open brace.",
				Range:   Range{Start: tok.start, End: tok.end},
			})
			parser.advance()
			continue
		}
		if st := parser.parseStatement(); st != nil {
			statements = append(statements, st)
		} else if !parser.atEOF() {
			parser.advance()
		}
	}
	return &Root{Statements: statements, Range: Range{Start: 0, End: len(parser.text)}}
}

func (parser *parser) skipComments() {
	for parser.pos < len(parser.tokens) && parser.tokens[parser.pos].kind == tokComment {
		comment := parser.tokens[parser.pos]
		parser.comments = append(parser.comments, Comment{
			Range: Range{Start: comment.start, End: comment.end},
		})
		parser.pos++
	}
}

func (parser *parser) peek() token {
	parser.skipComments()
	return parser.tokens[parser.pos]
}

func (parser *parser) peekAhead() token {
	parser.skipComments()
	j := parser.pos + 1
	for j < len(parser.tokens) && parser.tokens[j].kind == tokComment {
		j++
	}
	if j >= len(parser.tokens) {
		return parser.tokens[len(parser.tokens)-1]
	}
	return parser.tokens[j]
}

func (parser *parser) advance() token {
	parser.skipComments()
	text := parser.tokens[parser.pos]
	if parser.pos < len(parser.tokens)-1 {
		parser.pos++
	}
	return text
}

func (parser *parser) atEOF() bool {
	parser.skipComments()
	return parser.tokens[parser.pos].kind == tokEOF
}

// parseStatement := key (op value | block)? | value
func (parser *parser) parseStatement() Statement {
	tok := parser.peek()
	if tok.kind == tokEOF || tok.kind == tokRBrace {
		return nil
	}

	// A block starting here is an anonymous list element.
	if tok.kind == tokLBrace {
		block := parser.parseBlock()
		return slabAlloc(&parser.values, ValueStmt{Value: block, Range: block.Range})
	}

	if tok.kind == tokWord || tok.kind == tokString {
		key := parser.makeScalar(parser.advance())
		next := parser.peek()

		if next.kind == tokOp {
			opTok := parser.advance()
			value := parser.parseValueAfterOperator()
			if value == nil {
				parser.errors = append(parser.errors, ParseError{
					Code:    ErrMissingValue,
					Message: "Missing value after '" + opTok.value + "'.",
					Range:   Range{Start: opTok.start, End: opTok.end},
				})
			}
			end := opTok.end
			if value != nil {
				end = value.NodeRange().End
			}
			return slabAlloc(&parser.assigns, Assignment{Key: *key, Op: opTok.value, Value: value, Range: Range{Start: key.Range.Start, End: end}})
		}

		// GUI-style `key { ... }` with no operator.
		if next.kind == tokLBrace {
			block := parser.parseBlock()
			return slabAlloc(&parser.assigns, Assignment{Key: *key, Op: "", Value: block, Range: Range{Start: key.Range.Start, End: block.Range.End}})
		}

		// Bare scalar — list element.
		return slabAlloc(&parser.values, ValueStmt{Value: key, Range: key.Range})
	}

	parser.advance()
	return nil
}

// parseValueAfterOperator := scalar | block | tagged-block | operator-as-scalar.
// Returns nil if no value is parseable.
func (parser *parser) parseValueAfterOperator() Value {
	tok := parser.peek()

	if tok.kind == tokLBrace {
		return parser.parseBlock()
	}

	// A comparison operator used as a value (list/any triggers).
	if tok.kind == tokOp {
		opTok := parser.advance()
		return slabAlloc(&parser.scalars, Scalar{Text: parser.text[opTok.start:opTok.end], Quoted: false, Range: Range{Start: opTok.start, End: opTok.end}})
	}

	if tok.kind == tokWord || tok.kind == tokString {
		// `key =\n nextKey = ...`: if this scalar is immediately followed by an
		// operator, it is the next statement's key, not this value.
		if parser.peekAhead().kind == tokOp {
			return nil
		}
		scalar := parser.makeScalar(parser.advance())
		if parser.peek().kind == tokLBrace {
			block := parser.parseBlock()
			return slabAlloc(&parser.tagged, TaggedBlock{Tag: *scalar, Block: *block, Range: Range{Start: scalar.Range.Start, End: block.Range.End}})
		}
		return scalar
	}

	return nil
}

// parseBlock := `{` statement* `}`. On EOF before `}`, records unclosed-brace at
// the opening brace and swallows the rest of the input.
func (parser *parser) parseBlock() *Block {
	open := parser.advance()
	openBrace := open.start
	var statements []Statement
	closeBrace := -1
	end := open.end

	for {
		tok := parser.peek()
		if tok.kind == tokEOF {
			parser.errors = append(parser.errors, ParseError{
				Code:    ErrUnclosedBrace,
				Message: "Unclosed '{': the rest of the file is swallowed by this block.",
				Range:   Range{Start: openBrace, End: openBrace + 1},
			})
			end = tok.start
			break
		}
		if tok.kind == tokRBrace {
			closeBrace = tok.start
			end = tok.end
			parser.advance()
			break
		}
		if st := parser.parseStatement(); st != nil {
			statements = append(statements, st)
		} else if next := parser.peek().kind; next != tokRBrace && next != tokEOF {
			parser.advance()
		}
	}

	return slabAlloc(&parser.blocks, Block{
		Statements: statements,
		Range:      Range{Start: openBrace, End: end},
		OpenBrace:  openBrace,
		CloseBrace: closeBrace,
	})
}

// makeScalar builds a Scalar from a word/string token, stripping quotes from
// strings (Text) while keeping the range over the quotes. Unterminated strings
// record a diagnostic and take everything after the opening quote.
func (parser *parser) makeScalar(tok token) *Scalar {
	if tok.kind == tokString {
		var inner string
		if tok.unterminated {
			parser.errors = append(parser.errors, ParseError{
				Code:    ErrUnterminatedString,
				Message: "Unterminated string; recovered at end of line.",
				Range:   Range{Start: tok.start, End: tok.end},
			})
			inner = parser.text[tok.start+1 : tok.end]
		} else {
			inner = parser.text[tok.start+1 : tok.end-1]
		}
		return slabAlloc(&parser.scalars, Scalar{Text: inner, Quoted: true, Range: Range{Start: tok.start, End: tok.end}})
	}
	return slabAlloc(&parser.scalars, Scalar{Text: parser.text[tok.start:tok.end], Quoted: false, Range: Range{Start: tok.start, End: tok.end}})
}
