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
	descriptionRel = ".workshop/description.bbcode"
	changedRel     = "changed.txt"
	extraRel       = "extra.txt"
	goneRel        = "gone.txt"
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

// copyFixture copies a folder under internal/testdata into a temporary folder and returns the copy.
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

// openService opens a workspace of a CK3 install with some mods and returns the Workshop service over it. PMT keeps
// its files in the test's own folders.
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
	// Steam's tags are kept already, so no read of a listing fetches them
	kept := Tags{Categories: []string{"Balance"}, Versions: []string{}}
	if err := app.SaveJSON(resolveTagsPath(config.Dirs.Cache, 1158310), kept); err != nil {
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

// TestReadListingReadsWorkshopFolder checks that a listing holds what its descriptor and .workshop folder say, with
// empty parts for missing files.
func TestReadListingReadsWorkshopFolder(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir, WorkshopID: "1234"})
	empty, err := stm.ReadListing(fixtureModID, "")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Description != "" || empty.Changelog != "" {
		t.Errorf("got description %q and changelog %q", empty.Description, empty.Changelog)
	}
	if empty.Previews == nil || empty.Tags == nil || empty.DLC == nil {
		t.Errorf("got a nil part in %+v, want empty ones", empty)
	}
	writeFiles(t, dir, map[string]string{
		descriptionRel:                  "[h1]Fixture[/h1]\n",
		".workshop/changelog.bbcode":    "[b]Fixed[/b]",
		".workshop/previews/b.png":      "b",
		".workshop/previews/a.png":      "a",
		".workshop/previews/videos.txt": "https://youtu.be/abc\n",
		".workshop/dlc.txt":             "1303183\n2071470\n",
	})
	listing, err := stm.ReadListing(fixtureModID, "")
	if err != nil {
		t.Fatal(err)
	}
	if listing.WorkshopID != "1234" || listing.Title != "Fixture Mod" {
		t.Errorf("got item %q titled %q", listing.WorkshopID, listing.Title)
	}
	if listing.Description != "[h1]Fixture[/h1]\n" || listing.Changelog != "[b]Fixed[/b]" {
		t.Errorf("got description %q and changelog %q", listing.Description, listing.Changelog)
	}
	want := []Preview{
		{Path: files.Join(dir, ".workshop/previews/a.png"), Type: previewImage, Bytes: 1},
		{Path: files.Join(dir, ".workshop/previews/b.png"), Type: previewImage, Bytes: 1},
		{Type: previewVideo, URL: "https://youtu.be/abc", Video: "abc"},
	}
	if !reflect.DeepEqual(listing.Previews, want) {
		t.Errorf("got previews %+v, want %+v", listing.Previews, want)
	}
	if !reflect.DeepEqual(listing.DLC, []string{"1303183", "2071470"}) {
		t.Errorf("got DLC %v", listing.DLC)
	}
}

// TestWriteListingRoundTripsListing checks that the next read of a listing returns what was written.
func TestWriteListingRoundTripsListing(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir})
	listing, err := stm.ReadListing(fixtureModID, "")
	if err != nil {
		t.Fatal(err)
	}
	listing.Title, listing.Tags = "Renamed", []string{"Balance", "Historical"}
	listing.Description, listing.Changelog, listing.DLC = "[h1]New[/h1]\n", "[b]New[/b]", []string{"1303183"}
	if err := stm.WriteListing(fixtureModID, "", listing); err != nil {
		t.Fatal(err)
	}
	got, err := stm.ReadListing(fixtureModID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, listing) {
		t.Errorf("got %+v, want %+v", got, listing)
	}
}

// TestListUploadSplitsFilesByRules checks that an upload leaves out what the rules, the .gitignore and the mod's
// exclusions cover. The .metadata folder is sent.
func TestListUploadSplitsFilesByRules(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	writeFiles(t, dir, map[string]string{
		".hidden":                     "",
		".gitignore":                  "*.log\n",
		"notes.log":                   "",
		".git/config":                 "",
		".metadata/metadata.json":     "{}",
		".workshop/previews/p.png":    "p",
		descriptionRel:                "text",
		"secret/key.txt":              "",
		"art/logo.psd":                "",
		"common/mod_things/extra.txt": "x",
	})
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir, Ignore: "secret/\n*.psd\n"})
	upload, err := stm.ListUpload(fixtureModID, "")
	if err != nil {
		t.Fatal(err)
	}
	wantExcluded := []string{".gitignore", ".hidden", "art/logo.psd", "notes.log", "secret/key.txt"}
	if !reflect.DeepEqual(upload.Excluded, wantExcluded) {
		t.Errorf("got excluded %v, want %v", upload.Excluded, wantExcluded)
	}
	sent := map[string]bool{}
	for _, file := range upload.Files {
		sent[file.Rel] = true
	}
	for _, rel := range []string{".metadata/metadata.json", "descriptor.mod", "common/mod_things/extra.txt"} {
		if !sent[rel] {
			t.Errorf("%s is not in the upload", rel)
		}
	}
	for _, rel := range []string{".git/config", descriptionRel, ".workshop/previews/p.png"} {
		if sent[rel] {
			t.Errorf("%s is in the upload", rel)
		}
	}
	if upload.Bytes <= 0 {
		t.Errorf("got %d bytes", upload.Bytes)
	}
}

// TestCompareReportsChanges checks that a comparison lists a changed version and the added, changed and removed files.
func TestCompareReportsChanges(t *testing.T) {
	dir := copyFixture(t, "mods", "ck3")
	stm := openService(t, resolveFixture(t, "ck3"), app.Mod{ID: fixtureModID, Dir: dir})
	ws, mod, err := stm.findMod(fixtureModID)
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{sameRel: "1", changedRel: "2\n", extraRel: "3"})
	upload := Upload{Files: []files.File{
		{Path: filepath.Join(dir, sameRel), Rel: sameRel},
		{Path: filepath.Join(dir, changedRel), Rel: changedRel},
		{Path: filepath.Join(dir, extraRel), Rel: extraRel},
	}}
	hashes := buildHashes(upload.Files)
	steamDir := resolveSnapshotDir(stm.Workspace.Config.Dirs.Cache, "1")
	writeFiles(t, steamDir, map[string]string{"descriptor.mod": "version=\"0.9\"\n", changedRel: "old\n", goneRel: "x"})
	err = SaveSnapshot(stm.Workspace.Config.Dirs.Cache, Snapshot{
		WorkshopID: "1",
		Files:      map[string]string{sameRel: hashes[sameRel], changedRel: "old", goneRel: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := stm.compareFiles(ws.Game, mod, Listing{WorkshopID: "1", Version: "1.0"}, upload)
	if err != nil {
		t.Fatal(err)
	}
	want := []Difference{
		{Field: "version", Status: "changed", Before: "0.9", After: "1.0"},
		{Field: "file", Rel: changedRel, Status: "changed", Gained: 1, Lost: 1},
		{Field: "file", Rel: extraRel, Status: "added", Gained: 1},
		{Field: "file", Rel: goneRel, Status: "removed", Lost: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if status := toStatus(Comparison{Files: got}); status != statusChanged {
		t.Errorf("got the status %q, want %q", status, statusChanged)
	}
}

// TestListDLCReadsDescriptors checks that each DLC is read from its .dlc or .dlc.json file, and that one with no Steam
// id is left out.
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
