package inventory

import (
	"cmp"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/texture"
)

// tree writes files under a folder of the test's own; a value is the file's bytes.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRoots(t *testing.T) {
	rel := func(dir string, roots []string) []string {
		var out []string
		for _, root := range roots {
			path, _ := filepath.Rel(dir, root)
			out = append(out, filepath.ToSlash(path))
		}
		return out
	}

	for _, testCase := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"an install: base layers first, DLC last, nothing inside a root but its dlc folder", map[string]string{
			"game/common/traits/t.txt":               "",
			"game/gfx/models/buildings/common/x.txt": "",
			"game/localization/english/events/x.yml": "",
			"game/dlc/dlc001/gfx/x.dds":              "",
			"game/dlc/dlc002/events/e.txt":           "",
			"jomini/gui/x.gui":                       "",
			"clausewitz/gfx/x.dds":                   "",
			"binaries/ck3.exe":                       "",
			"launcher/common/x.txt":                  "",
		}, []string{"clausewitz", "jomini", "game", "launcher", "game/dlc/dlc001", "game/dlc/dlc002"}},
		{"EU5: every stage folder of every layer is a root", map[string]string{
			"game/in_game/common/x.txt":           "",
			"game/main_menu/gui/x.gui":            "",
			"game/loading_screen/localization/x":  "",
			"game/dlc/D008/in_game/gfx/x.dds":     "",
			"jomini/main_menu/gui/x.gui":          "",
			"clausewitz/loading_screen/gfx/x.dds": "",
		}, []string{"clausewitz/loading_screen", "jomini/main_menu", "game/in_game", "game/loading_screen", "game/main_menu", "game/dlc/D008/in_game"}},
		{"a mod is its own root", map[string]string{"descriptor.mod": "", "events/e.txt": ""}, []string{"."}},
		{"an EU5 mod's roots are its stage folders", map[string]string{".metadata/metadata.json": "{}", "in_game/common/x.txt": "", "main_menu/gui/x.gui": ""}, []string{"in_game", "main_menu"}},
		{"a folder with no layout has none", map[string]string{"notes/readme.txt": ""}, nil},
	} {
		dir := tree(t, testCase.files)
		if got := rel(dir, Roots(dir)); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("%s: Roots = %v; want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestInventory(t *testing.T) {
	dir := tree(t, map[string]string{
		"game/common/traits/00_traits.txt": "brave = { }\n",
		"game/gfx/x.dds":                   "",
		"game/gfx/source.psd":              "",
		"game/binaries.dll":                "",
		"game/LICENSE":                     "",
		"game/gfx/new_thing.voxel":         "",
		"game/dlc/dlc001/gfx/y.dds":        "",
		"game/dlc/dlc001/events/dlc_e.txt": "",
		"game/.git/config":                 "",

		// a file that is what it is by its name, and an upper-case extension
		"game/sound/GUIDs.txt":               "",
		"game/dlc/dlc001/sound/GUIDs.txt":    "",
		"game/common/GUIDs.txt":              "",
		"game/common/town_rights/readme.txt": "",
		"game/common/README.txt":             "",
		"game/gfx/interface/x.PNG":           "",

		// a .txt named as documentation is documentation only when its comments outnumber its script
		"game/common/missions/____Info.txt":            "# Missions\n# - potential: <trigger>\n",
		"game/common/advances/_advances_template.txt":  "#\tage = <age>\t# the age it is in\n#\ticon = <icon>\n",
		"game/events/my_example_events.txt":            "namespace = x\nx.1 = { type = character_event }\n",
		"game/common/artifacts/00_event_templates.txt": "# a list\nx = { }\ny = { }\n",
	})
	files := Files("vanilla", Roots(dir))

	names := [...]string{Unknown: "unknown", Script: "script", List: "list", Prose: "info", Image: "image", Skipped: "opaque"}
	var got []string
	for _, file := range files {
		root, _ := filepath.Rel(dir, file.Path[:len(file.Path)-len(file.Rel)-1])
		got = append(got, filepath.ToSlash(root)+" | "+file.Rel+" | "+names[file.Format]+" | "+file.Origin)
	}
	want := []string{
		"game | common/GUIDs.txt | script | vanilla",
		"game | common/README.txt | info | vanilla",
		"game | common/advances/_advances_template.txt | info | vanilla",
		"game | common/artifacts/00_event_templates.txt | script | vanilla",
		"game | common/missions/____Info.txt | info | vanilla",
		"game | common/town_rights/readme.txt | info | vanilla",
		"game | common/traits/00_traits.txt | script | vanilla",
		"game | events/my_example_events.txt | script | vanilla",
		"game | gfx/interface/x.PNG | image | vanilla",
		"game | gfx/new_thing.voxel | unknown | vanilla",
		"game | gfx/x.dds | image | vanilla",
		"game | sound/GUIDs.txt | list | vanilla",
		"game/dlc/dlc001 | events/dlc_e.txt | script | vanilla",
		"game/dlc/dlc001 | gfx/y.dds | image | vanilla",
		"game/dlc/dlc001 | sound/GUIDs.txt | list | vanilla",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Files =\n%v\nwant\n%v", got, want)
	}
}

// TestInstallsAreClassified is the gate over the real installs: every file has a format or an
// ignore rule, and every non-empty image gives up its header. It walks three installs and opens
// some sixty thousand images; set PMT_SOURCE_GATE=1 and the three PMT_TEST_INSTALL_ variables.
func TestInstallsAreClassified(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SOURCE_GATE") == "" {
		t.Skip("walks three installs; set PMT_SOURCE_GATE=1")
	}

	for _, testCase := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		install := os.Getenv(testCase.env)
		if install == "" {
			t.Errorf("%s is not set: the gate cannot clear a game it never walked", testCase.env)
			continue
		}
		roots := Roots(install)
		files := Files("vanilla", roots)
		if len(roots) == 0 || len(files) < 10000 {
			t.Errorf("%s: %d roots and %d files under %s: wrong folder", testCase.game, len(roots), len(files), install)
			continue
		}

		// every file has a format
		byFormat, unknown := map[Format]int{}, map[string]int{}
		for _, file := range files {
			byFormat[file.Format]++
			if file.Format == Unknown {
				unknown[strings.ToLower(path.Ext(file.Rel))]++
			}
		}
		t.Logf("SOURCE %s: %d roots, %d files: %v", testCase.game, len(roots), len(files), byFormat)
		if len(unknown) > 0 {
			t.Errorf("%s: extensions with no format and no ignore rule: %v", testCase.game, unknown)
		}

		// every image gives up its header
		var images, bad, empty int
		var examples []string
		formats := map[string]int{}
		for _, file := range files {
			if file.Format != Image {
				continue
			}

			// the games ship a handful of zero byte images, which have no header to read
			if stat, err := os.Stat(file.Path); err == nil && stat.Size() == 0 {
				empty++
				continue
			}
			images++
			info, err := texture.Info(file.Path)
			if err != nil || info.Width <= 0 || info.Height <= 0 {
				bad++
				if len(examples) < 8 {
					examples = append(examples, file.Rel)
				}
				continue
			}
			formats[info.Format]++
		}
		names := make([]string, 0, len(formats))
		for name := range formats {
			names = append(names, name)
		}
		slices.SortFunc(names, func(first, second string) int { return cmp.Compare(formats[second], formats[first]) })
		t.Logf("IMAGES %s: %d read, %d failed, %d empty files skipped; formats %v", testCase.game, images, bad, empty, names)
		if bad > 0 {
			t.Errorf("%s: %d of %d images gave no header facts, among them %v", testCase.game, bad, images, examples)
		}
	}
}

