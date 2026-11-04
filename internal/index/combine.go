// combine.go decides which file and which definition the game keeps when several compete.

package index

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
)

// A Winner is the definition the game keeps when several share a name, and the reason it wins. Where the sources do
// not say which copy the game keeps, the reason is unsettled and the winner is one of the copies in question.
type Winner struct {
	Site   Site   `json:"site"`
	Reason Reason `json:"reason"`          // why the game keeps it; "" for a lone copy
	Cause  Cause  `json:"cause,omitempty"` // what leaves an unsettled choice open; "" for a settled one
	Merged []Site `json:"merged,omitempty"`
}

// A Reason is why the game keeps a copy of a name, why it does not read a copy, or that no source says which copy it
// keeps.
type Reason string

// The reasons: the last copy, the first, the playset's last, the one with the highest priority, the one an entry
// mode puts in place, the first of copies merged into one, the copy left when another mod's replace_path or a path
// the game does not read drops the one that would win, and a localization key's in a replace folder; a copy merged
// into the winner by an entry mode or by its database; a choice no source settles; and a copy in a file the game does
// not read, because a file at the same path later in the load order replaces its file, or a mod's replace_path drops
// its folder.
const (
	ReasonLast          Reason = "last"
	ReasonFirst         Reason = "first"
	ReasonPlayset       Reason = "playset"
	ReasonPriority      Reason = "priority"
	ReasonMode          Reason = "mode"
	ReasonMerge         Reason = "merge"
	ReasonReplace       Reason = "replace"
	ReasonPath          Reason = "path"
	ReasonReplaceFolder Reason = "replace-folder"
	ReasonInjected      Reason = "injected"
	ReasonCombined      Reason = "combined"
	ReasonUnsettled     Reason = "unsettled"
	ReasonShadowed      Reason = "shadowed"
	ReasonDropped       Reason = "dropped"
)

// Reasons holds each reason in words: its label, and for a reason a contest's winner can have, why the winner wins at
// length, as a template the contest's facts fill: {folder}, {file}, {winner}, {beaten} and {beatenFile}. The label of
// a reason for a copy the game does not read is a template too, filled with {mod}, the mod that hides the copy.
var Reasons = map[Reason]struct{ Label, Why string }{
	ReasonLast: {Label: "Last file by name", Why: "The game reads every file in {folder} in order of file name and " +
		"keeps the copy it reads last. {winner}'s file, {file}, comes after {beaten}'s {beatenFile}, so its copy " +
		"wins. Changing the load order doesn't change this."},
	ReasonFirst: {Label: "First file by name", Why: "The game reads every file in {folder} in order of file name and " +
		"keeps the first copy it reads. {winner}'s file, {file}, comes before {beaten}'s {beatenFile}, so its copy " +
		"wins. Changing the load order doesn't change this."},
	ReasonPlayset: {Label: "Lower in the load order", Why: "The game keeps the copy from the mod lowest in the load " +
		"order. {winner} is lower than {beaten}, so its copy wins. Moving a mod in the load order changes this."},
	ReasonPath: {Label: "Same path, lower in the load order", Why: "Another copy would win, but it is in a file the " +
		"game never reads: a file at the same path, lower in the load order, hides it. Of the copies the game reads, " +
		"it keeps {winner}'s."},
	ReasonPriority: {Label: "Higher override priority", Why: "The game keeps the copy with the highest override " +
		"priority, whatever the file names or the load order. {winner}'s copy has the highest, so it wins."},
	ReasonMode: {Label: "By its entry mode", Why: "The game applies entry modes, such as REPLACE:, after every plain " +
		"copy, and the last one that creates or replaces the definition wins: {winner}'s, in {file}."},
	ReasonMerge:    {Label: "Combined"},
	ReasonInjected: {Label: "Injected"},
	ReasonCombined: {Label: "Combined"},
	ReasonReplace: {Label: "After a replace_path", Why: "The game's own copy would win, but a mod's replace_path " +
		"makes the game skip the game's files in that folder. Of the copies the game reads, it keeps {winner}'s."},
	ReasonReplaceFolder: {Label: "In a replace folder"},
	ReasonUnsettled:     {Label: "Not documented"},
	ReasonShadowed:      {Label: "The copy of this file in {mod} replaces it"},
	ReasonDropped:       {Label: "A replace_path in {mod} drops this folder"},
}

// A Cause is what leaves the choice of a kept copy open: a tie of priorities, a key the game may read as an entry
// mode, an INJECT no source settles, or a later mod's replace_path.
type Cause string

// The causes, in the order Cause's comment gives them.
const (
	CausePriority Cause = "priority"
	CauseKeyword  Cause = "keyword"
	CauseInject   Cause = "inject"
	CauseReplace  Cause = "replace"
)

