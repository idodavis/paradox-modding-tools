// Package health is the Health page's service. It builds the report over the open workspace: the contests between
// mods, the overrides of the game, the dependencies between mods, the dangling references, the localization and
// script problems, the unused definitions and the game's error log. It also counts each mod, compares two
// statements, and writes a translation scaffold. Everything reads the index and the walk's visitors; nothing here
// walks a file on its own.
package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// EventErrorLogChanged is sent when the game's error log grows while the game runs.
const EventErrorLogChanged = "errorlog:changed"

func init() { application.RegisterEvent[struct{}](EventErrorLogChanged) }

// Service is the Health service. It judges script with the editor's own verdicts, so Health and the editor give
// one verdict, and it watches the game's error log while the game runs.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex // held while stop is read or replaced
	stop      func()     // ends the error log's watch; nil while none runs
}

// ServiceStartup watches the game's error log while a workspace is open: the watch starts each time a workspace
// opens, for that workspace's game, and stops when it closes.
func (hlt *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	events := application.Get().Event
	events.On(workspace.EventOpened, func(*application.CustomEvent) {
		ws, err := hlt.Workspace.Session()
		if err != nil {
			hlt.setStop(nil)
			return
		}
		hlt.setStop(watchErrorLog(ws, func() { application.Get().Event.Emit(EventErrorLogChanged, struct{}{}) }))
	})
	events.On(workspace.EventClosed, func(*application.CustomEvent) { hlt.setStop(nil) })
	return nil
}

// ServiceShutdown stops the error log's watch.
func (hlt *Service) ServiceShutdown() error {
	hlt.setStop(nil)
	return nil
}

// A Section is one card of the Health page, with its count of findings.
type Section struct {
	ID    string `json:"id"` // contests, overrides, dependencies, dangling, localization, script, unused, errorlog
	Label string `json:"label"`
	Note  string `json:"note"`
	Count int    `json:"count"`
}

// Sections lists every Health section in the page's order.
var Sections = []Section{
	{ID: "contests", Label: "Contests", Note: "Names and files that two or more mods define, of which the game keeps one."},
	{ID: "overrides", Label: "Overrides", Note: "Names and files that a mod defines over the game's own."},
	{ID: "dependencies", Label: "Dependencies", Note: "Mods whose definitions change what other mods define."},
	{ID: "dangling", Label: "Dangling references", Note: "Names that script writes and nothing defines."},
	{ID: "localization", Label: "Localization", Note: "Missing, orphaned and untranslated messages, and lines the game cannot read."},
	{ID: "script", Label: "Script", Note: "Problems the editor finds in the mods' script files."},
	{ID: "unused", Label: "Unused definitions", Note: "Definitions of the mods that no file refers to."},
	{ID: "errorlog", Label: "Game errors", Note: "What the game wrote to its error log."},
}

// A FindingSite is one place a finding points at, with whether the game keeps that definition and why. The range of
// a definition covers its whole statement.
type FindingSite struct {
	Place  workspace.Place `json:"place"`
	Range  lsp.Range       `json:"range"`
	Kept   bool            `json:"kept"`
	Reason string          `json:"reason,omitempty"` // order, mode, priority, merge, replace
}

// A Finding is one row of the Health page: its section, its code, the name and database it concerns, the facts
// its message is written from, and the places it points at.
type Finding struct {
	Section  string            `json:"section"`
	Code     string            `json:"code"` // an lsp.Codes id
	Name     string            `json:"name"`
	Database string            `json:"database"` // the database's words, as the page shows them
	Facts    map[string]string `json:"facts"`
	Sites    []FindingSite     `json:"sites"`
}

// A Report is everything the Health page shows for one load order, except the game's error log.
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

// ruleLabels are the words a contest names its rule with: the winner's reason where it is one of these keys, else
// what the database keeps.
var ruleLabels = map[string]string{"last": "LIOS", "first": "FIOS", "highest": "priority", "mode": "mode"}

