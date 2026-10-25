package steam

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// The names the tests use more than once.
const (
	fixtureModID   = "mod-a"
	descriptionRel = "workshop/description.md"
	changedRel     = "changed.txt"
	extraRel       = "extra.txt"
	sameRel        = "same.txt"
)

// appOnce makes the one application the workspace service's events need.
var appOnce sync.Once

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// copyFixture copies a folder under internal/testdata into a temporary folder, so that a test may change its files,
// and returns the copy.
func copyFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, parts...))); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeFiles writes each text to its path under a folder, making the folders on the way.
func writeFiles(t *testing.T, dir string, texts map[string]string) {
	t.Helper()
	for rel, text := range texts {
		file := files.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// openService opens a workspace of the CK3 install at the given path with the given mods, in folders PMT keeps its
// files in under the test's own, and returns the Workshop service over it.
func openService(t *testing.T, install string, mods ...app.Mod) *Service {
	t.Helper()
	appOnce.Do(func() { application.New(application.Options{}) })
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}
	config, err := app.OpenConfig()
	if err != nil {
		t.Fatal(err)
	}
	err = config.Update("", func(records *app.Config) error {
		records.Installs = []app.Install{{ID: "i1", Game: "ck3", Path: install}}
		records.Workspaces = []app.Workspace{{ID: "w1", Name: "Test", Install: "i1", Mods: mods, Language: "english"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wsp := &workspace.Service{Config: config, Build: "test"}
	wsp.Open("w1")
	t.Cleanup(wsp.Close)
	if _, err := wsp.Session(); err != nil {
		t.Fatal(err)
	}
	return &Service{Workspace: wsp}
}

// TestReadListingReadsWorkshopFolder checks that a listing holds the descriptor's title, the description file's
// source and text, the changelog, the previews in order, the DLC ids, and the empty parts as empty.
func TestReadListingReadsWorkshopFolder(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir, WorkshopID: "1234"})
	empty, err := stm.ReadListing(fixtureModID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Source != sourceSteam || empty.Description != "" || empty.DescriptionPath != "" {
		t.Errorf("got source %q with description %q at %q", empty.Source, empty.Description, empty.DescriptionPath)
	}
	if empty.Previews == nil || empty.Tags == nil || empty.DLC == nil {
		t.Errorf("got a nil part in %+v, want empty ones", empty)
	}
	writeFiles(t, dir, map[string]string{
		descriptionRel:                 "# Fixture\n",
		"workshop/changelog.bbcode":    "[b]Fixed[/b]",
		"workshop/previews/b.png":      "b",
		"workshop/previews/a.png":      "a",
		"workshop/previews/videos.txt": "https://youtu.be/abc\n",
		"workshop/dlc.txt":             "1303183\n2071470\n",
	})
	listing, err := stm.ReadListing(fixtureModID)
	if err != nil {
		t.Fatal(err)
	}
	if listing.WorkshopID != "1234" || listing.Title != "Fixture Mod" || listing.Visibility != "" {
		t.Errorf("got item %q titled %q with visibility %q", listing.WorkshopID, listing.Title, listing.Visibility)
	}
	if listing.Source != sourceMarkdown || listing.Description != "# Fixture\n" ||
		listing.DescriptionPath != files.Join(dir, descriptionRel) {
		t.Errorf("got description %q from %q at %q", listing.Description, listing.Source, listing.DescriptionPath)
	}
	if listing.Changelog != "[b]Fixed[/b]" || listing.ChangelogPath != files.Join(dir, changelogFile) {
		t.Errorf("got changelog %q at %q", listing.Changelog, listing.ChangelogPath)
	}
	want := []Preview{
		{Path: files.Join(dir, "workshop/previews/a.png"), Type: previewImage, Order: 0},
		{Path: files.Join(dir, "workshop/previews/b.png"), Type: previewImage, Order: 1},
		{Type: previewVideo, URL: "https://youtu.be/abc", Video: "abc", Order: 2},
	}
	if !reflect.DeepEqual(listing.Previews, want) {
		t.Errorf("got previews %+v, want %+v", listing.Previews, want)
	}
	if !reflect.DeepEqual(listing.DLC, []string{"1303183", "2071470"}) {
		t.Errorf("got DLC %v", listing.DLC)
	}
}

// TestWriteListingRoundTripsListing checks that a listing written through the service is what the next read returns:
// the descriptor's fields, the description in the file made for it and as BBCode, the changelog and the DLC ids.
func TestWriteListingRoundTripsListing(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir})
	if err := stm.CreateDescriptionFile(fixtureModID, "html"); err == nil {
		t.Error("a description in html was accepted")
	}
	if err := stm.CreateDescriptionFile(fixtureModID, sourceMarkdown); err != nil {
		t.Fatal(err)
	}
	listing, err := stm.ReadListing(fixtureModID)
	if err != nil {
		t.Fatal(err)
	}
	listing.Title, listing.Tags, listing.WorkshopID = "Renamed", []string{"Balance", "Historical"}, "99"
	listing.Description, listing.Changelog, listing.DLC = "# New\n", "[b]New[/b]", []string{"1303183"}
	if err := stm.WriteListing(fixtureModID, listing); err != nil {
		t.Fatal(err)
	}
	got, err := stm.ReadListing(fixtureModID)
	if err != nil {
		t.Fatal(err)
	}
	listing.ChangelogPath, listing.BBCode = files.Join(dir, changelogFile), "[h1]New[/h1]"
	if !reflect.DeepEqual(got, listing) {
		t.Errorf("got %+v, want %+v", got, listing)
	}
}

// TestListUploadSplitsFilesByRules checks the upload set against every rule: dotfiles, the .gitignore and what it
// names, the previews and the mod's exclusions, with the .metadata folder sent.
func TestListUploadSplitsFilesByRules(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	writeFiles(t, dir, map[string]string{
		".hidden":                     "",
		".gitignore":                  "*.log\n",
		"notes.log":                   "",
		".git/config":                 "",
		".metadata/metadata.json":     "{}",
		"workshop/previews/p.png":     "p",
		descriptionRel:                "text",
		"secret/key.txt":              "",
		"art/logo.psd":                "",
		"common/mod_things/extra.txt": "x",
	})
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir, Ignore: "secret/\n*.psd\n"})
	upload, err := stm.ListUpload(fixtureModID)
	if err != nil {
		t.Fatal(err)
	}
	wantExcluded := []string{".gitignore", ".hidden", "art/logo.psd", "notes.log", "secret/key.txt", "workshop/previews/p.png"}
	if !reflect.DeepEqual(upload.Excluded, wantExcluded) {
		t.Errorf("got excluded %v, want %v", upload.Excluded, wantExcluded)
	}
	sent := map[string]bool{}
	for _, file := range upload.Files {
		sent[file.Rel] = true
	}
	for _, rel := range []string{".metadata/metadata.json", "descriptor.mod", descriptionRel, "common/mod_things/extra.txt"} {
		if !sent[rel] {
			t.Errorf("%s is not in the upload", rel)
		}
	}
	if sent[".git/config"] {
		t.Error(".git/config is in the upload")
	}
	if upload.Bytes <= 0 {
		t.Errorf("got %d bytes", upload.Bytes)
	}
}

