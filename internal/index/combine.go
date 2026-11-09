// combine.go decides which definition the game keeps when several compete,
// and why a copy the game does not read is hidden.

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
	Reason Reason `json:"reason"`

	// Cause is what leaves an unsettled choice open. It is "" for a settled
	// one.
	Cause Cause `json:"cause,omitempty"`

	// Merged holds the copies the game reads into the one definition: the
	// winner and those merged into it. It may be empty where none merge.
	Merged []Site `json:"merged,omitempty"`
}

// Reason is why the game keeps a copy of a name, why it does not read a copy,
// or that no source says which copy it keeps.
type Reason string

// The reasons a copy has.
const (
	// ReasonLast and ReasonFirst are the last and the first copy by file
	// name.
	ReasonLast  Reason = "last"
	ReasonFirst Reason = "first"

	// ReasonPlayset is the copy of the mod lowest in the load order.
	ReasonPlayset Reason = "playset"

	// ReasonPriority is the copy with the highest override priority.
	ReasonPriority Reason = "priority"

	// ReasonMode is a copy whose entry mode creates or replaces the
	// definition.
	ReasonMode Reason = "mode"

	// ReasonMerge is the first of copies merged into one.
	ReasonMerge Reason = "merge"

	// ReasonReplace and ReasonPath are a copy that wins because the copy that
	// would is in a file the game does not read: a mod's replace_path drops
	// it, or a file at the same path hides it.
	ReasonReplace Reason = "replace"
	ReasonPath    Reason = "path"

	// ReasonReplaceFolder is a localization key in a replace folder.
	ReasonReplaceFolder Reason = "replace-folder"

	// ReasonInjected and ReasonCombined are copies merged into the winner, by
	// an entry mode or by their database.
	ReasonInjected Reason = "injected"
	ReasonCombined Reason = "combined"

	// ReasonUnsettled is that no source says which copy the game keeps.
	ReasonUnsettled Reason = "unsettled"

	// ReasonShadowed and ReasonDropped are a copy in a file the game does not
	// read: a file at the same path later in the load order replaces it, or a
	// mod's replace_path drops its folder.
	ReasonShadowed Reason = "shadowed"
	ReasonDropped  Reason = "dropped"
)

// Reasons holds each reason in words: a label, and for a reason a contest's
// winner can have, a longer text saying why it wins. That text is a template
// filled with {folder}, {file}, {winner}, {beaten} and {beatenFile}. The label
// of a copy the game does not read takes {mod}, the mod that hides it.
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

// Cause is what leaves the choice of a kept copy open.
type Cause string

// The causes of an open choice.
const (
	// CausePriority is copies that share the highest override priority.
	CausePriority Cause = "priority"

	// CauseKeyword is a key the game may read as an entry mode, in a database
	// that takes none.
	CauseKeyword Cause = "keyword"

	// CauseInject is an INJECT: that no source settles.
	CauseInject Cause = "inject"

	// CauseReplace is a later mod's replace_path.
	CauseReplace Cause = "replace"
)