// BuildReport builds the Health report for the mods in the given load order, or the workspace's own order when
// none is given. A different order scans the mods again in that order without saving it, so the page can preview
// a dragged order.
func (hlt *Service) BuildReport(order []string) (Report, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return Report{}, err
	}
	mods := ws.Mods.Load()
	ids := lo.Map(ws.Order, func(mod workspace.Mod, _ int) string { return mod.ID })
	if len(order) > 0 && !slices.Equal(order, ids) {
		if mods, err = ws.ScanOrder(order); err != nil {
			return Report{}, err
		}
	}
	definitions := listDefinitions(ws, mods)
	defined := listDefined(mods, definitions)
	kept := map[schema.Reference]index.Winner{}
	for _, reference := range defined {
		if winner, ok := mods.FindKept(ws.Schema, reference); ok {
			kept[reference] = winner
		}
	}
	messages, paths := listMessages(mods), listPaths(mods)
	lists := []func() []Finding{
		func() []Finding { return listContests(ws, mods, defined, kept) },
		func() []Finding { return listFileContests(ws, mods, paths) },
		func() []Finding { return listOverrides(ws, mods, defined, kept) },
		func() []Finding { return listFileOverrides(ws, mods, paths) },
		func() []Finding { return listDependencies(ws, mods, defined, kept) },
		func() []Finding { return listDiagnostics(ws, mods) },
		func() []Finding { return listLocalization(ws, mods, definitions, messages) },
		func() []Finding { return listUnused(ws, mods, definitions) },
	}
	results := make([][]Finding, len(lists))
	var group sync.WaitGroup
	for position, list := range lists {
		group.Go(func() { results[position] = list() })
	}
	group.Wait()
	findings := slices.Concat(results...)
	sections := slices.Clone(Sections)
	for position := range sections {
		sections[position].Count = lo.CountBy(findings, func(finding Finding) bool {
			return finding.Section == sections[position].ID
		})
	}
	return Report{Sections: sections, Findings: findings}, nil
}

// ListSections returns every Health section.
func (hlt *Service) ListSections() []Section { return Sections }

// ListCodes returns every problem code, whose message template the page fills from a finding's facts.
func (hlt *Service) ListCodes() []lsp.Code { return lsp.Codes }

// ListOverview counts the definitions, files and messages of each mod in the workspace.
func (hlt *Service) ListOverview() ([]Overview, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	positions := mods.ListOwnFiles()
	return lo.Map(ws.Order, func(mod workspace.Mod, _ int) Overview {
		overview := Overview{Mod: mod.ID, Counts: map[string]int{}}
		for _, position := range positions {
			if mods.Files[position].Origin != mod.ID {
				continue
			}
			overview.Files++
			for database, count := range mods.CountDefinitions(position) {
				overview.Counts[database] += count
			}
			overview.Messages += lo.CountBy(mods.Occurrences[position], func(occurrence index.Occurrence) bool {
				return occurrence.Role == index.RoleLocalizes
			})
		}
		return overview
	}), nil
}

// CompareStatements returns the texts of two definitions' statements, for the compare view. Each statement is the
// one that starts where its site's range starts. It returns an error if a file cannot be read or no statement starts
// there.
func (hlt *Service) CompareStatements(a, b FindingSite) (Compared, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return Compared{}, err
	}
	first, err := findStatementText(ws, a)
	if err != nil {
		return Compared{}, err
	}
	second, err := findStatementText(ws, b)
	return Compared{A: first, B: second}, err
}

// findStatementText returns the text of the statement that starts where a finding site's range starts. It returns an
// error if the site's file cannot be read or no statement starts there.
func findStatementText(ws *workspace.Session, site FindingSite) (string, error) {
	tree, err := ws.LoadTree(site.Place.Path)
	if err != nil {
		return "", err
	}
	offset := lsp.ToOffset(tree.Src, tree.Lines, site.Range.StartLine, site.Range.StartColumn)
	statement, ok := tree.LookupStatement(offset)
	if !ok {
		return "", fmt.Errorf("no statement starts at line %d of %s", site.Range.StartLine, site.Place.Path)
	}
	return tree.Text(tree.Statements[statement].Span), nil
}