// IsKept reports whether the game keeps a definition of the name this winner won: the winner itself, or one merged
// into it.
func (winner Winner) IsKept(site Site) bool {
	return winner.Site == site || slices.Contains(winner.Merged, site)
}

// ResolveSite reports whether the game keeps a copy of a winner's name, and why. If the winner is unsettled, no copy
// is kept, and a copy in a file the game reads has the reason unsettled. A copy in a file the game does not read is
// never kept: its reason is shadowed or dropped, and the third result is the origin of the mod that hides its file, as
// FindHider gives it. A beaten copy in a file the game reads has no reason.
func (index *Index) ResolveSite(winner Winner, site Site) (bool, Reason, string) {
	unsettled := winner.Reason == ReasonUnsettled
	switch {
	case !unsettled && site == winner.Site:
		return true, winner.Reason, ""
	case !unsettled && slices.Contains(winner.Merged, site):
		return true, lo.Ternary(index.Occurrence(site).Mode != 0, ReasonInjected, ReasonCombined), ""
	case index.IsLoaded(site.File):
		return false, lo.Ternary(unsettled, ReasonUnsettled, ""), ""
	}
	reason, hider := index.FindHider(site.File)
	return false, reason, hider
}

// FindHider returns why the game does not read a file and the origin of the mod that hides it. The reason is dropped
// and the mod is the first whose replace_path names the file's folder, or shadowed and the mod is the one whose file at
// the same path comes later in the load order. It returns "" and "" if the game reads the file, or the index lists no
// such file for the game to read.
func (index *Index) FindHider(file int32) (Reason, string) {
	current := index.Files[file]
	loaded, hidden, _ := index.FindLoaded(current.Rel)
	if !slices.ContainsFunc(hidden, func(other files.File) bool { return other.Key == current.Key }) {
		return "", ""
	}
	if index.isDropped(file) {
		return ReasonDropped, index.Roots[index.listReplacers(index.placements[file].key)[0]].Origin
	}
	return ReasonShadowed, loaded.Origin
}

// FindWinner returns the definition of a name that the game keeps, by the database's precedence. It returns false if
// the game keeps none.
func (index *Index) FindWinner(sites []Site, precedence game.Precedence) (Winner, bool) {
	ordered := lo.Reject(index.listLoadOrder(sites, precedence.ByPlayset), func(site Site, _ int) bool {
		return index.placements[site.File].unmounted
	})
	loaded := lo.Filter(ordered, func(site Site, _ int) bool { return index.IsLoaded(site.File) })
	winner, ok := index.resolveWinner(loaded, precedence)
	if !ok || winner.Reason == ReasonUnsettled || precedence.KeepsEvery() {
		return winner, ok
	}
	if len(loaded) < len(ordered) {
		winner.Reason = index.resolveReason(winner, ordered, precedence)
	}
	if index.isReplaceOpen(loaded, winner, precedence) {
		winner.Reason, winner.Cause = ReasonUnsettled, CauseReplace
	}
	return winner, true
}

// FindKept returns the definition the game keeps of a name in a database, local ones aside. It returns false if there
// is none.
func (index *Index) FindKept(schema *schema.Schema, reference schema.Reference) (Winner, bool) {
	return index.findKept(schema, reference, func(Site) bool { return false })
}

// FindKeptLocal is FindKept for a reference written in a file: that file's local definitions count too. A file the
// index does not list has the position -1, which counts none.
func (index *Index) FindKeptLocal(schema *schema.Schema, reference schema.Reference, file int32) (Winner, bool) {
	return index.findKept(schema, reference, func(site Site) bool { return site.File == file })
}

// findKept returns the definition the game keeps of a name in a database. A local definition counts where a test of
// its site says it is visible.
func (index *Index) findKept(schema *schema.Schema, reference schema.Reference, isVisible func(Site) bool) (Winner, bool) {
	database, ok := schema.Databases[reference.Database]
	if !ok {
		return Winner{}, false
	}
	return index.FindWinner(index.listDefining(database.Name, reference.Name, isVisible), database.Precedence)
}

// ListKept returns the definition the game keeps of each name in a database, one for each name, in load order. A local
// definition never counts. It is empty if the schema has no such database.
func (index *Index) ListKept(schema *schema.Schema, database string) []Winner {
	var kept []Winner
	listed := map[string]bool{}
	for _, site := range index.ListDefinitions(database) {
		reference := index.ToReference(site)
		if listed[reference.Name] {
			continue
		}
		listed[reference.Name] = true
		if winner, ok := index.FindKept(schema, reference); ok {
			kept = append(kept, winner)
		}
	}
	return kept
}

// ListImages returns the image files the game reads under a folder, ignoring case. An empty folder gives every image.
func (index *Index) ListImages(folder string) []files.File {
	folder = strings.ToLower(folder)
	var images []files.File
	for position, file := range index.Files {
		if file.Format == files.FormatImage && strings.HasPrefix(index.placements[position].key, folder) &&
			index.IsLoaded(int32(position)) {
			images = append(images, file)
		}
	}
	return images
}

