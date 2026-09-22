// probe.go is the one descent to the cursor and what the cursor is on: index.Probe walks the statements around an offset the way the harvest walks a whole file, and identify reads the word there with the reader, plus the facts a position adds (a $PARAM$ span, a numbered branch's weight, a bare word in a list).
package lsp

import (
	"strconv"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
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
	s      *session.Session
	path   string
	text   string
	parsed *jomini.Result
	place  session.Place
}

// open parses a file (once per text for an open buffer) and places it.
func open(s *session.Session, path string) (*file, error) {
	parsed, text, err := s.Parsed(path)
	if err != nil {
		return nil, err
	}
	return &file{s: s, path: path, text: text, parsed: parsed, place: s.Where(path)}, nil
}

// offset is the byte offset of a line and column of the file.
func (f *file) offset(line, col int) int {
	return f.parsed.Lines().OffsetAt(line, col)
}

// span is the editor's range of a span of the file.
func (f *file) span(r jomini.Range) Range {
	return Range{Start: f.parsed.Lines().PositionAt(r.Start), End: f.parsed.Lines().PositionAt(r.End)}
}

// at is the word under an offset of a script file, or nil where there is none: a comment, a brace, a blank.
func (f *file) at(offset int) *site {
	if len(f.place.Rel) == 0 {
		return nil
	}
	found, _, _ := f.s.Located(f.path)
	d := index.Probe(found, f.text, f.s.Reader(), offset)
	if len(d.Chain) == 0 {
		return nil
	}
	level := d.Chain[len(d.Chain)-1]
	r := f.s.Reader()
	w := &site{Level: level, Path: d}

	switch st := level.Statement.(type) {
	case *jomini.Assignment:
		if within(st.Key.Range, offset) {
			w.Value, w.Text, w.Quoted, w.Range, w.IsKey = level.Key, st.Key.Text, st.Key.Quoted, st.Key.Range, true
			if w.Value.Type == schema.TypeNumber {
				w.Weight = weight(d, st)
			}
		} else if scalar, ok := st.Value.(*jomini.Scalar); ok && within(scalar.Range, offset) {
			w.Value, w.Text, w.Quoted, w.Range = r.Value(level.Below, scalar.Text, scalar.Quoted), scalar.Text, scalar.Quoted, scalar.Range
		} else {
			return nil
		}
	case *jomini.ValueStmt:
		scalar, ok := st.Value.(*jomini.Scalar)
		if !ok || !within(scalar.Range, offset) {
			return nil
		}
		w.Value, w.Text, w.Quoted, w.Range = r.Value(level.Field, scalar.Text, scalar.Quoted), scalar.Text, scalar.Quoted, scalar.Range
	default:
		return nil
	}

	// a $PARAM$ the cursor is inside outranks the word around it
	for _, span := range jomini.Interpolations(w.Text) {
		if within(jomini.Range{Start: w.Range.Start + span.Start, End: w.Range.Start + span.End}, offset) {
			w.Value, w.Range = schema.Value{Type: schema.TypeParam, Name: span.Name}, jomini.Range{Start: w.Range.Start + span.Start, End: w.Range.Start + span.End}
		}
	}
	return w
}

// within reports an offset inside a span, its end included, because the cursor sits after the last byte as often as on it.
func within(r jomini.Range, offset int) bool {
	return r.Start <= offset && offset <= r.End
}

// weight is a numbered branch's share of its siblings in percent (random_list = { 90 = { } 10 = { } }), and 0 for a number written under no declared token (a gene's 20 = empty).
func weight(d *index.Descent, st *jomini.Assignment) int {
	if len(d.Chain) < 2 || d.Chain[len(d.Chain)-2].Key.Type != schema.TypeToken {
		return 0
	}
	parent, ok := d.Chain[len(d.Chain)-2].Statement.(*jomini.Assignment)
	if !ok {
		return 0
	}
	sum, mine := 0.0, 0.0
	for _, sibling := range jomini.BlockOf(parent.Value).Statements {
		if a, ok := sibling.(*jomini.Assignment); ok && jomini.Literal(a.Key.Text) == jomini.LitNumber {
			n, _ := strconv.ParseFloat(a.Key.Text, 64)
			sum += n
			if a == st {
				mine = n
			}
		}
	}
	if sum == 0 {
		return 0
	}
	return int(mine*100/sum + 0.5)
}