// ListErrorLog lists the lines of the game's error log as findings, each with the script location the line names
// when it names one. The location is found in the workspace's own load order, the one the game ran with. The log's
// place and format come from the game's row.
func (hlt *Service) ListErrorLog() ([]Finding, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	errorLog := ws.Game.ErrorLog
	text, err := files.ReadText(resolveErrorLog(ws))
	if errors.Is(err, fs.ErrNotExist) || errorLog.Line == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	var findings []Finding
	for line := range strings.Lines(text.Text) {
		line = strings.TrimRight(line, "\r\n")
		if match := errorLog.Line.FindStringSubmatch(line); match != nil {
			findings = append(findings, makeFinding("game-error", map[string]string{
				"time": match[1] + ":" + match[2] + ":" + match[3], "source": match[5] + ":" + match[6], "message": match[7],
			}, nil))
		} else if len(findings) > 0 && strings.TrimSpace(line) != "" {
			findings[len(findings)-1].Facts["message"] += "\n" + line
		}
		// a location is on the line of the message or on one of the lines after it
		if last := len(findings) - 1; last >= 0 && len(findings[last].Sites) == 0 && errorLog.Location != nil {
			if site, ok := findLogSite(ws, mods, errorLog.Location, line); ok {
				findings[last].Sites = []FindingSite{site}
			}
		}
	}
	return findings, nil
}

// resolveErrorLog returns the path of the game's error log. The install's folder is the one above its first root.
func resolveErrorLog(ws *workspace.Session) string {
	dir := game.FindUserDataDir(ws.Game.ID, filepath.Dir(ws.Install.Roots[0].Dir))
	return filepath.Join(dir, filepath.FromSlash(ws.Game.ErrorLog.Rel))
}

// findLogSite returns the place a line of the error log names with the given expression: the line of a mod's file
// that the game reads. It returns false if the line names none, or names a file of the game's.
func findLogSite(ws *workspace.Session, mods *index.Index, location *regexp.Regexp, line string) (FindingSite, bool) {
	match := location.FindStringSubmatch(line)
	if match == nil {
		return FindingSite{}, false
	}
	file, _, ok := mods.FindLoaded(match[1])
	number, err := strconv.Atoi(match[2])
	if !ok || err != nil || file.Origin == game.OriginVanilla {
		return FindingSite{}, false
	}
	return FindingSite{
		Place: ws.Locate(file.Path), Range: lsp.Range{StartLine: number, StartColumn: 1, EndLine: number, EndColumn: 1},
	}, true
}

// WriteScaffold writes the localization files a mod is missing for a language, with the base language's text as
// the placeholder for each missing key, and returns the paths written. The watcher sends fs:changed for each file.
func (hlt *Service) WriteScaffold(modID, language string) ([]string, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mod, ok := findMod(ws, modID)
	if !ok || mod.Broken {
		return nil, fmt.Errorf("the mod %s is not in the workspace or has no folder", modID)
	}
	if language == "" || language != files.ToSlug(language) {
		return nil, fmt.Errorf("%q is not a language", language)
	}
	scaffolds := listScaffolds(ws, ws.Mods.Load(), mod, language)
	paths := slices.Sorted(maps.Keys(scaffolds))
	for position, target := range paths {
		text := files.Text{Text: loc.Encode(language, scaffolds[target]), Encoding: files.EncodingUTF8BOM}
		if err := hlt.Workspace.Write(target, text); err != nil {
			return paths[:position], err
		}
	}
	return paths, nil
}

// findMod returns the mod of the workspace with the given id. It returns false if the workspace has none.
func findMod(ws *workspace.Session, modID string) (workspace.Mod, bool) {
	return lo.Find(ws.Order, func(mod workspace.Mod) bool { return mod.ID == modID })
}

// listScaffolds returns the messages to write for a mod in the given language, by the path of the file each belongs
// in: every message of each base-language file of the mod whose counterpart in the language does not exist. The
// text of a message is its source's.
func listScaffolds(ws *workspace.Session, mods *index.Index, mod workspace.Mod, language string) map[string][]loc.Message {
	scaffolds := map[string][]loc.Message{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		target := files.Join(mod.Dir, toCounterpartRel(file.Rel, language))
		if _, err := os.Stat(target); err == nil || file.Origin != mod.ID || file.Format != files.FormatLocalization {
			continue
		}
		tree, err := ws.LoadTree(file.Path)
		if err != nil {
			continue
		}
		for ordinal, occurrence := range mods.Occurrences[position] {
			site := index.Site{File: position, Occurrence: int32(ordinal)}
			if occurrence.Role == index.RoleLocalizes && mods.Language(site) == game.BaseLanguage {
				scaffolds[target] = append(scaffolds[target], loc.Message{Key: tree.Text(occurrence.Span), Text: mods.Text(site)})
			}
		}
	}
	return scaffolds
}

