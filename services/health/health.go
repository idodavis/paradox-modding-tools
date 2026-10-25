// Package health is the Health page's service. It builds the report of what is wrong in the open workspace's mods and
// lists the game's error log.
package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
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

// init registers the error log event with the application.
func init() { application.RegisterEvent[struct{}](EventErrorLogChanged) }

// Service is the Health service. It reports the problems of the open workspace's mods and watches the game's error log
// while the game runs.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex // held while stop is read or replaced
	stop      func()     // ends the error log's watch; nil if none runs
}

// ServiceStartup watches the game's error log while a workspace is open. The watch stops when the workspace closes.
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

// A Section is one card of the Health page, with how many findings it holds.
type Section struct {
	ID      string  `json:"id"` // contests, overrides, dependencies, dangling, localization, script, errorlog, overview
	Label   string  `json:"label"`
	Note    string  `json:"note"`
	Count   int     `json:"count"`
	Tally   bool    `json:"-"`       // its findings are counted by kind
	Tallies []Tally `json:"tallies"` // none for a section counted only as a whole
}

// A Tally is how many of one section's findings are of one kind.
type Tally struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Sections lists every Health section in the page's order.
var Sections = []Section{
	{ID: "contests", Label: "Contests", Note: "Names and files that two or more mods define, of which the game keeps one.",
		Tally: true},
	{ID: "overrides", Label: "Overrides", Note: "Names and files that a mod defines over the game's own."},
	{ID: "dependencies", Label: "Dependencies and versions",
		Note: "Mods that change what other mods define, mods a descriptor needs, and mods made for an older game version."},
	{ID: "dangling", Label: "Dangling references", Note: "Names that script writes and nothing defines."},
	{ID: "localization", Label: "Localization", Note: "Missing and untranslated messages, and lines the game cannot read.",
		Tally: true},
	{ID: "script", Label: "Script", Note: "Problems the editor finds in the mods' script files."},
	{ID: "errorlog", Label: "Game errors", Note: "What the game wrote to its error log."},
	{ID: "overview", Label: "Mod overview", Note: "What each mod defines, by database."},
}

// A FindingSite is one place a finding points at, with whether the game keeps that definition and why.
type FindingSite struct {
	Place   workspace.Place `json:"place"`
	Range   lsp.Range       `json:"range"`
	Kept    bool            `json:"kept"`
	Reason  string          `json:"reason,omitempty"`  // the winner's: order, mode, priority, merge, replace; or merged
	Defines []string        `json:"defines,omitempty"` // the names a whole-file site defines
	Removed []string        `json:"removed,omitempty"` // the names a replaced site defines that the kept file does not
}

// A Finding is one row of the Health page: a problem, or a fact about the mods, with the places it points at.
type Finding struct {
	Section  string            `json:"section"`
	Code     string            `json:"code"` // an lsp.Codes id
	Name     string            `json:"name"`
	Database string            `json:"database"` // what the database calls a definition
	Origin   string            `json:"origin"`   // what it is about: a dependency's mod, else its last kept site's origin
	Facts    map[string]string `json:"facts"`
	Sites    []FindingSite     `json:"sites"`
	Removed  []string          `json:"removed,omitempty"` // the names the replaced sites define that the kept file does not
	// Comparable is true when the kept statement and the ones it replaces can be shown side by side.
	Comparable bool `json:"comparable"`
}

// A Report is the sections and findings the Health page shows for one load order. The game's error log is counted in
// its section and in the total, and listed by ListErrorLog.
type Report struct {
	Sections []Section `json:"sections"`
	Findings []Finding `json:"findings"`
	Total    int       `json:"total"` // the findings of every section but the overview
}

// An Overview is what one mod holds, counted.
type Overview struct {
	Mod      string         `json:"mod"`
	Counts   map[string]int `json:"counts"` // by what a database's definitions are called; two called the same share one
	Files    int            `json:"files"`
	Messages int            `json:"messages"`
}

// Compared holds the texts of two statements.
type Compared struct {
	A string `json:"a"`
	B string `json:"b"`
}

// modDatabase is the database a finding names a mod in.
const modDatabase = "mod"

// mergedReason is the reason of a kept site that is not the winner but joins it, as an injection or a merge does.
const mergedReason = "merged"

// ruleLabels are the words a contest names its rule with, by the reason the winner has or what the database keeps.
var ruleLabels = map[string]string{"last": "LIOS", "first": "FIOS", "highest": "priority", "mode": "mode"}