// Outcome is what the game does with one copy of a name: whether it keeps
// the copy, why, and what hides the copy's file where it does not read it.
type Outcome struct {
	// Kept is set for a copy the game keeps: the winner, or a copy merged
	// into it.
	Kept bool

	// Reason is why the game keeps the copy or does not read it, or that no
	// source says which copy it keeps. It is "" for a lone winner, and for a
	// copy the game reads but does not keep.
	Reason Reason

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
	if !ok || winner.Reason == ReasonUnsettled || precedence.KeepsEvery() {
		return winner, ok
	}

	if len(loaded) < len(ordered) {
		winner.Reason = i.resolveReason(winner, ordered, precedence)
	}

	if i.isReplaceOpen(loaded, winner, precedence) {
		winner.Reason, winner.Cause = ReasonUnsettled, CauseReplace
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
// the game reads. It returns false if there are none, or if entry modes find
// nothing to act on.
func (i *Index) resolveWinner(ordered []Site, precedence game.Precedence) (Winner, bool) {
	last := len(ordered) - 1
	switch {
	case len(ordered) == 0:
		return Winner{}, false
	case precedence.Modes != "":
		return i.resolveModes(ordered, precedence)
	case slices.ContainsFunc(ordered, func(site Site) bool { return i.Occurrence(site).Mode != 0 }):
		return Winner{Site: ordered[0], Reason: ReasonUnsettled, Cause: CauseKeyword}, true
	case len(ordered) == 1:
		return Winner{Site: ordered[0]}, true
	case precedence.Keeps == game.KeepFirst:
		return Winner{Site: ordered[0], Reason: ReasonFirst}, true
	case precedence.KeepsEvery():
		return Winner{Site: ordered[0], Reason: ReasonMerge, Merged: ordered}, true
	case precedence.Keeps == game.KeepHighest:
		return i.resolvePriority(ordered, precedence), true
	case precedence.ByPlayset:
		return Winner{Site: ordered[last], Reason: ReasonPlayset}, true
	}

	return Winner{Site: ordered[last], Reason: ReasonLast}, true
}

// resolveModes returns the winner among the copies of a name in a database
// that takes entry modes, in load order. It returns false if no copy is left
// for the modes to act on and none of them creates one.
func (i *Index) resolveModes(ordered []Site, precedence game.Precedence) (Winner, bool) {
	var winner Winner

	for _, site := range i.listModeOrder(ordered) {
		mode, hasMode := i.FindMode(site)
		switch {
		case len(ordered) == 1 && (!hasMode || mode.Creates):
			// A lone copy stands, and wins for no reason.
			winner.Merged = []Site{site}

		case !hasMode && len(winner.Merged) == 0 && precedence.KeepsEvery():
			// The first plain copy stands, and the later ones merge into it.
			winner.Merged, winner.Reason = []Site{site}, ReasonMerge

		case !hasMode && len(winner.Merged) == 0:
			// The first plain copy stands, and the later ones are not read.
			winner.Merged, winner.Reason = []Site{site}, ReasonFirst

		case !hasMode && precedence.KeepsEvery():
			winner.Merged = append(winner.Merged, site)

		case !hasMode:
			// A later plain copy is not read.

		case len(winner.Merged) == 0 && !mode.Creates:
			// A mode with nothing to act on does nothing.

		case mode.Injects() && precedence.Injection == game.InjectionUnsettled:
			// No source says what INJECT: does here, so a copy that adds
			// leaves the choice open, whether it creates the definition or
			// joins it.
			winner.Merged = append(winner.Merged, site)
			winner.Reason, winner.Cause = ReasonUnsettled, CauseInject

		case len(winner.Merged) == 0:
			// A mode that creates makes the definition.
			winner.Merged, winner.Reason = []Site{site}, ReasonMode

		case mode.Replaces && !mode.Creates && precedence.Injection == game.InjectionUnsettled &&
			i.restsOnInject(ordered):
			// Where no source says what INJECT: does, the game may refuse
			// every copy INJECT: made. With no plain copy and no
			// REPLACE_OR_CREATE: copy, this mode then has nothing to act on.
			// It takes the standing copy's place, and the choice stays open.
			winner = Winner{Reason: ReasonUnsettled, Cause: CauseInject, Merged: []Site{site}}

		case mode.Replaces:
			// A mode that replaces takes the standing copy's place. It
			// settles a choice INJECT: left open, since it creates a copy, or
			// one stood before it whatever INJECT: does.
			winner = Winner{Reason: ReasonMode, Merged: []Site{site}}

		case !mode.Optional && !mode.Creates && precedence.Injection == game.InjectionReplaces:
			// Where INJECT: acts as REPLACE:, a plain INJECT: takes the
			// standing copy's place.
			winner.Merged, winner.Reason = []Site{site}, ReasonMode

		default:
			// A mode that adds joins the standing copy.
			winner.Merged = append(winner.Merged, site)
		}
	}

	if len(winner.Merged) == 0 {
		return Winner{}, false
	}

	// Merged opens with the standing copy, which the rest joined, so it wins.
	winner.Site = winner.Merged[0]

	return winner, true
}

// listModeOrder returns the copies of a name in the order the game applies
// them: the plain copies in load order, then the copies of each entry mode in
// the order of game.EntryModes, each in load order (the Vic3 and EU5 wikis'
// Mod compatibility, Processing order). A copy written with a mode no game
// uses acts on nothing, so it is left out.
func (i *Index) listModeOrder(ordered []Site) []Site {
	order := slices.DeleteFunc(slices.Clone(ordered), func(site Site) bool { return i.Occurrence(site).Mode != 0 })

	for _, mode := range game.EntryModes {
		for _, site := range ordered {
			if i.Modes.Texts[i.Occurrence(site).Mode] == mode.Prefix {
				order = append(order, site)
			}
		}
	}

	return order
}

// restsOnInject reports whether no copy of a name stands whatever INJECT:
// does. Only a plain copy and one written with REPLACE_OR_CREATE: make the
// definition without INJECT:, and both act before TRY_REPLACE: and REPLACE:.
func (i *Index) restsOnInject(ordered []Site) bool {
	return !slices.ContainsFunc(ordered, func(site Site) bool {
		if i.Occurrence(site).Mode == 0 {
			return true
		}

		mode, ok := i.FindMode(site)

		return ok && mode.Replaces && mode.Creates
	})
}

// resolvePriority keeps the copy with the highest override priority, the
// first of them in load order. A tie is unsettled.
func (i *Index) resolvePriority(ordered []Site, precedence game.Precedence) Winner {
	priority := func(site Site) int16 { return i.Occurrence(site).Priority }
	top := slices.MaxFunc(ordered, func(first, second Site) int { return cmp.Compare(priority(first), priority(second)) })
	winner := Winner{Site: top, Reason: ReasonPriority}

	ties := 0
	for _, site := range ordered {
		if priority(site) == priority(top) {
			ties++
		}
	}

	if ties > 1 {
		winner.Reason, winner.Cause = ReasonUnsettled, CausePriority
	}

	// Where a higher priority replaces only some keys, every copy is read
	// into the definition.
	if len(precedence.Overrides) > 0 {
		winner.Merged = ordered
	}

	return winner
}

// resolveReason returns why a winner wins when some copies lie in files the
// game does not read: replace, path or the winner's own reason.
func (i *Index) resolveReason(winner Winner, ordered []Site, precedence game.Precedence) Reason {
	// The copies a replace_path drops are put back, to see whether they would
	// win.
	copies := slices.DeleteFunc(slices.Clone(ordered), func(site Site) bool {
		_, ok := i.FindReplacingMod(site.File)

		return !ok && !i.IsLoaded(site.File)
	})

	if other, ok := i.resolveWinner(copies, precedence); ok && other.Site != winner.Site {
		return ReasonReplace
	}

	if other, ok := i.resolveWinner(ordered, precedence); ok && other.Site != winner.Site {
		return ReasonPath
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
	unsettled := winner.Reason == ReasonUnsettled
	switch {
	case !unsettled && site == winner.Site:
		return Outcome{Kept: true, Reason: winner.Reason}

	case !unsettled && slices.Contains(winner.Merged, site) && i.Occurrence(site).Mode != 0:
		return Outcome{Kept: true, Reason: ReasonInjected}

	case !unsettled && slices.Contains(winner.Merged, site):
		return Outcome{Kept: true, Reason: ReasonCombined}

	case !i.IsLoaded(site.File):
		reason, hider := i.FindHider(site.File)

		return Outcome{Reason: reason, Hider: hider}

	case unsettled:
		return Outcome{Reason: ReasonUnsettled}
	}

	return Outcome{}
}

// FindHider returns why the game does not read a file, dropped or shadowed,
// and the origin of the mod that hides it. It returns "" and "" for a file
// nothing hides.
func (i *Index) FindHider(file int32) (Reason, string) {
	current := i.Files[file]
	loaded, hidden, _ := i.FindLoaded(current.Rel)
	if !slices.ContainsFunc(hidden, func(other files.File) bool { return other.Key == current.Key }) {
		return "", ""
	}

	if mod, ok := i.FindReplacingMod(file); ok {
		return ReasonDropped, mod.Origin
	}

	return ReasonShadowed, loaded.Origin
}
