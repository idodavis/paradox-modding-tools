// keep.go decides which copy of a name the game keeps.

package lang

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
)

// Precedence is the rule for which copy the game keeps when several files
// define one name in a database.
type Precedence struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	// Keeps is which copy the game keeps.
	Keeps Keep

	// ByPlayset is whether the copies are ordered by their mods' place in the
	// playset, not by the names of their files.
	ByPlayset bool

	// Modes is where a definition writes an entry mode such as REPLACE:. It
	// is "" for a database that takes none.
	Modes ModePlace

	// PriorityKey is the key under which a definition writes its override
	// priority, as CK3's id_override_priority. It is "" for none.
	PriorityKey string

	// Overrides are the keys that the copy with the higher priority replaces,
	// the rest being combined. Nil means it replaces the whole definition.
	Overrides []string

	// Injection is what INJECT: does to a definition already loaded.
	Injection Injection

	// Contested are the blocks that two combined copies cannot both write.
	Contested []string
}

// FolderPrecedence is a rule that holds for every database below some
// folders, or whose name fits some pattern.
type FolderPrecedence struct {
	// Databases are the patterns a database's name may fit, as history/*.
	// Folders are the folders it holds for, each with every folder below it.
	Databases []string
	Folders   []string

	Precedence
}

// Keep is which copy of a name the game keeps.
type Keep string

// The copies the game can keep: the first one it reads, the last one, the
// one with the highest override priority, or every copy combined into one.
const (
	KeepFirst   Keep = "first"
	KeepLast    Keep = "last"
	KeepHighest Keep = "highest"
	KeepMerged  Keep = "merged"
)

// ModePlace is where definitions write their entry modes. The zero ModePlace
// is nowhere: the database takes no modes.
type ModePlace string

// The places an entry mode is written in.
const (
	// ModePlaceEntry is before the name at the top of a file, as in
	// REPLACE:x = { … }.
	ModePlaceEntry ModePlace = "entry"

	// ModePlaceWrapped is before the name inside the wrapping block, as in
	// accessory_genes = { INJECT_OR_CREATE:x = { … } }.
	ModePlaceWrapped ModePlace = "wrapped"
)

// Injection is what INJECT: does to a definition already loaded. The zero
// Injection adds to it.
type Injection string

// The injections that differ from adding.
const (
	// InjectionReplaces takes the loaded definition's place, as REPLACE: does.
	InjectionReplaces Injection = "replaces"

	// InjectionUnsettled is that no source says what INJECT: does.
	InjectionUnsettled Injection = "unsettled"
)

// EntryMode is a word written before a definition's name and a colon, as
// REPLACE is in REPLACE:x, with what it does to a definition already loaded.
type EntryMode struct {
	// Prefix is the word as written, such as INJECT or TRY_REPLACE.
	Prefix string

	// Replaces is whether the copy takes the place of the definition already
	// loaded. A mode that does not replace adds to it.
	Replaces bool

	// Optional is whether the copy does nothing where no definition is
	// loaded, as TRY_ says.
	Optional bool

	// Creates is whether the copy makes the definition where none is loaded,
	// as _OR_CREATE says.
	Creates bool
}

// Reason is why the game keeps one copy of a name, or that no source says
// which copy it keeps. Reasons gives each in words.
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

	// ReasonReplace and ReasonPath are a copy that wins because the one that
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
)

// Cause is what leaves the choice of a winner open.
type Cause string

// The causes of an open choice.
const (
	// CausePriority is copies tied for the highest override priority.
	CausePriority Cause = "priority"

	// CauseKeyword is a key the game may read as an entry mode, in a database
	// that takes none.
	CauseKeyword Cause = "keyword"

	// CauseInject is an INJECT: that no source settles.
	CauseInject Cause = "inject"

	// CauseReplace is a later mod's replace_path.
	CauseReplace Cause = "replace"
)

// Winner is the copy of a name the game keeps, by its place among the copies
// in load order, and why.
type Winner struct {
	// Copy is the copy the game keeps, or one in question where the reason is
	// unsettled.
	Copy int

	// Merged are the copies read into the one definition, the winner
	// included. It is empty where none merge.
	Merged []int

	// Reason is why the game keeps the copy, "" for a lone copy. Cause is
	// what leaves an unsettled choice open.
	Reason Reason
	Cause  Cause

	// Prefix is the entry mode's prefix that leaves the choice open, such as
	// INJECT. It is "" for any other cause.
	Prefix string
}

