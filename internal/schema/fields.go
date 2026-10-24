package schema

import (
	"maps"
	"slices"
	"strings"
)

// An Input is one input of the Form view, for a key that a definition may write.
type Input struct {
	Key      string    `json:"key"`
	Type     ValueType `json:"type"`
	Database string    `json:"database,omitempty"`
	Choices  []string  `json:"choices,omitempty"`
	Required bool      `json:"required"`
	Default  string    `json:"default,omitempty"`
	Blurb    string    `json:"blurb,omitempty"`
	Inputs   []Input   `json:"inputs,omitempty"`
}

// ListInputs returns the Form view's inputs for a database, from the field tree of its definitions. It returns
// none for a database the schema cannot describe, so no form is offered where the engine would be guessing.
func (schema *Schema) ListInputs(database string) []Input {
	row := schema.Databases[database]
	if row == nil {
		return nil
	}
	return listInputs(row.Definition)
}

// listInputs returns the inputs for the children of a field, in key order. A child that no source gives a value or
// children of its own gives none, and neither does a pattern such as <any> in place of a key.
func listInputs(field *Field) []Input {
	var inputs []Input
	for _, key := range slices.Sorted(maps.Keys(field.Children)) {
		child := field.Children[key]
		if strings.HasPrefix(key, "<") || !child.Value.IsStated() && len(child.Children) == 0 {
			continue
		}
		expected := child.Value.Value
		inputs = append(inputs, Input{
			Key: key, Type: expected.Type, Database: expected.Database, Choices: expected.Choices,
			Required: child.Required.Value == "always", Default: child.Default.Value, Blurb: child.Blurb.Value,
			Inputs: listInputs(child),
		})
	}
	return inputs
}
