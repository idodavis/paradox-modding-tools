// Package health is the Health page's service. It builds the report over the open workspace: the contests between
// mods, the overrides of the game, the dependencies between mods, the dangling references, the localization and
// script problems, the unused definitions and the game's error log. It also counts each mod, compares two
// statements, and writes a translation scaffold. Everything reads the index and the walk's visitors; nothing here
// walks a file on its own.
package health

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// EventErrorLogChanged is sent when the game's error log grows while the game runs.
const EventErrorLogChanged = "errorlog:changed"

func init() { application.RegisterEvent[struct{}](EventErrorLogChanged) }

// Service is the Health service. It diagnoses script through the editor service, so Health and the editor give
// one verdict, and it watches the game's error log while the game runs.
type Service struct {
	Workspace *workspace.Service
	Editor    *lsp.Service // diagnoses each mod file for the script section
	stop      func()       // ends the error log's watch; nil while none runs
}

// ServiceShutdown stops the error log's watch.
func (hlt *Service) ServiceShutdown() error { panic("stub") }

// A Section is one card of the Health page, with its count of findings.
type Section struct {
	ID    string `json:"id"` // contests, overrides, dependencies, dangling, localization, script, unused, errorlog
	Label string `json:"label"`
	Note  string `json:"note"`
	Count int    `json:"count"`
}

// Sections lists every Health section in the page's order.
var Sections = []Section{}

// A FindingSite is one place a finding points at, with whether the game keeps that definition and why.
type FindingSite struct {
	Place  workspace.Place `json:"place"`
	Line   int             `json:"line"`
	Span   jomini.Span     `json:"span"`
	Kept   bool            `json:"kept"`
	Reason string          `json:"reason,omitempty"` // order, mode, priority, merge, replace
}

// A Finding is one row of the Health page: its section, its code, the name and database it concerns, the facts
// its message is written from, and the places it points at.
type Finding struct {
	Section  string            `json:"section"`
	Code     string            `json:"code"` // an lsp.Codes id, or the section's own: contest, override, depends, affects, unused, game-error
	Name     string            `json:"name"`
	Database string            `json:"database"`
	Facts    map[string]string `json:"facts"`
	Sites    []FindingSite     `json:"sites"`
}

// A Report is everything the Health page shows for one load order.
type Report struct {
	Sections []Section `json:"sections"`
	Findings []Finding `json:"findings"`
}

// An Overview counts one mod's definitions by database, its files and its messages.
type Overview struct {
	Mod      string         `json:"mod"`
	Counts   map[string]int `json:"counts"`
	Files    int            `json:"files"`
	Messages int            `json:"messages"`
}

// Compared is the texts of two statements, for the compare view to draw side by side.
type Compared struct {
	A string `json:"a"`
	B string `json:"b"`
}

// BuildReport builds the Health report for the mods in the given load order, or the workspace's own order when
// none is given. A different order is only read as if loaded that way; nothing is rescanned, so the page can
// preview a dragged order without saving it.
func (hlt *Service) BuildReport(order []string) (Report, error) { panic("stub") }

// ListSections returns every Health section.
func (hlt *Service) ListSections() []Section { panic("stub") }

// ListCodes returns every problem code, whose message template the page fills from a finding's facts.
func (hlt *Service) ListCodes() []lsp.Code { panic("stub") }

// ListOverview counts the definitions, files and messages of each mod in the workspace.
func (hlt *Service) ListOverview() ([]Overview, error) { panic("stub") }

// CompareStatements returns the texts of two definitions' statements, for the compare view.
func (hlt *Service) CompareStatements(a, b index.Site) (Compared, error) { panic("stub") }

// WriteScaffold writes the localization files a mod is missing for a language, with the base language's text as
// the placeholder for each missing key. It emits fs:changed for each file and returns the paths written.
func (hlt *Service) WriteScaffold(modID, language string) ([]string, error) { panic("stub") }

// listContests lists each name that two mods both define, saying which one the game keeps and whether the first
// or the last loaded wins.
func listContests(ws *workspace.Session) []Finding { panic("stub") }

// listOverrides lists each name a mod defines over the game's definition.
func listOverrides(ws *workspace.Session) []Finding { panic("stub") }

// listDependencies lists, for each mod, the mods it depends on and the mods it affects, found by comparing a kept
// definition's statement with the one it hides.
func listDependencies(ws *workspace.Session) []Finding { panic("stub") }

// listDangling lists each name a mod refers to that nothing defines, one row per name.
func listDangling(ws *workspace.Session) []Finding { panic("stub") }

// listLocalization lists the missing keys, the keys defined twice outside a replace folder, the orphaned keys and
// each language's coverage.
func listLocalization(ws *workspace.Session) []Finding { panic("stub") }

// listScript lists the problems in every mod file, one row per place, by running the editor's Diagnose over the
// files in parallel.
func listScript(ws *workspace.Session, editor *lsp.Service) []Finding { panic("stub") }

// listUnused lists the definitions in the mods that no file of the workspace refers to, as hints. It leaves out
// the databases whose definitions the game itself invokes by name, as the dumps declare.
func listUnused(ws *workspace.Session) []Finding { panic("stub") }

// listErrorLog lists the lines of the game's error log as findings, each with the script location the line names
// when it names one. The log's place and format come from the game's row.
func listErrorLog(ws *workspace.Session) []Finding { panic("stub") }

// watchErrorLog calls the given function each time the game's error log grows while the game runs. It returns a
// function that ends the watch.
func watchErrorLog(ws *workspace.Session, change func()) (stop func()) { panic("stub") }
