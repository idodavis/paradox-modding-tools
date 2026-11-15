// comparison.go compares the dumps, the documented keys and the defined names
// of two versions of a game, to say which keys a patch added, removed or
// changed.

package schema

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/lang"
)

// DumpChange is one key of the game's dumps that a patch added, removed or
// changed.
type DumpChange struct {
	// Database is the database of lang's dumps the key is in, as "effects",
	// and for an on_action the schema's own on_actions database. It is "" for
	// a function of a data type, whose key is its type and name, as
	// Character.GetName.
	Database string `json:"database"`

	Key    string `json:"key"`
	Status Status `json:"status"`

	// Blurb is the first line of the key's entry. For a removed key it is
	// the old entry's, else the new one's.
	Blurb string `json:"blurb"`

	// Differences lists which facts of a changed key's entry differ, such as
	// "sentence" or "scopes". It is empty for an added or a removed key.
	Differences []string `json:"differences"`
}

// KeyChange is one key of a database that a patch added or removed: a key the
// docs document, or a name the game's files define.
type KeyChange struct {
	Database string `json:"database"`
	Key      string `json:"key"`

	// Status is StatusAdded or StatusRemoved. A key has no other change.
	Status Status `json:"status"`
}

// Status says what a patch did to a key: it added, removed or changed it.
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
// and the facts whose change makes the key changed. A key with no facts
// cannot change.
type entry struct {
	blurb string
	facts []fact
}

// entryKey is the place of an entry: the database it is in and its key.
type entryKey struct{ database, key string }

// CompareDumps returns what changed in the keys of the dumps from the old
// version to the current one: each key only the current version has is
// added, each key only the old one has is removed, and each key whose entry
// says something else is changed, with the facts that differ. A key that
// fits a star key of the given schema, such as every_country_in_*, is the
// star key, so the generated keys the dumps of one version list and another
// does not are no change; a star key's own entry is not compared. A key that
// two entries share keeps the first. On_actions are left out where the
// schema has no on_actions database. The changes are sorted by database and
// key.
func CompareDumps(old, current *Dumps, language *lang.Schema) []DumpChange {
	before, after := listEntries(old, language), listEntries(current, language)
	var changes []DumpChange
	for place, latest := range after {
		earlier, ok := before[place]
		if !ok {
			changes = append(changes, makeChange(place, StatusAdded, latest.blurb, nil))

			continue
		}

		if differences := listDifferences(earlier, latest); len(differences) > 0 {
			changes = append(changes, makeChange(place, StatusChanged, latest.blurb, differences))
		}
	}

	for place, earlier := range before {
		if _, ok := after[place]; !ok {
			changes = append(changes, makeChange(place, StatusRemoved, earlier.blurb, nil))
		}
	}

	slices.SortFunc(changes, func(first, second DumpChange) int {
		return cmp.Or(strings.Compare(first.Database, second.Database), strings.Compare(first.Key, second.Key))
	})

	return changes
}

// CompareKeys returns the keys the current version has in a database and the
// old one does not as added, and the keys the old one has and the current
// one does not as removed. The keys are those ListDocKeys or ListDefinedKeys
// give. The changes are sorted by database and key.
func CompareKeys(old, current map[string][]string) []KeyChange {
	var changes []KeyChange

	for database, keys := range old {
		changes = append(changes, listMissing(database, keys, current[database], StatusRemoved)...)
	}

	for database, keys := range current {
		changes = append(changes, listMissing(database, keys, old[database], StatusAdded)...)
	}

	slices.SortFunc(changes, func(first, second KeyChange) int {
		return cmp.Or(strings.Compare(first.Database, second.Database), strings.Compare(first.Key, second.Key))
	})

	return changes
}

// listMissing returns a change of the given status for each key of one list
// that the other lacks.
func listMissing(database string, keys, other []string, status Status) []KeyChange {
	held := make(map[string]bool, len(other))
	for _, key := range other {
		held[key] = true
	}

	var changes []KeyChange

	for _, key := range keys {
		if !held[key] {
			changes = append(changes, KeyChange{Database: database, Key: key, Status: status})
		}
	}

	return changes
}

// makeChange returns the change of one key.
func makeChange(place entryKey, status Status, blurb string, differences []string) DumpChange {
	return DumpChange{Database: place.database, Key: place.key, Status: status, Blurb: blurb, Differences: differences}
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

// listEntries returns every key of the dumps with its entry: the effects,
// triggers and links, the scope types, the modifiers, the on_actions and the
// functions of the data types. A function is in no database, and its key is
// its data type and name, as Character.GetName.
func listEntries(dumps *Dumps, language *lang.Schema) map[entryKey]entry {
	entries := map[entryKey]entry{}
	for _, command := range dumps.Commands {
		place := entryKey{findDatabase(command.Declared), command.Name}
		listed := entry{blurb: firstLine(command.Description)}
		if patterns := language.FindPatterns(place.database, place.key); len(patterns) > 0 {
			place.key = patterns[0].Key
		} else {
			listed.facts = []fact{
				{factSentence, command.Description},
				{factScopes, strings.Join(slices.Sorted(slices.Values(command.Scopes)), ",")},
				{factTarget, command.Target},
				{factTraits, command.Traits},
			}
		}

		if _, ok := entries[place]; !ok {
			entries[place] = listed
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
			entries[entryKey{"", dataType.Name + "." + function.Name}] = entry{
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
