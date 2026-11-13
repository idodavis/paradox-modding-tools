// report.go builds the report of one load order: it runs the lists of
// findings, grades them, then words, folds, groups and counts them.

package health

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"weak"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// build is one load order's report, built or being built, shared by every
// caller that asks for it.
type build struct {
	stamp stamp

	// done is closed once the report is built.
	done chan struct{}

	report Report
	err    error
}

// stamp is what a report is built from. A kept report is out of date once
// the workspace's stamp is another. Its weak pointers keep no session, index
// or load order alive.
type stamp struct {
	session weak.Pointer[workspace.Session]
	mods    weak.Pointer[index.Index]
	order   weak.Pointer[[]workspace.Mod]

	// edits counts the editor's changes to its buffers.
	edits int64

	// config is config.json's revision.
	config int
}

// coverageCode is the finding that says how far a language covers a mod.
const coverageCode = "coverage"

// BuildReport returns the report of the mods' problems in a load order. An
// empty order is the workspace's own; another is a preview, which is not
// saved. A report is built once and kept until the workspace, its files, the
// editor's unsaved text or config.json change. It returns an error if no
// workspace is open or the order does not name each mod once.
func (s *Service) BuildReport(order []string) (Report, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Report{}, err
	}

	if len(order) == 0 {
		order = listIDs(*ws.Order.Load())
	}

	kept, isNew := s.findBuild(ws, order)
	if isNew {
		kept.report, kept.err = s.makeReport(ws, order)
		close(kept.done)
	}

	<-kept.done

	return kept.report, kept.err
}

func listIDs(mods []workspace.Mod) []string {
	ids := make([]string, len(mods))
	for position, mod := range mods {
		ids[position] = mod.ID
	}

	return ids
}

// findBuild returns the build of a load order's report and whether the
// caller is to build it; a caller that finds one being built waits for it. A
// build of another stamp, or one that failed, is replaced. Only the
// workspace's own order and the last preview are kept.
func (s *Service) findBuild(ws *workspace.Session, order []string) (*build, bool) {
	now := stamp{
		session: weak.Make(ws),
		mods:    weak.Make(ws.Mods.Load()),
		order:   weak.Make(ws.Order.Load()),
		edits:   ws.Edits.Load(),
		config:  s.Workspace.Config.Revision(),
	}
	key, own := strings.Join(order, " "), strings.Join(listIDs(*ws.Order.Load()), " ")

	s.mu.Lock()
	defer s.mu.Unlock()

	if kept, ok := s.reports[key]; ok && kept.stamp == now && !kept.failed() {
		return kept, false
	}

	next := &build{stamp: now, done: make(chan struct{})}
	reports := map[string]*build{key: next}
	for other, kept := range s.reports {
		if other != key && kept.stamp == now && (other == own || key == own) {
			reports[other] = kept
		}
	}

	s.reports = reports

	return next, true
}

// failed reports whether the build is done and ended in an error.
func (b *build) failed() bool {
	select {
	case <-b.done:
		return b.err != nil
	default:
		return false
	}
}

