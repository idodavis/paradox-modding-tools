// Package health builds the report of what is wrong in the open workspace's mods. It also lists the game's error log
// and watches it while a workspace is open.
package health

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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

// EventErrorLogChanged is sent when the game's error log grows while a workspace is open.
const EventErrorLogChanged = "errorlog:changed"

// init registers the error log event with the application.
func init() { application.RegisterEvent[struct{}](EventErrorLogChanged) }

// Service is the Wails service behind the Health page.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex // guards stop
	stop      func()     // ends the error log's watch, or nil
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

// A Use is one name a finding is about, with how often the files the game reads use it and where they first do.
type Use struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	First lsp.Site `json:"first"`
}

// A Finding is one problem, or one fact about the mods.
type Finding struct {
	Code     string     `json:"code"`            // an lsp.Codes id
	Question string     `json:"question"`        // the code's question
	Level    string     `json:"level"`           // the code's level, or a contest's own
	Label    string     `json:"label"`           // the code's title
	Topic    string     `json:"topic"`           // the code's topic
	Message  []lsp.Part `json:"message"`         // the code's template filled with the facts, or a log row's line
	Brief    []lsp.Part `json:"brief,omitempty"` // the line under its name in a list
	Detail   string     `json:"detail"`          // its place, such as a.txt:12, or what a log row names
	Why      []lsp.Part `json:"why,omitempty"`   // why the winner's copy wins, or why a log row is a mod's
	Name     string     `json:"name"`
	Database string     `json:"database"` // what the database calls a definition
	// Origin is the mod the finding is about, or "" for the game's own log row.
	Origin string            `json:"origin"`
	Mods   []string          `json:"mods"` // the mods it concerns, in load order
	Facts  map[string]string `json:"facts"`
	Sites  []lsp.Site        `json:"sites"`
	// Removed holds the names the replaced sites define that the kept file does not.
	Removed []string `json:"removed,omitempty"`
	// Uses holds the removed names that the files the game reads still use, the most used first.
	Uses []Use `json:"uses,omitempty"`
	// Defined is where the workspace defines the name, in a mod the finding's mod does not depend on.
	Defined []lsp.Site `json:"defined,omitempty"`
	// Comparable is true when the kept statement and the ones it replaces can be shown side by side.
	Comparable bool     `json:"comparable"`
	Order      []string `json:"order,omitempty"` // a load order that fixes a contest
	// Count is how many findings or game log lines the row stands for: 1 unless a report folded findings into it or the
	// log repeated the line.
	Count int `json:"count"`
	// Repeats holds the sites of the findings a report folded into this one.
	Repeats []lsp.Site `json:"repeats,omitempty"`
	// Pairs are each two workspace mods the finding sets against each other, in load order, which can be ignored.
	Pairs [][2]string `json:"pairs"`
}

// A Change is one name whose kept copy moves to another mod in a previewed load order.
type Change struct {
	Database string `json:"database"` // what the database calls a definition
	Name     string `json:"name"`
	Before   string `json:"before"` // the mod whose copy the workspace's order keeps
	After    string `json:"after"`  // the mod whose copy the previewed order keeps
}

// A Tally is how many findings there are of each level.
type Tally struct {
	Counts map[string]int `json:"counts"` // by level, with every level in it
	// Phrases holds each level's count as a phrase, such as 235 critical problems, by level. A level with no finding
	// has none.
	Phrases map[string]string `json:"phrases"`
	Worst   string            `json:"worst"` // the worst level that has a finding, or "" if none has
}

// A Check is one kind of finding at one level, and how many findings it has.
type Check struct {
	Code  string `json:"code"`  // an lsp.Codes id
	Label string `json:"label"` // the code's title
	Level string `json:"level"`
	Count int    `json:"count"` // the findings it has, the ones folded into others included
}

// A Group is the findings of the workspace's compatibility or of one mod, folded, counted and sorted.
type Group struct {
	Tally
	Mod string `json:"mod"` // the mod's id, or "" for the workspace's compatibility
	// Findings are sorted by level, the worst first, then by check, the one with the most findings first, then by
	// weight. A mod's findings leave out its coverage.
	Findings []Finding `json:"findings"`
	// Languages holds the mod's coverage findings, one for each language beside the base one.
	Languages []Finding `json:"languages"`
	Checks    []Check   `json:"checks"` // in the order of the findings
}

// A Report is the findings for one load order, grouped and counted, and what that order changes.
type Report struct {
	Tally                   // all the findings of the compatibility and of the mods
	Compatibility Group     `json:"compatibility"`
	Mods          []Group   `json:"mods"` // in load order
	Own           Tally     `json:"own"`  // the findings of the mods together, the compatibility's left out
	CheckedAt     time.Time `json:"checkedAt"`
	Changes       []Change  `json:"changes"` // none for the workspace's own order
}

