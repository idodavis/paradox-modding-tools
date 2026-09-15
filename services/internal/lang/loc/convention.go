// convention.go is the one loc-naming rule: a kind's definitions are localized
// under a small set of key templates, and which of them a kind actually uses is
// read from the install's own localization rather than guessed. Learn runs once
// per kind at scan time; Expand is the only way a key is derived from a
// definition afterwards.

package loc

import (
	"cmp"
	"slices"
	"strings"
)

// Template is one loc-key shape a kind uses: "{id}", "{id}_desc", "rule_{id}", "{id}_adj".
type Template string

// baseTemplates are tried for every kind; its block members and the install's own prefixes add more.
var baseTemplates = []Template{"{id}", "{id}_desc"}

// Convention is one template a kind's defs are localized with, and how often.
type Convention struct {
	Template Template `json:"template"`
	Coverage float64  `json:"coverage"` // share of vanilla defs of the kind that have this key
}

// Keys is the vanilla key set indexed once: exact lookup, and per suffix the prefixes seen before it.
type Keys struct {
	has      map[string]bool
	prefixes map[string][]string // suffix after an underscore -> prefixes that precede it
}

// NewKeys indexes the key set in one pass.
func NewKeys(keys map[string]bool) *Keys {
	k := &Keys{has: keys, prefixes: make(map[string][]string, len(keys))}
	for key := range keys {
		for i := range len(key) {
			if key[i] == '_' {
				k.prefixes[key[i+1:]] = append(k.prefixes[key[i+1:]], key[:i])
			}
		}
	}
	return k
}

// Has reports an exact key.
func (k *Keys) Has(key string) bool { return k != nil && k.has[key] }

// PrefixesOf is every P for which `P_<id>` is a key.
func (k *Keys) PrefixesOf(id string) []string {
	if k == nil {
		return nil
	}
	return k.prefixes[id]
}

// render fills a template's {id} with the def id.
func render(t Template, id string) string {
	return strings.ReplaceAll(string(t), "{id}", id)
}

// Expand renders every template for one def id.
func Expand(id string, cs []Convention) []string {
	if id == "" || len(cs) == 0 {
		return nil
	}
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, render(c.Template, id))
	}
	return out
}

// minConventionDefs is how many definitions a kind needs before a shared shape is evidence, not coincidence.
const minConventionDefs = 8

// Learn keeps the templates that cover at least minCoverage of a kind's vanilla ids.
func Learn(ids []string, members []string, keys *Keys, minCoverage float64) []Convention {
	if len(ids) < minConventionDefs || keys == nil {
		return nil
	}
	var out []Convention
	for _, t := range candidates(ids, members, keys, minCoverage) {
		hits := 0
		for _, id := range ids {
			if keys.Has(render(t, id)) {
				hits++
			}
		}
		if cov := float64(hits) / float64(len(ids)); cov >= minCoverage {
			out = append(out, Convention{Template: t, Coverage: cov})
		}
	}
	// Best evidence first, then the plainest shape, so the first hit is the def's own name.
	slices.SortFunc(out, func(a, b Convention) int {
		if c := cmp.Compare(b.Coverage, a.Coverage); c != 0 {
			return c
		}
		if c := cmp.Compare(len(a.Template), len(b.Template)); c != 0 {
			return c
		}
		return strings.Compare(string(a.Template), string(b.Template))
	})
	return out
}

// candidates is the base shapes, one literal template per block member, and one per qualifying prefix.
func candidates(ids, members []string, keys *Keys, minCoverage float64) []Template {
	out := slices.Clone(baseTemplates)
	add := func(t Template) {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	for _, m := range members {
		if m != "" {
			add(Template("{id}_" + m))
		}
	}
	// `P_<id>` reconstructs the key, so one id cannot be counted twice under one prefix.
	counts := map[string]int{}
	for _, id := range ids {
		for _, p := range keys.PrefixesOf(id) {
			counts[p]++
		}
	}
	prefixes := make([]string, 0, len(counts))
	for p, n := range counts {
		if p != "" && float64(n) >= minCoverage*float64(len(ids)) {
			prefixes = append(prefixes, p)
		}
	}
	slices.Sort(prefixes)
	for _, p := range prefixes {
		add(Template(p + "_{id}"))
	}
	return out
}

// minKeyPrefixHits is how many keys must resolve through a prefix before it is a convention.
const minKeyPrefixHits = 20

// LearnKeyPrefixes finds prefixes that build one loc key out of another (EU5's NOT_<key>).
func LearnKeyPrefixes(keys *Keys, minCoverage float64) []string {
	if keys == nil {
		return nil
	}
	hits, total := map[string]int{}, map[string]int{}
	for suffix, prefixes := range keys.prefixes {
		stem := keys.has[suffix]
		for _, p := range prefixes {
			total[p]++
			if stem {
				hits[p]++
			}
		}
	}
	var out []string
	for p, n := range hits {
		if p != "" && n >= minKeyPrefixHits && float64(n) >= minCoverage*float64(total[p]) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}
