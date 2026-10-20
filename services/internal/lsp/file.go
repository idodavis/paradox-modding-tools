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

// openFile is a file open in one request.
type openFile struct {
	ws     *workspace.Session
	path   string
	text   string
	parsed *jomini.Result      // its parse
	place  workspace.FilePlace // where it sits in the workspace
}

// open reads the file at a path as the request sees it: the editor's text where the file is open
// there, else the disk's, parsed once per text, and placed in the workspace.
func open(ws *workspace.Session, path string) (*openFile, error) {
	parsed, text, err := ws.Parsed(workspace.Parses{}, path)
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

// Range is a span of a file.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"` // exclusive
}

// Location is a place in a file of the workspace or the install.
type Location struct {
	Path   string `json:"path"`
	Origin string `json:"origin"` // game.OriginVanilla or a mod's id
	Rel    string `json:"rel"`    // its path from its origin's root
	Range  Range  `json:"range"`
}

// Locate returns the editor's location of a range of a file of the workspace or the install.
func Locate(ws *workspace.Session, path string, span Range) Location {
	file, _, _ := ws.Locate(path)
	return Location{Path: path, Origin: file.Origin, Rel: file.Rel, Range: span}
}

// locate returns Locate at a position.
func locate(ws *workspace.Session, path string, at Position) Location {
	return Locate(ws, path, Range{Start: at, End: at})
}

// siteOf gives where a definition is written.
func siteOf(ws *workspace.Session, def index.At) Location {
	return locate(ws, def.File().Path, Position{Line: int(def.Occurrence().Line), Character: int(def.Occurrence().Col)})
}

// step is one statement of the chain under the cursor.
type step struct {
	statement jomini.Statement
	context   schema.Context // where it is written
	inner     schema.Context // where its block reads
}

// command returns the declared effect, trigger or link the step's key is, by the rule that read it; nil for none.
func (step step) command(s *schema.Schema) *schema.Command {
	assign, isAssign := step.statement.(*jomini.Assignment)
	if !isAssign {
		return nil
	}
	switch step.inner.KeyRule {
	case game.KeyRuleCommand:
		return s.Commands[strings.ToLower(assign.Key.Text)]
	case game.KeyRuleLink:
		return s.Link(strings.ToLower(assign.Key.Text))
	}
	return nil
}

// cursor is the cursor's place in a script file, from one walk that stops past it.
type cursor struct {
	file        *openFile
	chain       []step              // the statements holding it, outer to inner
	occurrences []schema.Occurrence // what the walk read up to it
	offset      int                 // its byte offset
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

// blockAt returns the index in the chain of the innermost statement whose block holds the cursor, -1 at the file's top;
// its inner context says how its block reads.
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

// within returns the entry the cursor is in, then the keys of the blocks between; nil outside an entry. The entry's
// own statement is its name, not a key, and a nameless entry starts with its first key.
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

// scalarAt returns the scalar under the cursor (the innermost statement's key or value; nil for none), whether it is
// the key, and the definition or reference the walk read inside it: the first that resolves, else the first.
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

// locRef returns what an offset of a localization file names: a reference the scan read ($key$, a concept), else the
// line's key; with neither, the data call steps it is in.
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
				ref = &schema.Occurrence{Role: schema.Defines, Database: game.Localization, ID: key, Range: span}
			}
		}
	}
	return ref, nil
}

// dataCallAt returns the data call steps up to an offset of a text that starts at start; nil where the offset is in no
// [ … ] step.
func dataCallAt(text string, start, offset int) []jomini.CallStep {
	for _, call := range jomini.DataCalls(text, start) {
		for i, step := range call.Steps {
			if covers(step.Range, offset) {
				return call.Steps[:i+1]
			}
		}
	}
	return nil
}
