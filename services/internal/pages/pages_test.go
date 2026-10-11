// pages_test.go covers the three pages over the ck3 fixture install with the mods a test writes: the Health report,
// the event graph and its detail, and the searches.

package pages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/workspace"
)

// eventsGlob keeps what is under an events folder, searchToken is what the fixture holds in most files, and modA and
// modB are the ids openWorkspace gives its first two mods.
const (
	eventsGlob  = "**/events/**"
	searchToken = "unique_search_token"
	modA        = "moda"
	modB        = "modb"
)

// writeTree writes each text at its slash-separated rel under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// searchTree is the Find-in-Files fixture: one token across folders and extensions, a .git file, a
// NUL file, and the word and regex lines.
var searchTree = map[string]string{
	"events/hit.txt":          "title = unique_search_token\n",
	"events/hit.yml":          "unique_search_token yml\n",
	"gfx/skip.dds":            "unique_search_token in a dds\n",
	"common/other.txt":        "unique_search_token common\n",
	".git/objects/secret.txt": "hidden_token\n",
	"visible.txt":             "hidden_token visible\n",
	"binary.dat":              "findme\x00tail",
	"ok.txt":                  "findme ok\n",
	"a.txt":                   "TOK only\npartialtoken\nanother TOK\n",
}

func TestTextSearch(t *testing.T) {
	t.Parallel()
	root, other := t.TempDir(), t.TempDir()
	writeTree(t, root, searchTree)
	writeTree(t, other, searchTree)
	whole := []TextSearchFolder{{Path: root}}
	for _, row := range []struct {
		name  string
		query TextSearchQuery
		hits  int
		fails bool
	}{
		{
			name:  "an include glob limits hits to events/",
			query: TextSearchQuery{Pattern: searchToken, Folders: []TextSearchFolder{{Path: root, Includes: []string{eventsGlob}}}},
			hits:  2,
		},
		{
			name:  "an exclude glob drops .txt",
			query: TextSearchQuery{Pattern: searchToken, Folders: []TextSearchFolder{{Path: root, Excludes: []string{"*.txt"}}}},
			hits:  1,
		},
		{
			name: "each folder's globs apply only to it",
			query: TextSearchQuery{Pattern: searchToken, Folders: []TextSearchFolder{
				{Path: root, Includes: []string{eventsGlob}},
				{Path: other, Includes: []string{"**/common/**"}},
			}},
			hits: 3,
		},
		{
			name:  ".git is not searched",
			query: TextSearchQuery{Pattern: "hidden_token", Folders: whole},
			hits:  1,
		},
		{
			name:  "a file with a NUL byte is skipped",
			query: TextSearchQuery{Pattern: "findme", Folders: whole},
			hits:  1,
		},
		{name: "case-sensitive word", query: TextSearchQuery{Pattern: "tok", IsCaseSensitive: true, IsWordMatch: true, Folders: whole}, hits: 0},
		{name: "case-insensitive word", query: TextSearchQuery{Pattern: "tok", IsWordMatch: true, Folders: whole}, hits: 2},
		{name: "regex", query: TextSearchQuery{Pattern: `tok.+only`, IsRegexp: true, Folders: whole}, hits: 1},
		{name: "an invalid regex is an error", query: TextSearchQuery{Pattern: "[", IsRegexp: true, Folders: whole}, fails: true},
	} {
		result, err := TextSearch(t.Context(), row.query, func(TextSearchHits) {})
		if (err != nil) != row.fails {
			t.Errorf("%s: error %v, want one %v", row.name, err, row.fails)
		}
		if err != nil || row.fails {
			continue
		}
		if result.Found != row.hits {
			t.Errorf("%s: %d found, want %d", row.name, result.Found, row.hits)
		}
	}
}

func TestFileSearchNeedleAndGlob(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, searchTree)
	res := FileSearch(t.Context(), FileSearchQuery{
		FilePattern: "hit",
		Folders: []TextSearchFolder{{
			Path:     root,
			Includes: []string{eventsGlob},
		}},
	})
	if len(res.Hits) != 2 {
		t.Fatalf("file hits = %+v", res.Hits)
	}
	for _, hit := range res.Hits {
		if !strings.Contains(filepath.ToSlash(hit.Path), "/events/") {
			t.Fatalf("leaked %s", hit.Path)
		}
	}
}