// A GameLog is what the game's last run wrote to its error log, folded into rows.
type GameLog struct {
	Findings  []Finding `json:"findings"`
	Path      string    `json:"path"`      // the log file's full path
	WrittenAt time.Time `json:"writtenAt"` // when the game last wrote it, or zero if there is no log
	// Unloaded holds the workspace's mods the run that wrote the log did not read, by id. Their rows stay the game's.
	Unloaded []string `json:"unloaded"`
	// Copies holds, by mod id, the folder of another copy of a workspace mod that the run read in its place.
	Copies map[string]string `json:"copies"`
}

// An Overview is what one mod holds, counted, and the mods it is judged with.
type Overview struct {
	Mod      string         `json:"mod"`
	Counts   map[string]int `json:"counts"` // definitions, by what a database calls them
	Files    int            `json:"files"`
	Messages int            `json:"messages"`
	// Judged is the ids of the workspace's mods its own problems are judged with: those it depends on, all the way down.
	Judged []string `json:"judged"`
}

// Compared is the texts of two statements.
type Compared struct {
	A string `json:"a"`
	B string `json:"b"`
}

// A Snippet is the code around one finding site.
type Snippet struct {
	Text string `json:"text"`
	Line int    `json:"line"` // the line it begins at, counted from one
	Last int    `json:"last"` // the last line of the site's own code, counted from one
}

// snippetContext is how many lines a snippet shows before and after a site of one line.
const snippetContext = 2

// coverageCode is the code of the finding that says how far a language covers a mod.
const coverageCode = "coverage"

// BuildReport builds the report of the mods' problems in a load order. An empty order means the workspace's own.
// Another order is not saved. The report lists what it changes. It returns an error if the mods cannot be scanned.
func (hlt *Service) BuildReport(order []string) (Report, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return Report{}, err
	}
	base := ws.Mods.Load()
	mods, ids := base, lo.Map(*ws.Order.Load(), func(mod workspace.Mod, _ int) string { return mod.ID })
	isPreview := len(order) > 0 && !slices.Equal(order, ids)
	if isPreview {
		if mods, err = ws.ScanOrder(order); err != nil {
			return Report{}, err
		}
		ids = order
	}
	// the compatibility checks judge the mods as if those left out were not loaded; each mod's own checks see them all
	record := hlt.findRecord(ws)
	checked := listChecked(ids, record)
	compatible := mods.Narrow(checked)
	kept, defined := map[schema.Reference]index.Winner{}, []schema.Reference{}
	for _, reference := range listDefined(compatible, listDefinitions(ws, compatible)) {
		if winner, ok := compatible.FindKept(ws.Schema, reference); ok {
			kept[reference] = winner
			defined = append(defined, reference)
		}
	}
	paths, declared := listShippedPaths(ws, compatible), listDeclared(ws)
	lists := []func() []Finding{
		func() []Finding {
			return lo.FlatMap(defined, func(reference schema.Reference, _ int) []Finding {
				return listDefinitionFindings(ws, compatible, reference, kept[reference])
			})
		},
		func() []Finding {
			return lo.FlatMap(paths, func(path string, _ int) []Finding { return listPathFindings(ws, compatible, path) })
		},
		func() []Finding { return listSharedMessages(ws, compatible, listMessages(compatible)) },
		func() []Finding { return listDescriptors(ws, mods) },
		func() []Finding { return listUnmounted(ws, mods) },
		func() []Finding { return listDiagnostics(ws, mods) },
		func() []Finding { return listCoverage(ws, mods, listMessages(mods)) },
	}
	results := make([][]Finding, len(lists))
	var group sync.WaitGroup
	for position, list := range lists {
		group.Go(func() { results[position] = list() })
	}
	group.Wait()
	findings, pairs := slices.Concat(results...), record.IgnoredPairs
	resolveFindings(ws, findings, ids, declared, pairs)
	report := groupReport(slices.Concat(findings, listPairFindings(ws, compatible, findings, pairs)), ids)
	if isPreview {
		previous := base.Narrow(checked)
		report.Changes = append(listDefinitionChanges(ws, previous, compatible, kept),
			listPathChanges(previous, compatible, paths)...)
		slices.SortFunc(report.Changes, compareChanges)
	}
	return report, nil
}

// resolveFindings grades the contests and shared localization keys of a load order. A contest the mods' declared
// dependencies or an ignored pair settle is lowered, and a contest the order decides gets the order that fixes it.
// Each finding then lists the pairs of mods it sets against each other.
func resolveFindings(ws *workspace.Session, findings []Finding, order []string, declared map[[2]string]bool, pairs [][2]string) {
	forgiven := listForgiven(declared, pairs)
	for position := range findings {
		finding := &findings[position]
		switch finding.Code {
		case contestCode, fileContestCode:
			resolveContest(ws, finding, forgiven)
			resolveOrder(finding, order, declared)
		case sharedMessageCode, undecidedCode:
			resolveForgiven(finding, forgiven)
			finding.Pairs = listEveryPair(finding.Mods)
		}
	}
}