// Reasons holds each reason in words: a label, and for a winner's reason a
// text saying why it wins. The text is a template filled with {folder},
// {file}, {winner}, {beaten} and {beatenFile}.
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
}

// EntryModes are the modes a definition's name may start with, in the order
// they act (the Vic3 and EU5 wikis' Mod compatibility, Processing order).
var EntryModes = []EntryMode{
	{Prefix: "INJECT_OR_CREATE", Creates: true},
	{Prefix: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Prefix: "TRY_INJECT", Optional: true},
	{Prefix: "TRY_REPLACE", Replaces: true, Optional: true},
	{Prefix: "INJECT"},
	{Prefix: "REPLACE", Replaces: true},
}

// KeepsEvery reports whether the game merges every copy of a name.
func (p Precedence) KeepsEvery() bool { return p.Keeps == KeepMerged }

// FindWinner returns the copy the game keeps among the copies of one name.
// The copies are given in the order the game loads them, each by the entry
// mode it is written with ("" for none) and by its override priority. Every
// copy must be in a file the game reads. It returns false if there are no
// copies, or if every copy has an entry mode with nothing to act on.
func (p Precedence) FindWinner(modes []string, priorities []int) (Winner, bool) {
	last := len(modes) - 1
	firstMode := slices.IndexFunc(modes, func(mode string) bool { return mode != "" })
	switch {
	case len(modes) == 0:
		return Winner{}, false
	case p.Modes != "":
		return p.findModeWinner(modes)
	case firstMode >= 0:
		return Winner{Reason: ReasonUnsettled, Cause: CauseKeyword, Prefix: modes[firstMode]}, true
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

// findModeWinner returns the copy the game keeps in a database that takes
// entry modes. It returns false if no copy is left to act on and none
// creates one.
func (p Precedence) findModeWinner(modes []string) (Winner, bool) {
	var winner Winner

	for _, position := range listModeOrder(modes) {
		mode, hasMode := LookupEntryMode(modes[position])
		switch {
		case len(modes) == 1 && (!hasMode || mode.Creates):
			// A lone copy wins for no reason.
			winner.Merged = []int{position}

		case !hasMode && len(winner.Merged) == 0 && p.KeepsEvery():
			// The first plain copy stands; later ones merge into it.
			winner.Merged, winner.Reason = []int{position}, ReasonMerge

		case !hasMode && len(winner.Merged) == 0:
			// The first plain copy stands; later ones are not read.
			winner.Merged, winner.Reason = []int{position}, ReasonFirst

		case !hasMode && p.KeepsEvery():
			winner.Merged = append(winner.Merged, position)

		case !hasMode:
			// A later plain copy is not read.

		case len(winner.Merged) == 0 && !mode.Creates:
			// A mode with nothing to act on does nothing.

		case !mode.Replaces && p.Injection == InjectionUnsettled:
			// No source says what INJECT: does, so adding leaves the choice
			// open.
			winner.Merged = append(winner.Merged, position)
			winner.Reason, winner.Cause = ReasonUnsettled, CauseInject
			winner.Prefix = cmp.Or(winner.Prefix, modes[position])

		case len(winner.Merged) == 0:
			// A mode that creates makes the definition.
			winner.Merged, winner.Reason = []int{position}, ReasonMode

		case mode.Replaces && !mode.Creates && p.Injection == InjectionUnsettled && restsOnInject(modes):
			// The game may refuse every copy INJECT: made. This mode then has
			// nothing to act on, so it takes the standing copy's place and
			// the choice stays open.
			winner = Winner{Reason: ReasonUnsettled, Cause: CauseInject, Prefix: winner.Prefix, Merged: []int{position}}

		case mode.Replaces:
			// A mode that replaces takes the standing copy's place. It settles
			// a choice INJECT: left open: it creates a copy, or one stood
			// before it whatever INJECT: does.
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

	// Merged opens with the standing copy, so it wins.
	winner.Copy = winner.Merged[0]

	return winner, true
}

// listModeOrder returns the copies in the order the game applies them: plain
// copies in load order, then each entry mode's copies in the order of
// EntryModes. A copy written with a mode no game uses is left out.
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

// restsOnInject reports whether no copy stands whatever INJECT: does. Only a
// plain copy and a REPLACE_OR_CREATE: copy make the definition without it,
// and both act before TRY_REPLACE: and REPLACE:.
func restsOnInject(modes []string) bool {
	return !slices.ContainsFunc(modes, func(prefix string) bool {
		mode, ok := LookupEntryMode(prefix)

		return prefix == "" || ok && mode.Replaces && mode.Creates
	})
}

func listPositions(count int) []int {
	positions := make([]int, count)
	for position := range positions {
		positions[position] = position
	}

	return positions
}

// findHighest returns the first copy with the highest override priority. A
// tie is unsettled. Where a higher priority replaces only some keys, every
// copy is merged.
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

// FolderPrecedences holds how definitions combine in every database below a
// folder, or named by a pattern, where a database's declaration states
// nothing. The first row that holds wins. The Vic3 row cites the wiki's Mod
// compatibility page (1.13), and the EU5 rows the same page by line.
var FolderPrecedences = []FolderPrecedence{
	// dated history entries stack (CK3 _history.info:3-13), as
	// 01_admin_titles_tgp.txt:6339,6435 write c_nf_wang_738b_7 twice
	{Databases: []string{"history/*"}, Games: []string{game.CK3}, Keeps: KeepMerged},
	// the EU5 wiki's Setup modding, l.10: its managers are additive; Mod
	// structure, l.78
	{Databases: []string{"setup/*"}, Games: []string{game.EU5}, Keeps: KeepMerged},
	// the Vic3 wiki's list of folders: the first plain copy stays
	{
		Games: []string{game.Vic3}, Keeps: KeepFirst, Modes: ModePlaceEntry,
		Folders: []string{
			"common/acceptance_statuses", "common/achievements", "common/ai_strategies", "common/alert_groups",
			"common/alert_types", "common/amendments", "common/battle_conditions", "common/building_groups",
			"common/buildings", "common/buy_packages", "common/character_interactions", "common/character_templates",
			"common/character_traits", "common/cohesion_levels", "common/combat_unit_experience_levels",
			"common/combat_unit_groups", "common/combat_unit_types", "common/commander_orders", "common/commander_ranks",
			"common/company_charter_types", "common/company_types", "common/country_creation",
			"common/country_definitions", "common/country_formation", "common/country_ranks", "common/country_types",
			"common/culture_graphics", "common/cultures", "common/decisions", "common/decrees",
			"common/diplomatic_actions", "common/diplomatic_catalyst_categories", "common/diplomatic_catalysts",
			"common/diplomatic_plays", "common/discrimination_trait_groups", "common/discrimination_traits",
			"common/dna_data", "common/dynamic_company_names", "common/dynamic_country_map_colors",
			"common/dynamic_country_names", "common/dynamic_treaty_names", "common/ethnicities",
			"common/flag_definitions", "common/game_concepts", "common/genes", "common/geographic_regions",
			"common/goods", "common/government_types", "common/harvest_condition_types", "common/ideologies",
			"common/institutions", "common/interest_group_traits", "common/interest_groups", "common/journal_entries",
			"common/journal_entry_groups", "common/labels", "common/law_groups", "common/laws",
			"common/legitimacy_levels", "common/liberty_desire_levels", "common/military_formation_flags",
			"common/mobilization_option_groups", "common/mobilization_options", "common/modifier_type_definitions",
			"common/objective_subgoal_categories", "common/objective_subgoals", "common/objectives", "common/parties",
			"common/political_lobbies", "common/political_lobby_appeasement", "common/political_movement_categories",
			"common/political_movement_pop_support", "common/political_movements", "common/pop_needs",
			"common/pop_types", "common/power_bloc_coa_pieces", "common/power_bloc_identities",
			"common/power_bloc_map_textures", "common/power_bloc_names", "common/power_bloc_principle_groups",
			"common/power_bloc_principles", "common/prestige_goods", "common/production_method_groups",
			"common/production_methods", "common/proposal_types", "common/religions", "common/script_values",
			"common/scripted_buttons", "common/scripted_effects", "common/scripted_guis", "common/scripted_lists",
			"common/scripted_modifiers", "common/scripted_progress_bars", "common/scripted_rules",
			"common/scripted_triggers", "common/social_classes", "common/social_hierarchies", "common/state_traits",
			"common/static_modifiers", "common/strategic_regions", "common/subject_types", "common/technology",
			"common/terrain", "common/terrain_manipulators", "common/themes", "common/treaty_articles",
			"common/trigger_localization", "common/tutorial_lesson_chains", "common/tutorial_lessons",
			"common/war_goal_types", "gfx/map/army_dioramas", "gfx/map/city_data/city_building_vfx",
			"gfx/map/city_data/city_types", "gfx/map/fleet_dioramas", "gfx/map/fleet_entities", "gfx/map/front_entities",
			"gfx/portraits/accessories", "gfx/portraits/portrait_modifiers", "gui_animations", "modifier_icons", "music",
			"notifications", "sound/persistent_objects",
		},
	},
	// the EU5 wiki's Mod structure, l.78: modes work in the vast majority of
	// common's subfolders. Its exceptions, on_action and defines, hold their
	// own.
	{Folders: []string{"common"}, Games: []string{game.EU5}, Keeps: KeepFirst, Modes: ModePlaceEntry},
}

// LookupPrecedence returns how the definitions of a database combine: the
// first row of its declaration that holds for the game, else the first folder
// rule that holds. Where nothing holds, the last copy by file name wins.
func (d *Dialect) LookupPrecedence(declaration Declaration, database string) Precedence {
	for _, row := range declaration.Precedences {
		if d.Matches(row.Games) {
			return row
		}
	}

	for _, row := range FolderPrecedences {
		if d.Matches(row.Games) && row.holds(database, declaration.Folder) {
			return row.Precedence
		}
	}

	return Precedence{Keeps: KeepLast}
}

// holds reports whether the rule holds for a database: its name fits one of
// the rule's patterns, or its folder is one of the rule's folders or below
// one. A database with no folder, as a nested one, is tried by name only.
func (f FolderPrecedence) holds(database, folder string) bool {
	isNamed := slices.ContainsFunc(f.Databases, func(pattern string) bool {
		ok, _ := path.Match(pattern, database)

		return ok
	})

	return isNamed || (folder != "" && slices.ContainsFunc(f.Folders, func(root string) bool {
		return folder == root || strings.HasPrefix(folder, root+"/")
	}))
}

// Describe returns, in plain sentences, which copy the game keeps and what a
// mod can do about it.
func (p Precedence) Describe() string {
	var sentences []string

	switch {
	case p.KeepsEvery():
		sentences = append(sentences, "The game keeps every copy and combines them into one.")
		if len(p.Contested) > 0 {
			sentences = append(sentences, "Two copies cannot both write a "+strings.Join(p.Contested, " or ")+
				" block; the game does not say which one it keeps.")
		}

	case p.Keeps == KeepHighest:
		overrides := ""
		if len(p.Overrides) > 0 {
			overrides = " for " + strings.Join(p.Overrides, ", ") + ", and combines the rest"
		}

		sentences = append(sentences, "The game keeps the copy with the highest "+p.PriorityKey+overrides+
			". If two tie, it does not say which one it keeps.")

	case p.ByPlayset:
		sentences = append(sentences, "The game keeps the copy from the mod that loads last, so moving a mod in the load order "+
			"changes which one it is.")

	case p.Keeps == KeepFirst:
		sentences = append(sentences, "The game reads the files in a folder in order of file name and keeps the first copy it reads.")
		if p.Modes == "" {
			sentences = append(sentences, "A mod's copy wins when its file name sorts before the others.")
		}

	default:
		sentences = append(sentences, "The game reads the files in a folder in order of file name and keeps the last copy it reads.")
		if p.Modes == "" {
			sentences = append(sentences, "A mod's copy wins when its file name sorts after the others.")
		}
	}

	if p.Modes != "" {
		sentences = append(sentences, p.describeModes())
	}

	return strings.Join(sentences, " ")
}

func (p Precedence) describeModes() string {
	place := "in front of the name"
	if p.Modes == ModePlaceWrapped {
		place += " inside the block that holds it"
	}

	sentence := "A mod changes an entry by writing REPLACE: " + place + " to take its place"

	switch p.Injection {
	case InjectionReplaces:
		return sentence + "; INJECT: does the same here."
	case InjectionUnsettled:
		return sentence + "; the game does not say what INJECT: does here."
	}

	return sentence + ", or INJECT: to add to it."
}

// SplitMode splits an entry mode off a key as written: REPLACE:x gives the
// mode REPLACE and the name x. A key with no entry mode gives "" and the key
// itself, and so does every key in a game that takes no modes.
func (d *Dialect) SplitMode(key string) (mode, name string) {
	prefix, rest, cut := strings.Cut(key, ":")
	if _, ok := LookupEntryMode(prefix); cut && d.TakesModes && ok {
		return prefix, rest
	}

	return "", key
}

// LookupEntryMode returns the entry mode a prefix names, or false for "" or a
// prefix no game uses.
func LookupEntryMode(prefix string) (EntryMode, bool) {
	for _, mode := range EntryModes {
		if mode.Prefix == prefix {
			return mode, true
		}
	}

	return EntryMode{}, false
}
