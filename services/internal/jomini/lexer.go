// lexer.go turns a file's bytes into tokens: the single-pass, allocation-light tokenizer for
// Paradox script.

package jomini

import (
	"sync"
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

// BOM is the UTF-8 byte-order mark; Paradox files are UTF-8, frequently with one.
const BOM = "\xEF\xBB\xBF"

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
		char := text[pos]
		if char == '@' && pos+1 < length && text[pos+1] == '[' {
			pos += 2
			for pos < length && text[pos] != ']' && text[pos] != '\n' && text[pos] != '\r' {
				pos++
			}
			if pos < length && text[pos] == ']' {
				pos++
			}
			continue
		}
		if char == '?' {
			if pos+1 < length && text[pos+1] == '=' {
				break
			}
			pos++
			continue
		}
		if isWordTerminator(char) {
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

// lineIndentsFrom is each line's brace depth, from the tokens: the depth before the line, less the closing braces
// that lead it.
func lineIndentsFrom(src string, toks []token) []int {
	index := NewLineIndex(src)
	count := index.LineCount()
	openBefore := make([]int, count)
	closers := make([]int, count)
	depth := 0
	next := 0
	for line := range count {
		openBefore[line] = depth
		lineEnd := index.length
		if line+1 < count {
			lineEnd = index.lineStarts[line+1]
		}
		leading, sawNon := 0, false
		for next < len(toks) && toks[next].start < lineEnd {
			switch toks[next].kind {
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
			next++
		}
		closers[line] = leading
	}
	out := make([]int, count)
	for i := range out {
		depth := max(openBefore[i]-closers[i], 0)
		out[i] = depth
	}
	return out
}
