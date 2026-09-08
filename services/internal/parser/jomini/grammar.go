// grammar.go holds the Clausewitz words that are script structure rather than
// names, and the name shapes the games key their objects by. Both are shared by
// all three titles: a `limit` block is a limit in Crusader Kings III, Victoria 3
// and EU5 alike, and nothing here asks which game it is reading.
//
// Anything that DOES differ between the games lives in internal/game, in the
// file mirroring the package that needs it.

package jomini

import "strings"

// stopWords are keys that structure a script rather than name anything: control
// flow, boolean literals, arithmetic in a script value, and the description
// wrappers. A harvest that mints definitions from them fills the model with
// `if`, `yes` and `factor`.
var stopWords = map[string]bool{
	"if": true, "else": true, "else_if": true, "limit": true, "and": true,
	"or": true, "not": true, "nor": true, "nand": true, "this": true,
	"root": true, "prev": true, "from": true, "yes": true, "no": true,
	"value": true, "add": true, "multiply": true, "divide": true,
	"subtract": true, "min": true, "max": true, "factor": true, "base": true,
	"first_valid": true, "triggered_desc": true, "random_list": true,
}

// IsStop reports a structural keyword rather than a name.
func IsStop(key string) bool { return stopWords[strings.ToLower(key)] }

// The three name shapes below are tested on every key of every file, and a scan
// walks an install several times over, so they were the hottest regexes in the
// profile -- 4.5% of scan time for what are character-class checks. Written out
// they allocate nothing and never enter the regexp engine.
//
// nameshapes_test.go pins each against the pattern it replaced, and fuzzes the
// equivalence, because a silent difference changes what the whole model harvests.

// IsDefName reports `^[A-Za-z0-9_][A-Za-z0-9_.-]*$` -- a definition key.
func IsDefName(s string) bool {
	if s == "" {
		return false
	}
	if !isAlnumUnder(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isAlnumUnder(c) && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// IsEventID reports `^[A-Za-z0-9_-]+\.\d+$` -- `namespace.1234`.
func IsEventID(s string) bool {
	dot := strings.LastIndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 {
		return false
	}
	for i := 0; i < dot; i++ {
		if c := s[i]; !isAlnumUnder(c) && c != '-' {
			return false
		}
	}
	for i := dot + 1; i < len(s); i++ {
		if c := s[i]; c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// IsLowerName reports `^[a-z][a-z0-9_]*$` -- a lowercase script identifier.
func IsLowerName(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
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
