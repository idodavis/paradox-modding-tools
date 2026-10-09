// lexer.go turns a file's bytes into tokens: the single-pass, allocation-light tokenizer for
// Paradox script, and the decode and encode between a file's bytes and its text (a UTF-8 byte order
// mark apart, Windows-1252 where the bytes are not UTF-8).

package jomini

import (
	"bytes"
	"fmt"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
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
// value holds the operator text for tokOp; unterminated marks a string that reached
// the end of the text before its closing quote.
type token struct {
	kind         tokenKind
	start        int
	end          int
	value        string
	unterminated bool
}

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
// and the CST slices its strings out of the source, so no token outlives its parse. It holds
// *[]token, so a Put stores no slice header in an interface.
var tokenPool sync.Pool

// getTokens takes a buffer sized for text, from the pool where one fits. Script averages about
// 9 bytes a token; under-guessing costs one doubling, over-guessing costs memory on every file.
func getTokens(text string) []token {
	want := len(text)/8 + 16
	if pooled, ok := tokenPool.Get().(*[]token); ok && cap(*pooled) >= want {
		return (*pooled)[:0]
	}
	return make([]token, 0, want)
}

// putTokens returns a buffer. Very large ones are dropped rather than pinned:
// one 5 MB file would otherwise keep its buffer alive for the whole run.
func putTokens(tokens []token) {
	if cap(tokens) > 1<<20 {
		return
	}
	tokens = tokens[:0]
	tokenPool.Put(&tokens)
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

// lexString consumes a quoted string starting at *cursor (on the opening quote), to its closing
// quote across lines as the game reads it; only the end of the text leaves it unterminated.
func lexString(text string, cursor *int) token {
	start, pos := *cursor, *cursor+1
	for pos < len(text) && text[pos] != '"' {
		if text[pos] == '\\' {
			pos++
		}
		pos++
	}
	unterminated := pos >= len(text)
	if !unterminated {
		pos++
	}
	*cursor = min(pos, len(text))
	return token{kind: tokString, start: start, end: *cursor, unterminated: unterminated}
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

// BOM is the UTF-8 byte-order mark; Paradox files are UTF-8, frequently with one.
const BOM = "\xEF\xBB\xBF"

// Decode is a file's bytes as text, for the engine and the editor alike: a leading BOM taken off (bom), and bytes
// that are not valid UTF-8 read as Windows-1252, the games' old encoding (legacy). Line ends are kept.
func Decode(raw []byte) (text string, bom, legacy bool) {
	body, bom := bytes.CutPrefix(raw, []byte(BOM))
	if utf8.Valid(body) {
		return string(body), bom, false
	}
	text, _ = charmap.Windows1252.NewDecoder().String(string(body))
	return text, bom, true
}

// Encode is text as a file's bytes: the BOM when it had one, and Windows-1252 when it was read so; a character
// Windows-1252 has no byte for is an error, never a silent change.
func Encode(text string, bom, legacy bool) ([]byte, error) {
	if legacy {
		var err error
		if text, err = charmap.Windows1252.NewEncoder().String(text); err != nil {
			return nil, fmt.Errorf("the file is Windows-1252, which has no byte for a character in it: %w", err)
		}
	}
	if bom {
		text = BOM + text
	}
	return []byte(text), nil
}