// toCounterpartRel returns the path of the file that holds a base-language file's messages in the given language: the
// same path with the language in place of the base language in its folders and in the ending of its name.
func toCounterpartRel(rel, language string) string {
	dir, file := path.Split(rel)
	folders := lo.Map(strings.Split(dir, "/"), func(folder string, _ int) string {
		return lo.Ternary(strings.EqualFold(folder, game.BaseLanguage), language, folder)
	})
	if stem, ok := strings.CutSuffix(strings.TrimSuffix(file, ".yml"), "_l_"+game.BaseLanguage); ok {
		file = stem + "_l_" + language + ".yml"
	}
	return path.Join(slices.Concat(folders, []string{file})...)
}

// listContests lists each of the given names that two or more mods define in the given index of the mods, saying
// which definition the game keeps and by what rule. The map holds the definition the game keeps of each name. A
// database whose definitions the game merges has no contests.
func listContests(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	for _, reference := range defined {
		sites, winner, ok := findLoadedDefinitions(ws, mods, reference, kept)
		origins := lo.FilterMap(sites, func(site index.Site, _ int) (string, bool) {
			return mods.File(site).Origin, isMod(mods, site)
		})
		if !ok || len(lo.Uniq(origins)) < 2 {
			continue
		}
		facts := describeReference(reference)
		facts["rule"] = cmp.Or(ruleLabels[winner.Reason], ruleLabels[ws.Schema.Databases[reference.Database].Precedence.Keeps])
		findings = append(findings, makeFinding("contest", facts, makeSites(ws, mods, sites, winner)))
	}
	return findings
}

// listDefined returns each name that the given definitions of the mods define, once, as its database and its name.
func listDefined(mods *index.Index, definitions []index.Site) []schema.Reference {
	return lo.Uniq(lo.Map(definitions, func(site index.Site, _ int) schema.Reference { return mods.ToReference(site) }))
}

// listDefinitions returns the sites of the definitions in the mods' files that sit in a database the schema knows.
func listDefinitions(ws *workspace.Session, mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool {
		_, known := ws.Schema.Databases[mods.ToReference(site).Database]
		return mods.Occurrence(site).Role == index.RoleDefines && known
	})
}

// listModSites returns the sites of the occurrences in the mods' own files that the given function keeps.
func listModSites(mods *index.Index, keep func(site index.Site) bool) []index.Site {
	var sites []index.Site
	for _, file := range mods.ListOwnFiles() {
		for position := range mods.Occurrences[file] {
			if site := (index.Site{File: file, Occurrence: int32(position)}); keep(site) {
				sites = append(sites, site)
			}
		}
	}
	return sites
}

// findLoadedDefinitions returns the definitions of a name that sit in files the game loads, in load order, and the
// one the game keeps, which the given map holds for each name. It returns false for a database the schema lacks or
// whose definitions the game merges, and where the game keeps none.
func findLoadedDefinitions(ws *workspace.Session, mods *index.Index, reference schema.Reference, kept map[schema.Reference]index.Winner) ([]index.Site, index.Winner, bool) {
	database, known := ws.Schema.Databases[reference.Database]
	winner, ok := kept[reference]
	if !known || database.Precedence.Keeps == "merged" || !ok {
		return nil, index.Winner{}, false
	}
	sites := lo.Filter(mods.Resolve(ws.Schema, reference), func(site index.Site, _ int) bool { return mods.IsLoaded(site.File) })
	return sites, winner, true
}

// describeReference returns the facts that say which name a finding is about: the name and what its database calls
// one of its definitions.
func describeReference(reference schema.Reference) map[string]string {
	return map[string]string{"name": reference.Name, "database": game.DescribeDefinition(reference.Database, false)}
}

// makeFinding returns a finding of the code with the given id. Its section is the code's; its name and database are
// the facts' words of those names.
func makeFinding(id string, facts map[string]string, sites []FindingSite) Finding {
	code, _ := lo.Find(lsp.Codes, func(code lsp.Code) bool { return code.ID == id })
	return Finding{Section: code.Section, Code: id, Name: facts["name"], Database: facts["database"], Facts: facts, Sites: sites}
}

// makeSites returns the finding sites of the given definitions of one name. Each definition the game keeps is
// marked kept, with the reason the winner has.
func makeSites(ws *workspace.Session, mods *index.Index, definitions []index.Site, winner index.Winner) []FindingSite {
	return lo.Map(definitions, func(definition index.Site, _ int) FindingSite {
		site := makeSite(ws, mods, definition)
		if site.Kept = winner.IsKept(definition); site.Kept {
			site.Reason = winner.Reason
		}
		return site
	})
}

