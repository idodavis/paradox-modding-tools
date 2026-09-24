// loc_test.go covers the loc dialect: entries, header/tab/unterminated
// errors, last-quote-wins values, byte-offset ranges, language extraction,
// and which strings look like keys; and learning a kind's loc-key templates and rendering them.

package loc

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("basic_entries", func(t *testing.T) {
		src := "l_english:\n key1:0 \"Hello\"\n key2: \"World\" # comment\n"
		result := Parse(src)
		if result.Language != "english" {
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
	})

	t.Run("bom", func(t *testing.T) {
		src := string(rune(0xFEFF)) + "l_english:\n k:0 \"Hi\"\n"
		result := Parse(src)
		if !result.HadBOM {
			t.Fatal("HadBOM")
		}
		if result.Language != "english" || len(result.Entries) != 1 {
			t.Fatalf("parsed %+v", result)
		}
		entry := result.Entries[0]
		if src[entry.KeyRange.Start:entry.KeyRange.End] != "k" ||
			src[entry.ValueRange.Start:entry.ValueRange.End] != "Hi" {
			t.Fatalf("BOM ranges key=%q val=%q",
				src[entry.KeyRange.Start:entry.KeyRange.End], src[entry.ValueRange.Start:entry.ValueRange.End])
		}
	})

	t.Run("interps", func(t *testing.T) {
		src := "l_english:\n a:0 \"see $used$ and $used|U$\"\n"
		entry := Parse(src).Entries[0]
		got := Interps(entry.Value, entry.ValueRange.Start)
		if len(got) != 2 || got[0].Key != "used" || got[1].Key != "used" {
			t.Fatalf("interps=%+v", got)
		}
		if src[got[0].KeyRange.Start:got[0].KeyRange.End] != "used" ||
			src[got[0].WrapRange.Start:got[0].WrapRange.End] != "$used$" ||
			src[got[1].WrapRange.Start:got[1].WrapRange.End] != "$used|U$" {
			t.Fatalf("interp ranges %+v", got)
		}
		if len(Interps("plain $notclosed", 0)) != 0 {
			t.Fatal("unterminated interpolations are not keys")
		}
	})

	t.Run("errors", func(t *testing.T) {
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
	})

	t.Run("language_from_filename", func(t *testing.T) {
		cases := map[string]string{
			"events_l_english.yml":   "english",
			"foo_l_french.yml":       "french",
			"bar_l_SimpChinese.yaml": "simpchinese",
			"x_l_simp_chinese.yml":   "simp_chinese",
			"x_l_braz_por.yml":       "braz_por",
			"tool_l_english.yml":     "english",
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
	})
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

func TestLearnAndExpand(t *testing.T) {
	keys := map[string]bool{}
	var traits, titles, rules []string
	for i := range 10 {
		text := string(rune('a' + i))
		traits = append(traits, "trait"+text)
		keys["trait"+text], keys["trait"+text+"_desc"] = true, true
		titles = append(titles, "k_"+text)
		keys["k_"+text], keys["k_"+text+"_adj"] = true, true
		rules = append(rules, "r"+text)
		keys["rule_r"+text] = true
	}
	idx := NewKeys(keys)
	for _, testCase := range []struct {
		kind         string
		ids, members []string
		want         []Template
	}{
		{"traits", traits, nil, []Template{"{id}", "{id}_desc"}},
		{"titles", titles, []string{"adj"}, []Template{"{id}", "{id}_adj"}},
		{"game_rules", rules, nil, []Template{"rule_{id}"}},
		// A shape one definition has is a coincidence, not a convention: it has to repeat.
		{"one def", traits[:1], nil, nil},
		{"two defs", traits[:2], nil, []Template{"{id}", "{id}_desc"}},
	} {
		got := Learn(testCase.ids, testCase.members, idx)
		var names []Template
		for _, template := range got {
			names = append(names, template.Template)
		}
		if !slices.Equal(names, testCase.want) {
			t.Errorf("%s: %v, want %v", testCase.kind, names, testCase.want)
		}
	}
	if got := Expand("bold", []Convention{{"{id}", 1}, {"{id}_desc", 1}}); !slices.Equal(got, []string{"bold", "bold_desc"}) {
		t.Errorf("Expand = %v", got)
	}
}
