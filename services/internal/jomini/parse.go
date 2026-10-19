// parse.go is the error-tolerant recursive-descent parser: it never panics and always returns a
// usable CST plus any recovered structural errors.

// Package jomini is the game-agnostic Paradox/Jomini script language: the syntax tree and grammar
// every game on the engine shares. It is a leaf that never imports game, and every offset is a
// UTF-8 byte index (UTF-16 is only the frontend's, at the Monaco boundary).
package jomini

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

// Parse parses script text into a CST: one lex and one parse, whose tokens and line index the Result keeps. It never
// panics and always returns a usable (possibly empty) tree plus any recovered structural errors.
func Parse(src string) Result {
	tokens := tokenize(src)
	parser := &parser{text: src, tokens: tokens}
	root := parser.parse()
	return Result{
		Root:     root,
		Errors:   parser.errors,
		Comments: parser.comments,
		Src:      src,
		tokens:   tokens,
		lines:    NewLineIndex(src),
	}
}

// parse consumes every token into the root's statements: a stray closing brace is recorded and
// passed, and a token no statement starts at is skipped.
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
		if statement := parser.parseStatement(); statement != nil {
			statements = append(statements, statement)
		} else if !parser.atEOF() {
			parser.advance()
		}
	}
	return &Root{Statements: statements, Range: Range{Start: 0, End: len(parser.text)}}
}

// skipComments records the comment tokens at the cursor and moves it past them, so it rests on a
// token that is no comment.
func (parser *parser) skipComments() {
	for parser.pos < len(parser.tokens) && parser.tokens[parser.pos].kind == tokComment {
		comment := parser.tokens[parser.pos]
		parser.comments = append(parser.comments, Comment{
			Range: Range{Start: comment.start, End: comment.end},
		})
		parser.pos++
	}
}

// peek returns the next token that is no comment and leaves the cursor on it.
func (parser *parser) peek() token {
	parser.skipComments()
	return parser.tokens[parser.pos]
}

// peekAhead returns the token after the next, comments passed over, or the end-of-input token when
// none follows; it leaves the cursor on the next token.
func (parser *parser) peekAhead() token {
	parser.skipComments()
	ahead := parser.pos + 1
	for ahead < len(parser.tokens) && parser.tokens[ahead].kind == tokComment {
		ahead++
	}
	if ahead >= len(parser.tokens) {
		return parser.tokens[len(parser.tokens)-1]
	}
	return parser.tokens[ahead]
}

// advance consumes the next token that is no comment and returns it; at the end-of-input token the
// cursor stays where it is.
func (parser *parser) advance() token {
	parser.skipComments()
	next := parser.tokens[parser.pos]
	if parser.pos < len(parser.tokens)-1 {
		parser.pos++
	}
	return next
}

// atEOF reports whether only the end-of-input token is left, comments passed over.
func (parser *parser) atEOF() bool {
	parser.skipComments()
	return parser.tokens[parser.pos].kind == tokEOF
}

// parseStatement consumes one statement, `key (op value | block)? | value`, and returns nil where
// none starts: at the end of input or a closing brace it consumes nothing, and a token that starts
// no statement it consumes alone. A key's value parses through parseBlock back into this function,
// once a nesting level; the statement is built by assignment, a leaf, so this frame stays small on
// that cycle.
func (parser *parser) parseStatement() Statement {
	tok := parser.peek()
	switch tok.kind {
	case tokEOF, tokRBrace:
		return nil
	case tokLBrace:
		// A block starting here is an anonymous list element.
		block := parser.parseBlock()
		return slabAlloc(&parser.values, ValueStmt{Value: block, Range: block.Range})
	case tokWord, tokString:
	default:
		parser.advance()
		return nil
	}
	key := parser.makeScalar(parser.advance())
	switch parser.peek().kind {
	case tokOp:
		op := parser.advance()
		return parser.assignment(key, op, parser.parseValueAfterOperator())
	case tokLBrace:
		// GUI-style `key { ... }` with no operator.
		return parser.assignment(key, token{}, parser.parseBlock())
	}
	// Bare scalar: a list element.
	return slabAlloc(&parser.values, ValueStmt{Value: key, Range: key.Range})
}

