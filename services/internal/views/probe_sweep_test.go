//go:build probe

// probe_sweep_test.go is the engine's scoreboard, not a gate: it builds a workspace over each mod a game's mod folder lists, and prints what the health report finds and how often the editor has something to say. Counts are not targets: a workshop mod goes stale, and a mod that names what a patch removed produces a correct finding. It asserts nothing, so it is build-tagged probe and runs behind `task test:sweep`. Its samples are budgets, not thresholds: nothing here decides anything, and every budget is there because one editor request costs a walk of its file, so an unbounded sample is quadratic in a mod's size.
package views

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/lsp"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// The sample budgets: files diagnosed per mod, files hovered in, and words hovered in each of those.
const (
	diagnosed  = 300
	hoverFiles = 40
	hoverWords = 8
)

var modPath = regexp.MustCompile(`(?m)^\s*path\s*=\s*"([^"]+)"`)

// TestSweep walks every mod the folders PMT_TEST_MODS_<GAME> list (a game's Documents mod folder, whose .mod descriptors say where each mod is), over the install PMT_TEST_INSTALL_<GAME>.
func TestSweep(t *testing.T) {
	if os.Getenv("PMT_TEST_SWEEP") == "" {
		t.Skip("walks every mod of a game's mod folder; set PMT_TEST_SWEEP=1")
	}
	for _, id := range []string{"ck3", "vic3", "eu5"} {
		install, mods := os.Getenv("PMT_TEST_INSTALL_"+strings.ToUpper(id)), os.Getenv("PMT_TEST_MODS_"+strings.ToUpper(id))
		if install == "" || mods == "" {
			continue
		}
		user := game.UserDataDir(id, install)
		started := time.Now()
		scanned, err := index.Scan(context.Background(), index.Request{Game: id, Dir: install, Version: "sweep", Docs: []string{filepath.Join(user, "logs"), filepath.Join(user, "docs")}})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SWEEP %s: install scanned in %.1fs", id, time.Since(started).Seconds())

		descriptors, _ := filepath.Glob(filepath.Join(mods, "*.mod"))
		for _, d := range descriptors {
			raw, err := os.ReadFile(d)
			if err != nil {
				continue
			}
			m := modPath.FindSubmatch(raw)
			if m == nil {
				continue
			}
			dir := string(m[1])
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(user, dir)
			}
			s, err := session.New(context.Background(), game.Get(id), scanned, []session.Mod{{ID: "mod", Name: filepath.Base(d), Dir: dir}}, "")
			if err != nil {
				t.Logf("SWEEP %s %s: %v", id, filepath.Base(d), err)
				continue
			}
			sweep(t, id, filepath.Base(d), s)
		}
	}
}

// sweep reports one mod: the health counts, the findings over a sample of its script files, and the hover answer rate over a sample of the words in a sample of those files.
func sweep(t *testing.T, id, name string, s *session.Session) {
	started := time.Now()
	h := WorkspaceHealth(s, HealthParams{})
	missing, orphaned := 0, 0
	for _, l := range h.Loc {
		missing, orphaned = missing+l.MissingCount, orphaned+l.OrphanedCount
	}
	var files []source.File
	for _, f := range s.Workspace.Files {
		if f.Format == source.Script {
			files = append(files, f)
		}
	}
	slices.SortFunc(files, func(a, b source.File) int { return strings.Compare(a.Rel, b.Rel) })

	findings := map[string]int{}
	for _, f := range spread(files, diagnosed) {
		for _, d := range must(lsp.Diagnose(s, f.Path, nil)) {
			findings[d.Code]++
		}
	}
	hovers, answered := 0, 0
	for _, f := range spread(files, hoverFiles) {
		text, err := s.Text(f.Path)
		if err != nil {
			continue
		}
		asked := 0
		for line, l := range strings.Split(text, "\n") {
			for _, word := range strings.Fields(l) {
				if strings.HasPrefix(word, "#") {
					break
				}
				col := strings.Index(l, word)
				if col < 0 || word == "=" || word == "{" || word == "}" {
					continue
				}
				asked++
				if asked > hoverWords {
					break
				}
				hovers++
				if card, _ := lsp.Hover(s, f.Path, line, col); card != nil {
					answered++
				}
			}
			if asked > hoverWords {
				break
			}
		}
	}
	fmt.Printf("SWEEP %s %s: %d files, %d defs; contests %d, depends %d, dangling %d, loc missing %d orphaned %d, files %d; findings %v; hover answered %d of %d; %.1fs\n",
		id, name, len(s.Workspace.Files), len(s.Workspace.Defs), len(h.Contests), len(h.Depends), h.DanglingCount, missing, orphaned, h.FilesCount, findings, answered, hovers, time.Since(started).Seconds())
}

// spread takes at most n of the files, evenly across the whole list, so a sample of a big mod reaches the same corners a small one does.
func spread(files []source.File, n int) []source.File {
	if len(files) <= n {
		return files
	}
	out := make([]source.File, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, files[i*len(files)/n])
	}
	return out
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
