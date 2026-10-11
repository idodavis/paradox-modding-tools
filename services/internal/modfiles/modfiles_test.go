// modfiles_test.go covers a mod's own files: the descriptor round trip in both formats, where a thumbnail is
// found and installed, and a new mod's skeleton.

package modfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
)

// The names the fixtures repeat.
const (
	gameCK3           = "ck3"
	gameVic3          = "vic3"
	gameEU5           = "eu5"
	rootThumbnail     = "thumbnail.png"
	extPNG            = ".png"
	langEnglish       = "english"
	metadataFile      = ".metadata/metadata.json"
	metadataThumbnail = ".metadata/thumbnail.png"
)

// patchedDescriptor writes src as a mod's descriptor, loads it, patches it through edit, and returns the fields
// loaded and the descriptor's text after the patch.
func patchedDescriptor(t *testing.T, gameID, src string, edit func(*ListingFields)) (ListingFields, string) {
	t.Helper()
	root := t.TempDir()
	path := game.DescriptorPath(gameID, root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fields, err := LoadDescriptor(gameID, root)
	if err != nil {
		t.Fatalf("%+v (%v)", fields, err)
	}
	loaded := fields
	edit(&fields)
	if err := WriteDescriptor(gameID, root, fields); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return loaded, string(raw)
}

func TestDescriptorMod(t *testing.T) {
	loaded, patched := patchedDescriptor(t, gameCK3, `version="1.0"
name="Old"
supported_version="1.16.*"
# keep
picture="thumbnail.png"
tags={
	"Events"
}
`, func(fields *ListingFields) {
		fields.Name, fields.Version, fields.RemoteFileID, fields.Tags = "New", "1.2.0", "99", []string{"Events", "Gameplay"}
	})
	if loaded.Name != "Old" || loaded.SupportedVersion != "1.16.*" || len(loaded.Tags) != 1 || loaded.Tags[0] != "Events" {
		t.Fatalf("%+v", loaded)
	}
	for _, want := range []string{`name="New"`, `remote_file_id="99"`, "# keep", `"Gameplay"`} {
		if !strings.Contains(patched, want) {
			t.Fatalf("no %s in %s", want, patched)
		}
	}
}

func TestDescriptorMetadataJSON(t *testing.T) {
	_, patched := patchedDescriptor(t, gameVic3, `{
  "name": "V",
  "id": "v",
  "version": "0.1",
  "game_id": "victoria3",
  "relationships": []
}
`, func(fields *ListingFields) {
		fields.Version, fields.ShortDescription, fields.Tags = "0.2.0", "hi", []string{"a"}
	})
	for _, want := range []string{`"version": "0.2.0"`, `"game_id"`, `"relationships"`} {
		if !strings.Contains(patched, want) {
			t.Fatalf("no %s in %s", want, patched)
		}
	}
}

func TestThumbnail(t *testing.T) {
	for _, tt := range []struct {
		what, game string
		put        []string // the files the mod already has
		copied     string   // the extension of a picture CopyThumbnail installs; none asks Thumbnail
		want       string
	}{
		{"ck3.png", gameCK3, nil, extPNG, rootThumbnail},
		{"vic3.png", gameVic3, nil, extPNG, rootThumbnail},
		{"eu5.png", gameEU5, nil, extPNG, metadataThumbnail},
		{"eu5.gif", gameEU5, nil, ".gif", ".metadata/thumbnail.gif"},
		{extPNG, "", nil, extPNG, rootThumbnail},
		// A mod that already made a choice keeps it, whatever the game's convention; moving it
		// would leave two thumbnail files and the descriptor naming the stale one.
		{"eu5 mod that uses the root", gameEU5, []string{rootThumbnail}, "", rootThumbnail},
		{"vic3 mod that uses .metadata", gameVic3, []string{metadataThumbnail}, "", metadataThumbnail},
		{"a gif, which the corpus ships", gameCK3, []string{"thumbnail.gif"}, "", "thumbnail.gif"},
		// A mod carrying both resolves to the one the game itself reads, not to whichever
		// the walk happened to reach first.
		{"vic3 with both", gameVic3, []string{rootThumbnail, metadataThumbnail}, "", rootThumbnail},
		{"eu5 with both", gameEU5, []string{rootThumbnail, metadataThumbnail}, "", metadataThumbnail},
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
				if got, known := Thumbnail(tt.game, root, ""); !known || got != tt.want {
					t.Errorf("Thumbnail = %q, %v, want %q", got, known, tt.want)
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

func TestCreate(t *testing.T) {
	for _, tt := range []struct {
		what, game, name, version, lang string
		thumbnail                       string // the extension of a picture the mod starts with, or none
		descriptor, loc                 string
		has, hasNot                     []string // in the descriptor
		files                           []string
	}{
		{gameCK3, gameCK3, "My Cool Mod", "1.16.2", langEnglish, "", "descriptor.mod", "localization/english/my_cool_mod_l_english.yml",
			[]string{`name="My Cool Mod"`, `supported_version="1.16.2"`}, nil, []string{"common", "events"}},
		{"vic3 omits latest", gameVic3, "V", "latest", "french", "", metadataFile, "localization/french/v_l_french.yml",
			[]string{`"name": "V"`}, []string{"supported_game_version"}, nil},
		// EU5 writes the file into .metadata/ and still declares the bare name as its picture.
		{"eu5 thumbnail goes beside metadata", gameEU5, "M", "1", langEnglish, ".gif", metadataFile, "main_menu/localization/english/m_l_english.yml",
			[]string{`"picture": "thumbnail.gif"`}, nil, []string{".metadata/thumbnail.gif", "in_game/common", "in_game/events"}},
		{"ck3 thumbnail", gameCK3, "M", "1", langEnglish, extPNG, "descriptor.mod", "localization/english/m_l_english.yml",
			[]string{`picture="thumbnail.png"`}, nil, []string{rootThumbnail}},
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
			if err := Create(opts); err != nil {
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
			if err := Create(opts); err == nil {
				t.Error("an existing mod is refused")
			}
		})
	}
}
