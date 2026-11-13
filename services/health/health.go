// Package health builds the report of what is wrong in the open workspace's
// mods. It also lists the game's error log and watches it while a workspace is
// open.
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
	"weak"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// EventErrorLogChanged is sent when the game's error log grows while a
// workspace is open.
const EventErrorLogChanged = "errorlog:changed"

// init registers the error log event with the application.
func init() { application.RegisterEvent[struct{}](EventErrorLogChanged) }

// Service is the Wails service behind the Health page.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex        // guards stop and reports
	stop      func()            // ends the error log's watch, or nil
	reports   map[string]*build // the reports kept, by the load order each judges
}

// A build is one load order's report, built or being built, which every caller
// that asks for it shares.
type build struct {
	stamp  stamp
	done   chan struct{} // closed once the report is built
	report Report
	err    error
}

// A stamp is what a report is built from. A kept report is out of date once the
// workspace's stamp is another. Its weak pointers keep no session, index or
// load order alive.
type stamp struct {
	session weak.Pointer[workspace.Session]
	mods    weak.Pointer[index.Index]
	order   weak.Pointer[[]workspace.Mod]
	edits   int64 // the session's count of the editor's changes to its buffers
	config  int   // config.json's revision
}

// ServiceStartup watches the game's error log while a workspace is open. The
// watch stops when the workspace closes.
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
	events.On(workspace.EventClosed, func(*application.CustomEvent) {
		hlt.setStop(nil)
		hlt.mu.Lock()
		defer hlt.mu.Unlock()
		clear(hlt.reports)
	})
	return nil
}

// ServiceShutdown stops the error log's watch.
func (hlt *Service) ServiceShutdown() error {
	hlt.setStop(nil)
	return nil
}

// A Use is one name a finding is about, with how often the files the game reads
// use it and where they first do.
type Use struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	First lsp.Site `json:"first"`
}

// A Finding is one problem, or one fact about the mods.
type Finding struct {
	Code       string            `json:"code"`            // an lsp.Codes id
	Question   string            `json:"question"`        // the code's question, or compatibility where another mod is concerned
	Level      string            `json:"level"`           // the code's level, or a contest's own
	Label      string            `json:"label"`           // the code's title
	Topic      string            `json:"topic"`           // the code's topic
	Message    []lsp.Part        `json:"message"`         // the code's template filled with the facts, or a log row's line
	Brief      []lsp.Part        `json:"brief,omitempty"` // the line under its name in a list
	Detail     string            `json:"detail"`          // its place, such as a.txt:12, or what a log row names
	Why        []lsp.Part        `json:"why,omitempty"`   // why the winner's copy wins, or why a log row is a mod's
	Name       string            `json:"name"`
	Database   string            `json:"database"` // what the database calls a definition
	Origin     string            `json:"origin"`   // the mod the finding is about; "" for the game's own log row
	Mods       []string          `json:"mods"`     // the mods it concerns, in load order
	Facts      map[string]string `json:"facts"`
	Sites      []lsp.Site        `json:"sites"`
	Removed    []string          `json:"removed,omitempty"` // the names the replaced sites define that the kept file does not
	Uses       []Use             `json:"uses,omitempty"`    // the removed names that the files the game reads still use, the most used first
	Defined    []lsp.Site        `json:"defined,omitempty"` // where the workspace defines the name, in a mod the finding's mod does not depend on
	Diagnosed  bool              `json:"diagnosed"`         // one of the editor's own diagnostics, which the editor shows itself
	Comparable bool              `json:"comparable"`        // the kept statement and the ones it replaces can be shown side by side
	Order      []string          `json:"order,omitempty"`   // a load order that fixes a contest
	Count      int               `json:"count"`             // how many findings or game log lines the row stands for: 1 unless a report folded findings into it or the log repeated the line
	Repeats    []lsp.Site        `json:"repeats,omitempty"` // the sites of the findings a report folded into this one
	Pairs      [][2]string       `json:"pairs"`             // each two workspace mods the finding sets against each other, in load order, which can be ignored
}

// A Change is one name whose kept copy moves to another mod in a previewed load
// order.
type Change struct {
	Database string `json:"database"` // what the database calls a definition
	Name     string `json:"name"`
	Before   string `json:"before"` // the mod whose copy the workspace's order keeps
	After    string `json:"after"`  // the mod whose copy the previewed order keeps
}

// A Tally is how many findings there are of each level.
type Tally struct {
	Counts  map[string]int    `json:"counts"`  // by level, with every level in it
	Phrases map[string]string `json:"phrases"` // each level's count as a phrase, such as 235 critical problems, by level; a level with no finding has none
	Worst   string            `json:"worst"`   // the worst level that has a finding, or "" if none has
}

