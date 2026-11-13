// contests.go finds the names and files that two or more mods, or a mod and
// the game, both write, and what another load order changes.

package health

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The codes of the findings where the mods and the game write the same thing.
const (
	// contestCode is two or more mods defining one name, of which the game
	// keeps one copy.
	contestCode = "contest"

	// fileContestCode is two or more mods shipping one file.
	fileContestCode = "file-contest"

	// undecidedCode is copies of one name whose kept copy no source names.
	undecidedCode = "undecided-contest"

	// sharedMessageCode is two or more mods defining one localization key in
	// one language.
	sharedMessageCode = "shared-message"
)

// listDefinitionFindings lists the findings about the loaded copies of one
// name: contests, undecided contests, overrides and unused copies. A
// database that keeps every copy has no contest or override.
func listDefinitionFindings(ws *workspace.Session, mods *index.Index, reference lang.Reference, winner index.Winner) []Finding {
	sites := listLoaded(mods, mods.Resolve(reference))
	precedence := ws.Schema.Databases[reference.Database].Precedence
	findings := listBlockContests(ws, mods, sites, precedence.Contested)
	if winner.Reason == lang.ReasonUnsettled {
		if finding, ok := makeUndecided(ws, mods, sites, describeCause(winner, precedence)); ok {
			findings = append(findings, finding)
		}

		return findings
	}

	if precedence.KeepsEvery() {
		return findings
	}

	origins := listOrigins(mods, sites)
	modCount := len(origins)
	hasGame := slices.Contains(origins, files.OriginVanilla)
	if hasGame {
		modCount--
	}

	copies := lsp.MakeSites(ws, mods, sites, &winner)
	if modCount >= 2 && isMod(mods, winner.Site) {
		contest := makeFinding(mods, contestCode, lang.DescribeReference(reference), copies)
		if len(listBeaten(contest)) > 0 {
			contest.Comparable = true
			findings = append(findings, contest)
		}
	}

	if !hasGame || modCount == 0 {
		return findings
	}

	kept := slices.IndexFunc(sites, func(site index.Site) bool {
		return isMod(mods, site) && mods.ResolveSite(winner, site).Kept
	})
	if kept >= 0 {
		facts := lang.DescribeReference(reference)
		facts["kept"] = describeKept(mods, sites[kept])
		findings = append(findings, makeFinding(mods, "override", facts, copies))
	}

	if !isMod(mods, winner.Site) {
		findings = append(findings, listUnused(ws, mods, sites, winner)...)
	}

	return findings
}

// listBlockContests lists an undecided contest for each of the given
// blocks, such as an on_action's effect, that copies in two or more files
// write: merged copies cannot both keep it.
func listBlockContests(ws *workspace.Session, mods *index.Index, sites []index.Site, blocks []string) []Finding {
	var findings []Finding
	for _, block := range blocks {
		var writers []index.Site
		for _, site := range sites {
			if writesBlock(ws, mods, site, block) {
				writers = append(writers, site)
			}
		}

		if !slices.ContainsFunc(writers, func(site index.Site) bool { return site.File != writers[0].File }) {
			continue
		}

		if finding, ok := makeUndecided(ws, mods, writers, "two copies of its "+block+" block conflict"); ok {
			findings = append(findings, finding)
		}
	}

	return findings
}

// writesBlock reports whether a definition writes a block. It is false if
// its file cannot be read.
func writesBlock(ws *workspace.Session, mods *index.Index, site index.Site, block string) bool {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return false
	}

	_, ok := tree.FindChild(statement, block)

	return ok
}

// makeUndecided returns the finding for copies of a name whose kept copy no
// source names. It returns false if they lie in fewer than two of the mods
// and the game.
func makeUndecided(ws *workspace.Session, mods *index.Index, definitions []index.Site, why string) (Finding, bool) {
	sites := lsp.MakeSites(ws, mods, definitions, &index.Winner{Reason: lang.ReasonUnsettled})
	parties := listParties(sites)
	if len(parties) < 2 {
		return Finding{}, false
	}

	facts := lang.DescribeReference(mods.ToReference(definitions[0]))
	facts["mods"], facts["why"] = describeMods(ws, parties), why
	finding := makeFinding(mods, undecidedCode, facts, sites)
	finding.Comparable = true

	return finding, true
}

