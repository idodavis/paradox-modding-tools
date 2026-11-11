// combine.go finds which definition the game keeps when several compete, by
// the game package's rule, and why a copy the game does not read is hidden.

package index

import (
	"cmp"
	"slices"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// Winner is the definition the game keeps when several share a name, and the
// reason it wins. Where the sources do not say which copy the game keeps, the
// reason is unsettled and the winner is one of the copies in question.
type Winner struct {
	Site Site `json:"site"`

	// Reason is why the game keeps the copy. It is "" for a lone copy.
	Reason game.Reason `json:"reason"`

	// Cause is what leaves an unsettled choice open. It is "" for a settled
	// one.
	Cause game.Cause `json:"cause,omitempty"`

	// Merged holds the copies the game reads into the one definition: the
	// winner and those merged into it. It may be empty where none merge.
	Merged []Site `json:"merged,omitempty"`
}

// Outcome is what the game does with one copy of a name: whether it keeps
// the copy, why, and what hides the copy's file where it does not read it.
type Outcome struct {
	// Kept is set for a copy the game keeps: the winner, or a copy merged
	// into it.
	Kept bool

	// Reason is why the game keeps the copy or does not read it, or that no
	// source says which copy it keeps. It is "" for a lone winner, and for a
	// copy the game reads but does not keep.
	Reason game.Reason

	// Hider is the origin of the mod that hides the copy's file. It is ""
	// where nothing hides the file.
	Hider string
}

// FindWinner returns the definition the game keeps among the given sites, all
// definitions of one name, by the database's precedence. It returns false if
// the game keeps none.
func (i *Index) FindWinner(sites []Site, precedence game.Precedence) (Winner, bool) {
	ordered := slices.DeleteFunc(i.listLoadOrder(sites, precedence), func(site Site) bool {
		return i.IsUnmounted(site.File)
	})
	loaded := slices.DeleteFunc(slices.Clone(ordered), func(site Site) bool { return !i.IsLoaded(site.File) })

	winner, ok := i.resolveWinner(loaded, precedence)
	if !ok || winner.Reason == game.ReasonUnsettled || precedence.KeepsEvery() {
		return winner, ok
	}

	if len(loaded) < len(ordered) {
		winner.Reason = i.resolveReason(winner, ordered, precedence)
	}

	if i.isReplaceOpen(loaded, winner, precedence) {
		winner.Reason, winner.Cause = game.ReasonUnsettled, game.CauseReplace
	}

	return winner, true
}

// listLoadOrder returns sites in the order the game loads their definitions:
// by their files' load order, or by their files' roots where the precedence
// says the playset decides, and within one file as written.
func (i *Index) listLoadOrder(sites []Site, precedence game.Precedence) []Site {
	return slices.SortedStableFunc(slices.Values(sites), func(first, second Site) int {
		if precedence.ByPlayset {
			return cmp.Or(i.CompareRoots(first.File, second.File), compareSites(first, second))
		}

		return cmp.Or(i.ComparePaths(first.File, second.File), compareSites(first, second))
	})
}

// resolveWinner returns the winner among copies in load order, all in files
// the game reads, as the precedence finds it. It returns false if there are
// none, or if entry modes find nothing to act on.
func (i *Index) resolveWinner(ordered []Site, precedence game.Precedence) (Winner, bool) {
	modes, priorities := make([]string, len(ordered)), make([]int, len(ordered))
	for position, site := range ordered {
		occurrence := i.Occurrence(site)
		modes[position], priorities[position] = i.Modes.Texts[occurrence.Mode], int(occurrence.Priority)
	}

	placed, ok := precedence.FindWinner(modes, priorities)
	if !ok {
		return Winner{}, false
	}

	winner := Winner{Site: ordered[placed.Copy], Reason: placed.Reason, Cause: placed.Cause}
	for _, position := range placed.Merged {
		winner.Merged = append(winner.Merged, ordered[position])
	}

	return winner, true
}

// resolveReason returns why a winner wins when some copies lie in files the
// game does not read: replace, path or the winner's own reason.
func (i *Index) resolveReason(winner Winner, ordered []Site, precedence game.Precedence) game.Reason {
	// The copies a replace_path drops are put back, to see whether they would
	// win.
	copies := slices.DeleteFunc(slices.Clone(ordered), func(site Site) bool {
		_, ok := i.FindReplacingMod(site.File)

		return !ok && !i.IsLoaded(site.File)
	})

	if other, ok := i.resolveWinner(copies, precedence); ok && other.Site != winner.Site {
		return game.ReasonReplace
	}

	if other, ok := i.resolveWinner(ordered, precedence); ok && other.Site != winner.Site {
		return game.ReasonPath
	}

	return winner.Reason
}

// isReplaceOpen reports whether a later mod's replace_path may change which
// copy the game keeps. No source says whether a replace_path drops another
// mod's files.
func (i *Index) isReplaceOpen(loaded []Site, winner Winner, precedence game.Precedence) bool {
	rest := slices.DeleteFunc(slices.Clone(loaded), func(site Site) bool { return i.IsReplacedLater(site.File) })
	if len(rest) == len(loaded) {
		return false
	}

	other, ok := i.resolveWinner(rest, precedence)

	return !ok || other.Site != winner.Site
}

// FindKept returns the definition the game keeps of a name in a database,
// local ones aside. It returns false if there is none.
func (i *Index) FindKept(schema *schema.Schema, reference schema.Reference) (Winner, bool) {
	return i.findKept(schema, reference, func(Site) bool { return false })
}

// FindKeptLocal is FindKept for a reference written in a file: that file's
// local definitions count too. A file the index does not list has the
// position -1, which counts none.
func (i *Index) FindKeptLocal(schema *schema.Schema, reference schema.Reference, file int32) (Winner, bool) {
	return i.findKept(schema, reference, func(site Site) bool { return site.File == file })
}

// findKept returns the definition the game keeps of a name in a database. A
// local definition counts only if the given test accepts its site. It returns
// false if the schema has no such database.
func (i *Index) findKept(schema *schema.Schema, reference schema.Reference, isVisible func(Site) bool) (Winner, bool) {
	database, ok := schema.Databases[reference.Database]
	if !ok {
		return Winner{}, false
	}

	return i.FindWinner(i.listDefining(database.Name, reference.Name, isVisible), database.Precedence)
}

// ListKept returns the definition the game keeps of each name in a database,
// one for each name, in load order. A local definition never counts. It is
// empty if the schema has no such database.
func (i *Index) ListKept(schema *schema.Schema, database string) []Winner {
	var kept []Winner
	listed := map[string]bool{}

	for _, site := range i.ListDefinitions(database) {
		reference := i.ToReference(site)
		if listed[reference.Name] {
			continue
		}

		listed[reference.Name] = true
		if winner, ok := i.FindKept(schema, reference); ok {
			kept = append(kept, winner)
		}
	}

	return kept
}

// ResolveSite returns what the game does with a copy of a winner's name:
// whether it keeps the copy, why, and the mod that hides the copy's file if
// the game does not read it. An unsettled winner keeps none.
func (i *Index) ResolveSite(winner Winner, site Site) Outcome {
	unsettled := winner.Reason == game.ReasonUnsettled
	switch {
	case !unsettled && site == winner.Site:
		return Outcome{Kept: true, Reason: winner.Reason}

	case !unsettled && slices.Contains(winner.Merged, site) && i.Occurrence(site).Mode != 0:
		return Outcome{Kept: true, Reason: game.ReasonInjected}

	case !unsettled && slices.Contains(winner.Merged, site):
		return Outcome{Kept: true, Reason: game.ReasonCombined}

	case !i.IsLoaded(site.File):
		reason, hider := i.FindHider(site.File)

		return Outcome{Reason: reason, Hider: hider}

	case unsettled:
		return Outcome{Reason: game.ReasonUnsettled}
	}

	return Outcome{}
}

// FindHider returns why the game does not read a file, dropped or shadowed,
// and the origin of the mod that hides it. It returns "" and "" for a file
// nothing hides.
func (i *Index) FindHider(file int32) (game.Reason, string) {
	current := i.Files[file]
	loaded, hidden, _ := i.FindLoaded(current.Rel)
	if !slices.ContainsFunc(hidden, func(other files.File) bool { return other.Key == current.Key }) {
		return "", ""
	}

	if mod, ok := i.FindReplacingMod(file); ok {
		return game.ReasonDropped, mod.Origin
	}

	return game.ReasonShadowed, loaded.Origin
}
