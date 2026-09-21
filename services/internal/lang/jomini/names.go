// names.go holds the name shapes the games key their objects by. They are shared
// by all three titles, and nothing here asks which game it is reading.
//
// Anything that DOES differ between the games lives in internal/game.

package jomini

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The three name shapes below are tested on every key of every file, and a scan
// walks an install several times over, so they were the hottest regexes in the
// profile -- 4.5% of scan time for what are character-class checks. Written out
// they allocate nothing and never enter the regexp engine.
//
// names_test.go pins each against the pattern it replaced, and fuzzes the
// equivalence, because a silent difference changes what the whole model harvests.

// The games ship non-ASCII identifiers and PMT has to read them. Victoria 3's
// `mon_general_petar_vukotić` is a vanilla character template DEFINITION; CK3
// saves a scope as `Linnéa`; a Victoria 3 mod names a journal entry
// `bpm_je_propaganda_of_the_deed_бомбометр`. Roughly 3 such tokens across the
// three vanilla installs and 11 across 31 workshop mods -- rare, and wrong every
// time it occurs.
//
// A byte at or above utf8.RuneSelf opens a multi-byte sequence, so it is decoded
// and judged by unicode. ASCII costs one extra comparison per byte and no
// allocation, which keeps these off the profile they were written to leave.
//
// Deliberately still rejected: `country_event·` (U+00B7 is punctuation, not a
// letter), which is a real mod typo these predicates should keep catching.

// IsDefName reports a definition key: the ASCII shape
// `^[A-Za-z0-9_][A-Za-z0-9_.-]*$`, widened to any Unicode letter or digit.
func IsDefName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if !isAlnumUnder(c) && !(i > 0 && (c == '.' || c == '-')) {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		i += size
	}
	return true
}

// IsEventID reports `namespace.1234`: the ASCII shape `^[A-Za-z0-9_-]+\.\d+$`,
// with the namespace half widened the same way as IsDefName. The number after
// the dot stays ASCII digits — it is an engine-assigned index, not a name.
func IsEventID(s string) bool {
	dot := strings.LastIndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 {
		return false
	}
	for i := 0; i < dot; {
		c := s[i]
		if c < utf8.RuneSelf {
			if !isAlnumUnder(c) && c != '-' {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:dot])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		i += size
	}
	for i := dot + 1; i < len(s); i++ {
		if c := s[i]; c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// IsLowerName reports a lowercase script identifier: the ASCII shape
// `^[a-z][a-z0-9_]*$`, widened to any lowercase Unicode letter.
//
// Lowercase, not any letter: this decides whether a word is an effect or trigger
// name, and the games write those in lower case. A script with no case
// distinction stays out, which is correct — nothing in the three games names a
// token that way, and admitting it would widen the vocabulary on a guess.
func IsLowerName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			ok := (c >= 'a' && c <= 'z') ||
				(i > 0 && (c == '_' || (c >= '0' && c <= '9')))
			if !ok {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLower(r) && !(i > 0 && unicode.IsDigit(r)) {
			return false
		}
		i += size
	}
	return true
}

func isAlnumUnder(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// PeelComment strips leading `#` markers from a line and reports whether any
// were there. A commented-out template still parses as script once peeled,
// which is how a game's shipped README documents a shape it never runs.
func PeelComment(raw string) (body string, hashed bool) {
	t := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if t == "" {
		return "", false
	}
	hashed = strings.HasPrefix(t, "#")
	for strings.HasPrefix(t, "#") {
		t = strings.TrimSpace(t[1:])
	}
	return t, hashed
}