// FindLoaded returns the file the game reads at a relative path, and the files it hides. It returns false if the game
// reads none.
func (index *Index) FindLoaded(rel string) (loaded files.File, hidden []files.File, ok bool) {
	var kept []files.File
	for _, position := range index.byRel[toRelKey(index.row, rel)] {
		file := index.Files[position]
		switch {
		case !index.IsListed(position), index.placements[position].unmounted, !index.hasFile(position):
		case index.isDropped(position):
			hidden = append(hidden, file)
		default:
			kept = append(kept, file)
		}
	}
	if len(kept) == 0 {
		return files.File{}, hidden, false
	}
	return kept[len(kept)-1], append(hidden, kept[:len(kept)-1]...), true
}

// toRelKey returns the key of the group a relative path belongs to: the path in lower case, less its stage folder,
// so that a path shared by two stages is one path.
func toRelKey(row *game.Game, rel string) string {
	_, rest := row.SplitStage(strings.ToLower(rel))
	return rest
}

// ResolveTexture returns the file the game reads for a texture's path as script writes it, trying each image
// extension the game accepts in place of the written one. It returns false if the game reads no such file.
func (index *Index) ResolveTexture(rel string) (string, bool) {
	return texture.Resolve(rel, func(candidate string) (string, bool) {
		loaded, _, ok := index.FindLoaded(candidate)
		return loaded.Path, ok
	})
}

// IsLoaded reports whether the game reads a file at all.
func (index *Index) IsLoaded(file int32) bool {
	current := index.Files[file]
	loaded, _, ok := index.FindLoaded(current.Rel)
	return ok && loaded.Key == current.Key
}

// listLoadOrder returns sites in the order the game loads their definitions: by their files' load order, or by
// their files' roots where the playset decides, and within one file as written.
func (index *Index) listLoadOrder(sites []Site, byPlayset bool) []Site {
	return slices.SortedStableFunc(slices.Values(sites), func(a, b Site) int {
		first, second := index.placements[a.File], index.placements[b.File]
		if byPlayset {
			return cmp.Or(cmp.Compare(first.root, second.root), compareSites(a, b))
		}
		return cmp.Or(CompareLoadOrder(first.rest, second.rest), compareSites(a, b))
	})
}

// CompareLoadOrder orders two relative paths the way the game loads files, a folder's files before its subfolders'. It
// returns a negative number if the first path loads first, a positive one if it loads last, and 0 if they are equal.
func CompareLoadOrder(a, b string) int {
	for {
		first, restA, moreA := strings.Cut(a, "/")
		second, restB, moreB := strings.Cut(b, "/")
		switch {
		case moreA != moreB:
			if moreA {
				return 1
			}
			return -1
		case first != second:
			return strings.Compare(first, second)
		case !moreA:
			return 0
		}
		a, b = restA, restB
	}
}

// resolveWinner returns the winner among copies in load order, all in files the game reads. It returns false if
// there are none, or if entry modes find nothing to act on.
func (index *Index) resolveWinner(ordered []Site, precedence game.Precedence) (Winner, bool) {
	last := len(ordered) - 1
	switch {
	case len(ordered) == 0:
		return Winner{}, false
	case precedence.Modes != "":
		return index.resolveModes(ordered, precedence)
	case lo.SomeBy(ordered, func(site Site) bool { return index.Occurrence(site).Mode != 0 }):
		return Winner{Site: ordered[0], Reason: ReasonUnsettled, Cause: CauseKeyword}, true
	case len(ordered) == 1:
		return Winner{Site: ordered[0]}, true
	case precedence.Keeps == "first":
		return Winner{Site: ordered[0], Reason: ReasonFirst}, true
	case precedence.KeepsEvery():
		return Winner{Site: ordered[0], Reason: ReasonMerge, Merged: ordered}, true
	case precedence.Keeps == "highest":
		return index.resolvePriority(ordered, precedence), true
	case precedence.ByPlayset:
		return Winner{Site: ordered[last], Reason: ReasonPlayset}, true
	}
	return Winner{Site: ordered[last], Reason: ReasonLast}, true
}