// describeCause returns why no source settles a winner, in words.
func describeCause(winner index.Winner, precedence lang.Precedence) string {
	switch winner.Cause {
	case lang.CausePriority:
		return "both have the same override priority; give one a higher " + precedence.PriorityKey
	case lang.CauseKeyword:
		return "no source says that the folder takes the " + winner.Prefix + " keyword, or what the game does with it"
	case lang.CauseInject:
		return "the folder takes REPLACE only, and whether " + winner.Prefix + " is refused or replaces is not documented"
	}

	return "it is not documented whether a replace_path drops another mod's files"
}

// describeKept returns what the game does with a mod's copy of a name the
// game also defines, as the end of a sentence.
func describeKept(mods *index.Index, site index.Site) string {
	mode, ok := mods.FindMode(site)
	switch {
	case !ok:
		return "the game keeps the mod's"
	case mode.Replaces:
		return "the mod's entry replaces the game's"
	}

	return "the mod adds to the game's entry"
}

// listUnused returns a finding for each mod whose copies of a name the game
// never uses because it keeps its own.
func listUnused(ws *workspace.Session, mods *index.Index, sites []index.Site, winner index.Winner) []Finding {
	reference := mods.ToReference(sites[0])
	var findings []Finding
	for _, mod := range listMods(mods, listOrigins(mods, sites)) {
		var copies []index.Site
		isKept := false
		for _, site := range sites {
			origin := mods.File(site).Origin
			if origin != mod && origin != files.OriginVanilla {
				continue
			}

			copies = append(copies, site)
			isKept = isKept || origin == mod && mods.ResolveSite(winner, site).Kept
		}

		if isKept {
			continue
		}

		facts := lang.DescribeReference(reference)
		facts["winner"], facts["beaten"] = ws.DescribeMod(mods.File(winner.Site).Origin), ws.DescribeMod(mod)
		findings = append(findings, makeFinding(mods, "unused-copy", facts, lsp.MakeSites(ws, mods, copies, &winner)))
	}

	return findings
}

// listShippedPaths returns the paths the mods ship in the folders the game
// reads, each once, sorted without regard to case. Files that differ only in
// their stage folder share a path. A file the game never reads that hides no
// other is left out.
func listShippedPaths(mods *index.Index) []string {
	var paths []string

	// groups holds the key of the first file at each path listed.
	groups := map[string]bool{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		loaded, copies, ok := mods.FindLoaded(file.Rel)
		if ok {
			copies = append(copies, loaded)
		}

		isCopy := slices.ContainsFunc(copies, func(other files.File) bool { return other.Key == file.Key })
		if !mods.IsGameFolder(position) || !isCopy || groups[copies[0].Key] {
			continue
		}

		groups[copies[0].Key] = true
		paths = append(paths, file.Rel)
	}

	slices.SortFunc(paths, func(first, second string) int { return cmp.Compare(strings.ToLower(first), strings.ToLower(second)) })

	return paths
}

// listPathFindings lists the findings about the files the mods ship at one
// path: file contests, overrides and removed names.
func listPathFindings(ws *workspace.Session, mods *index.Index, path string) []Finding {
	installed, shipped := splitSites(makeFileSites(ws, mods, path), func(site lsp.Site) bool {
		return site.Place.Origin == files.OriginVanilla
	})
	kept := slices.ContainsFunc(shipped, isKept)
	var findings []Finding
	if len(listParties(shipped)) >= 2 && kept {
		facts := lang.DescribeReference(lang.Reference{Database: lang.File, Name: shipped[0].Place.Rel})
		contest := makeFinding(mods, fileContestCode, facts, shipped)
		contest.Removed = listRemoved(contest.Sites)
		findings = append(findings, contest)
	}

	if len(installed) == 0 {
		return findings
	}

	facts := lang.DescribeReference(lang.Reference{Database: lang.File, Name: installed[0].Place.Rel})
	facts["kept"] = "the game keeps its own"
	var uses []Use
	var users []string
	if kept {
		facts["kept"] = "the game keeps the mod's"
		uses, users = listRemovedUses(ws, mods, installed[0].Place.Path)
	}

	code := "override"
	if len(uses) > 0 {
		code = "removed-names"
	}

	override := makeFinding(mods, code, facts, slices.Concat(installed[:1], shipped))
	override.Removed = listRemoved(override.Sites)
	if len(uses) > 0 {
		describeRemoved(ws, mods, &override, uses)
	}

	// Entries a replaced file leaves out are its mod's own problem, unless
	// another mod uses one.
	if slices.ContainsFunc(users, func(origin string) bool { return origin != override.Origin && origin != files.OriginVanilla }) {
		override.Question = lsp.QuestionCompatibility
	}

	return append(findings, override)
}