// BuildReport builds the Health report for the mods in the given load order. It uses the workspace's own order if none
// is given. A different order is not saved. It returns an error if the mods cannot be scanned in that order.
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
	messages, paths := listMessages(mods), listShippedPaths(mods)
	lists := []func() []Finding{
		func() []Finding { return listContests(ws, mods, defined, kept) },
		func() []Finding { return listFileContests(ws, mods, paths) },
		func() []Finding { return listOverrides(ws, mods, defined, kept) },
		func() []Finding { return listFileOverrides(ws, mods, paths) },
		func() []Finding { return listDependencies(ws, mods, defined, kept) },
		func() []Finding { return listDescriptors(ws, mods) },
		func() []Finding { return listDiagnostics(ws, mods) },
		func() []Finding { return listLocalization(ws, mods, definitions, messages) },
	}
	results := make([][]Finding, len(lists))
	var group sync.WaitGroup
	for position, list := range lists {
		group.Go(func() { results[position] = list() })
	}
	group.Wait()
	findings := slices.Concat(results...)
	sections := slices.Clone(Sections)
	// a game with no error log, or one that cannot be read, counts none
	errorLog, _ := hlt.ListErrorLog()
	counts := lo.CountValuesBy(findings, func(finding Finding) string { return finding.Section })
	counts["errorlog"], counts["overview"] = len(errorLog), len(ws.Order)
	for position := range sections {
		sections[position].Count = counts[sections[position].ID]
		if sections[position].Tally {
			sections[position].Tallies = tallyFindings(findings, sections[position].ID)
		}
	}
	return Report{Sections: sections, Findings: findings, Total: len(findings) + len(errorLog)}, nil
}

// tallyFindings counts the findings of the section with the given id by kind: a finding's rule, else its code's label.
// The kinds are in the order they first appear.
func tallyFindings(findings []Finding, sectionID string) []Tally {
	var tallies []Tally
	for _, finding := range findings {
		if finding.Section != sectionID {
			continue
		}
		kind := cmp.Or(finding.Facts["rule"], lookupCode(finding.Code).Label)
		position := slices.IndexFunc(tallies, func(tally Tally) bool { return tally.Kind == kind })
		if position < 0 {
			position = len(tallies)
			tallies = append(tallies, Tally{Kind: kind})
		}
		tallies[position].Count++
	}
	return tallies
}

// lookupCode returns the code with the given id. An id no code has gives the zero code.
func lookupCode(id string) lsp.Code {
	code, _ := lo.Find(lsp.Codes, func(code lsp.Code) bool { return code.ID == id })
	return code
}

// ListSections returns every Health section.
func (hlt *Service) ListSections() []Section { return Sections }

// ListCodes returns every problem code.
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
				overview.Counts[game.DescribeDefinition(database, true)] += count
			}
			overview.Messages += lo.CountBy(mods.Occurrences[position], func(occurrence index.Occurrence) bool {
				return occurrence.Role == index.RoleLocalizes
			})
		}
		return overview
	}), nil
}

// CompareStatements returns the texts of the statements at two sites. It returns an error if a file cannot be read or
// no statement starts at a site.
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

// ListSources returns the first ten lines of each finding site's range, an open file's from the editor's text. It
// returns an error if a file cannot be read.
func (hlt *Service) ListSources(sites []FindingSite) ([]string, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	sources := make([]string, len(sites))
	for position, site := range sites {
		tree, err := ws.LoadTree(site.Place.Path)
		if err != nil {
			return nil, err
		}
		last := tree.Lines.CountLines() - 1
		first := min(max(site.Range.StartLine-1, 0), last)
		start := tree.Lines.LineSpan(first)
		end := tree.Lines.LineSpan(min(max(site.Range.EndLine-1, first), last, first+9))
		sources[position] = tree.Src[start.Start:end.End]
	}
	return sources, nil
}

// ListReaders returns every place in script and localization that reads a localization key.
func (hlt *Service) ListReaders(key string) ([]FindingSite, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	return lo.Map(mods.ListUses(game.Localization, key), func(site index.Site, _ int) FindingSite {
		return makeSite(ws, mods, site)
	}), nil
}

// findStatementText returns the text of the statement at a finding site. It returns an error if the file cannot be read
// or no statement starts there.
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

// ListErrorLog lists the messages of the game's error log as findings. A message that names a line of a mod's file
// points at that line. It returns none if there is no log, and an error if the log cannot be read.
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

