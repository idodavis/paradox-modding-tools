// nameshapes_test.go pins the hand-written name checks against the regular
// expressions they stand in for. They are tested on every key of every file and
// a scan walks the install four times, so they were the hottest regexes in the
// profile -- but a silent difference would change what the whole model harvests.
//
// The patterns are Unicode-aware because the checks are: the games ship
// `mon_general_petar_vukotić` as a Victoria 3 character template and save a CK3
// scope as `Linnéa`. The earlier ASCII-only patterns are still here as a second
// assertion, scoped to ASCII input, so the widening cannot quietly go further
// than intended.

package jomini

import (
	"regexp"
	"testing"
	"unicode/utf8"
)

var (
	defNameRe = regexp.MustCompile(`^[\p{L}\p{Nd}_][\p{L}\p{Nd}_.-]*$`)
	eventIDRe = regexp.MustCompile(`^[\p{L}\p{Nd}_-]+\.\d+$`)
	nameOKRe  = regexp.MustCompile(`^\p{Ll}[\p{Ll}\p{Nd}_]*$`)

	// The originals, verbatim. Equivalence to these holds for ASCII only.
	asciiDefNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	asciiEventIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.\d+$`)
	asciiNameOKRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func TestNameShapesMatchPatterns(t *testing.T) {
	// Real shapes from the three installs, plus the edges that decide the
	// boundaries: empty, leading punctuation, trailing dot, spaces — and the
	// non-ASCII identifiers the games and mods actually ship.
	inputs := []string{
		"", " ", ".", "-", "_", "a", "A", "0", "9",
		"brave", "k_england", "00_traits", "birth.1", "birth.0001",
		"namespace.1234", "a-b", "a.b", "a..b", "a.", ".a", "-a", "a-",
		"scope:foo", "title:k_england", "$PARAM$", "yes", "no",
		"867.1.1", "8082.1.1", "1.0", "1.", "x.1x", "x.1.2",
		"UPPER", "Mixed_Case", "with space", "tab\there", "naïve", "café",
		"a_b_c", "_leading", "trailing_", "__", "a1_b2",
		"add_gold", "any_trait_in_category", "PRICE", "SCORE_VAR",
		"e.1", "e.01", "e.1a", "1.e", "a.b.1", "a-b.1", "a_b.12345",

		// Shipped non-ASCII. See the comment above IsDefName.
		"mon_general_petar_vukotić", "Linnéa", "RU_GL_КР_DEFAULT",
		"bpm_je_propaganda_of_the_deed_бомбометр",
		"gaudi_pavellons_güell_global_var", "religión",
		"vukotić.1", "бомбометр.42",
		// Punctuation that merely looks like a letter stays out: this one is a
		// real mod typo (`type = country_event·`) worth continuing to reject.
		"country_event·", "café·", "·",
	}
	for _, s := range inputs {
		t.Run("q_"+s, func(t *testing.T) {
			if got, want := IsDefName(s), defNameRe.MatchString(s); got != want {
				t.Errorf("IsDefName(%q) = %v, pattern says %v", s, got, want)
			}
			if got, want := IsEventID(s), eventIDRe.MatchString(s); got != want {
				t.Errorf("IsEventID(%q) = %v, pattern says %v", s, got, want)
			}
			if got, want := IsLowerName(s), nameOKRe.MatchString(s); got != want {
				t.Errorf("IsLowerName(%q) = %v, pattern says %v", s, got, want)
			}
			if !isASCII(s) {
				return
			}
			// Nothing ASCII may have changed meaning.
			if got, want := IsDefName(s), asciiDefNameRe.MatchString(s); got != want {
				t.Errorf("ASCII drift: IsDefName(%q) = %v, original says %v", s, got, want)
			}
			if got, want := IsEventID(s), asciiEventIDRe.MatchString(s); got != want {
				t.Errorf("ASCII drift: IsEventID(%q) = %v, original says %v", s, got, want)
			}
			if got, want := IsLowerName(s), asciiNameOKRe.MatchString(s); got != want {
				t.Errorf("ASCII drift: IsLowerName(%q) = %v, original says %v", s, got, want)
			}
		})
	}
}

// The three shipped identifiers, stated as behaviour rather than as equivalence
// to a pattern — so the intent survives someone rewriting the patterns.
func TestNameShapesAcceptShippedNonASCII(t *testing.T) {
	for _, s := range []string{
		"mon_general_petar_vukotić",               // Vic3 vanilla character template
		"bpm_je_propaganda_of_the_deed_бомбометр", // Vic3 mod journal entry
		"gaudi_pavellons_güell_global_var",        // CK3 mod global var
	} {
		if !IsDefName(s) {
			t.Errorf("IsDefName(%q) = false; the game ships this as a key", s)
		}
	}
	if IsDefName("country_event·") {
		t.Error("U+00B7 is punctuation, not a letter: this typo must stay rejected")
	}
}

// FuzzNameShapes is the same equivalence, over inputs nobody thought of.
func FuzzNameShapes(f *testing.F) {
	for _, s := range []string{
		"", "a", "birth.1", "k_england", "a-b.12", "\x00", "é",
		"vukotić", "бомбометр.42", "\xff\xfe", "·", "Ā", "ā",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if IsDefName(s) != defNameRe.MatchString(s) {
			t.Fatalf("IsDefName(%q) disagrees with the pattern", s)
		}
		if IsEventID(s) != eventIDRe.MatchString(s) {
			t.Fatalf("IsEventID(%q) disagrees with the pattern", s)
		}
		if IsLowerName(s) != nameOKRe.MatchString(s) {
			t.Fatalf("IsLowerName(%q) disagrees with the pattern", s)
		}
	})
}

// These run on every key of every file, four walks per scan, so they are kept
// off the profile deliberately. Widening them to Unicode cost ~16% here
// (232 -> 270 ns for 44 calls), which is ~0.7% of a scan against the 4.5% these
// checks account for. Measure before assuming a decode per rune is free.
func BenchmarkNameShapes(b *testing.B) {
	keys := []string{
		"brave", "k_england", "00_traits", "namespace.1234", "add_gold",
		"any_trait_in_category", "scope:duel_target", "var:gold_amt",
		"culture_group_turkic", "b_appleby", "hegemon_favors_advancement_trigger",
	}
	var n int
	for b.Loop() {
		for _, k := range keys {
			if IsDefName(k) {
				n++
			}
			if IsEventID(k) {
				n++
			}
			if IsLowerName(k) {
				n++
			}
			if _, ok := ParsePrefixed(k); ok {
				n++
			}
		}
	}
	_ = n
}
