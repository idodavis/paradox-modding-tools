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
