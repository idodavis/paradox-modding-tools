// fields.go is the keys an entry of a database writes, each with what the schema states of its value.

package schema

import (
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Fields is the keys an entry of a database writes.
type Fields struct {
	Database string
	Keys     []Field // one per key it writes
}

// Field is one key of an entry's fields.
type Field struct {
	Key      string
	Type     ValueType // the kind of its value
	Choices  []string  // the words it takes; empty any
	Database string    // the database its values name; "" none
	Required bool      // an entry must write it
	Default  string    // the value it takes when unwritten
	Blurb    string    // what it is, in a sentence
}

// ReadFields returns the fields of a database's entry; nil where the database is unknown or its entry holds no fields
// (a script body holds commands) or no key, since a guessed field is worse than none.
func (schema *Schema) ReadFields(database string) *Fields {
	if schema.Databases[database] == nil {
		return nil
	}
	entry := schema.Databases[database].Entry
	if entry.Shape.Value != game.ShapeFields || len(entry.Children) == 0 {
		return nil
	}
	fields := &Fields{Database: database}
	for _, key := range slices.Sorted(maps.Keys(entry.Children)) {
		position := entry.Children[key]
		fields.Keys = append(fields.Keys, Field{
			Key: key, Type: position.Value.Value.Type, Choices: position.Value.Value.Choices, Database: schema.resolveValueDatabase(position),
			Required: position.Required.Value.Level == LevelAlways, Default: position.Default.Value, Blurb: position.Blurb.Value,
		})
	}
	return fields
}

// resolveValueDatabase returns the database the values at a position name: the value claim's, else the one every value
// vanilla writes there names by its link prefix; "" for none or for values that name several.
func (schema *Schema) resolveValueDatabase(position *Position) string {
	if claim := position.Value.Value; claim.Type == ValueReference {
		return claim.Database
	}
	database := ""
	for word := range position.Vanilla.Values {
		prefixed, isPrefixed := jomini.SplitPrefixed(word)
		target := schema.Prefixes[strings.ToLower(prefixed.Prefix)]
		if !isPrefixed || target == "" || database != "" && target != database {
			return ""
		}
		database = target
	}
	return database
}
