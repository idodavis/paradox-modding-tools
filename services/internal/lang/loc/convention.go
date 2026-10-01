// convention.go is the one loc-naming rule: a database's definitions are localized
// under a small set of key templates, and which of them a database actually uses is
// read from the install's own localization rather than guessed. Conventions runs once
// per database at scan time; Expand is the only way a key is derived from a
// definition afterwards.

package loc

import (
	"cmp"
	"slices"
	"strings"
)

// Template is one loc-key shape a database uses: "{id}", "{id}_desc", "rule_{id}", "{id}_adj".
type Template string

// baseTemplates are tried for every database; its block keys and the install's prefixes add more.
var baseTemplates = []Template{"{id}", "{id}_desc"}

// Convention is one template a database's defs are localized with, and how often.
type Convention struct {
	Template Template `json:"template"`
	Coverage float64  `json:"coverage"` // share of vanilla defs of the database that have this key
}

// Keys is the vanilla key set, indexed once: exact lookup, and per suffix the prefixes before it.
type Keys struct {
	has      map[string]bool
	prefixes map[string][]string // suffix after an underscore -> prefixes that precede it
}

// NewKeys indexes the key set in one pass.
func NewKeys(keys map[string]bool) *Keys {
	built := &Keys{has: keys, prefixes: make(map[string][]string, len(keys))}
	for key := range keys {
		for i := range len(key) {
			if key[i] == '_' {
				built.prefixes[key[i+1:]] = append(built.prefixes[key[i+1:]], key[:i])
			}
		}
	}
	return built
}

// PrefixesOf is every P for which `P_<id>` is a key.
func (keys *Keys) PrefixesOf(id string) []string {
	if keys == nil {
		return nil
	}
	return keys.prefixes[id]
}

// Has reports an exact key.
func (keys *Keys) Has(key string) bool { return keys != nil && keys.has[key] }

// render fills a template's {id} with the def id into buf, which the caller reuses: Conventions
// tests every template against every id, and a string built per pair would be the whole cost.
func render(buf []byte, template Template, id string) []byte {
	buf, rest := buf[:0], string(template)
	for {
		at := strings.Index(rest, "{id}")
		if at < 0 {
			return append(buf, rest...)
		}
		buf = append(append(buf, rest[:at]...), id...)
		rest = rest[at+len("{id}"):]
	}
}

// Expand renders every template for one def id.
func Expand(id string, cs []Convention) []string {
	if id == "" || len(cs) == 0 {
		return nil
	}
	out, buf := make([]string, 0, len(cs)), []byte{}
	for _, convention := range cs {
		buf = render(buf, convention.Template, id)
		out = append(out, string(buf))
	}
	return out
}

// Conventions keeps the templates under which most of a database's vanilla ids have a key, each
// with its exact coverage (required-loc demands 1.0). A shape only one definition has is no
// convention. A template the docs declare is measured beside them and kept even below the majority.
func Conventions(ids []string, entryKeys []string, keys *Keys, declared []string) []Convention {
	if keys == nil {
		return nil
	}
	var out []Convention
	buf := make([]byte, 0, 64)
	found := candidates(ids, entryKeys, keys)
	for _, template := range declared {
		if !slices.Contains(found, Template(template)) {
			found = append(found, Template(template))
		}
	}
	for _, template := range found {
		// stop once a found template cannot reach most ids: still exact, and most pairs go untested
		hits, isDeclared := 0, slices.Contains(declared, string(template))
		for i, id := range ids {
			if buf = render(buf, template, id); keys.has[string(buf)] {
				hits++
			} else if !isDeclared && (hits+len(ids)-i-1)*2 <= len(ids) {
				break
			}
		}
		if hits > 0 && isDeclared || hits > 1 && hits*2 > len(ids) {
			out = append(out, Convention{Template: template, Coverage: float64(hits) / float64(len(ids))})
		}
	}
	// Best evidence first, then the plainest shape, so the first hit is the def's own name.
	slices.SortFunc(out, func(first, second Convention) int {
		if order := cmp.Compare(second.Coverage, first.Coverage); order != 0 {
			return order
		}
		if order := cmp.Compare(len(first.Template), len(second.Template)); order != 0 {
			return order
		}
		return strings.Compare(string(first.Template), string(second.Template))
	})
	return out
}

// candidates is the base shapes, one per block key, and one per prefix that precedes most ids.
func candidates(ids, entryKeys []string, keys *Keys) []Template {
	out := slices.Clone(baseTemplates)
	add := func(template Template) {
		if !slices.Contains(out, template) {
			out = append(out, template)
		}
	}
	for _, entryKey := range entryKeys {
		if entryKey != "" {
			add(Template("{id}_" + entryKey))
		}
	}
	// `P_<id>` reconstructs the key, so one id cannot be counted twice under one prefix.
	counts := map[string]int{}
	for _, id := range ids {
		for _, prefix := range keys.PrefixesOf(id) {
			counts[prefix]++
		}
	}
	prefixes := make([]string, 0, len(counts))
	for prefix, count := range counts {
		if prefix != "" && count*2 > len(ids) {
			prefixes = append(prefixes, prefix)
		}
	}
	slices.Sort(prefixes)
	for _, prefixe := range prefixes {
		add(Template(prefixe + "_{id}"))
	}
	return out
}
