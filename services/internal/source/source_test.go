package source

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// tree writes files under a folder of the test's own; a value is the file's bytes.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestClassify(t *testing.T) {
	for rel, want := range map[string]Format{
		"common/traits/00_traits.txt":          Script,
		"gui/window_character.gui":             Script,
		"gfx/models/buildings/x.asset":         Script,
		"fonts/fonts.font":                     Script,
		"map_data/default.map":                 Script,
		"gfx/particles/fire.particle2":         Script,
		"localization/english/x_l_english.yml": Loc,
		"map_data/definition.csv":              Table,
		"map_data/adjacencies.csv":             Table,
		"sound/GUIDs.txt":                      List,
		"dlc/dlc001/sound/GUIDs.txt":           List,
		"common/GUIDs.txt":                     Script,
		".metadata/metadata.json":              JSON,
		"common/traits/_traits.info":           Info,
		"common/laws/laws.md":                  Info,
		"common/town_rights/readme.txt":        Info,
		"common/README.txt":                    Info,
		"gfx/interface/icons/traits/brave.dds": Image,
		"gfx/interface/x.PNG":                  Image,
		"gfx/coat_of_arms/patterns/p.tga":      Image,
		"gfx/models/x.mesh":                    Opaque,
		"sound/banks/Master.bank":              Opaque,
		"fonts/Open_Sans/OpenSans.ttf":         Opaque,
		"gfx/FX/pdxmesh.shader":                Opaque,

		// an extension the table does not list, and a file with none
		"gfx/new_thing.voxel": Unknown,
		"licenses/LICENSE":    Unknown,
	} {
		if got := Classify(rel); got != want {
			t.Errorf("Classify(%q) = %v; want %v", rel, got, want)
		}
	}
}

func TestRoots(t *testing.T) {
	rel := func(dir string, roots []string) []string {
		var out []string
		for _, r := range roots {
			p, _ := filepath.Rel(dir, r)
			out = append(out, filepath.ToSlash(p))
		}
		return out
	}

	for _, c := range []struct {
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
		dir := tree(t, c.files)
		if got := rel(dir, Roots(dir)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Roots = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestInventory(t *testing.T) {
	dir := tree(t, map[string]string{
		"game/common/traits/00_traits.txt": "\xEF\xBB\xBFbrave = { }\r\n",
		"game/common/traits/latin1.txt":    "name = \"Rh\xF4ne\"\n",
		"game/gfx/x.dds":                   "",
		"game/gfx/source.psd":              "",
		"game/binaries.dll":                "",
		"game/LICENSE":                     "",
		"game/gfx/new_thing.voxel":         "",
		"game/dlc/dlc001/gfx/y.dds":        "",
		"game/dlc/dlc001/events/dlc_e.txt": "",
		"game/.git/config":                 "",
	})
	files := Inventory("vanilla", Roots(dir))

	var got []string
	for _, f := range files {
		root, _ := filepath.Rel(dir, f.Path[:len(f.Path)-len(f.Rel)-1])
		got = append(got, filepath.ToSlash(root)+" | "+f.Rel+" | "+f.Format.String()+" | "+f.Origin)
	}
	want := []string{
		"game | common/traits/00_traits.txt | script | vanilla",
		"game | common/traits/latin1.txt | script | vanilla",
		"game | gfx/new_thing.voxel | unknown | vanilla",
		"game | gfx/x.dds | image | vanilla",
		"game/dlc/dlc001 | events/dlc_e.txt | script | vanilla",
		"game/dlc/dlc001 | gfx/y.dds | image | vanilla",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Inventory =\n%v\nwant\n%v", got, want)
	}

	// Text strips the BOM and folds CRLF, and reads Latin-1 where the bytes are not UTF-8
	if text, err := files[0].Text(); err != nil || text != "brave = { }\n" {
		t.Errorf("Text = %q, %v", text, err)
	}
	if text, err := files[1].Text(); err != nil || text != "name = \"Rhône\"\n" {
		t.Errorf("Latin-1 Text = %q, %v", text, err)
	}
}
