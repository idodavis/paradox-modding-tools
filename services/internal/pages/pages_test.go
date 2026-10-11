// pages_test.go covers the three pages over the ck3 fixture install with the mods a test writes: the Health report,
// the event graph and its detail, and the searches.

package pages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// eventsGlob keeps what is under an events folder, and searchToken is what the fixture holds in most files.
const (
	eventsGlob  = "**/events/**"
	searchToken = "unique_search_token"
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