// makeFileSites returns the sites of the files at a path, in load order.
// The file the game reads comes last, kept for the path reason where it
// hides others.
func makeFileSites(ws *workspace.Session, mods *index.Index, path string) []lsp.Site {
	loaded, hidden, ok := mods.FindLoaded(path)
	if ok {
		hidden = append(hidden, loaded)
	}

	sites := make([]lsp.Site, len(hidden))
	for position, file := range hidden {
		listed, _ := mods.FindFile(file.Path)
		sites[position] = makeFileSite(ws, mods, listed)
	}

	if ok && len(sites) > 1 {
		last := &sites[len(sites)-1]
		last.Reason, last.Label = lang.ReasonPath, lang.Reasons[lang.ReasonPath].Label
	}

	return sites
}

// listRemoved returns the names that the replaced sites define and the kept
// ones do not, each once.
func listRemoved(sites []lsp.Site) []string {
	kept, replaced := splitSites(sites, isKept)
	keptNames := listDefines(kept)

	return slices.DeleteFunc(listDefines(replaced), func(name string) bool { return slices.Contains(keptNames, name) })
}

// listDefines returns the names the sites define, each once, in order.
func listDefines(sites []lsp.Site) []string {
	var names []string
	for _, site := range sites {
		names = append(names, site.Defines...)
	}

	return listUnique(names, func(name string) string { return name })
}

// listRemovedUses returns the names the game's copy of a file defines that
// no file the game reads defines, each with its uses in the game's files,
// the most used first, and the origins of the files that use them. A name
// nothing uses is left out, and so is one the file keeps to itself.
func listRemovedUses(ws *workspace.Session, mods *index.Index, path string) ([]Use, []string) {
	position, ok := mods.FindFile(path)
	if !ok {
		return nil, nil
	}

	var definitions []index.Site
	for number, occurrence := range mods.Occurrences[position] {
		if occurrence.Own && !occurrence.Local {
			definitions = append(definitions, index.Site{File: position, Occurrence: int32(number)})
		}
	}

	var uses []Use
	var users []string
	for _, reference := range listUnique(definitions, mods.ToReference) {
		if len(listLoaded(mods, mods.Resolve(reference))) > 0 {
			continue
		}

		loaded := listLoaded(mods, mods.ListUses(reference.Database, reference.Name))
		if len(loaded) > 0 {
			uses = append(uses, Use{Name: reference.Name, Count: len(loaded), First: lsp.MakeSite(ws, mods, loaded[0])})
		}

		users = append(users, listOrigins(mods, loaded)...)
	}

	slices.SortStableFunc(uses, func(first, second Use) int { return cmp.Compare(second.Count, first.Count) })

	return uses, users
}