// A Check is one kind of finding at one level, and how many findings it has.
type Check struct {
	Code  string `json:"code"`  // an lsp.Codes id
	Label string `json:"label"` // the code's title
	Level string `json:"level"`
	Count int    `json:"count"` // the findings it has, the ones folded into others included
}

// A Group is the findings of the workspace's compatibility or of one mod,
// folded, counted and sorted.
type Group struct {
	Tally
	Mod       string    `json:"mod"`       // the mod's id, or "" for the workspace's compatibility
	Findings  []Finding `json:"findings"`  // sorted by level, the worst first, then by check, the one with the most findings first, then by weight; a mod's leave out its coverage
	Languages []Finding `json:"languages"` // the mod's coverage findings, one for each language beside the base one
	Checks    []Check   `json:"checks"`    // in the order of the findings
}

// A Report is the findings for one load order, grouped and counted, and what
// that order changes.
type Report struct {
	Tally                   // all the findings of the compatibility and of the mods
	Compatibility Group     `json:"compatibility"`
	Mods          []Group   `json:"mods"` // in load order
	Own           Tally     `json:"own"`  // the findings of the user's own mods together, external mods' left out
	CheckedAt     time.Time `json:"checkedAt"`
	Changes       []Change  `json:"changes"` // none for the workspace's own order
}

// A GameLog is what the game's last run wrote to its error log, folded into
// rows.
type GameLog struct {
	Findings  []Finding         `json:"findings"`
	Path      string            `json:"path"`      // the log file's full path
	WrittenAt time.Time         `json:"writtenAt"` // when the game last wrote it, or zero if there is no log
	Unloaded  []string          `json:"unloaded"`  // the workspace's mods the run that wrote the log did not read, by id; their rows stay the game's
	Copies    map[string]string `json:"copies"`    // by mod id, the folder of another copy of a workspace mod that the run read in its place
}

// An Overview is what one mod holds, counted, and the mods it is judged with.
type Overview struct {
	Mod      string         `json:"mod"`
	Counts   map[string]int `json:"counts"` // definitions, by what a database calls them
	Files    int            `json:"files"`
	Messages int            `json:"messages"`
	Judged   []string       `json:"judged"` // the ids of the mods this mod's own problems are judged with: the mods it depends on, directly or not
}

// A Compared is the texts of two statements, as the two sides of a compare.
type Compared struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

// A Snippet is the code around one finding site.
type Snippet struct {
	Text string `json:"text"`
	Line int    `json:"line"` // the line it begins at, counted from one
	Last int    `json:"last"` // the last line of the site's own code, counted from one
}

// snippetContext is how many lines a snippet shows before and after a site of
// one line.
const snippetContext = 2

// coverageCode is the code of the finding that says how far a language covers a
// mod.
const coverageCode = "coverage"

// BuildReport returns the report of the mods' problems in a load order. An
// empty order means the workspace's own. Another order is not saved. The report
// lists what it changes. A report is built once and kept until the workspace,
// its files, the editor's unsaved text or config.json change, and a caller that
// asks while it is built waits for it. It returns an error if the mods cannot
// be scanned.
func (hlt *Service) BuildReport(order []string) (Report, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return Report{}, err
	}
	if len(order) == 0 {
		order = lo.Map(*ws.Order.Load(), func(mod workspace.Mod, _ int) string { return mod.ID })
	}
	kept, isNew := hlt.findBuild(ws, order)
	if isNew {
		kept.report, kept.err = hlt.makeReport(ws, order)
		close(kept.done)
	}
	<-kept.done
	return kept.report, kept.err
}

// findBuild returns the build of a load order's report for the workspace as it
// is now, and whether the caller is to build it. A kept build of another stamp,
// or one that failed, is replaced. The workspace's own order and the last
// preview are kept.
func (hlt *Service) findBuild(ws *workspace.Session, order []string) (*build, bool) {
	now := stamp{
		session: weak.Make(ws),
		mods:    weak.Make(ws.Mods.Load()),
		order:   weak.Make(ws.Order.Load()),
		edits:   ws.Edits.Load(),
		config:  hlt.Workspace.Config.Revision(),
	}
	key := strings.Join(order, " ")
	own := strings.Join(lo.Map(*ws.Order.Load(), func(mod workspace.Mod, _ int) string { return mod.ID }), " ")
	hlt.mu.Lock()
	defer hlt.mu.Unlock()
	if kept, ok := hlt.reports[key]; ok && kept.stamp == now && !kept.failed() {
		return kept, false
	}
	hlt.reports = lo.PickBy(hlt.reports, func(other string, kept *build) bool {
		return kept.stamp == now && (other == own || key == own)
	})
	next := &build{stamp: now, done: make(chan struct{})}
	hlt.reports[key] = next
	return next, true
}

