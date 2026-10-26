// contests.go finds the names and files that two or more mods define, which mod wins each, and the ones whose winner
// the sources leave open. It also lists what another load order changes.

package health

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
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

// sharedCodes are the codes of the problems a definition has when others share its name.
var sharedCodes = []string{"duplicate-definition", "conflicting-priority"}

// listContests lists the names that two or more mods define, with the mod whose copy the game keeps and why.
func listContests(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	for _, reference := range defined {
		sites, winner, ok := findSettled(ws, mods, reference, kept)
		origins := lo.FilterMap(sites, func(site index.Site, _ int) (string, bool) {
			return mods.File(site).Origin, isMod(mods, site)
		})
		if !ok || len(lo.Uniq(origins)) < 2 || !isMod(mods, winner.Site) {
			continue
		}
		finding := makeFinding(mods, contestCode, lsp.DescribeReference(reference), makeSites(ws, mods, sites, winner))
		if len(listBeaten(finding)) == 0 {
			continue
		}
		finding.Comparable = true
		findings = append(findings, finding)
	}
	return findings
}

// listFileContests lists the files that two or more mods ship in a folder the game reads. A file has no contest if no
// copy is read.
func listFileContests(ws *workspace.Session, mods *index.Index, paths []string) []Finding {
	folders := listFolders(ws)
	var findings []Finding
	for _, path := range paths {
		sites := lo.Reject(makeFileSites(ws, mods, path), func(site FindingSite, _ int) bool { return site.Place.ReadOnly })
		origins := lo.Map(sites, func(site FindingSite, _ int) string { return site.Place.Origin })
		if len(lo.Uniq(origins)) < 2 || !isGameFolder(folders, sites[0].Place.Rel) ||
			!lo.SomeBy(sites, func(site FindingSite) bool { return site.Kept }) {
			continue
		}
		facts := lsp.DescribeReference(schema.Reference{Database: game.File, Name: sites[0].Place.Rel})
		findings = append(findings, makeFinding(mods, fileContestCode, facts, sites))
	}
	return findings
}

// listUndecided lists the names whose kept copy the sources leave open, and each block of a merged name that copies
// in two files write. Each lies in two or more of the mods and the game.
func listUndecided(ws *workspace.Session, mods *index.Index, defined []schema.Reference, kept map[schema.Reference]index.Winner) []Finding {
	var findings []Finding
	for _, reference := range defined {
		winner, ok := kept[reference]
		precedence := ws.Schema.Databases[reference.Database].Precedence
		if !ok || winner.Reason != index.Unsettled && len(precedence.Contested) == 0 {
			continue
		}
		sites := listLoaded(mods, reference)
		if winner.Reason == index.Unsettled {
			if finding, ok := makeUndecided(ws, mods, sites, describeCause(mods, sites, winner, precedence)); ok {
				findings = append(findings, finding)
			}
		}
		for _, block := range precedence.Contested {
			writers := lo.Filter(sites, func(site index.Site, _ int) bool { return writesBlock(ws, mods, site, block) })
			if len(lo.UniqBy(writers, func(site index.Site) int32 { return site.File })) < 2 {
				continue
			}
			if finding, ok := makeUndecided(ws, mods, writers, "two copies of its "+block+" block conflict"); ok {
				findings = append(findings, finding)
			}
		}
	}
	return findings
}

// describeCause returns the words that say why the sources leave a winner open.
func describeCause(mods *index.Index, sites []index.Site, winner index.Winner, precedence game.Precedence) string {
	switch winner.Cause {
	case "priority":
		return "both have the same override priority; give one a higher " + precedence.PriorityKey
	case "keyword":
		prefix := findModePrefix(mods, sites, func(game.EntryMode) bool { return true })
		return "no source says that the folder takes the " + prefix + " keyword, or what the game does with it"
	case "inject":
		prefix := findModePrefix(mods, sites, func(mode game.EntryMode) bool { return !mode.Replaces })
		return "the folder takes REPLACE only, and whether " + prefix + " is refused or replaces is not documented"
	}
	return "it is not documented whether a replace_path drops another mod's files"
}

// findModePrefix returns the prefix of the first entry mode among definitions that a test of the mode accepts. It
// returns "" if none does.
func findModePrefix(mods *index.Index, sites []index.Site, accepts func(game.EntryMode) bool) string {
	for _, site := range sites {
		if mode, known := mods.FindMode(site); known && accepts(mode) {
			return mode.Prefix
		}
	}
	return ""
}

// makeUndecided returns the finding for copies of a name whose kept copy no source names. It returns false if they
// lie in fewer than two of the mods and the game.
func makeUndecided(ws *workspace.Session, mods *index.Index, definitions []index.Site, why string) (Finding, bool) {
	sites := lo.Map(definitions, func(definition index.Site, _ int) FindingSite {
		site := makeSite(ws, mods, definition)
		site.Reason = index.Unsettled
		return site
	})
	parties := listParties(sites)
	if len(parties) < 2 {
		return Finding{}, false
	}
	facts := lsp.DescribeReference(mods.ToReference(definitions[0]))
	facts["mods"], facts["why"] = describeMods(ws, parties), why
	finding := makeFinding(mods, undecidedCode, facts, sites)
	finding.Comparable = true
	return finding, true
}

