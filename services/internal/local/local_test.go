// local_test.go covers dropping an install's cache (its files go, another install's stay), the
// write that leaves no half-file and no leftovers, and a mod's own files: the descriptor round
// trip in both formats, where a thumbnail is found and installed, and a new mod's skeleton.

package local

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points the user config directory at a temporary folder, away from the real cache.
func isolate(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestCachePathAndDrop(t *testing.T) {
	isolate(t)
	base, _ := cachePath("steam:1158310", "1.0")
	other, _ := cachePath("steam:529340", "1.0")
	for _, path := range []string{base + ".gob", base + ".schema.txt", other + ".gob"} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := DropInstall("steam:1158310"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base + ".gob"); err == nil {
		t.Error("dropping an install removes every file of it")
	}
	if _, err := os.Stat(base + ".schema.txt"); err == nil {
		t.Error("dropping an install removes its schema text too")
	}
	if _, err := os.Stat(other + ".gob"); err != nil {
		t.Error("dropping an install leaves another install's files")
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt")

	if err := Write(path, func(out io.Writer) error { _, err := io.WriteString(out, "first"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, func(out io.Writer) error { _, err := io.WriteString(out, "second"); return err }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q, want second", got)
	}

	// the directory holds the file and nothing else: the temporary is gone
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.txt" {
		t.Fatalf("leftovers: %v", entries)
	}

	// a failing fill keeps the previous file and leaves nothing behind
	boom := errors.New("boom")
	if err := Write(path, func(out io.Writer) error {
		io.WriteString(out, "half")
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("the old file was lost: %q", got)
	}
	entries, _ = os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("a failed write left something behind: %v", entries)
	}
}

func TestListingFields(t *testing.T) {
	for _, tt := range []struct {
		game, rel, src string
		read           func(fields ListingFields) bool // what is read before the patch; nil checks nothing
		edit           func(fields *ListingFields)
		want           []string // in the patched file
	}{
		{"ck3", "descriptor.mod", `version="1.0"
name="Old"
supported_version="1.16.*"
# keep
picture="thumbnail.png"
tags={
	"Events"
}
`,
			func(fields ListingFields) bool {
				return fields.Name == "Old" && fields.SupportedVersion == "1.16.*" && len(fields.Tags) == 1 && fields.Tags[0] == "Events"
			},
			func(fields *ListingFields) {
				fields.Name, fields.Version, fields.RemoteFileID, fields.Tags = "New", "1.2.0", "99", []string{"Events", "Gameplay"}
			},
			[]string{`name="New"`, `remote_file_id="99"`, "# keep", `"Gameplay"`}},
		{"vic3", ".metadata/metadata.json", `{
  "name": "V",
  "id": "v",
  "version": "0.1",
  "game_id": "victoria3",
  "relationships": []
}
`,
			nil,
			func(fields *ListingFields) {
				fields.Version, fields.ShortDescription, fields.Tags = "0.2.0", "hi", []string{"a"}
			},
			[]string{`"version": "0.2.0"`, `"game_id"`, `"relationships"`}},
	} {
		t.Run(tt.game, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(tt.rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.src), 0o644); err != nil {
				t.Fatal(err)
			}
			fields, err := ReadListingFields(tt.game, root)
			if err != nil || (tt.read != nil && !tt.read(fields)) {
				t.Fatalf("%+v (%v)", fields, err)
			}
			tt.edit(&fields)
			if err := WriteListingFields(tt.game, root, fields); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(raw), want) {
					t.Fatalf("no %s in %s", want, raw)
				}
			}
		})
	}
}

