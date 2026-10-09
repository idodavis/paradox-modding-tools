// records.go turns one file into its records: script through the schema's walk, localization through its own
// reader.

package index

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
)

// Read walks a file with the schema and collects its records: each entry a definition, each naming a definition
// or a reference with its file, line and entry, and the scope each command, link or link-read word is written in.
// visit (nil for none) sees each reading; returning false stops the walk.
func Read(s *schema.Schema, file inventory.File, parsed *jomini.Result, visit func(*schema.Reading) bool) *Records {
	c := &collector{schema: s, file: file, visit: visit, records: &Records{scoped: map[[2]string]bool{}}}
	if parsed != nil {
		c.lines = parsed.Lines()
	}
	entries := s.FindEntries(file, parsed)
	for _, entry := range entries {
		span := entry.Statement.NodeRange()
		if assign, isAssign := entry.Statement.(*jomini.Assignment); isAssign {
			span = assign.Key.Range
		}
		c.define(Def{Database: entry.Database, Name: entry.Name, Mode: entry.Mode, Range: span, Local: entry.Local})
	}
	s.WalkFile(file, parsed, entries, schema.Visitor{Visit: c.collect})
	return c.records
}

// collector is one file's records as the walk fills them, and the visitor they are handed to.
type collector struct {
	schema  *schema.Schema
	file    inventory.File
	records *Records
	lines   *jomini.LineIndex // the parse's; a table or sound list is read for them
	visit   func(*schema.Reading) bool
	stopped bool
}

// position returns the line and column an offset starts, 0-based; a table or sound list is read for its lines
// the first time.
func (c *collector) position(offset int) (line, col int32) {
	if c.lines == nil {
		text, _ := c.file.Text()
		c.lines = jomini.NewLineIndex(text)
	}
	at := c.lines.PositionAt(offset)
	return int32(at.Line), int32(at.Character)
}

// define adds a definition where it is written. Entries come first, in the detector's order, so an entry's index
// in Defs is the Reading.Entry a reference stores as From.
func (c *collector) define(def Def) {
	def.Path = c.file.Path
	def.Line, def.Col = c.position(def.Range.Start)
	c.records.Defs = append(c.records.Defs, def)
}

// refer adds a reference where it is written, in the entry the reading is in.
func (c *collector) refer(ref Ref, r *schema.Reading) {
	ref.Path, ref.From, ref.InTrigger = c.file.Path, int32(r.Entry), r.InTrigger
	ref.Line, ref.Col = c.position(ref.Range.Start)
	c.records.Refs = append(c.records.Refs, ref)
}

// collect records what a reading names and the scope its command, link or link-read word is written in, then
// hands the reading to visit; once visit returns false the walk reads nothing more.
func (c *collector) collect(r *schema.Reading) bool {
	if c.stopped {
		return false
	}
	for _, naming := range r.Names {
		if naming.Defines {
			c.define(Def{Database: naming.Database, Name: naming.Name, Range: naming.Range, Named: naming.Named})
		} else {
			c.refer(Ref{Database: naming.Database, Name: naming.Name, Called: naming.Called, Range: naming.Range}, r)
		}
	}
	assign, isAssign := r.Statement.(*jomini.Assignment)
	scalar, isScalar := jomini.ValueOf(r.Statement).(*jomini.Scalar)
	if r.Scope != "" && isAssign && r.Command != nil {
		c.records.scoped[scopePair(assign.Key.Text, r.Scope)] = true
	}
	if r.Scope != "" && isScalar && c.schema.Link(strings.ToLower(scalar.Text)) != nil {
		c.records.scoped[scopePair(scalar.Text, r.Scope)] = true
	}
	if c.visit != nil && !c.visit(r) {
		c.stopped = true
	}
	return !c.stopped
}

// scopePair is a written command, link or link-read word and the scope it is read in, as Index.Scoped keeps
// them: the first part's name before any colon, lower-case (scope:x.liege is scope).
func scopePair(written, scope string) [2]string {
	first, _, _ := strings.Cut(written, ".")
	name, _, _ := strings.Cut(first, ":")
	return [2]string{strings.ToLower(name), scope}
}

// recordsOf returns one file's records by its format: script walked, a table or sound list walked without a
// tree, localization parsed with no schema (nil before the schema stage); nil when the format holds
// none or the language is not kept (languages none: every one).
func recordsOf(gameID string, s *schema.Schema, file inventory.File, text string, languages []string) *Records {
	switch file.Format {
	case inventory.Script:
		parsed := jomini.Parse(text)
		return Read(s, file, &parsed, nil)
	case inventory.Table, inventory.List:
		return Read(s, file, nil, nil)
	case inventory.Loc:
		return locRecords(gameID, file, text, languages)
	}
	return nil
}

// locRecords reads a localization file's entries, nil when its language is not kept (languages none: every one).
// A loc text is cloned so no record pins the file; its $key$ names a loc key, and its concepts a game concept
// (conceptReferences): each a reference in no definition.
func locRecords(gameID string, file inventory.File, text string, languages []string) *Records {
	parsed := loc.Parse(text)
	language := strings.ToLower(cmp.Or(parsed.Language, loc.LanguageFromRel(file.Rel)))
	if len(languages) > 0 && !slices.Contains(languages, language) {
		return nil
	}
	concepts := ""
	for at, row := range game.Get(gameID).Folders {
		if row.Concepts {
			concepts = schema.NameOf(at)
		}
	}
	records, lines := &Records{}, jomini.NewLineIndex(text)
	refer := func(database, name string, span jomini.Range) {
		at := lines.PositionAt(span.Start)
		records.Refs = append(records.Refs, Ref{
			Database: database, Name: name, Path: file.Path, Range: span,
			Line: int32(at.Line), Col: int32(at.Character), From: -1,
		})
	}
	for _, entry := range parsed.Entries {
		records.Loc = append(records.Loc, Loc{
			Key: entry.Key, Language: language, Text: strings.Clone(entry.Value), Path: file.Path, Line: entry.Line,
		})
		for _, interp := range loc.Interps(entry.Value, entry.ValueRange.Start) {
			refer(schema.Localization, interp.Key, interp.KeyRange)
		}
		if concepts != "" {
			conceptReferences(text, entry, concepts, refer)
		}
	}
	return records
}

// conceptReferences refers to each game concept a loc entry's data calls name: [x|E] and Concept('x', …) name
// one in the concepts database.
func conceptReferences(
	text string, entry loc.Entry, concepts string, refer func(database, name string, span jomini.Range),
) {
	for _, call := range jomini.Calls(entry.Value, entry.ValueRange.Start) {
		steps := jomini.ParseCall(text[call.Start:call.End], call.Start)
		if len(steps) == 0 {
			continue
		}
		format := strings.TrimLeft(text[steps[0].Range.End:call.End], " \t")
		if len(steps) == 1 && len(steps[0].Args) == 0 && (format == "|E]" || format == "|e]") {
			refer(concepts, steps[0].Name, steps[0].Range)
		}
		for _, step := range steps {
			if step.Name == "Concept" && len(step.Args) > 0 && step.Args[0].Quoted {
				arg := step.Args[0]
				refer(concepts, arg.Text, jomini.Range{Start: arg.Range.Start + 1, End: arg.Range.End - 1})
			}
		}
	}
}
