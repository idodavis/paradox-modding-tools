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
)

// A Winner is the definition the game keeps when several share a name, and the reason it wins.
type Winner struct {
	Site   Site   `json:"site"`
	Reason string `json:"reason"` // order, mode, priority, merge, replace
	Merged []Site `json:"merged,omitempty"`
}

// FindWinner decides which of several definitions of one name the game keeps, by the database's precedence: the
// first or the last loaded, the highest override priority, the entry modes in the game's order, or all of them
// merged. It returns false if the game keeps none of them: each is dropped by a replace_path, or only entry modes
// with nothing to act on are written.
func (index *Index) FindWinner(sites []Site, precedence game.Precedence) (Winner, bool) {
	ordered := index.listLoadOrder(sites, precedence.ByPlayset)
	ideal, hasIdeal := index.resolveWinner(ordered, precedence)
	loaded := lo.Filter(ordered, func(site Site, _ int) bool { return index.IsLoaded(site.File) })
	if len(loaded) == len(ordered) {
		return ideal, hasIdeal
	}
	// A replace_path dropped some definitions, so the game decides among the rest, and the reason is the replace if
	// that changes which one it keeps.
	winner, ok := index.resolveWinner(loaded, precedence)
	if !ok {
		return Winner{}, false
	}
	if !hasIdeal || winner.Site != ideal.Site {
		winner.Reason = "replace"
	}
	return winner, true
}

// FindLoaded returns the file the game actually reads for a relative path: the last one in load order, except
// that a replace_path drops the earlier sources' files in that folder. It also returns the files the loaded one
// hides. It returns false if no file at that path is read; the files dropped are then the hidden ones.
func (index *Index) FindLoaded(rel string) (loaded files.File, hidden []files.File, ok bool) {
	row := game.LookupGame(index.Game)
	if row == nil {
		row = &game.Game{}
	}
	// The path is looked for under each source's root, as it is and under each stage folder, so that a path shared by
	// two stages is one path.
	_, rel = schema.SplitStage(row, rel)
	variants := []string{rel}
	for _, stage := range row.StageRoots {
		variants = append(variants, stage+"/"+rel)
	}
	replacer, _ := index.findReplacer(row, rel)
	var kept []files.File
	for source, root := range index.Roots {
		for _, variant := range variants {
			position, ok := index.FindFile(files.Join(root.Dir, variant))
			switch {
			case !ok:
			case source < replacer:
				hidden = append(hidden, index.Files[position])
			default:
				kept = append(kept, index.Files[position])
			}
		}
	}
	if len(kept) == 0 {
		return files.File{}, hidden, false
	}
	return kept[len(kept)-1], append(hidden, kept[:len(kept)-1]...), true
}

// IsLoaded reports whether the game reads a file at all.
func (index *Index) IsLoaded(file int32) bool {
	current := index.File(Site{File: file})
	loaded, _, ok := index.FindLoaded(current.Rel)
	return ok && loaded.Key == current.Key
}

// listLoadOrder returns sites in the order the game loads their definitions: by their files' load order, or by
// their files' roots where the playset decides, and within one file as written.
func (index *Index) listLoadOrder(sites []Site, byPlayset bool) []Site {
	// Each site's place is worked out once: its root's position, or its file's path without its stage folder.
	sources, rels := map[Site]int{}, map[Site]string{}
	if byPlayset {
		keys := listRootKeys(index.Roots)
		for _, site := range sites {
			sources[site], _ = findRoot(keys, index.File(site))
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

// findReplacer returns the position among the roots of the last mod root whose replace_path holds the folder of a
// relative path. The files of every root before it in that folder are dropped. It returns false if no root's does.
func (index *Index) findReplacer(row *game.Game, rel string) (int, bool) {
	folder := path.Dir(rel)
	if folder == "." {
		folder = ""
	}
	replacer, ok := 0, false
	for position, folders := range index.Replaced {
		if slices.ContainsFunc(folders, func(replaced string) bool {
			_, rest := schema.SplitStage(row, strings.Trim(replaced, "/"))
			return strings.EqualFold(rest, folder)
		}) {
			replacer, ok = position, true
		}
	}
	return replacer, ok
}

// resolveWinner decides among sites in load order, none of them dropped. It returns false if there are none, or if
// entry modes find nothing to act on.
func (index *Index) resolveWinner(ordered []Site, precedence game.Precedence) (Winner, bool) {
	if len(ordered) == 0 {
		return Winner{}, false
	}
	if precedence.Modes != "" {
		return index.resolveModes(ordered, precedence)
	}
	winner := Winner{Site: ordered[len(ordered)-1], Reason: "order"}
	switch {
	case precedence.Keeps == "first":
		winner.Site = ordered[0]
	case precedence.Keeps == "merged":
		winner.Site, winner.Merged = ordered[0], ordered
		if len(ordered) > 1 {
			winner.Reason = "merge"
		}
	case len(ordered) == 1:
	case precedence.Keeps == "highest":
		winner.Site = slices.MaxFunc(ordered, func(a, b Site) int {
			return cmp.Compare(index.Occurrence(a).Priority, index.Occurrence(b).Priority)
		})
		winner.Reason = "priority"
		if len(precedence.Overrides) > 0 {
			winner.Merged = ordered
		}
	}
	return winner, true
}

// resolveModes decides among the definitions of a database that takes entry modes. It returns false if there is no
// definition for the modes to act on and none of them creates one.
func (index *Index) resolveModes(ordered []Site, precedence game.Precedence) (Winner, bool) {
	// The first plain definition is the one the modes act on, each in the game's order of modes and then of files.
	// A mode that replaces takes its place, one that adds joins it, and one with nothing to act on does nothing
	// unless it creates.
	withModes, plain := lo.FilterReject(ordered, func(site Site, _ int) bool { return index.Occurrence(site).Mode != 0 })
	positions := map[Site]int{}
	withModes = slices.DeleteFunc(withModes, func(site Site) bool {
		position, known := index.findMode(site)
		positions[site] = position
		return !known
	})
	slices.SortStableFunc(withModes, func(a, b Site) int { return cmp.Compare(positions[a], positions[b]) })
	winner, defined := Winner{Reason: "order"}, len(plain) > 0
	if defined {
		winner.Site, winner.Merged = plain[0], []Site{plain[0]}
	}
	for _, site := range withModes {
		mode := game.EntryModes[positions[site]]
		// Where injecting acts as replacing, the mode that only injects replaces.
		replaces := mode.Replaces || precedence.ReplaceOnly && !mode.Optional && !mode.Creates
		switch {
		case !defined && mode.Creates:
			winner.Site, winner.Merged, defined = site, []Site{site}, true
		case !defined:
			continue
		case replaces:
			winner.Site, winner.Merged = site, []Site{site}
		default:
			winner.Merged = append(winner.Merged, site)
		}
		winner.Reason = "mode"
	}
	if !defined {
		return Winner{}, false
	}
	return winner, true
}

// findMode returns the position of a definition's entry mode among the game's modes, in the order they run. It
// returns false for a plain definition, which has none, and for a mode the game lacks.
func (index *Index) findMode(site Site) (int, bool) {
	prefix := index.Modes.Texts[index.Occurrence(site).Mode]
	position := slices.IndexFunc(game.EntryModes, func(mode game.EntryMode) bool { return mode.Prefix == prefix })
	return position, position >= 0
}

// compareLoadOrder orders two relative paths the way the game loads files: a folder's files come before its
// subfolders', and names are compared byte by byte, case included.
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