// failed reports whether the build is done and ended in an error.
func (kept *build) failed() bool {
	select {
	case <-kept.done:
		return kept.err != nil
	default:
		return false
	}
}

// makeReport builds the report of the mods' problems in a load order. The
// workspace's own order is the base, and another is a preview. It returns an
// error if the mods cannot be scanned.
func (hlt *Service) makeReport(ws *workspace.Session, order []string) (Report, error) {
	base := ws.Mods.Load()
	mods, ids := base, lo.Map(*ws.Order.Load(), func(mod workspace.Mod, _ int) string { return mod.ID })
	isPreview := !slices.Equal(order, ids)
	if isPreview {
		scanned, err := ws.ScanOrder(order, func(int) {})
		if err != nil {
			return Report{}, err
		}
		mods, ids = scanned, order
	}
	// the compatibility checks judge the mods as if those left out were not
	// loaded; each mod's own checks see them all
	record, _ := lo.Find(hlt.Workspace.Config.Config().Workspaces, func(record app.Workspace) bool { return record.ID == ws.ID })
	checked := listChecked(ids, record)
	compatible := mods.Narrow(checked)
	kept, defined := map[lang.Reference]index.Winner{}, []lang.Reference{}
	references := lo.Map(listDefinitions(ws, compatible), func(site index.Site, _ int) lang.Reference {
		return compatible.ToReference(site)
	})
	for _, reference := range lo.Uniq(references) {
		if winner, ok := compatible.FindKept(ws.Schema, reference); ok {
			kept[reference] = winner
			defined = append(defined, reference)
		}
	}
	paths, declared := listShippedPaths(compatible), listDeclared(ws)
	lists := []func() []Finding{
		func() []Finding {
			return lo.FlatMap(defined, func(reference lang.Reference, _ int) []Finding {
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
	findings, pairs := dropRepeatedTies(slices.Concat(results...)), record.IgnoredPairs
	resolveFindings(ws, findings, ids, declared, pairs)
	pairFindings := listPairFindings(ws, compatible, findings, pairs)
	report := groupReport(slices.Concat(findings, pairFindings), ids, hlt.listExternal())
	if isPreview {
		previous := base.Narrow(checked)
		report.Changes = append(listDefinitionChanges(ws, previous, compatible, kept),
			listPathChanges(previous, compatible, paths)...)
		slices.SortFunc(report.Changes, compareChanges)
	}
	return report, nil
}

// dropRepeatedTies returns the findings without a file's priority tie for
// a name that an undecided contest reports, so that one tie between several
// mods is shown once, with every mod it lies in.
func dropRepeatedTies(findings []Finding) []Finding {
	undecided := map[string]bool{}
	for _, finding := range findings {
		if finding.Code == undecidedCode {
			undecided[finding.Facts["database"]+":"+finding.Facts["name"]] = true
		}
	}
	return slices.DeleteFunc(findings, func(finding Finding) bool {
		return finding.Code == index.ProblemPriority && undecided[finding.Facts["database"]+":"+finding.Facts["name"]]
	})
}

// resolveFindings grades the contests and shared localization keys of a load
// order, and lists the pairs of mods each sets against each other. A pair is
// forgiven if the winner depends on the other mod or the workspace ignores the
// two.
func resolveFindings(ws *workspace.Session, findings []Finding, order []string, declared map[[2]string]bool, pairs [][2]string) {
	forgiven := maps.Clone(declared)
	for _, pair := range pairs {
		forgiven[pair], forgiven[[2]string{pair[1], pair[0]}] = true, true
	}
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

// listExternal returns the ids of the open workspace's external mods.
func (hlt *Service) listExternal() map[string]bool {
	return lo.FilterSliceToMap(hlt.Workspace.ListMods(), func(mod workspace.Mod) (string, bool, bool) {
		return mod.ID, true, mod.External
	})
}

// groupReport fills in the words of the findings, folds them and groups them
// into the workspace's compatibility and each mod of the load order. A finding
// about a mod outside the order is left out, and the tally of the user's own
// mods leaves out the given external ones. The report has no changes.
func groupReport(findings []Finding, order []string, external map[string]bool) Report {
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
	ownFindings := lo.FlatMap(mods, func(group Group, _ int) []Finding {
		return lo.Ternary(external[group.Mod], nil, group.Findings)
	})
	return Report{
		Tally:         makeTally(slices.Concat(compatibility, modFindings)),
		Compatibility: makeGroup("", compatibility, nil),
		Mods:          mods,
		Own:           makeTally(ownFindings),
		CheckedAt:     time.Now(),
		Changes:       []Change{},
	}
}

// describeFinding fills in a finding's wording from its code, facts and sites.
// Its place is the first site's, such as a.txt:12. A log row with no site names
// what it is about, if it names anything.
func describeFinding(finding *Finding) {
	code := lsp.LookupCode(finding.Code)
	finding.Label, finding.Topic = code.Label, code.Topic
	finding.Message = lsp.SplitMessage(code.Template(finding.Facts), finding.Facts)
	finding.Brief, finding.Why = splitBrief(*finding), splitWhy(*finding)
	finding.Detail = lo.Ternary(finding.Facts["named"] != "", "Names "+finding.Facts["named"], "")
	if site, ok := lo.First(finding.Sites); ok {
		finding.Detail = site.Place.Rel + ":" + strconv.Itoa(site.Line)
	}
}

// splitBrief returns the line under a finding's name in a list, as parts
// separated by dots. It is empty if the finding has nothing to say there.
func splitBrief(finding Finding) []lsp.Part {
	var parts []lsp.Part
	add := func(piece ...lsp.Part) {
		if len(parts) > 0 {
			parts = append(parts, lsp.Part{Text: " · "})
		}
		parts = append(parts, piece...)
	}
	if finding.Database != "" && finding.Database != lang.File {
		add(lsp.Part{Text: finding.Database})
	}
	hasUses := len(finding.Uses) > 0
	if hasUses {
		entries := lo.Ternary(finding.Facts["count"] == "1", "Leaves out {count} entry", "Leaves out {count} entries")
		places := lo.Ternary(finding.Facts["uses"] == "1", " still used in {uses} place", " still used in {uses} places")
		add(lsp.SplitMessage(entries+places, finding.Facts)...)
	}
	if other := finding.Facts["other"]; other != "" {
		add(lsp.Part{Text: "only in "}, lsp.Part{Text: other, Style: "mod"})
	}
	// a finding's kept sites come first, so its first site is the one the game
	// keeps where it keeps one
	if site, _ := lo.First(finding.Sites); site.Label != "" && !hasUses {
		add(lsp.Part{Text: site.Label})
	}
	return parts
}

// foldFindings folds each finding into the earlier one with the same key, which
// counts it in its Count. The given function adds whatever else the earlier one
// keeps of it.
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

// findOwner returns the id of the mod a finding is about: the first mod it
// concerns, else its origin.
func findOwner(finding Finding) string { return lo.FirstOr(finding.Mods, finding.Origin) }

// makeGroup returns the group of a mod's findings, or of the workspace's
// compatibility for the mod "". Its findings and checks are sorted as Group
// says.
func makeGroup(mod string, findings, languages []Finding) Group {
	checks := lo.PartitionBy(findings, func(finding Finding) [2]string { return [2]string{finding.Level, finding.Code} })
	slices.SortStableFunc(checks, func(first, second []Finding) int {
		levels := cmp.Compare(rankLevel(first[0].Level), rankLevel(second[0].Level))
		return cmp.Or(levels, cmp.Compare(countFindings(second), countFindings(first)))
	})
	for _, check := range checks {
		slices.SortStableFunc(check, func(first, second Finding) int { return cmp.Compare(weigh(second), weigh(first)) })
	}
	return Group{
		Tally: makeTally(findings), Mod: mod, Findings: lo.Flatten(checks), Languages: lo.CoalesceSliceOrEmpty(languages),
		Checks: lo.Map(checks, func(check []Finding, _ int) Check {
			return Check{Code: check[0].Code, Label: check[0].Label, Level: check[0].Level, Count: countFindings(check)}
		}),
	}
}

// rankLevel returns where a level comes among the levels of a finding, the
// worst first. It returns -1 for a level that is not one of them.
func rankLevel(level string) int {
	return slices.IndexFunc(lsp.Levels, func(row lsp.Level) bool { return row.ID == level })
}

// countFindings returns how many findings some findings stand for, the ones
// folded into them included.
func countFindings(findings []Finding) int {
	return lo.SumBy(findings, func(finding Finding) int { return finding.Count })
}

// makeTally counts some findings by level, each as many times as its Count
// says, and words each count.
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

// weigh returns how bad a finding is among those of its check: how often the
// game's files use the names it removes, or 1 if none, times its Count.
func weigh(finding Finding) int {
	return finding.Count * max(1, lo.SumBy(finding.Uses, func(use Use) int { return use.Count }))
}

// ListCodes returns every problem code.
func (hlt *Service) ListCodes() []lsp.Code { return lsp.Codes }

// ListLevels returns the levels of a finding, the worst first.
func (hlt *Service) ListLevels() []lsp.Level { return lsp.Levels }

// ListTopics returns the topics of the checks, in the order they are listed.
func (hlt *Service) ListTopics() []lsp.Topic { return lsp.Topics }

// ListOverview counts the definitions, files and messages of each mod in the
// workspace, and lists the mods each is judged with.
func (hlt *Service) ListOverview() ([]Overview, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	positions := mods.ListOwnFiles()
	return lo.Map(*ws.Order.Load(), func(mod workspace.Mod, _ int) Overview {
		overview := Overview{Mod: mod.ID, Counts: map[string]int{}, Judged: lo.CoalesceSliceOrEmpty(ws.ListDependencies(mod.ID))}

		// a key counts once, however many languages the mod writes it in
		keys := map[uint32]bool{}
		for _, position := range positions {
			if mods.Files[position].Origin != mod.ID {
				continue
			}
			overview.Files++
			for database, count := range mods.CountDefinitions(position) {
				overview.Counts[lang.DescribeDefinition(database, true)] += count
			}
			for _, occurrence := range mods.Occurrences[position] {
				if occurrence.Role == index.RoleLocalizes {
					keys[occurrence.Name] = true
				}
			}
		}

		overview.Messages = len(keys)

		return overview
	}), nil
}

// CompareStatements returns the texts of the statements at two sites. It
// returns an error if a file cannot be read or no statement starts at a site.
func (hlt *Service) CompareStatements(left, right lsp.Site) (Compared, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return Compared{}, err
	}
	first, err := findStatementText(ws, left)
	if err != nil {
		return Compared{}, err
	}
	second, err := findStatementText(ws, right)
	return Compared{Left: first, Right: second}, err
}

// ListSnippets returns the code at each finding site, at most ten lines of it:
// the site's range, or the statement on its line if it has none. A site of one
// line also gets two lines on each side. It returns an error if a file cannot
// be read.
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
		margin := lo.Ternary(end == first, snippetContext, 0)
		top, bottom := max(first-margin, 0), min(end+margin, last)
		snippets[position] = Snippet{
			Text: tree.Src[tree.Lines.LineSpan(top).Start:tree.Lines.LineSpan(bottom).End], Line: top + 1, Last: end + 1,
		}
	}
	return snippets, nil
}

// ListReaders returns every place in script and localization that reads a
// localization key.
func (hlt *Service) ListReaders(key string) ([]lsp.Site, error) {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	return lo.Map(mods.ListUses(lang.Localization, key), func(site index.Site, _ int) lsp.Site {
		return lsp.MakeSite(ws, mods, site)
	}), nil
}

// findStatementText returns the text of the statement that starts on a finding
// site's line. It returns an error if the file cannot be read or no statement
// starts there.
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

// WriteScaffold writes the localization keys a mod lacks in a language, with
// the base text as the placeholder, and returns the files it wrote. Each key
// goes at the end of the language's matching file, which gets its header first
// if it is missing or empty. It returns an error if the mod or the language is
// not valid, or a file cannot be read or written.
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
		text, err := files.LoadText(target)
		if err == nil && strings.TrimSpace(text.Text) == "" {
			if err = hlt.Workspace.CreateLocalization(target); err == nil {
				text, err = files.LoadText(target)
			}
		}
		if err == nil {
			text.Text += loc.EncodeAddition(text.Text, scaffolds[target])
			err = hlt.Workspace.Write(target, text)
		}
		if err != nil {
			return paths[:position], err
		}
	}
	// a file the rescan cannot read waits for the watcher
	_ = ws.Rescan(lo.Map(paths, func(target string, _ int) workspace.FileChange { return workspace.FileChange{Path: target} }))
	return paths, nil
}