// listParties returns the mods and the game that a finding's sites lie in, each once, in load order.
func listParties(sites []FindingSite) []string {
	return lo.Uniq(lo.Map(sites, func(site FindingSite, _ int) string { return site.Place.Origin }))
}

// writesBlock reports whether a definition writes a block, such as an on_action's effect. It is false if its file
// cannot be read.
func writesBlock(ws *workspace.Session, mods *index.Index, site index.Site, block string) bool {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return false
	}
	_, ok := tree.FindChild(statement, block)
	return ok
}

// listSharedMessages lists the localization keys that two or more mods define in one language. Which copy the game
// shows is not known, so none is marked kept.
func listSharedMessages(ws *workspace.Session, mods *index.Index, messages []index.Site) []Finding {
	loaded := lo.Filter(messages, func(site index.Site, _ int) bool { return mods.IsLoaded(site.File) })
	groups := lo.PartitionBy(loaded, func(site index.Site) [2]string {
		return [2]string{mods.Language(site), mods.ToReference(site).Name}
	})
	var findings []Finding
	for _, sites := range groups {
		origins := lo.Uniq(lo.Map(sites, func(site index.Site, _ int) string { return mods.File(site).Origin }))
		if len(origins) < 2 {
			continue
		}
		facts := lsp.DescribeReference(mods.ToReference(sites[0]))
		facts["language"], facts["mods"] = mods.Language(sites[0]), describeMods(ws, listMods(mods, origins))
		copies := lo.Map(sites, func(site index.Site, _ int) FindingSite { return makeSite(ws, mods, site) })
		findings = append(findings, makeFinding(mods, sharedMessageCode, facts, copies))
	}
	return findings
}

// listFolders returns the lower-case names at the top of the game's own folders. The game reads a mod's files only
// under these. A file at the top of a DLC's folder is left out.
func listFolders(ws *workspace.Session) map[string]bool {
	folders := map[string]bool{}
	for _, file := range ws.Install.Files {
		folder, _, nested := strings.Cut(strings.ToLower(file.Rel), "/")
		if nested || !isDLCFile(file) {
			folders[folder] = true
		}
	}
	return folders
}

// isDLCFile reports whether a file lies at the top of a DLC's folder.
func isDLCFile(file files.File) bool {
	return filepath.Base(filepath.Dir(filepath.Dir(file.Path))) == game.DLCFolder
}

// isGameFolder reports whether the game reads a mod's file at a path.
func isGameFolder(folders map[string]bool, rel string) bool {
	folder, _, _ := strings.Cut(strings.ToLower(rel), "/")
	return folders[folder]
}

// resolveQuestion makes a finding of a shared name a compatibility question when the name's loaded definitions lie in
// two or more mods. It lists those mods on the finding.
func resolveQuestion(mods *index.Index, finding *Finding) {
	origins := lo.FilterMap(mods.Resolve(schema.Reference{Name: finding.Name}), func(site index.Site, _ int) (string, bool) {
		file := mods.File(site)
		return file.Origin, mods.IsLoaded(site.File) && file.Origin != game.OriginVanilla &&
			game.DescribeDefinition(mods.ToReference(site).Database, false) == finding.Database
	})
	if origins = lo.Uniq(origins); len(origins) > 1 {
		finding.Question, finding.Mods = lsp.QuestionCompatibility, listMods(mods, origins)
	}
}

// listForgiven returns the pairs of mods whose contests are no problem, the winner first. The winner depends on the
// other mod, or the workspace ignores the pair.
func listForgiven(declared map[[2]string]bool, ignored [][2]string) map[[2]string]bool {
	forgiven := maps.Clone(declared)
	for _, pair := range ignored {
		forgiven[pair], forgiven[[2]string{pair[1], pair[0]}] = true, true
	}
	return forgiven
}

// resolveContest names a contest's winner and beaten mods and sets its level. The level is info if every beaten mod
// is forgiven, and serious otherwise.
func resolveContest(ws *workspace.Session, finding *Finding, forgiven map[[2]string]bool) {
	winner, beaten := findWinner(*finding), listBeaten(*finding)
	finding.Facts["winner"] = describeMod(ws, winner)
	finding.Facts["beaten"] = describeMods(ws, beaten)
	finding.Level = lo.Ternary(lo.EveryBy(beaten, func(id string) bool { return forgiven[[2]string{winner, id}] }),
		lsp.LevelInfo, lsp.LevelSerious)
}

