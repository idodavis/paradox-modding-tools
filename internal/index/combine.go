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
	Site Site `json:"site"`
	// Reason is last, first, playset, path, priority, mode, merge, replace or unsettled; "" for a lone copy. A
	// localization key's winner is replace-folder, unsettled or "".
	Reason string `json:"reason"`
	// Cause is what leaves an unsettled choice open: priority, keyword, inject or replace; else "".
	Cause  string `json:"cause,omitempty"`
	Merged []Site `json:"merged,omitempty"`
}

// The reasons a copy other than the winner can have.
const (
	Injected  = "injected"
	Combined  = "combined"
	Unsettled = "unsettled"
)

// IsKept reports whether the game keeps a definition of the name this winner won: the winner itself, or one merged
// into it.
func (winner Winner) IsKept(site Site) bool {
	return winner.Site == site || slices.Contains(winner.Merged, site)
}

// ResolveSite reports whether the game keeps a copy of a winner's name, and why. If the winner is unsettled, no copy
// is kept, and a copy in a file the game reads has the reason unsettled.
func (index *Index) ResolveSite(winner Winner, site Site) (bool, string) {
	switch {
	case winner.Reason == Unsettled:
		return false, lo.Ternary(index.IsLoaded(site.File), Unsettled, "")
	case site == winner.Site:
		return true, winner.Reason
	case slices.Contains(winner.Merged, site):
		return true, lo.Ternary(index.Occurrence(site).Mode != 0, Injected, Combined)
	}
	return false, ""
}

// FindWinner returns the definition of a name that the game keeps, by the database's precedence. It returns false if
// the game keeps none.
func (index *Index) FindWinner(sites []Site, precedence game.Precedence) (Winner, bool) {
	row := game.LookupGame(index.Game)
	ordered := lo.Reject(index.listLoadOrder(sites, precedence.ByPlayset), func(site Site, _ int) bool {
		return IsUnmounted(row, index.File(site))
	})
	loaded := lo.Filter(ordered, func(site Site, _ int) bool { return index.IsLoaded(site.File) })
	winner, ok := index.resolveWinner(loaded, precedence)
	if !ok || winner.Reason == Unsettled || precedence.KeepsEvery() {
		return winner, ok
	}
	if len(loaded) < len(ordered) {
		winner.Reason = index.resolveReason(row, winner, ordered, precedence)
	}
	if index.isReplaceOpen(row, loaded, winner, precedence) {
		winner.Reason, winner.Cause = Unsettled, "replace"
	}
	return winner, true
}

// FindKept returns the definition the game keeps of a name in a database, local ones aside. It returns false if there
// is none.
func (index *Index) FindKept(schema *schema.Schema, reference schema.Reference) (Winner, bool) {
	return index.findKept(schema, reference, func(Site) bool { return false })
}

// FindKeptLocal is FindKept for a reference written in a file: that file's local definitions count too.
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
	row := game.LookupGame(index.Game)
	folder = strings.ToLower(folder)
	var images []files.File
	for position, file := range index.Files {
		if file.Format == files.FormatImage && strings.HasPrefix(toRelKey(row, file.Rel), folder) &&
			index.IsLoaded(int32(position)) {
			images = append(images, file)
		}
	}
	return images
}