// ListScaffolds returns the base-language messages of a mod whose key has no
// message in a language, by the file of that language they belong in.
func ListScaffolds(mods *index.Index, mod workspace.Mod, language string) map[string][]loc.Message {
	messages := lo.Filter(listMessages(mods), func(site index.Site, _ int) bool { return mods.File(site).Origin == mod.ID })
	scaffolds := map[string][]loc.Message{}
	for _, site := range listUntranslated(mods, lo.GroupBy(messages, mods.Language), language) {
		target := files.Join(mod.Dir, loc.ToLanguageRel(mods.File(site).Rel, language))
		scaffolds[target] = append(scaffolds[target], loc.Message{Key: mods.ToReference(site).Name, Text: mods.Text(site)})
	}
	return scaffolds
}

// listDefinitions returns the mods' definitions in known databases, without
// aliases.
func listDefinitions(ws *workspace.Session, mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool {
		_, known := ws.Schema.Databases[mods.ToReference(site).Database]
		return mods.Occurrence(site).Own && known
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

// hasWinner reports whether one copy of a name wins: its database does not keep
// every copy, and a source says which copy the game keeps. It is false for a
// database the schema lacks.
func hasWinner(ws *workspace.Session, reference lang.Reference, winner index.Winner) bool {
	database := ws.Schema.Databases[reference.Database]
	return database != nil && !database.Precedence.KeepsEvery() && winner.Reason != lang.ReasonUnsettled
}

// listLoaded returns the sites that lie in files the game reads, in the order
// given.
func listLoaded(mods *index.Index, sites []index.Site) []index.Site {
	return lo.Filter(sites, func(site index.Site, _ int) bool { return mods.IsLoaded(site.File) })
}

// makeFinding returns a finding of a problem code. Its kept sites come first.
// Its origin is the last kept site's, or the first site's if none is kept.
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

// listMods returns those of the given sources that are mods, each once, in load
// order. The game is not a mod.
func listMods(mods *index.Index, origins []string) []string {
	return lo.FilterMap(mods.Roots, func(root files.Root, _ int) (string, bool) {
		return root.Origin, root.Origin != files.OriginVanilla && slices.Contains(origins, root.Origin)
	})
}

// listRemoved returns the names that the sites the game does not keep define
// and the kept sites do not, each once.
func listRemoved(sites []lsp.Site) []string {
	kept, replaced := lo.FilterReject(sites, func(site lsp.Site, _ int) bool { return site.Kept })
	defines := func(site lsp.Site, _ int) []string { return site.Defines }
	removed, _ := lo.Difference(lo.FlatMap(replaced, defines), lo.FlatMap(kept, defines))
	return lo.Uniq(removed)
}

// findLineStatement returns the statement that starts on a line of a tree,
// counted from one, or false if none does.
func findLineStatement(tree *jomini.Tree, line int) (int32, bool) {
	span := tree.Lines.LineSpan(min(max(line-1, 0), tree.Lines.CountLines()-1))
	text := tree.Src[span.Start:span.End]
	return tree.LookupStatement(span.Start + len(text) - len(strings.TrimLeft(text, " \t")))
}

// isMod reports whether a site lies in a mod's file.
func isMod(mods *index.Index, site index.Site) bool {
	return mods.File(site).Origin != files.OriginVanilla
}

// listShippedPaths returns the paths the mods ship in the folders the game
// reads, each once, sorted without regard to case. Files that differ only in
// their stage folder share a path. A file the game never reads, which hides no
// other, is left out.
func listShippedPaths(mods *index.Index) []string {
	var paths []string
	groups := map[string]bool{} // by the key of the first file at a path
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		loaded, copies, ok := mods.FindLoaded(file.Rel)
		if ok {
			copies = append(copies, loaded)
		}
		if !mods.IsGameFolder(position) ||
			!slices.ContainsFunc(copies, func(other files.File) bool { return other.Key == file.Key }) || groups[copies[0].Key] {
			continue
		}
		groups[copies[0].Key] = true
		paths = append(paths, file.Rel)
	}
	slices.SortFunc(paths, func(first, second string) int { return cmp.Compare(strings.ToLower(first), strings.ToLower(second)) })
	return paths
}

// makeFileSites returns the finding sites of the files at a path, in load
// order. The file the game reads comes last, kept for the reason path where it
// hides others.
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
		sites[len(sites)-1].Reason, sites[len(sites)-1].Label = lang.ReasonPath, lang.Reasons[lang.ReasonPath].Label
	}
	return sites
}

