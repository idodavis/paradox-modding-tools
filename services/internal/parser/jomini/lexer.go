// lexer.go is the single-pass, allocation-light tokenizer for Paradox script.
// All significant characters are ASCII, so it scans by byte; non-ASCII bytes are
// only ever part of words/strings/comments.

package jomini

import "sync"

// tokenKind enumerates the lexical token classes of Paradox script.
type tokenKind uint8

const (
	tokLBrace  tokenKind = iota // {
	tokRBrace                   // }
	tokOp                       // = ?= == != < <= > >=
	tokString                   // quoted "..."
	tokComment                  // # to end of line (text includes leading #)
	tokWord                     // scalar run
	tokEOF
)

// token is a single lexeme. start/end are UTF-8 byte offsets into the source.
// value holds the operator text for tokOp; unterminated marks a string that hit
// end-of-line/EOF before its closing quote.
type token struct {
	kind         tokenKind
	start        int
	end          int
	value        string
	unterminated bool
}

// maxStringSpan caps how many newlines a quoted string may cross while a `[`
// data-function bracket is still open, so a stray `"[` cannot swallow a whole file.
const maxStringSpan = 32

// isWhitespace reports the space/tab/newline/CR/FF/VT bytes that separate tokens.
func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// isWordTerminator reports bytes that end a bare word. `?` is deliberately absent:
// it only starts an operator when immediately followed by `=`.
func isWordTerminator(c byte) bool {
	switch c {
	case '{', '}', '#', '"', '=', '<', '>', '!':
		return true
	default:
		return isWhitespace(c)
	}
}

// tokenize lexes the entire source into tokens (with a trailing EOF). Never panics.
// tokenPool reuses token buffers across parses. A scan walks the install four
// times over and tokenize was 27.9% of everything it allocated, all of it thrown
// away a moment later -- the CST slices its strings out of the source, so no
// token outlives the parse that produced it.
var tokenPool sync.Pool

// getTokens takes a buffer sized for text, from the pool where one fits.
func getTokens(text string) []token {
	want := len(text)/8 + 16
	if v := tokenPool.Get(); v != nil {
		if b, ok := v.([]token); ok && cap(b) >= want {
			return b[:0]
		}
	}
	return make([]token, 0, want)
}

// putTokens returns a buffer. Very large ones are dropped rather than pinned:
// one 5 MB file would otherwise keep its buffer alive for the whole run.
func putTokens(b []token) {
	if cap(b) > 1<<20 {
		return
	}
	tokenPool.Put(b[:0]) //nolint:staticcheck // slices are the point here
}

func tokenize(text string) []token {
	// Measured across 900 CK3 vanilla files: 8.84 bytes per token. The buffer
	// assumed 4, so every parse over-allocated the token slice 2.2×, and a scan
	// parses the whole install several times over. Slightly under-guessing costs
	// at most one doubling; over-guessing costs the difference on every file.
	tokens := getTokens(text)
	length := len(text)
	i := 0

	for i < length {
		c := text[i]

		if isWhitespace(c) {
			i++
			continue
		}

		if c == '#' {
			start := i
			i++
			for i < length && text[i] != '\n' && text[i] != '\r' {
				i++
			}
			tokens = append(tokens, token{kind: tokComment, start: start, end: i})
			continue
		}

		if c == '{' {
			tokens = append(tokens, token{kind: tokLBrace, start: i, end: i + 1})
			i++
			continue
		}
		if c == '}' {
			tokens = append(tokens, token{kind: tokRBrace, start: i, end: i + 1})
			i++
			continue
		}

		if c == '"' {
			tokens = append(tokens, lexString(text, &i))
			continue
		}

		if tok, ok := lexOperator(text, &i); ok {
			tokens = append(tokens, tok)
			continue
		}

		tokens = append(tokens, lexWord(text, &i))
	}

	tokens = append(tokens, token{kind: tokEOF, start: length, end: length})
	return tokens
}

