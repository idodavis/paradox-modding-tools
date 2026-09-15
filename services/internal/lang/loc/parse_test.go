// parse_test.go covers the loc dialect: entries, header/tab/unterminated
// errors, last-quote-wins values, byte-offset ranges, language extraction,
// and STRICT/BROAD/none property classification.

package loc

import (
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("basic_entries", func(t *testing.T) {
		src := "l_english:\n key1:0 \"Hello\"\n key2: \"World\" # comment\n"
		r := Parse(src)
		if r.Language != "english" {
			t.Fatalf("language = %q", r.Language)
		}
		if len(r.Entries) != 2 {
			t.Fatalf("entries = %d %+v", len(r.Entries), r.Entries)
		}
		if r.Entries[0].Key != "key1" || r.Entries[0].Version != 0 || r.Entries[0].Value != "Hello" {
			t.Fatalf("entry0 = %+v", r.Entries[0])
		}
		if r.Entries[1].Key != "key2" || r.Entries[1].Version != -1 || r.Entries[1].Value != "World" {
			t.Fatalf("entry1 = %+v", r.Entries[1])
		}

		// Inner quotes are literal; value runs to the LAST quote on the line.
		q := Parse("l_english:\n k: \"say \"\"hi\"\" now\"\n")
		if len(q.Entries) != 1 || q.Entries[0].Value != "say \"\"hi\"\" now" {
			t.Fatalf("last quote = %+v", q.Entries)
		}
		rng := "l_english:\n k: \"Hi\"\n"
		e := Parse(rng).Entries[0]
		if rng[e.ValueRange.Start:e.ValueRange.End] != "Hi" ||
			rng[e.KeyRange.Start:e.KeyRange.End] != "k" {
			t.Fatalf("ranges key=%q val=%q",
				rng[e.KeyRange.Start:e.KeyRange.End],
				rng[e.ValueRange.Start:e.ValueRange.End])
		}
	})

	t.Run("bom", func(t *testing.T) {
		src := string(rune(0xFEFF)) + "l_english:\n k:0 \"Hi\"\n"
		r := Parse(src)
		if !r.HadBOM {
			t.Fatal("HadBOM")
		}
		if r.Language != "english" || len(r.Entries) != 1 {
			t.Fatalf("parsed %+v", r)
		}
		e := r.Entries[0]
		if src[e.KeyRange.Start:e.KeyRange.End] != "k" ||
			src[e.ValueRange.Start:e.ValueRange.End] != "Hi" {
			t.Fatalf("BOM ranges key=%q val=%q",
				src[e.KeyRange.Start:e.KeyRange.End], src[e.ValueRange.Start:e.ValueRange.End])
		}
	})

	t.Run("interps", func(t *testing.T) {
		src := "l_english:\n a:0 \"see $used$ and $used|U$\"\n"
		e := Parse(src).Entries[0]
		got := Interps(e.Value, e.ValueRange.Start)
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
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := Parse(tt.src)
				found := false
				for _, e := range r.Errors {
					if e.Code == tt.code {
						found = true
					}
				}
				if !found {
					t.Fatalf("want %s, got %+v", tt.code, r.Errors)
				}
			})
		}
	})
}