// makeReport builds the report of a load order. Any order but the
// workspace's own is a preview, which reads no file again. It returns an
// error if the order does not name each mod once.
func (s *Service) makeReport(ws *workspace.Session, order []string) (Report, error) {
	base := ws.Mods.Load()
	mods, ids := base, listIDs(*ws.Order.Load())
	isPreview := !slices.Equal(order, ids)
	if isPreview {
		previewed, err := ws.PreviewOrder(order)
		if err != nil {
			return Report{}, err
		}

		mods, ids = previewed, order
	}

	var record app.Workspace
	for _, candidate := range s.Workspace.Config.Config().Workspaces {
		if candidate.ID == ws.ID {
			record = candidate
		}
	}

	// The compatibility checks see only the mods the workspace checks.
	checked := listChecked(ids, record)
	compatible := mods.Narrow(checked)
	definitions, messages := listOwnSites(mods)
	shared := slices.DeleteFunc(slices.Clone(messages), func(site index.Site) bool { return !compatible.Covers(site.File) })
	kept, defined := findKept(ws, compatible, definitions)
	paths, declared := listShippedPaths(compatible), listDeclared(ws)

	lists := []func() []Finding{
		func() (findings []Finding) {
			for _, reference := range defined {
				findings = append(findings, listDefinitionFindings(ws, compatible, reference, kept[reference])...)
			}

			return findings
		},
		func() (findings []Finding) {
			for _, path := range paths {
				findings = append(findings, listPathFindings(ws, compatible, path)...)
			}

			return findings
		},
		func() []Finding { return listSharedMessages(ws, compatible, shared) },
		func() []Finding { return listDescriptors(ws, mods) },
		func() []Finding { return listUnmounted(ws, mods) },
		func() []Finding { return listDiagnostics(ws, base, mods) },
		func() []Finding { return listCoverage(ws, mods, messages) },
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
	report := groupReport(slices.Concat(findings, pairFindings), ids, s.listExternal())
	if isPreview {
		previous := base.Narrow(checked)
		report.Changes = append(listDefinitionChanges(ws, previous, compatible, kept),
			listPathChanges(previous, compatible, paths)...)
		slices.SortFunc(report.Changes, func(first, second Change) int {
			return cmp.Or(cmp.Compare(first.Database, second.Database), cmp.Compare(first.Name, second.Name))
		})
	}

	return report, nil
}

// listOwnSites returns the mods' own definitions, an alias not counted, and
// their messages in every language, each in load order.
func listOwnSites(mods *index.Index) (definitions, messages []index.Site) {
	for _, file := range mods.ListOwnFiles() {
		for position, occurrence := range mods.Occurrences[file] {
			site := index.Site{File: file, Occurrence: int32(position)}
			if occurrence.Own {
				definitions = append(definitions, site)
			}

			if occurrence.Role == index.RoleLocalizes {
				messages = append(messages, site)
			}
		}
	}

	return definitions, messages
}

// findKept returns the copy the game keeps of each name the definitions
// define, and those names in the order first defined. A definition outside
// the index, and a name the game keeps no copy of, are left out.
func findKept(ws *workspace.Session, mods *index.Index, definitions []index.Site) (map[lang.Reference]index.Winner, []lang.Reference) {
	covered := slices.DeleteFunc(slices.Clone(definitions), func(site index.Site) bool { return !mods.Covers(site.File) })
	kept, defined := map[lang.Reference]index.Winner{}, []lang.Reference{}
	for _, reference := range listUnique(covered, mods.ToReference) {
		if winner, ok := mods.FindKept(ws.Schema, reference); ok {
			kept[reference] = winner
			defined = append(defined, reference)
		}
	}

	return kept, defined
}

// listDeclared returns the pairs of mods where the first depends on the
// second, directly or not. A dependency the workspace lacks is left out.
func listDeclared(ws *workspace.Session) map[[2]string]bool {
	declared := map[[2]string]bool{}
	for _, mod := range *ws.Order.Load() {
		for _, dependency := range ws.ListDependencies(mod.ID) {
			declared[[2]string{mod.ID, dependency}] = true
		}
	}

	return declared
}

// listDescriptors lists the mods' missing dependencies, each named by the
// title its relationship gives, if any.
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

// listUnmounted lists each top folder of a mod that lies outside the stage
// folders of a game that keeps its files under stages, pointing at its first
// file. Dot folders and files at a mod's root are left out.
func listUnmounted(ws *workspace.Session, mods *index.Index) []Finding {
	var unmounted []int32
	for _, position := range mods.ListOwnFiles() {
		name, _, nested := strings.Cut(mods.Files[position].Rel, "/")
		if nested && !strings.HasPrefix(name, ".") && mods.IsUnmounted(position) {
			unmounted = append(unmounted, position)
		}
	}

	folders := partition(unmounted, func(position int32) [2]string {
		name, _, _ := strings.Cut(mods.Files[position].Rel, "/")

		return [2]string{mods.Files[position].Origin, name}
	})
	findings := make([]Finding, len(folders))
	for position, folder := range folders {
		first := mods.Files[folder[0]]
		name, _, _ := strings.Cut(first.Rel, "/")
		facts := map[string]string{"name": name, "count": strconv.Itoa(len(folder))}
		site := lsp.Site{Place: ws.Locate(mods, first.Path), Line: 1, Range: jomini.ToLineRange(1)}
		findings[position] = makeFinding(mods, "unmounted-folder", facts, []lsp.Site{site})
	}

	return findings
}

// listDiagnostics lists the editor's problems in every mod file and
// descriptor. They are the ones kept for the workspace's own index, so a
// preview judges no file again.
func listDiagnostics(ws *workspace.Session, base, mods *index.Index) []Finding {
	var paths []string
	for _, position := range mods.ListOwnFiles() {
		if mods.Files[position].Format != files.FormatDescriptor {
			paths = append(paths, mods.Files[position].Path)
		}
	}

	for _, mod := range *ws.Order.Load() {
		if mod.Descriptor.Path != "" {
			paths = append(paths, mod.Descriptor.Path)
		}
	}

	diagnostics := lsp.DiagnoseFiles(ws, base, paths)
	var findings []Finding
	for position, target := range paths {
		place := ws.Locate(mods, target)
		for _, diagnostic := range diagnostics[position] {
			site := lsp.Site{Place: place, Line: diagnostic.Range.StartLine, Range: diagnostic.Range}

			// The facts are cloned, since a finding adds to them.
			finding := makeFinding(mods, diagnostic.Code, maps.Clone(diagnostic.Facts), []lsp.Site{site})
			finding.Defined, finding.Diagnosed = diagnostic.Defined, true
			findings = append(findings, finding)
		}
	}

	return findings
}

// listCoverage lists how many of each mod's base-language keys each of its
// other languages has. A mod with no base-language key has none.
func listCoverage(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	var findings []Finding
	byMod := groupMessages(mods, messages)
	for _, mod := range *ws.Order.Load() {
		byLanguage := byMod[mod.ID]
		keys := len(listUnique(byLanguage[loc.BaseLanguage], func(site index.Site) string { return mods.ToReference(site).Name }))
		if keys == 0 {
			continue
		}

		for _, language := range slices.Sorted(maps.Keys(byLanguage)) {
			if language == loc.BaseLanguage {
				continue
			}

			facts := lang.DescribeReference(lang.Reference{Database: lang.Localization, Name: language})
			facts["mod"] = ws.DescribeMod(mod.ID)
			facts["have"] = strconv.Itoa(keys - len(listUntranslated(mods, byLanguage, language)))
			facts["of"] = strconv.Itoa(keys)

			var sites []lsp.Site
			for _, file := range listUnique(byLanguage[language], func(site index.Site) int32 { return site.File }) {
				sites = append(sites, makeFileSite(ws, mods, file))
			}

			findings = append(findings, makeFinding(mods, coverageCode, facts, sites))
		}
	}

	return findings
}

// dropRepeatedTies drops a file's priority tie for a name that an undecided
// contest reports, so that one tie between several mods shows once.
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

// resolveFindings grades the contests and shared localization keys, and
// lists the pairs of mods each sets against each other. A pair is forgiven
// where the winner depends on the other mod or the workspace ignores the
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
			winner, beaten := findWinner(*finding), listBeaten(*finding)
			resolveContest(ws, finding, winner, beaten, forgiven)
			resolveOrder(finding, winner, beaten, order, declared)

		case sharedMessageCode, undecidedCode:
			resolveForgiven(finding, forgiven)
			finding.Pairs = listEveryPair(finding.Mods)
		}
	}
}

