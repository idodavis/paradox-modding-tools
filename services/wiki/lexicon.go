// lexicon.go builds the rows of the script command lexicon.

package wiki

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// LexiconRow is one effect, trigger, link, scope or data function the dumps
// declare, or one scripted effect or trigger.
type LexiconRow struct {
	Class  string   `json:"class"`
	Name   string   `json:"name"`
	Blurb  string   `json:"blurb"`
	Usage  string   `json:"usage"`
	Scopes []string `json:"scopes,omitempty"`
	Target string   `json:"target,omitempty"`

	// Examples are the first uses the index finds, or for a data function the
	// first calls in the game's files.
	Examples []lsp.Site `json:"examples"`

	// Defined is where the game's copy of a scripted effect or trigger is. It
	// is nil for what the dumps declare.
	Defined *lsp.Site `json:"defined,omitempty"`
}

// ListClasses returns the lexicon's classes in the order its tabs show them.
func (s *Service) ListClasses() []lsp.Class { return lsp.Classes }

// BuildLexicon returns the rows of a class, sorted by name, scripted effects
// and triggers included. It returns an error if no workspace is open or the
// class is unknown.
func (s *Service) BuildLexicon(class string) ([]LexiconRow, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mods != mods {
		s.lexicon, s.mods = map[string][]LexiconRow{}, mods
	}

	if rows, ok := s.lexicon[class]; ok {
		return rows, nil
	}

	rows, err := buildLexicon(ws, mods, class)
	if err == nil {
		s.lexicon[class] = rows
	}

	return rows, err
}

func buildLexicon(ws *workspace.Session, mods *index.Index, id string) ([]LexiconRow, error) {
	var rows []LexiconRow
	var class lsp.Class
	for _, candidate := range lsp.Classes {
		if candidate.ID == id {
			class = candidate

			break
		}
	}

	switch {
	case class.Database != "":
		for _, key := range ws.Schema.ListKeys(class.Database) {
			command := ws.Schema.LookupDefinition(class.Database, key)
			rows = append(rows, LexiconRow{
				Class: id, Name: key, Blurb: command.Blurb, Usage: command.Usage, Scopes: command.Scopes,
				Target: command.Target, Examples: listExamples(ws, mods, class.Database, key),
			})
		}

		rows = append(rows, listScripted(ws, mods, class)...)

	case id == "scope":
		for _, key := range ws.Schema.ListKeys(lang.DumpTypes) {
			rows = append(rows, LexiconRow{Class: id, Name: key, Examples: []lsp.Site{}})
		}

	case id == "function":
		for _, dataType := range ws.Schema.DataTypes {
			for _, function := range dataType.Functions {
				rows = append(rows, LexiconRow{
					Class: id, Name: dataType.Name + "." + function.Name, Blurb: function.Blurb,
					Target: lang.ReturnType(function), Examples: toExamples(ws, mods, function.Examples),
				})
			}
		}

	default:
		return nil, fmt.Errorf("%q is not a lexicon class", id)
	}

	slices.SortFunc(rows, func(first, second LexiconRow) int {
		return cmp.Or(strings.Compare(strings.ToLower(first.Name), strings.ToLower(second.Name)), strings.Compare(first.Name, second.Name))
	})

	return rows, nil
}

// listExamples returns the first uses of a name in the index, up to
// schema.ExampleCount.
func listExamples(ws *workspace.Session, mods *index.Index, database, name string) []lsp.Site {
	uses := mods.ListUses(database, name)
	uses = uses[:min(len(uses), schema.ExampleCount)]
	sites := make([]lsp.Site, len(uses))
	for position, use := range uses {
		sites[position] = lsp.MakeSite(ws, mods, use)
	}

	return sites
}

// listScripted returns a row for each scripted definition that acts as a
// command of a class.
func listScripted(ws *workspace.Session, mods *index.Index, class lsp.Class) []LexiconRow {
	var rows []LexiconRow
	for _, database := range lang.Called {
		owner := ws.Schema.Databases[database]
		if owner == nil || owner.Declaration.Command() != class.Database {
			continue
		}

		for _, winner := range mods.ListKept(ws.Schema, database) {
			rows = append(rows, makeScripted(ws, mods, class.ID, mods.ToReference(winner.Site), winner.Site))
		}
	}

	return rows
}

// makeScripted returns the row of a scripted definition. Its usage is "" when
// the definition reads no parameter.
func makeScripted(ws *workspace.Session, mods *index.Index, class string, reference lang.Reference, kept index.Site) LexiconRow {
	usage := ""
	if tree, statement, err := ws.FindStatement(mods, kept); err == nil {
		var parameters []string
		for _, key := range slices.Sorted(maps.Keys(lang.ListParameters(tree, statement).Children)) {
			if !strings.HasPrefix(key, "<") {
				parameters = append(parameters, key+" = …")
			}
		}

		if len(parameters) > 0 {
			usage = reference.Name + " = { " + strings.Join(parameters, " ") + " }"
		}
	}

	defined := lsp.MakeSite(ws, mods, kept)

	return LexiconRow{
		Class: class, Name: reference.Name, Usage: usage, Defined: &defined,
		Examples: listExamples(ws, mods, reference.Database, reference.Name),
	}
}

// toExamples turns the schema's examples into sites, counting lines from one.
func toExamples(ws *workspace.Session, mods *index.Index, examples []lang.Example) []lsp.Site {
	sites := make([]lsp.Site, len(examples))
	for position, example := range examples {
		sites[position] = lsp.Site{Place: ws.Locate(mods, example.Path), Line: example.Line + 1}
	}

	return sites
}