// makeFileSite returns the finding site of a whole file, at its first line. It
// is kept if the game loads the file.
func makeFileSite(ws *workspace.Session, mods *index.Index, position int32) lsp.Site {
	return lsp.Site{
		Place: ws.Locate(mods, mods.Files[position].Path), Line: 1, Range: jomini.ToLineRange(1),
		Kept: mods.IsLoaded(position),
		Defines: lo.Map(listFileDefinitions(mods, position), func(occurrence index.Occurrence, _ int) string {
			return mods.Names.Texts[occurrence.Name]
		}),
	}
}

// listFileDefinitions returns the occurrences that define the names of a file,
// in the order written. A name the file saves while the game runs, such as a
// saved scope, is none of them.
func listFileDefinitions(mods *index.Index, position int32) []index.Occurrence {
	return lo.Filter(mods.Occurrences[position], func(occurrence index.Occurrence, _ int) bool { return occurrence.Own })
}

// listUnused returns a finding for each mod whose copies of a name the game
// never uses because it keeps its own. Each points at the game's copies and
// that mod's, with the game as the winner and the mod as the beaten one.
func listUnused(ws *workspace.Session, mods *index.Index, sites []index.Site, winner index.Winner) []Finding {
	reference := mods.ToReference(sites[0])
	byMod := lo.GroupBy(lo.Filter(sites, func(site index.Site, _ int) bool { return isMod(mods, site) }),
		func(site index.Site) string { return mods.File(site).Origin })
	var findings []Finding
	for _, mod := range listMods(mods, slices.Collect(maps.Keys(byMod))) {
		if lo.SomeBy(byMod[mod], func(site index.Site) bool { return mods.ResolveSite(winner, site).Kept }) {
			continue
		}
		facts := lang.DescribeReference(reference)
		facts["winner"], facts["beaten"] = ws.DescribeMod(mods.File(winner.Site).Origin), ws.DescribeMod(mod)
		own := lo.Filter(sites, func(site index.Site, _ int) bool { return !isMod(mods, site) || mods.File(site).Origin == mod })
		findings = append(findings, makeFinding(mods, "unused-copy", facts, lsp.MakeSites(ws, mods, own, &winner)))
	}
	return findings
}