func TestTextSearchCancel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{}
	for i := range 40 {
		files[fmt.Sprintf("dir/%s/f%d.txt", strings.Repeat("a", i%5+1), i)] = strings.Repeat("needle\n", 200)
	}
	writeTree(t, root, files)
	query := TextSearchQuery{
		Pattern: "needle",
		Folders: []TextSearchFolder{{Path: root}},
	}

	// the full result, so the canceled run has something real to be smaller than
	full, err := TextSearch(t.Context(), query, func(TextSearchHits) {})
	if err != nil {
		t.Fatalf("uncanceled search: %v", err)
	}
	if full.Found == 0 {
		t.Fatal("fixture produced no hits; the test would prove nothing")
	}

	// canceled before the call, not on a timer: on a fast machine the search would win the race
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, err := TextSearch(ctx, query, func(TextSearchHits) {})
	if err != nil {
		t.Fatalf("cancellation must not surface as an error: %v", err)
	}
	if res.Found >= full.Found {
		t.Fatalf("cancel did not stop the search: got %d hits, full run is %d", res.Found, full.Found)
	}
}

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("..", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test"})
})

// openWorkspace is a session over the fixture install with the given mods, each a map of rel to text, written to
// a temporary folder.
func openWorkspace(t *testing.T, mods ...map[string]string) *workspace.Session {
	t.Helper()
	install, err := scanned()
	if err != nil {
		t.Fatal(err)
	}
	var playset []workspace.Mod
	for i, files := range mods {
		dir := t.TempDir()
		writeTree(t, dir, files)
		playset = append(playset, workspace.Mod{ID: "mod" + string(rune('a'+i)), Dir: dir})
	}
	ws, err := workspace.Open(t.Context(), game.Get("ck3"), install, playset, "")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// healthWorkspace is a session over the fixture install with two mods: mod a defines and uses names, ships english
// and french localization, and writes references that resolve, depend on mod b and dangle; mod b redefines some.
func healthWorkspace(t *testing.T) *workspace.Session {
	t.Helper()
	return openWorkspace(t,
		map[string]string{
			"common/traits/zz_a.txt":               "brave = { name = x }\nshared_a = { name = x }\nbold = { name = x }\ncraven = { name = x }\n",
			"events/a.txt":                         "namespace = a\na.1 = {\n\ttype = character_event\n\ttitle = a.1.t\n\ttrigger = {\n\t\thas_trait = brave\n\t\thas_trait = bold_b\n\t\texists = title:k_nowhere\n\t\texists = title:k_x\n\t\tfaith = faith:catholic\n\t\texists = scope:whatever\n\t}\n\timmediate = { add_gold = small_value }\n}\n",
			"localization/english/a_l_english.yml": "\xEF\xBB\xBFl_english:\n a.1.t:0 \"Hi $VALUE|=+0$\"\n a_dead:0 \"Nothing reads me\"\n a_reader:0 \"$a_read$\"\n a_read:0 \"Read by a key\"\n bold:0 \"Bold\"\n bold_extra:0 \"More\"\n",
			"localization/french/a_l_french.yml":   "\xEF\xBB\xBFl_french:\n a.1.t:0 \"Salut\"\n",
		},
		map[string]string{
			"common/traits/zz_b.txt":               "brave = {\n" + strings.Repeat("\tname = x\n", 11) + "}\nshared_a = { name = x }\nbold_b = { name = x }\n",
			"localization/english/b_l_english.yml": "\xEF\xBB\xBFl_english:\n b_only:0 \"B\"\n",
		})
}

// healthOf is the report of a session in a load order, its findings made as the service makes them.
func healthOf(ws *workspace.Session, params HealthParams) *HealthReport {
	return Health(ws, params, (&Findings{}).Current(ws))
}

// linesAt is the source each site shows, a definition's statement or a reference's line.
func linesAt(ws *workspace.Session, health *HealthReport, sites []Site, definition bool) [][]string {
	var places []SourceAt
	for _, site := range sites {
		places = append(places, SourceAt{Path: health.Sources[site.File].Path, Line: site.Line, Col: site.Col, Definition: definition})
	}
	return Lines(ws, places)
}

// contestNamed is the test of a contest by its name.
func contestNamed(name string) func(Contest) bool {
	return func(contest Contest) bool { return contest.Name == name }
}

// TestHealthContests covers a name three origins define, won by the last file loaded, the load order a page may
// ask for, the difference between an override and a contest, and a site's lines cut at ten.
func TestHealthContests(t *testing.T) {
	ws := healthWorkspace(t)
	health := healthOf(ws, HealthParams{})

	// contests: brave is defined three times, the last file loaded wins under LIOS; files load by
	// path, so reversing the mods' order leaves the winner of two differently named files alone
	brave := slices.IndexFunc(health.Contests, contestNamed("brave"))
	origin := func(report *HealthReport, site Site) string { return report.Sources[site.File].Origin }
	if brave < 0 || health.Contests[brave].Rule != "LIOS" || origin(health, health.Contests[brave].Sites[health.Contests[brave].Winner]) != modB || len(health.Contests[brave].Sites) != 3 {
		t.Fatalf("brave contested by three, won by the last: %+v", health.Contests)
	}
	reversed := healthOf(ws, HealthParams{Order: []string{modB, modA}})
	if i := slices.IndexFunc(reversed.Contests, contestNamed("brave")); i < 0 || origin(reversed, reversed.Contests[i].Sites[reversed.Contests[i].Winner]) != modB {
		t.Errorf("the reversed order: %+v", reversed.Contests)
	}

	// one mod over the install's name is an override; two mods on a name, with or without the
	// install's, are a contest
	if slices.ContainsFunc(health.Contests, contestNamed("craven")) || !slices.ContainsFunc(health.Overrides, contestNamed("craven")) {
		t.Errorf("mod a's craven is an override: %+v %+v", health.Contests, health.Overrides)
	}
	if !slices.ContainsFunc(health.Contests, contestNamed("shared_a")) {
		t.Errorf("two mods on a name the install lacks: %+v", health.Contests)
	}

	// a site's lines, read when its row opens: a definition's short block whole, a long one cut at
	// ten lines and a "…"
	braves := linesAt(ws, health, health.Contests[brave].Sites, true)
	if !slices.Equal(braves[0], []string{"brave = {", "\tcategory = personality", "\tdiplomacy = 0", "\ticon = \"gfx/interface/icons/traits/brave.dds\"", "}"}) {
		t.Errorf("the install's brave, whole: %q", braves[0])
	}
	if cut := braves[2]; len(cut) != 11 || cut[0] != "brave = {" || cut[10] != "…" {
		t.Errorf("mod b's brave, cut: %q", cut)
	}
}

// TestHealthReferences covers what a mod uses of another's, with each use's kind, name and line, and the names
// nothing defines, never what resolves or what no check claims.
func TestHealthReferences(t *testing.T) {
	ws := healthWorkspace(t)
	health := healthOf(ws, HealthParams{})

	if i := slices.IndexFunc(health.Depends, func(dep Dependency) bool { return dep.From == modA && dep.To == modB }); i < 0 {
		t.Errorf("mod a names bold_b, which only mod b defines: %+v", health.Depends)
	} else if uses := health.Depends[i].Sites; len(uses) != 1 || uses[0].Database != "traits" || uses[0].Name != "bold_b" || !slices.Equal(linesAt(ws, health, uses, false)[0], []string{"\t\thas_trait = bold_b"}) {
		t.Errorf("each use, with its kind, name and line: %+v", uses)
	}

	// dangling: a declared cite nothing defines, with its sites; never what resolves or is unclaimed
	if len(health.Dangling) != 1 || health.Dangling[0].Name != "k_nowhere" || len(health.Dangling[0].Sites) != 1 {
		t.Fatalf("one dangling reference: %+v", health.Dangling)
	}
	if got := linesAt(ws, health, health.Dangling[0].Sites, false)[0]; !slices.Equal(got, []string{"\t\texists = title:k_nowhere"}) {
		t.Errorf("a dangling site's line: %q", got)
	}
}

// TestHealthLocalization covers each language's untranslated keys, the file a row names by its number in Sources,
// and what reads one key.
func TestHealthLocalization(t *testing.T) {
	ws := healthWorkspace(t)
	health := healthOf(ws, HealthParams{})

	langs := map[string]Language{}
	for _, loc := range health.Loc {
		langs[loc.Language] = loc
	}
	if len(langs) != 2 {
		t.Fatalf("both languages: %+v", health.Loc)
	}
	var keys []string
	for _, row := range langs["french"].Untranslated {
		keys = append(keys, row.Key)
	}
	if !slices.Contains(keys, "bold") || slices.Contains(keys, "a.1.t") || slices.Contains(keys, "b_only") {
		t.Errorf("french: an english key with no french entry is untranslated where its mod ships french; one with an entry is not, nor a key of a mod that ships none: %v", keys)
	}
	if row := langs["french"].Untranslated[0]; !strings.HasSuffix(health.Sources[row.File].Rel, "a_l_english.yml") {
		t.Errorf("a row names its file by its number in Sources: %+v in %+v", row, health.Sources[row.File])
	}

	// what reads one key
	if readers := LocReaders(ws, "a.1.t"); len(readers) != 1 || readers[0].At.Origin != modA {
		t.Errorf("a key's readers: %+v", readers)
	}
}