// describeRemoved fills in a finding about a mod file that leaves out
// entries still in use: its uses, and the counts its message gives.
func describeRemoved(ws *workspace.Session, mods *index.Index, finding *Finding, uses []Use) {
	// The kept file comes first among a finding's sites.
	kept := finding.Sites[0]
	original := finding.Sites[slices.IndexFunc(finding.Sites, func(site lsp.Site) bool {
		return site.Place.Origin == files.OriginVanilla
	})]

	finding.Uses = uses
	finding.Facts["mod"] = ws.DescribeMod(finding.Origin)
	finding.Facts["total"] = strconv.Itoa(len(listDefines([]lsp.Site{original})))
	finding.Facts["removed"] = strconv.Itoa(len(listRemoved([]lsp.Site{kept, original})))
	finding.Facts["count"] = strconv.Itoa(len(uses))
	finding.Facts["uses"] = strconv.Itoa(countUses(uses))
	finding.Facts["definitions"] = "entries"

	position, _ := mods.FindFile(original.Place.Path)
	if counts := mods.CountDefinitions(position); len(counts) == 1 {
		for database := range counts {
			finding.Facts["definitions"] = lang.DescribeDefinition(database, true)
		}
	}
}

// listSharedMessages lists the localization keys that two or more mods
// define in one language. The game shows the copy in the replace folder
// that ranks first; where two rank alike no source says which it shows, so
// none is kept.
func listSharedMessages(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	groups := partition(listLoaded(mods, messages), func(site index.Site) [2]string {
		return [2]string{mods.Language(site), mods.ToReference(site).Name}
	})

	var findings []Finding
	for _, sites := range groups {
		origins := listOrigins(mods, sites)
		if len(origins) < 2 {
			continue
		}

		reference, language := mods.ToReference(sites[0]), mods.Language(sites[0])
		facts := lang.DescribeReference(reference)
		facts["language"], facts["mods"] = language, describeMods(ws, listMods(mods, origins))
		var shown *index.Winner
		if winner, ok := mods.FindMessage(reference.Name, language); ok {
			shown = &winner
		}

		findings = append(findings, makeFinding(mods, sharedMessageCode, facts, lsp.MakeSites(ws, mods, sites, shown)))
	}

	return findings
}

// findWinner returns the mod whose copy of a contest the game keeps, else
// the finding's origin. A copy merged into the kept one does not win.
func findWinner(finding Finding) string {
	for _, site := range finding.Sites {
		if site.Kept && site.Reason != lang.ReasonInjected && site.Reason != lang.ReasonCombined {
			return site.Place.Origin
		}
	}

	return finding.Origin
}

// listBeaten returns a contest's mods that have no kept copy, in load
// order.
func listBeaten(finding Finding) []string {
	return slices.DeleteFunc(slices.Clone(finding.Mods), func(id string) bool {
		return slices.ContainsFunc(finding.Sites, func(site lsp.Site) bool { return site.Kept && site.Place.Origin == id })
	})
}

// resolveContest names a contest's winner and beaten mods, lists the pair
// of the winner with each beaten mod, and sets its level: info if every
// beaten mod is forgiven, else critical.
func resolveContest(ws *workspace.Session, finding *Finding, winner string, beaten []string, forgiven map[[2]string]bool) {
	finding.Facts["winner"] = ws.DescribeMod(winner)
	finding.Facts["beaten"] = describeMods(ws, beaten)
	finding.Level = lsp.LevelInfo
	finding.Pairs = make([][2]string, len(beaten))
	for position, id := range beaten {
		if !forgiven[[2]string{winner, id}] {
			finding.Level = lsp.LevelCritical
		}

		finding.Pairs[position] = [2]string{winner, id}
		if slices.Index(finding.Mods, id) < slices.Index(finding.Mods, winner) {
			finding.Pairs[position] = [2]string{id, winner}
		}
	}
}

// resolveOrder gives a critical contest that the load order decides, with
// one beaten mod that depends on the winner, the order that fixes it: the
// beaten mod moved just after the winner.
func resolveOrder(finding *Finding, winner string, beaten, order []string, declared map[[2]string]bool) {
	// The load order decides a file two mods ship, and a name kept by the
	// playset's order.
	isDecided := finding.Code == fileContestCode ||
		slices.ContainsFunc(finding.Sites, func(site lsp.Site) bool { return site.Kept && site.Reason == lang.ReasonPlayset })
	if !isDecided || finding.Level != lsp.LevelCritical || len(beaten) != 1 || !declared[[2]string{beaten[0], winner}] {
		return
	}

	rest := slices.DeleteFunc(slices.Clone(order), func(id string) bool { return id == beaten[0] })
	finding.Order = slices.Insert(rest, slices.Index(rest, winner)+1, beaten[0])
}

