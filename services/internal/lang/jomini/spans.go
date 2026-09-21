// spans.go splits the four spellings a single written word can carry inside it, the same way in every Jomini game: a cite (prefix:name, with a |member and a .chain after it), an entry mode before a definition's name (REPLACE:name), a constant (@name), and the $NAME$ parameters a macro's text holds. It only splits. Whether a prefix is one the game declares, which kind it belongs to, and what a mode means are questions for the layers above, which is what keeps this package free of any game.
package jomini

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Cited is a word written prefix:name, split.
type Cited struct {
	Prefix string // character, title, scope, define
	Name   string // 186004, k_france, target, NGame
	Member string // the key after a bar: define:NGame|START_DATE
	Chain  string // what follows the first dot: scope:target.culture -> culture
}

// Cite splits a word written prefix:name. It refuses a word whose prefix is not one plain lower-case identifier (root.var:x is a scope path, INJECT:x an entry mode), and a colon followed by a slash (event:/SFX/x is a sound's name, file:/x a file, C:/x a path).
func Cite(text string) (Cited, bool) {
	colon := strings.IndexByte(text, ':')
	if colon <= 0 || colon == len(text)-1 || strings.ContainsRune(`/\`, rune(text[colon+1])) {
		return Cited{}, false
	}

	// the prefix: a letter, then letters, digits and underscores, with at least one lower-case letter
	prefix, lower := text[:colon], false
	for i, r := range prefix {
		if !(unicode.IsLetter(r) || r == '_' || (i > 0 && unicode.IsDigit(r))) || (i == 0 && r == '_') {
			return Cited{}, false
		}
		lower = lower || unicode.IsLower(r)
	}
	if !lower {
		return Cited{}, false
	}

	// the name runs to a bar, a dot or the end
	rest := text[colon+1:]
	end := nameEnd(rest)
	if end == 0 {
		return Cited{}, false
	}
	c := Cited{Prefix: prefix, Name: rest[:end]}
	rest = rest[end:]

	// a member after a bar, then a chain after a dot
	if strings.HasPrefix(rest, "|") {
		end = nameEnd(rest[1:])
		if end == 0 {
			return Cited{}, false
		}
		c.Member, rest = rest[1:1+end], rest[1+end:]
	}
	if strings.HasPrefix(rest, ".") && len(rest) > 1 {
		c.Chain, rest = rest[1:], ""
	}
	if rest != "" {
		return Cited{}, false
	}
	return c, true
}

// nameEnd is how many bytes of s are a name: letters, digits, underscores and hyphens.
func nameEnd(s string) int {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return i
		}
		i += size
	}
	return len(s)
}

// Mode splits an entry mode off a definition's name: REPLACE:name gives REPLACE and name. A mode is upper-case letters and underscores; any other word comes back whole with no mode.
func Mode(key string) (mode, name string) {
	colon := strings.IndexByte(key, ':')
	if colon <= 0 || colon == len(key)-1 {
		return "", key
	}

	for _, r := range key[:colon] {
		if !(r >= 'A' && r <= 'Z') && r != '_' {
			return "", key
		}
	}
	return key[:colon], key[colon+1:]
}

// Constant is the name in @name. An inline expression, @[ … ], is a constant with no name.
func Constant(text string) (name string, ok bool) {
	if len(text) < 2 || text[0] != '@' {
		return "", false
	}
	if text[1] == '[' {
		return "", true
	}
	return text[1:], true
}

// Span is one $NAME$ inside a word or a string, by byte offset into it; Start is the first dollar and End is past the last.
type Span struct {
	Name       string
	Start, End int
}

// Interpolations is every $NAME$ in text, in order. A dollar with no partner, and two dollars with nothing between, are not parameters.
func Interpolations(text string) []Span {
	var spans []Span
	for i := 0; i < len(text); {
		open := strings.IndexByte(text[i:], '$')
		if open < 0 {
			break
		}
		open += i

		// the name is word characters up to the closing dollar
		j := open + 1
		for j < len(text) && isAlnumUnder(text[j]) {
			j++
		}
		if j > open+1 && j < len(text) && text[j] == '$' {
			spans = append(spans, Span{Name: text[open+1 : j], Start: open, End: j + 1})
			i = j + 1
			continue
		}
		i = open + 1
	}
	return spans
}