func TestWalk(t *testing.T) {
	dir := tree(t, map[string]string{
		"b/2.txt":      "",
		"a/1.txt":      "",
		"a/deep/0.txt": "",
		"top.txt":      "",
		".git/config":  "",
	})

	walked := func() []string {
		var got []string
		_ = Walk(dir, func(path string) error {
			rel, _ := filepath.Rel(dir, path)
			got = append(got, filepath.ToSlash(rel))
			return nil
		})
		return got
	}

	// name order within a folder, folders entered where they sort, .git skipped
	want := []string{"a/1.txt", "a/deep/0.txt", "b/2.txt", "top.txt"}
	if got := walked(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk = %v; want %v", got, want)
	}

	// a link elsewhere is followed and spelled under the root; a link back to an ancestor is not
	outside := tree(t, map[string]string{"linked.txt": ""})
	link := func(target, at string) {
		if os.Symlink(target, at) == nil {
			return
		}

		// Windows refuses a symlink without a privilege, and a linked mod folder is usually a junction
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", at, target).CombinedOutput(); err != nil {
			t.Skipf("this machine can make neither a symlink nor a junction: %v %s", err, out)
		}
	}
	link(outside, filepath.Join(dir, "c"))
	link(dir, filepath.Join(dir, "a", "loop"))
	want = []string{"a/1.txt", "a/deep/0.txt", "b/2.txt", "c/linked.txt", "top.txt"}
	if got := walked(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk with links = %v; want %v", got, want)
	}

	// a root that does not exist walks nothing
	_ = Walk(filepath.Join(dir, "missing"), func(string) error { t.Error("walked a missing root"); return nil })
}
