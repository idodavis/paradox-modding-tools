// collect.go is one file's occurrences from the schema's walk, for the install's files after the
// schema is built, for a mod file and for an open file; and the worker pool that collects every file of an
// index.

package index

import (
	"context"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
)

// collector gathers one file's occurrences as the walk visits it.
type collector struct {
	schema      *schema.Schema
	occurrences []schema.Occurrence
	scoped      map[[2]string]bool // the scope pairs written
	visit       schema.Visit       // the caller's visitor; nil none
	stopped     bool               // the visitor returned false
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
		return collectFile(s, file, text.Content)
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
		return collectLoc(s.Concepts, file, text, nil)
	}
	return collected{}
}

// Collect walks a file with the schema and returns its occurrences, each entry's definition first, and the scope each
// command or link word is written in; visit, if given, sees each statement and may stop the walk.
func Collect(s *schema.Schema, file inventory.File, parsed *jomini.Result, visit schema.Visit) ([]schema.Occurrence, map[[2]string]bool) {
	c := &collector{schema: s, visit: visit, scoped: map[[2]string]bool{}}
	entries := s.WalkFile(file, parsed, false, c.collect)
	c.occurrences = append(entries, c.occurrences...)
	if len(c.occurrences) > 0 {
		lines := (*jomini.LineIndex)(nil)
		if parsed != nil {
			lines = parsed.Lines()
		} else {
			text, _ := file.Text()
			lines = jomini.NewLineIndex(text.Content)
		}
		for n := range c.occurrences {
			at := lines.PositionAt(c.occurrences[n].Range.Start)
			c.occurrences[n].Line, c.occurrences[n].Col = int32(at.Line), int32(at.Character)
		}
	}
	return c.occurrences, c.scoped
}

// collect keeps what a statement's words name and the scope its command, link or link-read word is written in,
// then hands the statement to visit; once visit returns false the walk reads nothing more.
func (collector *collector) collect(statement jomini.Statement, context schema.Context, inner *schema.Context, occurrences []schema.Occurrence) bool {
	if collector.stopped {
		return false
	}
	collector.occurrences = append(collector.occurrences, occurrences...)
	assign, isAssign := statement.(*jomini.Assignment)
	scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar)
	isCommand := inner.KeyRule == game.KeyRuleCommand || inner.KeyRule == game.KeyRuleLink
	if context.Scope.This != "" && isAssign && isCommand {
		collector.scoped[scopePair(assign.Key.Text, context.Scope.This)] = true
	}
	if context.Scope.This != "" && isScalar && collector.schema.Link(strings.ToLower(scalar.Text)) != nil {
		collector.scoped[scopePair(scalar.Text, context.Scope.This)] = true
	}
	if collector.visit != nil && !collector.visit(statement, context, inner, occurrences) {
		collector.stopped = true
	}
	return !collector.stopped
}

// scopePair returns a written command, link or link-read word with the scope it is read in, as Index.Scoped keeps
// them: the word's part before any colon, in lower case (scope:x.liege is scope).
func scopePair(written, scope string) [2]string {
	first, _, _ := strings.Cut(written, ".")
	name, _, _ := strings.Cut(first, ":")
	return [2]string{strings.ToLower(name), scope}
}

// collectLoc reads a localization file's entries, none when its language is not kept (no languages: every one); texts
// are cloned so no occurrence pins the file, and $key$s and concepts become references in no entry.
func collectLoc(concepts string, file inventory.File, text string, languages []string) collected {
	parsed := loc.Parse(text)
	language := loc.Language(parsed.Language, file.Rel)
	if len(languages) > 0 && !slices.Contains(languages, language) {
		return collected{}
	}
	records, lines := collected{language: language}, parsed.Lines()
	refer := func(database, name string, span jomini.Range) {
		at := lines.PositionAt(span.Start)
		records.occurrences = append(records.occurrences, schema.Occurrence{
			Role: schema.Refers, Database: database, ID: name, Range: span, Line: int32(at.Line), Col: int32(at.Character), Entry: -1,
		})
	}
	for _, entry := range parsed.Entries {
		records.occurrences = append(records.occurrences, schema.Occurrence{
			Role: schema.Localizes, Database: game.Localization, ID: entry.Key, Range: entry.KeyRange, Line: int32(entry.Line), Entry: -1,
		})
		records.texts = append(records.texts, strings.Clone(entry.Value))
		for _, interp := range loc.Interpolations(entry.Value, entry.ValueRange.Start) {
			refer(game.Localization, interp.Key, interp.KeyRange)
		}
		if concepts != "" {
			conceptReferences(text, entry, concepts, refer)
		}
	}
	return records
}

// conceptReferences refers to each game concept a loc entry's data calls mark as one (loc.Concepts) in the
// concepts database.
func conceptReferences(
	text string, entry loc.Entry, concepts string, refer func(database, name string, span jomini.Range),
) {
	for _, call := range jomini.DataCalls(entry.Value, entry.ValueRange.Start) {
		for _, concept := range loc.Concepts(call, text) {
			if concept.IsMarked {
				refer(concepts, concept.Name, concept.Span)
			}
		}
	}
}
