// nameshapes_test.go pins the hand-written name checks against the regular
// expressions they replaced. They are tested on every key of every file and a
// scan walks the install four times, so they were the hottest regexes in the
// profile -- but a silent difference would change what the whole model harvests.

package jomini

import (
	"regexp"
	"testing"
)

func TestNameShapesMatchPatterns(t *testing.T) {
	// The originals, verbatim.
	var (
		defNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
		eventIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.\d+$`)
		nameOKRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	)
	// Real shapes from the three installs, plus the edges that decide the
	// boundaries: empty, leading punctuation, trailing dot, unicode, spaces.
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
	}
	for _, s := range inputs {
		t.Run("q_"+s, func(t *testing.T) {
			if got, want := IsDefName(s), defNameRe.MatchString(s); got != want {
				t.Errorf("IsDefName(%q) = %v, regexp says %v", s, got, want)
			}
			if got, want := IsEventID(s), eventIDRe.MatchString(s); got != want {
				t.Errorf("IsEventID(%q) = %v, regexp says %v", s, got, want)
			}
			if got, want := IsLowerName(s), nameOKRe.MatchString(s); got != want {
				t.Errorf("IsLowerName(%q) = %v, regexp says %v", s, got, want)
			}
		})
	}
}

// FuzzNameShapes is the same equivalence, over inputs nobody thought of.
func FuzzNameShapes(f *testing.F) {
	defNameRe := regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	eventIDRe := regexp.MustCompile(`^[A-Za-z0-9_-]+\.\d+$`)
	nameOKRe := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	for _, s := range []string{"", "a", "birth.1", "k_england", "a-b.12", "\x00", "é"} {
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
