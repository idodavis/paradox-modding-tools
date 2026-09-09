// options.go finds databases whose rows live inside another def's block and are
// referenced by a field, never by a `prefix:id` cite — CK3 game-rule options
// (`has_game_rule = suf_quieter`), cultural parameters
// (`has_cultural_parameter = has_access_to_shieldmaidens`), realm laws, EU5
// policies. The nested-database pass is cite-driven, so it is blind to all of
// them, and Workspace Health reported every such reference as dangling.

package catalog

import (
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

const (
	// minOptionRefs is how many distinct names a field must reference before its
	// owner can be identified. Below this, coincidence dominates: a field with
	// five values matched unrelated blocks on every install measured.
	minOptionRefs = 8
	// optionCoverage is the share of a field's unreferenced values one block must
	// account for. The real cases sit at 96–100%; the noise sat at 80–92%.
	optionCoverage = 95
	// optionOwnerShare is the share of the owner's inner keys that must be rows.
	// Storing the complement only makes sense when rows dominate. Real option
	// databases sit at 64–96%.
	//
	// It does NOT reject a data blob, which is what it was believed to do. In a
	// blob almost every key is unique, so optionFieldShare skips nothing and the
	// ratio is ~100% — EU5's `start` scored a perfect 38,017 of 38,017 and
	// sailed through. See maxOptionKeysPerParent, which is the guard that
	// actually catches it.
	optionOwnerShare = 50

	// maxOptionKeysPerParent caps how many inner keys one parent instance may
	// hold before the block is a file dump rather than a database.
	//
	// This is the shape claim optionOwnerShare was reaching for and measured
	// against the wrong denominator: a database groups a handful of rows under
	// each parent, so keys-per-parent stays small no matter how many parents a
	// patch adds. A blob is one giant block.
	//
	// Measured over all three vanilla installs, 2026-09-08, every candidate the
	// pass considered:
	//
	//	start/father              1357.8   28 parents, 38,017 keys  <- the blob
	//	animations_props            90.5    8 parents,    724 keys  <- largest real
	//	animations                  29.7   47 parents,  1,395 keys
	//	genes/age                   13.9    9 parents,    125 keys
	//	map_data/area                9.8   82 parents,    805 keys
	//	everything else             < 8
	//
	// 250 sits ~2.8x above the largest genuine shape and 5.4x below the blob.
	// The gap is wide because the two things are not the same kind of object.
	maxOptionKeysPerParent = 250
)

// optionOwner is a block that may hold an option database: the kind of the
// enclosing def, and the group key the rows sit under ("" = direct children).
type optionOwner struct{ parentKind, groupKey string }

// ownerKeys is what one candidate block contains: how many instances of the
// parent were seen, and how many of them carry each inner key.
//
// The counts are what separate a row from an ordinary field, and the separation
// is stark. Under CK3 game rules, `categories` and `default` appear in all 81
// rules while 299 other keys appear in exactly one; under CK3 laws, `flag`
// appears in 24 of 27 groups and 189 law names in one apiece.
type ownerKeys struct {
	parents int
	keys    map[string]int
}

// anyGroup and anyGroup2 stand for "any block one (or two) levels down", used
// when a database is grouped by keys that are themselves row names, so evidence
// for it is split across every group. CK3 needs both: morph templates sit one
// level down (per gene), accessory templates two (per gene, per category).
const (
	anyGroup  = "*"
	anyGroup2 = "**"
)

// optionFieldShare is the share of parent instances a key must appear in to be
// an ordinary field rather than a row. Rows sit at 1/81 and 1/27; the lowest
// field measured was 17/27.
const optionFieldShare = 25

// addOptionOwners counts one file's block-child names by (kind, group key):
// direct children of each top-level def, and children one group deep. Callers
// hold the lock; this runs inside the shared derive walk.
func addOptionOwners(gameID, rel string, root *jomini.Root, out map[optionOwner]*ownerKeys) {
	rule := game.MatchExtract(gameID, rel)
	if rule.Mode != game.ModeTopLevelKey || root == nil {
		return
	}
	owner := func(o optionOwner) *ownerKeys {
		w := out[o]
		if w == nil {
			w = &ownerKeys{keys: map[string]int{}}
			out[o] = w
		}
		return w
	}
	for _, st := range root.Statements {
		top, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		b := jomini.BlockOf(top.Value)
		if b == nil {
			continue
		}
		direct := owner(optionOwner{rule.Kind, ""})
		direct.parents++
		// Count each key once per parent: a key repeated inside one block is
		// still one instance of "this parent has that key".
		local := map[string]bool{}
		for _, in := range b.Statements {
			ia, ok := in.(*jomini.Assignment)
			if !ok || ia.Key.Quoted {
				continue
			}
			local[ia.Key.Text] = true
			gb := jomini.BlockOf(ia.Value)
			if gb == nil {
				continue
			}
			inner := map[string]bool{}
			for _, gs := range gb.Statements {
				if ga, ok := gs.(*jomini.Assignment); ok && !ga.Key.Quoted {
					inner[ga.Key.Text] = true
				}
			}
			grouped := owner(optionOwner{rule.Kind, ia.Key.Text})
			grouped.parents++
			for k := range inner {
				grouped.keys[k]++
			}
			// Also aggregate across every group at this level. Some databases
			// are grouped by a key that is itself a row name — CK3 morph
			// templates sit under one block per gene — so evidence for the
			// database is split across dozens of owners and no single one can
			// account for the field's values.
			any := owner(optionOwner{rule.Kind, anyGroup})
			any.parents++
			for k := range inner {
				any.keys[k]++
			}
			for _, gs := range gb.Statements {
				ga, ok := gs.(*jomini.Assignment)
				if !ok || ga.Key.Quoted {
					continue
				}
				deep := jomini.BlockOf(ga.Value)
				if deep == nil {
					continue
				}
				names := map[string]bool{}
				for _, ds := range deep.Statements {
					if da, ok := ds.(*jomini.Assignment); ok && !da.Key.Quoted {
						names[da.Key.Text] = true
					}
				}
				any2 := owner(optionOwner{rule.Kind, anyGroup2})
				any2.parents++
				for k := range names {
					any2.keys[k]++
				}
			}
		}
		for k := range local {
			direct.keys[k]++
		}
	}
}

// deriveNestedOptions joins fields against nested blocks: when nearly every
// value a field takes is a child of one particular block, that block holds the
// database the field references.
//
// This is a deterministic join over many citations, in the same spirit as
// bindKinds — not a guess from shape. Precision comes from three filters, all
// tuned against CK3, Vic3 and EU5 vanilla: quantities and dotted values are not
// names, a field must reference at least minOptionRefs distinct names, and one
// block must account for optionCoverage of them.
func deriveNestedOptions(
	gameID string, defs []Def, fieldRHS map[string]map[string]bool,
	owners map[optionOwner]*ownerKeys,
) []NestedShape {
	if len(fieldRHS) == 0 {
		return nil
	}
	defKeys := defKeySet(defs)
	// Fields that agree on an owner are the same database seen through
	// different verbs (has_realm_law / add_realm_law / remove_realm_law), so
	// they are folded together and the busiest field names the kind.
	type claim struct {
		field string
		names map[string]bool
	}
	// Collect the values still looking for a home before touching the owners,
	// so the reverse index below covers only names some field actually asks
	// about rather than every inner key in the install.
	wanted := map[string]bool{}
	byField := map[string][]string{}
	for field, vals := range fieldRHS {
		if jomini.SkipObjectRHS(field) || isDigitKey(field) {
			continue
		}
		unresolved := make([]string, 0, len(vals))
		for v := range vals {
			if !defKeys[v] && !jomini.SkipObjectRHS(v) {
				unresolved = append(unresolved, v)
			}
		}
		if len(unresolved) < minOptionRefs {
			continue
		}
		// Sorted, because the greedy loop consumes this slice in order.
		slices.Sort(unresolved)
		byField[field] = unresolved
		for _, v := range unresolved {
			wanted[v] = true
		}
	}

	// Which owners contain each wanted name. The greedy loop below used to
	// answer that by probing every owner's key map once per remaining value,
	// which is thousands of owners times hundreds of values times a field:
	// 42% of the entire CK3 scan's CPU went into those string-map hashes.
	// Only a handful of owners hold any given name, so counting hits by
	// walking the values instead makes the work proportional to the answer.
	ownersOf := make(map[string][]optionOwner, len(wanted))
	for o, w := range owners {
		for k := range w.keys {
			if wanted[k] {
				ownersOf[k] = append(ownersOf[k], o)
			}
		}
	}

	best := map[optionOwner]claim{}
	// In field-name order, not map order. The greedy loop below consumes values
	// as it assigns owners, so which field is processed first changes how much
	// is left for the next one — and that decided which field named the
	// database. Two scans of one CK3 install disagreed on whether the `genes`
	// option database was `template` or `positive_mirror`, which moved 352 defs
	// to a different kind and made Workspace Health's counts differ between runs
	// of the same workspace. Scheduling must not decide what anything is called.
	fields := slices.Sorted(maps.Keys(byField))
	for _, field := range fields {
		unresolved := byField[field]
		// A field may reference more than one database: CK3's `template` names
		// morph templates, grouped one level down per gene, and accessory
		// templates two levels down. Owners are taken greedily until the field's
		// values are explained, and the whole set is discarded if they never are.
		//
		// Counting before materialising matters: allocating a set per candidate
		// owner was the largest source of garbage in the whole scan, and EU5 has
		// thousands of candidate blocks.
		remaining := unresolved
		claimed := map[optionOwner]map[string]bool{}
		explained := 0
		counts := map[optionOwner]int{}
		for len(remaining) > 0 {
			clear(counts)
			for _, v := range remaining {
				for _, o := range ownersOf[v] {
					counts[o]++
				}
			}
			var owner optionOwner
			bestHits := 0
			for o, n := range counts {
				if n > bestHits || (n == bestHits && betterOwner(o, owner)) {
					owner, bestHits = o, n
				}
			}
			// Every owner taken must carry evidence of its own. Accepting a long
			// tail of one-hit owners let a GUI property (`color1`) become a
			// database on a single coincidental name.
			if bestHits < minOptionRefs {
				break
			}
			hits := make(map[string]bool, bestHits)
			rest := remaining[:0:0]
			for _, v := range remaining {
				if owners[owner].keys[v] > 0 {
					hits[v] = true
				} else {
					rest = append(rest, v)
				}
			}
			claimed[owner] = hits
			explained += len(hits)
			remaining = rest
		}
		if explained*100 < len(unresolved)*optionCoverage {
			continue
		}
		// Sorted for the same reason: these writes are order-sensitive because a
		// losing field still contributes its names.
		for _, owner := range slices.SortedFunc(maps.Keys(claimed),
			func(a, b optionOwner) int {
				if a.parentKind != b.parentKind {
					return strings.Compare(a.parentKind, b.parentKind)
				}
				return strings.Compare(a.groupKey, b.groupKey)
			}) {
			hits := claimed[owner]
			if cur, ok := best[owner]; !ok ||
				betterOptionField(field, len(hits), cur.field, len(cur.names)) {
				if ok {
					// Keep the names the losing field contributed.
					for n := range cur.names {
						hits[n] = true
					}
				}
				best[owner] = claim{field: field, names: hits}
			} else {
				// A second verb on the same database: keep its names too.
				for n := range hits {
					cur.names[n] = true
				}
			}
		}
	}

	// Store the complement — the block's ordinary fields — rather than the rows,
	// so a mod's own new option is a row by default instead of being frozen out
	// by a list derived from vanilla.
	//
	// An ordinary field is one that appears in most instances of the parent.
	// Defining it as "not referenced by the field" instead was wrong: it swept in
	// every genuine row vanilla happens never to reference, which is how CK3's
	// `feudal_elective_succession_law` ended up excluded and then reported as a
	// dangling reference.
	out := make([]NestedShape, 0, len(best))
	for owner, c := range best {
		w := owners[owner]
		if w == nil {
			continue
		}
		var skip []string
		rows := 0
		for n, seen := range w.keys {
			if w.parents > 1 && seen*100 >= w.parents*optionFieldShare {
				skip = append(skip, n)
				continue
			}
			rows++
		}
		// A parent instance holding hundreds of keys is a file dump, not a
		// database of rows. EU5's game-setup `start` blocks reached here with
		// 38,017 keys across 28 parents and minted 57,652 defs — 40% of the
		// whole EU5 model — under a kind named after whichever field happened
		// to win: `institutions`, `maliki_school`, `add_market`.
		if len(w.keys) > w.parents*maxOptionKeysPerParent {
			continue
		}
		if rows*100 < len(w.keys)*optionOwnerShare {
			continue
		}
		slices.Sort(skip)
		out = append(out, NestedShape{
			ParentKind: owner.parentKind,
			ChildKind:  optionKindName(c.field),
			GroupKey:   owner.groupKey,
			SkipKeys:   skip,
			IsOption:   true,
		})
	}
	slices.SortFunc(out, func(a, b NestedShape) int {
		if a.ParentKind != b.ParentKind {
			return strings.Compare(a.ParentKind, b.ParentKind)
		}
		return strings.Compare(a.ChildKind, b.ChildKind)
	})
	return out
}

// betterOptionField picks which field gets to name the database. A field that
// asks about the thing (`has_game_rule`) names it better than one that merely
// takes it (`default`), so a verb form wins regardless of citation count;
// otherwise the busiest field wins, ties broken by name for determinism.
func betterOptionField(field string, hits int, curField string, curHits int) bool {
	a, b := optionVerbForm(field), optionVerbForm(curField)
	if a != b {
		return a
	}
	if hits != curHits {
		return hits > curHits
	}
	return field < curField
}

func optionVerbForm(field string) bool { return optionKindName(field) != strings.ToLower(field) }

// optionKindName turns the referencing field into the kind modders use for the
// thing: has_game_rule → game_rule, has_cultural_parameter → cultural_parameter.
func optionKindName(field string) string {
	k := strings.ToLower(field)
	for _, verb := range []string{
		"has_", "add_", "remove_", "set_", "change_", "complete_",
	} {
		if strings.HasPrefix(k, verb) && len(k) > len(verb) {
			return strings.TrimPrefix(k, verb)
		}
	}
	return k
}

// applyOptionShape harvests the named rows of an option database. Membership is
// settled by the name list the join produced, so neither the value's shape nor
// a citation is consulted: a game rule's option is a block, a cultural
// parameter is `name = yes`, and both are equally real.
func applyOptionShape(
	gameID, path, origin string, root *jomini.Root, li *jomini.LineIndex,
	shape NestedShape,
) []Def {
	skip := make(map[string]bool, len(shape.SkipKeys))
	for _, n := range shape.SkipKeys {
		skip[n] = true
	}
	var defs []Def
	add := func(a *jomini.Assignment) {
		name := game.KeyIdentity(gameID, a.Key.Text)
		if skip[name] || !jomini.IsDefName(name) || jomini.IsStop(name) {
			return
		}
		d := makeDef(shape.ChildKind, name, path, origin, a.Key.Range, li)
		d.Nested = true
		defs = append(defs, d)
	}
	for _, st := range root.Statements {
		top, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		b := jomini.BlockOf(top.Value)
		if b == nil {
			continue
		}
		for _, in := range b.Statements {
			ia, ok := in.(*jomini.Assignment)
			if !ok || ia.Key.Quoted {
				continue
			}
			if shape.GroupKey == "" {
				add(ia)
				continue
			}
			wild := shape.GroupKey == anyGroup || shape.GroupKey == anyGroup2
			if !wild && !strings.EqualFold(ia.Key.Text, shape.GroupKey) {
				continue
			}
			gb := jomini.BlockOf(ia.Value)
			if gb == nil {
				continue
			}
			for _, gs := range gb.Statements {
				ga, ok := gs.(*jomini.Assignment)
				if !ok || ga.Key.Quoted {
					continue
				}
				if shape.GroupKey != anyGroup2 {
					add(ga)
					continue
				}
				// One level deeper: the rows sit under a second group whose key
				// is also a name rather than a label.
				if deep := jomini.BlockOf(ga.Value); deep != nil {
					for _, ds := range deep.Statements {
						if da, ok := ds.(*jomini.Assignment); ok && !da.Key.Quoted {
							add(da)
						}
					}
				}
			}
		}
	}
	return defs
}

// betterOwner breaks a tie between owners that explain the same names.
// A named group says exactly where the rows are; the wildcards say only how far
// down, so they are the fallback and the deeper one is the last resort. Beyond
// that the order is by name, so that two equally good owners resolve the same
// way on every scan rather than by Go map iteration order.
func betterOwner(a, b optionOwner) bool {
	if ra, rb := groupRank(a.groupKey), groupRank(b.groupKey); ra != rb {
		return ra < rb
	}
	if a.parentKind != b.parentKind {
		return a.parentKind < b.parentKind
	}
	return a.groupKey < b.groupKey
}

func groupRank(group string) int {
	switch group {
	case anyGroup2:
		return 3
	case anyGroup:
		return 2
	default:
		return 1
	}
}
