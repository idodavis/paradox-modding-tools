package schema

// A DocField is what the docs say about one key of a database's definitions.
type DocField struct {
	Blurb    string
	Type     string // the type as the docs write it; "" if they give none
	Required string // optional, always, conditional, or ""
	Default  string
}

// A DocDatabase is what the docs say about one database: its blurb and its fields.
type DocDatabase struct {
	Blurb  string
	Fields map[string]DocField
}

// Docs is what the game's own prose files say: every .info, readme and .md file whose folder or file name names a
// database.
type Docs struct {
	Databases map[string]DocDatabase
}

// ReadDocs reads the game's prose files under an install, for the database folders given. A database's blurb is
// the first paragraph of prose, never a `== heading ==` line or an example. Script fragments inside the prose are
// read with the docs' own tokenizer rather than the parser, because prose is not valid script.
func ReadDocs(installPath string, folders []string) (*Docs, error) { panic("stub") }
