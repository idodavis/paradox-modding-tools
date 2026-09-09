// searchservice_test.go covers Find-in-Files globs, filters, and cancellation.
package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSearchTree(t *testing.T, root string) {
	t.Helper()
	txt := filepath.Join(root, "events", "hit.txt")
	yml := filepath.Join(root, "events", "hit.yml")
	dds := filepath.Join(root, "gfx", "skip.dds")
	other := filepath.Join(root, "common", "other.txt")
	for _, p := range []string{txt, yml, dds, other} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(txt, []byte("title = unique_search_token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yml, []byte("unique_search_token yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dds, []byte("unique_search_token in a dds\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("unique_search_token common\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTextSearchIncludeEvents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchTree(t, root)
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern: "unique_search_token",
		Folders: []TextSearchFolder{{
			Path:     root,
			Includes: []string{"**/events/**"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("include events hits = %+v", res.Hits)
	}
	for _, h := range res.Hits {
		if !strings.Contains(filepath.ToSlash(h.Path), "/events/") {
			t.Fatalf("leaked path %s", h.Path)
		}
	}
}

func TestTextSearchExcludeTxt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchTree(t, root)
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern: "unique_search_token",
		Folders: []TextSearchFolder{{
			Path:     root,
			Excludes: []string{"*.txt"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Hits {
		if strings.HasSuffix(filepath.ToSlash(h.Path), ".txt") {
			t.Fatalf("txt not excluded: %s", h.Path)
		}
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected yml/dds hits after excluding txt")
	}
}

func TestTextSearchFoldersDoNotLeakGlobs(t *testing.T) {
	t.Parallel()
	a := t.TempDir()
	b := t.TempDir()
	writeSearchTree(t, a)
	writeSearchTree(t, b)
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern: "unique_search_token",
		Folders: []TextSearchFolder{
			{Path: a, Includes: []string{"**/events/**"}},
			{Path: b, Includes: []string{"**/common/**"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var aHits, bHits int
	for _, h := range res.Hits {
		p := filepath.ToSlash(h.Path)
		switch {
		case strings.HasPrefix(p, filepath.ToSlash(a)):
			aHits++
			if !strings.Contains(p, "/events/") {
				t.Fatalf("folder a leaked %s", h.Path)
			}
		case strings.HasPrefix(p, filepath.ToSlash(b)):
			bHits++
			if !strings.Contains(p, "/common/") {
				t.Fatalf("folder b leaked %s", h.Path)
			}
		}
	}
	if aHits == 0 || bHits == 0 {
		t.Fatalf("expected hits in both folders: a=%d b=%d %+v", aHits, bHits, res.Hits)
	}
}

func TestTextSearchEmptyIncludeUsesPassedExcludes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchTree(t, root)
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern: "unique_search_token",
		Folders: []TextSearchFolder{{
			Path:     root,
			Excludes: []string{"*.dds"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Hits {
		if strings.HasSuffix(filepath.ToSlash(h.Path), ".dds") {
			t.Fatalf("dds not excluded: %s", h.Path)
		}
	}
	if len(res.Hits) == 0 {
		t.Fatal("expected script hits")
	}
}

func TestTextSearchSkipsGitDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitFile := filepath.Join(root, ".git", "objects", "secret.txt")
	if err := os.MkdirAll(filepath.Dir(gitFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitFile, []byte("hidden_token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vis := filepath.Join(root, "visible.txt")
	if err := os.WriteFile(vis, []byte("hidden_token visible\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern: "hidden_token",
		Folders: []TextSearchFolder{{Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %+v", res.Hits)
	}
	if strings.Contains(filepath.ToSlash(res.Hits[0].Path), "/.git/") {
		t.Fatalf("searched .git: %s", res.Hits[0].Path)
	}
}

func TestTextSearchSkipsNULFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	nul := filepath.Join(root, "binary.dat")
	data := append(append([]byte("findme"), 0), []byte("tail")...)
	if err := os.WriteFile(nul, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ok := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(ok, []byte("findme ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern: "findme",
		Folders: []TextSearchFolder{{Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %+v", res.Hits)
	}
	if strings.HasSuffix(res.Hits[0].Path, "binary.dat") {
		t.Fatal("NUL file was searched")
	}
}

func TestTextSearchRegexpWordCase(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := "TOK only\npartialtoken\nanother TOK\n"
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ss := &SearchService{}
	res, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern:         "tok",
		IsCaseSensitive: true,
		IsWordMatch:     true,
		Folders:         []TextSearchFolder{{Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("case-sensitive word hits = %+v", res.Hits)
	}
	res, err = ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern:     "tok",
		IsWordMatch: true,
		Folders:     []TextSearchFolder{{Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("case-insensitive word hits = %+v", res.Hits)
	}
	res, err = ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern:  `tok.+only`,
		IsRegexp: true,
		Folders:  []TextSearchFolder{{Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("regex hits = %+v", res.Hits)
	}
}

func TestTextSearchInvalidRegex(t *testing.T) {
	t.Parallel()
	ss := &SearchService{}
	_, err := ss.TextSearch(context.Background(), TextSearchQuery{
		Pattern:  "[",
		IsRegexp: true,
		Folders:  []TextSearchFolder{{Path: t.TempDir()}},
	})
	if err == nil {
		t.Fatal("expected invalid pattern error")
	}
}

func TestFileSearchNeedleAndGlob(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchTree(t, root)
	ss := &SearchService{}
	res, err := ss.FileSearch(context.Background(), FileSearchQuery{
		FilePattern: "hit",
		Folders: []TextSearchFolder{{
			Path:     root,
			Includes: []string{"**/events/**"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("file hits = %+v", res.Hits)
	}
	for _, h := range res.Hits {
		if !strings.Contains(filepath.ToSlash(h.Path), "/events/") {
			t.Fatalf("leaked %s", h.Path)
		}
	}
}

func TestTextSearchCancel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 40 {
		name := filepath.Join(root, "dir", strings.Repeat("a", i%5+1), fmt.Sprintf("f%d.txt", i))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		body := strings.Repeat("needle\n", 200)
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ss := &SearchService{}
	q := TextSearchQuery{
		Pattern: "needle",
		Folders: []TextSearchFolder{{Path: root}},
	}

	// The full result, so the cancelled run has something real to be smaller
	// than. The previous assertion was `len(res.Hits) > 8000` against a fixture
	// of 40 files x 200 lines — exactly 8,000 — so it was unreachable even with
	// cancellation removed entirely.
	full, err := ss.TextSearch(context.Background(), q)
	if err != nil {
		t.Fatalf("uncancelled search: %v", err)
	}
	if len(full.Hits) == 0 {
		t.Fatal("fixture produced no hits; the test would prove nothing")
	}

	// Cancelled before the call, not on a timer: a 5ms race against the walk
	// decides nothing on a fast machine, where the search finishes first and
	// the test passes for the wrong reason.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := ss.TextSearch(ctx, q)
	if err != nil {
		t.Fatalf("cancellation must not surface as an error: %v", err)
	}
	if len(res.Hits) >= len(full.Hits) {
		t.Fatalf("cancel did not stop the search: got %d hits, full run is %d",
			len(res.Hits), len(full.Hits))
	}
}
