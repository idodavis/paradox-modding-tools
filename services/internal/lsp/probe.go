// Package lsp answers the editor's requests for one open file: the word under the cursor, where it
// is defined and used, what may be written there, what is wrong, and how the file is folded,
// formatted and annotated. Every answer starts from one descent to the cursor (index.Probe) read by
// schema.Reader; payloads carry that reading (schema.Value) and the frontend templates the text.
//
// probe.go holds the payloads and the descent that fills them: index.Probe walks the statements
// around an offset as the harvest walks a whole file, and at reads the word there.
package lsp

import (
	"strconv"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// site is the cursor's word: what the reader made of it, the statement it is in, and its span.
type site struct {
	Value  schema.Value
	Level  index.Level // the innermost statement around the cursor
	Path   *index.Descent
	Text   string // the word as written
	Quoted bool
	Range  jomini.Range
	IsKey  bool
	Weight int // a numbered key in a body: its share of its siblings in percent; 0 otherwise
}

// file is an open file's parse and place, held for one request.
type file struct {
	sess   *session.Session
	path   string
	text   string
	parsed *jomini.Result
	place  session.Place
}

// open parses a file (once per text for an open buffer) and places it.
func open(sess *session.Session, path string) (*file, error) {
	parsed, text, err := sess.Parsed(path)
	if err != nil {
		return nil, err
	}
	return &file{sess: sess, path: path, text: text, parsed: parsed, place: sess.Where(path)}, nil
}

// offset is the byte offset of a line and column of the file.
func (file *file) offset(line, col int) int {
	return file.parsed.Lines().OffsetAt(line, col)
}

// span is the editor's range of a span of the file.
func (file *file) span(span jomini.Range) Range {
	return Range{Start: file.parsed.Lines().PositionAt(span.Start), End: file.parsed.Lines().PositionAt(span.End)}
}

// at is the word under an offset of a script file, or nil on a comment, a brace or a blank.
func (file *file) at(offset int) *site {
	if len(file.place.Rel) == 0 {
		return nil
	}
	found, _, _ := file.sess.Located(file.path)
	descent := index.Probe(found, file.parsed, file.sess.Reader(), offset)
	if len(descent.Chain) == 0 {
		return nil
	}
	level := descent.Chain[len(descent.Chain)-1]
	reader := file.sess.Reader()
	word := &site{Level: level, Path: descent}

	switch st := level.Statement.(type) {
	case *jomini.Assignment:
		if within(st.Key.Range, offset) {
			word.Value, word.Text, word.Quoted, word.Range, word.IsKey = level.Key, st.Key.Text, st.Key.Quoted, st.Key.Range, true
			if word.Value.Type == schema.TypeNumber {
				word.Weight = weight(descent, st)
			}
		} else if scalar, ok := st.Value.(*jomini.Scalar); ok && within(scalar.Range, offset) {
			word.Value, word.Text, word.Quoted, word.Range = reader.Value(level.Below, scalar.Text, scalar.Quoted), scalar.Text, scalar.Quoted, scalar.Range
		} else {
			return nil
		}
	case *jomini.ValueStmt:
		scalar, ok := st.Value.(*jomini.Scalar)
		if !ok || !within(scalar.Range, offset) {
			return nil
		}
		word.Value, word.Text, word.Quoted, word.Range = reader.Value(level.Field, scalar.Text, scalar.Quoted), scalar.Text, scalar.Quoted, scalar.Range
	default:
		return nil
	}

	// a $PARAM$ the cursor is inside outranks the word around it
	for _, span := range jomini.Interpolations(word.Text) {
		if within(jomini.Range{Start: word.Range.Start + span.Start, End: word.Range.Start + span.End}, offset) {
			word.Value, word.Range = schema.Value{Type: schema.TypeParam, Name: span.Name}, jomini.Range{Start: word.Range.Start + span.Start, End: word.Range.Start + span.End}
		}
	}
	return word
}

// within reports an offset inside a span, end included, since the cursor often sits after it.
func within(span jomini.Range, offset int) bool {
	return span.Start <= offset && offset <= span.End
}

// weight is a numbered branch's percent of its siblings (random_list = { 90 = { } 10 = { } }),
// and 0 for a number under no declared token (a gene's 20 = empty).
func weight(descent *index.Descent, st *jomini.Assignment) int {
	if len(descent.Chain) < 2 || descent.Chain[len(descent.Chain)-2].Key.Type != schema.TypeToken {
		return 0
	}
	parent, ok := descent.Chain[len(descent.Chain)-2].Statement.(*jomini.Assignment)
	if !ok {
		return 0
	}
	sum, mine := 0.0, 0.0
	for _, sibling := range jomini.BlockOf(parent.Value).Statements {
		if assignment, ok := sibling.(*jomini.Assignment); ok && jomini.Literal(assignment.Key.Text) == jomini.LitNumber {
			n, _ := strconv.ParseFloat(assignment.Key.Text, 64)
			sum += n
			if assignment == st {
				mine = n
			}
		}
	}
	if sum == 0 {
		return 0
	}
	return int(mine*100/sum + 0.5)
}

// Position and Range are the editor's: 0-based line and byte column.
type Position = jomini.Position

// Range is a span of the file.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a place in a file of the workspace or the install.
type Location struct {
	Path   string `json:"path"`
	Origin string `json:"origin"` // game.OriginVanilla or a mod's id
	Rel    string `json:"rel"`
	Range  Range  `json:"range"`
}

// Inspect is the hover card: the reader's reading of the word and what the workspace knows of it.
type Inspect struct {
	Value    schema.Value      `json:"value"`
	Key      string            `json:"key,omitempty"`      // the key the word is written at, or is
	Class    string            `json:"class,omitempty"`    // a key's shape: value, list, structure, database, body
	Accepts  []string          `json:"accepts,omitempty"`  // a key: the value types it accepts, as the schema spells them
	Scope    string            `json:"scope,omitempty"`    // the scope type the site runs in
	ScopeOut string            `json:"scopeOut,omitempty"` // a link or an iterator: the scope type it leads to
	Docs     string            `json:"docs,omitempty"`     // prose: a token's or field's docs, or a definition's comment
	Usage    string            `json:"usage,omitempty"`    // a token's declared usage
	Label    string            `json:"label,omitempty"`    // a definition's label, a loc key's text, a constant's value
	Weight   int               `json:"weight,omitempty"`   // a numbered branch: its share of its siblings, in percent
	Date     string            `json:"date,omitempty"`     // a date written out; with a mod calendar, the era's date too
	Color    string            `json:"color,omitempty"`    // a colour, as #rrggbb
	Image    *source.ImageInfo `json:"image,omitempty"`    // a file that is an image: its header
	Sites    []Location        `json:"sites,omitempty"`    // where it is defined, in load order
	Winner   int               `json:"winner"`             // the site the game keeps; -1 for none
	Graphed  bool              `json:"graphed,omitempty"`  // the event graph draws it: an event or on_action, or it fires one
	Values   []Seen            `json:"values,omitempty"`   // a parameter: the words its macro's callers pass for it
	Range    Range             `json:"range"`
}

// Seen is one word and how often it was written.
type Seen struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// Signature is a token's declared usage with its parameters.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`
	Docs       string   `json:"docs,omitempty"`
}