// resolveModes returns the winner among the copies of a name in a database that takes entry modes, in load order. It
// returns false if no copy is left for the modes to act on and none of them creates one.
func (index *Index) resolveModes(ordered []Site, precedence game.Precedence) (Winner, bool) {
	plain, modes := lo.FilterReject(ordered, func(site Site, _ int) bool { return index.Occurrence(site).Mode == 0 })
	rank := func(site Site) int {
		mode, _ := index.FindMode(site)
		return slices.IndexFunc(game.EntryModes, func(other game.EntryMode) bool { return other.Prefix == mode.Prefix })
	}
	// the modes act after the plain copies, kind by kind, then in load order (the Vic3 and EU5 wikis' Mod
	// compatibility, Processing order)
	slices.SortStableFunc(modes, func(a, b Site) int { return cmp.Compare(rank(a), rank(b)) })
	var winner Winner
	defined, modeWins, hasAdding := len(plain) > 0, false, false
	if defined {
		winner = Winner{Site: plain[0], Merged: lo.Ternary(precedence.KeepsEvery(), plain, plain[:1])}
	}
	for _, site := range modes {
		mode, known := index.FindMode(site)
		isInject := known && !mode.Replaces && !mode.Optional && !mode.Creates
		hasAdding = hasAdding || known && !mode.Replaces
		switch {
		case !known, !defined && !mode.Creates:
			// a mode with nothing to act on does nothing
		case !defined:
			winner, defined, modeWins = Winner{Site: site, Merged: []Site{site}}, true, true
		case mode.Replaces || isInject && precedence.Injects == "replaces":
			winner.Site, winner.Merged, modeWins = site, []Site{site}, true
		default:
			winner.Merged = append(winner.Merged, site)
		}
	}
	if !defined {
		return Winner{}, false
	}
	switch {
	case len(ordered) == 1:
	case hasAdding && precedence.Injects == "unsettled":
		winner.Reason, winner.Cause = ReasonUnsettled, CauseInject
	case modeWins:
		winner.Reason = ReasonMode
	default:
		winner.Reason = lo.Ternary(precedence.KeepsEvery(), ReasonMerge, ReasonFirst)
	}
	return winner, true
}

// resolvePriority keeps the copy with the highest override priority. A tie is unsettled.
func (index *Index) resolvePriority(ordered []Site, precedence game.Precedence) Winner {
	priority := func(site Site) int16 { return index.Occurrence(site).Priority }
	top := slices.MaxFunc(ordered, func(a, b Site) int { return cmp.Compare(priority(a), priority(b)) })
	winner := Winner{Site: top, Reason: ReasonPriority}
	if lo.CountBy(ordered, func(site Site) bool { return priority(site) == priority(top) }) > 1 {
		winner.Reason, winner.Cause = ReasonUnsettled, CausePriority
	}
	if len(precedence.Overrides) > 0 {
		winner.Merged = ordered
	}
	return winner
}

// resolveReason returns why a winner wins when some copies lie in files the game does not read: replace, path or
// the winner's own reason.
func (index *Index) resolveReason(winner Winner, ordered []Site, precedence game.Precedence) Reason {
	copies := lo.Filter(ordered, func(site Site, _ int) bool {
		return index.IsLoaded(site.File) || index.isDropped(site.File)
	})
	if other, ok := index.resolveWinner(copies, precedence); ok && other.Site != winner.Site {
		return ReasonReplace
	}
	if other, ok := index.resolveWinner(ordered, precedence); ok && other.Site != winner.Site {
		return ReasonPath
	}
	return winner.Reason
}

// isReplaceOpen reports whether a later mod's replace_path may change which copy the game keeps. No source says
// whether a replace_path drops another mod's files.
func (index *Index) isReplaceOpen(loaded []Site, winner Winner, precedence game.Precedence) bool {
	rest := lo.Reject(loaded, func(site Site, _ int) bool { return index.isReplacedLater(site.File) })
	if len(rest) == len(loaded) {
		return false
	}
	other, ok := index.resolveWinner(rest, precedence)
	return !ok || other.Site != winner.Site
}

// isDropped reports whether a replace_path drops the file at a position: a game file in a folder that a mod's
// replace_path names.
func (index *Index) isDropped(position int32) bool {
	return index.Files[position].Origin == game.OriginVanilla && len(index.listReplacers(index.placements[position].key)) > 0
}

// isReplacedLater reports whether the file at a position is a mod's file in a folder that a later mod's replace_path
// names.
func (index *Index) isReplacedLater(position int32) bool {
	place := index.placements[position]
	return index.Files[position].Origin != game.OriginVanilla && slices.ContainsFunc(index.listReplacers(place.key),
		func(replacer int) bool { return replacer > place.root })
}

// listReplacers returns the positions among the roots of the mods whose replace_path names the folder of a path,
// given as its group's key. A replace_path names one folder exactly, not the folders below it. A mod a narrowed index
// does not answer for replaces nothing.
func (index *Index) listReplacers(key string) []int {
	folder := path.Dir(key)
	if folder == "." {
		folder = ""
	}
	var replacers []int
	for position, folders := range index.replacedKeys {
		if index.hasMod(index.Roots[position].Origin) && slices.Contains(folders, folder) {
			replacers = append(replacers, position)
		}
	}
	return replacers
}
