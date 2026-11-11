// lexicon.go builds the lexicon of a class of script commands, each with
// places the files write it.

package wiki

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// A LexiconRow is one effect, trigger, link, scope or data function the
// game's dumps declare, or one scripted effect or trigger. Its examples are
// the first uses the index finds; a data function's are the first places the
// game's own files call it, as the schema keeps them.
type LexiconRow struct {
	Class    string     `json:"class"` // effect, trigger, link, scope, function
	Name     string     `json:"name"`
	Blurb    string     `json:"blurb"`
	Usage    string     `json:"usage"`
	Scopes   []string   `json:"scopes,omitempty"`
	Target   string     `json:"target,omitempty"`
	Examples []lsp.Site `json:"examples"`
	Defined  *lsp.Site  `json:"defined,omitempty"` // where the game's copy of a scripted effect or trigger is; nil for what the dumps declare
}

// ListClasses returns the lexicon's classes, in the order its tabs show them.
func (wik *Service) ListClasses() []lsp.Class { return lsp.Classes }

// BuildLexicon returns the rows of the given class (effect, trigger, link,
// scope or function), sorted by name. Effects and triggers include the
// scripted ones. It returns an error if no workspace is open or the class is
// unknown.
func (wik *Service) BuildLexicon(class string) ([]LexiconRow, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	wik.mu.Lock()
	defer wik.mu.Unlock()
	if wik.mods != mods {
		wik.lexicon, wik.mods = map[string][]LexiconRow{}, mods
	}

	if rows, ok := wik.lexicon[class]; ok {
		return rows, nil
	}

	rows, err := buildLexicon(ws, mods, class)
	if err == nil {
		wik.lexicon[class] = rows
	}

	return rows, err
}

// buildLexicon builds the rows of the class with the given id. It returns an
// error if the id is not a lexicon class.
func buildLexicon(ws *workspace.Session, mods *index.Index, id string) ([]LexiconRow, error) {
	var rows []LexiconRow
	class, _ := lo.Find(lsp.Classes, func(class lsp.Class) bool { return class.ID == id })
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
		for _, key := range ws.Schema.ListKeys(game.DumpTypes) {
			rows = append(rows, LexiconRow{Class: id, Name: key, Examples: []lsp.Site{}})
		}

	case id == "function":
		for _, dataType := range ws.Schema.DataTypes {
			for _, function := range dataType.Functions {
				rows = append(rows, LexiconRow{
					Class: id, Name: dataType.Name + "." + function.Name, Blurb: function.Blurb,
					Target: schema.ReturnType(function), Examples: toExamples(ws, mods, function.Examples),
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

// listExamples returns the first places the index finds a name of a database
// used, up to schema.ExampleCount.
func listExamples(ws *workspace.Session, mods *index.Index, database, name string) []lsp.Site {
	uses := mods.ListUses(database, name)

	return lo.Map(uses[:min(len(uses), schema.ExampleCount)], func(use index.Site, _ int) lsp.Site {
		return lsp.MakeSite(ws, mods, use)
	})
}

// toExamples turns the schema's examples into sites, with lines counted from
// one.
func toExamples(ws *workspace.Session, mods *index.Index, examples []schema.Example) []lsp.Site {
	return lo.Map(examples, func(example schema.Example, _ int) lsp.Site {
		return lsp.Site{Place: ws.Locate(mods, example.Path), Line: example.Line + 1}
	})
}

// listScripted returns a row for each scripted definition the game keeps
// that acts as a command of the given class.
func listScripted(ws *workspace.Session, mods *index.Index, class lsp.Class) []LexiconRow {
	var rows []LexiconRow
	for database, callable := range game.Callables {
		if callable.Command != class.Database {
			continue
		}

		for _, winner := range mods.ListKept(ws.Schema, database) {
			rows = append(rows, makeScripted(ws, mods, class.ID, mods.ToReference(winner.Site), winner.Site))
		}
	}

	return rows
}

// makeScripted returns the lexicon row of a scripted definition, with where
// the game's copy is defined and the first places that use it. Its usage is
// "" if the definition reads no parameter.
func makeScripted(ws *workspace.Session, mods *index.Index, class string, reference schema.Reference, kept index.Site) LexiconRow {
	usage := ""
	if tree, statement, err := ws.FindStatement(mods, kept); err == nil {
		var parameters []string
		for _, key := range slices.Sorted(maps.Keys(schema.ListParameters(tree, statement).Children)) {
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