// resolveErrorLog returns the path of the game's error log.
func resolveErrorLog(ws *workspace.Session) string {
	dir := game.FindUserDataDir(ws.Game.ID, ws.InstallPath)
	return files.Join(dir, ws.Game.ErrorLog.Rel)
}

// findLogSite returns the line of a mod's file that a line of the error log names, found with the given expression. It
// returns false if the line names none, or names a file of the game.
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
		Place: ws.Locate(mods, file.Path), Range: lsp.ToLineRange(number),
	}, true
}

// WriteScaffold writes the localization files a mod lacks in a language, with the base language's text as the
// placeholder for each message. It returns the paths written. It returns an error if the mod is not in the workspace or
// has no folder, if the language is not one, or if a file cannot be written. The paths written before it are returned
// too.
func (hlt *Service) WriteScaffold(modID, language string) ([]string, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mod, ok := hlt.Workspace.LookupMod(modID)
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

// listScaffolds returns the messages to write for a mod in the given language, by the path of the file each belongs in.
// Each is a base-language message of the mod whose file does not exist in the language yet.
func listScaffolds(ws *workspace.Session, mods *index.Index, mod workspace.Mod, language string) map[string][]loc.Message {
	scaffolds := map[string][]loc.Message{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		if file.Origin != mod.ID || file.Format != files.FormatLocalization {
			continue
		}
		target := files.Join(mod.Dir, loc.ToLanguageRel(file.Rel, language))
		if _, err := os.Stat(target); err == nil {
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

// listContests lists each of the given names that two or more mods define, with the definition the game keeps and the
// rule that keeps it. The map holds the kept definition of each name. A database that merges has no contests.
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
		facts := lsp.DescribeReference(reference)
		facts["rule"] = cmp.Or(ruleLabels[winner.Reason], ruleLabels[ws.Schema.Databases[reference.Database].Precedence.Keeps])
		finding := makeFinding("contest", facts, makeSites(ws, mods, sites, winner))
		finding.Comparable = true
		findings = append(findings, finding)
	}
	return findings
}

// listDefined returns each name the given definitions define, once, as a reference.
func listDefined(mods *index.Index, definitions []index.Site) []schema.Reference {
	return lo.Uniq(lo.Map(definitions, func(site index.Site, _ int) schema.Reference { return mods.ToReference(site) }))
}

// listDefinitions returns the sites of the definitions in the mods' files that sit in a known database. It leaves out
// aliases.
func listDefinitions(ws *workspace.Session, mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool {
		_, known := ws.Schema.Databases[mods.ToReference(site).Database]
		occurrence := mods.Occurrence(site)
		return occurrence.Role == index.RoleDefines && !occurrence.Alias && known
	})
}

// listModSites returns the sites of the occurrences in the mods' files that the given function accepts.
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

// findLoadedDefinitions returns the definitions of a name in loaded files, in load order, and the one the game keeps.
// The map holds the kept definition of each name. It returns false if the database is unknown or merges, or if the game
// keeps none.
func findLoadedDefinitions(ws *workspace.Session, mods *index.Index, reference schema.Reference, kept map[schema.Reference]index.Winner) ([]index.Site, index.Winner, bool) {
	database, known := ws.Schema.Databases[reference.Database]
	winner, ok := kept[reference]
	if !known || database.Precedence.Keeps == "merged" || !ok {
		return nil, index.Winner{}, false
	}
	sites := lo.Filter(mods.Resolve(reference), func(site index.Site, _ int) bool { return mods.IsLoaded(site.File) })
	return sites, winner, true
}

// makeFinding returns a finding of the code with the given id, in the code's section. Its name and database come from
// the given facts. Its sites are the kept ones first, the rest in the order given. Its origin is the last kept site's,
// or the first site's when none is kept.
func makeFinding(id string, facts map[string]string, sites []FindingSite) Finding {
	kept, replaced := lo.FilterReject(sites, func(site FindingSite, _ int) bool { return site.Kept })
	sites = slices.Concat(kept, replaced)
	origin := ""
	switch {
	case len(kept) > 0:
		origin = kept[len(kept)-1].Place.Origin
	case len(sites) > 0:
		origin = sites[0].Place.Origin
	}
	return Finding{
		Section: lookupCode(id).Section, Code: id, Name: facts["name"], Database: facts["database"], Origin: origin,
		Facts: facts, Sites: sites, Removed: listRemoved(sites),
	}
}

// listRemoved gives each whole-file site the game does not keep the names it defines that the kept one does not. It
// returns those names across the sites, each once.
func listRemoved(sites []FindingSite) []string {
	kept := lo.FlatMap(sites, func(site FindingSite, _ int) []string { return lo.Ternary(site.Kept, site.Defines, nil) })
	var removed []string
	for position := range sites {
		if !sites[position].Kept {
			sites[position].Removed, _ = lo.Difference(sites[position].Defines, kept)
			removed = append(removed, sites[position].Removed...)
		}
	}
	return lo.Uniq(removed)
}

// makeSites returns the finding sites of the given definitions of one name. Each kept one is marked kept: the winner
// with the winner's reason, one merged into it with "merged".
func makeSites(ws *workspace.Session, mods *index.Index, definitions []index.Site, winner index.Winner) []FindingSite {
	return lo.Map(definitions, func(definition index.Site, _ int) FindingSite {
		site := makeSite(ws, mods, definition)
		site.Kept = winner.IsKept(definition)
		switch {
		case definition == winner.Site:
			site.Reason = winner.Reason
		case site.Kept:
			site.Reason = mergedReason
		}
		return site
	})
}

// makeSite returns the finding site of an occurrence. The range of a definition covers its whole statement. The range
// is empty if the file cannot be read.
func makeSite(ws *workspace.Session, mods *index.Index, site index.Site) FindingSite {
	target := mods.File(site).Path
	span := mods.Occurrence(site).Span
	tree, err := ws.LoadTree(target)
	if err != nil {
		return FindingSite{Place: ws.Locate(mods, target)}
	}
	if mods.Occurrence(site).Role == index.RoleDefines {
		if statementTree, statement, err := ws.FindStatement(mods, site); err == nil && statement > 0 {
			span = statementTree.Statements[statement].Span
		}
	}
	return FindingSite{Place: ws.Locate(mods, target), Range: lsp.ToRange(tree.Src, tree.Lines, span)}
}

// isMod reports whether a site is in a mod's file and not the game's.
func isMod(mods *index.Index, site index.Site) bool {
	return mods.File(site).Origin != game.OriginVanilla
}

// listFileContests lists each of the given paths that two or more mods ship in a folder the game reads. The game loads
// the last file at a path.
func listFileContests(ws *workspace.Session, mods *index.Index, paths []string) []Finding {
	folders := map[string]bool{} // the first folders of the game's files
	for _, file := range ws.Install.Files {
		folder, _, _ := strings.Cut(strings.ToLower(file.Rel), "/")
		folders[folder] = true
	}
	var findings []Finding
	for _, path := range paths {
		sites := lo.Reject(makeFileSites(ws, mods, path), func(site FindingSite, _ int) bool { return site.Place.ReadOnly })
		origins := lo.Map(sites, func(site FindingSite, _ int) string { return site.Place.Origin })
		if len(lo.Uniq(origins)) < 2 {
			continue
		}
		if folder, _, _ := strings.Cut(strings.ToLower(sites[0].Place.Rel), "/"); !folders[folder] {
			continue
		}
		facts := lsp.DescribeReference(schema.Reference{Database: game.File, Name: sites[0].Place.Rel})
		facts["rule"] = ruleLabels["last"]
		findings = append(findings, makeFinding("contest", facts, sites))
	}
	return findings
}

// listShippedPaths returns each path a mod ships once, sorted without regard to case. Files that differ only in their
// stage folder share a path.
func listShippedPaths(mods *index.Index) []string {
	positions := lo.UniqBy(mods.ListOwnFiles(), func(position int32) string {
		loaded, hidden, _ := mods.FindLoaded(mods.Files[position].Rel)
		return lo.FirstOr(hidden, loaded).Key
	})
	paths := lo.Map(positions, func(position int32, _ int) string { return mods.Files[position].Rel })
	slices.SortFunc(paths, func(a, b string) int { return cmp.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return paths
}

// makeFileSites returns the finding sites of the files at a path in load order: the files the game hides, then the one
// it reads.
func makeFileSites(ws *workspace.Session, mods *index.Index, path string) []FindingSite {
	loaded, hidden, ok := mods.FindLoaded(path)
	if ok {
		hidden = append(hidden, loaded)
	}
	return lo.Map(hidden, func(file files.File, _ int) FindingSite {
		position, _ := mods.FindFile(file.Path)
		return makeFileSite(ws, mods, position)
	})
}

// makeFileSite returns the finding site of a file, at its first line, with the names it defines. The file is kept if
// the game loads it.
func makeFileSite(ws *workspace.Session, mods *index.Index, position int32) FindingSite {
	return FindingSite{
		Place: ws.Locate(mods, mods.Files[position].Path), Range: lsp.ToLineRange(1),
		Kept: mods.IsLoaded(position),
		Defines: lo.FilterMap(mods.Occurrences[position], func(occurrence index.Occurrence, _ int) (string, bool) {
			return mods.Names.Texts[occurrence.Name], occurrence.Role == index.RoleDefines && !occurrence.Alias
		}),
	}
}

// listOverrides lists each of the given names that a mod and the game both define, with the definition the game keeps.
// The map holds the kept definition of each name. A database that merges has no overrides.
func listOverrides(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	for _, reference := range defined {
		sites, winner, ok := findLoadedDefinitions(ws, mods, reference, kept)
		if !ok || !lo.SomeBy(sites, func(site index.Site) bool { return !isMod(mods, site) }) {
			continue
		}
		facts := lsp.DescribeReference(reference)
		facts["kept"] = lo.Ternary(lo.SomeBy(sites, func(site index.Site) bool { return isMod(mods, site) && winner.IsKept(site) }),
			"the mod's", "its own")
		findings = append(findings, makeFinding("override", facts, makeSites(ws, mods, sites, winner)))
	}
	return findings
}

// listFileOverrides lists each of the given paths where a mod ships a file that replaces a file of the game. The
// finding points at the game's file and at each mod's.
func listFileOverrides(ws *workspace.Session, mods *index.Index, paths []string) []Finding {
	var findings []Finding
	for _, path := range paths {
		installed, shipped := lo.FilterReject(makeFileSites(ws, mods, path), func(site FindingSite, _ int) bool {
			return site.Place.ReadOnly
		})
		if len(installed) == 0 {
			continue
		}
		facts := lsp.DescribeReference(schema.Reference{Database: game.File, Name: installed[0].Place.Rel})
		facts["kept"] = lo.Ternary(lo.SomeBy(shipped, func(site FindingSite) bool { return site.Kept }), "the mod's", "its own")
		findings = append(findings, makeFinding("override", facts, slices.Concat(installed[:1], shipped)))
	}
	return findings
}

// listDependencies lists the dependencies between mods. A mod depends on another if the game keeps its definition of a
// name over the other's and the two differ. Each such pair gives a depends finding and an affects finding. The map
// holds the kept definition of each name.
func listDependencies(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	positions := map[[2]string]int{} // the position in findings of the depends finding of a keeper's mod and another mod
	listed := map[[3]string]bool{}   // the names counted for a keeper's mod and another mod
	for _, reference := range defined {
		winner, ok := kept[reference]
		if !ok || !isMod(mods, winner.Site) {
			continue
		}
		for _, site := range mods.Resolve(reference) {
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

// isChanged reports whether the hidden definition is written differently from the kept one. Comments, spacing and
// quotes do not count. It is false if either file cannot be read.
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

// makeDependencies returns the two findings of a pair of mods whose first mod keeps definitions over the second's: the
// first depends on the second, and the second is affected by the first. Neither has a site.
func makeDependencies(ws *workspace.Session, pair [2]string) []Finding {
	keeper, other := describeMod(ws, pair[0]), describeMod(ws, pair[1])
	depends := lsp.DescribeReference(schema.Reference{Database: modDatabase, Name: keeper})
	affects := lsp.DescribeReference(schema.Reference{Database: modDatabase, Name: other})
	depends["other"], affects["other"] = other, keeper
	findings := []Finding{makeFinding("depends", depends, nil), makeFinding("affects", affects, nil)}
	findings[0].Origin, findings[1].Origin = pair[0], pair[1]
	return findings
}

// describeMod returns the name of a mod: the one its descriptor gives, else its folder's.
func describeMod(ws *workspace.Session, modID string) string {
	mod, _ := lo.Find(ws.Order, func(mod workspace.Mod) bool { return mod.ID == modID })
	return cmp.Or(mod.Descriptor.Name, filepath.Base(mod.Dir))
}

// listDescriptors lists the problems in the mods' descriptors: a dependency the workspace lacks, and a mod made for an
// older release than the game's. Each finding points at the first line of the descriptor. A mod with no readable
// descriptor has none.
func listDescriptors(ws *workspace.Session, mods *index.Index) []Finding {
	var findings []Finding
	for _, mod := range ws.Order {
		descriptor := mod.Descriptor
		if descriptor.Path == "" {
			continue
		}
		reference := schema.Reference{Database: modDatabase, Name: describeMod(ws, mod.ID)}
		sites := []FindingSite{{Place: ws.Locate(mods, descriptor.Path), Range: lsp.ToLineRange(1)}}
		for _, dependency := range descriptor.Dependencies {
			if !hasMod(ws, dependency) {
				facts := lsp.DescribeReference(reference)
				facts["dependency"] = dependency
				findings = append(findings, makeFinding("missing-dependency", facts, sites))
			}
		}
		if workspace.IsOlderRelease(descriptor.SupportedVersion, ws.Schema.Version) {
			facts := lsp.DescribeReference(reference)
			facts["supported"], facts["version"] = descriptor.SupportedVersion, ws.Schema.Version
			findings = append(findings, makeFinding("mod-for-older-version", facts, sites))
		}
	}
	return findings
}

// hasMod reports whether the workspace has a mod with the given dependency as its id or its name.
func hasMod(ws *workspace.Session, dependency string) bool {
	return slices.ContainsFunc(ws.Order, func(mod workspace.Mod) bool {
		return mod.Descriptor.ID == dependency || mod.Descriptor.Name == dependency
	})
}

// listLocalization lists the given definitions whose title has no message in the base language, and each language's
// coverage of the given messages.
func listLocalization(ws *workspace.Session, mods *index.Index, definitions, messages []index.Site) []Finding {
	return slices.Concat(listMissingMessages(ws, mods, definitions), listCoverage(ws, mods, messages))
}

// listMissingMessages lists the given definitions whose title key has no message in the base language.
func listMissingMessages(ws *workspace.Session, mods *index.Index, definitions []index.Site) []Finding {
	var findings []Finding
	for _, site := range definitions {
		key, ok := ws.FindTitleKey(mods, site)
		if !ok || !jomini.IsIdentifier(key) || hasMessage(mods, key, game.BaseLanguage) {
			continue
		}
		facts := lsp.DescribeReference(mods.ToReference(site))
		facts["key"] = key
		findings = append(findings, makeFinding("missing-message", facts, []FindingSite{makeSite(ws, mods, site)}))
	}
	return findings
}

// hasMessage reports whether a message with the given key exists in the given language.
func hasMessage(mods *index.Index, key, language string) bool {
	return lo.ContainsBy(mods.ListMessages(key), func(message index.Site) bool { return mods.Language(message) == language })
}

// listMessages returns the sites of the messages in the mods' files, in every language.
func listMessages(mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool { return mods.Occurrence(site).Role == index.RoleLocalizes })
}

// listCoverage lists, for each mod and each other language it writes, how many of the mod's base-language keys the
// language has too, among the given messages. A mod with no base-language keys has none.
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
			facts := lsp.DescribeReference(schema.Reference{Database: game.Localization, Name: language})
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

// listKeys returns the keys of the given messages, each once.
func listKeys(mods *index.Index, messages []index.Site) []string {
	return lo.Uniq(lo.Map(messages, func(site index.Site, _ int) string { return mods.ToReference(site).Name }))
}

// listDiagnostics lists the problems in every mod file, as the editor finds them, one row for each.
func listDiagnostics(ws *workspace.Session, mods *index.Index) []Finding {
	paths := lo.Map(mods.ListOwnFiles(), func(position int32, _ int) string { return mods.Files[position].Path })
	diagnostics := make([][]lsp.Diagnostic, len(paths))
	// the files are judged in parallel
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
	for position, target := range paths {
		if len(diagnostics[position]) == 0 {
			continue
		}
		place := ws.Locate(mods, target)
		for _, diagnostic := range diagnostics[position] {
			site := FindingSite{Place: place, Range: diagnostic.Range}
			findings = append(findings, makeFinding(diagnostic.Code, diagnostic.Facts, []FindingSite{site}))
		}
	}
	return findings
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

// setStop ends the running watch, if any, and keeps the given function as the one that ends the next. A nil function
// means no watch runs.
func (hlt *Service) setStop(stop func()) {
	hlt.mu.Lock()
	running := hlt.stop
	hlt.stop = stop
	hlt.mu.Unlock()
	if running != nil {
		running()
	}
}
