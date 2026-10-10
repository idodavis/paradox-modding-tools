// collect.go is step 5: one file's occurrences from the schema's walk, for the install's files after the
// schema is built, for a mod file and for an open file; and the worker pool that collects every file of an
// index.

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

// collector is one file's occurrences as the walk fills them, the scope pairs it writes, and the visitor
// they are handed to.
type collector struct {
	schema      *schema.Schema
	occurrences []schema.Occurrence
	scoped      map[[2]string]bool
	visit       func(*schema.Placement) bool
	stopped     bool
}

// collectFiles reads the files of Files that hold occurrences, localization in every language only when
// withLoc is set: a worker per CPU parses and walks ahead while add runs in file order here.
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
	return inventory.Ordered(ctx, files, func(file inventory.File) collected {
		text, _ := file.Text()
		return collectFile(s, file, text)
	}, func(file inventory.File, records collected) {
		if len(records.occurrences) > 0 {
			index.add(file.Path, records)
		}
		done++
		if progress != nil {
			progress(done, len(files))
		}
	})
}

// collectFile returns one file's occurrences by its format: script walked, a table or sound list walked
// without a tree, localization in every language; a format that holds none gives no lines.
func collectFile(s *schema.Schema, file inventory.File, text string) collected {
	switch file.Format {
	case inventory.Script:
		parsed := jomini.Parse(text)
		occurrences, scoped := Collect(s, file, &parsed, nil)
		return collected{occurrences: occurrences, scoped: scoped}
	case inventory.Table, inventory.List:
		occurrences, scoped := Collect(s, file, nil, nil)
		return collected{occurrences: occurrences, scoped: scoped}
	case inventory.Loc:
		return collectLoc(s.Game, file, text, nil)
	}
	return collected{}
}

// Collect walks a file with the schema and collects its occurrences: each entry a definition first, in
// source order, then what the walk's statements name, each in its entry, each with its line and column; and
// the scope each command, link or link-read word is written in. visit (nil for none) sees each reading;
// returning false stops the walk. A table or sound list (parsed nil) is read for its lines.
func Collect(s *schema.Schema, file inventory.File, parsed *jomini.Result, visit func(*schema.Placement) bool) ([]schema.Occurrence, map[[2]string]bool) {
	c := &collector{schema: s, visit: visit, scoped: map[[2]string]bool{}}
	entries := s.FindEntries(file, parsed)
	for n, entry := range entries {
		span := entry.Statement.NodeRange()
		if assign, isAssign := entry.Statement.(*jomini.Assignment); isAssign {
			span = assign.Key.Range
		}
		c.occurrences = append(c.occurrences, schema.Occurrence{
			Role: schema.Defines, Database: entry.Database, ID: entry.ID, Mode: entry.Mode, Range: span, Entry: int32(n), Local: entry.Local,
		})
	}
	s.WalkFile(file, parsed, entries, schema.Visitor{Visit: c.collect})
	if len(c.occurrences) > 0 {
		lines := (*jomini.LineIndex)(nil)
		if parsed != nil {
			lines = parsed.Lines()
		} else {
			text, _ := file.Text()
			lines = jomini.NewLineIndex(text)
		}
		for n := range c.occurrences {
			at := lines.PositionAt(c.occurrences[n].Range.Start)
			c.occurrences[n].Line, c.occurrences[n].Col = int32(at.Line), int32(at.Character)
		}
	}
	return c.occurrences, c.scoped
}

// collect keeps what a reading names and the scope its command, link or link-read word is written in,
// then hands the reading to visit; once visit returns false the walk reads nothing more.
func (collector *collector) collect(placement *schema.Placement) bool {
	if collector.stopped {
		return false
	}
	collector.occurrences = append(collector.occurrences, placement.Occurrences...)
	assign, isAssign := placement.Statement.(*jomini.Assignment)
	scalar, isScalar := jomini.ValueOf(placement.Statement).(*jomini.Scalar)
	if placement.Scope != "" && isAssign && placement.Command != nil {
		collector.scoped[scopePair(assign.Key.Text, placement.Scope)] = true
	}
	if placement.Scope != "" && isScalar && collector.schema.Link(strings.ToLower(scalar.Text)) != nil {
		collector.scoped[scopePair(scalar.Text, placement.Scope)] = true
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

// collectLoc reads a localization file's entries, nothing when its language is not kept (languages
// none: every one). A loc text is cloned so no occurrence pins the file; its $key$ names a loc key, and
// its concepts a game concept (conceptReferences): each a reference in no entry.
func collectLoc(gameID string, file inventory.File, text string, languages []string) collected {
	parsed := loc.Parse(text)
	language := strings.ToLower(cmp.Or(parsed.Language, loc.LanguageFromRel(file.Rel)))
	if len(languages) > 0 && !slices.Contains(languages, language) {
		return collected{}
	}
	concepts := ""
	for at, row := range game.Get(gameID).Folders {
		if row.Concepts {
			concepts = schema.NameOf(at)
		}
	}
	records, lines := collected{language: language}, jomini.NewLineIndex(text)
	refer := func(database, name string, span jomini.Range) {
		at := lines.PositionAt(span.Start)
		records.occurrences = append(records.occurrences, schema.Occurrence{
			Role: schema.Refers, Database: database, ID: name, Range: span, Line: int32(at.Line), Col: int32(at.Character), Entry: -1,
		})
	}
	for _, entry := range parsed.Entries {
		records.occurrences = append(records.occurrences, schema.Occurrence{
			Role: schema.Localizes, Database: schema.Localization, ID: entry.Key, Range: entry.KeyRange, Line: int32(entry.Line), Entry: -1,
		})
		records.texts = append(records.texts, strings.Clone(entry.Value))
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
