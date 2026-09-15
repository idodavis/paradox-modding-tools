// registry_test.go covers the per-game table and the rules that read it:
// folder-to-extract mapping, FIOS, key identity, and the EU5 entry-mode
// prefixes. Moved out of detect_test.go, which they did not test.

package game

import "testing"

func TestAll(t *testing.T) {
	got := All()
	if len(got) != 3 || got[0].ID != "ck3" || got[1].ID != "eu5" || got[2].ID != "vic3" {
		t.Fatalf("All ids: %v %v %v", got[0], got[1], got[2])
	}
	if Get("ck3") != got[0] || OriginVanilla != "vanilla" {
		t.Fatal("Get/OriginVanilla")
	}
	if Get("unknown") != nil {
		t.Error("Get(unknown) should be nil")
	}
}

func TestKeyIdentityStripsEntryModePrefix(t *testing.T) {
	cases := []struct {
		gameID, in, want string
	}{
		{"eu5", "INJECT:foo", "foo"},
		{"eu5", "REPLACE:bar", "bar"},
		{"vic3", "INJECT:ALD", "ALD"},
		{"", "INJECT:foo", "foo"},           // merge: game-agnostic strip
		{"ck3", "INJECT:foo", "INJECT:foo"}, // ck3 has no entry modes -> untouched
		{"eu5", "plain", "plain"},
		{"eu5", "\"quoted\"", "quoted"},
		{"", "  spaced  ", "spaced"},
	}
	for _, c := range cases {
		if got := KeyIdentity(c.gameID, c.in); got != c.want {
			t.Errorf("KeyIdentity(%q, %q) = %q want %q", c.gameID, c.in, got, c.want)
		}
	}
}

// An entry mode is a prefix the game spends on merge semantics, so it never
// names a database. Without this rejection EU5's `INJECT:foo` harvests a kind
// literally called "inject".
func TestParseTypedRejectsEntryModes(t *testing.T) {
	for _, c := range []struct{ gameID, text string }{
		{"eu5", "INJECT:foo"},
		{"eu5", "REPLACE_OR_CREATE:foo"},
		{"vic3", "REPLACE:foo"},
		{"", "INJECT:foo"},
	} {
		if kind, _, ok := ParseTyped(c.gameID, c.text); ok {
			t.Errorf("ParseTyped(%q, %q) = kind %q, want rejected", c.gameID, c.text, kind)
		}
	}
	// CK3 declares no entry modes, so the same prefix stays a plain cite there,
	// and an ordinary typed cite is untouched in every game.
	if kind, id, ok := ParseTyped("ck3", "culture:english"); !ok || kind != "culture" || id != "english" {
		t.Errorf("plain cite broke: kind=%q id=%q ok=%v", kind, id, ok)
	}
}

func TestMatchExtract(t *testing.T) {
	cases := []struct {
		gameID, path string
		kind         string
		mode         ExtractMode
	}{
		{"ck3", "events/my_events.txt", "event", ModeEventID},
		{"ck3", "common/traits/00_traits.txt", "traits", ModeTopLevelKey},
		{"ck3", "gui/window.gui", "gui_type", ModeGUIType},
		{"ck3", "localization/english/foo_l_english.yml", "loc_key", ModeLocKey},
		{"ck3", "descriptor.mod", "mod_descriptor", ModeTopLevelKey},
		{"ck3", "my_mod.mod", "mod_descriptor", ModeTopLevelKey},
		{"ck3", "common/religion/religion_types/00_islam.txt", "religion_types", ModeTopLevelKey},
		{"ck3", "common/on_action/activities/x.txt", "on_action", ModeTopLevelKey},
		{"eu5", "in_game/events/foo.txt", "event", ModeEventID},
		{"eu5", "in_game/common/religions/x.txt", "religions", ModeTopLevelKey},
	}
	for _, c := range cases {
		r := MatchExtract(c.gameID, c.path)
		if r.Kind != c.kind || r.Mode != c.mode {
			t.Errorf("MatchExtract(%q, %q) = %+v want {%s %s}", c.gameID, c.path, r, c.kind, c.mode)
		}
	}
}

func TestIsFIOS(t *testing.T) {
	cases := []struct {
		gameID, kind string
		want         bool
	}{
		{"ck3", "gui_type", true},
		{"ck3", "event", false},
		{"vic3", "gui_type", true},
		{"vic3", "event", true},
		{"eu5", "event", true},
		{"eu5", "gui_type", false},
		{"", "gui_type", true},
		{"", "event", false},
		{"unknown", "gui_type", true},
	}
	for _, c := range cases {
		if got := IsFIOS(c.gameID, c.kind); got != c.want {
			t.Errorf("IsFIOS(%q, %q) = %v want %v", c.gameID, c.kind, got, c.want)
		}
	}
}
