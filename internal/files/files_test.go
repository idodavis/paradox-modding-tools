package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestClassifyFormat checks the format of one path per rule, the extension in either case.
func TestClassifyFormat(t *testing.T) {
	formats := map[string]Format{
		"localization/english/v_l_english.yml": FormatLocalization,
		"localisation/replace/x_l_french.YML":  FormatLocalization,
		"launcher/settings.yml":                FormatText,
		"common/traits/v_traits.txt":           FormatScript,
		"sound/GUIDs.TXT":                      FormatScript,
		"gui/v_window.gui":                     FormatGUI,
		"gfx/models/v_shared.asset":            FormatAsset,
		"fonts/v.font":                         FormatFont,
		"gfx/interface/v.gfx":                  FormatGFX,
		"gfx/interface/v_bg.dds":               FormatImage,
		"gfx/interface/v.png":                  FormatImage,
		"gfx/interface/v.tga":                  FormatImage,
		"map_data/definition.csv":              FormatTable,
		"descriptor.mod":                       FormatDescriptor,
		".metadata/metadata.json":              FormatDescriptor,
		"sub/descriptor.mod":                   FormatText,
		"sub/metadata.json":                    FormatText,
		"gfx/shaders/v.fxh":                    FormatShader,
		"gfx/shaders/v.shader":                 FormatShader,
		"binaries/ck3.exe":                     FormatBinary,
		"fonts/Go-Regular.ttf":                 FormatBinary,
		"gfx/models/v_shared.mesh":             FormatBinary,
		"common/traits/_traits.info":           FormatText,
		"README.md":                            FormatText,
		"fonts/LICENSE":                        FormatText,
	}
	for rel, format := range formats {
		if Classify(rel) != format {
			t.Errorf("Classify(%q) = %q, want %q", rel, Classify(rel), format)
		}
	}
}

// TestDecodeText checks that each encoding is told apart and round-trips through Encode and the disk byte for
// byte, and that a rune Windows-1252 lacks is written as a question mark.
func TestDecodeText(t *testing.T) {
	samples := map[Encoding][]byte{
		EncodingUTF8:        []byte("name = \"caf\xC3\xA9\"\r\n"),
		EncodingUTF8BOM:     []byte("\xEF\xBB\xBFname = \"caf\xC3\xA9\"\n"),
		EncodingWindows1252: []byte("name = \"caf\xE9\"\n"),
	}
	path := filepath.Join(t.TempDir(), "sample.txt")
	for encoding, bytes := range samples {
		sample := DecodeText(bytes)
		if sample.Encoding != encoding || !strings.HasPrefix(sample.Text, "name = \"caf\xC3\xA9\"") {
			t.Errorf("DecodeText(%q) = %+v, want %s", bytes, sample, encoding)
		}
		if !slices.Equal(sample.Encode(), bytes) {
			t.Errorf("%s: Encode() = %q, want %q", encoding, sample.Encode(), bytes)
		}
		if err := WriteText(path, sample); err != nil {
			t.Fatal(err)
		}
		if text, err := ReadText(path); err != nil || text != sample {
			t.Errorf("%s: ReadText after WriteText = %+v, %v, want %+v", encoding, text, err, sample)
		}
	}
	sample := Text{Text: "\xE2\x82\xAC\xC4\x80", Encoding: EncodingWindows1252}
	if string(sample.Encode()) != "\x80?" {
		t.Errorf("Encode() = %q, want %q", sample.Encode(), "\x80?")
	}
}

// TestIsBinary checks that a NUL byte marks bytes as binary, and only within the first kilobyte.
func TestIsBinary(t *testing.T) {
	if !IsBinary([]byte("MZ\x00\x02")) || IsBinary([]byte("name = x\n")) {
		t.Error("IsBinary does not tell a NUL byte from text")
	}
	if IsBinary([]byte(strings.Repeat("a", 1024) + "\x00")) {
		t.Error("IsBinary looked past the first kilobyte")
	}
}

// TestToKey checks the key of Windows and POSIX spellings.
func TestToKey(t *testing.T) {
	keys := map[string]string{
		`C:\Mods\My Mod\events\..\common/traits.txt`: "c:/mods/my mod/common/traits.txt",
		"/home/me/Mods/x/":                           "/home/me/mods/x",
		"C:/MODS/x/./y":                              "c:/mods/x/y",
	}
	for path, key := range keys {
		if ToKey(path) != key {
			t.Errorf("ToKey(%q) = %q, want %q", path, ToKey(path), key)
		}
	}
}

// TestToRel checks a path against a root in both spellings, and IsUnder with it.
func TestToRel(t *testing.T) {
	rows := []struct {
		root, path, rel string
		ok              bool
	}{
		{`C:\Mods\Mod`, `c:\mods\mod\events\A.txt`, "events/A.txt", true},
		{"C:/Mods/Mod", `C:\Mods\Mod`, "", false},
		{"C:/Mods/Mod", `C:\Mods\Mod2\x.txt`, "", false},
		{"/home/me/mod/", "/home/me/mod/common/x.txt", "common/x.txt", true},
		{"/home/me/mod", "/home/me/mod/../other/x.txt", "", false},
		{`C:\Users\İsmail\mod`, `C:\Users\İsmail\mod\common\x.txt`, "common/x.txt", true},
	}
	for _, row := range rows {
		rel, ok := ToRel(row.root, row.path)
		if rel != row.rel || ok != row.ok || IsUnder(row.root, row.path) != row.ok {
			t.Errorf("ToRel(%q, %q) = %q, %v, want %q, %v", row.root, row.path, rel, ok, row.rel, row.ok)
		}
	}
}

