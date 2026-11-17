// comparison.go compares two versions of a game by key: the keys of their
// dumps and the names their files define.

package schema

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/lang"
)

// KeyChange is one key that a patch added, removed or changed: a key of the
// game's dumps, or a name the game's files define.
type KeyChange struct {
	// Database is the database the key is in. A function of a data type is in
	// lang.DataFunctions, under its type and name, as Character.GetName.
	Database string `json:"database"`

	Key    string `json:"key"`
	Status Status `json:"status"`

	// Blurb is the first line of a dump key's entry, the old entry's for a
	// removed key. Differences lists which facts of a changed entry differ,
	// such as "sentence" or "scopes". A defined name has neither.
	Blurb       string   `json:"blurb"`
	Differences []string `json:"differences"`
}

// Status says what a patch did to a key or a file: it added, removed or
// changed it.
type Status string

// The statuses of a key.
const (
	StatusAdded   Status = "added"
	StatusRemoved Status = "removed"
	StatusChanged Status = "changed"
)

// The facts of an entry, which a changed key's Differences name: a command's,
// a scope type's, an on_action's and a data function's.
const (
	factSentence  = "sentence"
	factScopes    = "scopes"
	factTarget    = "target"
	factTraits    = "traits"
	factTriggers  = "triggers"
	factEffects   = "effects"
	factScope     = "scope"
	factHardcoded = "hardcoded"
	factBlurb     = "blurb"
	factReturns   = "return type"
	factArguments = "arguments"
)

// fact is one thing a dump says about a key, with its value as a text.
type fact struct{ label, value string }

// entry is one key of the dumps as the comparison reads it: its first line,
// and the facts whose change makes the key changed.
type entry struct {
	blurb string
	facts []fact
}

// entryKey is the place of an entry: the database it is in and its key.
type entryKey struct{ database, key string }

// Compare returns the status of each key that two maps do not hold alike:
// added for a key only the current map has, removed for a key only the old
// map has, and changed for a key whose two values the given test tells apart.
func Compare[K comparable, V any](old, current map[K]V, differs func(earlier, latest V) bool) map[K]Status {
	statuses := map[K]Status{}
	for key, latest := range current {
		earlier, ok := old[key]
		switch {
		case !ok:
			statuses[key] = StatusAdded
		case differs(earlier, latest):
			statuses[key] = StatusChanged
		}
	}

	for key := range old {
		if _, ok := current[key]; !ok {
			statuses[key] = StatusRemoved
		}
	}

	return statuses
}

// CompareDumps returns what changed in the keys of the dumps from the old
// version to the current one, sorted by database and key. A changed key lists
// the facts that differ. It returns nothing if either dumps are nil.
//
// A key that fits a star key of the schema, such as every_country_in_*, is
// compared as the star key, so generated keys are no change. A kind of entry
// that one version lists none of, or that the two write differently, as
// modifiers by name and by template, is left out: its changes would be false.
func CompareDumps(old, current *Dumps, language *lang.Schema) []KeyChange {
	if old == nil || current == nil {
		return nil
	}

	compared := map[string]bool{
		lang.DataFunctions: len(old.DataTypes) > 0 && len(current.DataTypes) > 0,
		lang.DumpTypes:     len(old.Scopes) > 0 && len(current.Scopes) > 0,
		lang.DumpModifiers: hasTemplate(old.Modifiers) == hasTemplate(current.Modifiers),
	}
	before, after := listEntries(old, language), listEntries(current, language)
	differs := func(earlier, latest entry) bool { return len(listDifferences(earlier, latest)) > 0 }

	var changes []KeyChange
	for place, status := range Compare(before, after, differs) {
		if isCompared, ok := compared[place.database]; ok && !isCompared {
			continue
		}

		change := KeyChange{Database: place.database, Key: place.key, Status: status, Blurb: after[place].blurb}
		switch status {
		case StatusRemoved:
			change.Blurb = before[place].blurb
		case StatusChanged:
			change.Differences = listDifferences(before[place], after[place])
		}

		changes = append(changes, change)
	}

	return sortChanges(changes)
}

