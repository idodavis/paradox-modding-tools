// health_test.go covers the four passes of the report over two mods: contests and dependencies in a chosen order, dangling references and what must never be one, each language's missing, untranslated and orphaned keys, the files a definition lacks, the caps, and the lookup of one key.
package views

import (
	"slices"
	"testing"
)

func TestWorkspaceHealth(t *testing.T) {
	s := workspace(t,
		map[string]string{
			"common/traits/a.txt":                  "brave = { name = x }\nshared_a = { name = x }\nbold = { name = x }\n",
			"events/a.txt":                         "namespace = a\na.1 = {\n\ttype = character_event\n\ttitle = a.1.t\n\ttrigger = {\n\t\thas_trait = brave\n\t\thas_trait = bold_b\n\t\texists = title:k_nowhere\n\t\texists = title:k_x\n\t\tfaith = faith:catholic\n\t\texists = scope:whatever\n\t}\n\timmediate = { add_gold = small_value }\n}\n",
			"localization/english/a_l_english.yml": "\xEF\xBB\xBFl_english:\n a.1.t:0 \"Hi $VALUE|=+0$\"\n a_dead:0 \"Nothing reads me\"\n a_reader:0 \"$a_read$\"\n a_read:0 \"Read by a key\"\n bold:0 \"Bold\"\n",
			"localization/french/a_l_french.yml":   "\xEF\xBB\xBFl_french:\n a.1.t:0 \"Salut\"\n",
		},
		map[string]string{
			"common/traits/b.txt":                  "brave = { name = x }\nshared_a = { name = x }\nbold_b = { name = x }\n",
			"localization/english/b_l_english.yml": "\xEF\xBB\xBFl_english:\n b_only:0 \"B\"\n",
		})
	h := WorkspaceHealth(s, HealthParams{})

	// contests: brave is defined by the install and both mods, the last wins under LIOS; a reversed order changes the winner
	brave := slices.IndexFunc(h.Contests, func(c Contest) bool { return c.Name == "brave" })
	if brave < 0 || h.Contests[brave].Rule != "LIOS" || h.Contests[brave].Found[h.Contests[brave].Winner].At.Origin != "modb" || len(h.Contests[brave].Found) != 3 {
		t.Fatalf("brave contested by three, won by the last: %+v", h.Contests)
	}
	reversed := WorkspaceHealth(s, HealthParams{Order: []string{"modb", "moda"}})
	if at := slices.IndexFunc(reversed.Contests, func(c Contest) bool { return c.Name == "brave" }); at < 0 || reversed.Contests[at].Found[reversed.Contests[at].Winner].At.Origin != "moda" {
		t.Errorf("the reversed order: %+v", reversed.Contests)
	}
	if !slices.ContainsFunc(h.Depends, func(d Dependency) bool { return d.From == "moda" && d.To == "modb" }) {
		t.Errorf("mod a names bold_b, which only mod b defines: %+v", h.Depends)
	}

	// dangling: a declared cite nothing defines, grouped with its sites; what resolves, and what nothing claims, never
	if len(h.Dangling) != 1 || h.Dangling[0].Name != "k_nowhere" || !h.Dangling[0].Declared || h.Dangling[0].Count != 1 || len(h.Dangling[0].Sites) != 1 || h.DanglingCount != 1 {
		t.Errorf("one dangling reference: %+v", h.Dangling)
	}

	// localization: both languages; the definition's conventional key; untranslated; orphaned within the mod
	langs := map[string]Language{}
	for _, l := range h.Loc {
		langs[l.Language] = l
	}
	if len(langs) != 2 {
		t.Fatalf("both languages: %+v", h.Loc)
	}
	keys := func(rows []LocRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Key)
		}
		slices.Sort(out)
		return out
	}
	if got := keys(langs["english"].Missing); !slices.Equal(got, []string{"bold_b", "shared_a", "shared_a"}) {
		t.Errorf("english: a trait's own key is what every vanilla trait has; bold and brave have theirs; a name two mods define is two rows: %v", got)
	}
	if got := keys(langs["french"].Missing); !slices.Contains(got, "bold") || !slices.Contains(got, "shared_a") {
		t.Errorf("french: the keys english has are missing too: %v", got)
	}
	if got := keys(langs["french"].Untranslated); !slices.Contains(got, "bold") || slices.Contains(got, "a.1.t") {
		t.Errorf("french: an english key with no french entry is untranslated; one with an entry is not: %v", got)
	}
	if got := keys(langs["english"].Orphaned); !slices.Equal(got, []string{"a_dead", "a_reader", "b_only"}) {
		t.Errorf("orphaned: a key nothing reads, one that only reads; not one a definition names, one script reads, or one another key reads: %v", got)
	}
	if langs["english"].OrphanedCount != 3 || langs["english"].MissingCount != 3 {
		t.Errorf("the counts are whole: %+v", langs["english"])
	}

	// files: the fixture's icon template covers 5 of 8 vanilla traits, so a trait without one is no finding (§6a)
	if h.FilesCount != 0 || len(h.Files) != 0 {
		t.Errorf("a template short of full coverage accuses nothing: %+v", h.Files)
	}

	// the page's cap cuts rows, not counts
	capped := WorkspaceHealth(s, HealthParams{Limit: 1})
	for _, l := range capped.Loc {
		if l.Language == "english" && (len(l.Orphaned) != 1 || l.OrphanedCount != 3) {
			t.Errorf("capped rows, whole count: %+v", l)
		}
	}

	// looking one key up: its entries in every language, and its readers
	look := LookupLoc(s, "a.1.t")
	if len(look.Entries) != 2 || len(look.Readers) != 1 || look.Readers[0].At.Origin != "moda" {
		t.Errorf("a key's entries and readers: %+v", look)
	}
	if look := LookupLoc(s, "v.1.t"); len(look.Entries) != 1 || look.Entries[0].At.Origin != "vanilla" {
		t.Errorf("an install key: %+v", look)
	}
}
