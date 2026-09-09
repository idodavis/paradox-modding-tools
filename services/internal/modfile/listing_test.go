package modfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescriptorModRoundTrip(t *testing.T) {
	root := t.TempDir()
	src := `version="1.0"
name="Old"
supported_version="1.16.*"
# keep
picture="thumbnail.png"
tags={
	"Events"
}
`
	if err := os.WriteFile(filepath.Join(root, "descriptor.mod"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadListingFields("ck3", root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Old" || got.SupportedVersion != "1.16.*" ||
		len(got.Tags) != 1 || got.Tags[0] != "Events" {
		t.Fatalf("%+v", got)
	}
	got.Name = "New"
	got.Version = "1.2.0"
	got.RemoteFileID = "99"
	got.Tags = []string{"Events", "Gameplay"}
	if err := WriteListingFields("ck3", root, got); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "descriptor.mod"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `name="New"`) ||
		!strings.Contains(body, `remote_file_id="99"`) ||
		!strings.Contains(body, "# keep") ||
		!strings.Contains(body, `"Gameplay"`) {
		t.Fatalf("%s", body)
	}
}

func TestMetadataJSONRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".metadata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `{
  "name": "V",
  "id": "v",
  "version": "0.1",
  "game_id": "victoria3",
  "relationships": []
}
`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadListingFields("vic3", root)
	if err != nil {
		t.Fatal(err)
	}
	got.Version = "0.2.0"
	got.ShortDescription = "hi"
	got.Tags = []string{"a"}
	if err := WriteListingFields("vic3", root, got); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"version": "0.2.0"`) ||
		!strings.Contains(body, `"game_id"`) ||
		!strings.Contains(body, `"relationships"`) {
		t.Fatalf("%s", body)
	}
}

func TestEnsureDescriptionsCreatesMissingOnly(t *testing.T) {
	root := t.TempDir()
	if err := EnsureDescriptions(root, "M", "desc"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DescMdName), []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDescriptions(root, "M", "other"); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(root, DescMdName))
	if err != nil {
		t.Fatal(err)
	}
	if string(md) != "kept\n" {
		t.Fatalf("%q", md)
	}
}

func TestBumpVersion(t *testing.T) {
	if BumpPatch("1.2.3") != "1.2.4" {
		t.Fatal(BumpPatch("1.2.3"))
	}
	if BumpMinor("1.2.3") != "1.3.0" {
		t.Fatal(BumpMinor("1.2.3"))
	}
}

func TestModSlug(t *testing.T) {
	if got := ModSlug("My Cool Mod"); got != "my_cool_mod" {
		t.Fatalf("slug: %q", got)
	}
	if got := ModSlug("   "); got != "mod" {
		t.Fatalf("empty: %q", got)
	}
}

