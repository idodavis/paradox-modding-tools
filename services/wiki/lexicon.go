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
	"paradox-modding-tools/services/workspace"
)

// A LexiconRow is one effect, trigger, link, scope or data function: one the game's dumps declare, with the dump's
// text, or a scripted effect or trigger, with how a call to it is written. Each has example uses from the index.
type LexiconRow struct {
	Class    string    `json:"class"` // effect, trigger, link, scope, function
	Name     string    `json:"name"`
	Blurb    string    `json:"blurb"`
	Usage    string    `json:"usage"`
	Scopes   []string  `json:"scopes,omitempty"`
	Target   string    `json:"target,omitempty"`
	Examples []Example `json:"examples"`
	// Defined is where the copy the game keeps of a scripted effect or trigger is, or nil for what the dumps declare.
	Defined *Example `json:"defined,omitempty"`
}

// An Example is one place in the workspace's files: where a lexicon row's name is used, or defined.
type Example struct {
	Place workspace.Place `json:"place"`
	Line  int             `json:"line"` // counted from one
}

// declaredClasses are the lexicon classes of the commands the dumps declare, each with the word the dumps use for
// what declares it.
var declaredClasses = map[string]string{
	"effect":  game.DeclaredEffect,
	"trigger": game.DeclaredTrigger,
	"link":    game.DeclaredLink,
}

// BuildLexicon builds the lexicon rows of the given class: effect, trigger, link, scope or function. Effects and
// triggers take in the scripted ones the workspace defines. The rows are sorted by name, letter case aside, so each
// first letter is one run. It returns an error if no workspace is open or the class is none of those.
func (wik *Service) BuildLexicon(class string) ([]LexiconRow, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	var rows []LexiconRow
	switch declared := declaredClasses[class]; {
	case declared != "":
		for name, command := range ws.Schema.Commands {
			if slices.Contains(command.Declared, declared) {
				rows = append(rows, LexiconRow{
					Class: class, Name: name, Blurb: command.Blurb, Usage: command.Usage, Scopes: command.Scopes,
					Target: command.Target, Examples: toExamples(ws, mods, command.Examples),
				})
			}
		}
		rows = append(rows, listScripted(ws, mods, class)...)
	case class == "scope":
		for name := range ws.Schema.Scopes {
			rows = append(rows, LexiconRow{Class: class, Name: name, Examples: []Example{}})
		}
	case class == "function":
		for _, dataType := range ws.Schema.DataTypes {
			for _, function := range dataType.Functions {
				rows = append(rows, LexiconRow{
					Class: class, Name: dataType.Name + "." + function.Name, Blurb: function.Blurb,
					Target: function.Returns, Examples: toExamples(ws, mods, function.Examples),
				})
			}
		}
	default:
		return nil, fmt.Errorf("%q is not a lexicon class", class)
	}
	slices.SortFunc(rows, func(first, second LexiconRow) int {
		return cmp.Or(strings.Compare(strings.ToLower(first.Name), strings.ToLower(second.Name)), strings.Compare(first.Name, second.Name))
	})
	return rows, nil
}

// toExamples converts each given place where the game's own files use a name to an example. Each carries its place in
// the open workspace, as the given index of the mods holds it.
func toExamples(ws *workspace.Session, mods *index.Index, examples []schema.Example) []Example {
	return lo.Map(examples, func(example schema.Example, _ int) Example {
		return Example{Place: ws.Locate(mods, example.Path), Line: example.Line + 1}
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
		listed := map[string]bool{}
		for _, site := range mods.ListDefinitions(database) {
			reference := mods.ToReference(site)
			if listed[reference.Name] {
				continue
			}
			listed[reference.Name] = true
			if winner, ok := mods.FindKept(ws.Schema, reference); ok {
				rows = append(rows, makeScripted(ws, mods, class, reference, winner.Site))
			}
		}
	}
	return rows
}

// makeScripted returns the lexicon row of a scripted definition: how a call to the copy the game keeps is written,
// with the parameters its body reads, where that copy is and the first places that use the name.
func makeScripted(ws *workspace.Session, mods *index.Index, class string, reference schema.Reference, kept index.Site) LexiconRow {
	usage := reference.Name + " = yes"
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
	defined := locateExample(ws, mods, kept)
	return LexiconRow{
		Class: class, Name: reference.Name, Usage: usage, Defined: &defined,
		Examples: lo.Map(uses[:min(len(uses), schema.ExampleCount)], func(use index.Site, _ int) Example {
			return locateExample(ws, mods, use)
		}),
	}
}

// locateExample returns the example of one site of the index: its file's place and its line.
func locateExample(ws *workspace.Session, mods *index.Index, site index.Site) Example {
	return Example{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
}
