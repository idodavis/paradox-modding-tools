// gate_installs_test.go is Phase 2's gate over the real installs: every file of a game is given a format or ignored with a reason, and every image that is not an empty file gives up its header facts. It walks three installs and opens some sixty thousand images, so it runs only when asked: set PMT_SOURCE_GATE=1 and the three PMT_TEST_INSTALL_ variables, each a game's root folder.
package source

import (
	"os"
	"path"
	"sort"
	"strings"
	"testing"
)

func TestInstallsAreClassified(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SOURCE_GATE") == "" {
		t.Skip("walks three installs; set PMT_SOURCE_GATE=1")
	}

	for _, g := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		install := os.Getenv(g.env)
		if install == "" {
			t.Errorf("%s is not set: the gate cannot clear a game it never walked", g.env)
			continue
		}
		roots := Roots(install)
		files := Inventory("vanilla", roots)
		if len(roots) == 0 || len(files) < 10000 {
			t.Errorf("%s: %d roots and %d files under %s: wrong folder", g.game, len(roots), len(files), install)
			continue
		}

		// every file has a format
		byFormat, unknown := map[Format]int{}, map[string]int{}
		for _, f := range files {
			byFormat[f.Format]++
			if f.Format == Unknown {
				unknown[strings.ToLower(path.Ext(f.Rel))]++
			}
		}
		t.Logf("SOURCE %s: %d roots, %d files: %v", g.game, len(roots), len(files), byFormat)
		if len(unknown) > 0 {
			t.Errorf("%s: extensions with no format and no ignore rule: %v", g.game, unknown)
		}

		// every image gives up its header
		var images, bad, empty int
		var examples []string
		formats := map[string]int{}
		for _, f := range files {
			if f.Format != Image {
				continue
			}

			// the games ship a handful of zero byte images, which have no header to read
			if stat, err := os.Stat(f.Path); err == nil && stat.Size() == 0 {
				empty++
				continue
			}
			images++
			info, err := ReadImageInfo(f.Path)
			if err != nil || info.Width <= 0 || info.Height <= 0 {
				bad++
				if len(examples) < 8 {
					examples = append(examples, f.Rel)
				}
				continue
			}
			formats[info.Format]++
		}
		names := make([]string, 0, len(formats))
		for name := range formats {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool { return formats[names[i]] > formats[names[j]] })
		t.Logf("IMAGES %s: %d read, %d failed, %d empty files skipped; formats %v", g.game, images, bad, empty, names)
		if bad > 0 {
			t.Errorf("%s: %d of %d images gave no header facts, among them %v", g.game, bad, images, examples)
		}
	}
}
