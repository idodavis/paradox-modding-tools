// contests.go finds the names and files that two or more mods, or a mod and the
// game, both write. It reports which copy the game keeps, and the names whose
// kept copy the sources leave open. It also lists what another load order
// changes.

package health

import (
	"cmp"
	"maps"
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The codes of the findings where the mods and the game write the same thing.
const (
	contestCode       = "contest"           // two or more mods define one name, and the game keeps one copy
	fileContestCode   = "file-contest"      // two or more mods ship one file
	undecidedCode     = "undecided-contest" // copies of one name whose kept copy the sources leave open
	sharedMessageCode = "shared-message"    // two or more mods define one localization key in one language
)

// listDefinitionFindings lists the findings about the loaded copies of one
// name, given the winner: contests, undecided contests, overrides and unused
// copies. A database that keeps every copy has none.
func listDefinitionFindings(ws *workspace.Session, mods *index.Index, reference lang.Reference, winner index.Winner) []Finding {
	sites := listLoaded(mods, mods.Resolve(reference))
	precedence := ws.Schema.Databases[reference.Database].Precedence
	var findings []Finding
	for _, block := range precedence.Contested {
		writers := lo.Filter(sites, func(site index.Site, _ int) bool { return writesBlock(ws, mods, site, block) })
		if len(lo.UniqBy(writers, func(site index.Site) int32 { return site.File })) < 2 {
			continue
		}
		if finding, ok := makeUndecided(ws, mods, writers, "two copies of its "+block+" block conflict"); ok {
			findings = append(findings, finding)
		}
	}
	if winner.Reason == lang.ReasonUnsettled {
		if finding, ok := makeUndecided(ws, mods, sites, describeCause(winner, precedence)); ok {
			findings = append(findings, finding)
		}
		return findings
	}
	if precedence.KeepsEvery() {
		return findings
	}
	origins := lo.Uniq(lo.FilterMap(sites, func(site index.Site, _ int) (string, bool) {
		return mods.File(site).Origin, isMod(mods, site)
	}))
	if len(origins) >= 2 && isMod(mods, winner.Site) {
		contest := makeFinding(mods, contestCode, lang.DescribeReference(reference), lsp.MakeSites(ws, mods, sites, &winner))
		if len(listBeaten(contest)) > 0 {
			contest.Comparable = true
			findings = append(findings, contest)
		}
	}
	isGame := func(site index.Site) bool { return !isMod(mods, site) }
	if !lo.SomeBy(sites, isGame) || lo.EveryBy(sites, isGame) {
		return findings
	}
	if site, ok := lo.Find(sites, func(site index.Site) bool { return isMod(mods, site) && mods.ResolveSite(winner, site).Kept }); ok {
		facts := lang.DescribeReference(reference)
		facts["kept"] = describeKept(mods, site)
		findings = append(findings, makeFinding(mods, "override", facts, lsp.MakeSites(ws, mods, sites, &winner)))
	}
	if isGame(winner.Site) {
		findings = append(findings, listUnused(ws, mods, sites, winner)...)
	}
	return findings
}

// describeKept returns what the game does with a mod's copy of a name the game
// also defines, as the end of a sentence. A copy written with an entry mode
// replaces the game's or adds to it, as its mode says; a plain copy is kept in
// its place.
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

// listPathFindings lists the findings about the files the mods ship at one
// path: file contests, overrides and removed-names.
func listPathFindings(ws *workspace.Session, mods *index.Index, path string) []Finding {
	installed, shipped := lo.FilterReject(makeFileSites(ws, mods, path), func(site lsp.Site, _ int) bool {
		return site.Place.Origin == files.OriginVanilla
	})
	kept := lo.SomeBy(shipped, func(site lsp.Site) bool { return site.Kept })
	var findings []Finding
	if len(lo.UniqBy(shipped, func(site lsp.Site) string { return site.Place.Origin })) >= 2 && kept {
		facts := lang.DescribeReference(lang.Reference{Database: lang.File, Name: shipped[0].Place.Rel})
		contest := makeFinding(mods, fileContestCode, facts, shipped)
		contest.Removed = listRemoved(contest.Sites)
		findings = append(findings, contest)
	}
	if len(installed) == 0 {
		return findings
	}
	facts := lang.DescribeReference(lang.Reference{Database: lang.File, Name: installed[0].Place.Rel})
	facts["kept"] = lo.Ternary(kept, "the game keeps the mod's", "the game keeps its own")
	var uses []Use
	var users []string
	if kept {
		uses, users = listRemovedUses(ws, mods, installed[0].Place.Path)
	}
	override := makeFinding(mods, lo.Ternary(len(uses) > 0, "removed-names", "override"), facts,
		slices.Concat(installed[:1], shipped))
	override.Removed = listRemoved(override.Sites)
	if len(uses) > 0 {
		describeRemoved(ws, mods, &override, uses)
	}
	// entries a replaced file leaves out are its mod's own problem, unless
	// another mod uses one
	if lo.SomeBy(users, func(origin string) bool { return origin != override.Origin && origin != files.OriginVanilla }) {
		override.Question = lsp.QuestionCompatibility
	}
	return append(findings, override)
}

// describeCause returns the words that say why the sources leave a winner
// open.
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

// makeUndecided returns the finding for copies of a name whose kept copy no
// source names. It returns false if they lie in fewer than two of the mods and
// the game.
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

// listParties returns the mods and the game that a finding's sites lie in, each
// once, in load order.
func listParties(sites []lsp.Site) []string {
	return lo.Uniq(lo.Map(sites, func(site lsp.Site, _ int) string { return site.Place.Origin }))
}

// writesBlock reports whether a definition writes a block, such as an
// on_action's effect. It is false if its file cannot be read.
func writesBlock(ws *workspace.Session, mods *index.Index, site index.Site, block string) bool {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return false
	}
	_, ok := tree.FindChild(statement, block)
	return ok
}

