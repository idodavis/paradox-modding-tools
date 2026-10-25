package lsp

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
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

// A ColorSite is one color written in a file: where it is and the color it stands for.
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

// Inputs is the Form view of one definition: the inputs its database offers, with the values the definition writes.
type Inputs struct {
	Database string            `json:"database"`
	Range    Range             `json:"range"` // the definition's whole statement
	Inputs   []schema.Input    `json:"inputs"`
	Values   map[string]string `json:"values"` // by key path, a.b for b inside a; only the keys the definition writes
	Blocks   map[string]Range  `json:"blocks"` // by key path: each block the definition writes, for an edit inside it
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
		start, end := tree.Lines.ToPosition(statement.Block.Start).Line, tree.Lines.ToPosition(statement.Block.End).Line
		if end > start {
			folds = append(folds, Fold{Start: start + 1, End: end + 1})
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
	place := ws.Locate(ws.Mods.Load(), path)
	mod, _ := lsp.Workspace.LookupMod(place.Origin)
	visitor := &hintsVisitor{calendar: mod.Calendar, hints: []Hint{}}
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	return visitor.hints, nil
}

// A hintsVisitor collects the hints of a file.
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

// Leave does nothing.
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
			span := tree.Statements[statement].ResolveValueSpan()
			sites = append(sites, ColorSite{Range: ToRange(tree.Src, tree.Lines, span), Color: color})
		}
	}
	return sites, nil
}

// PresentColor returns the text that writes a color back into a file, in the same form as the color written in the
// given range. It returns an error if there is no color in the range or its form cannot be written.
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
	ws.Schema.WalkTo(tree, ws.Locate(ws.Mods.Load(), path).File, offset, visitor)
	call, callee, key, ok := findCall(ws, tree, visitor)
	if !ok {
		return nil, nil
	}
	parameters := lo.Filter(slices.Sorted(maps.Keys(callee.Children)), func(key string, _ int) bool {
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

// ListInputs returns the Form view of the innermost definition at a position, or else of the first one on its line.
// It returns an error if there is none.
func (lsp *Service) ListInputs(path string, line, column int) (Inputs, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return Inputs{}, err
	}
	place := ws.Locate(ws.Mods.Load(), path)
	visitor := &definitionFinder{offset: ToOffset(tree.Src, tree.Lines, line, column), line: line - 1}
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	if visitor.database == "" {
		return Inputs{}, fmt.Errorf("no definition is at line %d of %s", line, path)
	}
	inputs := Inputs{
		Database: visitor.database, Range: ToRange(tree.Src, tree.Lines, tree.Statements[visitor.statement].Span),
		Inputs: append([]schema.Input{}, ws.Schema.ListInputs(visitor.database)...),
		Values: map[string]string{}, Blocks: map[string]Range{},
	}
	listPaths(tree, visitor.statement, "", &inputs)
	return inputs, nil
}

// A definitionFinder finds the definition that the Form view shows at a position.
type definitionFinder struct {
	offset    int    // the position's byte offset
	line      int    // the position's line, counted from zero
	statement int32  // the definition found so far
	database  string // its database; "" while none is found
	covers    bool   // whether the definition found covers the offset
}

// Enter takes a definition that covers the offset, the innermost winning, or else the first one on the line.
func (visitor *definitionFinder) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if !context.Defines || context.Definition.Alias || context.Definition.Database == game.Constant {
		return true
	}
	span := tree.Statements[statement].Span
	switch {
	case span.Contains(visitor.offset):
		visitor.statement, visitor.database, visitor.covers = statement, context.Definition.Database, true
	case !visitor.covers && visitor.database == "" && tree.Lines.ToPosition(span.Start).Line == visitor.line:
		visitor.statement, visitor.database = statement, context.Definition.Database
	}
	return true
}

// Leave does nothing.
func (visitor *definitionFinder) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// listPaths records each value and block range a statement's children write, by key path. A key written twice keeps
// the last.
func listPaths(tree *jomini.Tree, holder int32, prefix string, inputs *Inputs) {
	for child := range tree.Children(holder) {
		current := tree.Statements[child]
		if current.Key.IsEmpty() {
			continue
		}
		key := prefix + tree.Unquoted(current.Key)
		if !current.IsBlock() {
			inputs.Values[key] = tree.Text(current.Value)
			continue
		}
		inputs.Blocks[key] = ToRange(tree.Src, tree.Lines, current.Span)
		listPaths(tree, child, key+".", inputs)
	}
}

// findCall returns the call to a scripted definition that holds the visited position: its statement, the field that
// lists the parameters the definition takes, and the key it is called by. It returns false if the position is in no
// such call, or if the definition takes no parameters.
func findCall(ws *workspace.Session, tree *jomini.Tree, visitor *hoverVisitor) (int32, *schema.Field, string, bool) {
	for statement, context := visitor.statement, &visitor.context; statement > 0; {
		current := tree.Statements[statement]
		if current.IsBlock() && visitor.offset > current.Block.Start && visitor.offset < current.Block.End &&
			context.Category == schema.CategoryCall && context.Field != nil {
			for _, reference := range lo.FlatMap(context.References, func(reference schema.Reference, _ int) []schema.Reference {
				return index.ListCallees(reference)
			}) {
				if database := ws.Schema.Databases[reference.Database]; reference.Name == tree.Unquoted(current.Key) &&
					database != nil && database.Callable.Parameters {
					return statement, context.Field, reference.Name, true
				}
			}
		}
		statement, context = current.Parent, context.Parent
	}
	return 0, nil, "", false
}
