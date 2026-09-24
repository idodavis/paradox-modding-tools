//go:build sweep

// sweep_test.go is the engine's scoreboard, not a gate: it builds a workspace over each mod a
// game's mod folder lists, and prints what the health report finds and how often the editor has
// something to say. It asserts nothing, so it is build-tagged and runs behind `task test:sweep`.
// Counts are not targets: a mod that names what a patch removed produces a correct finding. The
// samples are budgets: one editor request walks its file, so an unbounded sample is quadratic.
package services

import (
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
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/session"
)

// The sample budgets: files diagnosed per mod, files hovered in, and words hovered in each.
const (
	diagnosed  = 300
	hoverFiles = 40
	hoverWords = 8
)

var modPath = regexp.MustCompile(`(?m)^\s*path\s*=\s*"([^"]+)"`)

// TestSweep walks every mod the folders PMT_TEST_MODS_<GAME> list (a game's Documents mod folder,
// whose .mod descriptors say where each mod is), over the install PMT_TEST_INSTALL_<GAME>.
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
		scanned, err := index.Scan(t.Context(), index.Request{Game: id, Dir: install, Version: "sweep", Docs: []string{filepath.Join(user, "logs"), filepath.Join(user, "docs")}})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SWEEP %s: install scanned in %.1fs", id, time.Since(started).Seconds())

		descriptors, _ := filepath.Glob(filepath.Join(mods, "*.mod"))
		for _, descriptor := range descriptors {
			raw, err := os.ReadFile(descriptor)
			if err != nil {
				continue
			}
			match := modPath.FindSubmatch(raw)
			if match == nil {
				continue
			}
			dir := string(match[1])
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(user, dir)
			}
			sess, err := session.New(t.Context(), game.Get(id), scanned, []session.Mod{{ID: "mod", Name: filepath.Base(descriptor), Dir: dir}}, "")
			if err != nil {
				t.Logf("SWEEP %s %s: %v", id, filepath.Base(descriptor), err)
				continue
			}
			sweep(t, id, filepath.Base(descriptor), sess)
		}
	}
}

// sweep reports one mod: the health counts, the findings over a sample of its script files, and
// the hover answer rate over a sample of the words in a sample of those files.
func sweep(t *testing.T, id, name string, sess *session.Session) {
	started := time.Now()
	health := workspaceHealth(sess, HealthParams{})
	missing, orphaned := 0, 0
	for _, language := range health.Loc {
		missing, orphaned = missing+language.MissingCount, orphaned+language.OrphanedCount
	}
	var files []source.File
	for _, file := range sess.Workspace.Files {
		if file.Format == source.Script {
			files = append(files, file)
		}
	}
	slices.SortFunc(files, func(first, second source.File) int { return strings.Compare(first.Rel, second.Rel) })

	findings := map[string]int{}
	for _, file := range spread(files, diagnosed) {
		for _, finding := range must(lsp.Diagnose(sess, file.Path, nil)) {
			findings[finding.Code]++
		}
	}
	hovers, answered := 0, 0
	for _, hoverFile := range spread(files, hoverFiles) {
		text, err := sess.Text(hoverFile.Path)
		if err != nil {
			continue
		}
		asked := 0
		for line, lineText := range strings.Split(text, "\n") {
			for _, word := range strings.Fields(lineText) {
				if strings.HasPrefix(word, "#") {
					break
				}
				col := strings.Index(lineText, word)
				if col < 0 || word == "=" || word == "{" || word == "}" {
					continue
				}
				asked++
				if asked > hoverWords {
					break
				}
				hovers++
				if card, _ := lsp.Hover(sess, hoverFile.Path, line, col); card != nil {
					answered++
				}
			}
			if asked > hoverWords {
				break
			}
		}
	}
	fmt.Printf("SWEEP %s %s: %d files, %d defs; contests %d, depends %d, dangling %d, loc missing %d orphaned %d, files %d; findings %v; hover answered %d of %d; %.1fs\n",
		id, name, len(sess.Workspace.Files), len(sess.Workspace.Defs), len(health.Contests), len(health.Depends), health.DanglingCount, missing, orphaned, health.FilesCount, findings, answered, hovers, time.Since(started).Seconds())
}

// spread takes at most n files evenly across the list, so a big mod's sample reaches every corner.
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

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
