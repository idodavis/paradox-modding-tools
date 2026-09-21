// drift.go is what a scan saw and nothing explained: the alarm for a game update that changes the language. Every list is sorted by name, so two scans of one install write the same report.
package learn

import (
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// drift is the report for a learned schema.
func (c *census) drift(in Input, parseErrors []schema.Tally, s *schema.Schema) schema.Drift {
	d := schema.Drift{ParseErrors: parseErrors, DumpLabels: in.Vocab.Unknown}

	// files with no format, and top-level folders of script in which no kind has a folder
	extensions, folders, held := map[string]int{}, map[string]int{}, map[string]bool{}
	for _, k := range s.Kinds {
		for _, f := range k.Folders {
			top, _, _ := strings.Cut(strings.ToLower(f), "/")
			held[top] = true
		}
	}
	for _, f := range in.Files {
		top, _, nested := strings.Cut(strings.ToLower(f.Rel), "/")
		switch {
		case f.Format == source.Unknown:
			extensions[strings.ToLower(path.Ext(f.Rel))]++
		case f.Format == source.Script && nested && !held[top]:
			folders[top]++
		}
	}
	d.Extensions, d.Folders = tallies(extensions), tallies(folders)

	// entry modes the registry gives no meaning, and prefixes nothing declares and no kind owns
	modes, prefixes := map[string]int{}, map[string]int{}
	for mode, n := range c.modes {
		if !slices.Contains(in.EntryModes, mode) {
			modes[mode] = n
		}
	}
	made := slices.Collect(maps.Values(schema.ScriptKinds))
	for prefix, names := range c.cites {
		written := 0
		for _, n := range names {
			written += n
		}
		if !in.Vocab.Links[prefix].Prefix && len(c.prefixed[prefix]) == 0 && !slices.Contains(made, prefix) {
			prefixes[prefix] = written
		}
	}
	d.EntryModes, d.Prefixes = tallies(modes), tallies(prefixes)

	// an effect outside the hand table most of whose values are names that nothing declares or defines and that are cited under a script-made kind's prefix. Only an effect makes a name: a trigger that reads one (has_sexuality) takes words that any saved scope may share, root is a declared link that scope:root also spells, and a trait's name is also written flag:brave
	tokens, unclassified := map[string]int{}, map[string]int{}
	for token, n := range c.tokens {
		if _, makes := in.Vocab.Effects[token]; !makes || len(schema.NameTokensOf(token)) > 0 {
			continue
		}
		written, named := 0, 0
		for w, times := range n.words {
			written += times
			_, isLink := in.Vocab.Links[w]
			for _, prefix := range made {
				if prefix != "" && c.cites[prefix][w] > 0 && !isLink && len(c.defined[w]) == 0 {
					named += times
					break
				}
			}
		}
		if named*2 > written {
			tokens[token] = written
		}
	}
	for token, n := range c.written {
		if s.Arguments[token] == nil {
			unclassified[token] = n
		}
	}
	d.NameTokens, d.Unclassified = tallies(tokens), tallies(unclassified)
	return d
}

// tallies is a count by name as a sorted list, nil when there is nothing.
func tallies(counts map[string]int) []schema.Tally {
	var out []schema.Tally
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, schema.Tally{Name: name, Count: counts[name]})
	}
	return out
}