// describeRemoved fills in the finding for a mod file that leaves out entries
// in use: its Uses and the facts its message counts. The "definitions" fact
// names the kind of definition in the plural, such as "scripted triggers", or
// is "entries" for several kinds.
func describeRemoved(ws *workspace.Session, mods *index.Index, finding *Finding, uses []Use) {
	original, _ := lo.Find(finding.Sites, func(site lsp.Site) bool { return site.Place.Origin == files.OriginVanilla })
	kept, _ := lo.Find(finding.Sites, func(site lsp.Site) bool { return site.Kept })
	position, _ := mods.FindFile(original.Place.Path)
	databases := lo.Uniq(lo.Map(listFileDefinitions(mods, position), func(occurrence index.Occurrence, _ int) string {
		return mods.Databases.Texts[occurrence.Database]
	}))
	finding.Uses = uses
	finding.Facts["mod"] = ws.DescribeMod(finding.Origin)
	finding.Facts["total"] = strconv.Itoa(len(lo.Uniq(original.Defines)))
	finding.Facts["definitions"] = "entries"
	if len(databases) == 1 {
		finding.Facts["definitions"] = lang.DescribeDefinition(databases[0], true)
	}
	finding.Facts["removed"] = strconv.Itoa(len(listRemoved([]lsp.Site{kept, original})))
	finding.Facts["count"] = strconv.Itoa(len(uses))
	finding.Facts["uses"] = strconv.Itoa(lo.SumBy(uses, func(use Use) int { return use.Count }))
}