// makeSite returns the finding site of an occurrence: where its file sits and the range it covers. A definition
// covers its whole statement. The range is empty if the file cannot be read.
func makeSite(ws *workspace.Session, mods *index.Index, site index.Site) FindingSite {
	target := mods.File(site).Path
	span := mods.Occurrence(site).Span
	tree, err := ws.LoadTree(target)
	if err != nil {
		return FindingSite{Place: ws.Locate(target)}
	}
	if mods.Occurrence(site).Role == index.RoleDefines {
		if statementTree, statement, err := ws.FindStatement(mods, site); err == nil && statement > 0 {
			span = statementTree.Statements[statement].Span
		}
	}
	return FindingSite{Place: ws.Locate(target), Range: lsp.ToRange(tree.Src, tree.Lines, span)}
}

// isMod reports whether a definition is in a mod's file, not the game's.
func isMod(mods *index.Index, site index.Site) bool {
	return mods.File(site).Origin != game.OriginVanilla
}

// listFileContests lists each of the given paths that two or more mods ship in a folder the game reads files from.
// The paths are the ones listPaths returns. The file the game loads is the one it keeps.
func listFileContests(ws *workspace.Session, mods *index.Index, paths map[string][]int32) []Finding {
	folders := map[string]bool{} // the first folders of the game's files
	for _, file := range ws.Install.Files {
		folder, _, _ := strings.Cut(strings.ToLower(file.Rel), "/")
		folders[folder] = true
	}
	var findings []Finding
	for _, key := range slices.Sorted(maps.Keys(paths)) {
		shipped := lo.Filter(paths[key], func(position int32, _ int) bool { return mods.Files[position].Origin != game.OriginVanilla })
		origins := lo.Map(shipped, func(position int32, _ int) string { return mods.Files[position].Origin })
		if folder, _, _ := strings.Cut(key, "/"); !folders[folder] || len(lo.Uniq(origins)) < 2 {
			continue
		}
		facts := describeReference(schema.Reference{Database: game.File, Name: mods.Files[shipped[0]].Rel})
		facts["rule"] = ruleLabels["last"]
		findings = append(findings, makeFinding("contest", facts, lo.Map(shipped, func(position int32, _ int) FindingSite {
			return makeFileSite(ws, mods, position)
		})))
	}
	return findings
}

// listPaths returns, for each path that a mod ships, the positions of the files at it in the given index of the mods,
// in load order. A path is in lower case.
func listPaths(mods *index.Index) map[string][]int32 {
	shipped := map[string]bool{}
	for _, position := range mods.ListOwnFiles() {
		shipped[strings.ToLower(mods.Files[position].Rel)] = true
	}
	paths := map[string][]int32{}
	for position, file := range mods.Files {
		key := strings.ToLower(file.Rel)
		if mods.IsListed(int32(position)) && shipped[key] {
			paths[key] = append(paths[key], int32(position))
		}
	}
	return paths
}

// makeFileSite returns the finding site of a file of the given index of the mods, at its first line. The game keeps
// the file if it loads it.
func makeFileSite(ws *workspace.Session, mods *index.Index, position int32) FindingSite {
	return FindingSite{
		Place: ws.Locate(mods.Files[position].Path), Range: lsp.Range{StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 1},
		Kept: mods.IsLoaded(position),
	}
}

// listOverrides lists each of the given names that a mod and the game both define in the given index of the mods,
// saying which definition the game keeps. The map holds the definition the game keeps of each name. A database whose
// definitions the game merges has no overrides.
func listOverrides(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	for _, reference := range defined {
		sites, winner, ok := findLoadedDefinitions(ws, mods, reference, kept)
		if !ok || !lo.SomeBy(sites, func(site index.Site) bool { return !isMod(mods, site) }) {
			continue
		}
		facts := describeReference(reference)
		facts["kept"] = lo.Ternary(lo.SomeBy(sites, func(site index.Site) bool { return isMod(mods, site) && winner.IsKept(site) }),
			"the mod's", "its own")
		findings = append(findings, makeFinding("override", facts, makeSites(ws, mods, sites, winner)))
	}
	return findings
}

