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

// A Winner is the definition the game keeps when several share a name, and the reason it wins.
type Winner struct {
	Site   Site   `json:"site"`
	Reason string `json:"reason"` // order, mode, priority, merge, replace
	Merged []Site `json:"merged,omitempty"`
}

// IsKept reports whether the game keeps a definition of the name this winner won: the winner itself, or one merged
// into it.
func (winner Winner) IsKept(site Site) bool {
	return winner.Site == site || slices.Contains(winner.Merged, site)
}

// FindWinner decides which of several definitions of one name the game keeps, by the database's precedence: the
// first or the last loaded, the highest override priority, the entry modes in load order, or all of them merged. It
// returns false if the game keeps none of them: each is dropped by a replace_path, or only entry modes with nothing
// to act on are written.
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

// FindKept returns the definition the game keeps of a name in a database, with the definitions merged into it. A
// local definition never counts. It returns false if the schema has no such database, nothing defines the name, or
// the game keeps none of its definitions.
func (index *Index) FindKept(schema *schema.Schema, reference schema.Reference) (Winner, bool) {
	return index.findKept(schema, reference, func(Site) bool { return false })
}

// FindKeptLocal is FindKept for a reference written in the given file: the local definitions of that file count too.
func (index *Index) FindKeptLocal(schema *schema.Schema, reference schema.Reference, file int32) (Winner, bool) {
	return index.findKept(schema, reference, func(site Site) bool { return site.File == file })
}

// findKept returns the definition the game keeps of a name in a database. A local definition counts where the given
// function says it does.
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

// ListImages returns the image files the game reads whose path, less its stage folder, starts with a folder, at any
// depth below it. The folder is written with its closing slash, and "" gives every image. Letter case does not
// matter.
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

// FindLoaded returns the file the game actually reads for a relative path: the last one in load order, except
// that a replace_path drops the earlier sources' files in that folder. It also returns the files the loaded one
// hides. It returns false if no file at that path is read; the files dropped are then the hidden ones.
func (index *Index) FindLoaded(rel string) (loaded files.File, hidden []files.File, ok bool) {
	row := game.LookupGame(index.Game)
	key := toRelKey(row, rel)
	replacer, _ := index.findReplacer(row, key)
	var kept []files.File
	for _, position := range index.byRel[key] {
		file := index.Files[position]
		source, _ := findRoot(index.rootKeys, file.Key)
		switch {
		case !index.IsListed(position):
		case source < replacer:
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

// resolveModes decides among the definitions of a database that takes entry modes, in load order. A plain definition
// replaces what came before it. An entry mode acts on what came before it: one that replaces takes its place, one that
// adds joins it, and one with nothing to act on does nothing unless it creates. It returns false if there is no
// definition for the modes to act on and none of them creates one.
func (index *Index) resolveModes(ordered []Site, precedence game.Precedence) (Winner, bool) {
	var winner Winner
	defined := false
	for _, site := range ordered {
		if index.Occurrence(site).Mode == 0 {
			winner, defined = Winner{Site: site, Merged: []Site{site}, Reason: "order"}, true
			continue
		}
		position, known := index.findMode(site)
		if !known {
			continue
		}
		mode := game.EntryModes[position]
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
