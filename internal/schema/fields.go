package schema

// An Input is one input of the Form view: a key that a definition may write, what it takes, and the inputs inside
// it when it holds a block.
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
func (schema *Schema) ListInputs(database string) []Input { panic("stub") }