// listExternal returns the ids of the open workspace's external mods.
func (s *Service) listExternal() map[string]bool {
	external := map[string]bool{}
	for _, mod := range s.Workspace.ListMods() {
		if mod.External {
			external[mod.ID] = true
		}
	}

	return external
}

// groupReport words the findings, folds the repeats and groups them into the
// compatibility and each mod of the load order. A finding about a mod
// outside the order is dropped, and the own tally leaves out the given
// external mods.
func groupReport(findings []Finding, order []string, external map[string]bool) Report {
	for position := range findings {
		describeFinding(&findings[position])
	}

	folded := foldFindings(findings, func(finding Finding) [4]string {
		return [4]string{finding.Code, findOwner(finding), finding.Name, lsp.JoinParts(finding.Message)}
	}, func(first *Finding, repeat Finding) { first.Repeats = append(first.Repeats, repeat.Sites...) })

	var compatibility []Finding
	problems, languages := map[string][]Finding{}, map[string][]Finding{}
	for _, finding := range folded {
		owner := findOwner(finding)
		switch {
		case finding.Question == lsp.QuestionCompatibility:
			compatibility = append(compatibility, finding)
		case finding.Code == coverageCode:
			languages[owner] = append(languages[owner], finding)
		default:
			problems[owner] = append(problems[owner], finding)
		}
	}

	mods := make([]Group, len(order))
	every, own := slices.Clone(compatibility), []Finding{}
	for position, id := range order {
		mods[position] = makeGroup(id, problems[id], languages[id])
		every = append(every, mods[position].Findings...)
		if !external[id] {
			own = append(own, mods[position].Findings...)
		}
	}

	return Report{
		Tally:         makeTally(every),
		Compatibility: makeGroup("", compatibility, nil),
		Mods:          mods,
		Own:           makeTally(own),
		CheckedAt:     time.Now(),
		Changes:       []Change{},
	}
}