// CompareKeys returns the keys only the current version has as added, and the
// keys only the old one has as removed, sorted by database and key.
func CompareKeys(old, current map[string][]string) []KeyChange {
	never := func(_, _ bool) bool { return false }

	var changes []KeyChange
	for place, status := range Compare(listKeys(old), listKeys(current), never) {
		changes = append(changes, KeyChange{Database: place.database, Key: place.key, Status: status})
	}

	return sortChanges(changes)
}

// sortChanges sorts changes by database and key, and returns them.
func sortChanges(changes []KeyChange) []KeyChange {
	slices.SortFunc(changes, func(first, second KeyChange) int {
		return cmp.Or(strings.Compare(first.Database, second.Database), strings.Compare(first.Key, second.Key))
	})

	return changes
}

// listKeys returns the keys of each database as one set.
func listKeys(keys map[string][]string) map[entryKey]bool {
	set := map[entryKey]bool{}
	for database, names := range keys {
		for _, name := range names {
			set[entryKey{database, name}] = true
		}
	}

	return set
}

// hasTemplate reports whether one of the keys is a template, which has a name
// between dollar signs, as $CULTURE$_opinion.
func hasTemplate(keys map[string]bool) bool {
	for key := range keys {
		if strings.Contains(key, "$") {
			return true
		}
	}

	return false
}

// listDifferences returns the labels of the facts whose values differ
// between two entries of one key, in the order the facts come.
func listDifferences(earlier, latest entry) []string {
	var labels []string
	for position, fact := range latest.facts {
		if position < len(earlier.facts) && earlier.facts[position].value != fact.value {
			labels = append(labels, fact.label)
		}
	}

	return labels
}

// listEntries returns every key of the dumps with its entry. A key that fits
// a star key has no facts, so it cannot change.
func listEntries(dumps *Dumps, language *lang.Schema) map[entryKey]entry {
	entries := map[entryKey]entry{}
	for _, command := range dumps.Commands {
		place := entryKey{findDatabase(command.Declared), command.Name}
		commandEntry := entry{blurb: firstLine(command.Description)}
		if patterns := language.FindPatterns(place.database, place.key); len(patterns) > 0 {
			place.key = patterns[0].Key
		} else {
			commandEntry.facts = []fact{
				{factSentence, command.Description},
				{factScopes, strings.Join(slices.Sorted(slices.Values(command.Scopes)), ",")},
				{factTarget, command.Target},
				{factTraits, command.Traits},
			}
		}

		if _, ok := entries[place]; !ok {
			entries[place] = commandEntry
		}
	}

	for key, scope := range dumps.Scopes {
		entries[entryKey{lang.DumpTypes, key}] = entry{facts: []fact{
			{factTriggers, strconv.FormatBool(scope.Triggers)}, {factEffects, strconv.FormatBool(scope.Effects)},
		}}
	}

	for key := range dumps.Modifiers {
		entries[entryKey{lang.DumpModifiers, key}] = entry{}
	}

	if language.OnActions != "" {
		for key, action := range dumps.OnActions {
			entries[entryKey{language.OnActions, key}] = entry{facts: []fact{
				{factScope, action.Scope}, {factHardcoded, strconv.FormatBool(action.Hardcoded)},
			}}
		}
	}

	for _, dataType := range dumps.DataTypes {
		for _, function := range dataType.Functions {
			entries[entryKey{lang.DataFunctions, dataType.Name + "." + function.Name}] = entry{
				blurb: firstLine(function.Blurb),
				facts: []fact{
					{factBlurb, function.Blurb}, {factReturns, function.Returns},
					{factArguments, strings.Join(function.Arguments, ", ")},
				},
			}
		}
	}

	return entries
}

// findDatabase returns the database a dump file's entries fill, from what
// the file declares. It is "" for a declaration no dump file makes.
func findDatabase(declared string) string {
	for _, section := range dumpDatabases {
		if section.declared == declared {
			return section.database
		}
	}

	return ""
}

// firstLine returns the first line of a text, without the space around it.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")

	return strings.TrimSpace(line)
}
