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

func TestIsFIOS(t *testing.T) {
	cases := []struct {
		gameID, kind string
		want         bool
	}{
		{"ck3", "gui.type", true},
		{"ck3", "events", false},
		{"vic3", "gui.type", true},
		{"vic3", "events", true},
		{"eu5", "events", true},
		{"eu5", "gui.type", false},
		{"", "gui.type", true},
		{"", "events", false},
		{"unknown", "gui.type", true},
	}
	for _, c := range cases {
		if got := IsFIOS(c.gameID, c.kind); got != c.want {
			t.Errorf("IsFIOS(%q, %q) = %v want %v", c.gameID, c.kind, got, c.want)
		}
	}
}
