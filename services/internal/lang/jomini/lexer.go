// lexer.go turns a file's bytes into tokens: first the decode, which strips a UTF-8 byte order
// mark, reads UTF-8 where the bytes are valid UTF-8 and latin1 where they are not, and
// normalises line endings; then the single-pass, allocation-light tokenizer for Paradox script.
package jomini

import (
	"strings"
	"sync"
	"unicode/utf8"
)

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
func isWhitespace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\f' || char == '\v'
}

// isWordTerminator reports bytes that end a bare word. `?` is deliberately absent:
// it only starts an operator when immediately followed by `=`.
func isWordTerminator(char byte) bool {
	switch char {
	case '{', '}', '#', '"', '=', '<', '>', '!':
		return true
	default:
		return isWhitespace(char)
	}
}

// tokenPool reuses token buffers across parses: a scan parses the install several times over,
// and the CST slices its strings out of the source, so no token outlives its parse.
var tokenPool sync.Pool

// getTokens takes a buffer sized for text, from the pool where one fits. Script averages about
// 9 bytes a token; under-guessing costs one doubling, over-guessing costs memory on every file.
func getTokens(text string) []token {
	want := len(text)/8 + 16
	if pooled := tokenPool.Get(); pooled != nil {
		if tokens, ok := pooled.([]token); ok && cap(tokens) >= want {
			return tokens[:0]
		}
	}
	return make([]token, 0, want)
}

// putTokens returns a buffer. Very large ones are dropped rather than pinned:
// one 5 MB file would otherwise keep its buffer alive for the whole run.
func putTokens(tokens []token) {
	if cap(tokens) > 1<<20 {
		return
	}
	tokenPool.Put(tokens[:0]) //nolint:staticcheck // slices are the point here
}

// tokenize lexes the entire source into tokens (with a trailing EOF). Never panics.
func tokenize(text string) []token {
	tokens := getTokens(text)
	length := len(text)
	i := 0

	for i < length {
		char := text[i]

		if isWhitespace(char) {
			i++
			continue
		}

		if char == '#' {
			start := i
			i++
			for i < length && text[i] != '\n' && text[i] != '\r' {
				i++
			}
			tokens = append(tokens, token{kind: tokComment, start: start, end: i})
			continue
		}

		if char == '{' {
			tokens = append(tokens, token{kind: tokLBrace, start: i, end: i + 1})
			i++
			continue
		}
		if char == '}' {
			tokens = append(tokens, token{kind: tokRBrace, start: i, end: i + 1})
			i++
			continue
		}

		if char == '"' {
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

// lexString consumes a quoted string starting at *cursor (on the opening quote). A
// newline continues the string only while a `[` data-function bracket is open.
func lexString(text string, cursor *int) token {
	length := len(text)
	start := *cursor
	pos := *cursor + 1
	unterminated := false
	brackets := 0
	spanned := 0
	for {
		if pos >= length {
			unterminated = true
			break
		}
		cc := text[pos]
		if cc == '\\' {
			pos += 2
			continue
		}
		if cc == '"' {
			pos++
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
			pos++
			continue
		}
		if cc == '[' {
			brackets++
		} else if cc == ']' && brackets > 0 {
			brackets--
		}
		pos++
	}
	*cursor = pos
	return token{kind: tokString, start: start, end: pos, unterminated: unterminated}
}

// lexOperator consumes an operator token at *cursor, or reports ok=false to fall
// through to word lexing (lone `?`, or `!`/`?` not forming an operator).
func lexOperator(text string, cursor *int) (token, bool) {
	length := len(text)
	pos := *cursor
	char := text[pos]
	twoChar := func(op string) token {
		*cursor = pos + 2
		return token{kind: tokOp, start: pos, end: pos + 2, value: op}
	}
	oneChar := func(op string) token {
		*cursor = pos + 1
		return token{kind: tokOp, start: pos, end: pos + 1, value: op}
	}
	hasEq := pos+1 < length && text[pos+1] == '='
	switch char {
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
		*cursor = pos + 1
		return token{kind: tokWord, start: pos, end: pos + 1}, true
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

// lexWord consumes a scalar run at *cursor, including inline-math `@[ ... ]` (spaces
// allowed inside) and a lone `?` that does not begin `?=`.
func lexWord(text string, cursor *int) token {
	length := len(text)
	start := *cursor
	pos := *cursor
	for pos < length {
		cc := text[pos]
		if cc == '@' && pos+1 < length && text[pos+1] == '[' {
			pos += 2
			for pos < length && text[pos] != ']' && text[pos] != '\n' && text[pos] != '\r' {
				pos++
			}
			if pos < length && text[pos] == ']' {
				pos++
			}
			continue
		}
		if cc == '?' {
			if pos+1 < length && text[pos+1] == '=' {
				break
			}
			pos++
			continue
		}
		if isWordTerminator(cc) {
			break
		}
		pos++
	}
	if pos == start {
		// Defensive: never fail to advance on pathological input.
		pos++
	}
	*cursor = pos
	return token{kind: tokWord, start: start, end: pos}
}

func lineIndentsFrom(src string, toks []token) []int {
	li := NewLineIndex(src)
	n := li.LineCount()
	openBefore := make([]int, n)
	closers := make([]int, n)
	depth := 0
	ti := 0
	for line := range n {
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
		depth := max(openBefore[i]-closers[i], 0)
		out[i] = depth
	}
	return out
}

// UTF-8 byte-order-mark bytes. Paradox files are UTF-8, frequently with a BOM.
var utf8BOM = [3]byte{0xEF, 0xBB, 0xBF}

// HasUTF8BOM reports whether raw begins with a UTF-8 byte-order mark.
func HasUTF8BOM(raw []byte) bool {
	return len(raw) >= 3 && raw[0] == utf8BOM[0] && raw[1] == utf8BOM[1] && raw[2] == utf8BOM[2]
}

// Normalize drops CR so Windows CRLF files share offsets with LF editor buffers.
func Normalize(text string) string {
	return strings.ReplaceAll(text, "\r", "")
}

// Decode turns raw file bytes into text for parsing. It strips a leading UTF-8
// BOM and CR, and returns hadBOM. If the bytes are not valid UTF-8 (typically
// hand-edited Latin-1/Windows-1252 files) it falls back to a latin1 decode so
// callers get usable text rather than U+FFFD replacement characters.
func Decode(raw []byte) (text string, hadBOM bool) {
	hadBOM = HasUTF8BOM(raw)
	body := raw
	if hadBOM {
		body = raw[3:]
	}
	if utf8.Valid(body) {
		return Normalize(string(body)), hadBOM
	}
	return Normalize(latin1Decode(raw)), false
}

// latin1Decode maps each byte 1:1 to U+00xx. Used only when raw is not valid UTF-8.
func latin1Decode(raw []byte) string {
	var out strings.Builder
	out.Grow(len(raw))
	for _, char := range raw {
		out.WriteRune(rune(char))
	}
	return out.String()
}