// assignment builds the Assignment of a key, its operator token and its value, and records a missing
// value where an operator has none. A GUI-style `key { ... }` has the zero token, which has no operator
// text and no end of its own.
func (parser *parser) assignment(key *Scalar, op token, value Value) Statement {
	operator := parser.text[op.start:op.end]
	if value == nil {
		parser.errors = append(parser.errors, ParseError{
			Code:    ErrMissingValue,
			Message: "Missing value after '" + operator + "'.",
			Range:   Range{Start: op.start, End: op.end},
		})
	}
	end := op.end
	if value != nil {
		end = value.NodeRange().End
	}
	return slabAlloc(&parser.assigns, Assignment{
		Key: *key, Op: operator, Value: value, Range: Range{Start: key.Range.Start, End: end},
	})
}

// parseValueAfterOperator consumes the value after an operator: a scalar, a block, a tagged block,
// or a comparison operator written as a scalar. It returns nil and consumes nothing where no value
// follows.
func (parser *parser) parseValueAfterOperator() Value {
	tok := parser.peek()
	switch tok.kind {
	case tokLBrace:
		return parser.parseBlock()
	case tokOp:
		// A comparison operator used as a value (list/any triggers).
		op := parser.advance()
		return slabAlloc(&parser.scalars, Scalar{Text: parser.text[op.start:op.end], Range: Range{Start: op.start, End: op.end}})
	case tokWord, tokString:
		// `key =\n nextKey = ...`: if this scalar is immediately followed by an
		// operator, it is the next statement's key, not this value.
		if parser.peekAhead().kind == tokOp {
			return nil
		}
		scalar := parser.makeScalar(parser.advance())
		switch next := parser.peek(); {
		case next.kind == tokLBrace:
			block := parser.parseBlock()
			return slabAlloc(&parser.tagged, TaggedBlock{Tag: *scalar, Block: *block, Range: Range{Start: scalar.Range.Start, End: block.Range.End}})
		case next.kind == tokString && !scalar.Quoted && parser.peekAhead().kind != tokOp:
			return parser.parseTaggedText(scalar)
		}
		return scalar
	}
	return nil
}

// parseTaggedText consumes the quoted text after a tag, `list "x"`, into a tagged value with no braces: its Block
// holds the text as its one statement and has no brace offsets.
func (parser *parser) parseTaggedText(tag *Scalar) Value {
	text := parser.makeScalar(parser.advance())
	statement := slabAlloc(&parser.values, ValueStmt{Value: text, Range: text.Range})
	block := Block{Statements: []Statement{statement}, Range: text.Range, OpenBrace: -1, CloseBrace: -1}
	return slabAlloc(&parser.tagged, TaggedBlock{Tag: *tag, Block: block, Range: Range{Start: tag.Range.Start, End: text.Range.End}})
}

// parseBlock consumes `{` statement* `}` from the opening brace to just past the closing one. On
// end of input before `}` it records unclosed-brace at the opening brace and swallows the rest of
// the input.
func (parser *parser) parseBlock() *Block {
	open := parser.advance()
	openBrace := open.start
	var statements []Statement
	closeBrace, end := -1, 0

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
		if statement := parser.parseStatement(); statement != nil {
			statements = append(statements, statement)
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
		textEnd := tok.end - 1
		if tok.unterminated {
			parser.errors = append(parser.errors, ParseError{
				Code:    ErrUnterminatedString,
				Message: "Unterminated string: it runs to the end of the file.",
				Range:   Range{Start: tok.start, End: tok.end},
			})
			textEnd = tok.end
		}
		return slabAlloc(&parser.scalars, Scalar{Text: parser.text[tok.start+1 : textEnd], Quoted: true, Range: Range{Start: tok.start, End: tok.end}})
	}
	return slabAlloc(&parser.scalars, Scalar{Text: parser.text[tok.start:tok.end], Quoted: false, Range: Range{Start: tok.start, End: tok.end}})
}

// slabAlloc stores value in the slab and returns its address, starting a fresh slab when the
// current one is full.
func slabAlloc[T any](slab *[]T, value T) *T {
	if len(*slab) == cap(*slab) {
		*slab = make([]T, 0, nodeSlab)
	}
	*slab = append(*slab, value)
	return &(*slab)[len(*slab)-1]
}