func TestClassifyAndLooksLikeKey(t *testing.T) {
	t.Run("language_from_filename", func(t *testing.T) {
		cases := map[string]string{
			"events_l_english.yml":   "english",
			"foo_l_french.yml":       "french",
			"bar_l_SimpChinese.yaml": "simpchinese",
			"descriptor.mod":         "",
			"noconvention.yml":       "",
		}
		for name, want := range cases {
			if got := LanguageFromFilename(name); got != want {
				t.Errorf("LanguageFromFilename(%q) = %q want %q", name, got, want)
			}
		}
		if got := LanguageOf("events.yml", "english"); got != "english" {
			t.Errorf("LanguageOf header fallback = %q", got)
		}
		if got := LanguageOf("foo_l_french.yml", "english"); got != "french" {
			t.Errorf("LanguageOf filename wins = %q", got)
		}
	})

	// The $KEY$ engine-substitution rule.
	t.Run("is_loc_engine_value", func(t *testing.T) {
		if !IsLocEngineValue("ORDER", "") {
			t.Fatal("$ORDER$ is engine data")
		}
		if !IsLocEngineValue("VALUE", "=+0") {
			t.Fatal("$VALUE|=+0$ is engine data")
		}
		if !IsLocEngineValue("value", "=+0") {
			t.Fatal("$value|=+0$ is engine data")
		}
		if IsLocEngineValue("used_key", "") {
			t.Fatal("$used_key$ is loc reuse")
		}
		if IsLocEngineValue("INDEPENDENCE_WAR_NAME", "") {
			t.Fatal("$INDEPENDENCE_WAR_NAME$ is loc reuse")
		}
		if IsLocEngineValue("used", "U") {
			t.Fatal("$used|U$ is loc reuse")
		}
	})

	t.Run("classify", func(t *testing.T) {
		cases := map[string]Property{
			"title":             PropStrict,
			"desc":              PropStrict,
			"first":             PropStrict,
			"third":             PropStrict,
			"global":            PropStrict,
			"first_not":         PropStrict,
			"first_past":        PropStrict,
			"global_past_neg":   PropStrict,
			"localization_key":  PropBroad, // customizable loc completes the stem at runtime
			"selection_tooltip": PropStrict,
			"war_name":          PropStrict,
			"cb_name":           PropStrict,
			"notification_text": PropStrict,
			"notification":      PropBroad,
			"TITLE":             PropNone, // trigger/effect arg, not event loc
			"DESC":              PropNone,
			"name":              PropBroad,
			"tooltip":           PropBroad,
			"id":                PropNone,
			"color":             PropNone,
		}
		for prop, want := range cases {
			if got := Classify(prop); got != want {
				t.Errorf("Classify(%q) = %q want %q", prop, got, want)
			}
		}
		// localization_key is BROAD, not STRICT; the properties that really demand a key stay strict.
		for _, p := range []string{"desc", "title", "custom_tooltip", "war_name"} {
			if got := Classify(p); got != PropStrict {
				t.Errorf("Classify(%s) = %v, want %v", p, got, PropStrict)
			}
		}
		if LooksLikeKey("yes") || LooksLikeKey("NONE") || !LooksLikeKey("evt.1.t") {
			t.Errorf("LooksLikeKey yes/none/evt.1.t")
		}
		if LooksLikeKey("scope:child") {
			t.Errorf("LooksLikeKey(scope:child) = true, want false")
		}
		if !LooksLikeKey("primary_title") {
			t.Errorf("LooksLikeKey(primary_title) = false, want true (key shape)")
		}
	})

	// Quoting proves nothing: Paradox quotes real keys and also writes display text into the same properties.
	t.Run("looks_like_key_rejects_prose", func(t *testing.T) {
		cases := []struct {
			in   string
			want bool
		}{
			{"HRE_CONQUEST_WAR_NAME", true},
			{"evt.1.t", true},
			{"Always make coronations!", false},
			{"The Red Keep", false},
			{"Wrong culture", false},
			{"Base test value", false},
			{"trailing ", false},
			{"tab\there", false},
		}
		for _, tc := range cases {
			if got := LooksLikeKey(tc.in); got != tc.want {
				t.Errorf("LooksLikeKey(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	})

	// A key completed at call time (`desc = $TT$`) is never demanded.
	t.Run("looks_like_key_macro_params", func(t *testing.T) {
		for _, s := range []string{"$TT$", "$title$", "evt_$TYPE$_desc", "$COA$"} {
			if LooksLikeKey(s) {
				t.Errorf("LooksLikeKey(%q) = true, want false", s)
			}
		}
		for _, s := range []string{"HRE_CONQUEST_WAR_NAME", "my_event.0001.desc"} {
			if !LooksLikeKey(s) {
				t.Errorf("LooksLikeKey(%q) = false, want true", s)
			}
		}
	})
}