// groupReport fills in the words of a report's findings, folds them and groups them: the workspace's compatibility,
// then each mod of the load order. A health finding about a mod outside the order is left out. The report has no
// changes.
func groupReport(findings []Finding, order []string) Report {
	for position := range findings {
		describeFinding(&findings[position])
	}
	folded := foldFindings(findings, func(finding Finding) [4]string {
		return [4]string{finding.Code, findOwner(finding), finding.Name, lsp.JoinParts(finding.Message)}
	}, func(first *Finding, repeat Finding) { first.Repeats = append(first.Repeats, repeat.Sites...) })
	compatibility, own := lo.FilterReject(folded, func(finding Finding, _ int) bool {
		return finding.Question == lsp.QuestionCompatibility
	})
	languages, problems := lo.FilterReject(own, func(finding Finding, _ int) bool { return finding.Code == coverageCode })
	problemsByMod, languagesByMod := lo.GroupBy(problems, findOwner), lo.GroupBy(languages, findOwner)
	mods := lo.Map(order, func(id string, _ int) Group { return makeGroup(id, problemsByMod[id], languagesByMod[id]) })
	modFindings := lo.FlatMap(mods, func(group Group, _ int) []Finding { return group.Findings })
	return Report{
		Tally:         makeTally(slices.Concat(compatibility, modFindings)),
		Compatibility: makeGroup("", compatibility, nil),
		Mods:          mods,
		Own:           makeTally(modFindings),
		CheckedAt:     time.Now(),
		Changes:       []Change{},
	}
}

// describeFinding fills in a finding's words from its code, its facts and its sites: its code's title and topic, its
// message, the line under its name in a list, its place, and why the winner's copy wins.
func describeFinding(finding *Finding) {
	code := lsp.LookupCode(finding.Code)
	finding.Label, finding.Topic = code.Label, code.Topic
	finding.Message = lsp.SplitMessage(code.Template(finding.Facts), finding.Facts)
	finding.Brief, finding.Detail, finding.Why = splitBrief(*finding), describeDetail(*finding), splitWhy(*finding)
}

// splitBrief returns the line under a finding's name in a list, as parts, its pieces apart by a dot. It says what kind
// of thing the name is, how many names a replaced file leaves out that are still used and how often, the mod that
// alone defines the name, and why the game keeps its copy. It is empty if the finding holds none of these.
func splitBrief(finding Finding) []lsp.Part {
	var parts []lsp.Part
	add := func(piece ...lsp.Part) {
		if len(parts) > 0 {
			parts = append(parts, lsp.Part{Text: " · "})
		}
		parts = append(parts, piece...)
	}
	if finding.Database != "" && finding.Database != game.File {
		add(lsp.Part{Text: finding.Database})
	}
	hasUses := len(finding.Uses) > 0
	if hasUses {
		add(lsp.SplitMessage(lo.Ternary(finding.Facts["count"] == "1", "{count} name still used · {uses} uses",
			"{count} names still used · {uses} uses"), finding.Facts)...)
	}
	if other := finding.Facts["other"]; other != "" {
		add(lsp.Part{Text: "only in "}, lsp.Part{Text: other, Style: "mod"})
	}
	// a finding's kept sites come first, so its first site is the one the game keeps where it keeps one
	if site, _ := lo.First(finding.Sites); site.Label != "" && !hasUses {
		add(lsp.Part{Text: site.Label})
	}
	return parts
}

// describeDetail returns a finding's place, such as a.txt:12: its first site's, which is the one the game keeps where
// it keeps one. A log row with no site says what it names instead. It is "" if there is nothing to add.
func describeDetail(finding Finding) string {
	if site, ok := lo.First(finding.Sites); ok {
		return site.Place.Rel + ":" + strconv.Itoa(site.Line)
	}
	return lo.Ternary(finding.Facts["named"] != "", "Names "+finding.Facts["named"], "")
}

// foldFindings folds each finding that has the key of an earlier one into that one. The earlier finding counts the
// other in its Count, and the given function adds whatever else the earlier one keeps of it. The folded findings stay
// in the order of their first.
func foldFindings[Key comparable](
	findings []Finding, key func(Finding) Key, merge func(first *Finding, repeat Finding),
) []Finding {
	return lo.Map(lo.PartitionBy(findings, key), func(group []Finding, _ int) Finding {
		first := group[0]
		for _, repeat := range group[1:] {
			first.Count += repeat.Count
			merge(&first, repeat)
		}
		return first
	})
}

// findOwner returns the id of the mod a finding is about: the first mod it concerns, else its origin.
func findOwner(finding Finding) string { return lo.FirstOr(finding.Mods, finding.Origin) }