// resolveForgiven sets a finding's level to info if every pair of the mods
// and the game among its sites is forgiven. A pair with the game never is.
func resolveForgiven(finding *Finding, forgiven map[[2]string]bool) {
	for _, pair := range listEveryPair(listParties(finding.Sites)) {
		if !forgiven[pair] && !forgiven[[2]string{pair[1], pair[0]}] {
			return
		}
	}

	finding.Level = lsp.LevelInfo
}

// listEveryPair returns each pair of the ids, in the order they come in.
func listEveryPair(ids []string) [][2]string {
	pairs := [][2]string{}
	for position, first := range ids {
		for _, second := range ids[position+1:] {
			pairs = append(pairs, [2]string{first, second})
		}
	}

	return pairs
}

// splitWhy returns why the game keeps the winner's copy of a name, as
// parts. It is empty for a file, a finding with no winner, and a reason
// with no long form.
func splitWhy(finding Finding) []lsp.Part {
	kept := slices.IndexFunc(finding.Sites, func(site lsp.Site) bool {
		return site.Kept && lang.Reasons[site.Reason].Why != ""
	})
	if kept < 0 || finding.Facts["winner"] == "" || finding.Database == lang.File {
		return nil
	}

	winner := finding.Sites[kept]
	var beaten []string
	for _, site := range finding.Sites {
		if site.Kept || site.Place.Origin == files.OriginVanilla {
			continue
		}

		file := site.Place.Rel
		if site.Place.Folder == winner.Place.Folder {
			file = site.Place.Name
		}

		beaten = append(beaten, file)
	}

	facts := maps.Clone(finding.Facts)
	facts["folder"], facts["file"] = winner.Place.Folder+"/", winner.Place.Name
	facts["beatenFile"] = lang.JoinList(beaten, "and")

	return lsp.SplitMessage(lang.Reasons[winner.Reason].Why, facts)
}

// describeMods returns the mods' titles joined as in a sentence, such as
// "A, B and C".
func describeMods(ws *workspace.Session, modIDs []string) string {
	titles := make([]string, len(modIDs))
	for position, id := range modIDs {
		titles[position] = ws.DescribeMod(id)
	}

	return lang.JoinList(titles, "and")
}

// listDefinitionChanges lists the names whose kept copy is in another mod
// than in the workspace's own order.
func listDefinitionChanges(ws *workspace.Session, base, mods *index.Index, kept map[lang.Reference]index.Winner) []Change {
	changes := []Change{}
	for reference, winner := range kept {
		if !hasWinner(ws, reference, winner) {
			continue
		}

		previous, known := base.FindKept(ws.Schema, reference)
		if known && !hasWinner(ws, reference, previous) {
			continue
		}

		before := ""
		if known {
			before = base.File(previous.Site).Origin
		}

		if after := mods.File(winner.Site).Origin; after != before {
			changes = append(changes, Change{
				Database: lang.DescribeDefinition(reference.Database, false), Name: reference.Name, Before: before, After: after,
			})
		}
	}

	return changes
}

// hasWinner reports whether one copy of a name wins: its database keeps
// one copy and a source says which. It is false for a database the schema
// lacks.
func hasWinner(ws *workspace.Session, reference lang.Reference, winner index.Winner) bool {
	database := ws.Schema.Databases[reference.Database]

	return database != nil && !database.Precedence.KeepsEvery() && winner.Reason != lang.ReasonUnsettled
}

// listPathChanges lists the paths whose loaded file is in another mod than
// in the workspace's own order, "" where nothing loads it.
func listPathChanges(base, mods *index.Index, paths []string) []Change {
	changes := []Change{}
	for _, path := range paths {
		previous, _, _ := base.FindLoaded(path)
		current, _, _ := mods.FindLoaded(path)
		if previous.Origin != current.Origin {
			changes = append(changes, Change{
				Database: lang.DescribeDefinition(lang.File, false), Name: path, Before: previous.Origin, After: current.Origin,
			})
		}
	}

	return changes
}