// listFileOverrides lists each file of a mod that sits at one of the given paths of a file of the game, which it
// replaces whole. The paths are the ones listPaths returns.
func listFileOverrides(ws *workspace.Session, mods *index.Index, paths map[string][]int32) []Finding {
	var findings []Finding
	for _, key := range slices.Sorted(maps.Keys(paths)) {
		installed, shipped := lo.FilterReject(paths[key], func(position int32, _ int) bool {
			return mods.Files[position].Origin == game.OriginVanilla
		})
		if len(installed) == 0 {
			continue
		}
		for _, position := range shipped {
			facts := describeReference(schema.Reference{Database: game.File, Name: mods.Files[position].Rel})
			facts["kept"] = "the mod's"
			findings = append(findings, makeFinding("override", facts, []FindingSite{
				makeFileSite(ws, mods, installed[0]), makeFileSite(ws, mods, position),
			}))
		}
	}
	return findings
}

// listDependencies lists, for each mod, the mods it depends on and the mods it affects in the given index of the
// mods, found by comparing a kept definition's statement with the one it hides. It reads the given names, and the map
// holds the definition the game keeps of each.
func listDependencies(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	positions := map[[2]string]int{} // the position in findings of the depends finding of a keeper's mod and another mod
	listed := map[[3]string]bool{}   // the names counted for a keeper's mod and another mod
	for _, reference := range defined {
		winner, ok := kept[reference]
		if !ok || !isMod(mods, winner.Site) {
			continue
		}
		for _, site := range mods.Resolve(ws.Schema, reference) {
			pair := [2]string{mods.File(winner.Site).Origin, mods.File(site).Origin}
			key := [3]string{pair[0], pair[1], reference.Database + "/" + reference.Name}
			if !isMod(mods, site) || winner.IsKept(site) || pair[0] == pair[1] || listed[key] || !isChanged(ws, mods, winner.Site, site) {
				continue
			}
			listed[key] = true
			if _, ok := positions[pair]; !ok {
				positions[pair] = len(findings)
				findings = append(findings, makeDependencies(ws, pair)...)
			}
			sites := makeSites(ws, mods, []index.Site{winner.Site, site}, winner)
			findings[positions[pair]].Sites = append(findings[positions[pair]].Sites, sites...)
			findings[positions[pair]+1].Sites = append(findings[positions[pair]+1].Sites, sites[1], sites[0])
		}
	}
	for position := range findings {
		findings[position].Facts["count"] = strconv.Itoa(len(findings[position].Sites) / 2)
	}
	return findings
}

// isChanged reports whether a definition the game hides is written otherwise than the definition that hides it.
// Comments, spacing and quotes do not count. It is false where either file cannot be read or no longer has the
// statement.
func isChanged(ws *workspace.Session, mods *index.Index, kept, hidden index.Site) bool {
	keptTree, keptStatement, err := ws.FindStatement(mods, kept)
	if err != nil {
		return false
	}
	hiddenTree, hiddenStatement, err := ws.FindStatement(mods, hidden)
	if err != nil {
		return false
	}
	return !keptTree.IsEqual(keptStatement, hiddenTree, hiddenStatement)
}

// makeDependencies returns the two findings of a pair of mods, the first of which keeps definitions over the second's:
// the first mod depends on the second, and the second is affected by the first. Neither has a site yet.
func makeDependencies(ws *workspace.Session, pair [2]string) []Finding {
	keeper, other := describeMod(ws, pair[0]), describeMod(ws, pair[1])
	depends := describeReference(schema.Reference{Database: "mod", Name: keeper})
	affects := describeReference(schema.Reference{Database: "mod", Name: other})
	depends["other"], affects["other"] = other, keeper
	return []Finding{makeFinding("depends", depends, nil), makeFinding("affects", affects, nil)}
}

// describeMod returns the words a finding uses for a mod: the name its descriptor gives, else its folder's name.
func describeMod(ws *workspace.Session, modID string) string {
	mod, _ := findMod(ws, modID)
	return cmp.Or(mod.Descriptor.Name, filepath.Base(mod.Dir))
}

// listLocalization lists, from the given index of the mods, the given definitions whose title has no message in the
// base language, the given messages nothing refers to and each language's coverage of them. A key defined twice and a
// malformed line are the editor's to find.
func listLocalization(ws *workspace.Session, mods *index.Index, definitions, messages []index.Site) []Finding {
	return slices.Concat(listMissingMessages(ws, mods, definitions), listOrphanedMessages(ws, mods, messages), listCoverage(ws, mods, messages))
}