// FindLoaded returns the file the game reads at a relative path, and the files it hides. It returns false if the game
// reads none.
func (index *Index) FindLoaded(rel string) (loaded files.File, hidden []files.File, ok bool) {
	row := game.LookupGame(index.Game)
	var kept []files.File
	for _, position := range index.byRel[toRelKey(row, rel)] {
		file := index.Files[position]
		switch {
		case !index.IsListed(position), IsUnmounted(row, file), !index.hasFile(position):
		case index.isDropped(row, file):
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
	_, rest := schema.SplitStage(row, strings.ToLower(rel))
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
	// Each site's place is worked out once: its root's position, or its file's path without its stage folder.
	sources, rels := map[Site]int{}, map[Site]string{}
	if byPlayset {
		for _, site := range sites {
			sources[site], _ = findRoot(index.rootKeys, index.File(site).Key)
		}
	} else {
		row := game.LookupGame(index.Game)
		for _, site := range sites {
			_, rels[site] = schema.SplitStage(row, index.File(site).Rel)
		}
	}
	return slices.SortedStableFunc(slices.Values(sites), func(a, b Site) int {
		if byPlayset {
			return cmp.Or(cmp.Compare(sources[a], sources[b]), compareSites(a, b))
		}
		return cmp.Or(compareLoadOrder(rels[a], rels[b]), compareSites(a, b))
	})
}

// compareLoadOrder orders two relative paths the way the game loads files, a folder's files before its subfolders'.
func compareLoadOrder(a, b string) int {
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

// IsUnmounted reports whether the game never mounts a file: a mod's file outside the stage folders, in a game that
// keeps its files under stages (EU5 debug.log:80-83, 5392; the wiki's Mod structure, l.46-48).
func IsUnmounted(row *game.Game, file files.File) bool {
	stage, _ := schema.SplitStage(row, strings.ToLower(file.Rel))
	return row != nil && len(row.StageRoots) > 0 && file.Origin != game.OriginVanilla && stage == ""
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
		return Winner{Site: ordered[0], Reason: Unsettled, Cause: "keyword"}, true
	case len(ordered) == 1:
		return Winner{Site: ordered[0]}, true
	case precedence.Keeps == "first":
		return Winner{Site: ordered[0], Reason: "first"}, true
	case precedence.KeepsEvery():
		return Winner{Site: ordered[0], Reason: "merge", Merged: ordered}, true
	case precedence.Keeps == "highest":
		return index.resolvePriority(ordered, precedence), true
	case precedence.ByPlayset:
		return Winner{Site: ordered[last], Reason: "playset"}, true
	}
	return Winner{Site: ordered[last], Reason: "last"}, true
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
	case hasAdding && precedence.Injects == Unsettled:
		winner.Reason, winner.Cause = Unsettled, "inject"
	case modeWins:
		winner.Reason = "mode"
	default:
		winner.Reason = lo.Ternary(precedence.KeepsEvery(), "merge", "first")
	}
	return winner, true
}

// resolvePriority keeps the copy with the highest override priority. A tie is unsettled.
func (index *Index) resolvePriority(ordered []Site, precedence game.Precedence) Winner {
	priority := func(site Site) int16 { return index.Occurrence(site).Priority }
	top := slices.MaxFunc(ordered, func(a, b Site) int { return cmp.Compare(priority(a), priority(b)) })
	winner := Winner{Site: top, Reason: "priority"}
	if lo.CountBy(ordered, func(site Site) bool { return priority(site) == priority(top) }) > 1 {
		winner.Reason, winner.Cause = Unsettled, "priority"
	}
	if len(precedence.Overrides) > 0 {
		winner.Merged = ordered
	}
	return winner
}

// resolveReason returns why a winner wins when some copies lie in files the game does not read: replace, path or
// the winner's own reason.
func (index *Index) resolveReason(row *game.Game, winner Winner, ordered []Site, precedence game.Precedence) string {
	copies := lo.Filter(ordered, func(site Site, _ int) bool {
		return index.IsLoaded(site.File) || index.isDropped(row, index.File(site))
	})
	if other, ok := index.resolveWinner(copies, precedence); ok && other.Site != winner.Site {
		return "replace"
	}
	if other, ok := index.resolveWinner(ordered, precedence); ok && other.Site != winner.Site {
		return "path"
	}
	return winner.Reason
}

// isReplaceOpen reports whether a later mod's replace_path may change which copy the game keeps. No source says
// whether a replace_path drops another mod's files.
func (index *Index) isReplaceOpen(row *game.Game, loaded []Site, winner Winner, precedence game.Precedence) bool {
	rest := lo.Reject(loaded, func(site Site, _ int) bool { return index.isReplacedLater(row, index.File(site)) })
	if len(rest) == len(loaded) {
		return false
	}
	other, ok := index.resolveWinner(rest, precedence)
	return !ok || other.Site != winner.Site
}

// isDropped reports whether a replace_path drops a file: a game file in a folder that a mod's replace_path names.
func (index *Index) isDropped(row *game.Game, file files.File) bool {
	return file.Origin == game.OriginVanilla && len(index.listReplacers(row, toRelKey(row, file.Rel))) > 0
}

// isReplacedLater reports whether a mod's file sits in a folder that a later mod's replace_path names.
func (index *Index) isReplacedLater(row *game.Game, file files.File) bool {
	source, _ := findRoot(index.rootKeys, file.Key)
	return file.Origin != game.OriginVanilla && slices.ContainsFunc(index.listReplacers(row, toRelKey(row, file.Rel)),
		func(replacer int) bool { return replacer > source })
}

// listReplacers returns the positions among the roots of the mods whose replace_path names the folder of a path,
// given as its group's key. A replace_path names one folder exactly, not the folders below it. A mod a narrowed index
// does not answer for replaces nothing.
func (index *Index) listReplacers(row *game.Game, key string) []int {
	folder := path.Dir(key)
	if folder == "." {
		folder = ""
	}
	var replacers []int
	for position, folders := range index.Replaced {
		if index.hasMod(index.Roots[position].Origin) && slices.ContainsFunc(folders, func(replaced string) bool {
			_, rest := schema.SplitStage(row, strings.Trim(replaced, "/"))
			return strings.EqualFold(rest, folder)
		}) {
			replacers = append(replacers, position)
		}
	}
	return replacers
}
