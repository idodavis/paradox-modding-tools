package source

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
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
	})
	files := Inventory("vanilla", Roots(dir))

	var got []string
	for _, file := range files {
		root, _ := filepath.Rel(dir, file.Path[:len(file.Path)-len(file.Rel)-1])
		got = append(got, filepath.ToSlash(root)+" | "+file.Rel+" | "+file.Format.String()+" | "+file.Origin)
	}
	want := []string{
		"game | common/GUIDs.txt | script | vanilla",
		"game | common/README.txt | info | vanilla",
		"game | common/town_rights/readme.txt | info | vanilla",
		"game | common/traits/00_traits.txt | script | vanilla",
		"game | gfx/interface/x.PNG | image | vanilla",
		"game | gfx/new_thing.voxel | unknown | vanilla",
		"game | gfx/x.dds | image | vanilla",
		"game | sound/GUIDs.txt | list | vanilla",
		"game/dlc/dlc001 | events/dlc_e.txt | script | vanilla",
		"game/dlc/dlc001 | gfx/y.dds | image | vanilla",
		"game/dlc/dlc001 | sound/GUIDs.txt | list | vanilla",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Inventory =\n%v\nwant\n%v", got, want)
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
		files := Inventory("vanilla", roots)
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
			info, err := ReadImageInfo(file.Path)
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
		Walk(dir, func(path string) {
			rel, _ := filepath.Rel(dir, path)
			got = append(got, filepath.ToSlash(rel))
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
	Walk(filepath.Join(dir, "missing"), func(string) { t.Error("walked a missing root") })
}

// dds builds a DDS file head: magic, 124 byte header, and a DX10 header when dxgiFormat is set.
func dds(width, height, mips, pfFlags uint32, fourCC string, bits, dxgiFormat uint32) []byte {
	head := make([]byte, 128)
	copy(head, "DDS ")
	put := func(off int, value uint32) { binary.LittleEndian.PutUint32(head[off:], value) }
	put(4, 124)
	put(12, height)
	put(16, width)
	put(28, mips)
	put(76, 32)
	put(80, pfFlags)
	copy(head[84:88], fourCC)
	put(88, bits)
	if dxgiFormat != 0 {
		ext := make([]byte, 20)
		binary.LittleEndian.PutUint32(ext, dxgiFormat)
		head = append(head, ext...)
	}
	return head
}

func TestReadImageInfo(t *testing.T) {
	var pngBytes, jpgBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpgBytes, img, nil); err != nil {
		t.Fatal(err)
	}
	tga := make([]byte, 18)
	binary.LittleEndian.PutUint16(tga[12:], 64)
	binary.LittleEndian.PutUint16(tga[14:], 32)
	tga[16] = 32

	dir := t.TempDir()
	for _, testCase := range []struct {
		name string
		data []byte
		want ImageInfo
		bad  bool
	}{
		{"dxt5.dds", dds(256, 128, 9, 0x4, "DXT5", 0, 0), ImageInfo{256, 128, "DXT5", 9}, false},
		{"bc7.dds", dds(1024, 1024, 11, 0x4, "DX10", 0, 98), ImageInfo{1024, 1024, "BC7", 11}, false},
		{"bc7_srgb.dds", dds(60, 60, 1, 0x4, "DX10", 0, 99), ImageInfo{60, 60, "BC7 sRGB", 1}, false},
		{"odd.dds", dds(8, 8, 0, 0x4, "DX10", 0, 1234), ImageInfo{8, 8, "DXGI 1234", 0}, false},
		{"rgba.dds", dds(256, 256, 0, 0x41, "", 32, 0), ImageInfo{256, 256, "RGBA8", 0}, false},
		{"rgb.dds", dds(256, 256, 0, 0x40, "", 24, 0), ImageInfo{256, 256, "RGB8", 0}, false},
		{"rgbx.dds", dds(4, 4, 0, 0x40, "", 32, 0), ImageInfo{4, 4, "RGBX8", 0}, false},
		{"lum.dds", dds(16, 16, 0, 0x20000, "", 8, 0), ImageInfo{16, 16, "8-bit", 0}, false},
		{"half.dds", dds(64, 64, 1, 0x4, "q\x00\x00\x00", 0, 0), ImageInfo{64, 64, "RGBA16F", 1}, false},
		{"numbered.dds", dds(64, 64, 1, 0x4, "\x15\x00\x00\x00", 0, 0), ImageInfo{64, 64, "D3DFMT 21", 1}, false},

		// the games ship a PNG named .dds: the file's own magic decides
		{"really_png.dds", pngBytes.Bytes(), ImageInfo{3, 2, "png", 0}, false},
		{"x.PNG", pngBytes.Bytes(), ImageInfo{3, 2, "png", 0}, false},
		{"x.jpg", jpgBytes.Bytes(), ImageInfo{3, 2, "jpeg", 0}, false},
		{"x.tga", tga, ImageInfo{64, 32, "TGA 32-bit", 0}, false},

		{"short.dds", []byte("DDS "), ImageInfo{}, true},
		{"empty.dds", nil, ImageInfo{}, true},
		{"empty.png", nil, ImageInfo{}, true},
		{"short.tga", []byte{1, 2, 3}, ImageInfo{}, true},
	} {
		path := filepath.Join(dir, testCase.name)
		if err := os.WriteFile(path, testCase.data, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := ReadImageInfo(path)
		if (err != nil) != testCase.bad || got != testCase.want {
			t.Errorf("ReadImageInfo(%s) = %+v, %v; want %+v, error %v", testCase.name, got, err, testCase.want, testCase.bad)
		}
	}

	if _, err := ReadImageInfo(filepath.Join(dir, "missing.dds")); err == nil {
		t.Error("a missing file read without an error")
	}
}
