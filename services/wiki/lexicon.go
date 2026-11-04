// lexicon.go builds the lexicon of a class of script commands: the effects, triggers, links, scopes and data functions
// the game's dumps declare, and the scripted effects and triggers the mods define, each with its example uses.

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

// A LexiconRow is one effect, trigger, link, scope or data function: one the game's dumps declare, with the dump's
// text, or a scripted effect or trigger, with how a call to it is written. Each has example uses from the index.
type LexiconRow struct {
	Class    string     `json:"class"` // effect, trigger, link, scope, function
	Name     string     `json:"name"`
	Blurb    string     `json:"blurb"`
	Usage    string     `json:"usage"`
	Scopes   []string   `json:"scopes,omitempty"`
	Target   string     `json:"target,omitempty"`
	Examples []lsp.Site `json:"examples"`
	// Defined is where the copy the game keeps of a scripted effect or trigger is, or nil for what the dumps declare.
	Defined *lsp.Site `json:"defined,omitempty"`
}

// ListClasses returns the lexicon's classes, in the order its tabs show them.
func (wik *Service) ListClasses() []lsp.Class { return lsp.Classes }

// BuildLexicon builds the lexicon rows of the given class: effect, trigger, link, scope or function. Effects and
// triggers take in the scripted ones the workspace defines. The rows are sorted by name, letter case aside, so each
// first letter is one run. A class built already for the current index is answered from memory. It returns an error
// if no workspace is open or the class is none of those.
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

// buildLexicon builds the lexicon rows of the class with the given id from the given index of the mods, as
// BuildLexicon describes them. It returns an error if the id is none of the five classes'.
func buildLexicon(ws *workspace.Session, mods *index.Index, id string) ([]LexiconRow, error) {
	var rows []LexiconRow
	class, _ := lo.Find(lsp.Classes, func(class lsp.Class) bool { return class.ID == id })
	switch {
	case class.Declared != "":
		for name, command := range ws.Schema.Commands {
			if slices.Contains(command.Declared, class.Declared) {
				rows = append(rows, LexiconRow{
					Class: id, Name: name, Blurb: command.Blurb, Usage: command.Usage, Scopes: command.Scopes,
					Target: command.Target, Examples: toExamples(ws, mods, command.Examples),
				})
			}
		}
		rows = append(rows, listScripted(ws, mods, id)...)
	case id == "scope":
		for name := range ws.Schema.Scopes {
			rows = append(rows, LexiconRow{Class: id, Name: name, Examples: []lsp.Site{}})
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

// toExamples converts each given place where the game's own files use a name to an example. Each carries its place in
// the open workspace, as the given index of the mods holds it.
func toExamples(ws *workspace.Session, mods *index.Index, examples []schema.Example) []lsp.Site {
	return lo.Map(examples, func(example schema.Example, _ int) lsp.Site {
		return lsp.Site{Place: ws.Locate(mods, example.Path), Line: example.Line + 1}
	})
}

// listScripted returns the lexicon rows of the scripted definitions that act as commands of the given class, one for
// each name the game keeps a copy of.
func listScripted(ws *workspace.Session, mods *index.Index, class string) []LexiconRow {
	var rows []LexiconRow
	for database, callable := range game.Callables {
		if callable.Command != class {
			continue
		}
		for _, winner := range mods.ListKept(ws.Schema, database) {
			rows = append(rows, makeScripted(ws, mods, class, mods.ToReference(winner.Site), winner.Site))
		}
	}
	return rows
}

// makeScripted returns the lexicon row of a scripted definition: where the copy the game keeps is, the first places
// that use the name, and how a call to it is written with the parameters its body reads. A definition that reads no
// parameter has no usage, since it is called as name = yes.
func makeScripted(ws *workspace.Session, mods *index.Index, class string, reference schema.Reference, kept index.Site) LexiconRow {
	usage := ""
	if tree, statement, err := ws.FindStatement(mods, kept); err == nil {
		parameters := lo.Filter(slices.Sorted(maps.Keys(schema.ListParameters(tree, statement).Children)), func(key string, _ int) bool {
			return !strings.HasPrefix(key, "<")
		})
		if len(parameters) > 0 {
			usage = reference.Name + " = { " + strings.Join(lo.Map(parameters, func(parameter string, _ int) string {
				return parameter + " = …"
			}), " ") + " }"
		}
	}
	uses := mods.ListUses(reference.Database, reference.Name)
	defined := lsp.MakeSite(ws, mods, kept)
	return LexiconRow{
		Class: class, Name: reference.Name, Usage: usage, Defined: &defined,
		Examples: lo.Map(uses[:min(len(uses), schema.ExampleCount)], func(use index.Site, _ int) lsp.Site {
			return lsp.MakeSite(ws, mods, use)
		}),
	}
}
