// lexer.go turns script text into tokens in one pass over its bytes; the parse keeps them.

package jomini

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

// token is a single lexeme: start and end are UTF-8 byte offsets into the text, and unterminated marks a string that
// reached the end of the text before its closing quote.
type token struct {
	kind         tokenKind
	unterminated bool
	start        int
	end          int
}

// BOM is the UTF-8 byte-order mark; Paradox files are UTF-8, frequently with one.
const BOM = "\xEF\xBB\xBF"

// tokenize lexes the whole text into tokens, ending with the end-of-input token. It never panics.
func tokenize(text string) []token {
	// script averages about 9 bytes a token
	tokens := make([]token, 0, len(text)/8+16)
	for pos := 0; pos < len(text); {
		var tok token
		switch char := text[pos]; {
		case isWhitespace(char):
			pos++
			continue
		case char == '#':
			end := pos + 1
			for end < len(text) && text[end] != '\n' && text[end] != '\r' {
				end++
			}
			tok = token{kind: tokComment, start: pos, end: end}
		case char == '{':
			tok = token{kind: tokLBrace, start: pos, end: pos + 1}
		case char == '}':
			tok = token{kind: tokRBrace, start: pos, end: pos + 1}
		case char == '"':
			tok = lexString(text, pos)
		default:
			var isOperator bool
			if tok, isOperator = lexOperator(text, pos); !isOperator {
				tok = lexWord(text, pos)
			}
		}
		tokens = append(tokens, tok)
		pos = tok.end
	}
	return append(tokens, token{kind: tokEOF, start: len(text), end: len(text)})
}

// isWhitespace reports the space, tab, newline, CR, FF and VT bytes that separate tokens.
func isWhitespace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\f' || char == '\v'
}

// lexString lexes the quoted string that starts at the quote at start, to its closing quote across lines as the game
// reads it; only the end of the text leaves it unterminated.
func lexString(text string, start int) token {
	pos := start + 1
	for pos < len(text) && text[pos] != '"' {
		if text[pos] == '\\' {
			pos++
		}
		pos++
	}
	if pos >= len(text) {
		return token{kind: tokString, start: start, end: len(text), unterminated: true}
	}
	return token{kind: tokString, start: start, end: pos + 1}
}

// lexOperator lexes the operator at pos: = == != < <= > >= ?=. A lone ! is tolerated as a one-byte word; a lone ? is
// no operator, and the word lexer takes it, so false.
func lexOperator(text string, pos int) (token, bool) {
	end := pos + 1
	if end < len(text) && text[end] == '=' {
		end++
	}
	char, hasEquals := text[pos], end == pos+2
	switch {
	case char == '=' || char == '<' || char == '>' || (char == '!' || char == '?') && hasEquals:
		return token{kind: tokOp, start: pos, end: end}, true
	case char == '!':
		return token{kind: tokWord, start: pos, end: end}, true
	}
	return token{}, false
}

// lexWord lexes the scalar run that starts at start, inline math, @[ ... ], included with the spaces inside it, and a
// ? that does not begin ?=. The first byte is always part of the word: a terminator never starts one.
func lexWord(text string, start int) token {
	pos := start
	for pos < len(text) {
		char := text[pos]
		switch {
		case char == '@' && pos+1 < len(text) && text[pos+1] == '[':
			// the math runs to its ], or to the end of its line when it has none
			pos += 2
			for pos < len(text) && text[pos] != ']' && text[pos] != '\n' && text[pos] != '\r' {
				pos++
			}
			if pos < len(text) && text[pos] == ']' {
				pos++
			}
		case char == '?' && pos+1 < len(text) && text[pos+1] == '=':
			return token{kind: tokWord, start: start, end: pos}
		case char == '?' || !isWordTerminator(char):
			pos++
		default:
			return token{kind: tokWord, start: start, end: pos}
		}
	}
	return token{kind: tokWord, start: start, end: pos}
}

// isWordTerminator reports bytes that end a bare word. `?` is deliberately absent: it only starts an operator when
// immediately followed by `=`.
func isWordTerminator(char byte) bool {
	switch char {
	case '{', '}', '#', '"', '=', '<', '>', '!':
		return true
	default:
		return isWhitespace(char)
	}
}
