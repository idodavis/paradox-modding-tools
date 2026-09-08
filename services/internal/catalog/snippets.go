// snippets.go derives a fill-in-the-blanks skeleton for each kind the install
// defines, so a modder writing a new object starts from the shape the game
// actually uses rather than copying a vanilla file and deleting what they do not
// want.
//
// Derived, never written by hand. Other tools ship snippet files per game, which
// is why they cover one game and lag each patch; the shape is already in the
// install walk, so all three games are covered by construction and a game update
// is a rescan.

package catalog

import (
	"cmp"
	"slices"
	"strings"
)

const (
	// minSnippetDefs is how many definitions of a kind must exist before its
	// shape can be judged at all. Two definitions agreeing on a member proves
	// nothing — the same reasoning as minFieldHits and minOptionRefs.
	minSnippetDefs = 8
	// minSnippetCoverage is the share of a kind's definitions that must carry a
	// member for it to be part of the shape. Set high: a skeleton is a starting
	// point, and an optional field in it costs a modder more than a missing one.
	// Every member vanilla uses once would otherwise land in the snippet.
	minSnippetCoverage = 50
	// maxSnippetMembers bounds one skeleton. A decision has dozens of members
	// past the floor and a wall of them teaches nothing.
	maxSnippetMembers = 12
)

// SnippetMember is one line of a derived skeleton.
type SnippetMember struct {
	Key string `json:"key"`
	// Block is true when the member is written as a block rather than a scalar,
	// decided by how the install writes it more often than not.
	Block bool `json:"block,omitempty"`
	// Hits of Of definitions carry this member. Kept so a floor can be
	// recalibrated against real numbers instead of guessed at.
	Hits int `json:"hits"`
	Of   int `json:"of"`
}

// deriveSnippets reads the harvested block members and returns, per kind, the
// members that are part of that kind's shape.
//
// structures counts how often each member appears under a kind; blocks how often
// that member was itself a block. defs supplies the denominator — the number of
// definitions of the kind — which is what turns a raw count into evidence.
func deriveSnippets(
	structures, blocks map[string]map[string]int, defs []Def,
) map[string][]SnippetMember {
	total := map[string]int{}
	for _, d := range defs {
		total[d.Kind]++
	}
	out := map[string][]SnippetMember{}
	for kind, counts := range structures {
		of := total[kind]
		if of < minSnippetDefs {
			continue
		}
		members := make([]SnippetMember, 0, len(counts))
		for key, hits := range counts {
			// A member cannot appear in more definitions than exist; the count
			// is occurrences, so a repeated member is clamped rather than
			// allowed to clear the floor on its own.
			if hits > of {
				hits = of
			}
			if hits*100 < of*minSnippetCoverage {
				continue
			}
			members = append(members, SnippetMember{
				Key:   key,
				Block: blocks[kind][key]*2 > counts[key],
				Hits:  hits,
				Of:    of,
			})
		}
		if len(members) == 0 {
			continue
		}
		// Most-used first: that ordering is the closest thing the install gives
		// to "required", and it is what a reader wants at the top of a skeleton.
		slices.SortFunc(members, func(a, b SnippetMember) int {
			if c := cmp.Compare(b.Hits, a.Hits); c != 0 {
				return c
			}
			return strings.Compare(a.Key, b.Key)
		})
		if len(members) > maxSnippetMembers {
			members = members[:maxSnippetMembers]
		}
		out[kind] = members
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const (
	// minNestedHits is how often a member must be seen inside a block before it
	// is part of that block's shape. A block appears far more often than a
	// definition does, so a single sighting is even weaker evidence here than
	// it is for a kind.
	minNestedHits = 3
	// maxNestedMembers bounds one block's member list.
	maxNestedMembers = 40
)

// deriveNestedMembers turns raw block-member counts into the member list for
// each block, most-used first.
//
// This is what lets a query answer "what belongs inside triggered_desc?" rather
// than only "what belongs inside an event?". Keyed by the block's own name: a
// block called limit holds the same members wherever it appears, and merging the
// evidence is what makes the floor mean anything.
func deriveNestedMembers(nested map[string]map[string]int) map[string][]string {
	if len(nested) == 0 {
		return nil
	}
	out := make(map[string][]string, len(nested))
	for parent, counts := range nested {
		members := make([]string, 0, len(counts))
		for k, n := range counts {
			if n >= minNestedHits {
				members = append(members, k)
			}
		}
		if len(members) == 0 {
			continue
		}
		slices.SortFunc(members, func(a, b string) int {
			if c := cmp.Compare(counts[b], counts[a]); c != 0 {
				return c
			}
			return strings.Compare(a, b)
		})
		if len(members) > maxNestedMembers {
			members = members[:maxNestedMembers]
		}
		out[parent] = members
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// minAmbiguousParents is how many distinct blocks a field name must appear in
// before a single global description of it is untrustworthy. Two is enough: a
// name used in two different places has two different meanings often enough
// that showing one of them is a coin flip.
const minAmbiguousParents = 2

// deriveAmbiguousFields marks the field names that appear inside more than one
// kind of block, so a global description of them can be withheld.
func deriveAmbiguousFields(nested map[string][]string) map[string]bool {
	parents := map[string]int{}
	for _, members := range nested {
		for _, m := range members {
			parents[m]++
		}
	}
	out := map[string]bool{}
	for field, n := range parents {
		if n >= minAmbiguousParents {
			out[field] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// unambiguousBlocks names the blocks that appear under exactly one kind.
//
// A block name is not a position: CK3 has an `option` inside an event and
// another inside common/accolade_names, holding nothing in common. Merging them
// by name is right for OFFERING keys — the worst case is a suggestion that does
// not belong — and fatal for judging an absence, which reported 18,341 findings
// on 400 vanilla files. Only a name one kind uses can be judged.
func unambiguousBlocks(blockKinds map[string]map[string]bool) map[string]bool {
	if len(blockKinds) == 0 {
		return nil
	}
	out := map[string]bool{}
	for block, kinds := range blockKinds {
		if len(kinds) == 1 {
			out[block] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