// describeFinding fills in a finding's wording from its code, facts and
// sites.
func describeFinding(finding *Finding) {
	code := lsp.LookupCode(finding.Code)
	finding.Label, finding.Topic = code.Label, code.Topic
	finding.Message = lsp.SplitMessage(code.Template(finding.Facts), finding.Facts)
	finding.Brief, finding.Why = splitBrief(*finding), splitWhy(*finding)

	switch {
	case len(finding.Sites) > 0:
		finding.Detail = finding.Sites[0].Place.Rel + ":" + strconv.Itoa(finding.Sites[0].Line)
	case finding.Facts["named"] != "":
		finding.Detail = "Names " + finding.Facts["named"]
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
		entries, places := "Leaves out {count} entries", " still used in {uses} places"
		if finding.Facts["count"] == "1" {
			entries = "Leaves out {count} entry"
		}

		if finding.Facts["uses"] == "1" {
			places = " still used in {uses} place"
		}

		add(lsp.SplitMessage(entries+places, finding.Facts)...)
	}

	if other := finding.Facts["other"]; other != "" {
		add(lsp.Part{Text: "only in "}, lsp.Part{Text: other, Style: "mod"})
	}

	// Kept sites come first, so the first site is the kept one where any is.
	if len(finding.Sites) > 0 && finding.Sites[0].Label != "" && !hasUses {
		add(lsp.Part{Text: finding.Sites[0].Label})
	}

	return parts
}

// foldFindings folds each finding into the first one with the same key,
// which adds its Count and whatever else the given merge keeps of it.
func foldFindings[Key comparable](
	findings []Finding, key func(Finding) Key, merge func(first *Finding, repeat Finding),
) []Finding {
	folded, positions := []Finding{}, map[Key]int{}
	for _, finding := range findings {
		position, repeated := positions[key(finding)]
		if !repeated {
			positions[key(finding)] = len(folded)
			folded = append(folded, finding)

			continue
		}

		folded[position].Count += finding.Count
		merge(&folded[position], finding)
	}

	return folded
}

// findOwner returns the mod a finding is about: the first mod it concerns,
// else its origin.
func findOwner(finding Finding) string {
	if len(finding.Mods) > 0 {
		return finding.Mods[0]
	}

	return finding.Origin
}

// makeGroup returns the group of a mod's findings, or of the compatibility
// for the mod "", sorted as Group says.
func makeGroup(mod string, findings, languages []Finding) Group {
	checks := partition(findings, func(finding Finding) [2]string { return [2]string{finding.Level, finding.Code} })
	slices.SortStableFunc(checks, func(first, second []Finding) int {
		levels := cmp.Compare(rankLevel(first[0].Level), rankLevel(second[0].Level))

		return cmp.Or(levels, cmp.Compare(countFindings(second), countFindings(first)))
	})

	group := Group{
		Tally: makeTally(findings), Mod: mod, Findings: []Finding{}, Languages: append([]Finding{}, languages...),
		Checks: make([]Check, len(checks)),
	}
	for position, check := range checks {
		slices.SortStableFunc(check, func(first, second Finding) int { return cmp.Compare(weigh(second), weigh(first)) })
		group.Findings = append(group.Findings, check...)
		group.Checks[position] = Check{
			Code: check[0].Code, Label: check[0].Label, Level: check[0].Level, Count: countFindings(check),
		}
	}

	return group
}