func TestWriteNewModCK3(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my_cool_mod")
	if err := WriteNewMod(NewModOpts{
		GameID: "ck3", Root: root, Name: "My Cool Mod",
		SupportedVersion: "1.16.2", LocLang: "english",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "descriptor.mod"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `name="My Cool Mod"`) ||
		!strings.Contains(body, `supported_version="1.16.2"`) {
		t.Fatalf("descriptor: %s", body)
	}
	if _, err := os.Stat(filepath.Join(root, "common")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "events")); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(root, DescMdName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "# My Cool Mod") ||
		!strings.Contains(string(md), "My Cool Mod") {
		t.Fatalf("mod-description.md: %s", md)
	}
	bb, err := os.ReadFile(filepath.Join(root, DescBbName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bb), "[b]My Cool Mod[/b]") {
		t.Fatalf("mod-description.bbcode: %s", bb)
	}
	loc := filepath.Join(root, "localization", "english", "my_cool_mod_l_english.yml")
	locRaw, err := os.ReadFile(loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(locRaw) < 3 || locRaw[0] != 0xEF || locRaw[1] != 0xBB || locRaw[2] != 0xBF {
		t.Fatalf("want BOM, got %q", locRaw)
	}
	if !strings.Contains(string(locRaw), "l_english:") {
		t.Fatalf("loc: %q", locRaw)
	}
	if err := WriteNewMod(NewModOpts{
		GameID: "ck3", Root: root, Name: "My Cool Mod", LocLang: "english",
	}); err == nil {
		t.Fatal("want already-exists")
	}
}

func TestWriteNewModVic3OmitsLatest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "v")
	if err := WriteNewMod(NewModOpts{
		GameID: "vic3", Root: root, Name: "V",
		SupportedVersion: "latest", LocLang: "french", Description: "A desc",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".metadata", "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"name": "V"`) {
		t.Fatalf("meta: %s", s)
	}
	if strings.Contains(s, "supported_game_version") {
		t.Fatalf("latest must omit pin: %s", s)
	}
	md, err := os.ReadFile(filepath.Join(root, DescMdName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "A desc") {
		t.Fatalf("mod-description.md: %s", md)
	}
	loc := filepath.Join(root, "localization", "french", "v_l_french.yml")
	if _, err := os.Stat(loc); err != nil {
		t.Fatal(err)
	}
}

// Where a thumbnail lives and what descriptor a game uses are independent facts.
// PMT keyed the first off the second, which was wrong for all of Victoria 3: it
// writes metadata.json AND puts the thumbnail at the mod root, and not one of
// the 14 Vic3 workshop mods measured keeps it in .metadata alone.
func TestThumbnailPathConvention(t *testing.T) {
	cases := []struct {
		game, ext, want string
	}{
		{"ck3", ".png", "thumbnail.png"},
		{"vic3", ".png", "thumbnail.png"},
		{"eu5", ".png", ".metadata/thumbnail.png"},
		{"eu5", ".gif", ".metadata/thumbnail.gif"},
		{"ck3", "", "thumbnail.png"},
		{"", ".png", "thumbnail.png"},
	}
	for _, c := range cases {
		t.Run(c.game+c.ext, func(t *testing.T) {
			got, exists := ThumbnailPath(c.game, t.TempDir(), c.ext)
			if exists {
				t.Error("empty mod reported an existing thumbnail")
			}
			if got != c.want {
				t.Errorf("ThumbnailPath = %q, want %q", got, c.want)
			}
		})
	}
}

// A mod that already made a choice keeps it, whatever the game's convention.
// Rewriting to the convention left Vic3 and EU5 mods with two thumbnail files
// and the descriptor naming the stale one.
func TestThumbnailPathAdoptsWhatTheModHas(t *testing.T) {
	cases := []struct {
		name, game, put, want string
	}{
		{"eu5 mod that uses the root", "eu5", "thumbnail.png", "thumbnail.png"},
		{"vic3 mod that uses .metadata", "vic3", ".metadata/thumbnail.png", ".metadata/thumbnail.png"},
		{"a gif, which the corpus ships", "ck3", "thumbnail.gif", "thumbnail.gif"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, filepath.FromSlash(c.put))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			got, exists := ThumbnailPath(c.game, root, ".png")
			if !exists {
				t.Fatalf("ThumbnailPath did not find %s", c.put)
			}
			if got != c.want {
				t.Errorf("ThumbnailPath = %q, want %q", got, c.want)
			}
		})
	}
}

// A mod carrying both — 4 of 14 Vic3, 2 of 13 EU5 — resolves to the one the
// game itself reads, not to whichever the walk happened to reach first.
func TestThumbnailPathPrefersTheGamesOwnDirectory(t *testing.T) {
	for _, c := range []struct{ game, want string }{
		{"vic3", "thumbnail.png"},
		{"eu5", ".metadata/thumbnail.png"},
	} {
		t.Run(c.game, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".metadata"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, rel := range []string{"thumbnail.png", ".metadata/thumbnail.png"} {
				if err := os.WriteFile(
					filepath.Join(root, filepath.FromSlash(rel)), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got, _ := ThumbnailPath(c.game, root, ".png"); got != c.want {
				t.Errorf("ThumbnailPath = %q, want %q", got, c.want)
			}
		})
	}
}

// EU5 writes the file into .metadata/ and still declares `"picture":
// "thumbnail.png"` — every one of the 31 workshop mods measured declares a bare
// file name. The path written and the value declared are two different strings.
func TestWriteNewModEU5ThumbnailGoesBesideMetadata(t *testing.T) {
	src := filepath.Join(t.TempDir(), "art.gif")
	if err := os.WriteFile(src, []byte("gif"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "m")
	if err := WriteNewMod(NewModOpts{
		GameID: "eu5", Root: root, Name: "M",
		SupportedVersion: "1", LocLang: "english", ThumbnailSrc: src,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".metadata", "thumbnail.gif")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".metadata", "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"picture": "thumbnail.gif"`) {
		t.Fatalf("picture must be the bare name, not a path: %s", raw)
	}
}

func TestWriteNewModThumbnail(t *testing.T) {
	src := filepath.Join(t.TempDir(), "art.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "m")
	if err := WriteNewMod(NewModOpts{
		GameID: "ck3", Root: root, Name: "M",
		SupportedVersion: "1", LocLang: "english", ThumbnailSrc: src,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "thumbnail.png")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "descriptor.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `picture="thumbnail.png"`) {
		t.Fatalf("descriptor: %s", raw)
	}
}

func TestDefaultModParent(t *testing.T) {
	p := DefaultModParent("ck3")
	if p == "" || !strings.Contains(p, "Crusader Kings III") ||
		filepath.Base(p) != "mod" {
		t.Fatalf("parent: %q", p)
	}
	if DefaultModParent("nope") != "" {
		t.Fatal("unknown game")
	}
}
