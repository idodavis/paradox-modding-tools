// collect.go is step 5: one file's records from the schema's walk, and the worker pool that
// collects every file of an index.

package index

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
)

// collectFiles reads the files of Files that hold records, localization in every language only when
// withLoc is set: a worker per CPU parses and walks ahead while Add runs in file order here.
func (index *Index) collectFiles(ctx context.Context, s *schema.Schema, withLoc bool, progress func(done, total int)) error {
	index.Rebuild()
	var files []inventory.File
	for _, file := range index.Files {
		if slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format) ||
			withLoc && file.Format == inventory.Loc {
			files = append(files, file)
		}
	}

	done := 0
	return inventory.Ordered(ctx, files, func(file inventory.File) *Records {
		text, _ := file.Text()
		return collectFile(s, file, text)
	}, func(file inventory.File, records *Records) {
		if records != nil {
			index.Add(records, file.Path)
		}
		done++
		if progress != nil {
			progress(done, len(files))
		}
	})
}

// Collect walks a file with the schema and collects its records: each entry a definition, each naming
// a definition or a reference with its file, line and entry, and the scope each command, link or
// link-read word is written in. visit (nil for none) sees each reading; returning false stops the
// walk.
func Collect(s *schema.Schema, file inventory.File, parsed *jomini.Result, visit func(*schema.Placement) bool) *Records {
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
		c.define(Def{Database: entry.Database, Name: entry.ID, Mode: entry.Mode, Range: span, Local: entry.Local})
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
	visit   func(*schema.Placement) bool
	stopped bool
}

// position returns the line and column an offset starts, 0-based; a table or sound list is read for
// its lines the first time.
func (collector *collector) position(offset int) (line, col int32) {
	if collector.lines == nil {
		text, _ := collector.file.Text()
		collector.lines = jomini.NewLineIndex(text)
	}
	at := collector.lines.PositionAt(offset)
	return int32(at.Line), int32(at.Character)
}

// define adds a definition where it is written. Entries come first, in the detector's order, so an
// entry's index in Defs is the Placement.Entry a reference stores as From.
func (collector *collector) define(def Def) {
	def.Path = collector.file.Path
	def.Line, def.Col = collector.position(def.Range.Start)
	collector.records.Defs = append(collector.records.Defs, def)
}

// refer adds a reference where it is written, in the entry the reading is in.
func (collector *collector) refer(ref Ref, placement *schema.Placement) {
	ref.Path, ref.From, ref.InTrigger = collector.file.Path, int32(placement.Entry), placement.InTrigger
	ref.Line, ref.Col = collector.position(ref.Range.Start)
	collector.records.Refs = append(collector.records.Refs, ref)
}

// collect records what a reading names and the scope its command, link or link-read word is written
// in, then hands the reading to visit; once visit returns false the walk reads nothing more.
func (collector *collector) collect(placement *schema.Placement) bool {
	if collector.stopped {
		return false
	}
	for _, naming := range placement.Mentions {
		if naming.Defines {
			collector.define(Def{Database: naming.Database, Name: naming.ID, Range: naming.Range, Named: naming.Named})
		} else {
			collector.refer(Ref{Database: naming.Database, Name: naming.ID, Called: naming.Called, Range: naming.Range}, placement)
		}
	}
	assign, isAssign := placement.Statement.(*jomini.Assignment)
	scalar, isScalar := jomini.ValueOf(placement.Statement).(*jomini.Scalar)
	if placement.Scope != "" && isAssign && placement.Command != nil {
		collector.records.scoped[scopePair(assign.Key.Text, placement.Scope)] = true
	}
	if placement.Scope != "" && isScalar && collector.schema.Link(strings.ToLower(scalar.Text)) != nil {
		collector.records.scoped[scopePair(scalar.Text, placement.Scope)] = true
	}
	if collector.visit != nil && !collector.visit(placement) {
		collector.stopped = true
	}
	return !collector.stopped
}

// scopePair is a written command, link or link-read word and the scope it is read in, as
// Index.Scoped keeps them: the first part's name before any colon, lower-case (scope:x.liege is
// scope).
func scopePair(written, scope string) [2]string {
	first, _, _ := strings.Cut(written, ".")
	name, _, _ := strings.Cut(first, ":")
	return [2]string{strings.ToLower(name), scope}
}

// collectFile returns one file's records by its format: script walked, a table or sound list walked
// without a tree, localization in every language; nil when the format holds none.
func collectFile(s *schema.Schema, file inventory.File, text string) *Records {
	switch file.Format {
	case inventory.Script:
		parsed := jomini.Parse(text)
		return Collect(s, file, &parsed, nil)
	case inventory.Table, inventory.List:
		return Collect(s, file, nil, nil)
	case inventory.Loc:
		return collectLoc(s.Game, file, text, nil)
	}
	return nil
}

// collectLoc reads a localization file's entries, nil when its language is not kept (languages
// none: every one). A loc text is cloned so no record pins the file; its $key$ names a loc key, and
// its concepts a game concept (conceptReferences): each a reference in no definition.
func collectLoc(gameID string, file inventory.File, text string, languages []string) *Records {
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

// conceptReferences refers to each game concept a loc entry's data calls name: [x|E] and
// Concept('x', …) name one in the concepts database.
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