func TestThumbnail(t *testing.T) {
	for _, tt := range []struct {
		what, game string
		put        []string // the files the mod already has
		copied     string   // the extension of a picture CopyThumbnail installs; none asks FindThumbnail
		want       string
	}{
		{"ck3.png", "ck3", nil, ".png", "thumbnail.png"},
		{"vic3.png", "vic3", nil, ".png", "thumbnail.png"},
		{"eu5.png", "eu5", nil, ".png", ".metadata/thumbnail.png"},
		{"eu5.gif", "eu5", nil, ".gif", ".metadata/thumbnail.gif"},
		{".png", "", nil, ".png", "thumbnail.png"},
		// A mod that already made a choice keeps it, whatever the game's convention; moving it
		// would leave two thumbnail files and the descriptor naming the stale one.
		{"eu5 mod that uses the root", "eu5", []string{"thumbnail.png"}, "", "thumbnail.png"},
		{"vic3 mod that uses .metadata", "vic3", []string{".metadata/thumbnail.png"}, "", ".metadata/thumbnail.png"},
		{"a gif, which the corpus ships", "ck3", []string{"thumbnail.gif"}, "", "thumbnail.gif"},
		// A mod carrying both resolves to the one the game itself reads, not to whichever
		// the walk happened to reach first.
		{"vic3 with both", "vic3", []string{"thumbnail.png", ".metadata/thumbnail.png"}, "", "thumbnail.png"},
		{"eu5 with both", "eu5", []string{"thumbnail.png", ".metadata/thumbnail.png"}, "", ".metadata/thumbnail.png"},
	} {
		t.Run(tt.what, func(t *testing.T) {
			root := t.TempDir()
			for _, rel := range tt.put {
				path := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.copied == "" {
				if got, found := FindThumbnail(tt.game, root, ""); !found || got != tt.want {
					t.Errorf("FindThumbnail = %q, %v, want %q", got, found, tt.want)
				}
				return
			}
			src := filepath.Join(t.TempDir(), "art"+tt.copied)
			if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if got, err := CopyThumbnail(tt.game, root, src); err != nil || got != tt.want {
				t.Errorf("CopyThumbnail = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestWriteNewMod(t *testing.T) {
	for _, tt := range []struct {
		what, game, name, version, lang string
		thumbnail                       string // the extension of a picture the mod starts with, or none
		descriptor, loc                 string
		has, hasNot                     []string // in the descriptor
		files                           []string
	}{
		{"ck3", "ck3", "My Cool Mod", "1.16.2", "english", "", "descriptor.mod", "localization/english/my_cool_mod_l_english.yml",
			[]string{`name="My Cool Mod"`, `supported_version="1.16.2"`}, nil, []string{"common", "events"}},
		{"vic3 omits latest", "vic3", "V", "latest", "french", "", ".metadata/metadata.json", "localization/french/v_l_french.yml",
			[]string{`"name": "V"`}, []string{"supported_game_version"}, nil},
		// EU5 writes the file into .metadata/ and still declares the bare name as its picture.
		{"eu5 thumbnail goes beside metadata", "eu5", "M", "1", "english", ".gif", ".metadata/metadata.json", "main_menu/localization/english/m_l_english.yml",
			[]string{`"picture": "thumbnail.gif"`}, nil, []string{".metadata/thumbnail.gif", "in_game/common", "in_game/events"}},
		{"ck3 thumbnail", "ck3", "M", "1", "english", ".png", "descriptor.mod", "localization/english/m_l_english.yml",
			[]string{`picture="thumbnail.png"`}, nil, []string{"thumbnail.png"}},
	} {
		t.Run(tt.what, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "mod")
			opts := NewModOpts{GameID: tt.game, Root: root, Name: tt.name, SupportedVersion: tt.version, LocLang: tt.lang}
			if tt.thumbnail != "" {
				opts.ThumbnailSrc = filepath.Join(t.TempDir(), "art"+tt.thumbnail)
				if err := os.WriteFile(opts.ThumbnailSrc, []byte(tt.thumbnail), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := WriteNewMod(opts); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tt.descriptor)))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.has {
				if !strings.Contains(string(raw), want) {
					t.Errorf("no %s in the descriptor: %s", want, raw)
				}
			}
			for _, not := range tt.hasNot {
				if strings.Contains(string(raw), not) {
					t.Errorf("%s in the descriptor: %s", not, raw)
				}
			}
			for _, rel := range tt.files {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Error(err)
				}
			}
			stub, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tt.loc)))
			if err != nil || !strings.HasPrefix(string(stub), "\xEF\xBB\xBF") || !strings.Contains(string(stub), "l_"+tt.lang+":") {
				t.Errorf("the loc stub, with a BOM and its header: %q (%v)", stub, err)
			}
			if err := WriteNewMod(opts); err == nil {
				t.Error("an existing mod is refused")
			}
		})
	}
}