// resolveForgiven sets a finding's level to info if every two of the mods and the game among its sites are a forgiven
// pair, in either order. A pair with the game never is.
func resolveForgiven(finding *Finding, forgiven map[[2]string]bool) {
	if lo.EveryBy(listEveryPair(listParties(finding.Sites)), func(pair [2]string) bool {
		return forgiven[pair] || forgiven[[2]string{pair[1], pair[0]}]
	}) {
		finding.Level = lsp.LevelInfo
	}
}

// listEveryPair returns every two of some ids, each pair in their order.
func listEveryPair(ids []string) [][2]string {
	return lo.FlatMap(ids, func(first string, position int) [][2]string {
		return lo.Map(ids[position+1:], func(second string, _ int) [2]string { return [2]string{first, second} })
	})
}

// listRivals returns each pair of workspace mods a finding sets against each other, in load order.
func listRivals(finding Finding) [][2]string {
	switch finding.Code {
	case contestCode, fileContestCode:
		winner := findWinner(finding)
		return lo.Map(listBeaten(finding), func(beaten string, _ int) [2]string {
			if slices.Index(finding.Mods, beaten) < slices.Index(finding.Mods, winner) {
				return [2]string{beaten, winner}
			}
			return [2]string{winner, beaten}
		})
	case undecidedCode, sharedMessageCode:
		return listEveryPair(finding.Mods)
	}
	return nil
}

// findWinner returns the mod whose copy of a contest the game keeps. It returns the finding's own origin if no copy is
// kept.
func findWinner(finding Finding) string {
	site, ok := lo.Find(finding.Sites, func(site FindingSite) bool {
		return site.Kept && site.Reason != index.Injected && site.Reason != index.Combined
	})
	return lo.Ternary(ok, site.Place.Origin, finding.Origin)
}

// listBeaten returns the mods of a contest that have no kept copy, in load order. The game itself is never beaten.
func listBeaten(finding Finding) []string {
	return lo.Reject(finding.Mods, func(id string, _ int) bool {
		return lo.SomeBy(finding.Sites, func(site FindingSite) bool { return site.Kept && site.Place.Origin == id })
	})
}

// describeMods returns the titles of mods joined as in a sentence: "A", "A and B", "A, B and C". The game is called by
// its name. It returns "" for none.
func describeMods(ws *workspace.Session, modIDs []string) string {
	titles := lo.Map(modIDs, func(id string, _ int) string { return describeMod(ws, id) })
	if len(titles) < 2 {
		return strings.Join(titles, "")
	}
	return strings.Join(titles[:len(titles)-1], ", ") + " and " + titles[len(titles)-1]
}

// resolveOrder gives a serious contest that the load order decides the order that fixes it. It moves the one beaten
// mod just after the winner, where that mod declares the winner as a dependency.
func resolveOrder(finding *Finding, order []string, declared map[[2]string]bool) {
	winner, beaten := findWinner(*finding), listBeaten(*finding)
	if !isOrderDecided(*finding) || finding.Level != lsp.LevelSerious || len(beaten) != 1 ||
		!declared[[2]string{beaten[0], winner}] {
		return
	}
	rest := lo.Reject(order, func(id string, _ int) bool { return id == beaten[0] })
	finding.Order = slices.Insert(rest, slices.Index(rest, winner)+1, beaten[0])
}

// isOrderDecided reports whether the load order decides a contest: a file two mods ship, or a name the game keeps by
// the load order. File names, priorities and entry modes decide every other name, whatever the order.
func isOrderDecided(finding Finding) bool {
	return finding.Code == fileContestCode ||
		lo.SomeBy(finding.Sites, func(site FindingSite) bool { return site.Kept && site.Reason == "playset" })
}

// listDefinitionChanges lists the names whose kept copy moves to another mod than in the workspace's own order.
func listDefinitionChanges(ws *workspace.Session, base, mods *index.Index, kept map[schema.Reference]index.Winner) []Change {
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
				Database: game.DescribeDefinition(reference.Database, false), Name: reference.Name, Before: before, After: after,
			})
		}
	}
	return changes
}

// listPathChanges lists the paths in the game's folders whose loaded file lies in another mod than in the workspace's
// index. A path nothing loads has "" as its mod.
func listPathChanges(ws *workspace.Session, base, mods *index.Index, paths []string) []Change {
	folders := listFolders(ws)
	changes := []Change{}
	for _, path := range paths {
		previous, _, _ := base.FindLoaded(path)
		current, _, _ := mods.FindLoaded(path)
		if previous.Origin != current.Origin && isGameFolder(folders, path) {
			changes = append(changes, Change{
				Database: game.DescribeDefinition(game.File, false), Name: path, Before: previous.Origin, After: current.Origin,
			})
		}
	}
	return changes
}

// compareChanges orders two changes by database, then by name.
func compareChanges(a, b Change) int {
	return cmp.Or(cmp.Compare(a.Database, b.Database), cmp.Compare(a.Name, b.Name))
}