// listSharedMessages lists the localization keys that two or more mods
// define in one language. The copy in the replace folder that ranks first is
// the one the game shows; where two rank alike no source says which it
// shows, so none is marked kept.
func listSharedMessages(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	loaded := listLoaded(mods, messages)
	groups := lo.PartitionBy(loaded, func(site index.Site) [2]string {
		return [2]string{mods.Language(site), mods.ToReference(site).Name}
	})
	var findings []Finding
	for _, sites := range groups {
		origins := lo.Uniq(lo.Map(sites, func(site index.Site, _ int) string { return mods.File(site).Origin }))
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

// resolveContest names a contest's winner and beaten mods, lists its pairs of
// the winner with each beaten mod, and sets its level: info if every beaten mod
// is forgiven, critical otherwise.
func resolveContest(ws *workspace.Session, finding *Finding, forgiven map[[2]string]bool) {
	winner, beaten := findWinner(*finding), listBeaten(*finding)
	finding.Facts["winner"] = ws.DescribeMod(winner)
	finding.Facts["beaten"] = describeMods(ws, beaten)
	finding.Level = lo.Ternary(lo.EveryBy(beaten, func(id string) bool { return forgiven[[2]string{winner, id}] }),
		lsp.LevelInfo, lsp.LevelCritical)
	finding.Pairs = lo.Map(beaten, func(id string, _ int) [2]string {
		if slices.Index(finding.Mods, id) < slices.Index(finding.Mods, winner) {
			return [2]string{id, winner}
		}
		return [2]string{winner, id}
	})
}

// splitWhy returns why the game keeps the winner's copy of a name, as parts. It
// is empty for a file, a finding with no winner, and a reason with no long
// form.
func splitWhy(finding Finding) []lsp.Part {
	winner, ok := lo.Find(finding.Sites, func(site lsp.Site) bool {
		return site.Kept && lang.Reasons[site.Reason].Why != ""
	})
	if !ok || finding.Facts["winner"] == "" || finding.Database == lang.File {
		return nil
	}
	folder := winner.Place.Folder
	beaten := lo.FilterMap(finding.Sites, func(site lsp.Site, _ int) (string, bool) {
		return lo.Ternary(site.Place.Folder == folder, site.Place.Name, site.Place.Rel),
			!site.Kept && site.Place.Origin != files.OriginVanilla
	})
	facts := maps.Clone(finding.Facts)
	facts["folder"], facts["file"], facts["beatenFile"] = folder+"/", winner.Place.Name, lang.JoinList(beaten, "and")
	return lsp.SplitMessage(lang.Reasons[winner.Reason].Why, facts)
}

// resolveForgiven sets a finding's level to info if every pair of the mods and
// the game among its sites is forgiven. A pair with the game never is.
func resolveForgiven(finding *Finding, forgiven map[[2]string]bool) {
	if lo.EveryBy(listEveryPair(listParties(finding.Sites)), func(pair [2]string) bool {
		return forgiven[pair] || forgiven[[2]string{pair[1], pair[0]}]
	}) {
		finding.Level = lsp.LevelInfo
	}
}

// listEveryPair returns each pair of the given ids, in the order the ids come
// in.
func listEveryPair(ids []string) [][2]string {
	return lo.FlatMap(ids, func(first string, position int) [][2]string {
		return lo.Map(ids[position+1:], func(second string, _ int) [2]string { return [2]string{first, second} })
	})
}

// findWinner returns the mod whose copy of a contest the game keeps. It returns
// the finding's own origin if no copy is kept.
func findWinner(finding Finding) string {
	site, ok := lo.Find(finding.Sites, func(site lsp.Site) bool {
		return site.Kept && site.Reason != lang.ReasonInjected && site.Reason != lang.ReasonCombined
	})
	return lo.Ternary(ok, site.Place.Origin, finding.Origin)
}

// listBeaten returns the mods of a contest that have no kept copy, in load
// order. The game itself is never beaten.
func listBeaten(finding Finding) []string {
	return lo.Reject(finding.Mods, func(id string, _ int) bool {
		return lo.SomeBy(finding.Sites, func(site lsp.Site) bool { return site.Kept && site.Place.Origin == id })
	})
}

// describeMods returns the titles of mods joined as in a sentence, such as "A,
// B and C". It returns "" for none.
func describeMods(ws *workspace.Session, modIDs []string) string {
	return lang.JoinList(lo.Map(modIDs, func(id string, _ int) string { return ws.DescribeMod(id) }), "and")
}

// resolveOrder gives a critical contest that the load order decides the order
// that fixes it: the beaten mod moved just after the winner it depends on.
func resolveOrder(finding *Finding, order []string, declared map[[2]string]bool) {
	winner, beaten := findWinner(*finding), listBeaten(*finding)
	if !isOrderDecided(*finding) || finding.Level != lsp.LevelCritical || len(beaten) != 1 ||
		!declared[[2]string{beaten[0], winner}] {
		return
	}
	rest := lo.Reject(order, func(id string, _ int) bool { return id == beaten[0] })
	finding.Order = slices.Insert(rest, slices.Index(rest, winner)+1, beaten[0])
}

// isOrderDecided reports whether the load order decides a contest: a file two
// mods ship, or a name the game keeps by the load order.
func isOrderDecided(finding Finding) bool {
	return finding.Code == fileContestCode ||
		lo.SomeBy(finding.Sites, func(site lsp.Site) bool { return site.Kept && site.Reason == lang.ReasonPlayset })
}

// listDefinitionChanges lists the names whose kept copy moves to another mod
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
		before := lo.Ternary(known, base.File(previous.Site).Origin, "")
		if after := mods.File(winner.Site).Origin; after != before {
			changes = append(changes, Change{
				Database: lang.DescribeDefinition(reference.Database, false), Name: reference.Name, Before: before, After: after,
			})
		}
	}
	return changes
}

// listPathChanges lists the paths whose loaded file lies in another mod than in
// the workspace's own order. A path nothing loads has "" as its mod.
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

// compareChanges orders two changes by database, then by name.
func compareChanges(first, second Change) int {
	return cmp.Or(cmp.Compare(first.Database, second.Database), cmp.Compare(first.Name, second.Name))
}