// makeGroup returns a group of a mod's findings, or of the workspace's compatibility for the mod "". Its checks come
// the worst level first, and within a level the one with the most findings first; checks with as many stay in the
// order of their first finding. Its findings follow their checks, and within a check the heaviest come first.
func makeGroup(mod string, findings, languages []Finding) Group {
	checks := lo.PartitionBy(findings, func(finding Finding) [2]string { return [2]string{finding.Level, finding.Code} })
	slices.SortStableFunc(checks, func(a, b []Finding) int {
		levels := cmp.Compare(rankLevel(a[0].Level), rankLevel(b[0].Level))
		return cmp.Or(levels, cmp.Compare(countFindings(b), countFindings(a)))
	})
	for _, check := range checks {
		slices.SortStableFunc(check, func(a, b Finding) int { return cmp.Compare(weigh(b), weigh(a)) })
	}
	return Group{
		Tally: makeTally(findings), Mod: mod, Findings: lo.Flatten(checks), Languages: lo.CoalesceSliceOrEmpty(languages),
		Checks: lo.Map(checks, func(check []Finding, _ int) Check {
			return Check{Code: check[0].Code, Label: check[0].Label, Level: check[0].Level, Count: countFindings(check)}
		}),
	}
}

// rankLevel returns where a level comes among the levels of a finding, the worst first. It returns -1 for a level that
// is not one of them.
func rankLevel(level string) int {
	return slices.IndexFunc(lsp.Levels, func(row lsp.Level) bool { return row.ID == level })
}

// countFindings returns how many findings some findings stand for, the ones folded into them included.
func countFindings(findings []Finding) int {
	return lo.SumBy(findings, func(finding Finding) int { return finding.Count })
}

// makeTally counts some findings by level, each as many times as its Count says, and words each count.
func makeTally(findings []Finding) Tally {
	tally := Tally{Counts: map[string]int{}, Phrases: map[string]string{}}
	for _, level := range lsp.Levels {
		tally.Counts[level.ID] = 0
	}
	for _, finding := range findings {
		tally.Counts[finding.Level] += finding.Count
	}
	for _, level := range lsp.Levels {
		if count := tally.Counts[level.ID]; count > 0 {
			tally.Phrases[level.ID] = lsp.Plural(count, level.Noun, level.Nouns)
			tally.Worst = cmp.Or(tally.Worst, level.ID)
		}
	}
	return tally
}

// weigh returns how bad a finding is among those of its check: how often the files the game reads use the names a
// replaced file leaves out, else 1, as many times as the finding's Count says.
func weigh(finding Finding) int {
	return finding.Count * max(1, lo.SumBy(finding.Uses, func(use Use) int { return use.Count }))
}

// ListCodes returns every problem code.
func (hlt *Service) ListCodes() []lsp.Code { return lsp.Codes }

// ListLevels returns the levels of a finding, the worst first.
func (hlt *Service) ListLevels() []lsp.Level { return lsp.Levels }

// ListTopics returns the topics of the checks, in the order they are listed.
func (hlt *Service) ListTopics() []lsp.Topic { return lsp.Topics }