// listRemovedUses returns the names the game's copy of a file defines that no
// file the game reads defines, each with the uses the game's files make of it,
// the most used first, and the sources whose files make those uses. A name
// nothing uses is left out, and so is one the copy defines for itself alone or
// saves while the game runs.
func listRemovedUses(ws *workspace.Session, mods *index.Index, path string) ([]Use, []string) {
	position, ok := mods.FindFile(path)
	if !ok {
		return nil, nil
	}
	var uses []Use
	var users []string
	seen := map[lang.Reference]bool{}
	for number, occurrence := range mods.Occurrences[position] {
		reference := mods.ToReference(index.Site{File: position, Occurrence: int32(number)})
		if !occurrence.Own || occurrence.Local || seen[reference] || len(listLoaded(mods, mods.Resolve(reference))) > 0 {
			continue
		}
		seen[reference] = true
		loaded := listLoaded(mods, mods.ListUses(reference.Database, reference.Name))
		if len(loaded) > 0 {
			uses = append(uses, Use{Name: reference.Name, Count: len(loaded), First: lsp.MakeSite(ws, mods, loaded[0])})
		}
		for _, site := range loaded {
			users = append(users, mods.File(site).Origin)
		}
	}
	slices.SortStableFunc(uses, func(first, second Use) int { return cmp.Compare(second.Count, first.Count) })
	return uses, lo.Uniq(users)
}

// listDeclared returns the pairs of mods where the first depends on the second,
// directly or down the chain. A dependency the workspace lacks is left out.
func listDeclared(ws *workspace.Session) map[[2]string]bool {
	declared := map[[2]string]bool{}
	for _, mod := range *ws.Order.Load() {
		for _, dependency := range ws.ListDependencies(mod.ID) {
			declared[[2]string{mod.ID, dependency}] = true
		}
	}
	return declared
}

// listDescriptors lists the mods' missing dependencies, each named by the title
// its relationship gives where it gives one. A mod without a readable
// descriptor has none.
func listDescriptors(ws *workspace.Session, mods *index.Index) []Finding {
	var findings []Finding
	order := *ws.Order.Load()
	for _, mod := range order {
		descriptor := mod.Descriptor
		if descriptor.Path == "" {
			continue
		}
		sites := []lsp.Site{{Place: ws.Locate(mods, descriptor.Path), Line: 1, Range: jomini.ToLineRange(1)}}
		for _, dependency := range descriptor.Dependencies {
			if _, ok := workspace.FindMod(order, dependency); !ok {
				facts := lang.DescribeReference(lang.Reference{Name: cmp.Or(dependency.Title, dependency.ID)})
				facts["mod"] = ws.DescribeMod(mod.ID)
				findings = append(findings, makeFinding(mods, "missing-dependency", facts, sites))
			}
		}
	}
	return findings
}

