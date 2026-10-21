// Package app is the app's own service and its files on this machine: PMT's folders, config.json with the
// catalogue of settings, the JSON and gob files, and the atomic write.
package app

import (
	"sync"
	"time"
)

// Dirs says where PMT keeps its files on this machine.
type Dirs struct {
	App   string // config.json
	Cache string // the schema and index caches, the wiki's pages, the Steam snapshots
	Log   string // pmt.log
}

// ResolveDirs finds PMT's folders by the platform's conventions and creates any that are missing.
func ResolveDirs() (Dirs, error) { panic("stub") }

// LoadJSON reads a JSON file into a value. A missing file gives the zero value and no error.
func LoadJSON[T any](path string) (T, error) { panic("stub") }

// SaveJSON writes a value as indented JSON, atomically.
func SaveJSON(path string, value any) error { panic("stub") }

// LoadGob reads a gob cache into a value. It returns false if the cache is missing or cannot be read.
func LoadGob[T any](path string) (T, bool) { panic("stub") }

// SaveGob writes a value as a gob cache, atomically.
func SaveGob(path string, value any) error { panic("stub") }

// WriteAtomic writes bytes to a temporary file beside the target and renames it over, so a reader never sees a
// partial file.
func WriteAtomic(path string, bytes []byte) error { panic("stub") }

// An Install is one game install PMT reads: its id, game and folder, and what the last scan found.
type Install struct {
	ID        string    `json:"id"`
	Game      string    `json:"game"`
	Path      string    `json:"path"`
	Version   string    `json:"version"`
	ScannedAt time.Time `json:"scannedAt"`
}

// An Era is one era of a mod's calendar: its name and the year it starts.
type Era struct {
	Name string `json:"name"`
	Year int    `json:"year"`
}

// A Calendar is how a mod counts years and names months.
type Calendar struct {
	Eras   []Era    `json:"eras"`
	Months []string `json:"months,omitempty"` // twelve names, or none for the game's
}

// A Mod is one mod of a workspace: its folder and what the user set about it.
type Mod struct {
	ID         string   `json:"id"`
	Dir        string   `json:"dir"`
	Color      string   `json:"color"` // a slot `o1`…`o16`, or a CSS color
	Calendar   Calendar `json:"calendar"`
	WorkshopID string   `json:"workshopId"` // "" unpublished
	Ignore     string   `json:"ignore"`     // the upload exclusions, one pattern per line
}

// A Workspace is one workspace: an install and the mods loaded with it, in load order.
type Workspace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Install  string `json:"install"`
	Mods     []Mod  `json:"mods"`
	Language string `json:"language"` // the localization language beside the game's base language
	HomePage string `json:"homePage"` // the page it opens on
}

// Config is the content of config.json: the installs, the workspaces and the settings' values by key.
type Config struct {
	Installs   []Install         `json:"installs"`
	Workspaces []Workspace       `json:"workspaces"`
	Settings   map[string]string `json:"settings"`
}

// A Setting is one row of the catalogue: what a setting is, where it is shown and what it takes.
type Setting struct {
	Key     string   `json:"key"` // `<area>.<noun>`
	Label   string   `json:"label"`
	Help    string   `json:"help"`
	Section string   `json:"section"` // general, install, mods, appearance, ide
	Type    string   `json:"type"`    // switch, choice, number, text
	Default string   `json:"default"`
	Choices []string `json:"choices,omitempty"`
	Words   string   `json:"words"` // what the palette's search also matches
}

// Catalogue lists every setting.
var Catalogue = []Setting{}

// A ConfigFile holds config.json for every service: one lock, one copy, one writer. A missing file means a first
// run. Any other failure to read it makes the config empty and read-only, so a passing fault never overwrites the
// real file.
type ConfigFile struct {
	Dirs     Dirs // the folders PMT keeps its files in, resolved once with the config
	path     string
	mu       sync.RWMutex
	config   Config
	readOnly bool
}

// openConfig loads config.json from the app folder.
func openConfig(dirs Dirs) (*ConfigFile, error) { panic("stub") }

// Config returns a copy of the config as it stands.
func (file *ConfigFile) Config() Config { panic("stub") }

// Update applies a change to the config under the lock and saves it. It returns the write's error, or an error
// when the config is read-only.
func (file *ConfigFile) Update(change func(config *Config)) error { panic("stub") }
