// messages.go judges localization files: the file as a whole, its messages, and the markup, names and data calls each
// message's text writes.

package lsp

import (
	"strings"
	"unicode"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// diagnoseMessages returns the problems of a localization file, only parse errors for a game file. It judges each
// message's markup, the names it uses and its data calls, and flags duplicate keys, except in a replace folder.
func (visitor *diagnoseVisitor) diagnoseMessages(document workspace.Document) []Diagnostic {
	place, mods := visitor.place, visitor.index
	tree, file := document.Tree, document.Messages
	visitor.diagnostics = toDiagnostics(tree, file.Errors)
	if visitor.parseOnly {
		return visitor.diagnostics
	}
	visitor.diagnostics = append(visitor.diagnostics, diagnoseLocalizationFile(tree, file, place)...)
	for _, message := range file.Messages {
		segments := loc.Segments(message.Text, message.TextSpan.Start)
		visitor.diagnostics = append(visitor.diagnostics, toDiagnostics(tree, loc.ListMarkupFaults(segments))...)
		visitor.diagnoseMissing(tree, index.ListTextReferences(visitor.ws.Schema, message))
		visitor.diagnoseCalls(tree, segments, "")
	}
	if _, replaces := visitor.ws.Game.FindLocReplacement(place.Rel, file.Language); replaces {
		return visitor.diagnostics
	}
	counts := lo.CountValuesBy(file.Messages, func(message loc.Message) string { return message.Key })
	for _, message := range file.Messages {
		others := lo.CountBy(mods.ListMessages(message.Key), func(site index.Site) bool {
			other := mods.File(site)
			_, replaces := visitor.ws.Game.FindLocReplacement(other.Rel, file.Language)
			repeats := other.Origin == place.Origin || other.Origin == game.OriginVanilla && visitor.ws.Game.LogsRepeatedKey
			return site.File != place.Position && repeats && mods.Language(site) == file.Language &&
				mods.IsLoaded(site.File) && !replaces
		})
		if counts[message.Key]+others > 1 {
			facts := DescribeReference(schema.Reference{Database: game.Localization, Name: message.Key})
			facts["language"] = file.Language
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, message.KeySpan, "duplicate-message", facts))
		}
	}
	return visitor.diagnostics
}

// diagnoseLocalizationFile returns the problems of a mod's localization file as a whole.
func diagnoseLocalizationFile(tree *jomini.Tree, file *loc.File, place workspace.Place) []Diagnostic {
	diagnostics := []Diagnostic{}
	add := func(code string, facts map[string]string) {
		diagnostics = append(diagnostics, makeDiagnostic(tree, jomini.Span{}, code, facts))
	}
	if suffix := loc.FindSuffixLanguage(place.Path); suffix != "" && file.Language != "" &&
		!strings.EqualFold(suffix, file.Language) {
		add("loc-header-mismatch", map[string]string{"language": suffix, "header": file.Language})
	}
	diagnostics = append(diagnostics, diagnoseFolders(tree, place)...)
	if disk, err := files.ReadText(place.Path); err != nil || disk.Encoding != files.EncodingUTF8BOM {
		add("missing-bom", map[string]string{})
	}
	return diagnostics
}

// diagnoseFolders returns a problem for each misspelled localization folder in a file's path.
func diagnoseFolders(tree *jomini.Tree, place workspace.Place) []Diagnostic {
	diagnostics := []Diagnostic{}
	folders := strings.Split(strings.ToLower(place.Rel), "/")
	for _, folder := range folders[:len(folders)-1] {
		if right, misspelled := game.MisspelledFolders[folder]; misspelled {
			diagnostics = append(diagnostics, makeDiagnostic(tree, jomini.Span{}, "wrong-localization-folder",
				map[string]string{"wrong": folder, "right": right}))
		}
	}
	return diagnostics
}

// diagnoseCalls records a problem for each data call in the segments of a text that has a step no dump declares for
// the type before it, read in the given data context. A first step in lower case is a saved scope, so it is not a
// problem. A step is also known if the judged index defines its name in one of game.GUI.StepDatabases, or if the game's
// own text writes it after the same type.
func (visitor *diagnoseVisitor) diagnoseCalls(tree *jomini.Tree, segments []loc.Segment, datacontext string) {
	for _, call := range loc.ListCalls(segments) {
		step, before, ok := visitor.ws.Schema.FindUnlisted(call, datacontext)
		if !ok {
			continue
		}
		name := call.Steps[step].Text
		saved := step == 0 && name != "" && unicode.IsLower(rune(name[0]))
		isDefined := func(database string) bool {
			return len(visitor.index.Resolve(schema.Reference{Database: database, Name: name})) > 0
		}
		if saved || visitor.ws.Schema.UnlistedCalls[before+"."+name] || lo.SomeBy(game.GUI.StepDatabases, isDefined) {
			continue
		}
		facts := DescribeReference(schema.Reference{Name: name})
		facts["type"] = before
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, call.Steps[step].Span, "unknown-data-call", facts))
	}
}