// listUnmounted lists the top folders of the mods that lie outside the stage
// folders of a game that keeps its files under stages, one finding for each,
// pointing at its first file. A dot folder and a file at a mod's root are left
// out.
func listUnmounted(ws *workspace.Session, mods *index.Index) []Finding {
	type folder struct{ origin, name string }
	var order []folder
	positions := map[folder][]int32{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		name, _, nested := strings.Cut(file.Rel, "/")
		if !nested || strings.HasPrefix(name, ".") || !mods.IsUnmounted(position) {
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
		site := lsp.Site{Place: ws.Locate(mods, mods.Files[positions[key][0]].Path), Line: 1, Range: jomini.ToLineRange(1)}
		return makeFinding(mods, "unmounted-folder", facts, []lsp.Site{site})
	})
}

// listMessages returns the messages in the mods' files, in every language.
func listMessages(mods *index.Index) []index.Site {
	return listModSites(mods, func(site index.Site) bool { return mods.Occurrence(site).Role == index.RoleLocalizes })
}

// listCoverage lists how many of each mod's base-language keys each of its
// other languages has. A mod with no base-language keys has none.
func listCoverage(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	var findings []Finding
	byMod := lo.GroupBy(messages, func(site index.Site) string { return mods.File(site).Origin })
	for _, mod := range *ws.Order.Load() {
		byLanguage := lo.GroupBy(byMod[mod.ID], mods.Language)
		base := listKeys(mods, byLanguage[loc.BaseLanguage])
		if len(base) == 0 {
			continue
		}
		for _, language := range slices.Sorted(maps.Keys(byLanguage)) {
			if language == loc.BaseLanguage {
				continue
			}
			facts := lang.DescribeReference(lang.Reference{Database: lang.Localization, Name: language})
			facts["mod"] = ws.DescribeMod(mod.ID)
			facts["have"] = strconv.Itoa(len(base) - len(listUntranslated(mods, byLanguage, language)))
			facts["of"] = strconv.Itoa(len(base))
			positions := lo.Uniq(lo.Map(byLanguage[language], func(site index.Site, _ int) int32 { return site.File }))
			sites := lo.Map(positions, func(position int32, _ int) lsp.Site { return makeFileSite(ws, mods, position) })
			findings = append(findings, makeFinding(mods, coverageCode, facts, sites))
		}
	}
	return findings
}

// listUntranslated returns the base-language messages whose key has no message
// in a language, one for each key. The messages are one mod's, by their
// language.
func listUntranslated(mods *index.Index, byLanguage map[string][]index.Site, language string) []index.Site {
	translated := lo.Keyify(listKeys(mods, byLanguage[language]))
	untranslated := lo.Reject(byLanguage[loc.BaseLanguage], func(site index.Site, _ int) bool {
		_, ok := translated[mods.ToReference(site).Name]
		return ok
	})
	return lo.UniqBy(untranslated, func(site index.Site) string { return mods.ToReference(site).Name })
}

// listKeys returns each message's key once.
func listKeys(mods *index.Index, messages []index.Site) []string {
	return lo.Uniq(lo.Map(messages, func(site index.Site, _ int) string { return mods.ToReference(site).Name }))
}

// listDiagnostics lists the editor's problems in every mod file and in each
// mod's descriptor, one finding each.
func listDiagnostics(ws *workspace.Session, mods *index.Index) []Finding {
	paths := lo.FilterMap(mods.ListOwnFiles(), func(position int32, _ int) (string, bool) {
		return mods.Files[position].Path, mods.Files[position].Format != files.FormatDescriptor
	})
	for _, mod := range *ws.Order.Load() {
		if mod.Descriptor.Path != "" {
			paths = append(paths, mod.Descriptor.Path)
		}
	}
	diagnostics := lsp.DiagnoseFiles(ws, mods, paths)
	var findings []Finding
	for position, target := range paths {
		place := ws.Locate(mods, target)
		for _, diagnostic := range diagnostics[position] {
			site := lsp.Site{Place: place, Line: diagnostic.Range.StartLine, Range: diagnostic.Range}
			// the facts are the kept problem's own, which a finding may add to
			finding := makeFinding(mods, diagnostic.Code, maps.Clone(diagnostic.Facts), []lsp.Site{site})
			finding.Defined, finding.Diagnosed = diagnostic.Defined, true
			findings = append(findings, finding)
		}
	}
	return findings
}

// setStop ends the running watch, if any. It keeps a function that ends the
// next watch, or nil for none.
func (hlt *Service) setStop(stop func()) {
	hlt.mu.Lock()
	running := hlt.stop
	hlt.stop = stop
	hlt.mu.Unlock()
	if running != nil {
		running()
	}
}
