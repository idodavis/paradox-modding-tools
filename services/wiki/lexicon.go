package wiki

import (
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A LexiconRow is one effect, trigger, link, scope or data function the game's dumps declare, with the dump's text
// and example uses from the index.
type LexiconRow struct {
	Class    string    `json:"class"` // effect, trigger, link, scope, function
	Name     string    `json:"name"`
	Blurb    string    `json:"blurb"`
	Usage    string    `json:"usage"`
	Scopes   []string  `json:"scopes,omitempty"`
	Target   string    `json:"target,omitempty"`
	Examples []Example `json:"examples"`
}

// An Example is one place where the game's own files use a lexicon row's name.
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

// BuildLexicon builds the lexicon rows of the given class: effect, trigger, link, scope or function. The rows are
// sorted by name. It returns an error if no workspace is open or the class is none of those.
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
	slices.SortFunc(rows, func(first, second LexiconRow) int { return strings.Compare(first.Name, second.Name) })
	return rows, nil
}

// toExamples converts each given place where the game's own files use a name to an example. Each carries its place in
// the open workspace, as the given index of the mods holds it.
func toExamples(ws *workspace.Session, mods *index.Index, examples []schema.Example) []Example {
	return lo.Map(examples, func(example schema.Example, _ int) Example {
		return Example{Place: ws.Locate(mods, example.Path), Line: example.Line + 1}
	})
}