// TestJoinPath checks that a root and a rel join into this platform's spelling.
func TestJoinPath(t *testing.T) {
	root := filepath.Join("mods", "mod")
	if Join(root, "events/a.txt") != filepath.Join(root, "events", "a.txt") {
		t.Errorf("Join(%q, %q) = %q", root, "events/a.txt", Join(root, "events/a.txt"))
	}
}

// TestToSlug checks the slug of a title: case, runs of other characters, trimming and letters outside ASCII.
func TestToSlug(t *testing.T) {
	slugs := map[string]string{
		"  My Great Mod!! v2.0 ":            "my_great_mod_v2_0",
		"Cr\xC3\xA8me Br\xC3\xBBl\xC3\xA9e": "creme_brulee",
		"\xC3\x9Cnal's Mod":                 "unal_s_mod",
		"Stra\xC3\x9Fe":                     "strae",
		"___":                               "",
		"a-b_c d":                           "a_b_c_d",
	}
	for text, slug := range slugs {
		if ToSlug(text) != slug {
			t.Errorf("ToSlug(%q) = %q, want %q", text, ToSlug(text), slug)
		}
	}
}

// TestMatchIgnore checks anchored, negated, folder-only and `**` patterns, the last matching rule winning, and a
// file inside a covered folder.
func TestMatchIgnore(t *testing.T) {
	ignore := ParseIgnore("# comment\n\n*.dds\n!keep.dds\n/thumbnail.png\nbuild/\ndocs/**/*.md\n**/generated\n")
	rows := []struct {
		rel       string
		dir, want bool
	}{
		{"gfx/a.dds", false, true},
		{"gfx/keep.dds", false, false},
		{"thumbnail.png", false, true},
		{"gfx/thumbnail.png", false, false},
		{"build", true, true},
		{"build", false, false},
		{"build/out.txt", false, true},
		{"src/build", true, true},
		{"docs/a/b/c.md", false, true},
		{"docs/c.md", false, true},
		{"x/docs/c.md", false, false},
		{"a/b/generated", true, true},
		{"generated", false, true},
	}
	for _, row := range rows {
		if ignore.Match(row.rel, row.dir) != row.want {
			t.Errorf("Match(%q, %v) = %v, want %v", row.rel, row.dir, !row.want, row.want)
		}
	}
	var none Ignore
	if none.Match("gfx/a.dds", false) {
		t.Error("a nil Ignore matched")
	}
}

// TestWalkRoot checks the walk of the fixture install: every file once in order with its format, the ignore rules
// applied, a dot folder skipped and `.metadata` kept.
func TestWalkRoot(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "ck3", "game"))
	if err != nil {
		t.Fatal(err)
	}
	root := Root{Dir: dir, Origin: "vanilla"}
	list, err := Walk(root, nil)
	if err != nil || len(list) != 61 {
		t.Fatalf("Walk listed %d files, %v, want 61", len(list), err)
	}
	if !slices.IsSortedFunc(list, func(file, other File) int { return strings.Compare(file.Rel, other.Rel) }) {
		t.Error("the walk is not in order")
	}
	rel := "common/coat_of_arms/coat_of_arms/v_coats.txt"
	first := File{Path: Join(dir, rel), Key: ToKey(Join(dir, rel)), Rel: rel, Origin: "vanilla", Format: FormatScript}
	if list[0] != first {
		t.Errorf("first file = %+v, want %+v", list[0], first)
	}
	list, err = Walk(root, ParseIgnore("gfx/\n*.info\n"))
	if err != nil || len(list) != 61-17-3 {
		t.Errorf("Walk with gfx/ and *.info ignored listed %d files, %v, want %d", len(list), err, 61-17-3)
	}
	mod, descriptor := t.TempDir(), ".metadata/metadata.json"
	for _, rel := range []string{".git/HEAD", descriptor, "common/m.txt"} {
		if err := os.MkdirAll(filepath.Dir(Join(mod, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(Join(mod, rel), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err = Walk(Root{Dir: mod, Origin: "mod"}, nil)
	if err != nil || len(list) != 2 || list[0].Rel != descriptor || list[0].Format != FormatDescriptor {
		t.Errorf("Walk over a mod listed %+v, %v, want %s and common/m.txt", list, err, descriptor)
	}
}

// TestWalkInstalls checks, on each real install the environment names, that Walk lists the same files as
// filepath.WalkDir with the same skips.
func TestWalkInstalls(t *testing.T) {
	for _, env := range []string{"PMT_TEST_INSTALL_CK3", "PMT_TEST_INSTALL_VIC3", "PMT_TEST_INSTALL_EU5"} {
		t.Run(env, func(t *testing.T) {
			dir := os.Getenv(env)
			if dir == "" {
				t.Skipf("%s is not set", env)
			}
			list, err := Walk(Root{Dir: dir}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var rels []string
			err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() && path != dir && strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".metadata" {
					return fs.SkipDir
				}
				if rel, ok := ToRel(dir, path); ok && !entry.IsDir() {
					rels = append(rels, rel)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(rels)
			if !slices.EqualFunc(list, rels, func(file File, rel string) bool { return file.Rel == rel }) {
				t.Errorf("Walk listed %d files, filepath.WalkDir %d", len(list), len(rels))
			}
		})
	}
}