// listMissingMessages lists the given definitions of the mods that write a title key whose message the base language
// lacks.
func listMissingMessages(ws *workspace.Session, mods *index.Index, definitions []index.Site) []Finding {
	var findings []Finding
	for _, site := range definitions {
		key, ok := ws.FindTitleKey(mods, site)
		if !ok || !jomini.IsIdentifier(key) || hasMessage(mods, key, game.BaseLanguage) {
			continue
		}
		facts := describeReference(mods.ToReference(site))
		facts["key"] = key
		findings = append(findings, makeFinding("missing-message", facts, []FindingSite{makeSite(ws, mods, site)}))
	}
	return findings
}

// hasMessage reports whether the given index of the mods holds a message with the key in the language.
func hasMessage(mods *index.Index, key, language string) bool {
	return lo.ContainsBy(mods.ListMessages(key), func(message index.Site) bool { return mods.Language(message) == language })
}

// listOrphanedMessages lists the keys of the given messages of the mods that nothing refers to: no reference, and no
// definition with the key as its name.
func listOrphanedMessages(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	var findings []Finding
	for _, site := range lo.UniqBy(messages, func(site index.Site) string { return mods.ToReference(site).Name }) {
		key := findKey(ws, mods, site)
		if !isReferred(ws, mods, key) {
			reference := schema.Reference{Database: game.Localization, Name: key}
			findings = append(findings, makeFinding("orphaned-message", describeReference(reference), []FindingSite{makeSite(ws, mods, site)}))
		}
	}
	return findings
}

// listMessages returns the sites of the messages in the mods' files, in every language.
func listMessages(mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool { return mods.Occurrence(site).Role == index.RoleLocalizes })
}

// findKey returns the key of a message of the given index of the mods as its file writes it. The index folds the
// case of a key.
func findKey(ws *workspace.Session, mods *index.Index, site index.Site) string {
	tree, err := ws.LoadTree(mods.File(site).Path)
	if err != nil {
		return mods.ToReference(site).Name
	}
	return tree.Text(mods.Occurrence(site).Span)
}

// isReferred reports whether anything refers to a message: a reference to its key, or a definition of the schema's
// databases with the key as its name, whose message the game reads as the definition's title.
func isReferred(ws *workspace.Session, mods *index.Index, key string) bool {
	if len(mods.ListUses(game.Localization, key)) > 0 {
		return true
	}
	return slices.ContainsFunc(mods.ListDatabases(key), func(database string) bool {
		return database != game.Localization && ws.Schema.Databases[database] != nil && len(mods.Resolve(ws.Schema, schema.Reference{Database: database, Name: key})) > 0
	})
}

// listCoverage lists, for each mod and each language the mod writes other than the base language, how many of the
// mod's base-language keys it writes in that language too, among the given messages. A mod with no keys in the base
// language has none.
func listCoverage(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	var findings []Finding
	byMod := lo.GroupBy(messages, func(site index.Site) string { return mods.File(site).Origin })
	for _, mod := range ws.Order {
		byLanguage := lo.GroupBy(byMod[mod.ID], func(site index.Site) string { return mods.Language(site) })
		base := listKeys(mods, byLanguage[game.BaseLanguage])
		if len(base) == 0 {
			continue
		}
		for _, language := range slices.Sorted(maps.Keys(byLanguage)) {
			if language == game.BaseLanguage {
				continue
			}
			facts := describeReference(schema.Reference{Database: game.Localization, Name: language})
			facts["mod"] = describeMod(ws, mod.ID)
			facts["have"] = strconv.Itoa(len(lo.Intersect(base, listKeys(mods, byLanguage[language]))))
			facts["of"] = strconv.Itoa(len(base))
			positions := lo.Uniq(lo.Map(byLanguage[language], func(site index.Site, _ int) int32 { return site.File }))
			findings = append(findings, makeFinding("coverage", facts, lo.Map(positions, func(position int32, _ int) FindingSite {
				return makeFileSite(ws, mods, position)
			})))
		}
	}
	return findings
}

// listKeys returns the keys of the given messages of the index of the mods, each once.
func listKeys(mods *index.Index, messages []index.Site) []string {
	return lo.Uniq(lo.Map(messages, func(site index.Site, _ int) string { return mods.ToReference(site).Name }))
}

