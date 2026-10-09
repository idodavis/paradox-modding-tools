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

// recordsOf returns one file's records by its format: script walked, a table or sound list walked without a
// tree, localization parsed with no schema (nil before the schema stage); nil when the format holds
// none or the language is not kept (languages none: every one).
func recordsOf(gameID string, s *schema.Schema, file inventory.File, text string, languages []string) *Records {
	switch file.Format {
	case inventory.Script:
		parsed := jomini.Parse(text)
		return Walk(s, file, &parsed, nil)
	case inventory.Table, inventory.List:
		return Walk(s, file, nil, nil)
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
			refer(Localization, interp.Key, interp.KeyRange)
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
