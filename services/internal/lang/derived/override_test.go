// override_test.go is the load-order table: FIOS/LIOS per kind, vanilla always
// losing, and the contest rows the Health view reads.

package derived

import "testing"

func TestWinner(t *testing.T) {
	order := OrderMap([]string{"a", "b", "c"})
	for _, tt := range []struct {
		name  string
		gameo string
		defs  []Def
		want  string
	}{
		{"nothing", "ck3", nil, ""},
		{"vanilla alone", "ck3", []Def{{Kind: "traits", Key: "k"}}, ""},
		{"a mod beats vanilla", "ck3",
			[]Def{{Kind: "traits", Key: "k"}, {Kind: "traits", Key: "k", Origin: "a"}}, "a"},
		{"LIOS: the last mod wins", "ck3", []Def{
			{Kind: "traits", Key: "k", Origin: "a"},
			{Kind: "traits", Key: "k", Origin: "c"},
			{Kind: "traits", Key: "k", Origin: "b"},
		}, "c"},
		{"FIOS: the first mod wins", "ck3", []Def{
			{Kind: "gui_type", Key: "k", Origin: "c"},
			{Kind: "gui_type", Key: "k", Origin: "a"},
		}, "a"},
		{"two kinds reduce under their own rule", "ck3", []Def{
			{Kind: "gui_type", Key: "k", Origin: "a"},
			{Kind: "traits", Key: "k", Origin: "b"},
		}, "b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Winner(tt.gameo, tt.defs, order)
			if tt.want == "" {
				if got != nil && got.Origin != "" {
					t.Fatalf("winner = %+v, want vanilla or nothing", got)
				}
				return
			}
			if got == nil || got.Origin != tt.want {
				t.Fatalf("winner = %+v, want origin %q", got, tt.want)
			}
		})
	}
}

func TestContests(t *testing.T) {
	work := []Def{
		{Kind: "traits", Key: "brave", Origin: "a"},
		{Kind: "traits", Key: "brave", Origin: "b"},
		{Kind: "traits", Key: "bold", Origin: "a"},
		{Kind: "loc_key", Key: "brave_desc", Origin: "a"},
		{Kind: "traits", Key: "solo", Origin: "a"},
	}
	vanilla := []Def{{Kind: "traits", Key: "bold"}, {Kind: "traits", Key: "brave"}}
	rows := Contests("ck3", work, vanilla, []string{"a", "b"})
	got := map[string]Contest{}
	for _, r := range rows {
		got[r.Name] = r
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if r := got["brave"]; r.ModCount != 2 || r.Winner != "b" || r.Rule != "LIOS" || r.Overlay {
		t.Errorf("brave = %+v", r)
	}
	if r := got["bold"]; !r.Overlay || r.ModCount != 1 {
		t.Errorf("bold = %+v", r)
	}
	if _, ok := got["brave_desc"]; ok {
		t.Error("localization is not an override contest")
	}
	if _, ok := got["solo"]; ok {
		t.Error("a key only one mod defines is not contested")
	}
}
