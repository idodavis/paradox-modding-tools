package derived

import "paradox-modding-tools/services/internal/lang/loc"

// FloorUnit says how to read a floor's Value.
type FloorUnit string

const (
	// UnitCount is an absolute number of definitions, citations or hits.
	UnitCount FloorUnit = "count"
	// UnitPercent is a share written 0-100.
	UnitPercent FloorUnit = "percent"
	// UnitRatio is a share written 0-1.
	UnitRatio FloorUnit = "ratio"
	// UnitCap is a budget: an upper bound on output or work, not evidence.
	UnitCap FloorUnit = "cap"
)

// Floor is one threshold, with what it guards and what set it.
type Floor struct {
	Name  string
	Value float64
	Unit  FloorUnit
	// Guards is what goes wrong without it.
	Guards   string
	Evidence string
}

// Floors is every threshold in this package. Kept in one list so a reader can
// see the whole calibration at once, which is the thing that was impossible.
var Floors = []Floor{
	// --- binding declared types to harvested databases (schema.go)
	{"minBindCites", minBindCites, UnitCount,
		"binding a declared type to a database on a coincidental key match",
		"Refuted as a filter, kept as a floor: all 18 name-matched bindings across the three installs are real folders (I.2g)."},
	{"bindFloor", bindFloor, UnitRatio,
		"a folder claiming a type it explains almost none of", ""},
	{"bindShare", bindShare, UnitRatio,
		"an incidentally co-keyed folder counting as one of a type's homes", ""},
	{"bindSaturated", bindSaturated, UnitRatio,
		"searching a tail of folders that each explain a citation or two", ""},

	// --- typing a field from the definitions its values name (fields.go)
	{"minFieldHits", minFieldHits, UnitCount,
		"one coincidence typing a field",
		"240 CK3, 54 Vic3 and 2,717 EU5 fields were typed on a single hit and every sampled one was junk; `body_part = torso` alone produced 859 false dangling refs."},
	{"fieldKindCoverage", fieldKindCoverage, UnitPercent,
		"a field typed from a minority of its values",
		"`body_part` takes exactly two values in CK3 and one of them collides, so 1 of 2 cleared 50% exactly — the ratio needed a floor beside it, not a higher ratio."},
	{"minLocFieldHits", minLocFieldHits, UnitCount,
		"a field called localization on one coincidental key match", ""},
	{"locFieldCoverage", locFieldCoverage, UnitPercent,
		"a field that occasionally collides with a loc key being read as holding one",
		"Set above fieldKindCoverage because the target namespace is hundreds of thousands of keys, so a collision proves less. Electing 323 CK3 fields put has_trait and capital on the loc path; 179 is the honest number."},

	// --- nested option databases (options.go)
	{"minOptionRefs", minOptionRefs, UnitCount,
		"a long tail of one-hit owners turning a GUI property into a database",
		"Calibrated for large databases, and known to exclude small real ones: task_contract/failure_standard has three distinct names and is genuine."},
	{"optionCoverage", optionCoverage, UnitPercent,
		"an option database claimed from a minority of its rows", ""},
	{"optionOwnerShare", optionOwnerShare, UnitPercent,
		"a field naming an owner it explains little of", ""},
	{"optionFieldShare", optionFieldShare, UnitPercent,
		"a minority field naming the database", ""},
	{"maxOptionKeysPerParent", maxOptionKeysPerParent, UnitCap,
		"a file dump being harvested as a database",
		"EU5 game-setup `start` blocks hold 1,357.8 keys per parent instance against 90.5 for the largest genuine shape — a 15x gap, and 40.5% of that game's model (T-0). Kept as a number because the structural alternative was measured and admits the blob: `change_father` is declared, so a declared-owner gate passes it while rejecting 14 of 36 real shapes. TestOptionKeysPerParentBand asserts the margin."},

	// --- localization naming conventions (derive.go)
	{"loc.MinAffixDefs", loc.MinAffixDefs, UnitCount,
		"one accident becoming a naming convention", ""},
	{"loc.MinAffixCoverage", loc.MinAffixCoverage, UnitPercent,
		"a convention claimed from a sliver of its kind", ""},
	{"loc.MinAffixDefsStrong", loc.MinAffixDefsStrong, UnitCount,
		"discarding a convention many definitions share but few of a kind do",
		"CK3 portrait modifiers are keyed per portrait group, so each covers about a tenth of `accessories` and none reaches a fifth; they were 994 of one mod's uncited orphans."},
	{"loc.MaxAffixesPerKind", loc.MaxAffixesPerKind, UnitCap,
		"one kind contributing unbounded conventions", ""},
	{"loc.MinKeyAffixKeys", loc.MinKeyAffixKeys, UnitCount,
		"a loc-key-derived convention resting on too few keys",
		"Learns NOT_, THIRD_, PAST_, _adj, _possessive unaided at 90-100% resolution, and correctly refuses _desc on CK3 while accepting it on Vic3 and EU5."},
	{"loc.MinKeyAffixCoverage", loc.MinKeyAffixCoverage, UnitPercent,
		"a loc-key convention that holds for a minority", ""},
	{"loc.MaxAffixStrip", loc.MaxAffixStrip, UnitCap,
		"unbounded affix stripping", ""},

	// --- macro signatures (modelvocab.go)
	{"maxMacroParams", maxMacroParams, UnitCap,
		"a rendered macro signature running off the card",
		"The largest measured across the three installs is well under it; the cap bounds display, not evidence."},

	// --- fire keys (derive.go)
	{"fireKeyCoverage", fireKeyCoverage, UnitPercent,
		"a property that usually holds something else being read as firing an event",
		"Loc properties like title and desc name events often enough to vote; the coverage rule is what keeps them off the fire path."},
	{"minFireNames", minFireNames, UnitCount,
		"one usage electing a fire key", ""},

	// --- derived snippets and block shapes (snippets.go)
	{"minSnippetDefs", minSnippetDefs, UnitCount,
		"judging a kind's shape from too few definitions", ""},
	{"minSnippetCoverage", minSnippetCoverage, UnitPercent,
		"every member vanilla uses once landing in the skeleton", ""},
	{"maxSnippetMembers", maxSnippetMembers, UnitCap,
		"a 200-line skeleton for a decision", ""},
	{"minNestedHits", minNestedHits, UnitCount,
		"a member seen once counting as part of a nested block's shape", ""},
	{"maxNestedMembers", maxNestedMembers, UnitCap,
		"an unbounded nested member list", ""},
	{"minAmbiguousParents", minAmbiguousParents, UnitCount,
		"one parent making a field name look ambiguous", ""},

	// --- budgets on harvested output (extract.go, graph.go)
	{"maxDefDocLines", maxDefDocLines, UnitCap,
		"a whole changelog parked above a macro becoming its description",
		"Matches what the install-prose reader already keeps."},
	{"maxViaHops", maxViaHops, UnitCap,
		"an unbounded search for the effect chain that reaches an event", ""},
}
