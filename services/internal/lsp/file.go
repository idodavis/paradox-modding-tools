// file.go is what every request starts from: the open file, the cursor's place in it from one walk
// that stops there, and the editor's positions and locations.

// Package lsp answers the editor's requests for one open file: the card for what is under the
// cursor, where it is defined and used, what may be written there, what is wrong, and how the file
// is folded, formatted and annotated. Every answer starts from one walk of the file over the schema
// (index.Collect), stopped at the cursor or run whole; payloads carry display data and the frontend
// templates the text.
package lsp

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// openFile is a file open in one request: its text, its parse and where it sits in the workspace.
type openFile struct {
	ws     *workspace.Session
	path   string
	text   string
	parsed *jomini.Result
	place  workspace.FilePlace
}

// open reads the file at a path as the request sees it: the editor's text where the file is open
// there, else the disk's, parsed once per text, and placed in the workspace.
func open(ws *workspace.Session, path string) (*openFile, error) {
	parsed, text, err := ws.Parsed(path)
	if err != nil {
		return nil, err
	}
	return &openFile{ws: ws, path: path, text: text, parsed: parsed, place: ws.PlaceOf(path)}, nil
}

// span turns a byte span of the file into the editor's range.
func (file *openFile) span(span jomini.Range) Range {
	return Range{Start: file.parsed.Lines().PositionAt(span.Start), End: file.parsed.Lines().PositionAt(span.End)}
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

// locate gives the editor's location of a position in a file of the workspace or the install.
func locate(ws *workspace.Session, path string, at Position) Location {
	place, _, _ := ws.Locate(path)
	return Location{Path: path, Origin: place.Origin, Rel: place.Rel, Range: Range{Start: at, End: at}}
}

// siteOf gives where a definition is written.
func siteOf(ws *workspace.Session, def index.At) Location {
	return locate(ws, def.File().Path, Position{Line: int(def.Occurrence().Line), Character: int(def.Occurrence().Col)})
}

// step is one statement of the chain under the cursor: the statement, the context it is written in
// and the one its block reads in.
type step struct {
	statement jomini.Statement
	context   schema.Context
	inner     schema.Context
}

// command is the declared effect, trigger or link the step's key is, by the rule that read it; nil
// for none.
func (step step) command(s *schema.Schema) *schema.Command {
	assign, isAssign := step.statement.(*jomini.Assignment)
	if !isAssign {
		return nil
	}
	switch step.inner.Rule {
	case game.KeyRuleCommand:
		return s.Commands[strings.ToLower(assign.Key.Text)]
	case game.KeyRuleLink:
		return s.Link(strings.ToLower(assign.Key.Text))
	}
	return nil
}

// cursor is the cursor's place in a script file, from one walk that stops past it: the statements
// holding it, outer to inner, and the occurrences the walk read up to it.
type cursor struct {
	file        *openFile
	chain       []step
	occurrences []schema.Occurrence
	offset      int
}

// walkTo walks the file up to an offset. A statement holds the offset when its span does, or when
// it ends at an operator with only blanks up to the offset: a value still to come.
func (file *openFile) walkTo(offset int) cursor {
	cursor := cursor{file: file, offset: offset}
	cursor.occurrences, _ = index.Collect(file.ws.Install.Schema, file.place.File, file.parsed,
		func(statement jomini.Statement, context schema.Context, inner *schema.Context, _ []schema.Occurrence) bool {
			span := statement.NodeRange()
			if span.Start > offset {
				return false
			}
			assign, isAssign := statement.(*jomini.Assignment)
			valueToCome := isAssign && assign.Value == nil && strings.TrimSpace(file.text[span.End:offset]) == ""
			if offset <= span.End || valueToCome {
				cursor.chain = append(cursor.chain, step{statement, context, *inner})
			}
			return true
		})
	return cursor
}

// blockAt is the index in the chain of the innermost statement whose block holds the cursor, -1 at
// the file's top; that statement's inner context says how its block reads.
func (cursor cursor) blockAt() int {
	holder := -1
	for i, step := range cursor.chain {
		block := jomini.ChildBlock(step.statement)
		if block != nil && cursor.offset > block.OpenBrace && (block.CloseBrace < 0 || cursor.offset <= block.CloseBrace) {
			holder = i
		}
	}
	return holder
}

// within is the entry the cursor is in, then the keys of the blocks between it and the cursor; nil
// outside an entry. The entry's own statement is its name, not a key; an entry with no name starts
// with its first key.
func (cursor cursor) within() []string {
	holder := cursor.blockAt()
	if holder < 0 || cursor.chain[holder].context.Entry < 0 {
		return nil
	}
	entry := cursor.chain[holder].context.Entry
	def := cursor.occurrences[entry]
	out := []string{def.ID}
	for _, step := range cursor.chain[:holder+1] {
		if assign, isAssign := step.statement.(*jomini.Assignment); isAssign && step.context.Entry == entry && assign.Key.Range != def.Range {
			out = append(out, assign.Key.Text)
		}
	}
	if def.ID == "" {
		return out[1:]
	}
	return out
}

// scalarAt is the scalar under the cursor (the innermost statement's key or value; nil for none),
// whether it is the key, and the definition or reference the walk read inside it ($NAME$ included):
// of several references (a call's parameter and the call), the first that resolves, else the first;
// a definition with a name is named as itself.
func (cursor cursor) scalarAt() (scalar *jomini.Scalar, isKey bool, ref *schema.Occurrence) {
	if len(cursor.chain) == 0 {
		return nil, false, nil
	}
	ws, at := cursor.file.ws, cursor.chain[len(cursor.chain)-1]
	scalar, _ = jomini.ValueOf(at.statement).(*jomini.Scalar)
	if assign, isAssign := at.statement.(*jomini.Assignment); isAssign && covers(assign.Key.Range, cursor.offset) {
		scalar, isKey = &assign.Key, true
	}
	if scalar == nil || !covers(scalar.Range, cursor.offset) {
		return nil, false, nil
	}
	inScalar := func(span jomini.Range) bool {
		return scalar.Range.Start <= span.Start && span.End <= scalar.Range.End && covers(span, cursor.offset)
	}
	resolves := func(occurrence schema.Occurrence) bool {
		if occurrence.Role != schema.Refers || !inScalar(occurrence.Range) {
			return false
		}
		_, known := index.Resolve(ws.Install.Schema, ws.Indexes(), occurrence, cursor.file.place.Path)
		return known
	}
	refers := func(occurrence schema.Occurrence) bool {
		return occurrence.Role == schema.Refers && inScalar(occurrence.Range)
	}
	defines := func(occurrence schema.Occurrence) bool {
		return occurrence.Role == schema.Defines && occurrence.ID != "" && inScalar(occurrence.Range)
	}
	if i := slices.IndexFunc(cursor.occurrences, resolves); i >= 0 {
		return scalar, isKey, &cursor.occurrences[i]
	}
	if i := slices.IndexFunc(cursor.occurrences, refers); i >= 0 {
		return scalar, isKey, &cursor.occurrences[i]
	}
	if i := slices.IndexFunc(cursor.occurrences, defines); i >= 0 {
		def := cursor.occurrences[i]
		return scalar, isKey, &schema.Occurrence{Database: def.Database, ID: def.ID}
	}
	return scalar, isKey, nil
}

// covers reports an offset inside a span, end included, since the cursor often sits after it.
func covers(span jomini.Range, offset int) bool {
	return span.Start <= offset && offset <= span.End
}

// locRef is what an offset of a localization file names: a reference the scan read ($key$, a
// concept), else the line's key; with neither, the data call steps it is in, if any.
func (file *openFile) locRef(offset int) (ref *schema.Occurrence, steps []jomini.CallStep) {
	lines := file.parsed.Lines()
	start := lines.OffsetAt(lines.PositionAt(offset).Line, 0)
	line, _, _ := strings.Cut(file.text[start:], "\n")
	line = strings.TrimRight(line, "\r")
	for _, index := range file.ws.Indexes() {
		for _, at := range index.InFile(file.path) {
			if occurrence := at.Occurrence(); occurrence.Role == schema.Refers && covers(occurrence.Range, offset) {
				ref = &occurrence
			}
		}
	}
	if steps = dataCallAt(line, start, offset); ref == nil && steps != nil {
		return nil, steps
	}
	if ref == nil {
		if key, _, isEntry := strings.Cut(strings.TrimSpace(line), ":"); isEntry && !strings.HasPrefix(key, "l_") && !strings.HasPrefix(key, "#") {
			keyStart := start + strings.Index(line, key)
			if span := (jomini.Range{Start: keyStart, End: keyStart + len(key)}); covers(span, offset) {
				ref = &schema.Occurrence{Role: schema.Defines, Database: schema.Localization, ID: key, Range: span}
			}
		}
	}
	return ref, nil
}

// dataCallAt is the data call step at an offset of a text that starts at start: the steps up to it;
// nil where the offset is in no [ … ] step.
func dataCallAt(text string, start, offset int) []jomini.CallStep {
	for _, segment := range jomini.Calls(text, start) {
		steps := jomini.ParseCall(text[segment.Start-start:segment.End-start], segment.Start)
		for i, step := range steps {
			if covers(step.Range, offset) {
				return steps[:i+1]
			}
		}
	}
	return nil
}

// stepsLeadTo is the data type a data call's steps lead to: from the global functions, or a data
// type written first, through each function's return type; "" where a step is unknown.
func stepsLeadTo(types map[string]schema.DataType, steps []jomini.CallStep) string {
	typeName := game.GlobalType
	for i, step := range steps {
		function, isFunction := types[typeName].Functions[step.Name]
		_, isType := types[step.Name]
		switch {
		case isFunction:
			typeName = function.Returns
		case i == 0 && isType:
			typeName = step.Name
		default:
			return ""
		}
	}
	return typeName
}