// partition groups the items that share a key, in the order each group's
// first item comes.
func partition[Item any, Key comparable](items []Item, key func(Item) Key) [][]Item {
	var groups [][]Item
	positions := map[Key]int{}
	for _, item := range items {
		position, grouped := positions[key(item)]
		if !grouped {
			position = len(groups)
			positions[key(item)] = position
			groups = append(groups, nil)
		}

		groups[position] = append(groups[position], item)
	}

	return groups
}

// rankLevel returns where a level comes among the levels, the worst first,
// or -1 for one that is not a level.
func rankLevel(level string) int {
	return slices.IndexFunc(lsp.Levels, func(row lsp.Level) bool { return row.ID == level })
}

// countFindings returns how many findings some findings stand for, the
// folded ones included.
func countFindings(findings []Finding) int {
	count := 0
	for _, finding := range findings {
		count += finding.Count
	}

	return count
}

// makeTally counts findings by level, each as many times as its Count, and
// words each count.
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

// weigh returns how bad a finding is within its check: how often the game's
// files use the names it removes, at least 1, times its Count.
func weigh(finding Finding) int {
	return finding.Count * max(1, countUses(finding.Uses))
}

func countUses(uses []Use) int {
	count := 0
	for _, use := range uses {
		count += use.Count
	}

	return count
}

// makeFinding returns a finding of a problem code, its kept sites first. Its
// origin is the last kept site's, else the first site's.
func makeFinding(mods *index.Index, id string, facts map[string]string, sites []lsp.Site) Finding {
	kept, replaced := splitSites(sites, isKept)
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
		Mods: listMods(mods, listParties(sites)), Facts: facts, Sites: sites, Count: 1,
	}
}

// splitSites returns the sites that pass a test and the rest, each in the
// order given.
func splitSites(sites []lsp.Site, test func(lsp.Site) bool) (passed, rest []lsp.Site) {
	for _, site := range sites {
		if test(site) {
			passed = append(passed, site)
		} else {
			rest = append(rest, site)
		}
	}

	return passed, rest
}

func isKept(site lsp.Site) bool { return site.Kept }

// listParties returns the mods and the game the sites lie in, each once, in
// the order of the sites.
func listParties(sites []lsp.Site) []string {
	return listUnique(sites, func(site lsp.Site) string { return site.Place.Origin })
}

// listMods returns those of the origins that are mods, in load order.
func listMods(mods *index.Index, origins []string) []string {
	ids := []string{}
	for _, root := range mods.Roots {
		if root.Origin != files.OriginVanilla && slices.Contains(origins, root.Origin) {
			ids = append(ids, root.Origin)
		}
	}

	return ids
}

// listOrigins returns the mods and the game the sites lie in, each once, in
// the order of the sites.
func listOrigins(mods *index.Index, sites []index.Site) []string {
	return listUnique(sites, func(site index.Site) string { return mods.File(site).Origin })
}

// listUnique returns the key of each item, each key once, in the order
// first met.
func listUnique[Item any, Key comparable](items []Item, key func(Item) Key) []Key {
	var keys []Key
	seen := map[Key]bool{}
	for _, item := range items {
		if itemKey := key(item); !seen[itemKey] {
			seen[itemKey] = true
			keys = append(keys, itemKey)
		}
	}

	return keys
}

func isMod(mods *index.Index, site index.Site) bool {
	return mods.File(site).Origin != files.OriginVanilla
}

// listLoaded returns the sites in files the game reads.
func listLoaded(mods *index.Index, sites []index.Site) []index.Site {
	return slices.DeleteFunc(slices.Clone(sites), func(site index.Site) bool { return !mods.IsLoaded(site.File) })
}

// makeFileSite returns the finding site of a whole file, kept if the game
// loads it, with the names the file defines for itself.
func makeFileSite(ws *workspace.Session, mods *index.Index, position int32) lsp.Site {
	defines := []string{}
	for _, occurrence := range mods.Occurrences[position] {
		if occurrence.Own {
			defines = append(defines, mods.Names.Texts[occurrence.Name])
		}
	}

	return lsp.Site{
		Place: ws.Locate(mods, mods.Files[position].Path), Line: 1, Range: jomini.ToLineRange(1),
		Kept: mods.IsLoaded(position), Defines: defines,
	}
}
