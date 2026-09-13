// parse_test.go covers the loc dialect: entries, header/tab/unterminated errors,
// last-quote-wins values, and byte-offset ranges.

package loc

import (
	"testing"
)

func TestParseBasicEntries(t *testing.T) {
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
}

func TestParseBOM(t *testing.T) {
	src := "\ufeffl_english:\n k:0 \"Hi\"\n"
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
}

func TestInterps(t *testing.T) {
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
}

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
}

// covers language-id extraction from loc filenames.
func TestLanguageFromFilename(t *testing.T) {
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
}

// covers the $KEY$ engine-substitution rule.
func TestIsLocEngineValue(t *testing.T) {
	t.Parallel()
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
}

// covers STRICT/BROAD/none classification of loc properties.
func TestClassify(t *testing.T) {
	cases := map[string]Property{
		"title":             PropStrict,
		"desc":              PropStrict,
		"first":             PropStrict,
		"third":             PropStrict,
		"global":            PropStrict,
		"first_not":         PropStrict,
		"first_past":        PropStrict,
		"global_past_neg":   PropStrict,
		"localization_key":  PropBroad, // see TestLocalizationKeyIsBroadNotStrict
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
	if LooksLikeKey("yes") || LooksLikeKey("NONE") || !LooksLikeKey("evt.1.t") {
		t.Errorf("LooksLikeKey yes/none/evt.1.t")
	}
	if LooksLikeKey("scope:child") {
		t.Errorf("LooksLikeKey(scope:child) = true, want false")
	}
	if !LooksLikeKey("primary_title") {
		t.Errorf("LooksLikeKey(primary_title) = false, want true (key shape)")
	}
}

// Quoting proves nothing: Paradox quotes real keys and also writes display text
// into the same properties. The space is what separates them — a localization
// file is `key:0 "value"`, so a key is a single token.
func TestLooksLikeKeyRejectsProse(t *testing.T) {
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
}

// A property is strict only if an unresolved value is genuinely a defect.
// `localization_key` is not: customizable localization completes the stem at
// runtime, so `localization_key = CustomLoc_BR_male_` names a key that does not
// and should not exist. Measured over vanilla — where nothing is missing by
// definition — it failed to resolve 2,607 times on CK3, 11,597 on Victoria 3
// and 46,481 on EU5.
func TestLocalizationKeyIsBroadNotStrict(t *testing.T) {
	if got := Classify("localization_key"); got != PropBroad {
		t.Errorf("Classify(localization_key) = %v, want %v", got, PropBroad)
	}
	// The properties that really do demand a key keep demanding one.
	for _, p := range []string{"desc", "title", "custom_tooltip", "war_name"} {
		if got := Classify(p); got != PropStrict {
			t.Errorf("Classify(%s) = %v, want %v", p, got, PropStrict)
		}
	}
}

// TestLooksLikeKeyMacroParams pins that a key completed at call time is never
// demanded. `desc = $TT$` and `desc = $title$` both reached Workspace Health as
// missing localization on real Victoria 3 mods.
func TestLooksLikeKeyMacroParams(t *testing.T) {
	for _, s := range []string{"$TT$", "$title$", "evt_$TYPE$_desc", "$COA$"} {
		if LooksLikeKey(s) {
			t.Errorf("LooksLikeKey(%q) = true, want false", s)
		}
	}
	// Still a key when nothing is substituted.
	for _, s := range []string{"HRE_CONQUEST_WAR_NAME", "my_event.0001.desc"} {
		if !LooksLikeKey(s) {
			t.Errorf("LooksLikeKey(%q) = false, want true", s)
		}
	}
}