// lexString consumes a quoted string starting at *i (on the opening quote). A
// newline continues the string only while a `[` data-function bracket is open.
func lexString(text string, i *int) token {
	length := len(text)
	start := *i
	p := *i + 1
	unterminated := false
	brackets := 0
	spanned := 0
	for {
		if p >= length {
			unterminated = true
			break
		}
		cc := text[p]
		if cc == '\\' {
			p += 2
			continue
		}
		if cc == '"' {
			p++
			break
		}
		if cc == '\n' || cc == '\r' {
			if brackets == 0 || spanned >= maxStringSpan {
				unterminated = true
				break
			}
			if cc == '\n' {
				spanned++
			}
			p++
			continue
		}
		if cc == '[' {
			brackets++
		} else if cc == ']' && brackets > 0 {
			brackets--
		}
		p++
	}
	*i = p
	return token{kind: tokString, start: start, end: p, unterminated: unterminated}
}

// lexOperator consumes an operator token at *i, or reports ok=false to fall
// through to word lexing (lone `?`, or `!`/`?` not forming an operator).
func lexOperator(text string, i *int) (token, bool) {
	length := len(text)
	p := *i
	c := text[p]
	twoChar := func(op string) token {
		*i = p + 2
		return token{kind: tokOp, start: p, end: p + 2, value: op}
	}
	oneChar := func(op string) token {
		*i = p + 1
		return token{kind: tokOp, start: p, end: p + 1, value: op}
	}
	hasEq := p+1 < length && text[p+1] == '='
	switch c {
	case '=':
		if hasEq {
			return twoChar("=="), true
		}
		return oneChar("="), true
	case '!':
		if hasEq {
			return twoChar("!="), true
		}
		// Lone `!`: tolerate as a one-char word.
		*i = p + 1
		return token{kind: tokWord, start: p, end: p + 1}, true
	case '<':
		if hasEq {
			return twoChar("<="), true
		}
		return oneChar("<"), true
	case '>':
		if hasEq {
			return twoChar(">="), true
		}
		return oneChar(">"), true
	case '?':
		if hasEq {
			return twoChar("?="), true
		}
		// Lone `?`: not an operator; let the word scanner pick it up.
		return token{}, false
	default:
		return token{}, false
	}
}

// lexWord consumes a scalar run at *i, including inline-math `@[ ... ]` (spaces
// allowed inside) and a lone `?` that does not begin `?=`.
func lexWord(text string, i *int) token {
	length := len(text)
	start := *i
	p := *i
	for p < length {
		cc := text[p]
		if cc == '@' && p+1 < length && text[p+1] == '[' {
			p += 2
			for p < length && text[p] != ']' && text[p] != '\n' && text[p] != '\r' {
				p++
			}
			if p < length && text[p] == ']' {
				p++
			}
			continue
		}
		if cc == '?' {
			if p+1 < length && text[p+1] == '=' {
				break
			}
			p++
			continue
		}
		if isWordTerminator(cc) {
			break
		}
		p++
	}
	if p == start {
		// Defensive: never fail to advance on pathological input.
		p++
	}
	*i = p
	return token{kind: tokWord, start: start, end: p}
}

// LineIndents returns the target tab-indent for each line of src, computed from
// brace depth so strings and comments cannot fool it.
func LineIndents(src string) []int {
	return lineIndentsFrom(src, tokenize(src))
}

func lineIndentsFrom(src string, toks []token) []int {
	li := NewLineIndex(src)
	n := li.LineCount()
	openBefore := make([]int, n)
	closers := make([]int, n)
	depth := 0
	ti := 0
	for line := 0; line < n; line++ {
		openBefore[line] = depth
		lineEnd := li.length
		if line+1 < n {
			lineEnd = li.lineStarts[line+1]
		}
		leading, sawNon := 0, false
		for ti < len(toks) && toks[ti].start < lineEnd {
			switch toks[ti].kind {
			case tokLBrace:
				depth++
				sawNon = true
			case tokRBrace:
				if depth > 0 {
					depth--
				}
				if !sawNon {
					leading++
				}
			case tokEOF:
			default:
				sawNon = true
			}
			ti++
		}
		closers[line] = leading
	}
	out := make([]int, n)
	for i := range out {
		d := openBefore[i] - closers[i]
		if d < 0 {
			d = 0
		}
		out[i] = d
	}
	return out
}
