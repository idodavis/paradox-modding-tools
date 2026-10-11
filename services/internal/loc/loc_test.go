// loc_test.go covers the loc dialect: entries, header/tab/unterminated
// errors, last-quote-wins values, byte-offset ranges, language extraction,
// and which strings look like keys.

package loc

import "testing"

// The words the fixtures repeat.
const (
	fixtureLanguage = "english" // the language of a header and of a file name
	fixtureKey      = "used"    // the key of an interpolation
)

// TestParseEntries pins the entries of a plain file: key, version, value, the last quote ending a value, and ranges.
func TestParseEntries(t *testing.T) {
	src := "l_english:\n key1:0 \"Hello\"\n key2: \"World\" # comment\n"
	result := Parse(src)
	if result.Language != fixtureLanguage {
		t.Fatalf("language = %q", result.Language)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("entries = %d %+v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].Key != "key1" || result.Entries[0].Version != 0 || result.Entries[0].Value != "Hello" {
		t.Fatalf("entry0 = %+v", result.Entries[0])
	}
	if result.Entries[1].Key != "key2" || result.Entries[1].Version != -1 || result.Entries[1].Value != "World" {
		t.Fatalf("entry1 = %+v", result.Entries[1])
	}

	// Inner quotes are literal; value runs to the LAST quote on the line.
	parsed := Parse("l_english:\n k: \"say \"\"hi\"\" now\"\n")
	if len(parsed.Entries) != 1 || parsed.Entries[0].Value != "say \"\"hi\"\" now" {
		t.Fatalf("last quote = %+v", parsed.Entries)
	}
	rng := "l_english:\n k: \"Hi\"\n"
	entry := Parse(rng).Entries[0]
	if rng[entry.ValueRange.Start:entry.ValueRange.End] != "Hi" ||
		rng[entry.KeyRange.Start:entry.KeyRange.End] != "k" {
		t.Fatalf("ranges key=%q val=%q",
			rng[entry.KeyRange.Start:entry.KeyRange.End],
			rng[entry.ValueRange.Start:entry.ValueRange.End])
	}
}

// TestParseBOM pins that a leading byte order mark is noted and the ranges stay in the original text.
func TestParseBOM(t *testing.T) {
	src := string(rune(0xFEFF)) + "l_english:\n k:0 \"Hi\"\n"
	result := Parse(src)
	if !result.HadBOM {
		t.Fatal("HadBOM")
	}
	if result.Language != fixtureLanguage || len(result.Entries) != 1 {
		t.Fatalf("parsed %+v", result)
	}
	entry := result.Entries[0]
	if src[entry.KeyRange.Start:entry.KeyRange.End] != "k" ||
		src[entry.ValueRange.Start:entry.ValueRange.End] != "Hi" {
		t.Fatalf("BOM ranges key=%q val=%q",
			src[entry.KeyRange.Start:entry.KeyRange.End], src[entry.ValueRange.Start:entry.ValueRange.End])
	}
}

// TestInterpolations pins the keys and ranges of the reuses inside a value, and that an unclosed one is none.
func TestInterpolations(t *testing.T) {
	src := "l_english:\n a:0 \"see $used$ and $used|U$\"\n"
	entry := Parse(src).Entries[0]
	got := Interpolations(entry.Value, entry.ValueRange.Start)
	if len(got) != 2 || got[0].Key != fixtureKey || got[1].Key != fixtureKey {
		t.Fatalf("interpolations=%+v", got)
	}
	if src[got[0].KeyRange.Start:got[0].KeyRange.End] != fixtureKey ||
		src[got[1].KeyRange.Start:got[1].KeyRange.End] != fixtureKey {
		t.Fatalf("interpolation ranges %+v", got)
	}
	if len(Interpolations("plain $notclosed", 0)) != 0 {
		t.Fatal("unterminated interpolations are not keys")
	}
}

// TestParseErrors pins the error each malformed file reports.
func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, src string
		code      ErrorCode
	}{
		{"no header", "key: \"value\"\n", ErrNoHeader},
		{"content before header", "stray content\nl_english:\n key: \"v\"\n", ErrContentBeforeHeader},
		{"tab indent", "l_english:\n\tkey: \"v\"\n", ErrTabIndent},
		{"unterminated value", "l_english:\n key: \"unterminated\n", ErrUnterminatedValue},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := Parse(testCase.src)
			found := false
			for _, issue := range result.Errors {
				if issue.Code == testCase.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s, got %+v", testCase.code, result.Errors)
			}
		})
	}
}

// TestLanguageFromName pins the language a file name gives, and the folder where the name gives none.
func TestLanguageFromName(t *testing.T) {
	cases := map[string]string{
		"events_l_english.yml":   fixtureLanguage,
		"foo_l_french.yml":       "french",
		"bar_l_SimpChinese.yaml": "simpchinese",
		"x_l_simp_chinese.yml":   "simp_chinese",
		"x_l_braz_por.yml":       "braz_por",
		"tool_l_english.yml":     fixtureLanguage,
		"descriptor.mod":         "",
		"noconvention.yml":       "",
	}
	for name, want := range cases {
		if got := LanguageFromFilename(name); got != want {
			t.Errorf("LanguageFromFilename(%q) = %q want %q", name, got, want)
		}
	}
	if got := LanguageFromRel("localization/german/notes.yml"); got != "german" {
		t.Errorf("a rel's folder names the language where the filename does not: %q", got)
	}
	if got := LanguageFromRel("localization/languages.yml"); got != "" {
		t.Errorf("a file straight under localization is in no language: %q", got)
	}
}

func TestLooksLikeKey(t *testing.T) {
	for _, testCase := range []struct {
		text string
		want bool
	}{
		{"yes", false},
		{"NONE", false},
		{"evt.1.t", true},
		{"scope:child", false},
		{"primary_title", true},

		// Quoting proves nothing: the games quote real keys and display text in the same properties.
		{"HRE_CONQUEST_WAR_NAME", true},
		{"Always make coronations!", false},
		{"The Red Keep", false},
		{"Wrong culture", false},
		{"Base test value", false},
		{"trailing ", false},
		{"tab\there", false},

		// A key completed at call time (`desc = $TT$`) is never demanded.
		{"$TT$", false},
		{"$title$", false},
		{"evt_$TYPE$_desc", false},
		{"$COA$", false},
		{"my_event.0001.desc", true},
	} {
		if got := LooksLikeKey(testCase.text); got != testCase.want {
			t.Errorf("LooksLikeKey(%q) = %v, want %v", testCase.text, got, testCase.want)
		}
	}
}