// listDiagnostics lists the problems in every mod file of the given index of the mods, judged by the editor's own
// verdicts against that index, the files in parallel. Each problem goes under its code's section, one row per
// place, except an unresolved reference, which is one row per name with every place that writes it.
func listDiagnostics(ws *workspace.Session, mods *index.Index) []Finding {
	paths := lo.FilterMap(mods.ListOwnFiles(), func(position int32, _ int) (string, bool) {
		file := mods.Files[position]
		return file.Path, file.Format.IsScript() || file.Format == files.FormatLocalization
	})
	diagnostics := make([][]lsp.Diagnostic, len(paths))
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for position, target := range paths {
		group.Go(func() error {
			// a file that cannot be read has no verdicts
			diagnostics[position], _ = lsp.DiagnoseFile(ws, mods, target)
			return nil
		})
	}
	_ = group.Wait()
	var findings []Finding
	positions := map[[2]string]int{} // the position in findings of each name nothing defines, by database and name
	for position, target := range paths {
		place := ws.Locate(target)
		for _, diagnostic := range diagnostics[position] {
			site := FindingSite{Place: place, Range: diagnostic.Range}
			finding := makeFinding(diagnostic.Code, diagnostic.Facts, []FindingSite{site})
			if finding.Section == "dangling" {
				key := [2]string{finding.Database, finding.Name}
				if found, ok := positions[key]; ok {
					findings[found].Sites = append(findings[found].Sites, site)
					continue
				}
				positions[key] = len(findings)
			}
			findings = append(findings, finding)
		}
	}
	return findings
}

// listUnused lists the given definitions in the mods that no file of the given index refers to, as hints. It leaves
// out the databases whose definitions the game itself invokes by name, as the dumps declare.
func listUnused(ws *workspace.Session, mods *index.Index, definitions []index.Site) []Finding {
	var findings []Finding
	for _, site := range definitions {
		reference := mods.ToReference(site)
		if mods.Occurrence(site).Mode != 0 || isInvoked(ws, reference.Database) ||
			len(mods.ListUses(reference.Database, reference.Name)) > 0 {
			continue
		}
		findings = append(findings, makeFinding("unused-definition", describeReference(reference), []FindingSite{makeSite(ws, mods, site)}))
	}
	return findings
}

// isInvoked reports whether the game uses the definitions of a database by name itself: it shows them as windows,
// fires them, offers them or reads them as history.
func isInvoked(ws *workspace.Session, database string) bool {
	return database == game.GUI.WidgetDatabase || slices.Contains(game.EngineHouses, ws.Schema.Databases[database].Row.Houses)
}

// watchErrorLog calls the given function each time the game's error log grows while the game runs. It returns a
// function that ends the watch.
func watchErrorLog(ws *workspace.Session, change func()) (stop func()) {
	target := resolveErrorLog(ws)
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}
	}
	watched := ""
	// the log's folder may not exist yet, so the nearest folder above it is watched, and the watch moves down as the
	// folders appear
	follow := func() {
		nearest := findNearestFolder(filepath.Dir(target))
		if nearest == watched {
			return
		}
		_ = notifier.Remove(watched)
		watched = nearest
		_ = notifier.Add(nearest)
	}
	follow()
	done := make(chan struct{})
	go func() {
		var timer *time.Timer
		defer close(done)
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case event, ok := <-notifier.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Create | fsnotify.Remove | fsnotify.Rename) {
					follow()
				}
				if event.Has(fsnotify.Write|fsnotify.Create) && files.ToKey(event.Name) == files.ToKey(target) {
					// writes come in bursts, so the change is passed on once they stop
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(200*time.Millisecond, change)
				}
			case _, ok := <-notifier.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() {
		_ = notifier.Close()
		<-done
	}
}

// findNearestFolder returns the given folder if it exists, else the nearest folder above it that does. It returns ""
// if none does.
func findNearestFolder(dir string) string {
	for {
		if files.IsFolder(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// setStop ends the running watch, if any, and keeps the given function as the one that ends the next. A nil
// function means no watch runs.
func (hlt *Service) setStop(stop func()) {
	hlt.mu.Lock()
	running := hlt.stop
	hlt.stop = stop
	hlt.mu.Unlock()
	if running != nil {
		running()
	}
}