// ListOverview counts the definitions, files and messages of each mod in the workspace, and lists the mods each is
// judged with.
func (hlt *Service) ListOverview() ([]Overview, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	positions := mods.ListOwnFiles()
	return lo.Map(*ws.Order.Load(), func(mod workspace.Mod, _ int) Overview {
		overview := Overview{Mod: mod.ID, Counts: map[string]int{}, Judged: lo.CoalesceSliceOrEmpty(ws.ListDependencies(mod.ID))}
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
func (hlt *Service) CompareStatements(a, b lsp.Site) (Compared, error) {
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

// ListSnippets returns the code at each finding site, at most ten lines of it: the lines of its range, or the statement
// that starts on its line if it has no range. A site of one line also gets the two lines on each side. An open file's
// code is the editor's text. It returns an error if a file cannot be read.
func (hlt *Service) ListSnippets(sites []lsp.Site) ([]Snippet, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	snippets := make([]Snippet, len(sites))
	for position, site := range sites {
		tree, err := ws.LoadTree(site.Place.Path)
		if err != nil {
			return nil, err
		}
		last := tree.Lines.CountLines() - 1
		first := min(max(site.Line-1, 0), last)
		end := min(max(site.Range.EndLine-1, first), last, first+9)
		if statement, ok := findLineStatement(tree, site.Line); site.Range.EndLine == 0 && ok {
			end = min(tree.Lines.ToPosition(tree.Statements[statement].Span.End).Line, first+9)
		}
		around := lo.Ternary(end == first, snippetContext, 0)
		from, to := max(first-around, 0), min(end+around, last)
		snippets[position] = Snippet{
			Text: tree.Src[tree.Lines.LineSpan(from).Start:tree.Lines.LineSpan(to).End], Line: from + 1, Last: end + 1,
		}
	}
	return snippets, nil
}

// ListReaders returns every place in script and localization that reads a localization key.
func (hlt *Service) ListReaders(key string) ([]lsp.Site, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	return lo.Map(mods.ListUses(game.Localization, key), func(site index.Site, _ int) lsp.Site {
		return lsp.MakeSite(ws, mods, site)
	}), nil
}

// findStatementText returns the text of the statement that starts on a finding site's line. It returns an error if the
// file cannot be read or no statement starts there.
func findStatementText(ws *workspace.Session, site lsp.Site) (string, error) {
	tree, err := ws.LoadTree(site.Place.Path)
	if err != nil {
		return "", err
	}
	statement, ok := findLineStatement(tree, site.Line)
	if !ok {
		return "", fmt.Errorf("no statement starts at line %d of %s", site.Line, site.Place.Path)
	}
	return tree.Text(tree.Statements[statement].Span), nil
}

// WriteScaffold writes the localization keys a mod lacks in a language, with the base text as the placeholder, and
// returns the files it wrote. A key goes at the end of the language's file that matches the base-language file it is
// in, which is created if it does not exist. It returns an error if the mod or the language is not valid, or a file
// cannot be written.
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
	scaffolds := ListScaffolds(ws.Mods.Load(), mod, language)
	paths := slices.Sorted(maps.Keys(scaffolds))
	for position, target := range paths {
		if err := hlt.writeMessages(target, scaffolds[target]); err != nil {
			return paths[:position], err
		}
	}
	// a file the rescan cannot read waits for the watcher
	_ = ws.Rescan(lo.Map(paths, func(target string, _ int) workspace.FileChange { return workspace.FileChange{Path: target} }))
	return paths, nil
}

// writeMessages adds messages at the end of a localization file, in the file's own line ending and encoding. A file
// that is missing or holds nothing gets its header first, in the language its name ends in. It returns an error if
// the file cannot be read or written, or its name ends in no language.
func (hlt *Service) writeMessages(path string, messages []loc.Message) error {
	text, err := files.LoadText(path)
	if err == nil && strings.TrimSpace(text.Text) == "" {
		if err = hlt.Workspace.CreateLocalization(path); err == nil {
			text, err = files.LoadText(path)
		}
	}
	if err != nil {
		return err
	}
	text.Text += loc.EncodeAddition(text.Text, messages)
	return hlt.Workspace.Write(path, text)
}

// ListScaffolds returns the base-language messages of a mod whose key has no message in a language, by the file of
// that language they belong in. That file may exist already.
func ListScaffolds(mods *index.Index, mod workspace.Mod, language string) map[string][]loc.Message {
	messages := lo.Filter(listMessages(mods), func(site index.Site, _ int) bool { return mods.File(site).Origin == mod.ID })
	scaffolds := map[string][]loc.Message{}
	for _, site := range listUntranslated(mods, lo.GroupBy(messages, mods.Language), language) {
		target := files.Join(mod.Dir, loc.ToLanguageRel(mods.File(site).Rel, language))
		scaffolds[target] = append(scaffolds[target], loc.Message{Key: mods.ToReference(site).Name, Text: mods.Text(site)})
	}
	return scaffolds
}

// listDefined returns each defined name once.
func listDefined(mods *index.Index, definitions []index.Site) []schema.Reference {
	return lo.Uniq(lo.Map(definitions, func(site index.Site, _ int) schema.Reference { return mods.ToReference(site) }))
}

// listDefinitions returns the mods' definitions in known databases, without aliases.
func listDefinitions(ws *workspace.Session, mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool {
		_, known := ws.Schema.Databases[mods.ToReference(site).Database]
		occurrence := mods.Occurrence(site)
		return occurrence.Role == index.RoleDefines && !occurrence.Alias && known
	})
}

// listModSites returns the occurrences in the mods' files that pass a test.
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

// hasWinner reports whether one copy of a name wins: its database does not keep every copy, and a source says which
// copy the game keeps. It is false for a database the schema lacks.
func hasWinner(ws *workspace.Session, reference schema.Reference, winner index.Winner) bool {
	database := ws.Schema.Databases[reference.Database]
	return database != nil && !database.Precedence.KeepsEvery() && winner.Reason != index.ReasonUnsettled
}

// listLoaded returns the sites that lie in files the game reads, in the order given.
func listLoaded(mods *index.Index, sites []index.Site) []index.Site {
	return lo.Filter(sites, func(site index.Site, _ int) bool { return mods.IsLoaded(site.File) })
}

// makeFinding returns a finding of a problem code. Its kept sites come first. Its origin is the last kept site's, or
// the first site's if none is kept.
func makeFinding(mods *index.Index, id string, facts map[string]string, sites []lsp.Site) Finding {
	kept, replaced := lo.FilterReject(sites, func(site lsp.Site, _ int) bool { return site.Kept })
	sites = slices.Concat(kept, replaced)
	origin := ""
	switch {
	case len(kept) > 0:
		origin = kept[len(kept)-1].Place.Origin
	case len(sites) > 0:
		origin = sites[0].Place.Origin
	}
	code := lsp.LookupCode(id)
	return Finding{
		Code: id, Question: code.Question, Level: code.Level, Name: facts["name"], Database: facts["database"], Origin: origin,
		Mods:  listMods(mods, lo.Map(sites, func(site lsp.Site, _ int) string { return site.Place.Origin })),
		Facts: facts, Sites: sites, Count: 1,
	}
}

// listMods returns mods, each once, in load order. The game is left out.
func listMods(mods *index.Index, origins []string) []string {
	return lo.FilterMap(mods.Roots, func(root files.Root, _ int) (string, bool) {
		return root.Origin, root.Origin != game.OriginVanilla && slices.Contains(origins, root.Origin)
	})
}

// listRemoved returns the names that the sites the game does not keep define and the kept sites do not, each once.
func listRemoved(sites []lsp.Site) []string {
	kept, replaced := lo.FilterReject(sites, func(site lsp.Site, _ int) bool { return site.Kept })
	defines := func(site lsp.Site, _ int) []string { return site.Defines }
	removed, _ := lo.Difference(lo.FlatMap(replaced, defines), lo.FlatMap(kept, defines))
	return lo.Uniq(removed)
}

// findLineStatement returns the statement that starts on a line of a tree, counted from one, or false if none does.
func findLineStatement(tree *jomini.Tree, line int) (int32, bool) {
	span := tree.Lines.LineSpan(min(max(line-1, 0), tree.Lines.CountLines()-1))
	text := tree.Src[span.Start:span.End]
	return tree.LookupStatement(span.Start + len(text) - len(strings.TrimLeft(text, " \t")))
}

// isMod reports whether a site lies in a mod's file.
func isMod(mods *index.Index, site index.Site) bool {
	return mods.File(site).Origin != game.OriginVanilla
}

// listShippedPaths returns the paths the mods ship in the folders the game reads, each once, sorted without regard to
// case. Files that differ only in their stage folder share a path. A file the game never reads, which hides no other,
// is left out.
func listShippedPaths(ws *workspace.Session, mods *index.Index) []string {
	var paths []string
	groups := map[string]bool{} // by the key of the first file at a path
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		loaded, hidden, ok := mods.FindLoaded(file.Rel)
		group := lo.Ternary(ok, append(hidden, loaded), hidden)
		if !isGameFolder(ws, file.Rel) ||
			!slices.ContainsFunc(group, func(other files.File) bool { return other.Key == file.Key }) || groups[group[0].Key] {
			continue
		}
		groups[group[0].Key] = true
		paths = append(paths, file.Rel)
	}
	slices.SortFunc(paths, func(a, b string) int { return cmp.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return paths
}

// makeFileSites returns the finding sites of the files at a path, in load order. The file the game reads comes last,
// kept for the reason path where it hides others.
func makeFileSites(ws *workspace.Session, mods *index.Index, path string) []lsp.Site {
	loaded, hidden, ok := mods.FindLoaded(path)
	if ok {
		hidden = append(hidden, loaded)
	}
	sites := lo.Map(hidden, func(file files.File, _ int) lsp.Site {
		position, _ := mods.FindFile(file.Path)
		return makeFileSite(ws, mods, position)
	})
	if ok && len(sites) > 1 {
		sites[len(sites)-1].Reason, sites[len(sites)-1].Label = index.ReasonPath, index.Reasons[index.ReasonPath].Label
	}
	return sites
}

// makeFileSite returns the finding site of a whole file, at its first line. It is kept if the game loads the file.
func makeFileSite(ws *workspace.Session, mods *index.Index, position int32) lsp.Site {
	return lsp.Site{
		Place: ws.Locate(mods, mods.Files[position].Path), Line: 1, Range: lsp.ToLineRange(1),
		Kept: mods.IsLoaded(position),
		Defines: lo.Map(listFileDefinitions(mods, position), func(occurrence index.Occurrence, _ int) string {
			return mods.Names.Texts[occurrence.Name]
		}),
	}
}

// listFileDefinitions returns the occurrences that define the names of a file, in the order written. A name the file
// saves while the game runs, such as a saved scope, is none of them.
func listFileDefinitions(mods *index.Index, position int32) []index.Occurrence {
	return lo.Filter(mods.Occurrences[position], func(occurrence index.Occurrence, _ int) bool {
		return occurrence.Role == index.RoleDefines && !occurrence.Alias &&
			!slices.Contains(game.Links, mods.Databases.Texts[occurrence.Database])
	})
}

// listUnused returns an unused copy for each mod none of whose copies of a name the game uses, where it keeps its own.
// Each points at the game's copies and that mod's. The game is its winner and the mod its beaten one.
func listUnused(ws *workspace.Session, mods *index.Index, sites []index.Site, winner index.Winner) []Finding {
	reference := mods.ToReference(sites[0])
	byMod := lo.GroupBy(lo.Filter(sites, func(site index.Site, _ int) bool { return isMod(mods, site) }),
		func(site index.Site) string { return mods.File(site).Origin })
	var findings []Finding
	for _, mod := range listMods(mods, slices.Collect(maps.Keys(byMod))) {
		if lo.SomeBy(byMod[mod], winner.IsKept) {
			continue
		}
		facts := lsp.DescribeReference(reference)
		facts["winner"], facts["beaten"] = ws.DescribeMod(mods.File(winner.Site).Origin), ws.DescribeMod(mod)
		own := lo.Filter(sites, func(site index.Site, _ int) bool { return !isMod(mods, site) || mods.File(site).Origin == mod })
		findings = append(findings, makeFinding(mods, "unused-copy", facts, lsp.MakeSites(ws, mods, own, &winner)))
	}
	return findings
}

// describeRemoved fills in the finding of a mod's file that removes names in use. It sets the names still used, and
// the facts the message counts: the mod, how many names the game's copy defines and what they are called, how many of
// them the mod's copy leaves out, and how many of those are still used and how often.
func describeRemoved(ws *workspace.Session, mods *index.Index, finding *Finding, uses []Use) {
	original, _ := lo.Find(finding.Sites, func(site lsp.Site) bool { return site.Place.ReadOnly })
	kept, _ := lo.Find(finding.Sites, func(site lsp.Site) bool { return site.Kept })
	position, _ := mods.FindFile(original.Place.Path)
	finding.Uses = uses
	finding.Facts["mod"] = ws.DescribeMod(finding.Origin)
	finding.Facts["total"] = strconv.Itoa(len(lo.Uniq(original.Defines)))
	finding.Facts["definitions"] = describeDefinitions(mods, position)
	finding.Facts["removed"] = strconv.Itoa(len(listRemoved([]lsp.Site{kept, original})))
	finding.Facts["count"] = strconv.Itoa(len(uses))
	finding.Facts["uses"] = strconv.Itoa(lo.SumBy(uses, func(use Use) int { return use.Count }))
}

// describeDefinitions returns what the definitions of a file are called, in the plural, such as "scripted triggers".
// It returns "names" for a file whose definitions are of more than one kind.
func describeDefinitions(mods *index.Index, position int32) string {
	databases := lo.Uniq(lo.Map(listFileDefinitions(mods, position), func(occurrence index.Occurrence, _ int) string {
		return mods.Databases.Texts[occurrence.Database]
	}))
	if len(databases) != 1 {
		return "names"
	}
	return game.DescribeDefinition(databases[0], true)
}

// listRemovedUses returns the names the game's copy of a file defines that no file the game reads defines, each with
// the uses the files the game reads make of it, the most used first. A name they do not use is left out, and so is a
// name the copy defines for itself alone or saves while the game runs.
func listRemovedUses(ws *workspace.Session, mods *index.Index, path string) []Use {
	position, ok := mods.FindFile(path)
	if !ok {
		return nil
	}
	var uses []Use
	seen := map[schema.Reference]bool{}
	for occurrence, written := range mods.Occurrences[position] {
		reference := mods.ToReference(index.Site{File: position, Occurrence: int32(occurrence)})
		if written.Role != index.RoleDefines || written.Alias || written.Local || seen[reference] ||
			slices.Contains(game.Links, reference.Database) ||
			len(listLoaded(mods, mods.Resolve(reference))) > 0 {
			continue
		}
		seen[reference] = true
		loaded := listLoaded(mods, mods.ListUses(reference.Database, reference.Name))
		if len(loaded) > 0 {
			uses = append(uses, Use{Name: reference.Name, Count: len(loaded), First: lsp.MakeSite(ws, mods, loaded[0])})
		}
	}
	slices.SortStableFunc(uses, func(a, b Use) int { return cmp.Compare(b.Count, a.Count) })
	return uses
}

// listDeclared returns the pairs of mods where the first depends on the second, directly or down the chain. A
// dependency the workspace lacks is left out.
func listDeclared(ws *workspace.Session) map[[2]string]bool {
	declared := map[[2]string]bool{}
	for _, mod := range *ws.Order.Load() {
		for _, dependency := range ws.ListDependencies(mod.ID) {
			declared[[2]string{mod.ID, dependency}] = true
		}
	}
	return declared
}

// listDescriptors lists the mods' missing dependencies, each named by the title its relationship gives where it gives
// one. A mod without a readable descriptor has none.
func listDescriptors(ws *workspace.Session, mods *index.Index) []Finding {
	var findings []Finding
	order := *ws.Order.Load()
	for _, mod := range order {
		descriptor := mod.Descriptor
		if descriptor.Path == "" {
			continue
		}
		sites := []lsp.Site{{Place: ws.Locate(mods, descriptor.Path), Line: 1, Range: lsp.ToLineRange(1)}}
		for _, dependency := range descriptor.Dependencies {
			if _, ok := workspace.FindMod(order, dependency); !ok {
				facts := lsp.DescribeReference(schema.Reference{Name: cmp.Or(dependency.Title, dependency.ID)})
				facts["mod"] = ws.DescribeMod(mod.ID)
				findings = append(findings, makeFinding(mods, "missing-dependency", facts, sites))
			}
		}
	}
	return findings
}

// listUnmounted lists the top folders of the mods that lie outside the stage folders of a game that keeps its files
// under stages, one finding for each, pointing at its first file. A dot folder and a file at a mod's root are left out.
func listUnmounted(ws *workspace.Session, mods *index.Index) []Finding {
	type folder struct{ origin, name string }
	var order []folder
	positions := map[folder][]int32{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		name, _, nested := strings.Cut(file.Rel, "/")
		if !nested || strings.HasPrefix(name, ".") || !ws.Game.IsUnmounted(file) {
			continue
		}
		key := folder{file.Origin, name}
		if _, listed := positions[key]; !listed {
			order = append(order, key)
		}
		positions[key] = append(positions[key], position)
	}
	return lo.Map(order, func(key folder, _ int) Finding {
		facts := map[string]string{"name": key.name, "count": strconv.Itoa(len(positions[key]))}
		site := lsp.Site{Place: ws.Locate(mods, mods.Files[positions[key][0]].Path), Line: 1, Range: lsp.ToLineRange(1)}
		return makeFinding(mods, "unmounted-folder", facts, []lsp.Site{site})
	})
}

// listMessages returns the messages in the mods' files, in every language.
func listMessages(mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool { return mods.Occurrence(site).Role == index.RoleLocalizes })
}

// listCoverage lists how many of each mod's base-language keys each of its other languages has. A mod with no
// base-language keys has none.
func listCoverage(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	var findings []Finding
	byMod := lo.GroupBy(messages, func(site index.Site) string { return mods.File(site).Origin })
	for _, mod := range *ws.Order.Load() {
		byLanguage := lo.GroupBy(byMod[mod.ID], mods.Language)
		base := listKeys(mods, byLanguage[game.BaseLanguage])
		if len(base) == 0 {
			continue
		}
		for _, language := range slices.Sorted(maps.Keys(byLanguage)) {
			if language == game.BaseLanguage {
				continue
			}
			facts := lsp.DescribeReference(schema.Reference{Database: game.Localization, Name: language})
			facts["mod"] = ws.DescribeMod(mod.ID)
			facts["have"] = strconv.Itoa(len(base) - len(listUntranslated(mods, byLanguage, language)))
			facts["of"] = strconv.Itoa(len(base))
			positions := lo.Uniq(lo.Map(byLanguage[language], func(site index.Site, _ int) int32 { return site.File }))
			findings = append(findings, makeFinding(mods, coverageCode, facts, lo.Map(positions, func(position int32, _ int) lsp.Site {
				return makeFileSite(ws, mods, position)
			})))
		}
	}
	return findings
}

// listUntranslated returns the base-language messages whose key has no message in a language, one for each key. The
// messages are one mod's, by their language.
func listUntranslated(mods *index.Index, byLanguage map[string][]index.Site, language string) []index.Site {
	translated := lo.Keyify(listKeys(mods, byLanguage[language]))
	untranslated := lo.Reject(byLanguage[game.BaseLanguage], func(site index.Site, _ int) bool {
		_, ok := translated[mods.ToReference(site).Name]
		return ok
	})
	return lo.UniqBy(untranslated, func(site index.Site) string { return mods.ToReference(site).Name })
}

// listKeys returns each message's key once.
func listKeys(mods *index.Index, messages []index.Site) []string {
	return lo.Uniq(lo.Map(messages, func(site index.Site, _ int) string { return mods.ToReference(site).Name }))
}

// listDiagnostics lists the editor's problems in every mod file and in each mod's descriptor, one finding each.
func listDiagnostics(ws *workspace.Session, mods *index.Index) []Finding {
	paths := lo.FilterMap(mods.ListOwnFiles(), func(position int32, _ int) (string, bool) {
		return mods.Files[position].Path, mods.Files[position].Format != files.FormatDescriptor
	})
	for _, mod := range *ws.Order.Load() {
		if mod.Descriptor.Path != "" {
			paths = append(paths, mod.Descriptor.Path)
		}
	}
	diagnostics := make([][]lsp.Diagnostic, len(paths))
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for position, target := range paths {
		group.Go(func() error {
			// a file that cannot be read has no problems
			diagnostics[position], _ = lsp.DiagnoseFile(ws, mods, target)
			return nil
		})
	}
	_ = group.Wait()
	var findings []Finding
	for position, target := range paths {
		place := ws.Locate(mods, target)
		for _, diagnostic := range diagnostics[position] {
			site := lsp.Site{Place: place, Line: diagnostic.Range.StartLine, Range: diagnostic.Range}
			finding := makeFinding(mods, diagnostic.Code, diagnostic.Facts, []lsp.Site{site})
			finding.Defined = diagnostic.Defined
			findings = append(findings, finding)
		}
	}
	return findings
}

// setStop ends the running watch, if any. It keeps a function that ends the next watch, or nil for none.
func (hlt *Service) setStop(stop func()) {
	hlt.mu.Lock()
	running := hlt.stop
	hlt.stop = stop
	hlt.mu.Unlock()
	if running != nil {
		running()
	}
}
