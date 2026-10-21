package app

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

// TestLoadJSON reads a file that is not there and gets the zero value without an error.
func TestLoadJSON(t *testing.T) {
	config, err := LoadJSON[Config](filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || !reflect.DeepEqual(config, Config{}) {
		t.Fatalf("a missing file gave %+v, %v", config, err)
	}
}

// TestSaveJSON writes a config and reads the same one back.
func TestSaveJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	sample := Config{
		Installs:   []Install{{ID: "ck3-steam", Game: "ck3", Path: `C:\Games\ck3`, Version: "1.16"}},
		Workspaces: []Workspace{{ID: "w1", Name: "Mine", Install: "ck3-steam", Mods: []Mod{{ID: "m1", Dir: `C:\Mods\m1`}}}},
		Settings:   map[string]string{"appearance.dark": "false"},
	}
	if err := SaveJSON(path, sample); err != nil {
		t.Fatal(err)
	}
	config, err := LoadJSON[Config](path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, sample) {
		t.Fatalf("the file gave back %+v for %+v", config, sample)
	}
}

// TestWriteAtomic fails a write by aiming it at a folder and checks that no temporary file is left beside it.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(target, []byte("text")); err == nil {
		t.Fatal("a write over a folder passed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the folder holds %d entries after the failed write, want the target alone", len(entries))
	}
}

// TestOpenConfig opens a missing config.json as a writable first run and an unreadable one as read-only.
func TestOpenConfig(t *testing.T) {
	file, err := openConfig(Dirs{App: t.TempDir()})
	if err != nil || file.readOnly {
		t.Fatalf("a first run gave read-only %v, %v", file.readOnly, err)
	}
	// a folder named config.json cannot be read as a file
	dirs := Dirs{App: t.TempDir()}
	if err := os.Mkdir(filepath.Join(dirs.App, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err = openConfig(dirs)
	if err != nil || !file.readOnly {
		t.Fatalf("an unreadable file gave read-only %v, %v", file.readOnly, err)
	}
	if err := file.Update(func(*Config) {}); err == nil {
		t.Fatal("a read-only config was written")
	}
}

// TestUpdateConfig changes the config and finds the change in the file and in the next copy.
func TestUpdateConfig(t *testing.T) {
	file, err := openConfig(Dirs{App: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	err = file.Update(func(config *Config) {
		config.Workspaces = append(config.Workspaces, Workspace{ID: "w1", Name: "Mine"})
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := LoadJSON[Config](file.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Workspaces) != 1 || config.Workspaces[0].ID != "w1" {
		t.Fatalf("the file holds %+v after the update", config.Workspaces)
	}
	if snapshot := file.Config(); len(snapshot.Workspaces) != 1 || snapshot.Workspaces[0].ID != "w1" {
		t.Fatalf("the copy holds %+v after the update", snapshot.Workspaces)
	}
}

// TestListSettings checks that every setting's key is unique and of the form <area>.<noun>, that a number has a
// range and no choices, and that a choice has choices.
func TestListSettings(t *testing.T) {
	app := &Service{Config: &ConfigFile{}}
	shape := regexp.MustCompile(`^[a-z]+(\.[a-z][A-Za-z0-9]*)+$`)
	keys := map[string]bool{}
	for _, setting := range app.ListSettings() {
		if !shape.MatchString(setting.Key) {
			t.Errorf("key %q is not <area>.<noun>", setting.Key)
		}
		if keys[setting.Key] {
			t.Errorf("key %q is listed twice", setting.Key)
		}
		keys[setting.Key] = true
		switch setting.Type {
		case "number":
			if setting.Min >= setting.Max || len(setting.Choices) != 0 {
				t.Errorf("number %q has the range %d to %d and %d choices",
					setting.Key, setting.Min, setting.Max, len(setting.Choices))
			}
		case "choice":
			if len(setting.Choices) == 0 {
				t.Errorf("choice %q has no choices", setting.Key)
			}
		}
	}
}