// TestCompareReportsChanges checks that a file only the upload has is added, one with another hash is changed, one
// only the snapshot has is removed, and a listing field that differs is named.
func TestCompareReportsChanges(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{sameRel: "1", changedRel: "2", extraRel: "3"})
	upload := Upload{Files: []files.File{
		{Path: filepath.Join(dir, sameRel), Rel: sameRel},
		{Path: filepath.Join(dir, changedRel), Rel: changedRel},
		{Path: filepath.Join(dir, extraRel), Rel: extraRel},
	}}
	hashes := buildHashes(upload.Files)
	snapshot := Snapshot{
		Files:   map[string]string{sameRel: hashes[sameRel], changedRel: "old", "gone.txt": "x"},
		Listing: Listing{Title: "Old", Tags: []string{"a"}},
	}
	listing := Listing{Title: "New", Tags: []string{"a"}, Description: "text"}
	got := Compare(listing, upload, snapshot, &Details{Title: "On Steam"})
	want := Comparison{
		Added: []string{extraRel}, Changed: []string{changedRel}, Removed: []string{"gone.txt"},
		Listing: []string{"title", "description"}, Last: snapshot.Listing, Steam: &Details{Title: "On Steam"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if first := Compare(listing, upload, Snapshot{}, nil); len(first.Added) != 3 || len(first.Changed) != 0 || len(first.Removed) != 0 {
		t.Errorf("with no snapshot got %+v, want every file added", first)
	}
}

// TestToBBCodeConvertsConstructs checks every construct of the conversion on one text, with escapes and entities
// resolved outside code and kept inside it.
func TestToBBCodeConvertsConstructs(t *testing.T) {
	markdown := "# Title\n\n## Sub\n\n### Third\n\n#### Fourth\n\n" +
		"Some *italic*, **bold**, ~~struck~~ and `co\\*de` with a [link](https://example.com/x) and " +
		"![pic](https://example.com/p.png).\n\n" +
		"Escaped \\*stars\\* and &amp; &copy; entities.\n\n" +
		"- one\n- two\n  - nested\n\n1. first\n2. second\n\n" +
		"> quoted\n\n---\n\n```\nfn(\\*)\n```\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n<div>raw</div>\n\nlast <b>line</b>"
	want := "[h1]Title[/h1]\n\n[h2]Sub[/h2]\n\n[h3]Third[/h3]\n\n[h3]Fourth[/h3]\n\n" +
		"Some [i]italic[/i], [b]bold[/b], [strike]struck[/strike] and [code]co\\*de[/code] with a " +
		"[url=https://example.com/x]link[/url] and [img]https://example.com/p.png[/img].\n\n" +
		"Escaped *stars* and & \u00a9 entities.\n\n" +
		"[list]\n[*] one\n[*] two\n[list]\n[*] nested\n[/list]\n[/list]\n\n[olist]\n[*] first\n[*] second\n[/olist]\n\n" +
		"[quote]quoted\n\n[/quote]\n\n[hr][/hr]\n\n[code]\nfn(\\*)\n[/code]\n\n" +
		"[table]\n[tr][th]a[/th][th]b[/th][/tr]\n[tr][td]1[/td][td]2[/td][/tr]\n[/table]\n\nlast line"
	if got := new(Service).ToBBCode(markdown); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// TestListDLCReadsDescriptors checks that a DLC folder is read from its .dlc or else its .dlc.json, with the Steam
// id of the descriptor and the name from the script or from the localization key, and that one with no id or the id
// 0 is left out.
func TestListDLCReadsDescriptors(t *testing.T) {
	install := copyFixture(t, "ck3")
	writeFiles(t, install, map[string]string{
		"game/dlc/dlc003_fp1/dlc003.dlc":              "name = \"The Northern Lords\"\nsteam_id = \"1303183\"\n",
		"game/dlc/dlc004_ep1/dlc004.dlc":              "name = \"No Id\"\n",
		"game/dlc/D008_phoenix/D008.dlc.json":         "\xef\xbb\xbf{\"name\": \"d008\", \"localizable_name\": \"dlc_phoenix\", \"steam_id\": 3699010}",
		"game/dlc/D009_zero/D009.dlc.json":            "{\"name\": \"d009\", \"steam_id\": 0}",
		"game/dlc/D010_plain/D010.dlc.json":           "{\"name\": \"d010\", \"localizable_name\": \"missing\", \"steam_id\": \"42\"}",
		"game/dlc/D011_nothing/other.txt":             "",
		"game/localization/english/dlc_l_english.yml": "\xef\xbb\xbfl_english:\n dlc_phoenix:0 \"Fate of the Phoenix\"\n",
	})
	dir := copyFixture(t, "mods", "ck3")
	stm := openService(t, install, app.Mod{ID: fixtureModID, Dir: dir})
	got, err := stm.ListDLC()
	if err != nil {
		t.Fatal(err)
	}
	want := []DLC{{ID: "3699010", Name: "Fate of the Phoenix"}, {ID: "42", Name: "d010"}, {ID: "1303183", Name: "The Northern Lords"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
