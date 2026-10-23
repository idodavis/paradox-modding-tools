package lsp

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Fold is one foldable region: its first and last lines, counted from one.
type Fold struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// A Hint is a text drawn inline beside the code, such as a date read in the mod's calendar. Its line and column are
// Monaco's, counted from one, the column in UTF-16 code units.
type Hint struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

// A ColorSite is one color written in a file and the color it makes.
type ColorSite struct {
	Range Range     `json:"range"`
	Color game.RGBA `json:"color"`
}

// A Signature is the parameter list of a scripted definition at a call, with the parameter the position is on.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`
	Active     *int     `json:"active,omitempty"` // the parameter the position is on; absent if none
}

// ListFolds returns a file's foldable regions: every block that spans more than one line.
func (lsp *Service) ListFolds(path string) ([]Fold, error) {
	_, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	folds := []Fold{}
	for _, statement := range tree.Statements {
		if !statement.IsBlock() {
			continue
		}
		span := ToRange(tree.Src, tree.Lines, statement.Block)
		if span.EndLine > span.StartLine {
			folds = append(folds, Fold{Start: span.StartLine, End: span.EndLine})
		}
	}
	return folds, nil
}

// ListHints returns a file's inlay hints: each date in the file read in the mod's calendar. A value is a date only
// where its field expects one.
func (lsp *Service) ListHints(path string) ([]Hint, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	place := ws.Locate(path)
	mod, _ := lsp.Workspace.Config.LookupMod(ws.ID, place.Origin)
	visitor := &hintsVisitor{calendar: mod.Calendar, hints: []Hint{}}
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	return visitor.hints, nil
}

// A hintsVisitor is the visitor behind ListHints. It records a hint after each date the file writes in a field that
// expects one.
type hintsVisitor struct {
	calendar app.Calendar
	hints    []Hint
}

// Enter records the hint for the statement's value if it is a date.
func (visitor *hintsVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if date, ok := findDate(tree, statement, context); ok {
		end := tree.Statements[statement].Value.End
		point := ToRange(tree.Src, tree.Lines, jomini.Span{Start: end, End: end})
		visitor.hints = append(visitor.hints,
			Hint{Line: point.StartLine, Column: point.StartColumn, Text: describeDate(resolveDate(visitor.calendar, date))})
	}
	return true
}

// Leave does nothing: a hint is complete when its statement is entered.
func (visitor *hintsVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// describeDate writes a date read in a mod's calendar as a short phrase: the day, the month's name, the year and the
// era. A date that leaves out its day or its month leaves them out of the phrase.
func describeDate(card *CardDate) string {
	parts := []string{}
	if card.Date.Day > 0 {
		parts = append(parts, fmt.Sprint(card.Date.Day))
	}
	if card.Month != "" {
		parts = append(parts, card.Month)
	}
	parts = append(parts, fmt.Sprint(card.Date.Year))
	if card.Era != "" {
		parts = append(parts, card.Era)
	}
	return strings.Join(parts, " ")
}

// ListColorSites returns every color written in a file, with the color it makes.
func (lsp *Service) ListColorSites(path string) ([]ColorSite, error) {
	_, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	sites := []ColorSite{}
	for statement := int32(1); int(statement) < len(tree.Statements); statement++ {
		if color, ok := game.ToColor(tree, statement); ok {
			sites = append(sites, ColorSite{Range: ToRange(tree.Src, tree.Lines, findColorSpan(tree, statement)), Color: color})
		}
	}
	return sites, nil
}

// findColorSpan returns the span of the color a statement writes: its tag, if it has one, and its block.
func findColorSpan(tree *jomini.Tree, statement int32) jomini.Span {
	current := tree.Statements[statement]
	span := jomini.Span{Start: current.Block.Start, End: current.Block.End}
	if !current.Tag.IsEmpty() {
		span.Start = current.Tag.Start
	}
	return span
}

// PresentColor returns the text that writes a color back into a file in the same form as the block at the given
// range, for the color picker.
func (lsp *Service) PresentColor(path string, within Range, color game.RGBA) (string, error) {
	_, tree, err := lsp.loadTree(path)
	if err != nil {
		return "", err
	}
	statement := tree.FindStatement(ToOffset(tree.Src, tree.Lines, within.StartLine, within.StartColumn))
	tag, parts, ok := jomini.ColorParts(tree, statement)
	if !ok {
		return "", errors.New("there is no color at the range")
	}
	encoded, ok := game.EncodeColor(tag, parts, color)
	if !ok {
		return "", fmt.Errorf("the color form %q cannot be written", tag)
	}
	return strings.TrimSpace(tag + " { " + strings.Join(encoded, " ") + " }"), nil
}

// Signature returns the signature help for a call at a position in a file, or nil if the position is not inside
// a call.
func (lsp *Service) Signature(path string, line, column int) (*Signature, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	visitor := &hoverVisitor{offset: offset}
	ws.Schema.WalkTo(tree, ws.Locate(path).File, offset, visitor)
	call, callee, key, ok := findCall(ws, tree, visitor)
	if !ok {
		return nil, nil
	}
	parameters := lo.Filter(slices.Sorted(maps.Keys(callee.Parameters.Children)), func(key string, _ int) bool {
		return !strings.HasPrefix(key, "<")
	})
	var active *int
	for child := range tree.Children(call) {
		if position := slices.Index(parameters, tree.Unquoted(tree.Statements[child].Key)); position >= 0 &&
			tree.Statements[child].Span.Contains(offset) {
			active = &position
		}
	}
	return &Signature{Label: key + "(" + strings.Join(parameters, ", ") + ")", Parameters: parameters, Active: active}, nil
}

// findCall returns the statement of the call to a scripted definition whose block holds the visited position, with
// the definition and the key it is called by. It looks at the visited statement and then at the blocks that hold it,
// and returns false if the position is in no such call, or if the definition takes no parameters.
func findCall(ws *workspace.Session, tree *jomini.Tree, visitor *hoverVisitor) (int32, schema.ScriptedDefinition, string, bool) {
	for statement, context := visitor.statement, &visitor.context; statement > 0 && context != nil; {
		current := tree.Statements[statement]
		if current.IsBlock() && visitor.offset > current.Block.Start && visitor.offset < current.Block.End {
			for _, reference := range context.References {
				callee, known := ws.Schema.ScriptedDefinitions[reference.Caller+":"+reference.Name]
				if known && reference.Span == current.Key && callee.Parameters != nil {
					return statement, callee, reference.Name, true
				}
			}
		}
		statement, context = current.Parent, context.Parent
	}
	return 0, schema.ScriptedDefinition{}, "", false
}
