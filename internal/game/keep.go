// keep.go decides which copy of a name the game keeps when several files
// define it, and names the reasons.

package game

import "slices"

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

// Cause is what leaves the choice of a winner open.
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

// Winner is the copy of a name that the game keeps, by its place among the
// copies in load order, and why.
type Winner struct {
	// Copy is the copy the game keeps. Where no source says which it keeps,
	// the reason is unsettled and this is one of the copies in question.
	Copy int

	// Merged are the copies the game reads into the one definition: the
	// winner and those merged into it. It is empty where none merge.
	Merged []int

	// Reason is why the game keeps the copy, "" for a lone copy. Cause is
	// what leaves an unsettled choice open.
	Reason Reason
	Cause  Cause
}

// FindWinner returns the copy the game keeps among the copies of one name,
// all in files the game reads. The copies are given in load order, each by
// the prefix of the entry mode it is written with, "" for none, and by its
// override priority. It returns false if there are no copies, or if entry
// modes find nothing to act on.
func (p Precedence) FindWinner(modes []string, priorities []int) (Winner, bool) {
	last := len(modes) - 1
	switch {
	case len(modes) == 0:
		return Winner{}, false
	case p.Modes != "":
		return p.findModeWinner(modes)
	case slices.ContainsFunc(modes, func(mode string) bool { return mode != "" }):
		return Winner{Reason: ReasonUnsettled, Cause: CauseKeyword}, true
	case len(modes) == 1:
		return Winner{}, true
	case p.Keeps == KeepFirst:
		return Winner{Reason: ReasonFirst}, true
	case p.KeepsEvery():
		return Winner{Reason: ReasonMerge, Merged: listPositions(len(modes))}, true
	case p.Keeps == KeepHighest:
		return p.findHighest(priorities), true
	case p.ByPlayset:
		return Winner{Copy: last, Reason: ReasonPlayset}, true
	}

	return Winner{Copy: last, Reason: ReasonLast}, true
}

// listPositions returns the positions of the given number of copies, in
// order.
func listPositions(count int) []int {
	positions := make([]int, count)
	for position := range positions {
		positions[position] = position
	}

	return positions
}

// findModeWinner returns the copy the game keeps in a database that takes
// entry modes. It returns false if no copy is left for the modes to act on
// and none of them creates one.
func (p Precedence) findModeWinner(modes []string) (Winner, bool) {
	var winner Winner

	for _, position := range listModeOrder(modes) {
		mode, hasMode := LookupEntryMode(modes[position])
		switch {
		case len(modes) == 1 && (!hasMode || mode.Creates):
			// A lone copy stands, and wins for no reason.
			winner.Merged = []int{position}

		case !hasMode && len(winner.Merged) == 0 && p.KeepsEvery():
			// The first plain copy stands, and the later ones merge into it.
			winner.Merged, winner.Reason = []int{position}, ReasonMerge

		case !hasMode && len(winner.Merged) == 0:
			// The first plain copy stands, and the later ones are not read.
			winner.Merged, winner.Reason = []int{position}, ReasonFirst

		case !hasMode && p.KeepsEvery():
			winner.Merged = append(winner.Merged, position)

		case !hasMode:
			// A later plain copy is not read.

		case len(winner.Merged) == 0 && !mode.Creates:
			// A mode with nothing to act on does nothing.

		case mode.Injects() && p.Injection == InjectionUnsettled:
			// No source says what INJECT: does here, so a copy that adds
			// leaves the choice open, whether it creates the definition or
			// joins it.
			winner.Merged = append(winner.Merged, position)
			winner.Reason, winner.Cause = ReasonUnsettled, CauseInject

		case len(winner.Merged) == 0:
			// A mode that creates makes the definition.
			winner.Merged, winner.Reason = []int{position}, ReasonMode

		case mode.Replaces && !mode.Creates && p.Injection == InjectionUnsettled && restsOnInject(modes):
			// Where no source says what INJECT: does, the game may refuse
			// every copy INJECT: made. With no plain copy and no
			// REPLACE_OR_CREATE: copy, this mode then has nothing to act on.
			// It takes the standing copy's place, and the choice stays open.
			winner = Winner{Reason: ReasonUnsettled, Cause: CauseInject, Merged: []int{position}}

		case mode.Replaces:
			// A mode that replaces takes the standing copy's place. It
			// settles a choice INJECT: left open, since it creates a copy, or
			// one stood before it whatever INJECT: does.
			winner = Winner{Reason: ReasonMode, Merged: []int{position}}

		case !mode.Optional && !mode.Creates && p.Injection == InjectionReplaces:
			// Where INJECT: acts as REPLACE:, a plain INJECT: takes the
			// standing copy's place.
			winner.Merged, winner.Reason = []int{position}, ReasonMode

		default:
			// A mode that adds joins the standing copy.
			winner.Merged = append(winner.Merged, position)
		}
	}

	if len(winner.Merged) == 0 {
		return Winner{}, false
	}

	// Merged opens with the standing copy, which the rest joined, so it wins.
	winner.Copy = winner.Merged[0]

	return winner, true
}

// listModeOrder returns the copies of a name in the order the game applies
// them: the plain copies in load order, then the copies of each entry mode in
// the order of EntryModes, each in load order (the Vic3 and EU5 wikis' Mod
// compatibility, Processing order). A copy written with a mode no game uses
// acts on nothing, so it is left out.
func listModeOrder(modes []string) []int {
	var order []int
	for position, mode := range modes {
		if mode == "" {
			order = append(order, position)
		}
	}

	for _, entry := range EntryModes {
		for position, mode := range modes {
			if mode == entry.Prefix {
				order = append(order, position)
			}
		}
	}

	return order
}

// restsOnInject reports whether no copy of a name stands whatever INJECT:
// does. Only a plain copy and one written with REPLACE_OR_CREATE: make the
// definition without INJECT:, and both act before TRY_REPLACE: and REPLACE:.
func restsOnInject(modes []string) bool {
	return !slices.ContainsFunc(modes, func(prefix string) bool {
		mode, ok := LookupEntryMode(prefix)

		return prefix == "" || ok && mode.Replaces && mode.Creates
	})
}

// findHighest returns the copy with the highest override priority, the first
// of them in load order. A tie is unsettled. Where a higher priority replaces
// only some keys, every copy is read into the definition.
func (p Precedence) findHighest(priorities []int) Winner {
	highest := slices.Max(priorities)
	winner := Winner{Copy: slices.Index(priorities, highest), Reason: ReasonPriority}

	ties := 0
	for _, priority := range priorities {
		if priority == highest {
			ties++
		}
	}

	if ties > 1 {
		winner.Reason, winner.Cause = ReasonUnsettled, CausePriority
	}

	if len(p.Overrides) > 0 {
		winner.Merged = listPositions(len(priorities))
	}

	return winner
}
